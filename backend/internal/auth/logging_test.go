package auth_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/3lefeint/splash/backend/internal/auth"
	"github.com/3lefeint/splash/backend/internal/auth/authtest"
	"github.com/3lefeint/splash/backend/internal/logx"
)

func TestAFailedKeyFetchIsLoggedAsAWarningAndRetriedLater(t *testing.T) {
	iss := authtest.NewIssuer(t)
	var down atomic.Bool
	down.Store(true)
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "bad gateway", http.StatusBadGateway)
			return
		}
		resp, err := http.Get(iss.KeysURL())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		w.Write(buf.Bytes())
	}))
	defer front.Close()

	var out bytes.Buffer
	v, _ := auth.NewVerifier(iss.Domain, iss.Audience)
	v.UseKeysAt(front.URL, front.Client())
	v.SetLogger(logx.New(&out, slog.LevelDebug))
	clock := time.Now()
	v.SetClock(func() time.Time { return clock })

	tok := iss.Token(authtest.Spec{Email: "a@example.com"})
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Fatal("expected an error while the key server is down")
	}
	if !strings.Contains(out.String(), `level=WARN msg="access keys fetch failed, will retry with a later request"`) {
		t.Fatalf("%s", out.String())
	}
	down.Store(false)
	clock = clock.Add(time.Minute)
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !strings.Contains(out.String(), `msg="access keys refreshed" keys=1`) {
		t.Fatalf("%s", out.String())
	}
}
