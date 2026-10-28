-- +goose Up
CREATE TABLE pit_setups (
    car   text   NOT NULL,
    name  text   NOT NULL,
    steps jsonb  NOT NULL,
    seq   bigserial,
    PRIMARY KEY (car, name)
);

-- +goose Down
DROP TABLE pit_setups;
