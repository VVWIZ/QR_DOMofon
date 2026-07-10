// Свежесть команды (anti-stale), PROTOCOL.md §5.2, и разбор команды open_relay
// (§3.1, §5.5). Реализовано под контракт, покрытый commands_test.go (IsStale не
// изменяется).
package main

import (
	"encoding/json"
	"time"
)

// staleThreshold — базовый порог свежести команды (§5.2, §6).
const staleThreshold = 30 * time.Second

// clockSkewTolerance — допуск на рассинхрон часов устройства и сервера (±).
// Фактический порог отбрасывания лежит в интервале
// [staleThreshold-clockSkewTolerance, staleThreshold+clockSkewTolerance] = 25–35с.
const clockSkewTolerance = 5 * time.Second

// IsStale сообщает, устарела ли команда с временем выдачи issuedAt на момент now.
//
// Команда устаревает, когда |now − issuedAt| превышает порог 30с (с допуском ±5с
// на рассинхрон часов) — симметрично для прошлого (буферизованная брокером
// команда persistent session, §5.2) и для будущего (кривые часы устройства).
// Вызывающий не должен полагаться на точную границу внутри интервала 25–35с.
func IsStale(issuedAt, now time.Time) bool {
	diff := now.Sub(issuedAt)
	if diff < 0 {
		diff = -diff
	}
	return diff > staleThreshold
}

// openRelayCommand — payload команды devices/{device_id}/commands (§3.1).
type openRelayCommand struct {
	Cmd        string `json:"cmd"`
	RelayID    int    `json:"relay_id"`
	DurationMs int    `json:"duration_ms"`
	RequestID  string `json:"request_id"`
	IssuedBy   string `json:"issued_by"`
	IssuedAt   string `json:"issued_at"` // ISO 8601 UTC
}

// commandAck — ответ на команду (§4.2). Reason/Duplicate — опциональные:
// omitempty гарантирует их отсутствие при штатном первом исполнении.
type commandAck struct {
	Type      string `json:"type"` // всегда "command_ack"
	RequestID string `json:"request_id"`
	Result    string `json:"result"` // ok | rejected
	Reason    string `json:"reason,omitempty"`
	Duplicate bool   `json:"duplicate,omitempty"`
	Timestamp string `json:"timestamp"`
}

// handleCommand разбирает и исполняет входящую команду (§5.5):
//
//  1. невалидный JSON или неизвестный cmd → warning в лог, ack НЕ публикуется (§3.1);
//  2. stale (§5.2) → command_ack{rejected, stale}, лог stale_command_rejected,
//     реле не трогаем;
//  3. дубликат request_id (§5.1) → command_ack{ok, duplicate}, реле не трогаем;
//  4. иначе → OpenRelay, немедленный heartbeat relay_state=open, command_ack{ok}.
//     Авто-закрытие по истечении duration наблюдает pollState (heartbeat closed).
func (d *Device) handleCommand(payload []byte) {
	now := time.Now().UTC()

	var cmd openRelayCommand
	if err := json.Unmarshal(payload, &cmd); err != nil {
		d.log.Warn("command_parse_failed", "error", err, "payload", string(payload))
		return
	}
	if cmd.Cmd != "open_relay" {
		// §3.1: неизвестный cmd игнорируется, ack не публикуется.
		d.log.Warn("unknown_command", "cmd", cmd.Cmd, "request_id", cmd.RequestID)
		return
	}
	issuedAt, err := time.Parse(time.RFC3339, cmd.IssuedAt)
	if err != nil {
		// issued_at обязателен и является основой проверки свежести (§5.2);
		// без него команда считается некорректной.
		d.log.Warn("command_bad_issued_at", "issued_at", cmd.IssuedAt, "error", err, "request_id", cmd.RequestID)
		return
	}

	// §5.2: свежесть проверяется до идемпотентности и до актуации.
	if IsStale(issuedAt, now) {
		d.publishAck(commandAck{
			Type:      "command_ack",
			RequestID: cmd.RequestID,
			Result:    "rejected",
			Reason:    "stale",
			Timestamp: iso8601(now),
		})
		d.log.Info("stale_command_rejected", "request_id", cmd.RequestID, "issued_at", cmd.IssuedAt)
		return
	}

	// Идемпотентность и актуация — под единым mutex: FSM/буфер синхронизацию не
	// обеспечивают, её даёт вызывающий.
	d.mu.Lock()
	if d.idemp.Seen(cmd.RequestID, now) {
		d.mu.Unlock()
		// §5.1: дубликат — актуации нет, ack повторяется с исходным result=ok.
		d.publishAck(commandAck{
			Type:      "command_ack",
			RequestID: cmd.RequestID,
			Result:    "ok",
			Duplicate: true,
			Timestamp: iso8601(now),
		})
		d.log.Info("duplicate_command", "request_id", cmd.RequestID)
		return
	}
	d.fsm.OpenRelay(now, time.Duration(cmd.DurationMs)*time.Millisecond)
	newState := d.fsm.State(now)
	changed := newState != d.relayState
	d.relayState = newState
	d.mu.Unlock()

	// §5.5.1: немедленный heartbeat relay_state=open — до ack. Если реле уже было
	// open (перезапуск таймера или активный fail-open, §5.5.4/§5.5.5) — смены
	// состояния нет, внеочередной heartbeat не нужен.
	if changed {
		d.publishHeartbeat(now)
	}
	d.publishAck(commandAck{
		Type:      "command_ack",
		RequestID: cmd.RequestID,
		Result:    "ok",
		Timestamp: iso8601(now),
	})
	d.log.Info("command_executed", "request_id", cmd.RequestID, "duration_ms", cmd.DurationMs, "relay_state", string(newState))
}

// publishAck публикует command_ack в топик статуса.
func (d *Device) publishAck(ack commandAck) {
	d.publishJSON(d.statusTopic, ack)
}
