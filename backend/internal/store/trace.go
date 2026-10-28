package store

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// QueryLogger logs every database call through the pgx tracer hook: DEBUG with the timing, WARN above Slow, ERROR when
// the call failed. It logs the shape of the statement (placeholders, no values) and never the arguments.
type QueryLogger struct {
	Log  *slog.Logger
	Slow time.Duration
}

type queryKey struct{}

type queryStart struct {
	at  time.Time
	sql string
}

func (q *QueryLogger) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, queryKey{}, queryStart{time.Now(), data.SQL})
}

func (q *QueryLogger) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	st, _ := ctx.Value(queryKey{}).(queryStart)
	d := time.Since(st.at)
	stmt := Statement(st.sql)
	switch {
	case data.Err != nil && !errors.Is(data.Err, pgx.ErrNoRows) && !errors.Is(data.Err, context.Canceled):
		q.Log.Error("database call failed", "statement", stmt, "ms", d.Milliseconds(), "err", data.Err)
	case q.Slow > 0 && d >= q.Slow:
		q.Log.Warn("slow database call", "statement", stmt, "ms", d.Milliseconds(), "threshold_ms", q.Slow.Milliseconds())
	default:
		q.Log.Debug("database call", "statement", stmt, "ms", d.Milliseconds())
	}
}

// Statement shortens a SQL text to one line of at most 70 characters for the log.
func Statement(sql string) string {
	s := strings.Join(strings.Fields(sql), " ")
	if len(s) > 70 {
		s = s[:70] + "..."
	}
	return s
}
