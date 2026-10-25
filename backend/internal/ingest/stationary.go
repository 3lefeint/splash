package ingest

import (
	"strconv"
	"sync"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// pitStateStopped is the plugin's pit state of a car that stands in its pit box.
	pitStateStopped = 3
	// fuelRise is the fuel gain in litres between two samples that counts as refuelling.
	fuelRise = 0.02
	// wearJump is the rise of the mean wear value that counts as a tyre change.
	wearJump = 0.05
)

type stood struct {
	CarID       int
	VehicleName string
	store.Standing
}

type carStop struct {
	lastET float64
	from   float64
	in     bool

	// work seen by the telemetry while the car stands; times are game time, 0 means not seen
	lastFuel, lastWear      float64
	haveLast                bool
	fuelFrom, fuelTo, tyres float64
	lastTelET               float64
}

// stationary finds the times cars stand in their pit boxes from the pit state of the scoring samples and, from
// the telemetry of the driven car, how long the work in the box took.
type stationary struct {
	mu      sync.Mutex
	session string
	cars    map[string]*carStop
}

func key(id int, vehicle string) string { return vehicle + "|" + strconv.Itoa(id) }

func (s *stationary) car(session, k string, et float64) *carStop {
	if s.session != session || s.cars == nil {
		s.session, s.cars = session, map[string]*carStop{}
	}
	c := s.cars[k]
	if c == nil {
		c = &carStop{lastET: et - 1}
		s.cars[k] = c
	}
	return c
}

// observe processes a scoring sample taken at game time et and returns the stand-stills that just ended.
// A sample that is not newer than the last one of a car (the same sample from a second agent) is ignored.
func (s *stationary) observe(session string, et float64, vehicles []wire.ScoringVehicle) []stood {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []stood
	for _, v := range vehicles {
		c := s.car(session, key(v.CarID, v.VehicleName), et)
		if et <= c.lastET {
			continue
		}
		c.lastET = et
		switch {
		case v.PitState == pitStateStopped && !c.in:
			c.in, c.from = true, et
			c.haveLast, c.fuelFrom, c.fuelTo, c.tyres = false, 0, 0, 0
		case v.PitState != pitStateStopped && c.in:
			c.in = false
			st := store.Standing{From: c.from, To: et}
			if last := max(c.fuelTo, c.tyres); last > c.from {
				st.Work = last - c.from
			}
			if c.fuelTo > c.fuelFrom {
				st.FuelTime = c.fuelTo - c.fuelFrom
			}
			out = append(out, stood{v.CarID, v.VehicleName, st})
		}
	}
	return out
}

// telemetry notes fuel and tyre changes of the driven car while it stands in its box.
func (s *stationary) telemetry(session string, et float64, t wire.Telemetry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.car(session, key(t.CarID, t.VehicleName), et)
	if !c.in || len(t.Wear) != 4 || et <= c.lastTelET {
		return
	}
	wear := (t.Wear[0] + t.Wear[1] + t.Wear[2] + t.Wear[3]) / 4
	if c.haveLast {
		if t.Fuel > c.lastFuel+fuelRise {
			if c.fuelFrom == 0 {
				c.fuelFrom = c.lastTelET
			}
			c.fuelTo = et
		}
		if wear > c.lastWear+wearJump {
			c.tyres = et
		}
	}
	c.lastFuel, c.lastWear, c.haveLast, c.lastTelET = t.Fuel, wear, true, et
}
