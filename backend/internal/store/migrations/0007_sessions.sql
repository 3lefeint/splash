-- +goose Up
CREATE TABLE sessions (
    session_id   text             PRIMARY KEY,
    track        text             NOT NULL DEFAULT '',
    session_type text             NOT NULL DEFAULT '',
    car          text             NOT NULL DEFAULT '',
    started_at   timestamptz      NOT NULL,
    last_seen    timestamptz      NOT NULL,
    last_et      double precision NOT NULL DEFAULT 0
);

CREATE TABLE lap_conditions (
    session_id     text             NOT NULL,
    car_id         integer          NOT NULL,
    vehicle_name   text             NOT NULL,
    lap            integer          NOT NULL,
    et             double precision NOT NULL,
    control        smallint,
    ambient        double precision,
    track          double precision,
    rain           double precision,
    wetness        double precision,
    surface        jsonb,
    inner_temp     jsonb,
    pressure       jsonb,
    brake          jsonb,
    water          double precision,
    oil            double precision,
    compound_front text             NOT NULL DEFAULT '',
    compound_rear  text             NOT NULL DEFAULT '',
    PRIMARY KEY (session_id, car_id, vehicle_name, lap)
);

-- +goose Down
DROP TABLE lap_conditions;
DROP TABLE sessions;
