package devices

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"domofon/backend/internal/audit"
)

// statusMessage — общая обёртка входящего status-сообщения. Backend различает
// типы по полю type; его отсутствие = heartbeat (PROTOCOL.md §4).
type statusMessage struct {
	Type string `json:"type"`
}

// heartbeatMessage — PROTOCOL.md §4.1.
type heartbeatMessage struct {
	DeviceID        string `json:"device_id"`
	Status          string `json:"status"`
	RelayState      string `json:"relay_state"`
	FirmwareVersion string `json:"firmware_version"`
	Timestamp       string `json:"timestamp"`
}

// ackMessage — PROTOCOL.md §4.2.
type ackMessage struct {
	RequestID string `json:"request_id"`
	Result    string `json:"result"`
	Reason    string `json:"reason"`
	Duplicate bool   `json:"duplicate"`
	Timestamp string `json:"timestamp"`
}

// eventMessage — PROTOCOL.md §4.3.
type eventMessage struct {
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
}

// StatusConsumer разбирает devices/+/status: heartbeat → presence + last_seen;
// command_ack → аудит (обогащённый контекстом команды); event → аудит fail_open.
type StatusConsumer struct {
	presence *Presence
	repo     *Repo
	audit    audit.Recorder
	cmdCtx   *CommandContextStore
	log      *slog.Logger
}

// NewStatusConsumer собирает потребителя статусов.
func NewStatusConsumer(presence *Presence, repo *Repo, recorder audit.Recorder, cmdCtx *CommandContextStore, log *slog.Logger) *StatusConsumer {
	return &StatusConsumer{presence: presence, repo: repo, audit: recorder, cmdCtx: cmdCtx, log: log}
}

// Handle — обработчик MQTT-сообщения (передаётся в mqtt.Subscribe). Синхронный;
// собственный контекст с таймаутом, т.к. paho вызывает без контекста запроса.
func (c *StatusConsumer) Handle(topic string, payload []byte) {
	deviceID := deviceIDFromTopic(topic)
	if deviceID == "" {
		c.log.Warn("status_unknown_topic", "topic", topic)
		return
	}

	var msg statusMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		c.log.Warn("status_unmarshal_failed", "topic", topic, "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch msg.Type {
	case "", "heartbeat":
		c.handleHeartbeat(ctx, deviceID, payload)
	case "command_ack":
		c.handleAck(ctx, deviceID, payload)
	case "event":
		c.handleEvent(ctx, deviceID, payload)
	default:
		c.log.Warn("status_unknown_type", "topic", topic, "type", msg.Type)
	}
}

// handleHeartbeat обновляет presence (Redis TTL 90с) и last_seen_at (Postgres).
func (c *StatusConsumer) handleHeartbeat(ctx context.Context, deviceID string, payload []byte) {
	var hb heartbeatMessage
	_ = json.Unmarshal(payload, &hb) // поля опциональны для presence

	if err := c.presence.Mark(ctx, deviceID); err != nil {
		c.log.Error("presence_mark_failed", "device_id", deviceID, "error", err)
	}
	if err := c.repo.UpdateLastSeen(ctx, deviceID, time.Now().UTC()); err != nil {
		c.log.Error("last_seen_update_failed", "device_id", deviceID, "error", err)
	}
}

// handleAck пишет в аудит command_ack (или command_rejected при result=rejected),
// обогащая событие контекстом команды по request_id.
func (c *StatusConsumer) handleAck(ctx context.Context, deviceID string, payload []byte) {
	var ack ackMessage
	if err := json.Unmarshal(payload, &ack); err != nil {
		c.log.Warn("ack_unmarshal_failed", "device_id", deviceID, "error", err)
		return
	}

	eventType := "command_ack"
	if ack.Result == "rejected" {
		eventType = "command_rejected"
	}

	ev := audit.Event{
		EventType: eventType,
		Actor:     "device:" + deviceID,
		DeviceID:  deviceID,
		RequestID: ack.RequestID,
		Metadata: map[string]any{
			"result":    ack.Result,
			"duplicate": ack.Duplicate,
		},
	}
	if ack.Reason != "" {
		ev.Metadata["reason"] = ack.Reason
	}

	// Обогащение контекстом выданной команды (call_id/apartment_id/...).
	if meta, ok, err := c.cmdCtx.Get(ctx, ack.RequestID); err != nil {
		c.log.Warn("cmd_context_get_failed", "request_id", ack.RequestID, "error", err)
	} else if ok {
		ev.CallID = meta["call_id"]
		ev.ApartmentID = meta["apartment_id"]
		ev.AccessPointID = meta["access_point_id"]
		if meta["device_id"] != "" {
			ev.DeviceID = meta["device_id"]
		}
		ev.ManagementCompanyID = meta["management_company_id"]
	}

	if err := c.audit.Record(ctx, ev); err != nil {
		c.log.Error("audit_ack_failed", "request_id", ack.RequestID, "error", err)
	}
}

// handleEvent пишет в аудит асинхронное событие устройства (fail_open_*),
// обогащая его контекстом устройства из БД.
func (c *StatusConsumer) handleEvent(ctx context.Context, deviceID string, payload []byte) {
	var evt eventMessage
	if err := json.Unmarshal(payload, &evt); err != nil {
		c.log.Warn("event_unmarshal_failed", "device_id", deviceID, "error", err)
		return
	}
	if evt.Event == "" {
		c.log.Warn("event_missing_name", "device_id", deviceID)
		return
	}

	ev := audit.Event{
		EventType: evt.Event,
		Actor:     "device:" + deviceID,
		DeviceID:  deviceID,
		Metadata:  map[string]any{"timestamp": evt.Timestamp},
	}
	if dc, ok, err := c.repo.Context(ctx, deviceID); err != nil {
		c.log.Warn("device_context_failed", "device_id", deviceID, "error", err)
	} else if ok {
		ev.AccessPointID = dc.AccessPointID
		ev.ApartmentID = dc.ApartmentID
		ev.ManagementCompanyID = dc.ManagementCompanyID
	}

	if err := c.audit.Record(ctx, ev); err != nil {
		c.log.Error("audit_event_failed", "device_id", deviceID, "event", evt.Event, "error", err)
	}
}

// deviceIDFromTopic извлекает {device_id} из devices/{device_id}/status.
func deviceIDFromTopic(topic string) string {
	parts := strings.Split(topic, "/")
	if len(parts) >= 3 && parts[0] == "devices" && parts[2] == "status" {
		return parts[1]
	}
	return ""
}
