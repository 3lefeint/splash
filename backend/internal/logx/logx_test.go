package logx

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParseLevel(t *testing.T) {
	for in, want := range map[string]slog.Level{"": slog.LevelInfo, "INFO": slog.LevelInfo, "debug": slog.LevelDebug, " warn ": slog.LevelWarn, "error": slog.LevelError} {
		if got, err := ParseLevel(in); err != nil || got != want {
			t.Errorf("%q: %v %v", in, got, err)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("an invalid level must be an error")
	}
}

func TestScrubRemovesWhatMustNotBeLogged(t *testing.T) {
	jwt := "eyJhbGciOiJSUzI1NiJ9.eyJlbWFpbCI6InhAeS56In0.c2ln-_natur"
	for _, in := range []string{
		"dial postgres://user:fakepassword@db.example.com:5432/splash failed",
		"header Authorization: Bearer abc.DEF-123_xyz== rejected",
		"token " + jwt + " broke",
		"duplicate key (email)=(Someone.Name+tag@Example.com) already exists",
	} {
		out := Scrub(in)
		for _, secret := range []string{"fakepassword", "abc.DEF", jwt, "c2ln-_natur", "Someone", "Example.com"} {
			if strings.Contains(out, secret) {
				t.Errorf("%q still contains %q: %q", in, secret, out)
			}
		}
	}
	if got := Scrub("plain text, 5 laps at 12:30"); got != "plain text, 5 laps at 12:30" {
		t.Errorf("harmless text changed: %q", got)
	}
}

func TestHandlerScrubsStringsAndErrors(t *testing.T) {
	var buf bytes.Buffer
	log := New(&buf, slog.LevelDebug)
	log.Info("x", "note", "mail me at a@b.example.com", "err", errors.New("login postgres://u:pw123@h/db"))
	if out := buf.String(); strings.Contains(out, "a@b") || strings.Contains(out, "pw123") {
		t.Fatalf("%s", out)
	}
}

func TestShortIDIsStableAndHidesTheValue(t *testing.T) {
	a, b := ShortID("User@Example.com"), ShortID("user@example.com")
	if a != b || len(a) != 8 || strings.Contains(a, "user") {
		t.Fatalf("%q %q", a, b)
	}
	if ShortID("other@example.com") == a {
		t.Fatal("different identities collided")
	}
}

func TestThrottleTurnsAFloodIntoOneLinePerInterval(t *testing.T) {
	now := time.Now()
	th := NewThrottle(time.Minute)
	th.SetClock(func() time.Time { return now })
	var buf bytes.Buffer
	log := New(&buf, slog.LevelInfo)
	for i := 0; i < 100; i++ {
		th.Log(log, slog.LevelInfo, "k", "authentication failed", "reason", "expired")
	}
	th.Log(log, slog.LevelInfo, "other", "authentication failed", "reason", "bad_signature")
	if n := strings.Count(buf.String(), "authentication failed"); n != 2 {
		t.Fatalf("%d lines for two kinds:\n%s", n, buf.String())
	}
	now = now.Add(61 * time.Second)
	th.Log(log, slog.LevelInfo, "k", "authentication failed", "reason", "expired")
	if !strings.Contains(buf.String(), "suppressed=99") {
		t.Fatalf("the next line must report what was held back:\n%s", buf.String())
	}
}

func serve(t *testing.T, level slog.Level, h http.Handler) (*httptest.Server, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	ts := httptest.NewServer(Middleware(New(&buf, level), h))
	t.Cleanup(ts.Close)
	return ts, &buf
}

func TestMiddlewareLogsRequestsAtDebugWithoutQueryOrIds(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/sessions/{id}/export", func(w http.ResponseWriter, r *http.Request) {
		ReqFrom(r.Context()).Team = 7
		w.Write([]byte("ok"))
	})
	ts, buf := serve(t, slog.LevelDebug, mux)
	http.Get(ts.URL + "/api/sessions/secret-session-id/export?token=hunter2&car=other-team-car")
	out := buf.String()
	for _, want := range []string{"level=DEBUG", "msg=request", "method=GET", `route="GET /api/sessions/{id}/export"`, "status=200", "team=7", "ms="} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	for _, bad := range []string{"secret-session-id", "hunter2", "other-team-car"} {
		if strings.Contains(out, bad) {
			t.Errorf("%q leaked: %s", bad, out)
		}
	}
	ts2, buf2 := serve(t, slog.LevelInfo, mux)
	http.Get(ts2.URL + "/api/sessions/x/export")
	if buf2.Len() != 0 {
		t.Errorf("a healthy request must not log at info: %s", buf2.String())
	}
}

func TestMiddlewareLogsFailuresAndPanicsAtError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) {
		ReportError(w, errors.New("connect postgres://u:pw123@h/db: refused"))
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	mux.HandleFunc("GET /panic", func(http.ResponseWriter, *http.Request) { panic("oops") })
	mux.HandleFunc("GET /bad", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusBadRequest) })
	ts, buf := serve(t, slog.LevelInfo, mux)

	if resp, _ := http.Get(ts.URL + "/fail"); resp.StatusCode != 500 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if resp, _ := http.Get(ts.URL + "/panic"); resp.StatusCode != 500 {
		t.Fatalf("a panic must become a 500, got %d", resp.StatusCode)
	}
	http.Get(ts.URL + "/bad")
	out := buf.String()
	if !strings.Contains(out, `msg="request failed"`) || !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "route=\"GET /fail\"") {
		t.Errorf("no error line for the 5xx: %s", out)
	}
	if !strings.Contains(out, `msg="panic recovered"`) || !strings.Contains(out, "panic=oops") {
		t.Errorf("no panic line: %s", out)
	}
	if strings.Contains(out, "pw123") {
		t.Errorf("password leaked: %s", out)
	}
	if strings.Contains(out, "route=\"GET /bad\"") {
		t.Errorf("a 400 is not an error line: %s", out)
	}
}

func TestMiddlewareKeepsStreamingWorking(t *testing.T) {
	flushed := make(chan struct{})
	ts, _ := serve(t, slog.LevelDebug, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("first\n"))
		w.(http.Flusher).Flush()
		<-flushed
	}))
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 6)
	if _, err := resp.Body.Read(buf); err != nil || string(buf) != "first\n" {
		t.Fatalf("the flushed data must arrive before the handler ends: %q %v", buf, err)
	}
	close(flushed)
	resp.Body.Close()
}
