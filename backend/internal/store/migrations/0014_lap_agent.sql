-- +goose Up
-- The agent whose token delivered the lap. Null for laps from before this column and for the shared dev token.
ALTER TABLE laps ADD COLUMN agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE laps DROP COLUMN agent_id;
