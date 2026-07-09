// Свежесть команды (anti-stale), PROTOCOL.md §5.2.
// Реализовано под контракт, покрытый commands_test.go.
package main

import "time"

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
