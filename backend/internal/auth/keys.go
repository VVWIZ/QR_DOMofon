package auth

// Загрузка ключей RS256 (auth.md §3). Ключи подаются в PEM через окружение
// (JWT_PRIVATE_KEY / JWT_PUBLIC_KEY). В dev при пустых ключах используется
// фиксированный dev-keypair ниже — токены переживают рестарт backend (удобно,
// часто перезапускаем). В проде (AuthDevMode=false) пустые ключи → fail-closed:
// сервер не стартует. Сигнатуры пакета auth из этапа 5a здесь не затрагиваются.

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// devJWTPrivateKeyPEM / devJWTPublicKeyPEM — ФИКСИРОВАННЫЙ dev-keypair RS256
// (RSA-2048). НЕ ДЛЯ ПРОД. Тот же keypair продублирован в .env.example, поэтому
// токены валидны и при запуске из .env, и при пустом окружении в dev-режиме.
const devJWTPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEowIBAAKCAQEAo8qe0RrDUgsZmgg/YciIwT66iXE8mQuCWnHAKPP4tcLncZN9
yGIuaxJr1bRXpXG+sQ8S22CGVn7tJNfUtgkpZXaaJ02WMUe/cloLwwimiH4FpJHY
Tp15UW1mbtMLa1DMQXSK4ZlNJoatUJytm7RGhZLd5LYkR1j7/x6Qf7gbWg5rtYgZ
n19Gf2k5kh2byhFhvmBOyOfkfvDREEAq3kyV+qdy+SE5HhHGsZdqrsWwtYd5tc8m
UlMyHkCP9FZmp5b7sm/pY6kzijmBFZHAOedrgXGYoW5GW627xoOUbYbvIRIa9iZj
L+Tt6OhwLOsU0V0zNRujheZVTXe3tzEgmfBmeQIDAQABAoIBAAX66Owb9ipAY7+d
F2RUctLHEv7fkqhIqb/FNAWtbQTvisKkXA+v2apDiUqg4PYroPGHFNJhPoe5WWLy
muPHvJpQN4i3E1m1WQhIVkeEC7bYzrpaGqvPIdMZuOdTtFtnade7dQfPhd1h24GB
lkXvXHJLdmd08345Nwp9j7t2tI/yfx82FEI0fBSQ3FM5PtoxV6FjFuu2aDOr7NRf
VLoipRrlD+a9GEj0efu8erwN7Tb/Yf9kiBaA1aDG/rbatc1ubfua/nhd19ReSTj3
Sei9J7FEFMwe/yG3/jRfxRab3FHWHlgohNBhP1YVCF6zpPmIlO4OEQSlxENSEp82
08jf9YECgYEA1pux9+3D7ZDvCy/LWcHIB00IPTla5X8QdlK4Dv3ILcqErLCsZI0e
F3k8owNSi+WGzSSPfQX4Z6bm4xw42JEuGJBcuqYAZVtqojB41K/iSp3hs0pSGHPk
qdKrniIl0D6DSH0xpR8q3g8FokiDngeKBlvXyXhFghHT/kmuMCeZB/kCgYEAw2HY
OJp3iKkepG+yOPZ7wTBo9AETFWDbOS5gv7uVmhSPQ60sL/pI6VT8nsmrhkBDQb3Y
nt4e6Htyf05D2U/MHUpruZKcvCH74XCrwI7nRdXA7yXtMqpmrjonwJ4GLxza6nYw
YLqpaTxMOCV10leXYbjH/M0rLv4add2IidUa8oECgYEAvNp2Wn9Zk42fTnDYujvV
EtevEHGQk7Slf/p7DnY12lYFOxKeIj4s5OtDeRBLa+CoJ46s1pCScGRneiQzwiDA
N82STI4YexlfVSriqge9U3xsSaJ1bB9QckF51MaoEAFy9i91qKEs0AzYIF8/s6le
xQm9cwXr5PJbY8LjDm1KNcECgYB1/SuPGzEedUsM8GsHXUpk4zAuUkvM+D3LLUe9
4bE5aDsQGo75tkK7rdgUqCMOIta658PeRLMToCEH4iK1JCxWb+/YFELUlg0/GkSO
N35QvQITKasxkpgJlRMWjheb8ef9+TvD3lWaOJCqw2yAhubjW6xh7SCr80XVceAX
pHrugQKBgCQymk0IYuGM2AG5szT+oXSPzL4k3cjcSOMnGgr81mp8Hrfl7Rc6MUFN
ipG40MzUFou8tGUjEIiPaAP5nltsbTcWGGcw9yKCi9/qk44qMOAMLgoQJ2TNc7g9
E5Jol5YhdaLh4/zM380sAx0tnxMXA7aKN0T3g58SiI0vmN3XI624
-----END RSA PRIVATE KEY-----`

const devJWTPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAo8qe0RrDUgsZmgg/YciI
wT66iXE8mQuCWnHAKPP4tcLncZN9yGIuaxJr1bRXpXG+sQ8S22CGVn7tJNfUtgkp
ZXaaJ02WMUe/cloLwwimiH4FpJHYTp15UW1mbtMLa1DMQXSK4ZlNJoatUJytm7RG
hZLd5LYkR1j7/x6Qf7gbWg5rtYgZn19Gf2k5kh2byhFhvmBOyOfkfvDREEAq3kyV
+qdy+SE5HhHGsZdqrsWwtYd5tc8mUlMyHkCP9FZmp5b7sm/pY6kzijmBFZHAOedr
gXGYoW5GW627xoOUbYbvIRIa9iZjL+Tt6OhwLOsU0V0zNRujheZVTXe3tzEgmfBm
eQIDAQAB
-----END PUBLIC KEY-----`

// LoadKeys выбирает источник ключей RS256 и парсит их:
//
//   - privPEM и pubPEM оба заданы → парсим их (prod/явная конфигурация);
//   - оба пустые и devMode=true → фиксированный dev-keypair (переживает рестарт);
//   - иначе (пусто и !devMode, либо задан лишь один) → ошибка (fail-closed).
//
// Возвращает приватный ключ (для подписи access/refresh) и публичный (для
// верификации). Формат приватного: PKCS#1 или PKCS#8; публичного: PKIX или PKCS#1.
func LoadKeys(privPEM, pubPEM string, devMode bool) (*rsa.PrivateKey, *rsa.PublicKey, error) {
	switch {
	case privPEM != "" && pubPEM != "":
		// как есть
	case privPEM == "" && pubPEM == "" && devMode:
		privPEM, pubPEM = devJWTPrivateKeyPEM, devJWTPublicKeyPEM
	case privPEM == "" && pubPEM == "" && !devMode:
		return nil, nil, errors.New("auth: JWT_PRIVATE_KEY/JWT_PUBLIC_KEY are required when AUTH_DEV_MODE is false (fail-closed)")
	default:
		return nil, nil, errors.New("auth: both JWT_PRIVATE_KEY and JWT_PUBLIC_KEY must be set together")
	}

	priv, err := parsePrivateKeyPEM([]byte(privPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("auth: parse private key: %w", err)
	}
	pub, err := parsePublicKeyPEM([]byte(pubPEM))
	if err != nil {
		return nil, nil, fmt.Errorf("auth: parse public key: %w", err)
	}
	return priv, pub, nil
}

// parsePrivateKeyPEM парсит RSA-приватный ключ из PEM (PKCS#1 "RSA PRIVATE KEY"
// или PKCS#8 "PRIVATE KEY").
func parsePrivateKeyPEM(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not PKCS1/PKCS8: %w", err)
	}
	rsaKey, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	return rsaKey, nil
}

// parsePublicKeyPEM парсит RSA-публичный ключ из PEM (PKIX "PUBLIC KEY" или
// PKCS#1 "RSA PUBLIC KEY").
func parsePublicKeyPEM(pemBytes []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if keyAny, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		rsaKey, ok := keyAny.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
		return rsaKey, nil
	}
	rsaKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not PKIX/PKCS1: %w", err)
	}
	return rsaKey, nil
}
