-- +goose Up
ALTER TABLE stops ADD COLUMN stopped   double precision NOT NULL DEFAULT 0;
ALTER TABLE stops ADD COLUMN work      double precision NOT NULL DEFAULT 0;
ALTER TABLE stops ADD COLUMN fuel_time double precision NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE stops DROP COLUMN fuel_time;
ALTER TABLE stops DROP COLUMN work;
ALTER TABLE stops DROP COLUMN stopped;
