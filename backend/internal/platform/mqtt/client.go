// Package mqtt — обёртка над eclipse/paho.mqtt.golang (MQTT 3.1.1) с
// auto-reconnect и повторной подпиской при восстановлении соединения. Контракт
// топиков/payload — firmware/docs/PROTOCOL.md. Backend публикует команды
// open_relay и потребляет devices/+/status.
package mqtt

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// keepAlive — 30с (PROTOCOL.md §1: инвариант 1.5×keepalive < 90с).
const keepAlive = 30 * time.Second

// publishTimeout / subscribeTimeout — сколько ждём подтверждения брокера.
const (
	publishTimeout   = 5 * time.Second
	subscribeTimeout = 5 * time.Second
)

// MessageHandler — обработчик входящего сообщения (топик + сырой payload).
type MessageHandler func(topic string, payload []byte)

// subscription — сохранённая подписка для повторного применения на reconnect.
type subscription struct {
	handler MessageHandler
}

// Client — потокобезопасная обёртка над paho-клиентом.
type Client struct {
	c   paho.Client
	log *slog.Logger

	mu   sync.Mutex
	subs map[string]subscription
}

// New конфигурирует клиент (QoS1 везде, clean session true — backend
// пересоздаёт подписки сам в OnConnect), но ещё не подключается.
func New(brokerURL, clientID string, log *slog.Logger) *Client {
	client := &Client{
		log:  log,
		subs: make(map[string]subscription),
	}

	opts := paho.NewClientOptions().
		AddBroker(brokerURL).
		SetClientID(clientID).
		SetKeepAlive(keepAlive).
		SetCleanSession(true).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetMaxReconnectInterval(30 * time.Second)

	opts.SetOnConnectHandler(func(_ paho.Client) {
		log.Info("mqtt_connected", "broker", brokerURL, "client_id", clientID)
		client.resubscribe()
	})
	opts.SetConnectionLostHandler(func(_ paho.Client, err error) {
		log.Warn("mqtt_connection_lost", "error", err)
	})

	client.c = paho.NewClient(opts)
	return client
}

// Connect инициирует подключение. С SetConnectRetry(true) недоступность брокера
// на старте не фатальна — соединение устанавливается в фоне; здесь ждём первую
// попытку не дольше timeout и возвращаем её результат для логирования.
func (c *Client) Connect(ctx context.Context) error {
	tok := c.c.Connect()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh(tok):
		return tok.Error()
	}
}

// Publish публикует payload в topic с QoS1 (retained=false).
func (c *Client) Publish(topic string, payload []byte) error {
	tok := c.c.Publish(topic, 1, false, payload)
	if !tok.WaitTimeout(publishTimeout) {
		return fmt.Errorf("mqtt: publish timeout topic=%s", topic)
	}
	return tok.Error()
}

// Subscribe регистрирует обработчик topic (QoS1) и сохраняет подписку для
// восстановления после reconnect.
func (c *Client) Subscribe(topic string, handler MessageHandler) error {
	c.mu.Lock()
	c.subs[topic] = subscription{handler: handler}
	c.mu.Unlock()
	return c.subscribe(topic, handler)
}

// subscribe выполняет фактическую подписку в paho.
func (c *Client) subscribe(topic string, handler MessageHandler) error {
	tok := c.c.Subscribe(topic, 1, func(_ paho.Client, m paho.Message) {
		handler(m.Topic(), m.Payload())
	})
	if !tok.WaitTimeout(subscribeTimeout) {
		return fmt.Errorf("mqtt: subscribe timeout topic=%s", topic)
	}
	return tok.Error()
}

// resubscribe заново применяет все сохранённые подписки (вызов из OnConnect).
func (c *Client) resubscribe() {
	c.mu.Lock()
	subs := make(map[string]subscription, len(c.subs))
	for t, s := range c.subs {
		subs[t] = s
	}
	c.mu.Unlock()

	for topic, s := range subs {
		if err := c.subscribe(topic, s.handler); err != nil {
			c.log.Error("mqtt_resubscribe_failed", "topic", topic, "error", err)
		}
	}
}

// IsConnected сообщает, есть ли активное соединение с брокером (для /health).
func (c *Client) IsConnected() bool {
	return c.c.IsConnectionOpen()
}

// Disconnect закрывает соединение (graceful shutdown), давая брокеру 250мс.
func (c *Client) Disconnect() {
	c.c.Disconnect(250)
}

// waitCh превращает завершение paho-токена в канал для select с контекстом.
func waitCh(tok paho.Token) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		tok.Wait()
		close(ch)
	}()
	return ch
}
