package calls

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// tokenTTL — срок жизни LiveKit-токена. Токен нужен только на join; после
// подключения сессия живёт сама. 15 мин — с запасом на звонок, но без «на весь
// день» (прод-гэп: минимизировать окно валидности).
const tokenTTL = 15 * time.Minute

// Track sources для гранта publish (ТЗ §6.1): visitor — камера+микрофон,
// resident — только микрофон.
var (
	visitorSources  = []string{"camera", "microphone"}
	residentSources = []string{"microphone"}
)

// LiveKit — адаптер self-hosted LiveKit: создание комнат (room = call_id) и
// выпуск токенов. Токены выпускает ТОЛЬКО backend (architecture.md §4.4).
type LiveKit struct {
	wsURL      string // ws:// для браузера
	apiKey     string
	apiSecret  string
	roomClient *lksdk.RoomServiceClient
}

// NewLiveKit создаёт адаптер. Для server-API (Twirp) ws-схема конвертируется в
// http; браузеру возвращается исходный ws-URL.
func NewLiveKit(wsURL, apiKey, apiSecret string) *LiveKit {
	return &LiveKit{
		wsURL:      wsURL,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		roomClient: lksdk.NewRoomServiceClient(httpURL(wsURL), apiKey, apiSecret),
	}
}

// URL возвращает ws-URL для подключения браузера.
func (l *LiveKit) URL() string { return l.wsURL }

// CreateRoom создаёт комнату name (= call_id). Идемпотентно на стороне LiveKit;
// при join комната создаётся автоматически, вызов — явная инициализация.
func (l *LiveKit) CreateRoom(ctx context.Context, name string) error {
	_, err := l.roomClient.CreateRoom(ctx, &livekit.CreateRoomRequest{Name: name})
	if err != nil {
		return fmt.Errorf("calls: create room: %w", err)
	}
	return nil
}

// CloseRoom удаляет комнату (cancel/end).
func (l *LiveKit) CloseRoom(ctx context.Context, name string) error {
	_, err := l.roomClient.DeleteRoom(ctx, &livekit.DeleteRoomRequest{Room: name})
	if err != nil {
		return fmt.Errorf("calls: delete room: %w", err)
	}
	return nil
}

// VisitorToken выпускает токен посетителя (publish camera+mic, subscribe).
func (l *LiveKit) VisitorToken(room, identity string) (string, error) {
	return l.token(room, identity, visitorSources)
}

// ResidentToken выпускает токен жильца (publish только mic, subscribe).
func (l *LiveKit) ResidentToken(room, identity string) (string, error) {
	return l.token(room, identity, residentSources)
}

// Health — лёгкая проверка доступности LiveKit для /health (ListRooms).
func (l *LiveKit) Health(ctx context.Context) error {
	_, err := l.roomClient.ListRooms(ctx, &livekit.ListRoomsRequest{})
	return err
}

// token собирает JWT с грантом RoomJoin для room и указанными источниками
// публикации.
func (l *LiveKit) token(room, identity string, sources []string) (string, error) {
	canPublish := true
	canSubscribe := true
	grant := &auth.VideoGrant{
		RoomJoin:          true,
		Room:              room,
		CanPublish:        &canPublish,
		CanSubscribe:      &canSubscribe,
		CanPublishSources: sources,
	}
	at := auth.NewAccessToken(l.apiKey, l.apiSecret).
		SetVideoGrant(grant).
		SetIdentity(identity).
		SetValidFor(tokenTTL)
	jwt, err := at.ToJWT()
	if err != nil {
		return "", fmt.Errorf("calls: build token: %w", err)
	}
	return jwt, nil
}

// httpURL конвертирует ws/wss в http/https для server-API LiveKit.
func httpURL(wsURL string) string {
	switch {
	case strings.HasPrefix(wsURL, "wss://"):
		return "https://" + strings.TrimPrefix(wsURL, "wss://")
	case strings.HasPrefix(wsURL, "ws://"):
		return "http://" + strings.TrimPrefix(wsURL, "ws://")
	default:
		return wsURL
	}
}
