// Package live keeps the latest scoring and telemetry sample in memory.
package live

import (
	"sync"

	"github.com/3lefeint/splash/backend/internal/wire"
)

// State holds the newest sample per kind. When two agents send the same kind
// (scoring from driver and spotter) the sample with the higher game time wins.
type State struct {
	mu        sync.RWMutex
	sessionID string
	scoring   *wire.Scoring
	scoringET float64
	telemetry *wire.Telemetry
	telemET   float64
	firstET   float64
	hasFirst  bool
}

// Snapshot is a copy of the current live state.
type Snapshot struct {
	SessionID string
	Scoring   *wire.Scoring
	ScoringET float64
	Telemetry *wire.Telemetry
	TelemET   float64
	// FirstET is the game time of the first telemetry sample of the session;
	// it stands in for the start of the first stint.
	FirstET float64
}

func (s *State) resetOnNewSession(id string) {
	if s.sessionID != id {
		s.sessionID = id
		s.scoring, s.scoringET = nil, 0
		s.telemetry, s.telemET = nil, 0
		s.firstET, s.hasFirst = 0, false
	}
}

// UpdateScoring stores the sample unless a newer one is already present.
func (s *State) UpdateScoring(sessionID string, et float64, v wire.Scoring) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetOnNewSession(sessionID)
	if s.scoring != nil && et < s.scoringET {
		return
	}
	s.scoring, s.scoringET = &v, et
}

// UpdateTelemetry stores the sample unless a newer one is already present.
func (s *State) UpdateTelemetry(sessionID string, et float64, v wire.Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetOnNewSession(sessionID)
	if !s.hasFirst || et < s.firstET {
		s.firstET, s.hasFirst = et, true
	}
	if s.telemetry != nil && et < s.telemET {
		return
	}
	s.telemetry, s.telemET = &v, et
}

// Snapshot returns the current state.
func (s *State) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Snapshot{s.sessionID, s.scoring, s.scoringET, s.telemetry, s.telemET, s.firstET}
}
