// Command server — composition root walking skeleton: config → logger →
// postgres(+миграции) → redis → mqtt(+подписка на статусы) → livekit → wiring
// модулей → HTTP на HTTP_ADDR с graceful shutdown (architecture.md §4.7).
//
// Границы модулей (architecture.md §1): межмодульные интерфейсы объявлены на
// стороне потребителей; здесь они связываются с реализациями через тонкие
// адаптеры, чтобы прикладные пакеты не импортировали друг друга.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // встроенная IANA-база: LoadLocation работает и без zoneinfo ОС (Windows)

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/access"
	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/calls"
	"domofon/backend/internal/devices"
	"domofon/backend/internal/guests"
	"domofon/backend/internal/onboarding"
	"domofon/backend/internal/platform/config"
	"domofon/backend/internal/platform/httpx"
	"domofon/backend/internal/platform/logging"
	"domofon/backend/internal/platform/mqtt"
	"domofon/backend/internal/platform/postgres"
	predis "domofon/backend/internal/platform/redis"
	"domofon/backend/internal/property"
	"domofon/backend/internal/qr"
	"domofon/backend/internal/schedules"
	"domofon/backend/internal/sysadmin"
	"domofon/backend/migrations"
)

func main() {
	cfg := config.Load()
	log := logging.New(cfg.LogLevel)

	rootCtx := context.Background()

	// --- Postgres: миграции (обязательны) + пул ---
	if err := postgres.Migrate(cfg.DatabaseURL, migrations.FS); err != nil {
		log.Error("migrations_failed", "error", err)
		os.Exit(1)
	}
	pool, err := postgres.Connect(rootCtx, cfg.DatabaseURL)
	if err != nil {
		log.Error("postgres_connect_failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	log.Info("postgres_ready")

	// --- Redis (не фатально: /health покажет статус) ---
	rdb, err := predis.Connect(rootCtx, cfg.RedisAddr)
	if err != nil {
		log.Warn("redis_ping_failed", "error", err)
	}
	defer func() { _ = rdb.Close() }()

	// --- MQTT (не фатально: auto-reconnect) ---
	mqttClient := mqtt.New(cfg.MQTTBrokerURL, cfg.MQTTClientID, log)
	connectCtx, cancelConnect := context.WithTimeout(rootCtx, 5*time.Second)
	if err := mqttClient.Connect(connectCtx); err != nil {
		log.Warn("mqtt_connect_failed", "error", err)
	}
	cancelConnect()
	defer mqttClient.Disconnect()

	// --- LiveKit ---
	livekit := calls.NewLiveKit(cfg.LiveKitURL, cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)

	// --- Модули (владельцы состояния) ---
	propertyRepo := property.NewRepo(pool)
	propertySvc := property.NewService(propertyRepo)

	keyring, err := qr.NewKeyring(rootCtx, pool)
	if err != nil {
		log.Error("keyring_load_failed", "error", err)
		os.Exit(1)
	}
	// Prod-гэп §17.4: секрет подписи QR можно подать из окружения (не хранить в
	// БД plaintext). Пусто → используется сид qr_keys (dev).
	var qrKeyring qr.Keyring = keyring
	if cfg.QRSecret != "" {
		qrKeyring = qrSecretOverride{inner: keyring, kid: cfg.QRKid, secret: cfg.QRSecret}
		log.Info("qr_secret_from_env", "kid", cfg.QRKid)
	}

	presence := devices.NewPresence(rdb)
	deviceRepo := devices.NewRepo(pool)
	commander := devices.NewCommander(mqttClient)
	cmdCtxStore := devices.NewCommandContextStore(rdb)
	recorder := audit.NewRecorder(pool, log)

	// --- Auth / RBAC (auth.md) ---
	// Ключи RS256: env-PEM, dev-fallback при AuthDevMode, иначе fail-closed.
	jwtPriv, jwtPub, err := auth.LoadKeys(cfg.JWTPrivateKey, cfg.JWTPublicKey, cfg.AuthDevMode)
	if err != nil {
		log.Error("auth_keys_load_failed", "error", err)
		os.Exit(1)
	}
	if cfg.AuthDevMode {
		log.Warn("auth_dev_mode_enabled", "hint", "фиксированный dev-keypair — НЕ ДЛЯ ПРОД")
	}
	authVerifier := auth.NewRSAVerifier(jwtPub)
	authn := auth.Authenticator(authVerifier)
	authRepo := auth.NewRepo(pool)
	otpService := auth.NewOtpService(auth.NewRedisOtpStore(rdb), auth.NewDevSender(log), log)
	refreshWL := auth.NewRefreshWhitelist(auth.NewRedisRefreshKV(rdb))
	authSvc := auth.NewService(authRepo, otpService, refreshWL, jwtPriv, jwtPub, recorder, cfg.AuthDevMode, log)
	authHandler := auth.NewHandler(authSvc)
	// Адаптер авторизации по квартире (claims из ctx → 403 при отказе).
	authorizer := apartmentAuthorizer{}

	sessions := calls.NewStore(rdb)
	// SSE per-apartment: подписчик получает потоки квартир своих ролей (claims).
	sseHub := calls.NewSSEHub(log, auth.ApartmentsFromContext)

	statusConsumer := devices.NewStatusConsumer(presence, deviceRepo, recorder, cmdCtxStore, log)
	if err := mqttClient.Subscribe("devices/+/status", statusConsumer.Handle); err != nil {
		log.Warn("mqtt_subscribe_status_failed", "error", err)
	}

	// --- Сервисы (внедрение через адаптеры границ) ---
	callSvc := calls.NewService(
		qrVerifierAdapter{keyring: qrKeyring},
		callsPropertyAdapter{svc: propertySvc},
		presence,
		livekit,
		sseHub,
		sessions,
		authorizer,
		recorder,
		log,
	)

	accessSvc := access.NewService(
		callStoreAdapter{store: sessions},
		presence,
		commandPublisherAdapter{commander: commander},
		cmdCtxStore,
		authorizer,
		recorder,
		log,
	)

	// --- Онбординг + гранты (онбординг) ---
	// Репозиторий грантов реализует резолвер/листер access (прямое открытие
	// калиток/шлагбаумов по постоянному гранту) — внедряется сеттерами.
	onboardingRepo := onboarding.NewRepo(pool)
	onboardingSvc := onboarding.NewService(onboardingRepo, authSvc, cfg.VisitorBaseURL, recorder, log)
	onboardingHandler := onboarding.NewHandler(onboardingSvc)
	accessSvc.SetPointResolver(onboardingRepo)
	accessSvc.SetPointLister(onboardingRepo)

	// --- Гостевой доступ (инкремент B) ---
	// Открытие точки гостем переиспользует access-машинерию через адаптер
	// DoorOpener → accessSvc.OpenResolved (presence/publish/audit).
	guestsRepo := guests.NewRepo(pool)
	guestsSvc := guests.NewService(guestsRepo, guestDoorOpener{svc: accessSvc}, presence, cfg.VisitorBaseURL, recorder, log)
	guestsHandler := guests.NewHandler(guestsSvc)

	// --- Платформенная админка (инкремент C) ---
	sysadminSvc := sysadmin.NewService(sysadmin.NewRepo(pool), recorder, log)
	sysadminHandler := sysadmin.NewHandler(sysadminSvc)

	// --- Планировщик авто-открытия по расписанию (инкремент E) ---
	schedulesRepo := schedules.NewRepo(pool)
	schedulesHandler := schedules.NewHandler(schedules.NewService(schedulesRepo, recorder, log))
	reconciler := schedules.NewReconciler(pool, schedulesRepo, schedulePublisher{commander: commander}, presence, recorder, cfg.SchedulerTick, cfg.SchedulerLease, log)

	// --- Хендлеры ---
	qrHandler := qr.NewHandler(qrKeyring, qrPropertyAdapter{svc: propertySvc}, presence)
	callsHandler := calls.NewHandler(callSvc)
	accessHandler := access.NewHandler(accessSvc, auth.SubjectFromContext)
	devicesHandler := devices.NewHandler(deviceRepo, presence)
	auditHandler := audit.NewHandler(recorder, auth.MCIDFromContext)

	// --- Роутер ---
	r := httpx.NewRouter(log)
	r.Get("/health", httpx.Health(map[string]httpx.HealthFunc{
		"postgres": func(ctx context.Context) error { return pool.Ping(ctx) },
		"redis":    func(ctx context.Context) error { return rdb.Ping(ctx).Err() },
		"mqtt": func(_ context.Context) error {
			if mqttClient.IsConnected() {
				return nil
			}
			return errors.New("mqtt disconnected")
		},
		"livekit": func(ctx context.Context) error { return livekit.Health(ctx) },
	}))
	// Rate-limit на публичных (неаутентифицированных) POST — защита от абуза
	// (prod-гэп §5.6). 30 запросов/мин на IP: щедро для демо, режет флуд.
	publicRL := httpx.RateLimit(30, time.Minute)
	// Строгий лимитер для чувствительных auth-эндпоинтов (per-IP): OTP-запрос и
	// вход админа. OTP-per-phone лимит — уже в auth.OtpService.
	authRL := httpx.RateLimit(10, time.Minute)
	r.Route("/api/v1", func(r chi.Router) {
		// --- Публичные (без токена) под publicRL ---
		r.With(publicRL).Post("/qr/validate", qrHandler.Validate)
		r.With(publicRL).Post("/calls/initiate", callsHandler.Initiate)
		// cancel/end авторизуются обладанием call_id (capability), остаются публичными.
		r.With(publicRL).Post("/calls/{id}/cancel", callsHandler.Cancel)
		r.With(publicRL).Post("/calls/{id}/end", callsHandler.End)

		// --- Auth-эндпоинты (публичные, но со своими лимитерами; /me — под authn) ---
		r.Route("/auth", func(r chi.Router) {
			r.With(authRL).Post("/otp/send", authHandler.OtpSend)
			r.With(authRL).Post("/otp/verify", authHandler.OtpVerify)
			r.With(authRL).Post("/admin/login", authHandler.AdminLogin)
			r.With(publicRL).Post("/refresh", authHandler.Refresh)
			r.With(publicRL).Post("/logout", authHandler.Logout)
			// Приём инвайта — публичный (вход без OTP по секрет-ссылке), под authRL.
			r.With(authRL).Post("/invite/accept", onboardingHandler.AcceptInvite)
			r.With(authn).Get("/me", authHandler.Me)
		})

		// --- Гостевой доступ по ссылке (публичный, по токену-capability) ---
		// GET страницы — под publicRL; открытие — под строгим authRL (чувствительнее).
		r.With(publicRL).Get("/g/{token}", guestsHandler.View)
		r.With(authRL).Post("/g/{token}/open", guestsHandler.Open)

		// --- Resident-only (authn + RequireResident + доменная apartment-проверка) ---
		r.Group(func(r chi.Router) {
			r.Use(authn)
			r.Use(auth.RequireResident)
			r.Post("/calls/{id}/accept", callsHandler.Accept)
			r.Post("/access/open", accessHandler.Open)
			r.Get("/resident/events", sseHub.Handler())
			// Прямое открытие калиток/шлагбаумов по гранту (без звонка).
			r.Get("/access/points", accessHandler.ListPoints)
			r.Post("/access/open-point", accessHandler.OpenPoint)
			// Владелец приглашает жильца в свою квартиру (проверка ownership — в сервисе).
			r.Post("/apartments/{apartment_id}/residents/invite", onboardingHandler.InviteResident)

			// Гостевой доступ: создание/список/отзыв + делегирование права (гости).
			r.Get("/apartments/{apartment_id}/guest-points", guestsHandler.GuestPoints)
			r.Post("/apartments/{apartment_id}/guests", guestsHandler.CreateGuest)
			r.Get("/apartments/{apartment_id}/guests", guestsHandler.ListGuests)
			r.Post("/guests/{guest_id}/revoke", guestsHandler.RevokeGuest)
			r.Put("/apartments/{apartment_id}/residents/{user_id}/guest-permission", guestsHandler.SetPermission)
		})

		// --- Admin-only (authn + RequireAdmin, скоуп выборок по mc_id из claims) ---
		r.Group(func(r chi.Router) {
			r.Use(authn)
			r.Use(auth.RequireAdmin)
			r.Get("/devices", devicesHandler.List)
			r.Get("/audit/events", auditHandler.List)
			// Онбординг: УК создаёт владельцев, раздаёт гранты, видит жильцов.
			r.Post("/admin/owners", onboardingHandler.CreateOwner)
			r.Post("/admin/access-grants", onboardingHandler.CreateAccessGrant)
			r.Get("/admin/residents", onboardingHandler.ListResidents)
			r.Get("/admin/catalog", onboardingHandler.ListCatalog)
			// Матрица доступа (инкремент D): объекты, матрица, раскрытие жильцов, тогл гранта.
			r.Get("/admin/sites", onboardingHandler.ListSites)
			r.Get("/admin/sites/{site_id}/matrix", onboardingHandler.SiteMatrix)
			r.Get("/admin/apartments/{apartment_id}/residents", onboardingHandler.ApartmentResidents)
			r.Put("/admin/users/{user_id}/grants/{point_public_id}", onboardingHandler.GrantPoint)
			r.Delete("/admin/users/{user_id}/grants/{point_public_id}", onboardingHandler.RevokePoint)
			// Расписания авто-открытия (инкремент E): точки+окна, создать, удалить.
			r.Get("/admin/schedule-points", schedulesHandler.ListPoints)
			r.Post("/admin/access-points/{public_id}/schedules", schedulesHandler.Create)
			r.Delete("/admin/schedules/{id}", schedulesHandler.Delete)
		})

		// --- SystemAdmin-only (платформенная админка: УК/объекты/дома/подъезды) ---
		r.Group(func(r chi.Router) {
			r.Use(authn)
			r.Use(auth.RequireSystemAdmin)
			r.Get("/system/management-companies", sysadminHandler.ListMCs)
			r.Post("/system/management-companies", sysadminHandler.CreateMC)
			r.Post("/system/management-companies/{mc_id}/admins", sysadminHandler.CreateMCAdmin)
			r.Get("/system/management-companies/{mc_id}/catalog", sysadminHandler.Catalog)
			r.Post("/system/sites", sysadminHandler.CreateSite)
			r.Post("/system/buildings", sysadminHandler.CreateBuilding)
			r.Post("/system/entrances", sysadminHandler.CreateEntrance)
			r.Patch("/system/buildings/{building_id}", sysadminHandler.MoveBuilding)
		})
	})

	// --- HTTP-сервер + graceful shutdown ---
	// Таймауты — защита от slowloris (L1). WriteTimeout НЕ ставим: он оборвал бы
	// длинный SSE-стрим /resident/events; вместо него — IdleTimeout + лимиты тел
	// в хендлерах (http.MaxBytesReader).
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("http_listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http_serve_failed", "error", err)
			os.Exit(1)
		}
	}()

	// Планировщик авто-открытия по расписанию (инкремент E): отдельная горутина,
	// останавливается по shutdown (отмена schedCtx).
	schedCtx, stopSched := context.WithCancel(rootCtx)
	go reconciler.Run(schedCtx)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutdown_started")
	stopSched() // остановить планировщик

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http_shutdown_failed", "error", err)
	}
	log.Info("shutdown_complete")
}

// --- Адаптеры границ модулей (architecture.md §1) ---

// apartmentAuthorizer реализует calls.Authorizer и access.Authorizer поверх
// claims из контекста (auth.md §1, §5): доступ разрешён, если пользователь
// привязан к квартире apartmentID; иначе — ошибка (сервис отдаёт 403 FORBIDDEN).
type apartmentAuthorizer struct{}

func (apartmentAuthorizer) AllowApartment(ctx context.Context, apartmentID string) error {
	if auth.AllowApartmentFromContext(ctx, apartmentID) {
		return nil
	}
	return errors.New("apartment access denied")
}

// qrVerifierAdapter связывает calls.QRVerifier с qr.Verify + keyring.
type qrVerifierAdapter struct{ keyring qr.Keyring }

func (a qrVerifierAdapter) Verify(aid, v, kid, sig string) error {
	return qr.Verify(aid, v, kid, sig, a.keyring)
}

// qrSecretOverride переопределяет секрет для одного kid значением из окружения
// (prod-гэп §17.4); прочие kid делегируются вложенному keyring.
type qrSecretOverride struct {
	inner  qr.Keyring
	kid    string
	secret string
}

func (o qrSecretOverride) Secret(kid string) (string, bool) {
	if kid == o.kid {
		return o.secret, true
	}
	return o.inner.Secret(kid)
}

// qrPropertyAdapter связывает qr.PropertyResolver с property.Service.
type qrPropertyAdapter struct{ svc *property.Service }

func (a qrPropertyAdapter) ResolveByPublicID(ctx context.Context, publicID string) (qr.ResolvedProperty, error) {
	c, err := a.svc.ResolveByPublicID(ctx, publicID)
	if errors.Is(err, property.ErrNotFound) {
		return qr.ResolvedProperty{}, qr.ErrPropertyNotFound
	}
	if err != nil {
		return qr.ResolvedProperty{}, err
	}
	return qr.ResolvedProperty{
		AccessPointPublicID: c.AccessPoint.PublicID,
		AccessPointLabel:    c.AccessPoint.Label,
		ApartmentID:         c.Apartment.ID,
		ApartmentNumber:     c.Apartment.Number,
		DeviceID:            c.Device.ID,
	}, nil
}

// callsPropertyAdapter связывает calls.PropertyResolver с property.Service.
type callsPropertyAdapter struct{ svc *property.Service }

func (a callsPropertyAdapter) ResolveByPublicID(ctx context.Context, publicID string) (calls.Property, error) {
	c, err := a.svc.ResolveByPublicID(ctx, publicID)
	if errors.Is(err, property.ErrNotFound) {
		return calls.Property{}, calls.ErrPropertyNotFound
	}
	if err != nil {
		return calls.Property{}, err
	}
	return calls.Property{
		AccessPointID:       c.AccessPoint.ID,
		AccessPointPublicID: c.AccessPoint.PublicID,
		AccessPointLabel:    c.AccessPoint.Label,
		ApartmentID:         c.Apartment.ID,
		ApartmentNumber:     c.Apartment.Number,
		DeviceID:            c.Device.ID,
		ManagementCompanyID: c.ManagementCompanyID,
	}, nil
}

// callStoreAdapter связывает access.CallStore с calls.Store.
type callStoreAdapter struct{ store *calls.Store }

func (a callStoreAdapter) Lookup(ctx context.Context, callID string) (access.CallSession, bool, error) {
	s, ok, err := a.store.Get(ctx, callID)
	if err != nil || !ok {
		return access.CallSession{}, ok, err
	}
	return access.CallSession{
		CallID:              s.CallID,
		ApartmentID:         s.ApartmentID,
		AccessPointID:       s.AccessPointID,
		DeviceID:            s.DeviceID,
		ManagementCompanyID: s.ManagementCompanyID,
		State:               s.State,
	}, true, nil
}

// guestDoorOpener реализует guests.DoorOpener поверх access.Service.OpenResolved:
// открытие уже разрешённой гостю точки той же машинерией, что грантовый OpenPoint
// (presence → MQTT open_relay → аудит). Маппит guests.ResolvedPoint ↔
// access.GrantedPoint, чтобы модуль guests не импортировал access в доменной логике.
type guestDoorOpener struct{ svc *access.Service }

func (a guestDoorOpener) OpenResolved(ctx context.Context, rp guests.ResolvedPoint, actor string, meta map[string]any) (guests.OpenResult, *httpx.Error) {
	res, apiErr := a.svc.OpenResolved(ctx, access.GrantedPoint{
		DeviceID:            rp.DeviceID,
		AccessPointID:       rp.AccessPointID,
		ApartmentID:         rp.ApartmentID,
		ManagementCompanyID: rp.ManagementCompanyID,
	}, actor, meta)
	if apiErr != nil {
		return guests.OpenResult{}, apiErr
	}
	return guests.OpenResult{RequestID: res.RequestID, Status: res.Status}, nil
}

// schedulePublisher связывает schedules.Publisher с devices.Commander (аренда
// open_relay планировщика — та же публикация, что у ручного открытия).
type schedulePublisher struct{ commander *devices.Commander }

func (a schedulePublisher) PublishOpenRelay(ctx context.Context, deviceID string, cmd schedules.OpenCommand) error {
	return a.commander.PublishOpenRelay(ctx, deviceID, devices.OpenRelayPayload{
		RelayID:    cmd.RelayID,
		DurationMs: cmd.DurationMs,
		RequestID:  cmd.RequestID,
		IssuedBy:   cmd.IssuedBy,
		IssuedAt:   cmd.IssuedAt,
	})
}

// commandPublisherAdapter связывает access.CommandPublisher с devices.Commander.
type commandPublisherAdapter struct{ commander *devices.Commander }

func (a commandPublisherAdapter) PublishOpenRelay(ctx context.Context, deviceID string, cmd access.OpenRelayCommand) error {
	return a.commander.PublishOpenRelay(ctx, deviceID, devices.OpenRelayPayload{
		RelayID:    cmd.RelayID,
		DurationMs: cmd.DurationMs,
		RequestID:  cmd.RequestID,
		IssuedBy:   cmd.IssuedBy,
		IssuedAt:   cmd.IssuedAt,
	})
}
