-- +goose Up
-- Онбординг жильцов по одноразовым инвайт-ссылкам + постоянные гранты доступа
-- (онбординг + гранты). Секрет ссылки хранится только как SHA-256-хеш.

-- Одноразовые инвайты. target_kind различает онбординг в квартиру
-- (apartment_owner/apartment_resident) и выдачу гранта на точку (access_grant).
CREATE TABLE invites (
    id                    uuid PRIMARY KEY,
    token_hash            text NOT NULL UNIQUE,
    phone                 text,
    target_kind           text NOT NULL CHECK (target_kind IN ('apartment_owner', 'apartment_resident', 'access_grant')),
    apartment_id          uuid REFERENCES apartments (id),
    access_point_id       uuid REFERENCES access_points (id),
    role                  text CHECK (role IN ('owner', 'resident')),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    created_by            uuid NOT NULL REFERENCES users (id),
    expires_at            timestamptz NOT NULL,
    used_at               timestamptz,
    used_by               uuid REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- Форма записи зависит от target_kind: онбординг в квартиру требует
    -- apartment_id + role (без access_point_id); грант — access_point_id
    -- (без apartment_id/role).
    CONSTRAINT invites_shape CHECK (
        (target_kind IN ('apartment_owner', 'apartment_resident')
            AND apartment_id IS NOT NULL AND access_point_id IS NULL AND role IS NOT NULL)
        OR
        (target_kind = 'access_grant'
            AND access_point_id IS NOT NULL AND apartment_id IS NULL AND role IS NULL)
    )
);

CREATE INDEX invites_management_company_id_idx ON invites (management_company_id);

-- Постоянные гранты доступа пользователя на точку (результат активации инвайта
-- target_kind='access_grant'). Один грант на пару (user, access_point).
CREATE TABLE user_access_grants (
    id                    uuid PRIMARY KEY,
    user_id               uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    access_point_id       uuid NOT NULL REFERENCES access_points (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    granted_by            uuid REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, access_point_id)
);

CREATE INDEX user_access_grants_user_id_idx ON user_access_grants (user_id);

-- Сид точки-калитки и её устройства (эмулятор EMU-002) для сценария грантов.
INSERT INTO access_points (id, public_id, building_id, management_company_id, type, label, fail_mode, is_active) VALUES
    ('cccccccc-cccc-cccc-cccc-cccccccccccc',
     'eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee',
     '22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     'gate', 'Калитка двора', 'open', true);

INSERT INTO devices (id, serial, access_point_id, management_company_id, mqtt_client_id, type, firmware_version) VALUES
    ('dddddddd-dddd-dddd-dddd-dddddddddddd',
     'EMU-002',
     'cccccccc-cccc-cccc-cccc-cccccccccccc',
     '11111111-1111-1111-1111-111111111111',
     'device:EMU-002', 'emulator', 'emu-0.1.0');

-- +goose Down
-- Обратный порядок FK: сначала device, потом access_point.
DELETE FROM devices WHERE id = 'dddddddd-dddd-dddd-dddd-dddddddddddd';
DELETE FROM access_points WHERE id = 'cccccccc-cccc-cccc-cccc-cccccccccccc';
DROP TABLE user_access_grants;
DROP TABLE invites;
