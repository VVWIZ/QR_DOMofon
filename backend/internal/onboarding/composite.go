package onboarding

import (
	"strings"

	"domofon/backend/internal/platform/httpx"
)

// MaxCompositeGrants — кэп доп. точек в одном композитном инвайте УК. Ограничивает
// «радиус поражения» опечатки в телефоне (существующий пользователь молча получает
// owner + N грантов): даже при ошибке N ограничен (ревью архитектора, инкремент A).
const MaxCompositeGrants = 20

// NormalizeGrantPublicIDs чистит список публичных id точек доступа, поданный в
// композитном инвайте: выбрасывает пустые/пробельные, дедуплицирует с сохранением
// порядка, ограничивает число (> MaxCompositeGrants → VALIDATION_ERROR). Проверка
// «точка — gate/barrier своей УК» здесь НЕ делается (нужна БД) — это на стороне
// сервиса. Пустой результат (нет доп. точек) — валиден.
func NormalizeGrantPublicIDs(ids []string) ([]string, *httpx.Error) {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) > MaxCompositeGrants {
		return nil, httpx.NewError(httpx.CodeValidationError, "Too many access points in one invite")
	}
	return out, nil
}
