// Package auth — аутентификация и RBAC (auth.md, api.md "Аутентификация").
//
// Скоуп инкремента: жилец/владелец (телефон → SMS OTP → JWT) и УК-админ
// (email+пароль+TOTP) + RBAC-энфорсмент. Пакет содержит детерминированную
// доменную логику: подпись/валидация JWT RS256, bcrypt-пароли, TOTP, политика
// OTP-лимитов и whitelist refresh-токенов (последние два — поверх инъектируемых
// Redis-подобных интерфейсов, чтобы юнит-тесты шли без реального Redis).
//
// ВНИМАНИЕ: на этапе QA тела функций — скелет (panic "not implemented"). Этап
// backend заменяет panic на реализацию под зафиксированный здесь контракт
// (сигнатуры + тесты). Сигнатуры менять нельзя.
package auth

import (
	"crypto/rsa"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// keep jwt/v5 direct-зависимостью, пока Sign/Parse — скелет (см. go.mod).
var _ = jwt.SigningMethodRS256

// Kind — тип пользователя (значение claim "kind", auth.md §1).
type Kind string

const (
	KindResident Kind = "resident"
	KindOwner    Kind = "owner"
	KindAdmin    Kind = "mc_admin"
)

// TokenType — значение claim "typ": различает access и refresh (auth.md §3).
type TokenType string

const (
	TypeAccess  TokenType = "access"
	TypeRefresh TokenType = "refresh"
)

// AccessTTL / RefreshTTL — время жизни токенов (auth.md §3).
const (
	AccessTTL  = 15 * time.Minute
	RefreshTTL = 30 * 24 * time.Hour
)

// ApartmentRole — привязка пользователя к квартире (элемент claim "roles").
type ApartmentRole struct {
	ApartmentID     string `json:"apartment_id"`
	Role            string `json:"role"` // owner | resident
	CanCreateGuests bool   `json:"can_create_guests"`
}

// Claims — полезная нагрузка JWT (auth.md §3, api.md "Claims access-токена").
// У mc_admin: Roles пуст, MCID = UUID УК. У жильца: MCID = "" (в JSON — null).
type Claims struct {
	Subject   string          `json:"sub"`
	Kind      Kind            `json:"kind"`
	Roles     []ApartmentRole `json:"roles"`
	MCID      string          `json:"mc_id"`
	JTI       string          `json:"jti"`
	IssuedAt  int64           `json:"iat"`
	ExpiresAt int64           `json:"exp"`
	Type      TokenType       `json:"typ"`
}

// Verifier валидирует access-токен (инъектируется в middleware; боевая
// реализация — RSAVerifier поверх публичного ключа). Абстракция позволяет
// тестировать middleware без RSA (стаб в тесте).
type Verifier interface {
	// VerifyAccess проверяет подпись и срок токена и требует typ=access;
	// иначе — ошибка.
	VerifyAccess(token string) (Claims, error)
}

// Sign подписывает claims приватным ключом методом RS256 и возвращает компактный
// JWT. Поля claims (в т.ч. exp/iat/typ) кодируются как есть — вызывающий сам
// задаёт срок жизни и тип токена.
func Sign(priv *rsa.PrivateKey, claims Claims) (string, error) {
	panic("not implemented: auth.Sign")
}

// Parse проверяет подпись token публичным ключом (RS256) и срок действия (exp),
// возвращая распарсенные claims. Ошибка при неверной подписи или истёкшем
// токене. typ НЕ проверяется — это делает VerifyAccess.
func Parse(pub *rsa.PublicKey, token string) (Claims, error) {
	panic("not implemented: auth.Parse")
}

// RSAVerifier — боевая реализация Verifier поверх публичного RSA-ключа.
type RSAVerifier struct {
	pub *rsa.PublicKey
}

// NewRSAVerifier конструирует верификатор для публичного ключа pub.
func NewRSAVerifier(pub *rsa.PublicKey) *RSAVerifier {
	return &RSAVerifier{pub: pub}
}

// VerifyAccess = Parse + требование typ=access (иначе ошибка).
func (v *RSAVerifier) VerifyAccess(token string) (Claims, error) {
	panic("not implemented: auth.RSAVerifier.VerifyAccess")
}
