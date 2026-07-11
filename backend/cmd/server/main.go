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

	"github.com/go-chi/chi/v5"

	"domofon/backend/internal/access"
	"domofon/backend/internal/audit"
	"domofon/backend/internal/calls"
	"domofon/backend/internal/devices"
	"domofon/backend/internal/platform/config"
	"domofon/backend/internal/platform/httpx"
	"domofon/backend/internal/platform/logging"
	"domofon/backend/internal/platform/mqtt"
	"domofon/backend/internal/platform/postgres"
	predis "domofon/backend/internal/platform/redis"
	"domofon/backend/internal/property"
	"domofon/backend/internal/qr"
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

	sessions := calls.NewStore(rdb)
	sseHub := calls.NewSSEHub(log)

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
		recorder,
		log,
	)

	accessSvc := access.NewService(
		callStoreAdapter{store: sessions},
		presence,
		commandPublisherAdapter{commander: commander},
		cmdCtxStore,
		recorder,
		log,
	)

	// --- Хендлеры ---
	qrHandler := qr.NewHandler(qrKeyring, qrPropertyAdapter{svc: propertySvc}, presence)
	callsHandler := calls.NewHandler(callSvc)
	accessHandler := access.NewHandler(accessSvc)
	devicesHandler := devices.NewHandler(deviceRepo, presence)
	auditHandler := audit.NewHandler(recorder)

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
	r.Route("/api/v1", func(r chi.Router) {
		r.With(publicRL).Post("/qr/validate", qrHandler.Validate)
		r.With(publicRL).Post("/calls/initiate", callsHandler.Initiate)
		r.Post("/calls/{id}/accept", callsHandler.Accept)
		r.Post("/calls/{id}/cancel", callsHandler.Cancel)
		r.Post("/calls/{id}/end", callsHandler.End)
		r.Get("/resident/events", sseHub.Handler())
		r.With(publicRL).Post("/access/open", accessHandler.Open)
		r.Get("/devices", devicesHandler.List)
		r.Get("/audit/events", auditHandler.List)
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

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Info("shutdown_started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error("http_shutdown_failed", "error", err)
	}
	log.Info("shutdown_complete")
}

// --- Адаптеры границ модулей (architecture.md §1) ---

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
