package calls

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"domofon/backend/internal/platform/httpx"
)

// pingInterval — период keep-alive комментария SSE (api.md GET /resident/events).
const pingInterval = 15 * time.Second

// CallIncomingPayload — данные события call.incoming (api.md).
type CallIncomingPayload struct {
	CallID           string `json:"call_id"`
	AccessPointLabel string `json:"access_point_label"`
	ApartmentID      string `json:"apartment_id"`
}

// Notifier — транспорт сигнала жильцу (интерфейс на стороне потребителя calls).
// SSEHub — одна из реализаций; замена на WebSocket/push доменную логику не
// трогает (architecture.md §4.3).
type Notifier interface {
	CallIncoming(apartmentID string, p CallIncomingPayload)
	CallCancelled(apartmentID, callID string)
	CallAccepted(apartmentID, callID string)
}

// sseEvent — одно событие для рассылки подписчикам.
type sseEvent struct {
	name string
	data []byte
}

// SSEHub — широковещательный SSE-хаб (реализация Notifier + HTTP-хендлер
// /resident/events). В skeleton одна захардкоженная квартира без auth, поэтому
// apartmentID не фильтруется — события уходят всем подписчикам.
type SSEHub struct {
	log  *slog.Logger
	mu   sync.Mutex
	subs map[chan sseEvent]struct{}
}

// NewSSEHub создаёт пустой хаб.
func NewSSEHub(log *slog.Logger) *SSEHub {
	return &SSEHub{log: log, subs: make(map[chan sseEvent]struct{})}
}

func (h *SSEHub) subscribe() chan sseEvent {
	ch := make(chan sseEvent, 16)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHub) unsubscribe(ch chan sseEvent) {
	h.mu.Lock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// emit сериализует payload и неблокирующе рассылает подписчикам (медленный
// клиент пропускает событие, а не блокирует хаб).
func (h *SSEHub) emit(name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("sse_marshal_failed", "event", name, "error", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- sseEvent{name: name, data: data}:
		default:
			h.log.Warn("sse_client_slow_dropped", "event", name)
		}
	}
}

// CallIncoming рассылает call.incoming.
func (h *SSEHub) CallIncoming(_ string, p CallIncomingPayload) {
	h.emit("call.incoming", p)
}

// CallCancelled рассылает call.cancelled.
func (h *SSEHub) CallCancelled(_ string, callID string) {
	h.emit("call.cancelled", map[string]string{"call_id": callID})
}

// CallAccepted рассылает call.accepted.
func (h *SSEHub) CallAccepted(_ string, callID string) {
	h.emit("call.accepted", map[string]string{"call_id": callID})
}

// Handler — GET /api/v1/resident/events (text/event-stream, ping каждые 15с).
func (h *SSEHub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.WriteError(w, httpx.CodeInternal, "Streaming unsupported", httpx.RequestIDFromContext(r.Context()))
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		ch := h.subscribe()
		defer h.unsubscribe(ch)

		// Начальный ping — открыть поток немедленно.
		fmt.Fprint(w, ": ping\n\n")
		flusher.Flush()

		ping := time.NewTicker(pingInterval)
		defer ping.Stop()

		ctx := r.Context()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			case ev, ok := <-ch:
				if !ok {
					return
				}
				fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.name, ev.data)
				flusher.Flush()
			}
		}
	}
}
