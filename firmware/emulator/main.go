// Эмулятор устройства-домофона (firmware/emulator) — слой интеграции.
//
// Обвязывает детерминированное ядро (relay.go, commands.go, idempotency.go) в
// живой MQTT-клиент (eclipse/paho.mqtt.golang, MQTT 3.1.1). Единый контракт —
// firmware/docs/PROTOCOL.md; канонические UUID/serial — architecture.md §5.
//
// Устройство подключается к брокеру (ClientID device:{serial}, CleanSession
// false — брокер буферизует QoS1-команды, выданные пока устройство offline),
// подписывается на devices/{device_id}/commands, публикует heartbeat/
// command_ack/event в devices/{device_id}/status. Всё время в ядро инъецируется
// (now = time.Now().UTC()); фоновый тикер двигает время и наблюдает переходы
// реле и fail-open.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

const (
	// tickInterval — период фонового опроса состояния ядра. Выбран 100мс, чтобы
	// авто-закрытие реле по истечении duration_ms детектировалось в пределах
	// допуска ±100мс для эмулятора (§5.5.3), а не с задержкой до 1с.
	tickInterval = 100 * time.Millisecond
	// publishTimeout / subscribeTimeout — ожидание подтверждения брокера.
	publishTimeout   = 5 * time.Second
	subscribeTimeout = 5 * time.Second
	// rssiDummy — фиктивный уровень сигнала (§4.1: эмулятор публикует фиктивное
	// значение).
	rssiDummy = -65
)

// Device связывает ядро (FSM реле, буфер идемпотентности) с MQTT-клиентом и
// хранит наблюдаемое состояние для детекции переходов. Единый mutex mu
// сериализует доступ к ядру (FSM синхронизацию не обеспечивает — её даёт
// вызывающий, см. relay.go/idempotency.go).
type Device struct {
	cfg Config
	log *slog.Logger

	client paho.Client

	mu         sync.Mutex
	fsm        *RelayFSM
	idemp      *IdempotencyBuffer
	relayState RelayState // последнее наблюдённое состояние реле
	failOpen   bool       // последнее наблюдённое состояние защёлки fail-open
	online     bool       // есть ли сейчас соединение с брокером

	startedAt time.Time
	offline   *offlineEventBuffer // буфер событий на время отсутствия связи (§4.3)

	commandsTopic string
	statusTopic   string
}

func main() {
	cfg := LoadConfig()

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg.validate(log)
	log.Info("emulator_starting",
		"device_id", cfg.DeviceID,
		"serial", cfg.DeviceSerial,
		"broker", cfg.BrokerURL,
		"firmware_version", cfg.FirmwareVersion,
		"heartbeat_interval", cfg.HeartbeatInterval,
		"keepalive", cfg.Keepalive,
	)

	d := newDevice(cfg, log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Недоступность брокера на старте не фатальна: SetConnectRetry поднимает
	// соединение в фоне, OnConnect выполнит подписку/heartbeat при успехе.
	if err := d.connect(ctx); err != nil {
		log.Error("mqtt_connect_failed", "error", err)
	}

	go d.runStateLoop(ctx)
	go d.runHeartbeatLoop(ctx)

	<-ctx.Done()
	log.Info("emulator_stopping")
	d.client.Disconnect(250)
}

// newDevice создаёт устройство в состоянии closed/offline с чистым ядром.
func newDevice(cfg Config, log *slog.Logger) *Device {
	now := time.Now().UTC()
	return &Device{
		cfg:           cfg,
		log:           log,
		fsm:           NewRelayFSM(now),
		idemp:         NewIdempotencyBuffer(),
		relayState:    RelayClosed,
		startedAt:     now,
		offline:       newOfflineEventBuffer(),
		commandsTopic: fmt.Sprintf("devices/%s/commands", cfg.DeviceID),
		statusTopic:   fmt.Sprintf("devices/%s/status", cfg.DeviceID),
	}
}

// connect конфигурирует paho-клиент (ClientID device:{serial}, CleanSession
// false, keepalive из конфига, auto-reconnect + connect-retry) и инициирует
// подключение. Ждёт первую попытку не дольше ctx, результат — для логирования.
func (d *Device) connect(ctx context.Context) error {
	clientID := "device:" + d.cfg.DeviceSerial

	opts := paho.NewClientOptions().
		AddBroker(d.cfg.BrokerURL).
		SetClientID(clientID).
		SetKeepAlive(d.cfg.Keepalive).
		SetCleanSession(false).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetMaxReconnectInterval(30 * time.Second)

	// OnConnect срабатывает при первом подключении И при каждом реконнекте.
	// Работу (подписка/публикация с ожиданием) выносим в отдельную горутину:
	// блокирующие вызовы paho внутри callback могут застопорить его connect-рутину.
	opts.SetOnConnectHandler(func(_ paho.Client) {
		go d.handleConnect()
	})
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		d.handleConnectionLost(err)
	})

	d.client = paho.NewClient(opts)

	tok := d.client.Connect()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-tokenReady(tok):
		return tok.Error()
	}
}

// handleConnect (пере)устанавливает связь: фиксирует восстановление в ядре,
// переподписывается, выкладывает накопленные offline-события и шлёт немедленный
// heartbeat. Порядок публикации — PROTOCOL.md §4.3: буферизованные event →
// текущий heartbeat → обычный режим.
func (d *Device) handleConnect() {
	now := time.Now().UTC()

	d.mu.Lock()
	d.fsm.ConnectionRestored(now)
	d.online = true
	d.mu.Unlock()

	d.log.Info("mqtt_connected", "broker", d.cfg.BrokerURL, "client_id", "device:"+d.cfg.DeviceSerial)

	if err := d.subscribeCommands(); err != nil {
		d.log.Error("mqtt_subscribe_failed", "topic", d.commandsTopic, "error", err)
	}
	d.flushOfflineEvents()
	d.publishHeartbeat(now)
}

// handleConnectionLost фиксирует потерю связи в ядре (запускает отсчёт порога
// fail-open) и помечает устройство offline.
func (d *Device) handleConnectionLost(err error) {
	now := time.Now().UTC()

	d.mu.Lock()
	d.fsm.ConnectionLost(now)
	d.online = false
	d.mu.Unlock()

	d.log.Warn("mqtt_connection_lost", "error", err)
}

// subscribeCommands подписывается на топик команд (QoS1). Вызывается в
// OnConnect — при persistent-сессии переподписка идемпотентна.
func (d *Device) subscribeCommands() error {
	tok := d.client.Subscribe(d.commandsTopic, 1, func(_ paho.Client, m paho.Message) {
		d.handleCommand(m.Payload())
	})
	if !tok.WaitTimeout(subscribeTimeout) {
		return fmt.Errorf("subscribe timeout: %s", d.commandsTopic)
	}
	return tok.Error()
}

// runStateLoop раз в tickInterval двигает время: опрашивает ядро и публикует
// heartbeat/event на переходах реле и fail-open.
func (d *Device) runStateLoop(ctx context.Context) {
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.pollState(time.Now().UTC())
		}
	}
}

// pollState вычисляет текущее состояние реле и защёлки fail-open на момент now
// и публикует переходы:
//   - closed→fail_open (по потере связи) → event fail_open_activated (уходит в
//     offline-буфер, т.к. связи нет);
//   - fail_open→closed (после стабильного восстановления) → event
//     fail_open_deactivated + внеочередной heartbeat (§5.4);
//   - любая смена relay_state → внеочередной heartbeat (§4.1).
func (d *Device) pollState(now time.Time) {
	d.mu.Lock()
	newRelay := d.fsm.State(now)
	newFailOpen := d.fsm.FailOpen(now)
	oldRelay := d.relayState
	oldFailOpen := d.failOpen
	d.relayState = newRelay
	d.failOpen = newFailOpen
	d.mu.Unlock()

	if !oldFailOpen && newFailOpen {
		d.log.Warn("fail_open_activated", "timestamp", iso8601(now))
		d.emitEvent(eventFailOpenActivated, now)
	}
	if oldFailOpen && !newFailOpen {
		d.log.Info("fail_open_deactivated", "timestamp", iso8601(now))
		d.emitEvent(eventFailOpenDeactivated, now)
	}
	if oldRelay != newRelay {
		d.publishHeartbeat(now)
	}
}

// publishJSON сериализует v и публикует в topic с QoS1 (retained=false).
func (d *Device) publishJSON(topic string, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		d.log.Error("marshal_failed", "topic", topic, "error", err)
		return
	}
	tok := d.client.Publish(topic, 1, false, payload)
	if !tok.WaitTimeout(publishTimeout) {
		d.log.Error("publish_timeout", "topic", topic)
		return
	}
	if err := tok.Error(); err != nil {
		d.log.Error("publish_failed", "topic", topic, "error", err)
	}
}

// iso8601 форматирует момент как ISO 8601 UTC с суффиксом Z (без долей секунды).
func iso8601(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// tokenReady превращает завершение paho-токена в канал для select с контекстом.
func tokenReady(tok paho.Token) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		tok.Wait()
		close(ch)
	}()
	return ch
}
