package auth

// Хелперы извлечения данных claims из контекста запроса (после Authenticator).
// Используются адаптерами cmd/server: authorizer (accept/open), резолвер квартир
// SSE, скоуп mc_id для devices/audit. Claims не мутируются.

import "context"

// MCIDFromContext возвращает management_company_id из claims текущего запроса
// ("" если запрос не прошёл Authenticator или mc_id пуст, напр. у жильца).
func MCIDFromContext(ctx context.Context) string {
	c, ok := ClaimsFromContext(ctx)
	if !ok {
		return ""
	}
	return c.MCID
}

// ApartmentsFromContext возвращает apartment_id всех ролей пользователя из claims
// (для подписки SSE на квартиры). Пусто, если Authenticator не отработал или
// ролей нет (напр. mc_admin).
func ApartmentsFromContext(ctx context.Context) []string {
	c, ok := ClaimsFromContext(ctx)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(c.Roles))
	for _, r := range c.Roles {
		out = append(out, r.ApartmentID)
	}
	return out
}

// AllowApartmentFromContext сверяет привязку текущего пользователя к квартире
// apartmentID по claims из контекста (auth.AllowApartment). Используется
// Authorizer-адаптером calls/access.
func AllowApartmentFromContext(ctx context.Context, apartmentID string) bool {
	c, ok := ClaimsFromContext(ctx)
	if !ok {
		return false
	}
	return AllowApartment(c, apartmentID)
}
