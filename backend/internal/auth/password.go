package auth

import (
	"golang.org/x/crypto/bcrypt"
)

// keep golang.org/x/crypto direct-зависимостью, пока функции — скелет.
var _ = bcrypt.DefaultCost

// BcryptCost — стоимость bcrypt для паролей УК-админа (auth.md §2: cost 12).
const BcryptCost = 12

// HashPassword возвращает bcrypt-хеш пароля pw (cost = BcryptCost). Хеш
// недетерминирован (bcrypt подмешивает соль) — два вызова дают разные строки,
// каждая из которых верифицируется исходным паролем.
func HashPassword(pw string) (string, error) {
	panic("not implemented: auth.HashPassword")
}

// VerifyPassword сверяет bcrypt-хеш с паролем pw: nil при совпадении, иначе
// ошибка (не раскрывающая, что именно не совпало).
func VerifyPassword(hash, pw string) error {
	panic("not implemented: auth.VerifyPassword")
}
