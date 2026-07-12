-- +goose Up
-- Auth-схема (auth.md §2). Пользователи (жилец/владелец по телефону, УК-админ по
-- email+пароль+TOTP) и их привязки к квартирам. Refresh-токены здесь НЕ хранятся
-- (whitelist в Redis, auth.md §3).

CREATE TABLE users (
    id                    uuid PRIMARY KEY,
    phone                 text UNIQUE,
    email                 text UNIQUE,
    password_hash         text,
    totp_secret           text,
    kind                  text NOT NULL CHECK (kind IN ('resident', 'owner', 'mc_admin')),
    management_company_id uuid REFERENCES management_companies (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- Жилец/владелец идентифицируются телефоном, админ — email; хотя бы одно.
    -- В Postgres несколько NULL в UNIQUE-колонке не конфликтуют.
    CONSTRAINT users_phone_or_email CHECK (phone IS NOT NULL OR email IS NOT NULL)
);

CREATE TABLE user_apartment_roles (
    id                    uuid PRIMARY KEY,
    user_id               uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    apartment_id          uuid NOT NULL REFERENCES apartments (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    role                  text NOT NULL CHECK (role IN ('owner', 'resident')),
    can_create_guests     boolean NOT NULL DEFAULT false,
    created_by            uuid REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- Один пользователь — несколько квартир (ТЗ §2.2.7), но не дважды одну.
    CONSTRAINT user_apartment_roles_uniq UNIQUE (user_id, apartment_id)
);

CREATE INDEX idx_user_apartment_roles_user ON user_apartment_roles (user_id);

-- +goose Down
DROP TABLE IF EXISTS user_apartment_roles;
DROP TABLE IF EXISTS users;
