-- +goose Up
-- Инкремент C — сущность «Объект» (site) между УК и домом: Платформа → УК →
-- Объект(ЖК/отдельный объект) → дом → подъезд → квартира. Калитки/шлагбаумы
-- крепятся к объекту (общие для ЖК: двор/парковка/въезд), подъездные домофоны
-- (entrance) — к дому/подъезду.
--
-- `site_id` ставим сразу NOT NULL: в системе НЕТ код-путей, создающих
-- mc/buildings/access_points (всё seed-only), поэтому contract-фазу можно не
-- растягивать. Работающие резолверы (property.ResolveByPublicID джойнит
-- building_id для entrance/QR/звонков; гранты/гости — по access_points.id/mc) не
-- затрагиваются: building_id у существующих точек НЕ обнуляется.
--
-- Маппинг на ТЗ: site ≈ §2.2.2 ResidentialComplex (ЖК), но шире — вмещает и
-- отдельный объект (kind='standalone'); kind — только ярлык для UI, без ветвлений.

-- Идемпотентность создания УК (повтор имени → конфликт).
ALTER TABLE management_companies ADD CONSTRAINT management_companies_name_uniq UNIQUE (name);

CREATE TABLE sites (
    id                    uuid PRIMARY KEY,
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    name                  text NOT NULL,
    address               text NOT NULL,
    kind                  text NOT NULL DEFAULT 'complex' CHECK (kind IN ('complex', 'standalone')),
    is_active             boolean NOT NULL DEFAULT true,
    created_at            timestamptz NOT NULL DEFAULT now(),
    UNIQUE (management_company_id, name),   -- идемпотентность создания объекта
    UNIQUE (id, management_company_id)       -- опора композитных FK (паттерн 0006)
);
CREATE INDEX sites_mc_idx ON sites (management_company_id);

-- Сид демо-объекта для существующей УК/дома (читаемый UUID, паттерн 0002).
INSERT INTO sites (id, management_company_id, name, address, kind) VALUES
    ('e5e5e5e5-e5e5-e5e5-e5e5-e5e5e5e5e5e5',
     '11111111-1111-1111-1111-111111111111',
     'Демо ЖК', 'ул. Демонстрационная', 'complex');

-- Прод-safe backfill: дефолт-объект каждой УК, у которой есть дома, но нет объекта
-- (на dev — no-op, демо-объект уже создан выше).
INSERT INTO sites (id, management_company_id, name, address, kind)
SELECT gen_random_uuid(), b.management_company_id, 'Объект по умолчанию', min(b.address), 'standalone'
FROM buildings b
WHERE NOT EXISTS (SELECT 1 FROM sites s WHERE s.management_company_id = b.management_company_id)
GROUP BY b.management_company_id;

-- Дом → объект. Композитный FK гардит «дом чужой УК в объекте».
ALTER TABLE buildings ADD COLUMN site_id uuid;
ALTER TABLE buildings ADD CONSTRAINT buildings_site_fk
    FOREIGN KEY (site_id, management_company_id) REFERENCES sites (id, management_company_id);
UPDATE buildings b SET site_id = (
    SELECT s.id FROM sites s WHERE s.management_company_id = b.management_company_id
    ORDER BY s.created_at LIMIT 1
) WHERE b.site_id IS NULL;
ALTER TABLE buildings ALTER COLUMN site_id SET NOT NULL;   -- писателей нет — безопасно
ALTER TABLE buildings ADD CONSTRAINT buildings_site_address_uniq UNIQUE (site_id, address);
CREATE INDEX buildings_site_id_idx ON buildings (site_id);

-- Точка → объект (для gate/barrier site — авторитет). building_id становится
-- nullable (site-level калитка может не иметь дома), но entrance обязан иметь дом.
ALTER TABLE access_points ALTER COLUMN building_id DROP NOT NULL;
ALTER TABLE access_points ADD COLUMN site_id uuid;
ALTER TABLE access_points ADD CONSTRAINT access_points_site_fk
    FOREIGN KEY (site_id, management_company_id) REFERENCES sites (id, management_company_id);
UPDATE access_points ap SET site_id = b.site_id FROM buildings b WHERE b.id = ap.building_id;
ALTER TABLE access_points ALTER COLUMN site_id SET NOT NULL;
ALTER TABLE access_points ADD CONSTRAINT access_points_shape
    CHECK (type <> 'entrance' OR building_id IS NOT NULL);
CREATE INDEX access_points_site_id_idx ON access_points (site_id);

-- +goose Down
ALTER TABLE access_points DROP CONSTRAINT access_points_shape;
ALTER TABLE access_points DROP CONSTRAINT access_points_site_fk;
DROP INDEX access_points_site_id_idx;
ALTER TABLE access_points DROP COLUMN site_id;
ALTER TABLE access_points ALTER COLUMN building_id SET NOT NULL;

ALTER TABLE buildings DROP CONSTRAINT buildings_site_address_uniq;
ALTER TABLE buildings DROP CONSTRAINT buildings_site_fk;
DROP INDEX buildings_site_id_idx;
ALTER TABLE buildings DROP COLUMN site_id;

DROP TABLE sites;
ALTER TABLE management_companies DROP CONSTRAINT management_companies_name_uniq;
