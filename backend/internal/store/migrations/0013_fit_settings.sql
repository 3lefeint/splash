-- +goose Up
ALTER TABLE params ADD COLUMN fit_min_laps     integer          NOT NULL DEFAULT 5;
ALTER TABLE params ADD COLUMN fit_slow_percent double precision NOT NULL DEFAULT 8;
ALTER TABLE params ADD COLUMN fit_skip_fcy     boolean          NOT NULL DEFAULT true;
ALTER TABLE params ADD COLUMN fit_skip_wet     boolean          NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE params DROP COLUMN fit_skip_wet;
ALTER TABLE params DROP COLUMN fit_skip_fcy;
ALTER TABLE params DROP COLUMN fit_slow_percent;
ALTER TABLE params DROP COLUMN fit_min_laps;
