// Command splash-replay plays an NDJSON recording against a running backend.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/3lefeint/splash/backend/internal/replay"
)

func main() {
	var cfg replay.Config
	flag.StringVar(&cfg.File, "file", "", "NDJSON recording to play (required)")
	flag.StringVar(&cfg.URL, "url", "ws://localhost:8080/v1/agent/ws", "agent WebSocket endpoint")
	flag.StringVar(&cfg.Token, "token", os.Getenv("SPLASH_AGENT_TOKEN"), "agent token (default: $SPLASH_AGENT_TOKEN)")
	flag.Float64Var(&cfg.Speed, "speed", 1, "playback speed, 1 = real time, 0 = as fast as possible")
	flag.Parse()
	if cfg.File == "" {
		flag.Usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stats, err := replay.Run(ctx, cfg)
	fmt.Printf("sent %d, acknowledged %d\n", stats.Sent, stats.Acked)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
