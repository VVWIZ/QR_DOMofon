package auth

// RBAC-предикаты на claims (auth.md §1). Именование через методы Kind, чтобы не
// пересекаться с middleware RequireResident/RequireAdmin (middleware.go).

// IsResident — kind даёт жилец/владелец доступ (resident || owner).
func (k Kind) IsResident() bool {
	return k == KindResident || k == KindOwner
}

// IsAdmin — kind = mc_admin (УК-админ).
func (k Kind) IsAdmin() bool {
	return k == KindAdmin
}

// AllowApartment сообщает, привязан ли владелец claims к квартире apartmentID
// (есть роль с таким apartment_id). У mc_admin roles пуст → всегда false:
// admin не имеет apartment-доступа (доменная проверка на accept/access-open,
// auth.md §1).
func AllowApartment(c Claims, apartmentID string) bool {
	for _, r := range c.Roles {
		if r.ApartmentID == apartmentID {
			return true
		}
	}
	return false
}
