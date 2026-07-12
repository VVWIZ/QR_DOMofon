package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"domofon/backend/internal/platform/httpx"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeOtpStore — map-реализация OtpStore для юнит-тестов (TTL/окна трактуются
// относительно переданного now, без реального времени и Redis).
type fakeOtpStore struct {
	reqCount   map[string]int
	reqExp     map[string]time.Time
	otp        map[string]OtpRecord
	blockUntil map[string]time.Time
}

func newFakeOtpStore() *fakeOtpStore {
	return &fakeOtpStore{
		reqCount:   map[string]int{},
		reqExp:     map[string]time.Time{},
		otp:        map[string]OtpRecord{},
		blockUntil: map[string]time.Time{},
	}
}

func (f *fakeOtpStore) IncrRequests(_ context.Context, phone string, window time.Duration, now time.Time) (int, error) {
	if exp, ok := f.reqExp[phone]; !ok || !exp.After(now) {
		f.reqCount[phone] = 0
		f.reqExp[phone] = now.Add(window)
	}
	f.reqCount[phone]++
	return f.reqCount[phone], nil
}

func (f *fakeOtpStore) GetOTP(_ context.Context, phone string, now time.Time) (OtpRecord, bool, error) {
	rec, ok := f.otp[phone]
	if !ok || !rec.ExpiresAt.After(now) {
		return OtpRecord{}, false, nil
	}
	return rec, true, nil
}

func (f *fakeOtpStore) SetOTP(_ context.Context, phone string, rec OtpRecord) error {
	f.otp[phone] = rec
	return nil
}

func (f *fakeOtpStore) DelOTP(_ context.Context, phone string) error {
	delete(f.otp, phone)
	return nil
}

func (f *fakeOtpStore) IsBlocked(_ context.Context, phone string, now time.Time) (bool, error) {
	until, ok := f.blockUntil[phone]
	return ok && until.After(now), nil
}

func (f *fakeOtpStore) Block(_ context.Context, phone string, until time.Time) error {
	f.blockUntil[phone] = until
	return nil
}

// fakeSender возвращает код как devCode (как DevSender) — детерминированно.
type fakeSender struct{ lastCode string }

func (s *fakeSender) Send(_ context.Context, _ string, code string) (string, error) {
	s.lastCode = code
	return code, nil
}

const testPhone = "+77010000001"

// diffCode подбирает 6-значный код, гарантированно не равный given.
func diffCode(given string) string {
	if given != "000000" {
		return "000000"
	}
	return "111111"
}

func TestOtpSend_ThrottleAfterThreeRequests(t *testing.T) {
	s := NewOtpService(newFakeOtpStore(), &fakeSender{}, testLogger())
	ctx := context.Background()
	now := fixedTOTPTime

	for i := 1; i <= OtpMaxRequests; i++ {
		if _, err := s.Send(ctx, testPhone, now); err != nil {
			t.Fatalf("Send #%d = %v, want ok", i, err)
		}
	}
	_, err := s.Send(ctx, testPhone, now)
	if err == nil || err.Code != httpx.CodeRateLimit {
		t.Fatalf("Send #%d = %v, want RATE_LIMIT", OtpMaxRequests+1, err)
	}
}

func TestOtpVerify_BlockAfterMaxWrongAttempts(t *testing.T) {
	s := NewOtpService(newFakeOtpStore(), &fakeSender{}, testLogger())
	ctx := context.Background()
	now := fixedTOTPTime

	res, err := s.Send(ctx, testPhone, now)
	if err != nil {
		t.Fatalf("Send = %v", err)
	}
	wrong := diffCode(res.DevCode)

	for i := 1; i <= OtpMaxAttempts; i++ {
		verr := s.Verify(ctx, testPhone, wrong, now)
		if verr == nil {
			t.Fatalf("Verify(неверный) #%d = nil, want ошибка", i)
		}
	}

	// После OtpMaxAttempts неверных попыток телефон заблокирован: и send, и
	// verify (даже с верным кодом) отдают RATE_LIMIT.
	if _, serr := s.Send(ctx, testPhone, now); serr == nil || serr.Code != httpx.CodeRateLimit {
		t.Fatalf("Send после блокировки = %v, want RATE_LIMIT", serr)
	}
	if verr := s.Verify(ctx, testPhone, res.DevCode, now); verr == nil || verr.Code != httpx.CodeRateLimit {
		t.Fatalf("Verify после блокировки = %v, want RATE_LIMIT", verr)
	}
}

func TestOtpVerify_CorrectWithinTTL(t *testing.T) {
	s := NewOtpService(newFakeOtpStore(), &fakeSender{}, testLogger())
	ctx := context.Background()
	now := fixedTOTPTime

	res, err := s.Send(ctx, testPhone, now)
	if err != nil {
		t.Fatalf("Send = %v", err)
	}
	wrong := diffCode(res.DevCode)

	// Пара неверных попыток не мешает последующему верному коду в пределах TTL.
	_ = s.Verify(ctx, testPhone, wrong, now)
	_ = s.Verify(ctx, testPhone, wrong, now)

	if verr := s.Verify(ctx, testPhone, res.DevCode, now.Add(OtpTTL-time.Minute)); verr != nil {
		t.Fatalf("Verify(верный код в пределах TTL) = %v, want nil", verr)
	}
}

func TestOtpSend_ResetsAttempts(t *testing.T) {
	s := NewOtpService(newFakeOtpStore(), &fakeSender{}, testLogger())
	ctx := context.Background()
	now := fixedTOTPTime

	res1, err := s.Send(ctx, testPhone, now)
	if err != nil {
		t.Fatalf("Send #1 = %v", err)
	}
	wrong := diffCode(res1.DevCode)

	// 4 неверных (< OtpMaxAttempts) — блокировки ещё нет.
	for i := 0; i < OtpMaxAttempts-1; i++ {
		if verr := s.Verify(ctx, testPhone, wrong, now); verr != nil && verr.Code == httpx.CodeRateLimit {
			t.Fatalf("преждевременная блокировка на попытке %d", i+1)
		}
	}

	// Новый Send сбрасывает счётчик попыток (attempts=0 в новой записи).
	res2, err := s.Send(ctx, testPhone, now)
	if err != nil {
		t.Fatalf("Send #2 = %v", err)
	}
	wrong2 := diffCode(res2.DevCode)

	// Ещё 4 неверных — блокировки быть не должно, раз счётчик сброшен.
	for i := 0; i < OtpMaxAttempts-1; i++ {
		if verr := s.Verify(ctx, testPhone, wrong2, now); verr != nil && verr.Code == httpx.CodeRateLimit {
			t.Fatalf("блокировка на попытке %d после сброса счётчика Send'ом", i+1)
		}
	}
}

func TestDevSender_ReturnsCode(t *testing.T) {
	d := NewDevSender(testLogger())
	const code = "123456"
	dev, err := d.Send(context.Background(), testPhone, code)
	if err != nil {
		t.Fatalf("DevSender.Send = %v", err)
	}
	if dev != code {
		t.Fatalf("DevSender.Send devCode = %q, want %q", dev, code)
	}
}
