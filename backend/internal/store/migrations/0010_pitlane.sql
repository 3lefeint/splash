-- +goose Up
CREATE TABLE track_pits (
    track text  PRIMARY KEY,
    data  jsonb NOT NULL
);

-- +goose Down
DROP TABLE track_pits;
