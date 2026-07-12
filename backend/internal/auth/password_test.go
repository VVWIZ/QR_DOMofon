package auth

import "testing"

const (
	testPassword  = "admin-demo-123" // фикстура УК-админа (auth.md §7)
	wrongPassword = "wrong-password"
)

func TestVerifyPassword_Correct(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword = %v", err)
	}
	if err := VerifyPassword(hash, testPassword); err != nil {
		t.Fatalf("VerifyPassword(верный) = %v, want nil", err)
	}
}

func TestVerifyPassword_Wrong(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword = %v", err)
	}
	if err := VerifyPassword(hash, wrongPassword); err == nil {
		t.Fatalf("VerifyPassword(неверный) = nil, want ошибка")
	}
}

func TestHashPassword_NonDeterministic(t *testing.T) {
	h1, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword #1 = %v", err)
	}
	h2, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("HashPassword #2 = %v", err)
	}
	if h1 == h2 {
		t.Fatalf("два хеша совпали (%q) — соль не подмешана", h1)
	}
	// Оба хеша, при всей несхожести, верифицируются исходным паролем.
	if err := VerifyPassword(h1, testPassword); err != nil {
		t.Errorf("VerifyPassword(h1) = %v, want nil", err)
	}
	if err := VerifyPassword(h2, testPassword); err != nil {
		t.Errorf("VerifyPassword(h2) = %v, want nil", err)
	}
}
