-- +goose Up
-- Teams. Everything that exists before this migration belongs to the team "default".
CREATE TABLE teams (
    id   bigserial PRIMARY KEY,
    name text      NOT NULL UNIQUE
);

INSERT INTO teams (name) VALUES ('default');

CREATE TABLE team_members (
    email    text    PRIMARY KEY,
    team_id  bigint  NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    is_admin boolean NOT NULL DEFAULT false
);

CREATE TABLE agents (
    id         bigserial   PRIMARY KEY,
    name       text        NOT NULL,
    token_hash bytea       NOT NULL UNIQUE,
    team_id    bigint      NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    revoked    boolean     NOT NULL DEFAULT false,
    last_seen  timestamptz
);

CREATE TABLE audit_log (
    ts     timestamptz NOT NULL DEFAULT now(),
    email  text        NOT NULL,
    team   text        NOT NULL,
    method text        NOT NULL,
    path   text        NOT NULL
);

CREATE INDEX audit_log_ts ON audit_log (ts);

ALTER TABLE stint_plan ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE stint_plan SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE stint_plan ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE stint_plan DROP CONSTRAINT stint_plan_pkey;
ALTER TABLE stint_plan ADD PRIMARY KEY (team_id, position);

ALTER TABLE overrides ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE overrides SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE overrides ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE overrides DROP CONSTRAINT overrides_pkey;
ALTER TABLE overrides ADD PRIMARY KEY (team_id, key);

ALTER TABLE pit_setups ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE pit_setups SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE pit_setups ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE pit_setups DROP CONSTRAINT pit_setups_pkey;
ALTER TABLE pit_setups ADD PRIMARY KEY (team_id, car, name);

ALTER TABLE params ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE params SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE params ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE params DROP CONSTRAINT params_pkey;
ALTER TABLE params ADD PRIMARY KEY (team_id, track, car);

ALTER TABLE sessions ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE sessions SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE sessions ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE sessions DROP CONSTRAINT sessions_pkey;
ALTER TABLE sessions ADD PRIMARY KEY (team_id, session_id);

ALTER TABLE laps ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE laps SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE laps ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE laps DROP CONSTRAINT laps_pkey;
ALTER TABLE laps ADD PRIMARY KEY (team_id, session_id, car_id, vehicle_name, lap);

ALTER TABLE wear_laps ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE wear_laps SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE wear_laps ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE wear_laps DROP CONSTRAINT wear_laps_pkey;
ALTER TABLE wear_laps ADD PRIMARY KEY (team_id, session_id, car_id, vehicle_name, lap);

ALTER TABLE lap_conditions ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE lap_conditions SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE lap_conditions ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE lap_conditions DROP CONSTRAINT lap_conditions_pkey;
ALTER TABLE lap_conditions ADD PRIMARY KEY (team_id, session_id, car_id, vehicle_name, lap);

ALTER TABLE weather_points ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE weather_points SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE weather_points ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE weather_points DROP CONSTRAINT weather_points_pkey;
ALTER TABLE weather_points ADD PRIMARY KEY (team_id, session_id, et);

ALTER TABLE agent_progress ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE agent_progress SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE agent_progress ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE agent_progress DROP CONSTRAINT agent_progress_pkey;
ALTER TABLE agent_progress ADD PRIMARY KEY (team_id, agent_id, session_id);

ALTER TABLE stops ADD COLUMN team_id bigint REFERENCES teams (id);
UPDATE stops SET team_id = (SELECT id FROM teams WHERE name = 'default');
ALTER TABLE stops ALTER COLUMN team_id SET NOT NULL;
ALTER TABLE stops DROP CONSTRAINT stops_session_id_car_id_vehicle_name_entry_et_key;
ALTER TABLE stops ADD UNIQUE (team_id, session_id, car_id, vehicle_name, entry_et);

-- +goose Down
ALTER TABLE stops DROP CONSTRAINT stops_team_id_session_id_car_id_vehicle_name_entry_et_key;
ALTER TABLE stops ADD UNIQUE (session_id, car_id, vehicle_name, entry_et);
ALTER TABLE stops DROP COLUMN team_id;

ALTER TABLE agent_progress DROP CONSTRAINT agent_progress_pkey;
ALTER TABLE agent_progress DROP COLUMN team_id;
ALTER TABLE agent_progress ADD PRIMARY KEY (agent_id, session_id);

ALTER TABLE weather_points DROP CONSTRAINT weather_points_pkey;
ALTER TABLE weather_points DROP COLUMN team_id;
ALTER TABLE weather_points ADD PRIMARY KEY (session_id, et);

ALTER TABLE lap_conditions DROP CONSTRAINT lap_conditions_pkey;
ALTER TABLE lap_conditions DROP COLUMN team_id;
ALTER TABLE lap_conditions ADD PRIMARY KEY (session_id, car_id, vehicle_name, lap);

ALTER TABLE wear_laps DROP CONSTRAINT wear_laps_pkey;
ALTER TABLE wear_laps DROP COLUMN team_id;
ALTER TABLE wear_laps ADD PRIMARY KEY (session_id, car_id, vehicle_name, lap);

ALTER TABLE laps DROP CONSTRAINT laps_pkey;
ALTER TABLE laps DROP COLUMN team_id;
ALTER TABLE laps ADD PRIMARY KEY (session_id, car_id, vehicle_name, lap);

ALTER TABLE sessions DROP CONSTRAINT sessions_pkey;
ALTER TABLE sessions DROP COLUMN team_id;
ALTER TABLE sessions ADD PRIMARY KEY (session_id);

ALTER TABLE params DROP CONSTRAINT params_pkey;
ALTER TABLE params DROP COLUMN team_id;
ALTER TABLE params ADD PRIMARY KEY (track, car);

ALTER TABLE pit_setups DROP CONSTRAINT pit_setups_pkey;
ALTER TABLE pit_setups DROP COLUMN team_id;
ALTER TABLE pit_setups ADD PRIMARY KEY (car, name);

ALTER TABLE overrides DROP CONSTRAINT overrides_pkey;
ALTER TABLE overrides DROP COLUMN team_id;
ALTER TABLE overrides ADD PRIMARY KEY (key);

ALTER TABLE stint_plan DROP CONSTRAINT stint_plan_pkey;
ALTER TABLE stint_plan DROP COLUMN team_id;
ALTER TABLE stint_plan ADD PRIMARY KEY (position);

DROP TABLE audit_log;
DROP TABLE agents;
DROP TABLE team_members;
DROP TABLE teams;
