// Эмулятор устройства-домофона (firmware/emulator) — детерминированное ядро.
//
// Единый контракт устройства: firmware/docs/PROTOCOL.md §5. Пакет собран как
// package main (плоские файлы в каталоге эмулятора, см. architecture.md); func
// main появится на этапе firmware вместе с реализацией. `go test` этого пакета
// работает и без func main.
//
// Реализовано под контракт, покрытый тестами; func main появится на этапе
// интеграции эмулятора (5b).
//
// Этот файл: идемпотентность (PROTOCOL.md §5.1) — буфер последних 20 request_id,
// TTL 60с.
package main

import (
	"sync"
	"time"
)

// idempotencyCapacity — сколько последних request_id хранит устройство (§5.1).
const idempotencyCapacity = 20

// idempotencyTTL — время жизни записи в буфере (§5.1).
const idempotencyTTL = 60 * time.Second

// IdempotencyBuffer — буфер обработанных request_id: ёмкость 20, TTL 60с.
// Вытеснение при переполнении — самая старая запись (по времени вставки); по
// TTL запись исчезает через 60с. Синхронизацию обеспечивает вызывающий (единый
// цикл обработки команд), сам буфер потокобезопасным быть не обязан.
type IdempotencyBuffer struct {
	mu      sync.Mutex
	entries []idempotencyEntry
}

// idempotencyEntry — одна запись буфера: request_id и момент его добавления
// (для проверки TTL). Порядок в срезе = порядок вставки (FIFO для вытеснения).
type idempotencyEntry struct {
	id    string
	added time.Time
}

// NewIdempotencyBuffer создаёт пустой буфер.
func NewIdempotencyBuffer() *IdempotencyBuffer {
	return &IdempotencyBuffer{
		entries: make([]idempotencyEntry, 0, idempotencyCapacity),
	}
}

// Seen проверяет и одновременно фиксирует request_id на момент now:
//
//   - request_id ещё не в буфере (либо истёк по TTL, либо был вытеснен) →
//     запись добавляется, возвращается false («не видели»);
//   - request_id уже в буфере и не истёк → возвращается true («дубликат»),
//     содержимое буфера не меняется.
func (b *IdempotencyBuffer) Seen(requestID string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// 1. Вычистить протухшие записи (возраст больше TTL). Фильтрация на месте
	//    сохраняет порядок вставки.
	fresh := b.entries[:0]
	for _, e := range b.entries {
		if now.Sub(e.added) <= idempotencyTTL {
			fresh = append(fresh, e)
		}
	}
	b.entries = fresh

	// 2. Если request_id ещё в буфере (свежий) — это дубликат.
	for _, e := range b.entries {
		if e.id == requestID {
			return true
		}
	}

	// 3. Новый request_id: при переполнении вытеснить самый старый (FIFO),
	//    затем добавить в хвост.
	if len(b.entries) >= idempotencyCapacity {
		b.entries = b.entries[1:]
	}
	b.entries = append(b.entries, idempotencyEntry{id: requestID, added: now})
	return false
}
