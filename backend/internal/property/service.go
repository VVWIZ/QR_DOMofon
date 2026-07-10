package property

import "context"

// Service — доменный фасад property поверх Repo. Тонкий по замыслу: вся логика
// разрешения — в SQL репозитория; сервис фиксирует границу модуля и точку
// внедрения для потребителей (qr, calls) через адаптеры в cmd/server.
type Service struct {
	repo *Repo
}

// NewService создаёт сервис property.
func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

// ResolveByPublicID разрешает контекст точки доступа по public_id (ErrNotFound —
// не найдена/неактивна).
func (s *Service) ResolveByPublicID(ctx context.Context, publicID string) (Context, error) {
	return s.repo.ResolveByPublicID(ctx, publicID)
}
