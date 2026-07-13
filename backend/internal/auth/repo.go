package auth

// pgx-репозиторий пользователей и их квартирных ролей (auth.md §2). Только
// параметризованные SELECT; секреты (password_hash/totp_secret) читаются, но
// наружу из сервиса не логируются.

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrUserNotFound — пользователь не найден (неизвестный телефон/email).
var ErrUserNotFound = errors.New("auth: user not found")

// User — строка users (auth.md §2). Nullable-поля представлены пустой строкой.
type User struct {
	ID           string
	Phone        string
	Email        string
	PasswordHash string
	TOTPSecret   string
	Kind         Kind
	MCID         string // management_company_id ("" → NULL)
}

// RoleRow — привязка пользователя к квартире (user_apartment_roles).
type RoleRow struct {
	ApartmentID     string
	Role            string
	CanCreateGuests bool
	MCID            string
}

// Repo — доступ к users/user_apartment_roles поверх pgxpool.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo создаёт репозиторий auth.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// GetUserByPhone возвращает жильца/владельца по телефону (для OTP-логина).
// Не найден → ErrUserNotFound.
func (r *Repo) GetUserByPhone(ctx context.Context, phone string) (User, error) {
	const q = `
		SELECT id, kind, COALESCE(management_company_id::text, '')
		FROM users
		WHERE phone = $1`
	var u User
	err := r.pool.QueryRow(ctx, q, phone).Scan(&u.ID, &u.Kind, &u.MCID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by phone: %w", err)
	}
	u.Phone = phone
	return u, nil
}

// GetUserByID возвращает пользователя по id: id, phone, email, kind, mc_id (для
// выдачи токенов при приёме инвайта — вход без OTP). Не найден → ErrUserNotFound.
func (r *Repo) GetUserByID(ctx context.Context, id string) (User, error) {
	const q = `
		SELECT id, COALESCE(phone, ''), COALESCE(email, ''), kind,
		       COALESCE(management_company_id::text, '')
		FROM users
		WHERE id = $1`
	var u User
	err := r.pool.QueryRow(ctx, q, id).Scan(&u.ID, &u.Phone, &u.Email, &u.Kind, &u.MCID)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get user by id: %w", err)
	}
	return u, nil
}

// GetAdminByEmail возвращает УК-админа по email (для admin-логина): id, email,
// bcrypt-хеш пароля, TOTP-секрет, mc_id, kind. Не найден → ErrUserNotFound.
func (r *Repo) GetAdminByEmail(ctx context.Context, email string) (User, error) {
	const q = `
		SELECT id, email, COALESCE(password_hash, ''), COALESCE(totp_secret, ''),
		       COALESCE(management_company_id::text, ''), kind
		FROM users
		WHERE email = $1`
	var u User
	err := r.pool.QueryRow(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.TOTPSecret, &u.MCID, &u.Kind,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("auth: get admin by email: %w", err)
	}
	return u, nil
}

// GetRolesByUserID возвращает квартирные роли пользователя (claim "roles").
// Пустой срез — валиден (напр. у mc_admin ролей нет).
func (r *Repo) GetRolesByUserID(ctx context.Context, userID string) ([]RoleRow, error) {
	const q = `
		SELECT apartment_id::text, role, can_create_guests, management_company_id::text
		FROM user_apartment_roles
		WHERE user_id = $1
		ORDER BY apartment_id`
	rows, err := r.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: get roles: %w", err)
	}
	defer rows.Close()

	var out []RoleRow
	for rows.Next() {
		var rr RoleRow
		if err := rows.Scan(&rr.ApartmentID, &rr.Role, &rr.CanCreateGuests, &rr.MCID); err != nil {
			return nil, fmt.Errorf("auth: scan role: %w", err)
		}
		out = append(out, rr)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("auth: rows: %w", err)
	}
	return out, nil
}
