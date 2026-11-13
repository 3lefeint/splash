-- +goose Up
-- The agent that delivered a row, as laps already record (0014). Null for rows from before this column and for the shared
-- dev token. standing_agent_id is the agent whose stand-still times a stop holds: the first one to report it.
ALTER TABLE stops ADD COLUMN agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;
ALTER TABLE stops ADD COLUMN standing_agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;
ALTER TABLE lap_conditions ADD COLUMN agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;
ALTER TABLE wear_laps ADD COLUMN agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;
ALTER TABLE weather_points ADD COLUMN agent_id bigint REFERENCES agents (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE weather_points DROP COLUMN agent_id;
ALTER TABLE wear_laps DROP COLUMN agent_id;
ALTER TABLE lap_conditions DROP COLUMN agent_id;
ALTER TABLE stops DROP COLUMN standing_agent_id;
ALTER TABLE stops DROP COLUMN agent_id;
