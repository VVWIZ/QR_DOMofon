// Конфигурация эмулятора: значения по умолчанию (канонические фикстуры
// architecture.md §5 и пороги PROTOCOL.md §6) с возможностью override через
// env-переменные и флаги командной строки. Флаг имеет приоритет над env, env —
// над умолчанием.
package main

import (
	"flag"
	"log/slog"
	"os"
	"time"
)

// Канонические умолчания (architecture.md §5, PROTOCOL.md §1/§4.1).
const (
	defaultDeviceID        = "66666666-6666-6666-6666-666666666666"
	defaultDeviceSerial    = "EMU-001"
	defaultBrokerURL       = "tcp://localhost:1883"
	defaultFirmwareVersion = "emu-0.1.0"
)

// Config — параметры запуска эмулятора.
//
// StaleThreshold / FailOpenAfter / RecoverStable описывают пороги устройства
// (PROTOCOL.md §6), но их применяет детерминированное ядро (commands.go,
// relay.go) через compile-time-константы. Поля здесь документируют и валидируют
// конфигурацию (см. validate): при расхождении с ядром выводится предупреждение,
// т.к. параметризация ядра — вне скоупа этого этапа. HeartbeatInterval и
// Keepalive реально потребляются слоем интеграции.
type Config struct {
	DeviceID        string
	DeviceSerial    string
	BrokerURL       string
	FirmwareVersion string

	StaleThreshold    time.Duration // §5.2 (ядро: staleThreshold)
	FailOpenAfter     time.Duration // §5.3 (ядро: failOpenThreshold)
	RecoverStable     time.Duration // §5.4 (ядро: recoveryHysteresis)
	HeartbeatInterval time.Duration // §4.1 — период heartbeat
	Keepalive         time.Duration // §1 — MQTT keepalive
}

// LoadConfig собирает конфиг из умолчаний, env-переменных и флагов (флаг >
// env > умолчание). Вызывается один раз из main.
func LoadConfig() Config {
	var cfg Config

	flag.StringVar(&cfg.DeviceID, "device-id", envStr("DEVICE_ID", defaultDeviceID),
		"внутренний UUID устройства (топики devices/{id}/...)")
	flag.StringVar(&cfg.DeviceSerial, "device-serial", envStr("DEVICE_SERIAL", defaultDeviceSerial),
		"серийный номер (MQTT ClientID = device:{serial})")
	flag.StringVar(&cfg.BrokerURL, "broker-url", envStr("MQTT_BROKER_URL", defaultBrokerURL),
		"URL MQTT-брокера")
	flag.StringVar(&cfg.FirmwareVersion, "firmware-version", envStr("FIRMWARE_VERSION", defaultFirmwareVersion),
		"версия прошивки (поле heartbeat.firmware_version)")

	flag.DurationVar(&cfg.StaleThreshold, "stale-threshold", envDur("STALE_THRESHOLD", 30*time.Second),
		"порог свежести команды (§5.2)")
	flag.DurationVar(&cfg.FailOpenAfter, "fail-open-after", envDur("FAIL_OPEN_AFTER", 90*time.Second),
		"порог fail-open по потере связи (§5.3)")
	flag.DurationVar(&cfg.RecoverStable, "recover-stable", envDur("RECOVER_STABLE", 30*time.Second),
		"гистерезис восстановления (§5.4)")
	flag.DurationVar(&cfg.HeartbeatInterval, "heartbeat-interval", envDur("HEARTBEAT_INTERVAL", 30*time.Second),
		"период heartbeat (§4.1)")
	flag.DurationVar(&cfg.Keepalive, "keepalive", envDur("KEEPALIVE", 30*time.Second),
		"MQTT keepalive (§1)")

	flag.Parse()
	return cfg
}

// validate предупреждает, если пороги ядра переопределены в конфиге: ядро
// использует compile-time-константы, поэтому override этих трёх порогов не
// вступает в силу (в отличие от heartbeat/keepalive, которые применяются).
func (c Config) validate(log *slog.Logger) {
	if c.StaleThreshold != staleThreshold {
		log.Warn("config_override_ignored", "param", "stale_threshold",
			"config", c.StaleThreshold, "core", staleThreshold)
	}
	if c.FailOpenAfter != failOpenThreshold {
		log.Warn("config_override_ignored", "param", "fail_open_after",
			"config", c.FailOpenAfter, "core", failOpenThreshold)
	}
	if c.RecoverStable != recoveryHysteresis {
		log.Warn("config_override_ignored", "param", "recover_stable",
			"config", c.RecoverStable, "core", recoveryHysteresis)
	}
}

// envStr возвращает значение env-переменной или fallback, если она пуста.
func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envDur парсит env-переменную как time.Duration (например, "45s") или
// возвращает fallback, если она пуста или не парсится.
func envDur(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
