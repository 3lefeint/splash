-- +goose Up
CREATE TABLE wear_laps (
    session_id   text             NOT NULL,
    car_id       integer          NOT NULL,
    vehicle_name text             NOT NULL,
    lap          integer          NOT NULL,
    coverage     double precision NOT NULL,
    loss         jsonb            NOT NULL,
    PRIMARY KEY (session_id, car_id, vehicle_name, lap)
);

-- +goose Down
DROP TABLE wear_laps;
