-- +goose Up
-- Роль SystemAdmin (платформенная админка нашей компании) + dev-сид. НЕ ДЛЯ ПРОД.
--
-- system_admin создаёт УК, объекты, дома, подъезды; НЕ ограничен
-- management_company_id (видит все УК). Вход — тем же /auth/admin/login
-- (email+пароль+TOTP), различение по kind. У system_admin: mc_id = NULL.

-- Расширяем допустимые kind (CHECK создан inline в 0003 → автоимя users_kind_check).
ALTER TABLE users DROP CONSTRAINT users_kind_check;
ALTER TABLE users ADD CONSTRAINT users_kind_check
    CHECK (kind IN ('resident', 'owner', 'mc_admin', 'system_admin'));

-- Dev-сид платформенного админа. Пароль: 'sysadmin-demo-123' → bcrypt (cost 12).
-- TOTP-секрет (base32): 'JBSWY3DPEHPK3PXP' (тот же dev-секрет, что у mc_admin —
-- ТОЛЬКО для локальной разработки). В проде — отдельный INSERT с локально
-- сгенерённым хешем и уникальным секретом (см. RUNBOOK).
INSERT INTO users (id, phone, email, password_hash, totp_secret, kind, management_company_id) VALUES
    ('5a5a5a5a-5a5a-5a5a-5a5a-5a5a5a5a5a5a', NULL, 'sa@demo.example',
     '$2a$12$nqMt7XdVccZzd54KYxdKLOHtGvJPkOdGuhMthS7QJ8Rg.ipCiZ16q',
     'JBSWY3DPEHPK3PXP', 'system_admin', NULL);

-- +goose Down
DELETE FROM users WHERE id = '5a5a5a5a-5a5a-5a5a-5a5a-5a5a5a5a5a5a';
ALTER TABLE users DROP CONSTRAINT users_kind_check;
ALTER TABLE users ADD CONSTRAINT users_kind_check
    CHECK (kind IN ('resident', 'owner', 'mc_admin'));
