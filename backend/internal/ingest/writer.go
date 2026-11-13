package ingest

import (
	"context"
	"time"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// drivingWindow is how long an agent counts as driving, or as active, after its last telemetry or live sample.
const drivingWindow = 30 * time.Second

// raceType is the session type the agents send for a race.
const raceType = "race"

// mayWrite says whether the agent of this connection writes the laps, pit events and stand-still times of a car.
// Every agent sends them for every car it sees, so without a rule a car seen by two agents is stored twice.
//
// An agent drives when it sent telemetry in the last 30 seconds, and it owns the car that telemetry belongs to.
// The reference agent is the active agent with the lowest id.
//   - Race: only the driving agent writes. Without a driving agent the reference agent writes.
//   - Any other session: the agent that owns a car writes it, cars nobody owns (rivals, AI) are written by the
//     reference agent.
//
// Without a driving agent the reference agent writes everything, so no data is lost while a driver is not detected.
func (s *Server) mayWrite(cs *connStats, sessionID string, carID int, vehicleName string) bool {
	me := cs.ref.ID
	now := time.Now()
	var (
		meDriving, anyDriving, owned bool
		owner                        int64
		ref                          int64
		haveRef                      bool
	)
	s.agentsMu.Lock()
	for id, a := range s.agents {
		if now.Sub(a.live.Updated()) <= drivingWindow && (!haveRef || id < ref) {
			ref, haveRef = id, true
		}
		snap := a.live.Snapshot()
		if snap.SessionID != sessionID || snap.Telemetry == nil || now.Sub(a.live.TelemetryUpdated()) > drivingWindow {
			continue
		}
		anyDriving = true
		if id == me {
			meDriving = true
		}
		if snap.Telemetry.CarID == carID && snap.Telemetry.VehicleName == vehicleName && (!owned || id < owner) {
			owned, owner = true, id
		}
	}
	s.agentsMu.Unlock()
	if !haveRef {
		return true // nobody has sent anything lately, so there is nothing to choose from
	}
	if cs.st.sess.sessionType() == raceType && anyDriving {
		return meDriving
	}
	if owned {
		return me == owner
	}
	return me == ref
}

// observeWeather stores a point of the weather history when enough game time has passed since the last one of the
// session. The weather belongs to the session, not to a car: the writing agent is the one mayWrite picks for no car,
// so several agents of a team store one point and not one each.
func (s *Server) observeWeather(ctx context.Context, cs *connStats, session string, et float64, w *wire.Weather) {
	if w == nil || !s.mayWrite(cs, session, -1, "") {
		return
	}
	s.wxMu.Lock()
	defer s.wxMu.Unlock()
	last, ok := s.wxLast[session]
	if ok && et >= last && et-last < store.WeatherEvery {
		return
	}
	if s.wxLast == nil {
		s.wxLast = map[string]float64{}
	}
	s.wxLast[session] = et
	p := store.WeatherPoint{ET: et, Ambient: w.AmbientTemp, Track: w.TrackTemp, Rain: w.Rain, Wetness: w.Wetness, AgentID: cs.ref.ID}
	if err := s.Store.SaveWeatherPoint(ctx, session, p); err != nil {
		s.Log.Error("save weather point", "team", s.TeamID, "err", err)
	}
}
