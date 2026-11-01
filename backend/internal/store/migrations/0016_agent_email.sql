-- +goose Up
-- The member an agent belongs to; the empty string means none. Several agents may have the same address.
ALTER TABLE agents ADD COLUMN email text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agents DROP COLUMN email;
