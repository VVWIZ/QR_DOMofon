-- +goose Up
-- Инкремент E — авто-открытие точек по расписанию (ТЗ «время работы» калиток/
-- шлагбаумов). Одно окно = одна строка (день недели + opens..closes в таймзоне
-- точки). Планировщик держит реле открытым АРЕНДОЙ (короткий duration_ms,
-- переиздаётся каждый тик): отказ планировщика/сервера → реле закрывается само
-- (fail-secure). Расписания — только на gate/barrier (подъездные домофоны так не
-- управляются).

CREATE TABLE access_point_schedules (
    id                    uuid PRIMARY KEY,
    access_point_id       uuid NOT NULL REFERENCES access_points (id) ON DELETE CASCADE,
    management_company_id uuid NOT NULL REFERENCES management_companies (id),
    -- День недели: 0=воскресенье … 6=суббота (совпадает с Go time.Weekday и
    -- Postgres EXTRACT(DOW)).
    dow                   smallint NOT NULL CHECK (dow BETWEEN 0 AND 6),
    opens                 time NOT NULL,
    closes                time NOT NULL,
    -- IANA-таймзона точки (напр. 'Asia/Almaty'); окно считается в ней, DST — сам.
    timezone              text NOT NULL,
    is_active             boolean NOT NULL DEFAULT true,
    created_by            uuid REFERENCES users (id),
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- v1: окно в пределах суток (через полночь — отдельными строками при надобности).
    CONSTRAINT schedule_window_valid CHECK (closes > opens)
);

CREATE INDEX access_point_schedules_point_idx ON access_point_schedules (access_point_id);
CREATE INDEX access_point_schedules_mc_idx ON access_point_schedules (management_company_id);

-- +goose Down
DROP TABLE access_point_schedules;
