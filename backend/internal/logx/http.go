package logx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"time"
)

// Req carries what the layers below the middleware learn about a request, so the request line can show it.
type Req struct {
	Team int64
}

type reqKey struct{}

// ReqFrom returns the Req of the request, or nil outside the middleware.
func ReqFrom(ctx context.Context) *Req {
	r, _ := ctx.Value(reqKey{}).(*Req)
	return r
}

// recorder remembers the status and the error a handler reported. It passes streaming and connection upgrades
// through, which the SSE stream and the agent WebSocket need.
type recorder struct {
	http.ResponseWriter
	status int
	err    error
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

func (r *recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *recorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("hijacking is not supported")
	}
	r.status = http.StatusSwitchingProtocols
	return h.Hijack()
}

func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// SetErr lets a handler tell the middleware which error made it answer with a 5xx.
func (r *recorder) SetErr(err error) { r.err = err }

// ReportError hands err to the middleware that wraps w, if there is one.
func ReportError(w http.ResponseWriter, err error) {
	if s, ok := w.(interface{ SetErr(error) }); ok {
		s.SetErr(err)
	}
}

// Middleware writes one DEBUG line per request (method, route, status, duration, team id), an ERROR line for every
// answer with a status of 500 or more, and turns a panic into an ERROR line and a 500. The route is the pattern the
// request matched, not the path, so ids in the path (sessions, agents) stay out of the log; the query is never logged.
func Middleware(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		info := &Req{}
		r = r.WithContext(context.WithValue(r.Context(), reqKey{}, info))
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic recovered", "method", r.Method, "route", routeOf(r), "panic", fmt.Sprint(v),
					"stack", string(debug.Stack()), "team", info.Team)
				if rec.status == 0 {
					http.Error(rec, "internal error", http.StatusInternalServerError)
				}
			}
			status := rec.status
			if status == 0 {
				status = http.StatusOK
			}
			dur := time.Since(start)
			if status >= 500 {
				args := []any{"method", r.Method, "route", routeOf(r), "status", status, "ms", dur.Milliseconds(), "team", info.Team}
				if rec.err != nil {
					args = append(args, "err", rec.err)
				}
				log.Error("request failed", args...)
			}
			log.Debug("request", "method", r.Method, "route", routeOf(r), "status", status, "ms", dur.Milliseconds(), "team", info.Team)
		}()
		next.ServeHTTP(rec, r)
	})
}

// routeOf is the pattern the request matched (set by the mux while it serves the request), or a marker.
func routeOf(r *http.Request) string {
	if r.Pattern != "" {
		return r.Pattern
	}
	return "unmatched"
}
