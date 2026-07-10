// Command qrgen печатает канонический подписанный URL посетителя
// /v?aid&v&kid&sig для сид-точки доступа (architecture.md §5). Использует
// qr.Sign; секрет kid=dev1 берётся из env QR_SECRET или dev-дефолта (architecture.md §5).
package main

import (
	"fmt"
	"os"

	"domofon/backend/internal/qr"
)

func main() {
	aid := env("ACCESS_POINT_PUBLIC_ID", "55555555-5555-5555-5555-555555555555")
	v := env("QR_V", "1")
	kid := env("QR_KID", "dev1")
	secret := env("QR_SECRET", "dev-qr-secret-change-me") // ТОЛЬКО dev (architecture.md §5)
	baseURL := env("VISITOR_BASE_URL", "http://localhost:5173")

	sig := qr.Sign(aid, v, kid, secret)
	fmt.Printf("%s/v?aid=%s&v=%s&kid=%s&sig=%s\n", baseURL, aid, v, kid, sig)
}

func env(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}
