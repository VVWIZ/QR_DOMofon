// Асинхронные события устройства (event) и offline-буфер, PROTOCOL.md §4.3.
//
// fail_open_activated по определению возникает БЕЗ связи с брокером, поэтому
// событие сохраняется в локальный in-memory-буфер с исходным timestamp и
// публикуется сразу после реконнекта — иначе fail-open не попадёт в аудит
// (AC10/AC14). Порядок после реконнекта (§4.3): буферизованные event (в порядке
// возникновения) → текущий heartbeat → обычный режим.
package main

import (
	"sync"
	"time"
)

// Имена событий устройства (§4.3).
const (
	eventFailOpenActivated   = "fail_open_activated"
	eventFailOpenDeactivated = "fail_open_deactivated"
)

// eventMessage — payload event в devices/{device_id}/status.
type eventMessage struct {
	Type      string `json:"type"`  // всегда "event"
	Event     string `json:"event"` // fail_open_activated | fail_open_deactivated
	Timestamp string `json:"timestamp"`
}

// offlineEventBuffer — потокобезопасный FIFO-буфер событий, накопленных пока нет
// связи с брокером.
type offlineEventBuffer struct {
	mu     sync.Mutex
	events []eventMessage
}

// newOfflineEventBuffer создаёт пустой буфер.
func newOfflineEventBuffer() *offlineEventBuffer {
	return &offlineEventBuffer{}
}

// add добавляет событие в хвост (сохраняя порядок возникновения).
func (b *offlineEventBuffer) add(e eventMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, e)
}

// drain возвращает все накопленные события в порядке возникновения и очищает
// буфер.
func (b *offlineEventBuffer) drain() []eventMessage {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.events
	b.events = nil
	return out
}

// emitEvent публикует событие с исходным timestamp, если есть связь; иначе
// откладывает его в offline-буфер до реконнекта (§4.3).
func (d *Device) emitEvent(event string, now time.Time) {
	msg := eventMessage{Type: "event", Event: event, Timestamp: iso8601(now)}

	d.mu.Lock()
	online := d.online
	d.mu.Unlock()

	if online {
		d.publishJSON(d.statusTopic, msg)
		return
	}
	d.offline.add(msg)
	d.log.Info("event_buffered_offline", "event", event, "timestamp", msg.Timestamp)
}

// flushOfflineEvents публикует все накопленные offline-события (с исходными
// timestamp) в порядке возникновения. Вызывается из handleConnect до
// немедленного heartbeat.
func (d *Device) flushOfflineEvents() {
	for _, e := range d.offline.drain() {
		d.publishJSON(d.statusTopic, e)
		d.log.Info("offline_event_published", "event", e.Event, "timestamp", e.Timestamp)
	}
}
