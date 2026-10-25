-- +goose Up
CREATE TABLE params (
    track            text             NOT NULL,
    car              text             NOT NULL,
    pit_through_green double precision NOT NULL DEFAULT 0,
    pit_through_fcy   double precision NOT NULL DEFAULT 0,
    refuel_rate      double precision NOT NULL DEFAULT 0,
    tyre_change      double precision NOT NULL DEFAULT 0,
    driver_swap      double precision NOT NULL DEFAULT 0,
    parallel_service boolean          NOT NULL DEFAULT false,
    fuel_effect      double precision NOT NULL DEFAULT 0,
    deg_per_lap      double precision NOT NULL DEFAULT 0,
    save_cost        double precision NOT NULL DEFAULT 0,
    reserve          double precision NOT NULL DEFAULT 3,
    PRIMARY KEY (track, car)
);

CREATE TABLE stint_plan (
    position integer          PRIMARY KEY,
    driver   text             NOT NULL,
    duration double precision NOT NULL
);

CREATE TABLE overrides (
    key    text             PRIMARY KEY,
    value  double precision NOT NULL,
    set_at timestamptz      NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE overrides;
DROP TABLE stint_plan;
DROP TABLE params;
