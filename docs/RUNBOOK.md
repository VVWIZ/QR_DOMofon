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

**Auth-режим:** `AUTH_DEV_MODE` по умолчанию **false** (secure-by-default). Для dev
нужен `.env` рядом с местом запуска (`backend/`, откуда `godotenv` его читает):

```powershell
cp .env.example backend/.env    # AUTH_DEV_MODE=true + dev-keypair RS256
cd backend
go run ./cmd/server
```

Без `backend/.env` (и без реальных ключей) сервер не стартует — fail-closed по auth.
Миграции (`0001_init`, `0002_seed`, `0003_auth`, `0004_auth_seed`) применяются
автоматически при старте.

Проверка: `curl http://localhost:8080/health` → `{"deps":{"livekit":"ok","mqtt":"ok","postgres":"ok","redis":"ok"},"status":"ok"}`.

**Прод:** `AUTH_DEV_MODE=false` + реальные `JWT_PRIVATE_KEY`/`JWT_PUBLIC_KEY` (RS256 PEM)
из KMS/секрет-стора; `sslmode=require` в `DATABASE_URL`. Dev-keypair из `.env.example` —
**скомпрометирован** (в репозитории), в проде заменить.

**Dev-креды для входа** (сид `0004_auth_seed`, auth.md §7):
- жилец: телефон `+77010000001` (owner: `+77010000002`) → OTP-код возвращается в
  `dev_code` (dev-режим);
- УК-админ: `admin@demo.example` / пароль `admin-demo-123` / TOTP-secret `JBSWY3DPEHPK3PXP`
  (завести в приложение-аутентификатор).
- **Платформенный админ (SystemAdmin):** `sa@demo.example` / пароль `sysadmin-demo-123` / тот же
  dev TOTP-secret `JBSWY3DPEHPK3PXP`. Вход через ту же вкладку «Администратор» — редиректит в `/system`.
  Создаёт УК, объекты (ЖК), дома, подъезды, УК-админов. В проде — отдельный INSERT с локально
  сгенерённым bcrypt-хешем и уникальным TOTP-секретом (dev-сид `0009` НЕ ДЛЯ ПРОД).

**`VISITOR_BASE_URL`** (опционально, дефолт `http://localhost:5173`) — база инвайт-ссылок
онбординга: ответ API отдаёт `{VISITOR_BASE_URL}/invite/{token}`. Менять, если фронт не на :5173.

Миграции: `0001_init`, `0002_seed`, `0003_auth`, `0004_auth_seed`, `0005_onboarding` — применяются
автоматически при старте.

---

## 4. Эмулятор устройства

```powershell
cd firmware\emulator
go build -ldflags="-s -w" -o emulator.exe .   # -ldflags обходит ложный детект Kaspersky
.\emulator.exe
```

Проверка: `curl http://localhost:8080/api/v1/devices` → `"status":"online"` (heartbeat дошёл).

> Без `-ldflags="-s -w"` Kaspersky выдаёт `Access is denied` при запуске бинаря.

**Второе устройство — калитка (EMU-002)** нужно для сценария прямого открытия по гранту
(`POST /access/open-point`, инкремент онбординга). Это тот же бинарь с другими флагами — запускать
**во втором окне**, параллельно с EMU-001:

```powershell
.\emulator.exe -device-id dddddddd-dddd-dddd-dddd-dddddddddddd -device-serial EMU-002
```

Точка `Калитка двора` (`public_id = eeeeeeee-…`, type `gate`) и устройство EMU-002 приезжают
сидом миграции `0005_onboarding.sql`.

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

**Платформенная админка:** `http://localhost:5173/system` — вход `sa@demo.example` (см. §3). Список УК +
создание; выбор УК → дерево объект→дом→подъезд + формы создания объекта/дома/подъезда/УК-админа
(otpauth-ключ нового админа показывается один раз).

**УК-консоль:** `http://localhost:5173/admin` — вход админом (`/login` → вкладка «Администратор»).
В шапке — выпадашка объектов и кнопка «+ Владелец» (композитная форма: дом→подъезд→квартира + ФИО +
чекбоксы калиток/шлагбаумов → инвайт-ссылка, админ **копирует и сам пересылает** адресату). Ниже —
**матрица доступа** выбранного объекта: строки-владельцы × столбцы-точки с чекбоксами (дать/забрать
грант мгновенно); фильтр и вкладки Все/Калитки/Шлагбаумы; клик по владельцу раскрывает жильцов
квартиры с их чекбоксами.

> **Резидентские флоу онбординга** (приём инвайта, открытие калиток, приглашение жильца) на вебе
> НЕ реализованы: по ТЗ §14 это мобильное приложение. Проверять их — через API (см. `api.md`
> §Онбординг), напр. `POST /api/v1/auth/invite/accept {"token":"…"}`.

**Гостевая страница:** `http://localhost:5173/g/{token}` — публичная (без входа), по ссылке из
`POST /api/v1/apartments/{id}/guests`. Показывает имя гостя, окно действия и кнопки открытия точек
(подъезд/калитка/шлагбаум). Создание гостя — API-first (у создателя в проде мобильное приложение).
Для сценария открытия гостем нужны запущенные эмуляторы соответствующих точек (EMU-001 подъезд,
EMU-002 калитка).

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
