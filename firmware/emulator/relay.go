// Конечный автомат реле + fail-open/гистерезис, PROTOCOL.md §5.3–5.5.
// Реализовано под контракт, покрытый relay_test.go.
package main

import "time"

// RelayState — состояние реле замка.
type RelayState string

const (
	// RelayClosed — реле в покое, NC-контакты замкнуты, замок под питанием,
	// дверь заперта.
	RelayClosed RelayState = "closed"
	// RelayOpen — катушка под сигналом, замок обесточен, дверь открыта.
	RelayOpen RelayState = "open"
)

// Пороги устройства (PROTOCOL.md §6, единый источник).
const (
	// failOpenThreshold — сколько непрерывного отсутствия связи до fail-open (§5.3).
	failOpenThreshold = 90 * time.Second
	// recoveryHysteresis — сколько непрерывно стабильной связи нужно для выхода
	// из fail-open (§5.4).
	recoveryHysteresis = 30 * time.Second
)

// RelayFSM — детерминированный автомат реле с ИНЪЕКЦИЕЙ времени: никаких
// реальных таймеров/sleep, каждый метод принимает now, все переходы
// вычисляются относительно переданного времени. Это делает воспроизводимыми в
// тестах: авто-закрытие по duration (§5.5), срабатывание fail-open по потере
// связи (§5.3), гистерезис восстановления и защиту от флаппинга (§5.4),
// приоритет fail-open над таймером команды (§5.5.5).
type RelayFSM struct {
	// openUntil — реле открыто командой, пока now < openUntil (§5.5).
	openUntil time.Time
	// connected — есть ли сейчас связь с брокером.
	connected bool
	// lostAt — момент последней потери связи (актуален при !connected), от него
	// отсчитывается порог fail-open (§5.3).
	lostAt time.Time
	// restoredAt — момент последнего восстановления связи (актуален при
	// connected), от него отсчитывается гистерезис восстановления (§5.4).
	restoredAt time.Time
	// failOpen — защёлка режима fail-open: взводится при потере связи ≥90с и
	// снимается только после ≥30с стабильной связи. Обновляется лениво в refresh,
	// т.к. срабатывание/снятие происходит по течению времени, а не по событию.
	failOpen bool
}

// NewRelayFSM создаёт автомат в состоянии closed с активной связью на момент now.
func NewRelayFSM(now time.Time) *RelayFSM {
	return &RelayFSM{
		connected:  true,
		restoredAt: now,
	}
}

// refresh лениво пересчитывает защёлку fail-open на момент now: взводит её при
// непрерывной потере связи ≥90с и снимает после ≥30с стабильной связи. Вызывается
// из всех методов, зависящих от времени. now в эмуляторе монотонно неубывающий.
func (r *RelayFSM) refresh(now time.Time) {
	if r.connected {
		if r.failOpen && now.Sub(r.restoredAt) >= recoveryHysteresis {
			r.failOpen = false
		}
		return
	}
	if !r.failOpen && now.Sub(r.lostAt) >= failOpenThreshold {
		r.failOpen = true
	}
}

// OpenRelay исполняет команду open_relay: реле удерживается open в течение
// duration, отсчитываемых от now (§5.5). Новая команда во время открытого реле
// перезапускает таймер удержания («последняя команда выигрывает», §5.5.4).
// Во время fail-open команда подтверждается, но реле уже open и остаётся open —
// fail-open имеет приоритет над таймером duration (§5.5.5).
func (r *RelayFSM) OpenRelay(now time.Time, duration time.Duration) {
	r.refresh(now)
	r.openUntil = now.Add(duration)
}

// ConnectionLost фиксирует потерю связи с MQTT-брокером в момент now. С этого
// момента идёт отсчёт порога fail-open (90с).
func (r *RelayFSM) ConnectionLost(now time.Time) {
	r.refresh(now)
	r.connected = false
	r.lostAt = now
}

// ConnectionRestored фиксирует (ре)установление связи в момент now и
// (пере)запускает отсчёт гистерезиса восстановления (30с стабильной связи).
func (r *RelayFSM) ConnectionRestored(now time.Time) {
	// Сначала учесть, не был ли к этому моменту достигнут порог fail-open за
	// время офлайна (взвести защёлку даже без промежуточных запросов State),
	// затем зафиксировать связь и (пере)запустить отсчёт гистерезиса.
	r.refresh(now)
	r.connected = true
	r.restoredAt = now
}

// State возвращает состояние реле на момент now, вычисляя переходы по времени:
// истечение duration команды, срабатывание fail-open при потере связи ≥90с,
// выход из fail-open после ≥30с стабильной связи.
func (r *RelayFSM) State(now time.Time) RelayState {
	r.refresh(now)
	if r.failOpen {
		return RelayOpen
	}
	if now.Before(r.openUntil) {
		return RelayOpen
	}
	return RelayClosed
}

// FailOpen сообщает, удерживается ли реле open режимом fail-open на момент now.
func (r *RelayFSM) FailOpen(now time.Time) bool {
	r.refresh(now)
	return r.failOpen
}
