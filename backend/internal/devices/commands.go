package devices

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// commandContextTTL — сколько живёт контекст команды для корреляции ack.
// Совпадает по порядку с TTL call-сессии (120с), чтобы ack успел прийти.
const commandContextTTL = 120 * time.Second

// MQTTPublisher — минимальный интерфейс публикации в MQTT (реализуется
// platform/mqtt.Client). Объявлен на стороне devices (потребитель транспорта).
type MQTTPublisher interface {
	Publish(topic string, payload []byte) error
}

// OpenRelayPayload — payload команды open_relay (firmware/docs/PROTOCOL.md §3.1).
type OpenRelayPayload struct {
	Cmd        string `json:"cmd"`
	RelayID    int    `json:"relay_id"`
	DurationMs int    `json:"duration_ms"`
	RequestID  string `json:"request_id"`
	IssuedBy   string `json:"issued_by"`
	IssuedAt   string `json:"issued_at"`
}

// commandTopic формирует топик команд устройства.
func commandTopic(deviceID string) string { return "devices/" + deviceID + "/commands" }

// Commander публикует команды устройству через MQTT (QoS1). Адаптер в
// cmd/server связывает его с потребительским интерфейсом access.CommandPublisher.
type Commander struct {
	pub MQTTPublisher
}

// NewCommander создаёт публикатор команд.
func NewCommander(pub MQTTPublisher) *Commander {
	return &Commander{pub: pub}
}

// PublishOpenRelay публикует open_relay в devices/{deviceID}/commands. Поле cmd
// проставляется здесь (устройство различает команды по нему).
func (c *Commander) PublishOpenRelay(_ context.Context, deviceID string, p OpenRelayPayload) error {
	p.Cmd = "open_relay"
	body, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("devices: marshal open_relay: %w", err)
	}
	return c.pub.Publish(commandTopic(deviceID), body)
}

// commandContextKey формирует Redis-ключ контекста команды по request_id.
func commandContextKey(requestID string) string { return "cmd:" + requestID }

// CommandContextStore хранит контекст выданной команды (call_id, apartment_id и
// т.п.) по request_id в Redis, чтобы статусный потребитель обогатил command_ack
// в аудите корреляцией с звонком. Пишет access (через свой интерфейс Save с
// map[string]string), читает status_consumer.
type CommandContextStore struct {
	rdb *redis.Client
}

// NewCommandContextStore создаёт хранилище контекста команд.
func NewCommandContextStore(rdb *redis.Client) *CommandContextStore {
	return &CommandContextStore{rdb: rdb}
}

// Save сохраняет контекст команды (TTL commandContextTTL). meta — плоская карта
// строк (call_id/apartment_id/access_point_id/device_id/management_company_id),
// чтобы не тащить общий доменный тип между access и devices.
func (s *CommandContextStore) Save(ctx context.Context, requestID string, meta map[string]string) error {
	body, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("devices: marshal cmd context: %w", err)
	}
	return s.rdb.Set(ctx, commandContextKey(requestID), body, commandContextTTL).Err()
}

// Get возвращает контекст команды по request_id (ok=false, если отсутствует).
func (s *CommandContextStore) Get(ctx context.Context, requestID string) (map[string]string, bool, error) {
	body, err := s.rdb.Get(ctx, commandContextKey(requestID)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var meta map[string]string
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, false, fmt.Errorf("devices: unmarshal cmd context: %w", err)
	}
	return meta, true, nil
}
