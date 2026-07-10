// Heartbeat, PROTOCOL.md §4.1. Публикуется каждые HeartbeatInterval и немедленно
// при любой смене relay_state (closed↔open) — именно по внеочередным heartbeat
// наблюдается цикл реле (AC6). Backend отличает heartbeat от command_ack/event
// по отсутствию поля type.
package main

import (
	"context"
	"time"
)

// heartbeat — payload devices/{device_id}/status без поля type.
type heartbeat struct {
	DeviceID        string `json:"device_id"`
	Status          string `json:"status"`
	RelayState      string `json:"relay_state"`
	FirmwareVersion string `json:"firmware_version"`
	UptimeSec       int    `json:"uptime_sec"`
	RSSI            int    `json:"rssi"`
	Timestamp       string `json:"timestamp"`
}

// publishHeartbeat публикует текущее состояние устройства. При отсутствии связи
// не публикует (сообщение было бы потеряно) — актуальный heartbeat уйдёт сразу
// после реконнекта из handleConnect.
func (d *Device) publishHeartbeat(now time.Time) {
	d.mu.Lock()
	online := d.online
	state := d.relayState
	d.mu.Unlock()

	if !online {
		return
	}

	d.publishJSON(d.statusTopic, heartbeat{
		DeviceID:        d.cfg.DeviceID,
		Status:          "online",
		RelayState:      string(state),
		FirmwareVersion: d.cfg.FirmwareVersion,
		UptimeSec:       int(now.Sub(d.startedAt).Seconds()),
		RSSI:            rssiDummy,
		Timestamp:       iso8601(now),
	})
}

// runHeartbeatLoop публикует периодический heartbeat каждые HeartbeatInterval.
// Внеочередные heartbeat при смене relay_state публикуют pollState и
// handleCommand.
func (d *Device) runHeartbeatLoop(ctx context.Context) {
	t := time.NewTicker(d.cfg.HeartbeatInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.publishHeartbeat(time.Now().UTC())
		}
	}
}
