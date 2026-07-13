package onboarding

import (
	"time"

	"domofon/backend/internal/platform/httpx"
)

// Invite — состояние инвайт-ссылки, достаточное для проверки валидности без БД.
// TokenHash — SHA-256 секрета (HashToken), в открытом виде секрет не хранится.
type Invite struct {
	TokenHash   string
	ApartmentID string
	Role        string     // роль, выдаваемая по инвайту (owner | resident)
	UsedAt      *time.Time // nil, пока не активирован
	ExpiresAt   time.Time
}

// Validate проверяет пригодность инвайта на момент now:
//
//   - уже активирован (UsedAt != nil) → INVITE_INVALID (404);
//   - истёк (now > ExpiresAt)         → INVITE_EXPIRED (410);
//   - иначе                           → nil (валиден).
//
// Порядок: сначала used, затем expiry (использованная ссылка невалидна вне
// зависимости от срока). now инъектируется для детерминизма.
func (i Invite) Validate(now time.Time) *httpx.Error {
	panic("not implemented: onboarding.Invite.Validate")
}
