-- +goose Up
CREATE TABLE track_corners (
    track   text  PRIMARY KEY,
    corners jsonb NOT NULL
);

-- +goose Down
DROP TABLE track_corners;
