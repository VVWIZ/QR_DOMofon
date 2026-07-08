// Эмулятор устройства-домофона (firmware/emulator) — детерминированное ядро.
//
// Единый контракт устройства: firmware/docs/PROTOCOL.md §5. Пакет собран как
// package main (плоские файлы в каталоге эмулятора, см. architecture.md); func
// main появится на этапе firmware вместе с реализацией. `go test` этого пакета
// работает и без func main.
//
// СКЕЛЕТ ЭТАПА QA: тела паникуют ("not implemented"). Зафиксированы сигнатуры и
// инварианты под RED-тесты; реализацию пишет этап firmware.
//
// Этот файл: идемпотентность (PROTOCOL.md §5.1) — буфер последних 20 request_id,
// TTL 60с.
package main

import "time"

// idempotencyCapacity — сколько последних request_id хранит устройство (§5.1).
const idempotencyCapacity = 20

// idempotencyTTL — время жизни записи в буфере (§5.1).
const idempotencyTTL = 60 * time.Second

// IdempotencyBuffer — буфер обработанных request_id: ёмкость 20, TTL 60с.
// Вытеснение при переполнении — самая старая запись (по времени вставки); по
// TTL запись исчезает через 60с. Синхронизацию обеспечивает вызывающий (единый
// цикл обработки команд), сам буфер потокобезопасным быть не обязан.
type IdempotencyBuffer struct {
	// Поля реализует этап firmware.
}

// NewIdempotencyBuffer создаёт пустой буфер.
func NewIdempotencyBuffer() *IdempotencyBuffer {
	panic("not implemented: NewIdempotencyBuffer")
}

// Seen проверяет и одновременно фиксирует request_id на момент now:
//
//   - request_id ещё не в буфере (либо истёк по TTL, либо был вытеснен) →
//     запись добавляется, возвращается false («не видели»);
//   - request_id уже в буфере и не истёк → возвращается true («дубликат»),
//     содержимое буфера не меняется.
func (b *IdempotencyBuffer) Seen(requestID string, now time.Time) bool {
	panic("not implemented: IdempotencyBuffer.Seen")
}
