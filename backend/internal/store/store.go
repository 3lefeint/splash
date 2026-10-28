// Package store persists laps, pit stops and the per-agent sequence progress.
package store

import (
	"context"
	"time"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/trackmap"
	"github.com/3lefeint/splash/backend/internal/wear"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// LapRecord is a stored lap.
type LapRecord struct {
	SessionID string
	wire.Lap
}

// StopRecord is a stored pit stop.
type StopRecord struct {
	SessionID    string
	CarID        int
	VehicleName  string
	EntryET      float64
	ExitET       *float64
	FuelAdded    *float64
	TyresChanged bool
	DriverSwap   bool
	// Stopped is the time in seconds the car stood in its pit box, 0 when unknown.
	Stopped float64
	// Work is the time from standing still to the last change of fuel or tyres, FuelTime the time fuel was flowing.
	// Both are 0 when unknown. A car may stand longer than it works, e.g. while the driver waits.
	Work, FuelTime float64
}

// Standing is one stand-still of a car in its pit box.
type Standing struct {
	From, To float64
	// Work and FuelTime in seconds, see StopRecord.
	Work, FuelTime float64
}

// Store is the persistence used by the ingest. Writes are idempotent: the same
// lap or event arriving from both agents, or twice after a resend, is stored once.
type Store interface {
	// SaveLap upserts a lap. A field that is null in the new record keeps its
	// stored value, so the driver agent's fuel data and the spotter agent's
	// field data end up in one row.
	SaveLap(ctx context.Context, sessionID string, l wire.Lap) error
	// SaveEvent applies a pit entry/exit or driver swap to the stops. Other kinds are ignored.
	SaveEvent(ctx context.Context, sessionID string, et float64, e wire.Event) error
	// Progress returns the highest durable seq stored for an agent and session.
	Progress(ctx context.Context, agentID, sessionID string) (seq int64, ok bool, err error)
	// SetProgress records the highest durable seq. It never moves backwards.
	SetProgress(ctx context.Context, agentID, sessionID string, seq int64) error
	Laps(ctx context.Context, sessionID string) ([]LapRecord, error)
	// LapsOfCar returns the laps of one car ordered by lap number.
	LapsOfCar(ctx context.Context, sessionID string, carID int, vehicleName string) ([]LapRecord, error)
	Stops(ctx context.Context, sessionID string) ([]StopRecord, error)
	// AddStopped adds a stand-still (from game time from to to) and the work done during it to the stop it belongs to.
	AddStopped(ctx context.Context, sessionID string, carID int, vehicleName string, st Standing) error

	// GetParams returns the parameter set of a track and car; ok is false when none exists.
	GetParams(ctx context.Context, track, car string) (p Params, ok bool, err error)
	ListParams(ctx context.Context) ([]Params, error)
	SetParams(ctx context.Context, p Params) error
	// GetStintPlan returns the global stint plan in order.
	GetStintPlan(ctx context.Context) ([]PlanRow, error)
	// SetStintPlan replaces the whole stint plan.
	SetStintPlan(ctx context.Context, rows []PlanRow) error
	GetOverrides(ctx context.Context) (map[string]Override, error)
	SetOverride(ctx context.Context, key string, value float64) error
	DeleteOverride(ctx context.Context, key string) error

	// GetTrackMap returns the outline of a track; ok is false when none was recorded yet.
	GetTrackMap(ctx context.Context, track string) (m trackmap.Map, ok bool, err error)
	// SaveTrackMap stores the outline of a track, replacing an existing one.
	SaveTrackMap(ctx context.Context, m trackmap.Map) error
	DeleteTrackMap(ctx context.Context, track string) error
	// ListTracks returns the tracks that have a stored outline, by name.
	ListTracks(ctx context.Context) ([]TrackInfo, error)
	// GetCorners returns the corner names of a track in lap order; empty when none were entered.
	GetCorners(ctx context.Context, track string) ([]Corner, error)
	// SaveCorners replaces the corner names of a track.
	SaveCorners(ctx context.Context, track string, corners []Corner) error

	// PitSetups returns the saved pit stop setups of a car in the order they were created.
	PitSetups(ctx context.Context, car string) ([]PitSetup, error)
	// SavePitSetup stores a setup, replacing one of the same car and name.
	SavePitSetup(ctx context.Context, s PitSetup) error
	DeletePitSetup(ctx context.Context, car, name string) error

	// TouchSession records that a session is running: it is created on first sight, later calls keep its start and
	// update the last sign of life and the fields that are not empty.
	TouchSession(ctx context.Context, s SessionInfo) error
	// Sessions lists every session seen, newest first, with what is stored for it.
	Sessions(ctx context.Context) ([]SessionSummary, error)
	// SaveLapConditions stores the conditions of one lap of the driven car, replacing an earlier record of that lap.
	SaveLapConditions(ctx context.Context, c LapConditions) error
	// LapConditions returns the stored laps of a session ordered by car and lap.
	LapConditions(ctx context.Context, sessionID string) ([]LapConditions, error)

	// SaveWeatherPoint stores one sample of the weather history of a session; the same game time replaces itself.
	SaveWeatherPoint(ctx context.Context, sessionID string, p WeatherPoint) error
	// WeatherPoints returns the newest points of a session, oldest first.
	WeatherPoints(ctx context.Context, sessionID string, limit int) ([]WeatherPoint, error)

	// GetPitLane returns what was learned about the pit lane of a track; ok is false when nothing was stored yet.
	GetPitLane(ctx context.Context, track string) (l pitlane.Lane, ok bool, err error)
	// SavePitLane stores the pit lane of a track, replacing an earlier one.
	SavePitLane(ctx context.Context, l pitlane.Lane) error

	// SaveLapWear stores the tyre wear by track section of one lap, replacing an earlier record of the same lap.
	SaveLapWear(ctx context.Context, sessionID string, carID int, vehicleName string, w wear.LapWear) error
	// LapWears returns the stored laps of one car ordered by lap number.
	LapWears(ctx context.Context, sessionID string, carID int, vehicleName string) ([]wear.LapWear, error)
}

// PitEntryWindow is the time in seconds within which two pit entries of the same
// car count as one stop. Both agents report every stop, each with its own sample time.
const PitEntryWindow = 30.0

// mergeLap applies the null-keeps-old rule shared by all implementations.
func mergeLap(old, n wire.Lap) wire.Lap {
	out := old
	if n.Driver != "" {
		out.Driver = n.Driver
	}
	pick := func(dst **float64, v *float64) {
		if v != nil {
			*dst = v
		}
	}
	pick(&out.LapTime, n.LapTime)
	pick(&out.S1, n.S1)
	pick(&out.S2, n.S2)
	pick(&out.S3, n.S3)
	pick(&out.FuelStart, n.FuelStart)
	pick(&out.FuelEnd, n.FuelEnd)
	if n.TyreAge != nil {
		out.TyreAge = n.TyreAge
	}
	if len(n.Wear) == 4 {
		out.Wear = n.Wear
	}
	out.FCY = old.FCY || n.FCY
	out.InLap = old.InLap || n.InLap
	out.OutLap = old.OutLap || n.OutLap
	return out
}

// PitSetup is a named list of pit menu rows to set, e.g. maximum fuel and medium tyres.
type PitSetup struct {
	Car   string      `json:"car"`
	Name  string      `json:"name"`
	Steps []wire.Step `json:"steps"`
}

// SessionInfo identifies a game session. Times are wall clock; LastET is the game time of the last sign of life.
type SessionInfo struct {
	SessionID   string    `json:"session_id"`
	Track       string    `json:"track"`
	SessionType string    `json:"session_type"`
	Car         string    `json:"car"`
	CarID       int       `json:"car_id"` // slot id of the driven car, -1 while unknown
	StartedAt   time.Time `json:"started_at"`
	LastSeen    time.Time `json:"last_seen"`
	LastET      float64   `json:"last_et"`
}

// SessionSummary is a session with the amount of data stored for it.
type SessionSummary struct {
	SessionInfo
	Laps       int `json:"laps"`        // laps of all cars
	Stops      int `json:"stops"`       // pit stops of all cars
	Conditions int `json:"conditions"`  // laps of the driven car with conditions
	AILaps     int `json:"ai_laps"`     // of those, laps the AI drove
	PlayerLaps int `json:"player_laps"` // of those, laps a person drove
}

// LapConditions is what the driven car and the weather were like during one lap, averaged over the lap.
// Control is mControl of the car: 0 local player, 1 local AI, 2 remote; nil when unknown.
type LapConditions struct {
	SessionID     string    `json:"session_id"`
	CarID         int       `json:"car_id"`
	VehicleName   string    `json:"vehicle_name"`
	Lap           int       `json:"lap"`
	ET            float64   `json:"et"`
	Control       *int      `json:"control"`
	Ambient       *float64  `json:"ambient"`
	Track         *float64  `json:"track"`
	Rain          *float64  `json:"rain"`
	Wetness       *float64  `json:"wetness"`
	Surface       []float64 `json:"surface"` // mean tread temperature per wheel (fl, fr, rl, rr)
	Inner         []float64 `json:"inner"`
	Pressure      []float64 `json:"pressure"` // kPa
	Brake         []float64 `json:"brake"`
	Water         *float64  `json:"water"`
	Oil           *float64  `json:"oil"`
	CompoundFront string    `json:"compound_front"`
	CompoundRear  string    `json:"compound_rear"`
}

// WeatherPoint is one sample of the weather during a session; ET is the game time.
type WeatherPoint struct {
	ET      float64  `json:"et"`
	Ambient *float64 `json:"ambient"`
	Track   *float64 `json:"track"`
	Rain    *float64 `json:"rain"`
	Wetness *float64 `json:"wetness"`
}

// Corner is a named stretch of a track. From and To are distances along the lap in metres in driving direction (To
// below From means the stretch crosses the start line); nil while not marked. Label is where the name is shown on the
// map, in world coordinates; nil for the default place.
type Corner struct {
	Name  string      `json:"name"`
	From  *float64    `json:"from"`
	To    *float64    `json:"to"`
	Label *[2]float64 `json:"label"`
}

// TrackInfo names a track with a stored outline and its lap length in metres.
type TrackInfo struct {
	Track  string  `json:"track"`
	Length float64 `json:"length"`
}
