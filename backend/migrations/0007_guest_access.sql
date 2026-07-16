-- +goose Up
-- Инкремент B — гостевой доступ (ТЗ §2.2.8, §3.1.5, §4.6). Гость без аккаунта:
-- аутентификация по токену ссылки /g/{token} (bearer-capability, риск принят
-- заказчиком 2026-07-06). Доступ ПРОИЗВОДНЫЙ от прав создателя (проверяется на
-- каждом открытии — УК изъяла грант у владельца → гость сразу теряет), ограничен
-- окном времени ≤ 2 дней. Секрет ссылки в БД не хранится — только SHA-256.

CREATE TABLE guest_access (
    id                    uuid PRIMARY KEY,
    token_hash            text NOT NULL UNIQUE,
    full_name             text NOT NULL, -- имя гостя
    apartment_id          uuid NOT NULL REFERENCES apartments (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    -- Создатель (владелец/жилец с правом). Доступ производный → при удалении
    -- создателя гость обязан исчезнуть.
    created_by            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    valid_from            timestamptz NOT NULL DEFAULT now(),
    valid_to              timestamptz NOT NULL,
    revoked_at            timestamptz,
    revoked_by            uuid REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- Окно корректно и не длиннее 2 дней (ТЗ §3.5). Инвариант на уровне БД;
    -- человекочитаемая проверка дублируется в сервисе (VALIDATION_ERROR вместо 500).
    CONSTRAINT guest_access_window_valid CHECK (valid_to > valid_from),
    CONSTRAINT guest_access_window_max CHECK (valid_to <= valid_from + interval '2 days')
);

CREATE INDEX guest_access_created_by_idx ON guest_access (created_by);
CREATE INDEX guest_access_apartment_id_idx ON guest_access (apartment_id);
CREATE INDEX guest_access_management_company_id_idx ON guest_access (management_company_id);

-- Точки, на которые распространяется гостевой доступ (снимок набора; фактическое
-- право переспрашивается на открытии по правам создателя).
CREATE TABLE guest_access_points (
    guest_access_id       uuid NOT NULL REFERENCES guest_access (id) ON DELETE CASCADE,
    access_point_id       uuid NOT NULL REFERENCES access_points (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    PRIMARY KEY (guest_access_id, access_point_id)
);

-- +goose Down
DROP TABLE guest_access_points;
DROP TABLE guest_access;
