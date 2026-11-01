-- +goose Up
-- A team car is what the team calls its car (the BMW). The game reports a vehicle name per livery; each name can be
-- assigned to one team car, so parameters and set-ups follow the car and not the livery.
CREATE TABLE team_cars (
    id      bigserial PRIMARY KEY,
    team_id bigint    NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    name    text      NOT NULL,
    UNIQUE (team_id, name)
);

CREATE TABLE team_car_names (
    team_id      bigint NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    vehicle_name text   NOT NULL,
    car_id       bigint NOT NULL REFERENCES team_cars (id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, vehicle_name)
);

-- +goose Down
DROP TABLE team_car_names;
DROP TABLE team_cars;
