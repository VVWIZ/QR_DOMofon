-- +goose Up
-- Канонические фикстуры (architecture.md §5). Единственный источник истины для
-- конфигов, тестов и демо. UUID «читаемые»: 1=УК, 2=дом, 3=квартира,
-- 4/5=точка доступа, 6=устройство. Секрет dev1 — ТОЛЬКО dev, в прод не тащить.

INSERT INTO management_companies (id, name) VALUES
    ('11111111-1111-1111-1111-111111111111', 'Демо УК');

INSERT INTO buildings (id, management_company_id, address) VALUES
    ('22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     'ул. Демонстрационная, 1');

INSERT INTO apartments (id, building_id, management_company_id, number, is_active) VALUES
    ('33333333-3333-3333-3333-333333333333',
     '22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     '1', true);

INSERT INTO access_points
    (id, public_id, building_id, management_company_id, type, label, fail_mode, is_active) VALUES
    ('44444444-4444-4444-4444-444444444444',
     '55555555-5555-5555-5555-555555555555',
     '22222222-2222-2222-2222-222222222222',
     '11111111-1111-1111-1111-111111111111',
     'entrance', 'Подъезд №1', 'open', true);

INSERT INTO devices
    (id, serial, access_point_id, management_company_id, mqtt_client_id, type, firmware_version) VALUES
    ('66666666-6666-6666-6666-666666666666',
     'EMU-001',
     '44444444-4444-4444-4444-444444444444',
     '11111111-1111-1111-1111-111111111111',
     'device:EMU-001', 'emulator', 'emu-0.1.0');

INSERT INTO qr_keys (kid, secret, is_active) VALUES
    ('dev1', 'dev-qr-secret-change-me', true);

-- +goose Down
DELETE FROM qr_keys WHERE kid = 'dev1';
DELETE FROM devices WHERE id = '66666666-6666-6666-6666-666666666666';
DELETE FROM access_points WHERE id = '44444444-4444-4444-4444-444444444444';
DELETE FROM apartments WHERE id = '33333333-3333-3333-3333-333333333333';
DELETE FROM buildings WHERE id = '22222222-2222-2222-2222-222222222222';
DELETE FROM management_companies WHERE id = '11111111-1111-1111-1111-111111111111';
