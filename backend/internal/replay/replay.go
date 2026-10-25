// Package replay plays an NDJSON recording against a running backend as if a
// live agent were sending it.
package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// Config describes one replay run.
type Config struct {
	File  string
	URL   string // ws:// or wss:// endpoint of the agent socket
	Token string
	// Speed scales the recorded game time: 1 is real time, 10 is ten times
	// faster. 0 sends as fast as the backend acknowledges.
	Speed float64
	// AckTimeout bounds the wait for the final acknowledgements.
	AckTimeout time.Duration
}

// Stats reports what a run did.
type Stats struct{ Sent, Acked int }

// Run sends every message of the file and waits for all acknowledgements.
func Run(ctx context.Context, cfg Config) (Stats, error) {
	var st Stats
	msgs, err := load(cfg.File)
	if err != nil {
		return st, err
	}
	h := http.Header{}
	if cfg.Token != "" {
		h.Set("Authorization", "Bearer "+cfg.Token)
	}
	c, _, err := websocket.Dial(ctx, cfg.URL, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		return st, fmt.Errorf("dial: %w", err)
	}
	defer c.CloseNow()

	acked := make(chan error, 1)
	ackCount := make(chan int, len(msgs)+1)
	go func() { // the backend sends one ack per message, in order
		for i := 0; i < len(msgs); i++ {
			_, data, err := c.Read(ctx)
			if err != nil {
				acked <- fmt.Errorf("read ack %d: %w", i+1, err)
				return
			}
			var a wire.Ack
			if err := json.Unmarshal(data, &a); err != nil || a.Type != "ack" {
				acked <- fmt.Errorf("unexpected reply %q", data)
				return
			}
			ackCount <- i + 1
		}
		acked <- nil
	}()

	var prev float64
	for i, m := range msgs {
		if cfg.Speed > 0 && i > 0 && m.env.ET > prev {
			wait := time.Duration((m.env.ET - prev) / cfg.Speed * float64(time.Second))
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return st, ctx.Err()
			}
		}
		prev = m.env.ET
		if err := c.Write(ctx, websocket.MessageText, m.raw); err != nil {
			return st, fmt.Errorf("send line %d: %w", m.line, err)
		}
		st.Sent++
	}

	timeout := cfg.AckTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	select {
	case err := <-acked:
		st.Acked = len(ackCount)
		if err != nil {
			return st, err
		}
	case <-time.After(timeout):
		st.Acked = len(ackCount)
		return st, fmt.Errorf("timeout: %d of %d messages acknowledged", st.Acked, st.Sent)
	case <-ctx.Done():
		return st, ctx.Err()
	}
	c.Close(websocket.StatusNormalClosure, "")
	return st, nil
}

type line struct {
	line int
	raw  []byte
	env  wire.Envelope
}

func load(path string) ([]line, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []line
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		if len(sc.Bytes()) == 0 {
			continue
		}
		raw := append([]byte(nil), sc.Bytes()...)
		m, err := wire.Decode(raw)
		if err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		out = append(out, line{n, raw, m.Envelope})
	}
	return out, sc.Err()
}
