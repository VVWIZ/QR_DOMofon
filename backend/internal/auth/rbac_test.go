package auth

import "testing"

const (
	aptOwn   = "33333333-3333-3333-3333-333333333333" // квартира жильца (фикстура)
	aptOther = "44444444-4444-4444-4444-444444444444" // чужая квартира
	mcID     = "11111111-1111-1111-1111-111111111111"
)

func residentClaims() Claims {
	return Claims{
		Subject: "77777777-7777-7777-7777-777777777777",
		Kind:    KindResident,
		Roles:   []ApartmentRole{{ApartmentID: aptOwn, Role: "resident"}},
	}
}

func adminClaims() Claims {
	return Claims{
		Subject: "99999999-9999-9999-9999-999999999999",
		Kind:    KindAdmin,
		Roles:   nil,
		MCID:    mcID,
	}
}

func systemClaims() Claims {
	return Claims{
		Subject: "5a5a5a5a-5a5a-5a5a-5a5a-5a5a5a5a5a5a",
		Kind:    KindSystem,
		Roles:   nil,
		MCID:    "", // платформенный админ не привязан к УК
	}
}

// system_admin НЕ должен считаться mc-админом (иначе прошёл бы RequireAdmin и
// получил mc-скоуп с пустым MCID). Уровни доступа строго раздельны.
func TestKind_SystemAdminIsNotMcAdmin(t *testing.T) {
	if systemClaims().Kind.IsAdmin() {
		t.Fatalf("system_admin.IsAdmin() = true, want false")
	}
	if !systemClaims().Kind.IsSystemAdmin() {
		t.Fatalf("system_admin.IsSystemAdmin() = false, want true")
	}
	if adminClaims().Kind.IsSystemAdmin() {
		t.Fatalf("mc_admin.IsSystemAdmin() = true, want false")
	}
}

func TestAllowApartment_ResidentOwnApartment(t *testing.T) {
	if !AllowApartment(residentClaims(), aptOwn) {
		t.Fatalf("AllowApartment(своя квартира) = false, want true")
	}
}

func TestAllowApartment_ResidentOtherApartmentDenied(t *testing.T) {
	if AllowApartment(residentClaims(), aptOther) {
		t.Fatalf("AllowApartment(чужая квартира) = true, want false")
	}
}

func TestAllowApartment_AdminHasNoApartmentAccess(t *testing.T) {
	if AllowApartment(adminClaims(), aptOwn) {
		t.Fatalf("AllowApartment(mc_admin) = true, want false (нет apartment-ролей)")
	}
}

func TestKind_IsResident(t *testing.T) {
	cases := map[Kind]bool{
		KindResident: true,
		KindOwner:    true,
		KindAdmin:    false,
	}
	for k, want := range cases {
		if got := k.IsResident(); got != want {
			t.Errorf("Kind(%q).IsResident() = %v, want %v", k, got, want)
		}
	}
}

func TestKind_IsAdmin(t *testing.T) {
	cases := map[Kind]bool{
		KindAdmin:    true,
		KindResident: false,
		KindOwner:    false,
	}
	for k, want := range cases {
		if got := k.IsAdmin(); got != want {
			t.Errorf("Kind(%q).IsAdmin() = %v, want %v", k, got, want)
		}
	}
}
