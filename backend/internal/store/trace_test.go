package store

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestQueryLoggerLevels(t *testing.T) {
	var out bytes.Buffer
	q := &QueryLogger{Log: slog.New(slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})), Slow: 20 * time.Millisecond}
	run := func(sql string, wait time.Duration, err error) {
		ctx := q.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: sql, Args: []any{"fake-secret-argument"}})
		time.Sleep(wait)
		q.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: err})
	}
	run("SELECT  id,\n name FROM teams WHERE name = $1", 0, nil)
	run("UPDATE agents SET last_seen = now() WHERE id = $1", 40*time.Millisecond, nil)
	run("INSERT INTO laps (a) VALUES ($1)", 0, errors.New("connection reset"))
	run("SELECT 1", 0, pgx.ErrNoRows)
	log := out.String()
	if testing.Verbose() {
		t.Log("\n" + log)
	}
	for _, want := range []string{
		`level=DEBUG msg="database call" statement="SELECT id, name FROM teams WHERE name = $1"`,
		`level=WARN msg="slow database call" statement="UPDATE agents SET last_seen = now() WHERE id = $1"`,
		`level=ERROR msg="database call failed" statement="INSERT INTO laps (a) VALUES ($1)"`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in:\n%s", want, log)
		}
	}
	if strings.Contains(log, "fake-secret-argument") {
		t.Error("arguments must never be logged")
	}
	if strings.Contains(log, "level=ERROR msg=\"database call failed\" statement=\"SELECT 1\"") {
		t.Error("no rows is not a failure")
	}
}
