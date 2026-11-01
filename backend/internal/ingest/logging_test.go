package ingest

import (
	"bytes"
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/3lefeint/splash/backend/internal/live"
	"github.com/3lefeint/splash/backend/internal/logx"
	"github.com/3lefeint/splash/backend/internal/store"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func loggedServer(t *testing.T) (*Server, *lockedBuf, string, store.Store) {
	t.Helper()
	out := &lockedBuf{}
	db := store.NewMemory()
	st := db.ForTeam(1)
	srv := New(st, &live.State{}, logx.New(out, -4))
	srv.TeamID = 1
	ts := httptest.NewServer(&Gateway{Dir: db, DefaultTeam: 1, Server: func(int64) *Server { return srv }})
	t.Cleanup(ts.Close)
	return srv, out, "ws" + strings.TrimPrefix(ts.URL, "http"), st
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	end := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(end) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestConnectionSessionAndBacklogLines(t *testing.T) {
	_, out, url, st := loggedServer(t)
	st.SetProgress(context.Background(), "a", "s1", 1) // the backend already has lap 1: a resend of it is a duplicate

	c := dial(t, url, "")
	send(t, c, "a", 0, "hello", `{"agent_version":"1"}`)
	send(t, c, "a", 1, "lap", fmt.Sprintf(lapPayload, 1)) // resent, already stored
	send(t, c, "a", 2, "lap", fmt.Sprintf(lapPayload, 2)) // new
	send(t, c, "a", 3, "telemetry", `{"car_id":0,"vehicle_name":"v","lap":1,"fuel":50,"fuel_capacity":90,"wear":[1,1,1,1]}`)
	c.Close(websocket.StatusNormalClosure, "bye")
	waitFor(t, func() bool { return strings.Contains(out.String(), "agent disconnected") })

	log := out.String()
	if testing.Verbose() {
		t.Log("\n" + log)
	}
	for _, want := range []string{
		`level=INFO msg="agent connected" team=1 agent=a agent_id=0`,
		`level=INFO msg="session started" team=1 session=s1`,
		`level=INFO msg="backlog resent" team=1 agent=a stored=1 duplicates=1`,
		`level=INFO msg="agent disconnected" team=1 agent=a reason="closed by agent"`,
		"msg=frame", "type=lap", "bytes=",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in:\n%s", want, log)
		}
	}
	if !strings.Contains(log, "messages=4 stored=1 resent_duplicates=1") {
		t.Errorf("the disconnect line must carry the counts:\n%s", log)
	}
	if n := strings.Count(log, "backlog resent"); n != 1 {
		t.Errorf("the backlog line is written once per connection, got %d", n)
	}
}

func TestSessionChangeAndDroppedSamplesLines(t *testing.T) {
	srv, out, url, _ := loggedServer(t)
	srv.CountEvery = 30 * time.Millisecond
	c := dial(t, url, "")
	tel := func(sess string, et int) string {
		return fmt.Sprintf(`{"v":1,"type":"telemetry","agent_id":"a","role":"driver","session_id":%q,"seq":0,"et":%d,"payload":{"car_id":0,"vehicle_name":"v","lap":1,"fuel":50,"fuel_capacity":90,"wear":[1,1,1,1]}}`, sess, et)
	}
	write := func(msg string) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		c.Write(ctx, websocket.MessageText, []byte(msg))
		c.Read(ctx)
	}
	write(tel("s1", 100))
	write(tel("s1", 50)) // older than the newest: dropped
	time.Sleep(50 * time.Millisecond)
	write(tel("s1", 40)) // dropped again, and the interval is over: reported
	write(tel("s2", 10)) // another session
	log := out.String()
	if testing.Verbose() {
		t.Log("\n" + log)
	}
	for _, want := range []string{
		`level=INFO msg="session changed" team=1 from=s1 to=s2`,
		`level=WARN msg="samples dropped or deduplicated" team=1 older_live_samples=2`,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("missing %q in:\n%s", want, log)
		}
	}
}

func TestDeadConnectionIsLoggedWithItsReason(t *testing.T) {
	srv, out, url, _ := loggedServer(t)
	srv.PingInterval, srv.DeadAfter = 40*time.Millisecond, 80*time.Millisecond
	c := dial(t, url, "") // never reads, so it never answers pings
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Write(ctx, websocket.MessageText, []byte(`{"v":1,"type":"hello","agent_id":"a","role":"driver","session_id":"s1","seq":0,"et":0,"payload":{"agent_version":"1"}}`))
	waitFor(t, func() bool { return strings.Contains(out.String(), "agent disconnected") })
	log := out.String()
	if !strings.Contains(log, `level=WARN msg="agent connection dead, closing" team=1 agent=a`) || !strings.Contains(log, `reason="ping timeout"`) {
		t.Fatalf("%s", log)
	}
}
