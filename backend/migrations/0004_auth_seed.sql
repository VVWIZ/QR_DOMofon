-- +goose Up
-- Канонические auth-фикстуры (auth.md §7). НЕ ДЛЯ ПРОД. Ссылаются на УК 1111 и
-- квартиру 3333 из 0002_seed. UUID пользователей/ролей не пересекаются с 0002.
--
-- Пароль админа: 'admin-demo-123' → bcrypt (cost 12).
-- TOTP-секрет админа (base32): 'JBSWY3DPEHPK3PXP'.

INSERT INTO users (id, phone, email, password_hash, totp_secret, kind, management_company_id) VALUES
    ('77777777-7777-7777-7777-777777777777', '+77010000001', NULL, NULL, NULL, 'resident', NULL),
    ('88888888-8888-8888-8888-888888888888', '+77010000002', NULL, NULL, NULL, 'owner', NULL),
    ('99999999-9999-9999-9999-999999999999', NULL, 'admin@demo.example',
     '$2a$12$Z1fqa3HRk7BE8HlRUo5pHeHzjROah4HFnCD/KkuCEZAFVu/kgRpH2',
     'JBSWY3DPEHPK3PXP', 'mc_admin', '11111111-1111-1111-1111-111111111111');

INSERT INTO user_apartment_roles
    (id, user_id, apartment_id, management_company_id, role, can_create_guests, created_by) VALUES
    ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
     '77777777-7777-7777-7777-777777777777',
     '33333333-3333-3333-3333-333333333333',
     '11111111-1111-1111-1111-111111111111',
     'resident', false, NULL),
    ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb',
     '88888888-8888-8888-8888-888888888888',
     '33333333-3333-3333-3333-333333333333',
     '11111111-1111-1111-1111-111111111111',
     'owner', true, NULL);

-- +goose Down
DELETE FROM user_apartment_roles WHERE id IN (
    'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa',
    'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb'
);
DELETE FROM users WHERE id IN (
    '77777777-7777-7777-7777-777777777777',
    '88888888-8888-8888-8888-888888888888',
    '99999999-9999-9999-9999-999999999999'
);
