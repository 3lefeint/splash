// Command splash runs the Splash backend.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/3lefeint/splash/backend/internal/api"
	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/ratelimit"
	"github.com/3lefeint/splash/backend/internal/secretbox"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/teams"
	"github.com/3lefeint/splash/backend/internal/webui"
)

// logLevelName is the configured log level for the start line; the value was validated before the logger was built.
func logLevelName() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(logx.LevelVar))); v != "" {
		return v
	}
	return "info"
}

// migrate applies the pending database migrations and exits. The role in SPLASH_DATABASE_URL must own the schema.
func migrate(ctx context.Context, log *slog.Logger) error {
	url := os.Getenv("SPLASH_DATABASE_URL")
	if url == "" {
		return errors.New("SPLASH_DATABASE_URL not set")
	}
	pool, err := store.Connect(ctx, url, 0, nil)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

// loggerFromEnv builds the logger with the level of SPLASH_LOG_LEVEL; an invalid value is an error.
func loggerFromEnv(w io.Writer) (*slog.Logger, error) {
	level, err := logx.ParseLevel(os.Getenv(logx.LevelVar))
	if err != nil {
		return nil, err
	}
	return logx.New(w, level), nil
}

func main() {
	log, err := loggerFromEnv(os.Stderr)
	if err != nil {
		logx.New(os.Stderr, slog.LevelInfo).Error("exit", "err", err)
		os.Exit(1)
	}
	if err := run(log); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

// defaultMaxConns is the database pool size when SPLASH_DB_MAX_CONNS is not set.
const defaultMaxConns = 10

// Defaults of the rate limits per identity: write requests, and new SSE streams and agent connections.
const (
	defaultWritesPerMin   = 120
	defaultConnectsPerMin = 30
)

// envInt reads a whole number from the environment; a limit of 0 switches the limit off.
func envInt(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%s must be a whole number, 0 or more, got %q", name, v)
	}
	return n, nil
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) > 1 {
		if os.Args[1] == "migrate" {
			return migrate(ctx, log)
		}
		return fmt.Errorf("unknown command %q (only \"migrate\" is supported)", os.Args[1])
	}

	maxConns := int32(defaultMaxConns)
	if v := os.Getenv("SPLASH_DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 1 {
			return fmt.Errorf("SPLASH_DB_MAX_CONNS must be a positive integer, got %q", v)
		}
		maxConns = int32(n)
	}

	var db store.DB
	storeKind := "memory"
	if url := os.Getenv("SPLASH_DATABASE_URL"); url != "" {
		slow, err := envInt("SPLASH_DB_SLOW_MS", 500)
		if err != nil {
			return err
		}
		pool, err := store.Connect(ctx, url, maxConns, &store.QueryLogger{Log: log, Slow: time.Duration(slow) * time.Millisecond})
		if err != nil {
			return err
		}
		defer pool.Close()
		db = store.NewPostgres(pool)
		storeKind = "postgres"
	} else {
		log.Warn("SPLASH_DATABASE_URL not set: using in-memory store, data is lost on exit")
		db = store.NewMemory()
	}
	reg, err := teams.NewRegistry(ctx, db, log)
	if err != nil {
		return err
	}

	token := os.Getenv("SPLASH_AGENT_TOKEN")
	if token == "" {
		log.Warn("SPLASH_AGENT_TOKEN not set: agent connections are not authenticated")
	}

	admins := 0
	if list := os.Getenv("SPLASH_ADMIN_EMAILS"); list != "" {
		n, err := auth.BootstrapAdmins(ctx, db, reg.DefaultID(), list)
		if err != nil {
			return err
		}
		admins = n
		log.Info("administrators set from the environment", "count", n)
	}

	writes, err := envInt("SPLASH_RATE_WRITES_PER_MIN", defaultWritesPerMin)
	if err != nil {
		return err
	}
	connects, err := envInt("SPLASH_RATE_CONNECTS_PER_MIN", defaultConnectsPerMin)
	if err != nil {
		return err
	}

	beat, err := envInt("SPLASH_SSE_HEARTBEAT_SECONDS", int(15))
	if err != nil {
		return err
	}
	ping, err := envInt("SPLASH_WS_PING_SECONDS", 20)
	if err != nil {
		return err
	}
	dead, err := envInt("SPLASH_WS_DEAD_AFTER_SECONDS", 30)
	if err != nil {
		return err
	}
	reg.PingInterval, reg.DeadAfter = time.Duration(ping)*time.Second, time.Duration(dead)*time.Second
	reg.MinAgentVersion = strings.TrimSpace(os.Getenv("SPLASH_AGENT_MIN_VERSION"))

	a := api.New(reg)
	a.Heartbeat = time.Duration(beat) * time.Second
	a.Log = log
	a.WriteLimit, a.ConnLimit = ratelimit.New(writes, writes), ratelimit.New(connects, connects)
	domain, audience := os.Getenv("SPLASH_ACCESS_TEAM_DOMAIN"), os.Getenv("SPLASH_ACCESS_AUDIENCE")
	switch {
	case domain != "" && audience != "":
		if a.Verifier, err = auth.NewVerifier(domain, audience); err != nil {
			return err
		}
		a.Verifier.SetLogger(log)
	case domain != "" || audience != "":
		return errors.New("SPLASH_ACCESS_TEAM_DOMAIN and SPLASH_ACCESS_AUDIENCE must be set together")
	default:
		log.Warn("SPLASH_ACCESS_TEAM_DOMAIN not set: browser requests are not authenticated and belong to the team default")
	}
	if key := os.Getenv("SPLASH_SECRET_KEY"); key != "" {
		if a.Secrets, err = secretbox.New(key); err != nil {
			return fmt.Errorf("SPLASH_SECRET_KEY: %w", err)
		}
	} else {
		log.Warn("SPLASH_SECRET_KEY not set: Cloudflare secrets of members cannot be stored, agent configurations have no Cloudflare lines")
	}
	a.PublicURL = strings.TrimRight(strings.TrimSpace(os.Getenv("SPLASH_PUBLIC_URL")), "/")
	a.Start(ctx)

	mux := http.NewServeMux()
	mux.Handle(ingest.Path, &ingest.Gateway{Dir: db, Verifier: a.Verifier, Fallback: token, DefaultTeam: reg.DefaultID(),
		Server: func(id int64) *ingest.Server { return reg.Get(id).Ingest }, Limit: ratelimit.New(connects, connects), Log: log})
	a.Register(mux)
	mux.Handle("/", webui.Handler()) // the frontend; everything more specific above wins
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	addr := os.Getenv("SPLASH_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: logx.Middleware(log, mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		log.Info("shutting down")
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("starting", "listen", addr, "store", storeKind, "access_auth", a.Verifier != nil,
		"agent_shared_token", token != "", "admins_from_env", admins, "db_max_conns", maxConns,
		"sse_heartbeat_s", beat, "ws_ping_s", ping, "ws_dead_after_s", dead,
		"rate_writes_per_min", writes, "rate_connects_per_min", connects, "log_level", logLevelName())
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("stopped")
	return nil
}
