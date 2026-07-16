package schedules

// Прикладная логика управления расписаниями (УК-админ). Скоуп по mc из claims;
// расписания — только на gate/barrier точки своей УК.

import (
	"context"
	"log/slog"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// Service — доменная логика расписаний.
type Service struct {
	repo  *Repo
	audit audit.Recorder
	log   *slog.Logger
}

// NewService собирает сервис.
func NewService(repo *Repo, recorder audit.Recorder, log *slog.Logger) *Service {
	return &Service{repo: repo, audit: recorder, log: log}
}

// ListPoints — gate/barrier точки своей УК + их окна.
func (s *Service) ListPoints(ctx context.Context, claims auth.Claims) ([]PointSchedules, *httpx.Error) {
	out, err := s.repo.ListPoints(ctx, claims.MCID)
	if err != nil {
		return nil, s.internal("list_points_failed", err)
	}
	return out, nil
}

// Create создаёт окно авто-открытия на точке своей УК.
func (s *Service) Create(ctx context.Context, claims auth.Claims, publicID string, sched Schedule) (string, *httpx.Error) {
	if apiErr := ValidateSchedule(sched); apiErr != nil {
		return "", apiErr
	}
	apID, ok, err := s.repo.ResolvePoint(ctx, publicID, claims.MCID)
	if err != nil {
		return "", s.internal("resolve_point_failed", err)
	}
	if !ok {
		return "", httpx.NewError(httpx.CodeValidationError, "Access point not found")
	}
	id, err := s.repo.Create(ctx, apID, claims.MCID, sched, claims.Subject)
	if err != nil {
		return "", s.internal("create_schedule_failed", err)
	}
	s.record(ctx, audit.Event{EventType: "schedule_created", Actor: "user:" + claims.Subject, AccessPointID: apID, ManagementCompanyID: claims.MCID, Metadata: map[string]any{"dow": sched.Dow, "opens": sched.Opens, "closes": sched.Closes, "tz": sched.Timezone}})
	return id, nil
}

// Delete удаляет окно (скоуп по mc). Не найдено → VALIDATION_ERROR.
func (s *Service) Delete(ctx context.Context, claims auth.Claims, id string) *httpx.Error {
	removed, err := s.repo.Delete(ctx, id, claims.MCID)
	if err != nil {
		return s.internal("delete_schedule_failed", err)
	}
	if !removed {
		return httpx.NewError(httpx.CodeValidationError, "Schedule not found")
	}
	s.record(ctx, audit.Event{EventType: "schedule_deleted", Actor: "user:" + claims.Subject, ManagementCompanyID: claims.MCID, Metadata: map[string]any{"schedule_id": id}})
	return nil
}

func (s *Service) record(ctx context.Context, ev audit.Event) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Record(ctx, ev); err != nil {
		s.log.Error("audit_record_failed", "error", err, "event_type", ev.EventType)
	}
}

func (s *Service) internal(msg string, err error) *httpx.Error {
	s.log.Error(msg, "error", err)
	return httpx.NewError(httpx.CodeInternal, "Internal server error")
}
