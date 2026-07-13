package access

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/platform/httpx"
)

// --- фейки потребительских интерфейсов access ---

type fakeResolver struct {
	gp  GrantedPoint
	ok  bool
	err error
}

func (f fakeResolver) ResolveGrantedPoint(ctx context.Context, userID, publicID string) (GrantedPoint, bool, error) {
	return f.gp, f.ok, f.err
}

type fakePresence struct {
	online bool
	err    error
}

func (f fakePresence) IsOnline(ctx context.Context, deviceID string) (bool, error) {
	return f.online, f.err
}

type fakePublisher struct {
	called bool
	err    error
}

func (f *fakePublisher) PublishOpenRelay(ctx context.Context, deviceID string, cmd OpenRelayCommand) error {
	f.called = true
	return f.err
}

type fakeCmdCtx struct{}

func (fakeCmdCtx) Save(ctx context.Context, requestID string, meta map[string]string) error {
	return nil
}

type fakeAudit struct{}

func (fakeAudit) Record(ctx context.Context, ev audit.Event) error { return nil }

func grantedPoint() GrantedPoint {
	return GrantedPoint{
		DeviceID:            "dddddddd-dddd-dddd-dddd-dddddddddddd",
		AccessPointID:       "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa",
		ApartmentID:         "33333333-3333-3333-3333-333333333333",
		ManagementCompanyID: "11111111-1111-1111-1111-111111111111",
	}
}

// newOpenPointService собирает Service для OpenPoint: calls/authz не нужны
// (nil), presence/publisher/резолвер инъектируются.
func newOpenPointService(resolver PointResolver, presence PresenceChecker, pub CommandPublisher) *Service {
	svc := NewService(nil, presence, pub, fakeCmdCtx{}, nil, fakeAudit{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.SetPointResolver(resolver)
	return svc
}

func TestOpenPoint_GrantOnlinePublishes(t *testing.T) {
	pub := &fakePublisher{}
	svc := newOpenPointService(
		fakeResolver{gp: grantedPoint(), ok: true},
		fakePresence{online: true},
		pub,
	)

	res, herr := svc.OpenPoint(context.Background(), "user-1", "pub-1")
	if herr != nil {
		t.Fatalf("OpenPoint(грант+online) = ошибка %+v, want nil", herr)
	}
	if !pub.called {
		t.Errorf("publish не вызван, want вызван")
	}
	if res.RequestID == "" {
		t.Errorf("request_id пуст, want непустой")
	}
	if res.Status != "sent" {
		t.Errorf("status = %q, want \"sent\"", res.Status)
	}
}

func TestOpenPoint_NoGrantForbidden(t *testing.T) {
	pub := &fakePublisher{}
	svc := newOpenPointService(
		fakeResolver{ok: false},
		fakePresence{online: true},
		pub,
	)

	_, herr := svc.OpenPoint(context.Background(), "user-1", "pub-1")
	if herr == nil {
		t.Fatalf("OpenPoint(нет гранта) = nil, want FORBIDDEN")
	}
	if herr.Code != httpx.CodeForbidden {
		t.Errorf("code = %q, want %q", herr.Code, httpx.CodeForbidden)
	}
	if pub.called {
		t.Errorf("publish вызван при отсутствии гранта, want НЕ вызван")
	}
}

func TestOpenPoint_DeviceOffline(t *testing.T) {
	pub := &fakePublisher{}
	svc := newOpenPointService(
		fakeResolver{gp: grantedPoint(), ok: true},
		fakePresence{online: false},
		pub,
	)

	_, herr := svc.OpenPoint(context.Background(), "user-1", "pub-1")
	if herr == nil {
		t.Fatalf("OpenPoint(device offline) = nil, want DEVICE_OFFLINE")
	}
	if herr.Code != httpx.CodeDeviceOffline {
		t.Errorf("code = %q, want %q", herr.Code, httpx.CodeDeviceOffline)
	}
	if pub.called {
		t.Errorf("publish вызван при offline-устройстве, want НЕ вызван")
	}
}
