-- +goose Up
CREATE TABLE laps (
    session_id   text             NOT NULL,
    car_id       integer          NOT NULL,
    vehicle_name text             NOT NULL,
    lap          integer          NOT NULL,
    driver       text             NOT NULL DEFAULT '',
    lap_time     double precision,
    s1           double precision,
    s2           double precision,
    s3           double precision,
    fuel_start   double precision,
    fuel_end     double precision,
    tyre_age     integer,
    wear_fl      double precision,
    wear_fr      double precision,
    wear_rl      double precision,
    wear_rr      double precision,
    fcy          boolean          NOT NULL DEFAULT false,
    in_lap       boolean          NOT NULL DEFAULT false,
    out_lap      boolean          NOT NULL DEFAULT false,
    PRIMARY KEY (session_id, car_id, vehicle_name, lap)
);

CREATE TABLE stops (
    id            bigserial        PRIMARY KEY,
    session_id    text             NOT NULL,
    car_id        integer          NOT NULL,
    vehicle_name  text             NOT NULL,
    entry_et      double precision NOT NULL,
    exit_et       double precision,
    fuel_added    double precision,
    tyres_changed boolean          NOT NULL DEFAULT false,
    driver_swap   boolean          NOT NULL DEFAULT false,
    UNIQUE (session_id, car_id, vehicle_name, entry_et)
);

CREATE TABLE agent_progress (
    agent_id   text   NOT NULL,
    session_id text   NOT NULL,
    last_seq   bigint NOT NULL,
    PRIMARY KEY (agent_id, session_id)
);

-- +goose Down
DROP TABLE agent_progress;
DROP TABLE stops;
DROP TABLE laps;
