# HANDOFF — QR-домофонная система

**Дата:** 2026-07-17 · **Ветка:** `main` (синхронна с origin) · **ТЗ:** v1.3

**Git:**
- Remote (origin): `https://github.com/VVWIZ/QR_DOMofon.git`
- Клонировать: `git clone https://github.com/VVWIZ/QR_DOMofon.git`
- Локальный путь (dev-машина): `C:\claude\lenovo\QR Domofon`

Стартовая точка для продолжения работы. Полный контекст: [RUNBOOK.md](RUNBOOK.md) (запуск),
[skeleton/api.md](skeleton/api.md) (контракты API), [skeleton/architecture.md](skeleton/architecture.md)
(модель данных/решения), [skeleton/report.md](skeleton/report.md) (итоги по инкрементам).

---

## 0. Текущее состояние (важно)

- **Код:** всё закоммичено и запушено в origin/main. Рабочее дерево чистое. Последний коммит — `a3a462d`.
- **Локальный стек СЕЙЧАС НЕ ЗАПУЩЕН** (backend :8080 и Vite :5173 не отвечают — фоновые процессы
  прошлой сессии завершились). Docker-контейнеры (postgres/redis/emqx) могли остаться — проверить
  `docker ps`. Как поднять — §6.
- **Данные в Postgres сохранены в volume** (миграции 0001–0010 применены; есть демо-данные + УК
  «UK Romashka», созданная при тестах C).

---

## 1. Что это

QR-домофонная система: QR на двери → WebRTC-звонок жильцу (LiveKit) → открытие двери → MQTT → реле
устройства (эмулятор/ESP32). Плюс онбординг, гости, гранты доступа, платформенная админка, авто-открытие
по расписанию.

**Стек:** Go 1.26 модульный монолит (`domofon/backend`) + React18/Vite/TS (`web/visitor`) +
Postgres16/Redis7/EMQX5.8/LiveKit1.10 + Go-эмулятор устройства. Границы модулей = будущие микросервисы
(интерфейсы на стороне потребителя, адаптеры в `cmd/server/main.go`).

---

## 2. Что построено (по инкрементам)

| Инкремент | Что дал | Коммиты |
|---|---|---|
| **Walking skeleton** | QR→звонок→открытие→MQTT→реле, аудит, presence, fail-open | (ранние) |
| **auth/RBAC** | жилец/владелец (телефон→OTP), УК-админ (email+пароль+TOTP), JWT RS256, RBAC | (ранние) |
| **Онбординг** | УК создаёт владельцев + гранты; владелец приглашает жильцов; приём инвайта без СМС; прямое открытие калиток по гранту | `2ffe136`…`6d23e7e` |
| **A** | подъезды (`entrances`), ФИО, композитный инвайт (квартира + N грантов одной ссылкой), каталог, форма УК | `e4010fd`…`57dee92` |
| **B** | гости `/g/{token}` без аккаунта, окно ≤2д, делегирование `can_create_guests`; **производный доступ** (УК изъяла грант → гость мгновенно теряет точку) | `22cde3e`…`b7a6495` |
| **C** | сущность **Объект/ЖК** (`sites`) между УК и домом; платформенная админка **SystemAdmin** (`/system`): CRUD УК/объектов/домов/подъездов + создание mc_admin | `78042f0`…`50b55df` |
| **D** | **матрица доступа** в УК-консоли (владельцы × точки, чекбоксы дать/забрать); **отзыв гранта** (раньше был только выпуск); фильтр, раскрытие жильцов | `c096d6f`…`349468e` |
| **E** | **авто-открытие по расписанию** («время работы» калиток/шлагбаумов); reconciler-планировщик (fail-secure аренда) | `cea627b`…`5f07ea2` |
| dev-удобство | пропуск TOTP при `AUTH_DEV_MODE=true` (пароль остаётся) | `a3a462d` |

Каждый инкремент: дизайн через субагента `@architect` (Fable), живой прогон + security-ревью, коммиты
по стадиям.

**Иерархия (доросла до ТЗ):** Платформа → УК → **Объект(ЖК)** → дом → подъезд → квартира.
Калитки/шлагбаумы (`gate`/`barrier`) крепятся к Объекту; подъездные домофоны (`entrance`) — к дому/подъезду.

---

## 3. Модель данных / миграции

Goose, авто-применяются при старте backend (`//go:embed`).

| # | Файл | Содержимое |
|---|---|---|
| 0001 | init | mc, buildings, apartments, access_points, devices, qr_keys, audit_events |
| 0002 | seed | демо-фикстуры (mc 1111, дом 2222, кв 3333, точка 4444/public 5555, EMU-001 6666) |
| 0003/0004 | auth (+seed) | users (kind resident/owner/mc_admin), user_apartment_roles; demo admin/resident/owner |
| 0005 | onboarding | invites (hash-only), user_access_grants; калитка cccc/public eeee, EMU-002 dddd |
| 0006 | entrances_composite | entrances; `entrance_id` (nullable, композитный FK); `full_name`; invite_access_points |
| 0007 | guest_access | guest_access (+ CHECK окна ≤2д), guest_access_points |
| 0008 | sites | **sites** (Объект); `buildings.site_id`/`access_points.site_id` NOT NULL; `building_id` nullable |
| 0009 | sysadmin_seed | `users.kind += system_admin`; demo `sa@demo.example` |
| 0010 | schedules | access_point_schedules (dow/opens/closes/tz, только gate/barrier) |

**Ключевые решения:** `management_company_id` денормализован во всех tenant-таблицах (под RLS);
композитные FK `(child_id, mc)` гардят кросс-УК дрейф; секреты (инвайт/гость/QR) хранятся только как
SHA-256; `building_id` у существующих точек НЕ обнулялся (contract-фаза — долг инкремента звонков,
`property.ResolveByPublicID` остаётся building-scoped).

---

## 4. Модули backend (`backend/internal/`)

`platform` (config/httpx/logging/mqtt/postgres/redis) · `qr` · `property` · `calls` (LiveKit) ·
`access` (открытие: `OpenPoint` по гранту + `OpenResolved` общий хвост presence→MQTT→аудит) ·
`devices` (Commander/Presence/status_consumer) · `audit` · `auth` (JWT RS256, OTP, TOTP, RBAC,
middleware) · `onboarding` (инвайты, гранты, каталог, матрица — `matrix.go`) · `guests` (гостевой
доступ, производный резолв) · `sysadmin` (платформенный CRUD) · `schedules` (окна + reconciler).

Композиция — `backend/cmd/server/main.go` (адаптеры границ + группы роутов + старт reconciler-горутины).

---

## 5. Фронт (`web/visitor/src/`, роуты)

| Роут | Кто | Что |
|---|---|---|
| `/v?aid&v&kid&sig` | посетитель (публично) | QR-звонок |
| `/g/{token}` | гость (публично, без аккаунта) | открытие точек в окне |
| `/login` | все | вкладки Жилец (OTP) / Администратор (email+пароль[+TOTP]) |
| `/admin` | mc_admin | УК-консоль: матрица доступа, «+ Владелец», «Калитки/Шлагбаумы» (расписания) |
| `/system` | system_admin | платформенная админка: УК/объекты/дома/подъезды, создание mc_admin |
| `/resident` | жилец | демо-стенд-ин (в продукте — мобильное приложение, ТЗ §14) |

> **Резидентские флоу — API-first под мобильное приложение** (ТЗ §14: жилец/владелец = мобильный
> клиент, гость/посетитель = браузер). На вебе — только УК-консоль, платформа и гостевая страница.

---

## 6. Как поднять локально (Windows)

Полный порядок и подводные камни — [RUNBOOK.md](RUNBOOK.md). Кратко:

```powershell
cd deploy; docker compose up -d postgres redis emqx      # 1. инфра
livekit-server.exe --config …/livekit.yaml               # 2. LiveKit (нативно, node_ip=LAN) — только для звонков
cd backend; go run ./cmd/server                          # 3. backend (+миграции). Для быстрой демо расписаний: SCHEDULER_TICK=8s SCHEDULER_LEASE=20s
cd firmware/emulator; go build -ldflags="-s -w" -o emulator.exe .; ./emulator.exe   # 4. EMU-001 (подъезд)
./emulator.exe -device-id dddddddd-dddd-dddd-dddd-dddddddddddd -device-serial EMU-002   # 4б. EMU-002 (калитка), второе окно
cd web/visitor; npm run dev                              # 5. фронт :5173
```

Нужен `backend/.env` (`AUTH_DEV_MODE=true` + dev-keypair; `cp .env.example backend/.env`). Go в PATH:
`export PATH="$PATH:/c/Program Files/Go/bin"`.

---

## 7. Dev-креды

| Роль | Вход | Куда |
|---|---|---|
| SystemAdmin | `sa@demo.example` / `sysadmin-demo-123` | `/system` |
| УК-админ | `admin@demo.example` / `admin-demo-123` | `/admin` |
| Жилец / владелец | тел. `+77010000001` / `+77010000002` → OTP в `dev_code` | `/resident` (демо) |

> **TOTP в dev отключён** (`AUTH_DEV_MODE=true`): поле «Код 2FA» оставлять пустым, пароль обязателен.
> Прод (`AUTH_DEV_MODE=false`) — TOTP обязателен. dev TOTP-secret (если нужен): `JBSWY3DPEHPK3PXP`.

---

## 8. Особенности окружения (Windows)

- **Kaspersky** сильно замедляет/блокирует сборку бинарей (`go build` до ~2 мин, эмулятор — только с
  `-ldflags="-s -w"`). Тесты онбординга долго компилируются по той же причине.
- **LiveKit** — запускать нативно (`livekit-server.exe`), не в Docker; `node_ip` = реальный LAN-IP
  (виртуальные адаптеры WSL/Docker ломают ICE). Нужен только для сценария звонка.
- **tzdata** — в backend встроена (`_ "time/tzdata"` в main.go), `LoadLocation` работает без zoneinfo ОС.
  У системного Python зон нет (Asia/Almaty = UTC+5 вручную при расчётах).
- Кириллица в `curl` из git-bash бьётся (кодировка консоли) — для UTF-8-тел использовать python/urllib
  (бэкенд UTF-8 обрабатывает корректно, проверено).

---

## 9. Прод-гэпы / кандидаты на следующие инкременты

- **Курьеры** (ТЗ §2.2.9): как гости, но ≤24ч + one-time + цепочка точек (отложены из B).
- **Contract-фаза подъездов/объектов**: перевести `property.ResolveByPublicID` на подъезды/site;
  обнулить `building_id` у site-level калиток; `entrance_id NOT NULL`.
- **Расписания**: окна через полночь; лидер-элекшн вместо `pg_advisory_lock` при многоинстансности;
  latch-режим прошивки вместо аренды (для реального железа).
- **Гости/инвайты**: реальная доставка (сейчас ссылка в ответе — по решению заказчика это целевой
  дизайн), OTP-подтверждение приёма существующим пользователем, отзыв инвайта.
- **SystemAdmin**: impersonation (зайти в УК от имени mc_admin), глобальный аудит, деактивация/
  переименование сущностей.
- **Прод-hardening (общий, до продакшена):** реальный SMS-провайдер, MQTT TLS+X.509+ACL, QR-секрет в
  KMS, RLS/multi-tenancy enforcement, `sslmode=require`, ротация dev-keypair, CSRF на /auth/refresh,
  rate-limit на всё. ESP32-прошивка по `firmware/docs/PROTOCOL.md` (заменяет эмулятор без изменений backend).

---

## 10. Git / навигация

- Всё в `main`, синхронно с origin. Коммиты по стадиям (feat/fix/docs с суффиксом инкремента).
- Отчёты по инкрементам и итог A–E — в конце [skeleton/report.md](skeleton/report.md).
- Билд-артефакты `server*.exe`/`emulator*.exe` — в `.gitignore` (не коммитятся).
- Проверки перед коммитом: `go build/vet/test`, `gofmt -l`, `tsc --noEmit`, `vite build` — всё должно
  быть чисто (hook `go-quality.js` гоняет vet после правок Go).
