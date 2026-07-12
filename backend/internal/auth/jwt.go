// Package auth — аутентификация и RBAC (auth.md, api.md "Аутентификация").
//
// Скоуп инкремента: жилец/владелец (телефон → SMS OTP → JWT) и УК-админ
// (email+пароль+TOTP) + RBAC-энфорсмент. Пакет содержит детерминированную
// доменную логику: подпись/валидация JWT RS256, bcrypt-пароли, TOTP, политика
// OTP-лимитов и whitelist refresh-токенов (последние два — поверх инъектируемых
// Redis-подобных интерфейсов, чтобы юнит-тесты шли без реального Redis).
package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

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

// signedClaims оборачивает доменные Claims в интерфейс jwt.Claims. Поля Claims
// промоутятся в JSON (sub/kind/roles/mc_id/jti/iat/exp/typ) как есть — так
// round-trip Sign→Parse возвращает идентичную структуру. Методы Get* дают
// валидатору jwt/v5 доступ к exp/iat для проверки срока.
type signedClaims struct {
	Claims
}

func (c signedClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.ExpiresAt, 0)), nil
}

func (c signedClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

func (c signedClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c signedClaims) GetIssuer() (string, error)              { return "", nil }
func (c signedClaims) GetSubject() (string, error)             { return c.Subject, nil }
func (c signedClaims) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

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
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, signedClaims{Claims: claims})
	return tok.SignedString(priv)
}

// Parse проверяет подпись token публичным ключом (RS256) и срок действия (exp),
// возвращая распарсенные claims. Ошибка при неверной подписи или истёкшем
// токене. typ НЕ проверяется — это делает VerifyAccess.
func Parse(pub *rsa.PublicKey, token string) (Claims, error) {
	var sc signedClaims
	_, err := jwt.ParseWithClaims(token, &sc, func(t *jwt.Token) (any, error) {
		// Жёстко фиксируем алгоритм RS256: чужой alg (в т.ч. подмена на none
		// или HMAC с публичным ключом как секретом) — отказ.
		if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return pub, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}))
	if err != nil {
		return Claims{}, err
	}
	return sc.Claims, nil
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
	c, err := Parse(v.pub, token)
	if err != nil {
		return Claims{}, err
	}
	if c.Type != TypeAccess {
		return Claims{}, errors.New("token is not an access token")
	}
	return c, nil
}
