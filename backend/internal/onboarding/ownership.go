package onboarding

import "domofon/backend/internal/auth"

// CanInviteToApartment сообщает, вправе ли пользователь (claims) создать инвайт в
// квартиру apartmentID. Право есть ТОЛЬКО у владельца этой квартиры: среди
// claims.Roles должна быть роль с этим apartment_id И role == "owner". Жилец
// (resident) той же квартиры, владелец другой квартиры и mc_admin (roles пуст) —
// не вправе.
func CanInviteToApartment(claims auth.Claims, apartmentID string) bool {
	for _, r := range claims.Roles {
		if r.ApartmentID == apartmentID && r.Role == "owner" {
			return true
		}
	}
	return false
}
