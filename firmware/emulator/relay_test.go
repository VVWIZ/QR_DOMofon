package main

import (
	"testing"
	"time"
)

var relayBase = time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)

// §5.5: команда open_relay открывает реле; по истечении duration — закрывает.
func TestRelay_OpenThenAutoClose(t *testing.T) {
	r := NewRelayFSM(relayBase)
	if got := r.State(relayBase); got != RelayClosed {
		t.Fatalf("исходное состояние = %s, want closed", got)
	}
	r.OpenRelay(relayBase, 5*time.Second)
	if got := r.State(relayBase); got != RelayOpen {
		t.Fatalf("сразу после open_relay = %s, want open", got)
	}
	if got := r.State(relayBase.Add(2 * time.Second)); got != RelayOpen {
		t.Fatalf("+2с (в пределах duration) = %s, want open", got)
	}
	if got := r.State(relayBase.Add(6 * time.Second)); got != RelayClosed {
		t.Fatalf("+6с (после duration) = %s, want closed", got)
	}
}

// §5.3: непрерывная потеря связи ≥90с → реле переводится в open (fail-open).
func TestRelay_FailOpenAfter90sOffline(t *testing.T) {
	r := NewRelayFSM(relayBase)
	r.ConnectionLost(relayBase)

	// До порога — реле закрыто, fail-open не активен.
	if got := r.State(relayBase.Add(89 * time.Second)); got != RelayClosed {
		t.Fatalf("+89с без связи = %s, want closed", got)
	}
	if r.FailOpen(relayBase.Add(89 * time.Second)) {
		t.Fatal("+89с без связи: fail-open не должен быть активен")
	}

	// На пороге 90с — fail-open активен, реле open.
	if got := r.State(relayBase.Add(90 * time.Second)); got != RelayOpen {
		t.Fatalf("+90с без связи = %s, want open (fail-open)", got)
	}
	if !r.FailOpen(relayBase.Add(90 * time.Second)) {
		t.Fatal("+90с без связи: fail-open должен быть активен")
	}

	// Реле активно удерживается open, пока связи нет.
	if got := r.State(relayBase.Add(200 * time.Second)); got != RelayOpen {
		t.Fatalf("+200с без связи = %s, want open (удерживается)", got)
	}
}

// §5.4: выход из fail-open только после ≥30с непрерывно стабильной связи.
func TestRelay_RecoveryHysteresis(t *testing.T) {
	r := NewRelayFSM(relayBase)
	r.ConnectionLost(relayBase)
	if got := r.State(relayBase.Add(90 * time.Second)); got != RelayOpen {
		t.Fatalf("предусловие: fail-open не сработал (%s)", got)
	}

	restored := relayBase.Add(100 * time.Second)
	r.ConnectionRestored(restored)

	// Связь стабильна <30с — остаётся fail-open (реле open).
	if got := r.State(restored.Add(29 * time.Second)); got != RelayOpen {
		t.Fatalf("+29с стабильной связи = %s, want open (ещё fail-open)", got)
	}
	if !r.FailOpen(restored.Add(29 * time.Second)) {
		t.Fatal("+29с стабильной связи: fail-open ещё активен")
	}

	// Связь стабильна ≥30с — реле возвращается в closed.
	if got := r.State(restored.Add(30 * time.Second)); got != RelayClosed {
		t.Fatalf("+30с стабильной связи = %s, want closed", got)
	}
	if r.FailOpen(restored.Add(30 * time.Second)) {
		t.Fatal("+30с стабильной связи: fail-open должен быть снят")
	}
}

// §5.4: кратковременный реконнект (<30с) НЕ закрывает реле и сбрасывает таймер
// стабильности — защита от флаппинга.
func TestRelay_FlappingResetsStabilityTimer(t *testing.T) {
	r := NewRelayFSM(relayBase)
	r.ConnectionLost(relayBase)
	if got := r.State(relayBase.Add(90 * time.Second)); got != RelayOpen {
		t.Fatalf("предусловие: fail-open не сработал (%s)", got)
	}

	// Первый реконнект продержался лишь 20с — реле не закрывается.
	firstReconnect := relayBase.Add(100 * time.Second)
	r.ConnectionRestored(firstReconnect)
	if got := r.State(firstReconnect.Add(20 * time.Second)); got != RelayOpen {
		t.Fatalf("+20с первой связи = %s, want open", got)
	}
	// Связь снова оборвалась (раньше 30с).
	r.ConnectionLost(firstReconnect.Add(20 * time.Second))
	if got := r.State(firstReconnect.Add(25 * time.Second)); got != RelayOpen {
		t.Fatalf("после повторного обрыва = %s, want open", got)
	}

	// Второй реконнект: таймер стабильности стартует заново от него.
	secondReconnect := firstReconnect.Add(30 * time.Second)
	r.ConnectionRestored(secondReconnect)
	// 29с от ВТОРОГО реконнекта — ещё не закрыто (таймер сброшен, не суммируется).
	if got := r.State(secondReconnect.Add(29 * time.Second)); got != RelayOpen {
		t.Fatalf("+29с второй связи = %s, want open (таймер сброшен)", got)
	}
	// 30с непрерывной связи от второго реконнекта — закрывается.
	if got := r.State(secondReconnect.Add(30 * time.Second)); got != RelayClosed {
		t.Fatalf("+30с второй связи = %s, want closed", got)
	}
}

// §5.5.5: fail-open имеет приоритет — команда open_relay во время fail-open
// подтверждается, но реле остаётся open даже после истечения её duration
// (пока связь не восстановлена стабильно).
func TestRelay_FailOpenPriorityOverCommand(t *testing.T) {
	r := NewRelayFSM(relayBase)
	r.ConnectionLost(relayBase)
	failMoment := relayBase.Add(90 * time.Second)
	if got := r.State(failMoment); got != RelayOpen {
		t.Fatalf("предусловие: fail-open не сработал (%s)", got)
	}

	// Короткая команда приходит во время fail-open.
	r.OpenRelay(failMoment, 5*time.Second)
	if got := r.State(failMoment); got != RelayOpen {
		t.Fatalf("open_relay во время fail-open = %s, want open", got)
	}
	// duration (5с) истёк, но связи по-прежнему нет → реле держится open по fail-open.
	if got := r.State(failMoment.Add(6 * time.Second)); got != RelayOpen {
		t.Fatalf("+6с (duration истёк, связи нет) = %s, want open (приоритет fail-open)", got)
	}
	if !r.FailOpen(failMoment.Add(6 * time.Second)) {
		t.Fatal("fail-open должен оставаться активным")
	}
}
