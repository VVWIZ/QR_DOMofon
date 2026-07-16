-- +goose Up
-- Инкремент A (подъезды + ФИО + композитный инвайт УК).
--
-- Всё аддитивно (expand-фаза expand-contract): новые таблицы + NULLABLE-колонки
-- без DEFAULT → без rewrite, работающий скелет (property.ResolveByPublicID,
-- QR/звонки — джойн по building_id) не затрагивается. Перевод резолва на подъезды
-- и NOT NULL entrance_id — долг инкремента звонковой логики (contract-фаза).
--
-- Примечание к ТЗ: сущности Entrance в §2.2 буквально нет (только в диаграмме
-- иерархии §2.1) — таблица entrances это осознанное расширение ТЗ (подъезд ≠
-- точка доступа: существует без устройства, точек у подъезда может быть >1).

-- Подъезды: building → entrance → apartment/access_point.
CREATE TABLE entrances (
    id                    uuid PRIMARY KEY,
    building_id           uuid NOT NULL REFERENCES buildings (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    number                text NOT NULL,
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (building_id, number),
    -- Опора для композитных FK ниже (гард «подъезд чужого дома»).
    UNIQUE (id, building_id)
);

CREATE INDEX entrances_building_id_idx ON entrances (building_id);

-- Привязка квартиры/точки к подъезду. Композитный FK (entrance_id, building_id)
-- не даёт указать на подъезд ЧУЖОГО дома; при entrance_id IS NULL (MATCH SIMPLE)
-- проверка пропускается — back-compat со старыми строками бесплатный.
ALTER TABLE apartments
    ADD COLUMN entrance_id uuid,
    ADD CONSTRAINT apartments_entrance_fk
        FOREIGN KEY (entrance_id, building_id) REFERENCES entrances (id, building_id);
CREATE INDEX apartments_entrance_id_idx ON apartments (entrance_id);

ALTER TABLE access_points
    ADD COLUMN entrance_id uuid,
    ADD CONSTRAINT access_points_entrance_fk
        FOREIGN KEY (entrance_id, building_id) REFERENCES entrances (id, building_id);
CREATE INDEX access_points_entrance_id_idx ON access_points (entrance_id);

-- ФИО (заполняет создатель инвайта). NULLABLE — сид-пользователи без имени.
ALTER TABLE users ADD COLUMN full_name text;
ALTER TABLE invites ADD COLUMN full_name text;

-- Композитный инвайт: доп. гранты на калитки/шлагбаумы к owner/resident-инвайту
-- (одна ссылка = квартира + N точек). Чистый линк — суррогатный id не нужен.
-- Правило «доп. точки допустимы только у apartment_owner/apartment_resident
-- инвайтов» на уровне БД дёшево не выразить — гарантируется сервисом.
CREATE TABLE invite_access_points (
    invite_id             uuid NOT NULL REFERENCES invites (id) ON DELETE CASCADE,
    access_point_id       uuid NOT NULL REFERENCES access_points (id),
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    PRIMARY KEY (invite_id, access_point_id)
);

-- Сид: подъезд №1 для дома 2222; backfill существующих квартиры 3333 и
-- точки-подъезда 4444. Калитка cccc (0005) — building-level, к подъезду не вяжется.
INSERT INTO entrances (id, building_id, management_company_id, number) VALUES
    ('a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0',
     '22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     '1');

UPDATE apartments SET entrance_id = 'a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0'
    WHERE id = '33333333-3333-3333-3333-333333333333';
UPDATE access_points SET entrance_id = 'a0a0a0a0-a0a0-a0a0-a0a0-a0a0a0a0a0a0'
    WHERE id = '44444444-4444-4444-4444-444444444444';

-- +goose Down
DROP TABLE invite_access_points;
ALTER TABLE invites DROP COLUMN full_name;
ALTER TABLE users DROP COLUMN full_name;
-- DROP COLUMN снимает и композитный FK, и данные-ссылки на entrances.
ALTER TABLE access_points DROP COLUMN entrance_id;
ALTER TABLE apartments DROP COLUMN entrance_id;
DROP TABLE entrances;
