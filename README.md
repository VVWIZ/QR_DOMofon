# QR-Домофон — Walking Skeleton

Вертикальный срез QR-домофонной системы по [ТЗ v1.3](./ТЗ_QR_Домофонная_Система.md):

```
QR-валидация → LiveKit-видеозвонок жильцу → POST /access/open → MQTT open_relay → эмулятор реле
```

Поверх скелета доставлены два инкремента:

1. **auth/RBAC** — жилец/владелец (телефон → OTP), УК-админ (email+пароль+TOTP), JWT RS256, RBAC.
2. **Онбординг + гранты** — УК заводит владельцев и раздаёт доступ на калитки/шлагбаумы; владелец
   приглашает жильцов; приём инвайт-ссылки; **прямое открытие калитки/шлагбаума по гранту** (ТЗ §4.7).
   Итоги и security-ревью — [report.md](./docs/skeleton/report.md).

**Веб vs мобильное приложение (ТЗ §14).** Жилец/владелец в продукте работают через **мобильное
приложение**; браузер — у гостя/посетителя. Поэтому на вебе есть **УК-консоль** (`/admin`), а
резидентские флоу онбординга — **API-first** (потребитель — мобильный клиент). `/resident` в репо —
демо-стенд-ин скелета, не целевой UI.

Вне скоупа: гости/курьеры, гео-проверка, фотофиксация, push, реальный SMS-провайдер, мобильное
приложение, реальное железо (ESP32 — шаг 2), hash-chain аудита, OTA, TLS для MQTT, RLS, SystemAdmin.

Документация написана **до кода** и описывает намерение; расхождение кода с ней — сигнал проблемы.

> 🚀 **Запуск на Windows — см. [docs/RUNBOOK.md](./docs/RUNBOOK.md)** (проверенный порядок + подводные камни: VT-x/WSL2 для Docker, нативный LiveKit с `node_ip`=LAN для медиа, `-ldflags` для эмулятора под Kaspersky). Итоги живого прогона — [docs/skeleton/report.md](./docs/skeleton/report.md).

## Структура монорепо

| Директория | Содержимое |
|---|---|
| `deploy/` | `docker-compose.yml` (только инфраструктура) + `livekit/livekit.yaml` |
| `backend/` | Go модульный монолит: `cmd/server` (API :8080), `cmd/qrgen` (генератор QR-URL), `internal/{platform,qr,property,calls,access,devices,audit,auth,onboarding}`, `migrations/` |
| `web/visitor/` | React/TS SPA (Vite): `/v` — посетитель, `/admin` — УК-консоль, `/resident` — демо-стенд-ин жильца |
| `firmware/docs/PROTOCOL.md` | **MQTT-контракт устройства** — единый для эмулятора и ESP32 |
| `firmware/emulator/` | Go-эмулятор устройства (отдельный `go.mod`) |
| `firmware/esp32/` | Заглушка под ESP32-прошивку (второй шаг) |
| `docs/skeleton/` | [api.md](docs/skeleton/api.md) — REST/SSE контракты; [architecture.md](docs/skeleton/architecture.md) — модули, БД, решения, фикстуры |

## Канонические фикстуры (используются всеми этапами)

Полная таблица и обоснование — в [docs/skeleton/architecture.md](docs/skeleton/architecture.md). Кратко:

| Сущность | Значение |
|---|---|
| management_company_id | `11111111-1111-1111-1111-111111111111` |
| building_id | `22222222-2222-2222-2222-222222222222` |
| apartment_id (кв. «1») | `33333333-3333-3333-3333-333333333333` |
| access_point_id («Подъезд №1») | `44444444-4444-4444-4444-444444444444` |
| access_point_public_id (`aid` в QR) | `55555555-5555-5555-5555-555555555555` |
| device_id (в MQTT-топиках) | `66666666-6666-6666-6666-666666666666` |
| device_serial / MQTT ClientID | `EMU-001` / `device:EMU-001` |
| kid / QR dev-секрет | `dev1` / `dev-qr-secret-change-me` (только dev!) |
| Каноническая подпись `sig` | `oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6` |
| Канонический URL посетителя | `http://localhost:5173/v?aid=55555555-5555-5555-5555-555555555555&v=1&kid=dev1&sig=oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6` |

## Требования

- Windows 10 + Docker Desktop (WSL2)
- Go 1.22+
- Node.js 20+ (npm)
- [MQTTX CLI](https://mqttx.app/cli) — только для edge-сценария E2 (`npm i -g @emqx/mqttx-cli` или скачать exe)

## Quickstart (PowerShell)

```powershell
# 1. Инфраструктура: PostgreSQL 16, Redis 7, EMQX 5, LiveKit
docker compose -f deploy/docker-compose.yml up -d
docker compose -f deploy/docker-compose.yml ps   # все контейнеры Up/healthy

# 2. Backend (миграции применяются автоматически при старте)
cd backend
go run ./cmd/server
# проверка в другом окне: curl.exe http://localhost:8080/health  → 200

# 3. Эмулятор устройства (новое окно PowerShell)
cd firmware/emulator
go run .
# в логе: connected to broker, heartbeat каждые 30с

# 4. Веб-клиент (новое окно PowerShell)
cd web/visitor
npm install
npm run dev
# Vite на http://localhost:5173

# 5. Сгенерировать URL посетителя (новое окно PowerShell)
cd backend
go run ./cmd/qrgen
# печатает канонический URL (совпадает с таблицей фикстур выше)

# 6. Демо: URL посетителя — в первом окне браузера,
#    http://localhost:5173/resident — во втором
```

### Порты

| Порт | Сервис |
|---|---|
| 8080 | Backend API (хост) |
| 5173 | Vite dev server (хост) |
| 5432 | PostgreSQL 16 (docker) |
| 6379 | Redis 7 (docker) |
| 1883 / 18083 | EMQX 5: MQTT / dashboard (docker) |
| 7880 / 7881 / 7882udp | LiveKit: WS-API / TCP-fallback / RTP UDP mux (docker) |

## Демо-скрипт

### Happy path (AC2–AC6, AC14)

1. **Окно A (посетитель):** открыть канонический URL. Страница валидирует QR → показывает «Подъезд №1», кв. 1, статус устройства `online`.
2. Нажать «Позвонить». **Окно B (`/resident`):** входящий звонок появляется **≤2 секунд** (SSE `call.incoming`).
3. В окне B нажать «Принять» → устанавливается медиа: жилец **видит видео** посетителя, аудио **двустороннее** (разрешить браузеру камеру/микрофон в обоих окнах).
4. В окне B нажать «Открыть дверь» → ответ `{request_id, status: "sent"}`.
   **Лог эмулятора:** получена `open_relay` → `relay: open` → ровно через 5000 мс → `relay: closed`; опубликован `command_ack {result: "ok"}`. Актуация ≤1с от нажатия.
5. Проверить аудит: `curl.exe "http://localhost:8080/api/v1/audit/events?limit=20"` → события `call_initiated`, `call_accepted`, `door_open_requested`, `command_ack`.

### E1 — устройство offline (AC9, AC12)

1. Остановить эмулятор (**Ctrl+C** в его окне). Подождать **>90 секунд** (истекает Redis presence TTL).
2. `curl.exe http://localhost:8080/api/v1/devices` → у устройства `status: "offline"`.
3. Открыть URL посетителя заново → страница показывает **предупреждение** «Устройство временно недоступно…», но кнопка звонка **активна** (звонок не блокируется).
4. Позвонить, принять, нажать «Открыть дверь» → **503** `{"error":{"code":"DEVICE_OFFLINE",...}}`.
5. Запустить эмулятор снова → в течение ~30с (первый heartbeat) устройство снова `online`.

### E2 — повторная доставка команды (AC7), вручную через MQTTX

Подписаться на статус-топик (окно 1):

```powershell
mqttx sub -h localhost -p 1883 -t "devices/66666666-6666-6666-6666-666666666666/status" -q 1
```

Опубликовать команду **дважды** с одним `request_id` (окно 2; `issued_at` — обязательно текущее UTC, иначе сработает stale-защита):

```powershell
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
$cmd = '{"cmd":"open_relay","relay_id":1,"duration_ms":5000,"request_id":"e2e2e2e2-0000-4000-8000-000000000001","issued_by":"manual:mqttx","issued_at":"' + $now + '"}'
mqttx pub -h localhost -p 1883 -t "devices/66666666-6666-6666-6666-666666666666/commands" -q 1 -m $cmd
mqttx pub -h localhost -p 1883 -t "devices/66666666-6666-6666-6666-666666666666/commands" -q 1 -m $cmd
```

**Ожидание:** первая команда — реле open на 5с (heartbeat `relay_state: "open"` → `"closed"`); вторая (в пределах TTL 60с) — **актуации нет**, в статус-топике `command_ack` с `"duplicate": true`. В логе эмулятора: `duplicate request_id, skip actuation`.

### E3 — stale-команда (AC8) — окно отключения строго 30–90 секунд!

1. Установить звонок (окна A и B, звонок принят).
2. Остановить эмулятор (**Ctrl+C**).
3. **Сразу же** (пока backend ещё считает устройство online — presence-ключ живёт до 90с после последнего heartbeat) в окне B нажать «Открыть дверь» → ответ **200** `{status: "sent"}`; команда буферизуется брокером (persistent session, CleanSession=false).
4. Подождать **30–90 секунд** от момента нажатия. Почему именно это окно: **>30с** — чтобы команда протухла (порог свежести `issued_at`); **<90с** — чтобы шаг 3 успел пройти до перехода устройства в offline (иначе получите 503 и сценарий не начнётся).
5. Запустить эмулятор → брокер доставляет буферизованную команду → **лог эмулятора:** `stale_command_rejected`; в статус-топике `command_ack {result: "rejected", "reason": "stale"}`; **реле НЕ актуировалось** (двери «задним числом» нет).

### E4 — fail-open и восстановление (AC10, AC11) — тайминги 90с/30с

1. Эмулятор работает. Отключить связь, не убивая эмулятор:
   ```powershell
   docker compose -f deploy/docker-compose.yml pause emqx
   ```
2. Ждать **≥90 секунд** → **лог эмулятора:** `fail_open_activated`, `relay: open` (активное удержание). Событие копится в offline-буфере.
3. Восстановить связь:
   ```powershell
   docker compose -f deploy/docker-compose.yml unpause emqx
   ```
   Эмулятор реконнектится, публикует буферизованное событие `fail_open_activated` → оно попадает в аудит (`GET /api/v1/audit/events`). Backend тоже переподключается к брокеру автоматически.
4. **Проверка гистерезиса (реконнект <30с не закрывает):** в течение <30с после unpause снова `pause emqx` → реле **остаётся open**, таймер стабильности сброшен. Затем `unpause`.
5. После **≥30 секунд непрерывной** связи → **лог эмулятора:** `fail_open_deactivated`, `relay: closed`; heartbeat `relay_state: "closed"`; событие в аудите.

### E5 — занятая квартира (AC13)

1. Окно A: посетитель начал звонок (не завершён).
2. Окно C (ещё одна вкладка с тем же URL посетителя): нажать «Позвонить» → **409** `{"error":{"code":"CALL_IN_PROGRESS",...}}`.
3. Завершить/отменить первый звонок → повторный звонок из окна C проходит. Подвисшая сессия самоочищается по TTL 120с (Redis `SET NX EX`).

### E6 — битая подпись (AC2)

1. Взять канонический URL и изменить любой символ в `sig`.
2. Открыть → страница показывает ошибку; backend отвечает **400** `{"error":{"code":"INVALID_QR",...}}`. Данные о точке доступа/квартире **не раскрываются**. В логе backend — `qr_validation_failed`.

## Дальше

- REST/SSE-контракты: [docs/skeleton/api.md](docs/skeleton/api.md)
- Архитектура, схема БД, решения, фикстуры: [docs/skeleton/architecture.md](docs/skeleton/architecture.md)
- MQTT-контракт устройства: [firmware/docs/PROTOCOL.md](firmware/docs/PROTOCOL.md)
