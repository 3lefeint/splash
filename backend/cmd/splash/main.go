// Command splash runs the Splash backend.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/3lefeint/splash/backend/internal/api"
	"github.com/3lefeint/splash/backend/internal/engine"
	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var st store.Store
	if url := os.Getenv("SPLASH_DATABASE_URL"); url != "" {
		pool, err := store.Connect(ctx, url)
		if err != nil {
			return err
		}
		defer pool.Close()
		if err := store.Migrate(ctx, pool); err != nil {
			return err
		}
		st = store.NewPostgres(pool)
	} else {
		log.Warn("SPLASH_DATABASE_URL not set: using in-memory store, data is lost on exit")
		st = store.NewMemory()
	}

	token := os.Getenv("SPLASH_AGENT_TOKEN")
	if token == "" {
		log.Warn("SPLASH_AGENT_TOKEN not set: agent connections are not authenticated")
	}

	lv := &live.State{}
	a := api.New(st, engine.New(st, lv))
	go a.Hub.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle(ingest.Path, ingest.New(st, lv, token, log))
	a.Register(mux)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	addr := os.Getenv("SPLASH_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Info("listening", "addr", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
