package onboarding

// Матрица доступа УК-консоли (инкремент D): по объекту — владельцы (строки) ×
// калитки/шлагбаумы объекта (столбцы) с грантами; раскрытие строки — жильцы
// квартиры. Тогл гранта — выдать/отозвать (идемпотентно, с аудитом). Всё скоупится
// по mc из claims. Вертикальный слайс: repo + service + handler в одном файле.

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"domofon/backend/internal/audit"
	"domofon/backend/internal/auth"
	"domofon/backend/internal/platform/httpx"
)

// --- типы ---

// SiteBrief — объект УК для выпадашки консоли.
type SiteBrief struct {
	ID      string
	Name    string
	Address string
	Kind    string
}

// MatrixPoint — калитка/шлагбаум объекта (столбец матрицы).
type MatrixPoint struct {
	PublicID string
	Label    string
	Type     string
}

// MatrixApartment — квартира владельца в объекте.
type MatrixApartment struct {
	ID     string
	Number string
}

// MatrixOwner — строка матрицы (владелец + его квартиры + гранты на точки объекта).
type MatrixOwner struct {
	UserID     string
	FullName   string
	Phone      string
	Apartments []MatrixApartment
	Grants     []string // public_id точек, на которые есть грант
}

// Matrix — матрица доступа объекта.
type Matrix struct {
	Points []MatrixPoint
	Owners []MatrixOwner
}

// ResidentEntry — жилец квартиры при раскрытии строки (+ его гранты на точки объекта).
type ResidentEntry struct {
	UserID   string
	FullName string
	Phone    string
	Grants   []string
}

// --- repo ---

// ListSites возвращает объекты УК mcID.
func (r *Repo) ListSites(ctx context.Context, mcID string) ([]SiteBrief, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, name, address, kind FROM sites WHERE management_company_id = $1 ORDER BY name`, mcID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: list sites: %w", err)
	}
	defer rows.Close()
	var out []SiteBrief
	for rows.Next() {
		var s SiteBrief
		if err := rows.Scan(&s.ID, &s.Name, &s.Address, &s.Kind); err != nil {
			return nil, fmt.Errorf("onboarding: scan site: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SiteInMC сообщает, принадлежит ли объект УК (скоуп-гард).
func (r *Repo) SiteInMC(ctx context.Context, siteID, mcID string) (bool, error) {
	if _, err := uuid.Parse(siteID); err != nil {
		return false, nil
	}
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM sites WHERE id = $1 AND management_company_id = $2)`, siteID, mcID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("onboarding: site in mc: %w", err)
	}
	return ok, nil
}

// SiteMatrix собирает матрицу объекта: точки (столбцы), владельцы (строки) с
// квартирами и грантами. Сортировка владельцев — последний добавленный сверху.
func (r *Repo) SiteMatrix(ctx context.Context, siteID, mcID string) (Matrix, error) {
	var m Matrix

	// Точки объекта (столбцы).
	prows, err := r.pool.Query(ctx, `
		SELECT public_id::text, label, type
		FROM access_points
		WHERE site_id = $1 AND management_company_id = $2 AND type IN ('gate','barrier') AND is_active = true
		ORDER BY type, label`, siteID, mcID)
	if err != nil {
		return Matrix{}, fmt.Errorf("onboarding: matrix points: %w", err)
	}
	for prows.Next() {
		var p MatrixPoint
		if err := prows.Scan(&p.PublicID, &p.Label, &p.Type); err != nil {
			prows.Close()
			return Matrix{}, fmt.Errorf("onboarding: scan matrix point: %w", err)
		}
		m.Points = append(m.Points, p)
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return Matrix{}, err
	}

	// Владельцы объекта + их квартиры (сортировка: последний добавленный сверху).
	orows, err := r.pool.Query(ctx, `
		SELECT u.id::text, COALESCE(u.full_name,''), COALESCE(u.phone,''), a.id::text, a.number, r.created_at
		FROM user_apartment_roles r
		JOIN apartments a ON a.id = r.apartment_id
		JOIN buildings b ON b.id = a.building_id AND b.site_id = $1
		JOIN users u ON u.id = r.user_id
		WHERE r.role = 'owner' AND r.management_company_id = $2
		ORDER BY r.created_at DESC, u.id, a.number`, siteID, mcID)
	if err != nil {
		return Matrix{}, fmt.Errorf("onboarding: matrix owners: %w", err)
	}
	idx := map[string]int{}
	for orows.Next() {
		var uid, fn, ph, aid, anum string
		var created any
		if err := orows.Scan(&uid, &fn, &ph, &aid, &anum, &created); err != nil {
			orows.Close()
			return Matrix{}, fmt.Errorf("onboarding: scan matrix owner: %w", err)
		}
		i, ok := idx[uid]
		if !ok {
			i = len(m.Owners)
			idx[uid] = i
			m.Owners = append(m.Owners, MatrixOwner{UserID: uid, FullName: fn, Phone: ph})
		}
		m.Owners[i].Apartments = append(m.Owners[i].Apartments, MatrixApartment{ID: aid, Number: anum})
	}
	orows.Close()
	if err := orows.Err(); err != nil {
		return Matrix{}, err
	}

	// Гранты владельцев на точки объекта.
	grants, err := r.grantsBySite(ctx, siteID, mcID)
	if err != nil {
		return Matrix{}, err
	}
	for i := range m.Owners {
		m.Owners[i].Grants = grants[m.Owners[i].UserID]
	}
	return m, nil
}

// grantsBySite → map[userID][]publicID грантов на gate/barrier точки объекта.
func (r *Repo) grantsBySite(ctx context.Context, siteID, mcID string) (map[string][]string, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT g.user_id::text, ap.public_id::text
		FROM user_access_grants g
		JOIN access_points ap ON ap.id = g.access_point_id AND ap.site_id = $1 AND ap.type IN ('gate','barrier')
		WHERE g.management_company_id = $2`, siteID, mcID)
	if err != nil {
		return nil, fmt.Errorf("onboarding: grants by site: %w", err)
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var uid, pub string
		if err := rows.Scan(&uid, &pub); err != nil {
			return nil, fmt.Errorf("onboarding: scan grant: %w", err)
		}
		out[uid] = append(out[uid], pub)
	}
	return out, rows.Err()
}

// ApartmentResidents возвращает жильцов квартиры (role='resident') + их гранты на
// точки объекта квартиры. Скоуп по mc. ok=false → квартира не в этой УК.
func (r *Repo) ApartmentResidents(ctx context.Context, apartmentID, mcID string) ([]ResidentEntry, bool, error) {
	if _, err := uuid.Parse(apartmentID); err != nil {
		return nil, false, nil
	}
	// Объект квартиры (для грантов) + скоуп-гард.
	var siteID string
	err := r.pool.QueryRow(ctx, `
		SELECT b.site_id::text
		FROM apartments a JOIN buildings b ON b.id = a.building_id
		WHERE a.id = $1 AND a.management_company_id = $2`, apartmentID, mcID).Scan(&siteID)
	if err == pgx.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("onboarding: apartment site: %w", err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT u.id::text, COALESCE(u.full_name,''), COALESCE(u.phone,'')
		FROM user_apartment_roles r JOIN users u ON u.id = r.user_id
		WHERE r.apartment_id = $1 AND r.role = 'resident' AND r.management_company_id = $2
		ORDER BY u.full_name, u.id`, apartmentID, mcID)
	if err != nil {
		return nil, false, fmt.Errorf("onboarding: apartment residents: %w", err)
	}
	defer rows.Close()
	var out []ResidentEntry
	ids := map[string]int{}
	for rows.Next() {
		var e ResidentEntry
		if err := rows.Scan(&e.UserID, &e.FullName, &e.Phone); err != nil {
			return nil, false, fmt.Errorf("onboarding: scan resident: %w", err)
		}
		ids[e.UserID] = len(out)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	// Гранты жильцов на точки объекта.
	grants, err := r.grantsBySite(ctx, siteID, mcID)
	if err != nil {
		return nil, false, err
	}
	for i := range out {
		out[i].Grants = grants[out[i].UserID]
	}
	return out, true, nil
}

// resolvePointInMC резолвит gate/barrier точку по public_id в рамках УК. ok=false →
// нет/чужая/не gate-barrier.
func (r *Repo) resolvePointInMC(ctx context.Context, publicID, mcID string) (string, string, bool, error) {
	if _, err := uuid.Parse(publicID); err != nil {
		return "", "", false, nil
	}
	var apID, siteMC string
	var typ string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, management_company_id::text, type
		FROM access_points WHERE public_id = $1 AND is_active = true`, publicID).Scan(&apID, &siteMC, &typ)
	if err == pgx.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, fmt.Errorf("onboarding: resolve point: %w", err)
	}
	if siteMC != mcID || (typ != "gate" && typ != "barrier") {
		return "", "", false, nil
	}
	return apID, typ, true, nil
}

// userInMC сообщает, связан ли пользователь с УК (роль или грант).
func (r *Repo) userInMC(ctx context.Context, userID, mcID string) (bool, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return false, nil
	}
	var ok bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_apartment_roles WHERE user_id=$1 AND management_company_id=$2)
		    OR EXISTS (SELECT 1 FROM user_access_grants WHERE user_id=$1 AND management_company_id=$2)`,
		userID, mcID).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("onboarding: user in mc: %w", err)
	}
	return ok, nil
}

// grantReturning выдаёт грант, возвращая created (реально ли вставлено).
func (r *Repo) grantReturning(ctx context.Context, userID, apID, mcID, grantedBy string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO user_access_grants (id, user_id, access_point_id, management_company_id, granted_by)
		VALUES ($1,$2,$3,$4,$5) ON CONFLICT (user_id, access_point_id) DO NOTHING`,
		uuid.NewString(), userID, apID, mcID, grantedBy)
	if err != nil {
		return false, fmt.Errorf("onboarding: grant returning: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// revokeGrant отзывает грант (скоуп по mc). removed=false → гранта не было.
func (r *Repo) revokeGrant(ctx context.Context, userID, apID, mcID string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM user_access_grants WHERE user_id=$1 AND access_point_id=$2 AND management_company_id=$3`,
		userID, apID, mcID)
	if err != nil {
		return false, fmt.Errorf("onboarding: revoke grant: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// --- service ---

// ListSites (УК-админ) — объекты своей УК.
func (s *Service) ListSites(ctx context.Context, claims auth.Claims) ([]SiteBrief, *httpx.Error) {
	out, err := s.repo.ListSites(ctx, claims.MCID)
	if err != nil {
		return nil, s.internal("list_sites_failed", err)
	}
	return out, nil
}

// SiteMatrix (УК-админ) — матрица объекта своей УК.
func (s *Service) SiteMatrix(ctx context.Context, claims auth.Claims, siteID string) (Matrix, *httpx.Error) {
	ok, err := s.repo.SiteInMC(ctx, siteID, claims.MCID)
	if err != nil {
		return Matrix{}, s.internal("site_in_mc_failed", err)
	}
	if !ok {
		return Matrix{}, httpx.NewError(httpx.CodeValidationError, "Site not found")
	}
	m, err := s.repo.SiteMatrix(ctx, siteID, claims.MCID)
	if err != nil {
		return Matrix{}, s.internal("site_matrix_failed", err)
	}
	return m, nil
}

// ApartmentResidents (УК-админ) — жильцы квартиры своей УК (раскрытие строки).
func (s *Service) ApartmentResidents(ctx context.Context, claims auth.Claims, apartmentID string) ([]ResidentEntry, *httpx.Error) {
	out, ok, err := s.repo.ApartmentResidents(ctx, apartmentID, claims.MCID)
	if err != nil {
		return nil, s.internal("apartment_residents_failed", err)
	}
	if !ok {
		return nil, httpx.NewError(httpx.CodeValidationError, "Apartment not found")
	}
	return out, nil
}

// GrantPoint (УК-админ) — выдать пользователю грант на точку (тогл матрицы).
func (s *Service) GrantPoint(ctx context.Context, claims auth.Claims, userID, publicID string) *httpx.Error {
	apID, _, ok, err := s.repo.resolvePointInMC(ctx, publicID, claims.MCID)
	if err != nil {
		return s.internal("resolve_point_failed", err)
	}
	if !ok {
		return httpx.NewError(httpx.CodeValidationError, "Access point not found")
	}
	inMC, err := s.repo.userInMC(ctx, userID, claims.MCID)
	if err != nil {
		return s.internal("user_in_mc_failed", err)
	}
	if !inMC {
		return httpx.NewError(httpx.CodeForbidden, "User does not belong to this management company")
	}
	created, err := s.repo.grantReturning(ctx, userID, apID, claims.MCID, claims.Subject)
	if err != nil {
		return s.internal("grant_returning_failed", err)
	}
	if created {
		s.record(ctx, audit.Event{EventType: "access_grant_created", Actor: "user:" + claims.Subject, AccessPointID: apID, ManagementCompanyID: claims.MCID, Metadata: map[string]any{"granted_to": userID, "via": "matrix"}})
	}
	return nil
}

// RevokePoint (УК-админ) — отозвать грант (тогл матрицы). Идемпотентно.
func (s *Service) RevokePoint(ctx context.Context, claims auth.Claims, userID, publicID string) *httpx.Error {
	apID, _, ok, err := s.repo.resolvePointInMC(ctx, publicID, claims.MCID)
	if err != nil {
		return s.internal("resolve_point_failed", err)
	}
	if !ok {
		return httpx.NewError(httpx.CodeValidationError, "Access point not found")
	}
	removed, err := s.repo.revokeGrant(ctx, userID, apID, claims.MCID)
	if err != nil {
		return s.internal("revoke_grant_failed", err)
	}
	if removed {
		s.record(ctx, audit.Event{EventType: "access_grant_revoked", Actor: "user:" + claims.Subject, AccessPointID: apID, ManagementCompanyID: claims.MCID, Metadata: map[string]any{"revoked_from": userID, "via": "matrix"}})
	}
	return nil
}

// --- handlers ---

// ListSites — GET /api/v1/admin/sites.
func (h *Handler) ListSites(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	rows, apiErr := h.svc.ListSites(r.Context(), claims)
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, s := range rows {
		out = append(out, map[string]any{"id": s.ID, "name": s.Name, "address": s.Address, "kind": s.Kind})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"sites": out})
}

// SiteMatrix — GET /api/v1/admin/sites/{site_id}/matrix.
func (h *Handler) SiteMatrix(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	m, apiErr := h.svc.SiteMatrix(r.Context(), claims, chi.URLParam(r, "site_id"))
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	points := make([]map[string]any, 0, len(m.Points))
	for _, p := range m.Points {
		points = append(points, map[string]any{"public_id": p.PublicID, "label": p.Label, "type": p.Type})
	}
	owners := make([]map[string]any, 0, len(m.Owners))
	for _, o := range m.Owners {
		apts := make([]map[string]any, 0, len(o.Apartments))
		for _, a := range o.Apartments {
			apts = append(apts, map[string]any{"id": a.ID, "number": a.Number})
		}
		owners = append(owners, map[string]any{
			"user_id": o.UserID, "full_name": o.FullName, "phone": o.Phone,
			"apartments": apts, "grants": nonNil(o.Grants),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"points": points, "owners": owners})
}

// ApartmentResidents — GET /api/v1/admin/apartments/{apartment_id}/residents.
func (h *Handler) ApartmentResidents(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	rows, apiErr := h.svc.ApartmentResidents(r.Context(), claims, chi.URLParam(r, "apartment_id"))
	if apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, e := range rows {
		out = append(out, map[string]any{"user_id": e.UserID, "full_name": e.FullName, "phone": e.Phone, "grants": nonNil(e.Grants)})
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"residents": out})
}

// GrantPoint — PUT /api/v1/admin/users/{user_id}/grants/{point_public_id}.
func (h *Handler) GrantPoint(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	if apiErr := h.svc.GrantPoint(r.Context(), claims, chi.URLParam(r, "user_id"), chi.URLParam(r, "point_public_id")); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"granted": true})
}

// RevokePoint — DELETE /api/v1/admin/users/{user_id}/grants/{point_public_id}.
func (h *Handler) RevokePoint(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.claims(w, r)
	if !ok {
		return
	}
	if apiErr := h.svc.RevokePoint(r.Context(), claims, chi.URLParam(r, "user_id"), chi.URLParam(r, "point_public_id")); apiErr != nil {
		httpx.WriteErr(w, r, apiErr)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"granted": false})
}

// nonNil заменяет nil-срез пустым (стабильный JSON []).
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
