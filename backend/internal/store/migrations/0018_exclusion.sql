-- +goose Up
-- A person can leave laps out of the models. A lap has its own flag: null follows its session, true leaves the lap out,
-- false takes it in even though its session is left out.
ALTER TABLE laps ADD COLUMN excluded boolean;
ALTER TABLE sessions ADD COLUMN excluded boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE sessions DROP COLUMN excluded;
ALTER TABLE laps DROP COLUMN excluded;
