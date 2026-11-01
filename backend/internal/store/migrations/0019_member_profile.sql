-- +goose Up
-- What a member is called, their short name (the name of their agent) and the Cloudflare Access service token of their
-- agent. The secret is stored encrypted by the backend; the client id is no secret.
ALTER TABLE team_members ADD COLUMN name         text NOT NULL DEFAULT '';
ALTER TABLE team_members ADD COLUMN short        text NOT NULL DEFAULT '';
ALTER TABLE team_members ADD COLUMN cf_client_id text NOT NULL DEFAULT '';
ALTER TABLE team_members ADD COLUMN cf_secret     bytea;
CREATE UNIQUE INDEX team_members_short ON team_members (team_id, lower(short)) WHERE short <> '';

-- +goose Down
DROP INDEX team_members_short;
ALTER TABLE team_members DROP COLUMN cf_secret;
ALTER TABLE team_members DROP COLUMN cf_client_id;
ALTER TABLE team_members DROP COLUMN short;
ALTER TABLE team_members DROP COLUMN name;
