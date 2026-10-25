package live

import (
	"testing"

	"github.com/3lefeint/splash/backend/internal/wire"
)

func TestNewestSampleWins(t *testing.T) {
	var s State
	s.UpdateScoring("s1", 10, wire.Scoring{Track: "new"})
	s.UpdateScoring("s1", 5, wire.Scoring{Track: "old"}) // late sample from the other agent
	if got := s.Snapshot().Scoring.Track; got != "new" {
		t.Fatalf("got %q", got)
	}
	s.UpdateScoring("s1", 10, wire.Scoring{Track: "same-et"})
	if got := s.Snapshot().Scoring.Track; got != "same-et" {
		t.Fatalf("equal et must replace, got %q", got)
	}
}

func TestSessionChangeResets(t *testing.T) {
	var s State
	s.UpdateScoring("s1", 100, wire.Scoring{Track: "a"})
	s.UpdateTelemetry("s1", 100, wire.Telemetry{Fuel: 1})
	s.UpdateScoring("s2", 1, wire.Scoring{Track: "b"})
	snap := s.Snapshot()
	if snap.SessionID != "s2" || snap.Scoring.Track != "b" || snap.Telemetry != nil {
		t.Fatalf("%+v", snap)
	}
}
