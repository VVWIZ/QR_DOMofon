package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"log/slog"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// otpDigits — алфавит для генерации 6-значного кода.
const otpDigits = "0123456789"

// generateOtpCode генерирует n-значный числовой код на crypto/rand.
func generateOtpCode(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = otpDigits[int(buf[i])%len(otpDigits)]
	}
	return string(buf), nil
}

// Политика OTP (auth.md §4, ТЗ §12.4).
const (
	OtpTTL           = 5 * time.Minute  // время жизни выданного кода
	OtpRequestWindow = 10 * time.Minute // окно счётчика запросов
	OtpMaxRequests   = 3                // не более 3 запросов на телефон / окно
	OtpMaxAttempts   = 5                // на 5-й неверной попытке — блокировка
	OtpBlockTTL      = 30 * time.Minute // длительность блокировки
	OtpCodeLen       = 6                // длина кода (цифры)
)

// OtpRecord — активная запись OTP для телефона (Redis: otp:{phone}).
type OtpRecord struct {
	Code      string
	Attempts  int
	ExpiresAt time.Time
}

// OtpStore — состояние OTP-флоу (боевая реализация — go-redis; в тестах —
// map-фейк). Стор отвечает лишь за хранение значений с истечением относительно
// переданного now; вся политика лимитов/блокировок — в OtpService (чистая
// логика, тестируемая без Redis).
type OtpStore interface {
	// IncrRequests атомарно инкрементит счётчик запросов OTP для phone в окне
	// window (первый инкремент задаёт истечение now+window) и возвращает новое
	// значение счётчика.
	IncrRequests(ctx context.Context, phone string, window time.Duration, now time.Time) (int, error)

	// GetOTP возвращает активную (не истёкшую на now) запись OTP для phone.
	GetOTP(ctx context.Context, phone string, now time.Time) (rec OtpRecord, ok bool, err error)
	// SetOTP сохраняет запись OTP (перетирая прежнюю); истечение — rec.ExpiresAt.
	SetOTP(ctx context.Context, phone string, rec OtpRecord) error
	// DelOTP удаляет запись OTP (потребление после успешной верификации).
	DelOTP(ctx context.Context, phone string) error

	// IsBlocked сообщает, заблокирован ли phone на момент now.
	IsBlocked(ctx context.Context, phone string, now time.Time) (bool, error)
	// Block ставит блокировку phone до момента until.
	Block(ctx context.Context, phone string, until time.Time) error
}

// OtpSender доставляет код на телефон. Боевая реализация — SMS-провайдер
// (следующий инкремент); dev — DevSender.
type OtpSender interface {
	// Send отправляет code на phone и возвращает devCode: в dev это тот же код
	// (для поля dev_code ответа), в prod — "" (код уходит по SMS).
	Send(ctx context.Context, phone, code string) (devCode string, err error)
}

// DevSender логирует код и возвращает его как devCode (auth.md §4, "НЕ ДЛЯ ПРОД").
type DevSender struct {
	log *slog.Logger
}

// NewDevSender конструирует dev-отправитель.
func NewDevSender(log *slog.Logger) *DevSender {
	return &DevSender{log: log}
}

// Send логирует код и возвращает его же как devCode.
func (d *DevSender) Send(ctx context.Context, phone, code string) (string, error) {
	if d.log != nil {
		d.log.InfoContext(ctx, "otp dev code issued", "phone", phone, "code", code)
	}
	return code, nil
}

// SendResult — результат OtpService.Send (dev-код присутствует только в dev).
type SendResult struct {
	Sent    bool
	DevCode string
}

// OtpService реализует политику OTP-логина жильца/владельца поверх OtpStore и
// OtpSender. now инъектируется в методы → тесты детерминированы без sleep.
type OtpService struct {
	store  OtpStore
	sender OtpSender
	log    *slog.Logger
}

// NewOtpService собирает сервис.
func NewOtpService(store OtpStore, sender OtpSender, log *slog.Logger) *OtpService {
	return &OtpService{store: store, sender: sender, log: log}
}

// Send выдаёт OTP-код на phone:
//   - phone заблокирован → RATE_LIMIT;
//   - превышен лимит OtpMaxRequests за окно OtpRequestWindow → RATE_LIMIT;
//   - иначе: генерирует код, сохраняет запись (attempts=0, TTL OtpTTL),
//     отправляет через sender и возвращает SendResult{Sent:true, DevCode}.
//
// Ответ не раскрывает существование номера (успех при отсутствии троттла).
func (s *OtpService) Send(ctx context.Context, phone string, now time.Time) (SendResult, *httpx.Error) {
	blocked, err := s.store.IsBlocked(ctx, phone, now)
	if err != nil {
		return SendResult{}, httpx.NewError(httpx.CodeInternal, "otp store error")
	}
	if blocked {
		return SendResult{}, httpx.NewError(httpx.CodeRateLimit, "too many requests")
	}

	count, err := s.store.IncrRequests(ctx, phone, OtpRequestWindow, now)
	if err != nil {
		return SendResult{}, httpx.NewError(httpx.CodeInternal, "otp store error")
	}
	if count > OtpMaxRequests {
		return SendResult{}, httpx.NewError(httpx.CodeRateLimit, "too many requests")
	}

	code, err := generateOtpCode(OtpCodeLen)
	if err != nil {
		return SendResult{}, httpx.NewError(httpx.CodeInternal, "otp generation failed")
	}
	// Новая запись обнуляет счётчик попыток (attempts=0) — свежий Send сбрасывает
	// прогресс к блокировке.
	rec := OtpRecord{Code: code, Attempts: 0, ExpiresAt: now.Add(OtpTTL)}
	if err := s.store.SetOTP(ctx, phone, rec); err != nil {
		return SendResult{}, httpx.NewError(httpx.CodeInternal, "otp store error")
	}

	devCode, err := s.sender.Send(ctx, phone, code)
	if err != nil {
		return SendResult{}, httpx.NewError(httpx.CodeInternal, "otp send failed")
	}
	return SendResult{Sent: true, DevCode: devCode}, nil
}

// Verify сверяет code с активной записью OTP на момент now:
//   - phone заблокирован → RATE_LIMIT;
//   - нет активной записи / неверный код → UNAUTHORIZED; attempts++,
//     на OtpMaxAttempts-й неверной попытке телефон блокируется на OtpBlockTTL;
//   - верный код в пределах TTL → nil; запись потребляется (DelOTP), счётчик
//     попыток сбрасывается.
func (s *OtpService) Verify(ctx context.Context, phone, code string, now time.Time) *httpx.Error {
	blocked, err := s.store.IsBlocked(ctx, phone, now)
	if err != nil {
		return httpx.NewError(httpx.CodeInternal, "otp store error")
	}
	if blocked {
		return httpx.NewError(httpx.CodeRateLimit, "too many requests")
	}

	rec, ok, err := s.store.GetOTP(ctx, phone, now)
	if err != nil {
		return httpx.NewError(httpx.CodeInternal, "otp store error")
	}

	// Нет активной записи или код не совпал — считаем неверной попыткой.
	// Сравнение кода — constant-time (защита от timing-атаки).
	match := ok && subtle.ConstantTimeCompare([]byte(rec.Code), []byte(code)) == 1
	if !match {
		if ok {
			rec.Attempts++
			if rec.Attempts >= OtpMaxAttempts {
				// На OtpMaxAttempts-й неверной попытке — блокировка телефона.
				_ = s.store.Block(ctx, phone, now.Add(OtpBlockTTL))
			} else {
				_ = s.store.SetOTP(ctx, phone, rec)
			}
		}
		return httpx.NewError(httpx.CodeUnauthorized, "invalid or expired code")
	}

	// Верный код в пределах TTL — потребляем запись.
	if err := s.store.DelOTP(ctx, phone); err != nil {
		return httpx.NewError(httpx.CodeInternal, "otp store error")
	}
	return nil
}
