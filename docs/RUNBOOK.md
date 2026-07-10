# RUNBOOK — локальный запуск walking skeleton (Windows)

Практическое руководство по поднятию скелета на dev-машине. Составлено по итогам
живого прогона этапа 7 — включает реальные подводные камни окружения.

> Стек: Go-монолит (backend) + Go-эмулятор устройства + React/Vite (web) +
> Postgres/Redis/EMQX/LiveKit (инфраструктура). См. `docs/skeleton/architecture.md`.

---

## 0. Предусловия окружения (важно на Windows)

| Требование | Проверка / установка |
|---|---|
| **Аппаратная виртуализация (VT-x/SVM)** | Должна быть **включена в BIOS/UEFI**. `Get-ComputerInfo -Property HyperVisorPresent` → `True`. Без неё WSL2/Docker не стартуют (Docker Desktop висит на «Starting», нет WSL-дистрибутивов). |
| **WSL2** | `wsl --update` (ставит ядро). После включения VT-x может потребоваться перезагрузка. |
| **Docker Desktop** | WSL2-бэкенд. Внизу окна должно быть «Engine running» с ненулевыми RAM/CPU. Если завис — обновить версию / Troubleshoot → Reset to factory defaults. |
| **Go 1.24+** | `go version`. Ставится `winget install GoLang.Go`. |
| **Node 18+ / npm** | `node --version`. |
| **Антивирус Kaspersky** | Ложно блокирует свежесобранный бинарь эмулятора (`package main`). Обход — собирать/запускать с `-ldflags="-s -w"` (см. ниже), либо добавить исключение на каталог сборки. |

---

## 1. Инфраструктура (Docker)

Из каталога `deploy/`:

```powershell
cd deploy
docker compose up -d
docker compose ps          # Postgres/Redis/EMQX — healthy
```

Поднимает: Postgres 16 (5432), Redis 7 (6379), EMQX 5.8.6 (1883 MQTT, 18083 dashboard),
LiveKit (7880/7881/7882).

> **LiveKit на Windows — запускать нативно (см. §2), а не в Docker.** Медиа-плоскость
> LiveKit под Docker Desktop на Windows не пробивается из-за ICE-кандидатов (виртуальные
> адаптеры WSL/Docker). Docker-сервис `livekit` в compose оставлен как опция для Linux;
> на Windows его можно не поднимать (`docker compose up -d postgres redis emqx`).

---

## 2. LiveKit — нативный запуск (Windows, для рабочего медиа)

1. Скачать `livekit-server` для Windows (v1.10.0) с https://github.com/livekit/livekit/releases
   (`livekit_1.10.0_windows_amd64.zip`), распаковать `livekit-server.exe`.
2. **Проверить `node_ip`** в `deploy/livekit/livekit.yaml` — должен быть **реальный LAN-IP
   хоста** (напр. `192.168.0.104`), НЕ `127.0.0.1`. Узнать IP: `ipconfig` (адаптер Wi-Fi/Ethernet).
   На машине с адаптерами WSL/Docker (172.x) `127.0.0.1` и авто-detect дают нерабочую ICE-пару.
3. Запуск:

```powershell
& "путь\livekit-server.exe" --config "C:\claude\lenovo\QR Domofon\deploy\livekit\livekit.yaml"
```

Проверка: в логе `nodeIP: 192.168.0.104`, `curl http://localhost:7880` → 200.

---

## 3. Backend

```powershell
cd backend
go run ./cmd/server
```

Миграции (`0001_init`, `0002_seed`) и сиды применяются автоматически при старте.
Конфиг — из окружения с dev-дефолтами (`.env` опционален; дефолты в
`internal/platform/config/config.go` уже указывают на localhost-сервисы и канонические
фикстуры).

Проверка: `curl http://localhost:8080/health` → `{"deps":{"livekit":"ok","mqtt":"ok","postgres":"ok","redis":"ok"},"status":"ok"}`.

---

## 4. Эмулятор устройства

```powershell
cd firmware\emulator
go build -ldflags="-s -w" -o emulator.exe .   # -ldflags обходит ложный детект Kaspersky
.\emulator.exe
```

Проверка: `curl http://localhost:8080/api/v1/devices` → `"status":"online"` (heartbeat дошёл).

> Без `-ldflags="-s -w"` Kaspersky выдаёт `Access is denied` при запуске бинаря.

---

## 5. Frontend

```powershell
cd web\visitor
npm install
npm run dev            # Vite на :5173, proxy /api → :8080
```

---

## 6. Демо-URL

Сгенерировать подписанную ссылку посетителя:

```powershell
cd backend
go run ./cmd/qrgen
```

Канонический URL (фикстуры из `architecture.md §5`):
```
http://localhost:5173/v?aid=55555555-5555-5555-5555-555555555555&v=1&kid=dev1&sig=oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6
```
Жилец: `http://localhost:5173/resident` (второе окно браузера).

---

## 7. Порядок запуска (итого)

```
docker compose up -d postgres redis emqx      # 1. инфра
livekit-server.exe --config …/livekit.yaml    # 2. LiveKit (нативно, node_ip=LAN)
go run ./cmd/server                            # 3. backend (+миграции/сиды)
emulator.exe (собран с -ldflags)               # 4. устройство
npm run dev  (web/visitor)                      # 5. фронт
go run ./cmd/qrgen  →  открыть URL              # 6. демо
```

> Строгий порядок backend → эмулятор важен: первый heartbeat не retained, при обратном
> порядке устройство до 30с показывается offline.

---

## 8. Порты

| Порт | Сервис |
|---|---|
| 8080 | backend REST + SSE |
| 5173 | Vite (web) |
| 5432 | Postgres |
| 6379 | Redis |
| 1883 / 18083 | EMQX MQTT / dashboard |
| 7880 / 7881 / 7882(udp) | LiveKit signaling / TCP / UDP-mux |

---

## 9. Остановка

```powershell
# backend / emulator / vite / livekit — Ctrl+C в их окнах
cd deploy
docker compose down          # + `-v` чтобы снести данные Postgres
```

---

## 10. Быстрая диагностика

| Симптом | Причина / фикс |
|---|---|
| `/health` deps не `ok` | соответствующий контейнер не поднялся — `docker compose ps` |
| устройство `offline` | эмулятор не запущен, либо запущен раньше backend (heartbeat потерян, ≤90с) |
| `emulator.test.exe: Access is denied` | Kaspersky — собирать с `-ldflags="-s -w"` |
| видео чёрное / `publisher data channel closed` | `node_ip` LiveKit ≠ LAN-IP; поставить реальный IP хоста и перезапустить LiveKit |
| `INVALID_QR` на валидной ссылке | пересчитать подпись через `cmd/qrgen` (секрет/kid должны совпадать с сидом) |
| Docker висит на «Starting» | VT-x выключена в BIOS, либо WSL2-ядро не установлено (`wsl --update`) |
