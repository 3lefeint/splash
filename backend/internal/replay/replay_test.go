package replay

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/ingest"
	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
)

// gateway serves one team's ingest Server behind the token check, as the backend does.
func gateway(db *store.Memory, srv *ingest.Server, token string) *ingest.Gateway {
	return &ingest.Gateway{Dir: db, Fallback: token, DefaultTeam: 1, Server: func(int64) *ingest.Server { return srv }}
}

func TestReplaySampleRecording(t *testing.T) {
	db := store.NewMemory()
	st := db.ForTeam(1)
	lv := &live.State{}
	ts := httptest.NewServer(gateway(db, ingest.New(st, lv, nil), "tok"))
	defer ts.Close()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	for _, speed := range []float64{0, 100000} { // as fast as possible, and paced
		stats, err := Run(context.Background(), Config{File: "../../../testdata/sample.ndjson", URL: url, Token: "tok", Speed: speed})
		if err != nil {
			t.Fatalf("speed %v: %v", speed, err)
		}
		if stats.Sent != 4 || stats.Acked != 4 {
			t.Fatalf("stats=%+v", stats)
		}
	}
	laps, _ := st.Laps(context.Background(), "s1")
	if len(laps) != 1 {
		t.Fatalf("replaying twice must not duplicate laps, got %d", len(laps))
	}
	if snap := lv.Snapshot(); snap.Telemetry == nil || snap.Telemetry.Fuel != 58.3 {
		t.Fatalf("live telemetry missing: %+v", snap)
	}
}

func TestReplayRejectsBadFileBeforeConnecting(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bad.ndjson")
	os.WriteFile(p, []byte("{\"v\":1}\n"), 0o600)
	_, err := Run(context.Background(), Config{File: p, URL: "ws://127.0.0.1:1"})
	if err == nil || !strings.Contains(err.Error(), "bad.ndjson:1") {
		t.Fatalf("err=%v", err)
	}
}

func TestReplayWrongTokenFails(t *testing.T) {
	db := store.NewMemory()
	ts := httptest.NewServer(gateway(db, ingest.New(db.ForTeam(1), &live.State{}, nil), "tok"))
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Run(ctx, Config{File: "../../../testdata/sample.ndjson", URL: "ws" + strings.TrimPrefix(ts.URL, "http"), Token: "nope"})
	if err == nil {
		t.Fatal("expected dial error")
	}
}
