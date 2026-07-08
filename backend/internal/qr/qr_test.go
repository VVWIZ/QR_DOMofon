package qr

import (
	"errors"
	"testing"
)

// Канонические фикстуры (architecture.md §5). sig проверен независимо (openssl
// / python hmac): base64url(HMAC-SHA256("aid:v:kid", secret))[0:32].
const (
	canonAID    = "55555555-5555-5555-5555-555555555555"
	canonV      = "1"
	canonKID    = "dev1"
	canonSecret = "dev-qr-secret-change-me"
	canonSig    = "oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6"
)

// fakeKeyring — реализация Keyring на map для юнит-тестов (без БД).
type fakeKeyring map[string]string

func (k fakeKeyring) Secret(kid string) (string, bool) {
	s, ok := k[kid]
	return s, ok
}

func devKeyring() Keyring { return fakeKeyring{canonKID: canonSecret} }

func TestSign_KnownVector(t *testing.T) {
	got := Sign(canonAID, canonV, canonKID, canonSecret)
	if got != canonSig {
		t.Fatalf("Sign = %q, want %q", got, canonSig)
	}
}

func TestSign_Length32(t *testing.T) {
	if got := Sign(canonAID, canonV, canonKID, canonSecret); len(got) != 32 {
		t.Fatalf("len(Sign) = %d, want 32", len(got))
	}
}

func TestSign_URLSafeNoPadding(t *testing.T) {
	got := Sign(canonAID, canonV, canonKID, canonSecret)
	for _, c := range got {
		if c == '=' || c == '+' || c == '/' {
			t.Fatalf("Sign содержит недопустимый для base64url символ %q: %q", c, got)
		}
	}
}

func TestVerify_ValidSignature(t *testing.T) {
	if err := Verify(canonAID, canonV, canonKID, canonSig, devKeyring()); err != nil {
		t.Fatalf("Verify(валидная подпись) = %v, want nil", err)
	}
}

func TestVerify_TamperedSignatureRejected(t *testing.T) {
	// Изменение одного символа в любой позиции → подпись отвергается.
	// Проверяем разные позиции — контракт требует сравнения всей строки
	// (константного), а не префикса.
	cases := map[string]string{
		"первый байт":    "X" + canonSig[1:],
		"средний байт":   canonSig[:16] + "X" + canonSig[17:],
		"последний байт": canonSig[:len(canonSig)-1] + "X",
	}
	for name, bad := range cases {
		t.Run(name, func(t *testing.T) {
			err := Verify(canonAID, canonV, canonKID, bad, devKeyring())
			if !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("Verify(искажён %s) = %v, want ErrInvalidSignature", name, err)
			}
		})
	}
}

func TestVerify_UnknownKID(t *testing.T) {
	err := Verify(canonAID, canonV, "no-such-kid", canonSig, devKeyring())
	if !errors.Is(err, ErrUnknownKID) {
		t.Fatalf("Verify(неизвестный kid) = %v, want ErrUnknownKID", err)
	}
}

func TestVerify_TamperedPayloadRejected(t *testing.T) {
	// Каноническая подпись, но подменены aid или v — HMAC не сойдётся.
	cases := map[string]struct{ aid, v, kid string }{
		"чужой aid": {"00000000-0000-0000-0000-000000000000", canonV, canonKID},
		"чужой v":   {canonAID, "2", canonKID},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := Verify(tc.aid, tc.v, tc.kid, canonSig, devKeyring())
			if !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("Verify(%s) = %v, want ErrInvalidSignature", name, err)
			}
		})
	}
}

func TestSignVerify_RoundTrip(t *testing.T) {
	sig := Sign(canonAID, canonV, canonKID, canonSecret)
	if err := Verify(canonAID, canonV, canonKID, sig, devKeyring()); err != nil {
		t.Fatalf("round-trip Verify = %v, want nil", err)
	}
}
