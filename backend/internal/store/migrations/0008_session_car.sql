-- +goose Up
ALTER TABLE sessions ADD COLUMN car_id integer NOT NULL DEFAULT -1;

-- +goose Down
ALTER TABLE sessions DROP COLUMN car_id;
