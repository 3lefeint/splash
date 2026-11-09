-- +goose Up
-- The dashboard default of a team: the tile layout and the tyre and brake ranges, saved by an administrator as one JSON
-- document that the frontend reads and validates.
CREATE TABLE team_layouts (
    team_id bigint      PRIMARY KEY REFERENCES teams (id) ON DELETE CASCADE,
    doc     jsonb       NOT NULL,
    saved   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE team_layouts;
