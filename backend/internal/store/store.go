// Package store persists laps, pit stops and the per-agent sequence progress.
package store

import (
	"context"

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
}

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
