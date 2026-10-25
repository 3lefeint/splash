package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

func setup(t *testing.T, token string) (*Server, *store.Memory, string) {
	t.Helper()
	st := store.NewMemory()
	srv := New(st, &live.State{}, token, nil)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return srv, st, "ws" + strings.TrimPrefix(ts.URL, "http")
}

func dial(t *testing.T, url, token string) *websocket.Conn {
	t.Helper()
	h := http.Header{}
	if token != "" {
		h.Set("Authorization", "Bearer "+token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

func send(t *testing.T, c *websocket.Conn, agent string, seq int, typ, payload string) wire.Ack {
	t.Helper()
	msg := fmt.Sprintf(`{"v":1,"type":%q,"agent_id":%q,"role":"driver","session_id":"s1","seq":%d,"et":%d,"payload":%s}`,
		typ, agent, seq, seq*10, payload)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var ack wire.Ack
	if err := json.Unmarshal(data, &ack); err != nil {
		t.Fatal(err)
	}
	return ack
}

const lapPayload = `{"car_id":0,"vehicle_name":"v","lap":%d,"lap_time":98.4,"fuel_start":60,"fuel_end":57,"in_lap":false,"out_lap":false}`

func TestAckAndStore(t *testing.T) {
	_, st, url := setup(t, "")
	c := dial(t, url, "")
	if a := send(t, c, "a", 0, "hello", `{"agent_version":"1"}`); a.Type != "ack" || a.Seq != 0 {
		t.Fatalf("%+v", a)
	}
	if a := send(t, c, "a", 1, "lap", fmt.Sprintf(lapPayload, 1)); a.Seq != 1 {
		t.Fatalf("%+v", a)
	}
	laps, _ := st.Laps(context.Background(), "s1")
	if len(laps) != 1 || laps[0].Lap.Lap != 1 {
		t.Fatalf("laps=%v", laps)
	}
	if seq, ok, _ := st.Progress(context.Background(), "a", "s1"); !ok || seq != 1 {
		t.Fatalf("progress=%d ok=%v", seq, ok)
	}
}

func TestResendIsAckedButNotReprocessed(t *testing.T) {
	_, st, url := setup(t, "")
	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)
	send(t, c, "a", 1, "lap", fmt.Sprintf(lapPayload, 1))
	send(t, c, "a", 2, "lap", fmt.Sprintf(lapPayload, 2))
	// Reconnect and resend from 1: the backend answers with the highest seq it has.
	c2 := dial(t, url, "")
	a := send(t, c2, "a", 1, "lap", `{"car_id":0,"vehicle_name":"v","lap":1,"lap_time":1,"in_lap":true,"out_lap":false}`)
	if a.Seq != 2 {
		t.Fatalf("ack=%d want 2", a.Seq)
	}
	laps, _ := st.Laps(context.Background(), "s1")
	if len(laps) != 2 || laps[0].InLap {
		t.Fatalf("resend leaked into store: %+v", laps)
	}
}

func TestProgressSurvivesRestart(t *testing.T) {
	st := store.NewMemory()
	st.SetProgress(context.Background(), "a", "s1", 7)
	ts := httptest.NewServer(New(st, &live.State{}, "", nil)) // fresh server, no in-memory state
	defer ts.Close()
	c := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http"), "")
	if a := send(t, c, "a", 5, "lap", fmt.Sprintf(lapPayload, 5)); a.Seq != 7 {
		t.Fatalf("ack=%d want 7", a.Seq)
	}
	if laps, _ := st.Laps(context.Background(), "s1"); len(laps) != 0 {
		t.Fatal("already acknowledged seq must not be stored again")
	}
}

func TestLiveSamplesUpdateLiveState(t *testing.T) {
	srv, st, url := setup(t, "")
	c := dial(t, url, "")
	send(t, c, "a", 0, "telemetry", `{"car_id":0,"vehicle_name":"v","lap":3,"fuel":42.5,"fuel_capacity":90,"wear":[1,1,1,1]}`)
	snap := srv.Live.Snapshot()
	if snap.Telemetry == nil || snap.Telemetry.Fuel != 42.5 {
		t.Fatalf("%+v", snap)
	}
	if seq, ok, _ := st.Progress(context.Background(), "a", "s1"); ok {
		t.Fatalf("telemetry must not be durable, progress=%d", seq)
	}
}

func TestAuth(t *testing.T) {
	_, _, url := setup(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, resp, err := websocket.Dial(ctx, url, nil); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token must give 401, err=%v resp=%v", err, resp)
	}
	h := http.Header{"Authorization": {"Bearer wrong"}}
	if _, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: h}); err == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token must give 401, err=%v", err)
	}
	c := dial(t, url, "secret")
	if a := send(t, c, "a", 0, "hello", `{"agent_version":"1"}`); a.Seq != 0 {
		t.Fatalf("%+v", a)
	}
}

func TestInvalidMessageClosesConnection(t *testing.T) {
	_, _, url := setup(t, "")
	c := dial(t, url, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.Write(ctx, websocket.MessageText, []byte(`{"nonsense":true}`))
	if _, _, err := c.Read(ctx); websocket.CloseStatus(err) != websocket.StatusUnsupportedData {
		t.Fatalf("err=%v", err)
	}
}
