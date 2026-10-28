-- +goose Up
CREATE TABLE track_maps (
    track  text             PRIMARY KEY,
    length double precision NOT NULL,
    points jsonb            NOT NULL
);

-- +goose Down
DROP TABLE track_maps;
