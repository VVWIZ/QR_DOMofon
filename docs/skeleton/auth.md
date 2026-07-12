# Auth / RBAC — дизайн (инкремент)

Аутентификация и авторизация поверх walking skeleton. HTTP-контракт эндпоинтов — в
[api.md](api.md), здесь — модель, потоки и жизненный цикл токенов. Документ написан
**до кода** (намерение); расхождение с реализацией — сигнал проблемы.

**Скоуп инкремента:** жилец/владелец (телефон → SMS OTP → JWT) и УК-админ
(email+пароль+TOTP) + RBAC-энфорсмент на защищённых эндпоинтах.
**Вне скоупа:** гости/курьеры и инвайт-ссылки, реальный SMS-провайдер (сейчас мок),
SystemAdmin, полноценная админка УК (только вход), CSRF-токен, одноразовый SSE-тикет.

---

## 1. Роли и модель доступа

| Роль (`kind`) | Вход | Может |
|---|---|---|
| `resident` | телефон → OTP | принять звонок и открыть дверь **своей** квартиры, слушать SSE своей квартиры |
| `owner` | телефон → OTP | то же + `can_create_guests` (задел; создание гостей — вне скоупа) |
| `mc_admin` | email + пароль + TOTP | список устройств и аудит **своей УК** (`management_company_id`) |
| посетитель | — (без аккаунта) | публичный флоу: `qr/validate`, `calls/initiate`, `cancel`, `end` |

**RBAC-правила (энфорсмент):**
- `resident`/`owner`: `POST /calls/{id}/accept`, `POST /access/open`, `GET /resident/events` —
  доменная проверка «`apartment_id` сессии звонка ∈ квартиры пользователя» (иначе `403 FORBIDDEN`).
- `mc_admin`: `GET /devices`, `GET /audit/events` — выборки с `WHERE management_company_id = mc_id` из claims.
- Публичные (`qr/validate`, `calls/initiate`, `cancel`, `end`, `/health`, `/auth/*`) — без токена.
- `cancel`/`end` авторизуются **обладанием `call_id`** (128-битная неугадываемая capability, TTL 120с):
  посетитель без аккаунта должен уметь отменять/завершать свой звонок.

---

## 2. Схема БД (миграция 0003_auth.sql)

**users**
| Поле | Тип | Описание |
|---|---|---|
| id | uuid PK | — |
| phone | text UNIQUE NULL | жилец/владелец |
| email | text UNIQUE NULL | админ УК |
| password_hash | text NULL | bcrypt (cost 12), только админ |
| totp_secret | text NULL | base32, только админ |
| kind | text CHECK(`resident`/`owner`/`mc_admin`) | тип пользователя |
| management_company_id | uuid NULL FK | для админа; у жильца mc берётся из ролей |
| created_at | timestamptz | — |

`CHECK(phone IS NOT NULL OR email IS NOT NULL)`. В Postgres несколько `NULL` в UNIQUE-колонке
не конфликтуют — поэтому `phone`/`email` могут быть NULL у разных типов.

**user_apartment_roles**
| Поле | Тип | Описание |
|---|---|---|
| id | uuid PK | — |
| user_id | uuid FK→users ON DELETE CASCADE | — |
| apartment_id | uuid FK→apartments | — |
| management_company_id | uuid FK | денормализация для скоупа |
| role | text CHECK(`owner`/`resident`) | — |
| can_create_guests | bool DEFAULT false | делегируется владельцем (задел) |
| created_by | uuid NULL FK→users | кто назначил |
| created_at | timestamptz | — |

`UNIQUE(user_id, apartment_id)`, `INDEX(user_id)`. Один пользователь — несколько квартир (ТЗ §2.2.7).

**Refresh-токены** — НЕ в БД, в Redis (см. §4).

---

## 3. JWT (RS256)

**Claims access-токена:**
```json
{ "sub": "<user_id>", "kind": "resident|owner|mc_admin",
  "roles": [{ "apartment_id": "<uuid>", "role": "resident|owner", "can_create_guests": false }],
  "mc_id": "<uuid|null>", "jti": "<uuid>", "iat": 0, "exp": 0, "typ": "access|refresh" }
```
У `mc_admin`: `roles` пуст, `mc_id` = UUID УК.

**Жизненный цикл:**
- **Access** — RS256, TTL **15 мин**, stateless (валидация только публичным ключом, без Redis на горячем пути → защищённые эндпоинты переживают кратковременную недоступность Redis).
- **Refresh** — RS256, TTL **30 дней**, `jti` в Redis whitelist `auth:refresh:{jti} → user_id` (TTL 30д). Только HttpOnly-cookie, в теле ответов не фигурирует.
- **Ротация** на `/auth/refresh`: проверить `jti` в whitelist → `DEL` старый → выдать новый access+refresh → `SET` новый `jti`. Повторное использование украденного старого refresh (его `jti` уже удалён) → нет в whitelist → `401` (детект reuse).
- **Logout** — `DEL auth:refresh:{jti}` + очистка cookie.
- **Fail-closed:** Redis недоступен на `refresh`/`logout` → токены не выдаём (401/500). Access при этом продолжает валидироваться (stateless) до истечения 15 мин.

**Whitelist, а не blacklist:** ограниченный размер (1 ключ на активную сессию, авто-TTL), единая точка отзыва, детект reuse — согласуется с Redis-паттерном проекта (call-сессии, presence).

**Ключи RS256:** `JWT_PRIVATE_KEY` / `JWT_PUBLIC_KEY` (PEM в env).
- **dev:** фиксированный keypair в `.env.example` (токены переживают рестарт backend — удобно, часто перезапускаем). Помечен «НЕ ДЛЯ ПРОД».
- **prod:** ключи из env/секрет-стора; при пустых ключах в prod-режиме — **fail-closed** (сервер не стартует).

---

## 4. OTP (жилец/владелец)

- Код: 6 цифр. Хранилище Redis: `otp:{phone} = {code, attempts}`, TTL **5 мин**.
- **Rate-limit запросов:** `otp:req:{phone}` — не более **3 запросов / 10 мин** (иначе 429).
- **Блокировка:** на **5-й неверной** попытке → `blocked:{phone}` TTL **30 мин** (ТЗ §12.4), в это время verify/send → 429.
- **OtpSender** — интерфейс; dev-реализация `DevSender` логирует код и возвращает его в `dev_code` (в prod поля нет). Реальный SMS-провайдер — следующий инкремент.
- Ответ `otp/send` не раскрывает существование номера (всегда `{sent:true}` при отсутствии троттла).

---

## 5. Потоки

**Логин жильца:** `POST /auth/otp/send {phone}` → (dev) код в ответе → `POST /auth/otp/verify {phone,code}` → 200 `{access_token, expires_in:900, user}` + Set-Cookie refresh. Нет пользователя с телефоном → аккаунт создаётся при первом успешном verify? **Нет** (вне скоупа онбординг) — телефон должен существовать в `users` (сид); неизвестный номер → 401.

**Логин админа:** `POST /auth/admin/login {email, password, totp_code}` (одношагово) → bcrypt-сверка пароля + TOTP-валидация → 200 `{access_token, user}` + refresh cookie. Любая неверная часть → 401 без указания какая.

**Refresh:** `POST /auth/refresh` (refresh из cookie) → ротация → 200 новый access + новый refresh cookie.

**RBAC на accept/open:** middleware `Authenticator` (валидация JWT) → `RequireResident` (роль) → сервис через интерфейс `Authorizer.AllowApartment(ctx, sess.ApartmentID)` сверяет квартиру сессии с квартирами из claims → иначе 403.

**Admin-скоуп:** `RequireAdmin` → `devices.List(ctx, mc_id)` / `audit.List(ctx, mc_id, limit)` фильтруют по `management_company_id` из claims.

**SSE:** `EventSource` не шлёт заголовки → access-токен передаётся в query `?token=<access>`. Хаб рассылает события только подписчикам соответствующей квартиры (per-apartment). Наш AccessLog логирует только `Path` (без query) → токен не утекает в наши логи; митигация усилена коротким TTL (15 мин). Одноразовый SSE-тикет — hardening на будущее.

---

## 6. Middleware и коды ошибок

- `Authenticator` — парсит `Authorization: Bearer` (для SSE — `?token=`), валидирует RS256, кладёт claims в context; нет/невалиден → `401 UNAUTHORIZED`.
- `RequireResident` (`kind ∈ {resident, owner}`) / `RequireAdmin` (`kind = mc_admin`) → недостаточно роли → `403 FORBIDDEN`.
- Новые коды: `UNAUTHORIZED`→401, `FORBIDDEN`→403. OTP/login троттл → `RATE_LIMIT`→429.
- **Алиасы ТЗ §13.4:** `TOKEN_EXPIRED` — частный случай `UNAUTHORIZED`; `ACCESS_DENIED` — синоним `FORBIDDEN`.

---

## 7. Канонические auth-фикстуры (dev, миграция 0004_auth_seed.sql — НЕ ДЛЯ ПРОД)

| Объект | UUID | Данные |
|---|---|---|
| user resident | `77777777-7777-7777-7777-777777777777` | phone `+77010000001` |
| user owner | `88888888-8888-8888-8888-888888888888` | phone `+77010000002` |
| user mc_admin | `99999999-9999-9999-9999-999999999999` | email `admin@demo.example`, mc `1111…`, пароль `admin-demo-123`, TOTP-secret `JBSWY3DPEHPK3PXP` |
| роль resident | `aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa` | user7777 / apt `3333…` / `resident` / can_create_guests=false |
| роль owner | `bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb` | user8888 / apt `3333…` / `owner` / can_create_guests=true |

Квартира/УК — из [architecture.md](architecture.md) §5 (кв. `3333…`, УК `1111…`). TOTP dev-secret
`JBSWY3DPEHPK3PXP` заведите в приложение-аутентификатор (или вычислите код по нему в тесте).

---

## 8. Фронт (кратко)

`AuthProvider` + module-level `tokenStore` (access в памяти). `ProtectedRoute` для `/resident`:
нет сессии → тихий `POST /auth/refresh` по cookie → провал → `/login`. `LoginPage` — переключатель
Жилец (телефон→OTP) / Администратор (email+пароль+TOTP). `request()` подставляет `Bearer`, на 401 —
single-flight refresh + ретрай, повторный 401 → разлогин. Refresh — HttpOnly-cookie (браузер шлёт сам).
`ApiErrorCode += UNAUTHORIZED | FORBIDDEN | RATE_LIMIT`.
