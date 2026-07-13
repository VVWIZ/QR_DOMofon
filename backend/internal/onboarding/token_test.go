package onboarding

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"testing"
)

// base64url без паддинга: [A-Za-z0-9_-], без '='.
var reBase64URL = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestGenerateToken_ProducesDistinctSecrets(t *testing.T) {
	a, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken #1: неожиданная ошибка: %v", err)
	}
	b, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken #2: неожиданная ошибка: %v", err)
	}
	if a == b {
		t.Fatalf("два вызова GenerateToken дали одинаковый секрет %q — нет энтропии", a)
	}
}

func TestGenerateToken_FormatBase64URLNoPadding(t *testing.T) {
	raw, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: неожиданная ошибка: %v", err)
	}
	// 32 байта в base64url без паддинга → 43 символа.
	if got := len(raw); got != 43 {
		t.Errorf("len(raw) = %d, want 43 (32 байта base64url без паддинга)", got)
	}
	if !reBase64URL.MatchString(raw) {
		t.Errorf("raw = %q содержит символы вне base64url-алфавита или паддинг '='", raw)
	}
	// Декодируется как raw-url и даёт ровно 32 байта.
	dec, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("raw %q не декодируется как base64url без паддинга: %v", raw, err)
	}
	if len(dec) != 32 {
		t.Errorf("декодировано %d байт, want 32", len(dec))
	}
}

func TestHashToken_DeterministicForSameRaw(t *testing.T) {
	const raw = "example-invite-secret-abc123"
	if HashToken(raw) != HashToken(raw) {
		t.Fatalf("HashToken недетерминирован: два вызова на одном raw дали разные хеши")
	}
}

func TestHashToken_MatchesSHA256Hex(t *testing.T) {
	const raw = "example-invite-secret-abc123"
	sum := sha256.Sum256([]byte(raw))
	want := hex.EncodeToString(sum[:])
	got := HashToken(raw)
	if got != want {
		t.Errorf("HashToken(%q) = %q, want %q (SHA-256 hex)", raw, got, want)
	}
	if len(got) != 64 {
		t.Errorf("len(HashToken) = %d, want 64 (SHA-256 в hex)", len(got))
	}
}

func TestHashToken_NotEqualRaw(t *testing.T) {
	const raw = "example-invite-secret-abc123"
	if HashToken(raw) == raw {
		t.Fatalf("HashToken(raw) == raw — секрет не захеширован")
	}
}
