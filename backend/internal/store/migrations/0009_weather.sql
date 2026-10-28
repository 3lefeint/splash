-- +goose Up
CREATE TABLE weather_points (
    session_id text             NOT NULL,
    et         double precision NOT NULL,
    ambient    double precision,
    track      double precision,
    rain       double precision,
    wetness    double precision,
    PRIMARY KEY (session_id, et)
);

-- +goose Down
DROP TABLE weather_points;
