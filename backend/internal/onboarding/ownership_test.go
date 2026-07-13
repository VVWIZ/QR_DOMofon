package onboarding

import (
	"testing"

	"domofon/backend/internal/auth"
)

const (
	aptOwn   = "33333333-3333-3333-3333-333333333333"
	aptOther = "44444444-4444-4444-4444-444444444444"
)

func ownerClaims() auth.Claims {
	return auth.Claims{
		Subject: "77777777-7777-7777-7777-777777777777",
		Kind:    auth.KindOwner,
		Roles:   []auth.ApartmentRole{{ApartmentID: aptOwn, Role: "owner"}},
	}
}

func residentClaims() auth.Claims {
	return auth.Claims{
		Subject: "88888888-8888-8888-8888-888888888888",
		Kind:    auth.KindResident,
		Roles:   []auth.ApartmentRole{{ApartmentID: aptOwn, Role: "resident"}},
	}
}

func adminClaims() auth.Claims {
	return auth.Claims{
		Subject: "99999999-9999-9999-9999-999999999999",
		Kind:    auth.KindAdmin,
		Roles:   nil,
		MCID:    "11111111-1111-1111-1111-111111111111",
	}
}

func TestCanInviteToApartment_OwnerOfApartment(t *testing.T) {
	if !CanInviteToApartment(ownerClaims(), aptOwn) {
		t.Fatalf("владелец своей квартиры → false, want true")
	}
}

func TestCanInviteToApartment_ResidentDenied(t *testing.T) {
	if CanInviteToApartment(residentClaims(), aptOwn) {
		t.Fatalf("жилец (не владелец) той же квартиры → true, want false")
	}
}

func TestCanInviteToApartment_OwnerOfOtherApartmentDenied(t *testing.T) {
	if CanInviteToApartment(ownerClaims(), aptOther) {
		t.Fatalf("владелец другой квартиры → true, want false")
	}
}

func TestCanInviteToApartment_AdminDenied(t *testing.T) {
	if CanInviteToApartment(adminClaims(), aptOwn) {
		t.Fatalf("mc_admin (roles пуст) → true, want false")
	}
}
