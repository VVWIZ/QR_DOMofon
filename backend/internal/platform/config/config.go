// Package config читает конфигурацию сервиса из окружения (godotenv для .env).
// Значения по умолчанию — dev/walking skeleton с каноническими фикстурами
// (architecture.md §5). Прод-секреты сюда не хардкодятся.
package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// Config — конфигурация cmd/server. Инфраструктурные адреса + фиксированные
// UUID фикстуры (единственная захардкоженная квартира/устройство skeleton).
type Config struct {
	// Инфраструктура.
	DatabaseURL      string
	RedisAddr        string
	MQTTBrokerURL    string
	MQTTClientID     string
	LiveKitURL       string
	LiveKitAPIKey    string
	LiveKitAPISecret string
	HTTPAddr         string
	LogLevel         string

	// Канонические фикстуры (architecture.md §5).
	DeviceID            string
	DeviceSerial        string
	ApartmentID         string
	AccessPointPublicID string

	// QR-секрет из окружения (prod-гэп §17.4): если QRSecret задан, он
	// переопределяет секрет для QRKid и не хранится в БД plaintext. Пусто —
	// секрет берётся из таблицы qr_keys (dev-сид). В проде — из KMS/Vault.
	QRSecret string
	QRKid    string

	// Auth (auth.md §3). Ключи RS256 в PEM. Пусто + AuthDevMode → фиксированный
	// dev-keypair из auth.keys (токены переживают рестарт). Пусто + !AuthDevMode
	// → fail-closed (сервер не стартует). godotenv раскрывает \n в PEM-значениях.
	JWTPrivateKey string
	JWTPublicKey  string
	AuthDevMode   bool
}

// Load загружает .env (если присутствует; отсутствие файла не ошибка) и
// собирает Config из переменных окружения с dev-дефолтами.
func Load() Config {
	// .env опционален: в проде переменные приходят из окружения оркестратора.
	_ = godotenv.Load()

	return Config{
		DatabaseURL:      env("DATABASE_URL", "postgres://domofon:domofon@localhost:5432/domofon?sslmode=disable"),
		RedisAddr:        env("REDIS_ADDR", "localhost:6379"),
		MQTTBrokerURL:    env("MQTT_BROKER_URL", "tcp://localhost:1883"),
		MQTTClientID:     env("MQTT_CLIENT_ID", "domofon-backend"),
		LiveKitURL:       env("LIVEKIT_URL", "ws://localhost:7880"),
		LiveKitAPIKey:    env("LIVEKIT_API_KEY", "devkey"),
		LiveKitAPISecret: env("LIVEKIT_API_SECRET", "devsecret_change_me_at_least_32_chars"),
		HTTPAddr:         env("HTTP_ADDR", ":8080"),
		LogLevel:         env("LOG_LEVEL", "info"),

		DeviceID:            env("DEVICE_ID", "66666666-6666-6666-6666-666666666666"),
		DeviceSerial:        env("DEVICE_SERIAL", "EMU-001"),
		ApartmentID:         env("APARTMENT_ID", "33333333-3333-3333-3333-333333333333"),
		AccessPointPublicID: env("ACCESS_POINT_PUBLIC_ID", "55555555-5555-5555-5555-555555555555"),

		QRSecret: env("QR_SECRET", ""),
		QRKid:    env("QR_KID", "dev1"),

		JWTPrivateKey: env("JWT_PRIVATE_KEY", ""),
		JWTPublicKey:  env("JWT_PUBLIC_KEY", ""),
		// Dev по умолчанию (walking skeleton). В проде выставить AUTH_DEV_MODE=false
		// и подать реальные ключи, иначе сервер не стартует (fail-closed).
		AuthDevMode: envBool("AUTH_DEV_MODE", true),
	}
}

// env возвращает значение переменной окружения key или fallback, если пусто.
func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// envBool парсит булеву переменную окружения (true/1/yes → true; false/0/no →
// false); отсутствие/пустое/нераспознанное значение → fallback.
func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}
