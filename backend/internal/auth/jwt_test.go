package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"reflect"
	"testing"
	"time"
)

// testKey — свежий RSA-2048 на каждый вызов (детерминированность не нужна:
// тест подписывает и проверяет одним и тем же ключом).
func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return k
}

// accessClaims — эталонные access-claims жильца (api.md пример).
func accessClaims(exp time.Time) Claims {
	return Claims{
		Subject: "77777777-7777-7777-7777-777777777777",
		Kind:    KindResident,
		Roles: []ApartmentRole{
			{ApartmentID: "33333333-3333-3333-3333-333333333333", Role: "resident", CanCreateGuests: false},
		},
		MCID:      "",
		JTI:       "11111111-2222-3333-4444-555555555555",
		IssuedAt:  exp.Add(-AccessTTL).Unix(),
		ExpiresAt: exp.Unix(),
		Type:      TypeAccess,
	}
}

func TestSignParse_RoundTrip(t *testing.T) {
	priv := testKey(t)
	want := accessClaims(time.Now().Add(AccessTTL))

	token, err := Sign(priv, want)
	if err != nil {
		t.Fatalf("Sign = %v", err)
	}
	got, err := Parse(&priv.PublicKey, token)
	if err != nil {
		t.Fatalf("Parse = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round-trip claims\n got = %+v\nwant = %+v", got, want)
	}
}

func TestParse_Expired(t *testing.T) {
	priv := testKey(t)
	// exp в прошлом → токен просрочен.
	claims := accessClaims(time.Now().Add(-time.Minute))

	token, err := Sign(priv, claims)
	if err != nil {
		t.Fatalf("Sign = %v", err)
	}
	if _, err := Parse(&priv.PublicKey, token); err == nil {
		t.Fatalf("Parse(просроченный) = nil, want ошибка")
	}
}

func TestParse_WrongKeyRejected(t *testing.T) {
	signer := testKey(t)
	other := testKey(t)
	claims := accessClaims(time.Now().Add(AccessTTL))

	token, err := Sign(signer, claims)
	if err != nil {
		t.Fatalf("Sign = %v", err)
	}
	// Подпись проверяется ЧУЖИМ публичным ключом → отказ.
	if _, err := Parse(&other.PublicKey, token); err == nil {
		t.Fatalf("Parse(чужой ключ) = nil, want ошибка")
	}
}

func TestVerifyAccess_RejectsRefreshType(t *testing.T) {
	priv := testKey(t)
	claims := accessClaims(time.Now().Add(RefreshTTL))
	claims.Type = TypeRefresh // refresh-токен

	token, err := Sign(priv, claims)
	if err != nil {
		t.Fatalf("Sign = %v", err)
	}
	v := NewRSAVerifier(&priv.PublicKey)
	if _, err := v.VerifyAccess(token); err == nil {
		t.Fatalf("VerifyAccess(typ=refresh) = nil, want ошибка (ожидался access)")
	}
}

func TestVerifyAccess_AcceptsAccessType(t *testing.T) {
	priv := testKey(t)
	claims := accessClaims(time.Now().Add(AccessTTL))

	token, err := Sign(priv, claims)
	if err != nil {
		t.Fatalf("Sign = %v", err)
	}
	v := NewRSAVerifier(&priv.PublicKey)
	got, err := v.VerifyAccess(token)
	if err != nil {
		t.Fatalf("VerifyAccess(access) = %v, want nil", err)
	}
	if got.Subject != claims.Subject || got.Type != TypeAccess {
		t.Fatalf("VerifyAccess claims = %+v, want sub=%s typ=access", got, claims.Subject)
	}
}
