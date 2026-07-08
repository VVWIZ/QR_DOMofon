// Конечный автомат реле + fail-open/гистерезис, PROTOCOL.md §5.3–5.5.
//
// СКЕЛЕТ ЭТАПА QA: тела паникуют. Реализацию пишет этап firmware.
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
	// Поля реализует этап firmware.
}

// NewRelayFSM создаёт автомат в состоянии closed с активной связью на момент now.
func NewRelayFSM(now time.Time) *RelayFSM {
	panic("not implemented: NewRelayFSM")
}

// OpenRelay исполняет команду open_relay: реле удерживается open в течение
// duration, отсчитываемых от now (§5.5). Новая команда во время открытого реле
// перезапускает таймер удержания («последняя команда выигрывает», §5.5.4).
// Во время fail-open команда подтверждается, но реле уже open и остаётся open —
// fail-open имеет приоритет над таймером duration (§5.5.5).
func (r *RelayFSM) OpenRelay(now time.Time, duration time.Duration) {
	panic("not implemented: RelayFSM.OpenRelay")
}

// ConnectionLost фиксирует потерю связи с MQTT-брокером в момент now. С этого
// момента идёт отсчёт порога fail-open (90с).
func (r *RelayFSM) ConnectionLost(now time.Time) {
	panic("not implemented: RelayFSM.ConnectionLost")
}

// ConnectionRestored фиксирует (ре)установление связи в момент now и
// (пере)запускает отсчёт гистерезиса восстановления (30с стабильной связи).
func (r *RelayFSM) ConnectionRestored(now time.Time) {
	panic("not implemented: RelayFSM.ConnectionRestored")
}

// State возвращает состояние реле на момент now, вычисляя переходы по времени:
// истечение duration команды, срабатывание fail-open при потере связи ≥90с,
// выход из fail-open после ≥30с стабильной связи.
func (r *RelayFSM) State(now time.Time) RelayState {
	panic("not implemented: RelayFSM.State")
}

// FailOpen сообщает, удерживается ли реле open режимом fail-open на момент now.
func (r *RelayFSM) FailOpen(now time.Time) bool {
	panic("not implemented: RelayFSM.FailOpen")
}
