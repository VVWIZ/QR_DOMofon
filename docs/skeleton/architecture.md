# Архитектура — Walking Skeleton

Go модульный монолит + PostgreSQL 16 + Redis 7 + EMQX 5 + self-hosted LiveKit; React/TS SPA; Go-эмулятор устройства. Документ написан **до кода** и фиксирует намерение: структуру модулей, схему БД, ключевые решения и канонические фикстуры. Расхождение кода с этим документом — сигнал проблемы.

Связанные документы: [api.md](api.md) (REST/SSE), [firmware/docs/PROTOCOL.md](../../firmware/docs/PROTOCOL.md) (MQTT-контракт), [README.md](../../README.md) (quickstart + демо-скрипт).

---

## 1. Модульный монолит → микросервисы

Каждый модуль `backend/internal/*` — кандидат в отдельный сервис из ТЗ §1.3.2. Границы модулей = будущие сетевые границы, поэтому нарушать их нельзя даже «по-быстрому».

| Модуль | Будущий сервис (ТЗ §1.3.2) | Ответственность в skeleton |
|---|---|---|
| `qr` | QR Service | Валидация HMAC-подписи (`aid:v:kid`, base64url[0:32]), реестр ключей `qr_keys` по `kid` |
| `property` | Property Service | Чтение фикстур: УК → дом → квартира → AccessPoint |
| `calls` | Call Signaling Service (+ LiveKit = Media SFU) | Сессии звонков (Redis), комнаты/токены LiveKit, события жильцу через `Notifier` |
| `access` | Access Control Service | `POST /access/open`: проверка звонка и presence → публикация MQTT-команды `open_relay` |
| `devices` | Device Registry | Подписка на `devices/+/status`, presence в Redis, `last_seen_at`, список устройств |
| `audit` | Audit & Logging Service | Append-only запись событий, чтение с `limit` |
| `platform/*` | инфраструктурный слой (не сервис) | `config`, `logging` (slog JSON), `httpx` (роутер chi, конверт ошибок, SSE), `postgres` (pgx + goose), `redis`, `mqtt` (paho) |

**Правило границ: интерфейсы на стороне потребителя.** Модуль-потребитель объявляет у себя минимальный интерфейс того, что ему нужно, а конкретная реализация (чужой модуль или адаптер из `platform`) внедряется в `cmd/server` при сборке приложения. Примеры намерения:

- `calls` объявляет `type Notifier interface { CallIncoming(...); CallCancelled(...); CallAccepted(...) }` — SSE-hub из `platform/httpx` лишь реализует его. Замена SSE на WebSocket/push не трогает `calls`.
- `access` объявляет `type CommandPublisher interface { PublishOpenRelay(ctx, deviceID, cmd) error }` и `type DevicePresence interface { IsOnline(ctx, deviceID) (bool, error) }` — реализации приходят из `platform/mqtt` и `devices`.
- Прямые импорты `internal/<модуль>` из другого модуля запрещены; общих «доменных» пакетов-свалок нет. Так распил на сервисы = замена реализации интерфейса на сетевой клиент.

### Дерево монорепо

```
QR Domofon/
├── README.md
├── .env.example
├── .gitignore
├── deploy/
│   ├── docker-compose.yml          # ТОЛЬКО инфраструктура (PG, Redis, EMQX, LiveKit)
│   └── livekit/livekit.yaml
├── backend/
│   ├── go.mod
│   ├── cmd/
│   │   ├── server/                 # API :8080, миграции goose автоматически при старте
│   │   └── qrgen/                  # печатает канонический URL посетителя
│   ├── internal/
│   │   ├── platform/{config,logging,httpx,postgres,redis,mqtt}
│   │   ├── qr/  property/  calls/  access/  devices/  audit/
│   └── migrations/
│       ├── 0001_init.sql           # схема (§3)
│       └── 0002_seed.sql           # канонические фикстуры (§5)
├── web/visitor/
│   ├── package.json  vite.config.ts
│   └── src/
│       ├── main.tsx  App.tsx  api/
│       ├── pages/{VisitorPage,ResidentPage}   # роуты /v и /resident
│       ├── components/CallRoom.tsx            # общая LiveKit-комната
│       └── hooks/useSSE.ts
├── firmware/
│   ├── docs/PROTOCOL.md            # MQTT-контракт (единый для эмулятора и ESP32)
│   ├── esp32/.gitkeep              # второй шаг, вне skeleton
│   └── emulator/                   # отдельный go.mod
│       ├── go.mod  main.go  config.go
│       ├── relay.go                # цикл open→(duration_ms)→closed
│       ├── commands.go             # разбор open_relay, свежесть, ack
│       ├── idempotency.go          # буфер 20 request_id, TTL 60с
│       ├── heartbeat.go            # 30с + внеочередной при смене relay_state
│       └── failopen.go             # порог 90с, гистерезис 30с, offline-буфер событий
└── docs/skeleton/{api.md, architecture.md}
```

> Отклонение от плана этапа 2 (зафиксировано на этапе документации): пути документов скорректированы под глобальный guardrail-хук пользователя (`block-md.js` разрешает `.md` только в `docs*`/`specs*`-директориях): `firmware/PROTOCOL.md` → `firmware/docs/PROTOCOL.md`, `_docs/skeleton/` → `docs/skeleton/`. Все последующие этапы используют **эти** пути.

---

## 2. Поток данных (вертикальный срез)

```
QR-URL (/v?aid&v=1&kid&sig)
  → POST /qr/validate (qr + property)          — AC2
  → POST /calls/initiate (calls: busy-check Redis, комната LiveKit, SSE жильцу)  — AC3
  → медиа visitor↔resident через LiveKit        — AC4
  → POST /access/open (access: presence-check → MQTT open_relay)                — AC5
  → эмулятор: идемпотентность/свежесть → реле open→closed → command_ack         — AC6..AC8
  → devices: heartbeat → presence; audit: append событий                        — AC9, AC14
```

---

## 3. Схема БД (PostgreSQL 16, миграции goose)

Полная иерархия из ТЗ §2 сохранена (включая `management_company_id` под будущий RLS), но заполняется одной цепочкой фикстур. ЖК (ResidentialComplex) в skeleton опущен — `buildings` ссылается сразу на УК.

| Таблица | Поля | Связи / примечания |
|---|---|---|
| `management_companies` | `id uuid PK`, `name text`, `created_at timestamptz` | — |
| `buildings` | `id uuid PK`, `management_company_id uuid FK`, `address text`, `created_at` | → management_companies |
| `apartments` | `id uuid PK`, `building_id uuid FK`, `management_company_id uuid FK`, `number text`, `is_active bool` | → buildings; одна активная квартира-фикстура |
| `access_points` | `id uuid PK`, `public_id uuid UNIQUE`, `building_id uuid FK`, `management_company_id uuid FK`, `type text CHECK (type IN ('entrance','gate','barrier'))`, `label text`, `fail_mode text DEFAULT 'open'`, `is_active bool`, `created_at` | → buildings; `public_id` — то, что в QR (`aid`), внутренний `id` наружу не отдаётся (ТЗ §17.4) |
| `devices` | `id uuid PK`, `serial text UNIQUE`, `access_point_id uuid UNIQUE FK`, `management_company_id uuid FK`, `mqtt_client_id text`, `type text`, `firmware_version text`, `last_seen_at timestamptz`, `created_at` | → access_points (UNIQUE = связь 1:1, FK на стороне ребёнка — ТЗ §2.2.5). **Колонки `status` НЕТ** — статус derived из Redis-presence |
| `qr_keys` | `kid text PK`, `secret text`, `is_active bool`, `created_at` | Реестр ключей подписи QR (ротация по `kid`, ТЗ §5.3) |
| `audit_events` | `id bigserial PK`, `event_type text`, `occurred_at timestamptz`, `actor text`, `apartment_id uuid`, `access_point_id uuid`, `device_id uuid`, `call_id uuid`, `request_id uuid`, `management_company_id uuid`, `metadata jsonb` | **Append-only**: код выполняет только INSERT/SELECT; UPDATE/DELETE отсутствуют в репозитории. Hash-chain/WORM (ТЗ §16.1) — вне skeleton |

Звонки и presence в БД **не хранятся** — это эфемерное состояние в Redis (см. §4). `call_id` в аудите достаточно для трассировки.

**Инкремент auth (0003–0004):** `users`, `user_apartment_roles`. **Инкремент онбординга (0005):** `invites` (хранится только `sha256(token)`), `user_access_grants`.

**Инкремент A — подъезды/композитный инвайт (0006):**
- `entrances` (`id`, `building_id FK`, `management_company_id`, `number`, `UNIQUE(building_id,number)`, `UNIQUE(id,building_id)`) — иерархия `building → entrance → apartment/access_point`. **Осознанное расширение ТЗ:** в диаграмме иерархии ТЗ §2.1 подъезд есть, но в §2.2 отдельной сущности Entrance нет (Apartment/AccessPoint ссылаются на `building_id`). Ввели таблицу, т.к. подъезд ≠ точка доступа (существует без устройства, точек может быть >1).
- `apartments.entrance_id` / `access_points.entrance_id` — **nullable**, композитный `FK (entrance_id, building_id)` (гард «подъезд чужого дома»; NULL пропускается). `building_id` **сохранён везде** — это expand-фаза expand-contract: горячий `property.ResolveByPublicID` джойнит по `building_id` и НЕ изменён. Перевод резолва на подъезды и `entrance_id NOT NULL` (contract-фаза) — **долг инкремента звонковой логики**.
- `users.full_name`, `invites.full_name` (ФИО, nullable), `invite_access_points` (доп. гранты композитного инвайта; `PK(invite_id, access_point_id)`).

---

## 4. Ключевые решения

### 4.1 Redis-presence устройств, TTL 90с (AC9)

Ключ `device:online:{device_id}` устанавливается модулем `devices` на каждый heartbeat c `EX 90` (порог offline из ТЗ §2.2.6). Нет ключа = offline. Статус — всегда производное значение на момент запроса: нет фоновых джоб, нечему рассинхронизироваться, порог задаётся одним числом. `last_seen_at` в Postgres — для отображения, не для решения online/offline.

### 4.2 Call-сессия: Redis `SET NX EX 120` (AC13, E5)

Занятость квартиры — ключ `call:apartment:{apartment_id}` со значением `call_id`, атомарно через `SET NX EX 120`. NX-проигрыш = 409 `CALL_IN_PROGRESS`. Детали звонка — ключ `call:{call_id}` (apartment_id, access_point_id, device_id, state) с тем же TTL. TTL 120с — авто-очистка зависших сессий (брошенная вкладка) без фоновых воркеров; `cancel`/`end` удаляют ключи немедленно.

### 4.3 SSE за интерфейсом `Notifier`

Транспорт сигнала жильцу — деталь реализации: `calls` пишет в свой интерфейс `Notifier`, SSE-hub в `platform/httpx` — одна из реализаций (`GET /api/v1/resident/events`). В ТЗ §13.3 прод-транспорт — WebSocket + push; skeleton выбирает SSE как самый дешёвый однонаправленный канал, а интерфейс гарантирует, что замена не заденет доменную логику.

### 4.4 LiveKit: UDP mux на 7882, `node_ip 127.0.0.1`

`livekit.yaml`: `rtc.udp_port: 7882` (single-port mux — один UDP-порт вместо диапазона, критично для Docker Desktop на Windows, где проброс диапазонов портов мучителен), `rtc.tcp_port: 7881` (fallback), `node_ip: 127.0.0.1`, `use_external_ip: false` (демо строго на localhost — оба окна браузера на одной машине). Токены выпускает **только backend** (server-sdk-go/v2); гранты по ТЗ §6.1: visitor — publish camera+mic, resident — publish только mic; `room = call_id`. **План Б** (если UDP через Docker Desktop нестабилен): нативный `livekit-server.exe` на хосте с тем же `livekit.yaml`.

### 4.5 Compose = только инфраструктура

`deploy/docker-compose.yml` поднимает PG16 (:5432), Redis7 (:6379), EMQX5 (:1883, :18083, anonymous auth — только dev), LiveKit v1.9 (:7880/:7881/:7882udp). Backend, эмулятор и Vite — **на хосте** (`go run` / `npm run dev`): быстрый цикл правок без пересборки образов, простая отладка на Windows. Порядок запуска — README, quickstart.

### 4.6 MQTT keepalive 30с (отклонение от ТЗ §11.1)

Зафиксировано и обосновано в [PROTOCOL.md](../../firmware/docs/PROTOCOL.md) §1: инвариант `1.5 × keepalive < 90с` (порог fail-open/offline). Для ESP32 — пересмотреть синхронно с порогами.

### 4.7 Прочее

- Миграции goose применяются автоматически при старте `cmd/server` (идемпотентно).
- Ошибки — единый конверт `{error:{code,message,request_id}}` в `platform/httpx` (ТЗ §13.1); полный реестр кодов — [api.md](api.md).
- Логи — slog JSON в stdout; ключевые записи, на которые опирается демо: `qr_validation_failed`, `stale_command_rejected` (эмулятор), `fail_open_activated`.
- Библиотеки: backend — chi v5, paho.mqtt.golang v1.5, pgx/v5, goose v3, livekit server-sdk-go/v2, go-redis v9, google/uuid, godotenv, slog; web — react 18, react-router-dom, livekit-client v2, @livekit/components-react, vite, typescript.

---

## 5. Фиксированные UUID фикстуры (канонические)

Единственный источник истины для сидов (`0002_seed.sql`), конфигов, тестов и демо **всех последующих этапов**. UUID намеренно «читаемые» — мгновенно узнаются в логах (1…= УК, 2…= дом, 3…= квартира, 4/5…= точка доступа, 6…= устройство).

| Сущность | Поле | Значение |
|---|---|---|
| УК «Демо УК» | `management_companies.id` | `11111111-1111-1111-1111-111111111111` |
| Дом «ул. Демонстрационная, 1» | `buildings.id` | `22222222-2222-2222-2222-222222222222` |
| Квартира «1» | `apartments.id` | `33333333-3333-3333-3333-333333333333` |
| AccessPoint «Подъезд №1» (type `entrance`, fail_mode `open`) | `access_points.id` | `44444444-4444-4444-4444-444444444444` |
| — публичный ID (= `aid` в QR-URL) | `access_points.public_id` | `55555555-5555-5555-5555-555555555555` |
| Устройство (эмулятор) | `devices.id` | `66666666-6666-6666-6666-666666666666` |
| — серийник | `devices.serial` | `EMU-001` |
| — MQTT Client ID | `devices.mqtt_client_id` | `device:EMU-001` |
| Ключ подписи QR | `qr_keys.kid` | `dev1` |
| — секрет (ТОЛЬКО dev, в прод не тащить) | `qr_keys.secret` | `dev-qr-secret-change-me` |

Производные канонические значения (детерминированы фикстурами):

```
message = "55555555-5555-5555-5555-555555555555:1:dev1"
sig     = base64url(HMAC-SHA256(message, "dev-qr-secret-change-me"))[0:32]
        = "oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6"

URL посетителя (печатает cmd/qrgen):
http://localhost:5173/v?aid=55555555-5555-5555-5555-555555555555&v=1&kid=dev1&sig=oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6
```

MQTT-топики устройства-фикстуры:

```
devices/66666666-6666-6666-6666-666666666666/commands
devices/66666666-6666-6666-6666-666666666666/status
```

Эти значения обязаны совпадать в: `0002_seed.sql`, `.env.example` (секрет `dev1`), конфиге эмулятора (`device_id`, `serial`), `cmd/qrgen`, интеграционных тестах и демо-скрипте README. Тест, проверяющий подпись, должен получать `oRnZ1qQnxcI1GrLWAmBYrmVIH__CG-K6` на этих входах.
