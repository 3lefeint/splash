-- +goose Up
-- Overrides belong to the agent whose car they correct. Agent 0 is "no agent" (the shared dev token and the overrides
-- that existed before agents had their own state).
ALTER TABLE overrides ADD COLUMN agent_id bigint NOT NULL DEFAULT 0;
ALTER TABLE overrides DROP CONSTRAINT overrides_pkey;
ALTER TABLE overrides ADD PRIMARY KEY (team_id, agent_id, key);

-- +goose Down
DELETE FROM overrides WHERE agent_id <> 0;
ALTER TABLE overrides DROP CONSTRAINT overrides_pkey;
ALTER TABLE overrides DROP COLUMN agent_id;
ALTER TABLE overrides ADD PRIMARY KEY (team_id, key);
