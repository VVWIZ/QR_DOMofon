package auth

import (
	"context"
	"testing"
	"time"
)

// fakeRefreshKV — map-реализация refreshKV (TTL для теста несущественен: время
// не двигаем, проверяем логику whitelist/ротации).
type fakeRefreshKV struct {
	m map[string]string
}

func newFakeRefreshKV() *fakeRefreshKV { return &fakeRefreshKV{m: map[string]string{}} }

func (f *fakeRefreshKV) Set(_ context.Context, key, val string, _ time.Duration) error {
	f.m[key] = val
	return nil
}

func (f *fakeRefreshKV) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.m[key]
	return v, ok, nil
}

func (f *fakeRefreshKV) Del(_ context.Context, key string) error {
	delete(f.m, key)
	return nil
}

const (
	testUserID = "77777777-7777-7777-7777-777777777777"
	jtiOld     = "aaaaaaaa-0000-0000-0000-000000000001"
	jtiNew     = "bbbbbbbb-0000-0000-0000-000000000002"
)

func TestRefresh_IssueThenValidate(t *testing.T) {
	w := NewRefreshWhitelist(newFakeRefreshKV())
	ctx := context.Background()

	if err := w.Issue(ctx, jtiOld, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Issue = %v", err)
	}
	uid, ok, err := w.Validate(ctx, jtiOld)
	if err != nil {
		t.Fatalf("Validate = %v", err)
	}
	if !ok || uid != testUserID {
		t.Fatalf("Validate = (%q, %v), want (%q, true)", uid, ok, testUserID)
	}
}

func TestRefresh_RotateInvalidatesOld(t *testing.T) {
	w := NewRefreshWhitelist(newFakeRefreshKV())
	ctx := context.Background()

	if err := w.Issue(ctx, jtiOld, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Issue = %v", err)
	}
	if err := w.Rotate(ctx, jtiOld, jtiNew, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Rotate = %v", err)
	}

	// Старый jti больше не валиден.
	if _, ok, _ := w.Validate(ctx, jtiOld); ok {
		t.Fatalf("Validate(старый jti после ротации) = ok, want невалиден")
	}
	// Новый jti валиден и указывает на того же пользователя.
	uid, ok, _ := w.Validate(ctx, jtiNew)
	if !ok || uid != testUserID {
		t.Fatalf("Validate(новый jti) = (%q, %v), want (%q, true)", uid, ok, testUserID)
	}
}

func TestRefresh_ReuseAfterRotationRejected(t *testing.T) {
	w := NewRefreshWhitelist(newFakeRefreshKV())
	ctx := context.Background()

	if err := w.Issue(ctx, jtiOld, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Issue = %v", err)
	}
	if err := w.Rotate(ctx, jtiOld, jtiNew, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Rotate = %v", err)
	}
	// Повторное использование украденного старого jti (его уже нет в whitelist)
	// → ошибка (детект reuse, auth.md §3).
	if err := w.Rotate(ctx, jtiOld, "cccccccc-0000-0000-0000-000000000003", testUserID, RefreshTTL); err == nil {
		t.Fatalf("Rotate(переиспользование старого jti) = nil, want ошибка")
	}
}

func TestRefresh_RevokeInvalidates(t *testing.T) {
	w := NewRefreshWhitelist(newFakeRefreshKV())
	ctx := context.Background()

	if err := w.Issue(ctx, jtiOld, testUserID, RefreshTTL); err != nil {
		t.Fatalf("Issue = %v", err)
	}
	if err := w.Revoke(ctx, jtiOld); err != nil {
		t.Fatalf("Revoke = %v", err)
	}
	if _, ok, _ := w.Validate(ctx, jtiOld); ok {
		t.Fatalf("Validate(после Revoke) = ok, want невалиден")
	}
}
