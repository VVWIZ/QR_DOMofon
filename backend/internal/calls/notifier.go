package calls

import (
	"context"
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

// SSEHub — per-apartment SSE-хаб (реализация Notifier + HTTP-хендлер
// /resident/events). События рассылаются только подписчикам конкретной квартиры
// (auth.md §5, RBAC): подписчик получает потоки квартир, к которым привязан его
// токен (резолвер извлекает apartment_id из claims контекста).
type SSEHub struct {
	log      *slog.Logger
	resolver ApartmentResolver
	mu       sync.Mutex
	subs     map[string]map[chan sseEvent]struct{}
}

// ApartmentResolver возвращает квартиры текущего запроса (из claims контекста).
// Инъектируется адаптером cmd/server (auth.ClaimsFromContext → apartment_id ролей).
type ApartmentResolver func(ctx context.Context) []string

// NewSSEHub создаёт пустой хаб с резолвером квартир подписчика.
func NewSSEHub(log *slog.Logger, resolver ApartmentResolver) *SSEHub {
	return &SSEHub{
		log:      log,
		resolver: resolver,
		subs:     make(map[string]map[chan sseEvent]struct{}),
	}
}

// subscribe регистрирует новый канал для всех квартир apartments.
func (h *SSEHub) subscribe(apartments []string) chan sseEvent {
	ch := make(chan sseEvent, 16)
	h.mu.Lock()
	for _, apt := range apartments {
		if h.subs[apt] == nil {
			h.subs[apt] = make(map[chan sseEvent]struct{})
		}
		h.subs[apt][ch] = struct{}{}
	}
	h.mu.Unlock()
	return ch
}

// unsubscribe снимает канал со всех его квартир и закрывает его.
func (h *SSEHub) unsubscribe(ch chan sseEvent, apartments []string) {
	h.mu.Lock()
	for _, apt := range apartments {
		if m, ok := h.subs[apt]; ok {
			delete(m, ch)
			if len(m) == 0 {
				delete(h.subs, apt)
			}
		}
	}
	close(ch)
	h.mu.Unlock()
}

// emit сериализует payload и неблокирующе рассылает подписчикам квартиры
// apartmentID (медленный клиент пропускает событие, а не блокирует хаб).
func (h *SSEHub) emit(apartmentID, name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		h.log.Error("sse_marshal_failed", "event", name, "error", err)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[apartmentID] {
		select {
		case ch <- sseEvent{name: name, data: data}:
		default:
			h.log.Warn("sse_client_slow_dropped", "event", name, "apartment_id", apartmentID)
		}
	}
}

// CallIncoming рассылает call.incoming подписчикам квартиры.
func (h *SSEHub) CallIncoming(apartmentID string, p CallIncomingPayload) {
	h.emit(apartmentID, "call.incoming", p)
}

// CallCancelled рассылает call.cancelled подписчикам квартиры.
func (h *SSEHub) CallCancelled(apartmentID, callID string) {
	h.emit(apartmentID, "call.cancelled", map[string]string{"call_id": callID})
}

// CallAccepted рассылает call.accepted подписчикам квартиры.
func (h *SSEHub) CallAccepted(apartmentID, callID string) {
	h.emit(apartmentID, "call.accepted", map[string]string{"call_id": callID})
}

// Handler — GET /api/v1/resident/events (text/event-stream, ping каждые 15с).
// Подписывает клиента только на квартиры из его claims (резолвер). Ставится
// после Authenticator+RequireResident (claims в контексте).
func (h *SSEHub) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			httpx.WriteError(w, httpx.CodeInternal, "Streaming unsupported", httpx.RequestIDFromContext(r.Context()))
			return
		}

		apartments := h.resolver(r.Context())

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		ch := h.subscribe(apartments)
		defer h.unsubscribe(ch, apartments)

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
