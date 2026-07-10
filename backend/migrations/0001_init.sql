-- +goose Up
-- Схема walking skeleton (architecture.md §3). Полная иерархия ТЗ §2 с
-- management_company_id во всех tenant-таблицах (под будущий RLS). ЖК опущен —
-- buildings ссылается сразу на УК. Presence/звонки в БД НЕ хранятся (Redis).

CREATE TABLE management_companies (
    id         uuid PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE buildings (
    id                    uuid PRIMARY KEY,
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    address               text NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE apartments (
    id                    uuid PRIMARY KEY,
    building_id           uuid NOT NULL REFERENCES buildings (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    number                text NOT NULL,
    is_active             boolean NOT NULL DEFAULT true
);

CREATE TABLE access_points (
    id                    uuid PRIMARY KEY,
    public_id             uuid NOT NULL UNIQUE,
    building_id           uuid NOT NULL REFERENCES buildings (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    type                  text NOT NULL CHECK (type IN ('entrance', 'gate', 'barrier')),
    label                 text NOT NULL,
    fail_mode             text NOT NULL DEFAULT 'open',
    is_active             boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now()
);

-- Связь device 1:1 access_point (UNIQUE FK на стороне ребёнка, ТЗ §2.2.5).
-- Колонки status НЕТ — статус online/offline derived из Redis-presence.
CREATE TABLE devices (
    id                    uuid PRIMARY KEY,
    serial                text NOT NULL UNIQUE,
    access_point_id       uuid NOT NULL UNIQUE REFERENCES access_points (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    mqtt_client_id        text NOT NULL,
    type                  text NOT NULL,
    firmware_version      text NOT NULL,
    last_seen_at          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now()
);

-- Реестр ключей подписи QR (ротация по kid, ТЗ §5.3).
CREATE TABLE qr_keys (
    kid        text PRIMARY KEY,
    secret     text NOT NULL,
    is_active  boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Append-only журнал (architecture.md §3): код выполняет только INSERT/SELECT.
CREATE TABLE audit_events (
    id                    bigserial PRIMARY KEY,
    event_type            text NOT NULL,
    occurred_at           timestamptz NOT NULL DEFAULT now(),
    actor                 text,
    apartment_id          uuid,
    access_point_id       uuid,
    device_id             uuid,
    call_id               uuid,
    request_id            uuid,
    management_company_id uuid,
    metadata              jsonb
);

CREATE INDEX idx_audit_events_id_desc ON audit_events (id DESC);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS qr_keys;
DROP TABLE IF EXISTS devices;
DROP TABLE IF EXISTS access_points;
DROP TABLE IF EXISTS apartments;
DROP TABLE IF EXISTS buildings;
DROP TABLE IF EXISTS management_companies;
