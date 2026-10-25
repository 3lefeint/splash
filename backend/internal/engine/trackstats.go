package engine

import (
	"math"

	"github.com/3lefeint/splash/backend/internal/pitlane"
	"github.com/3lefeint/splash/backend/internal/segments"
)

// statLaps is the number of laps the section values are averaged over.
const statLaps = 3

// offTrackMargin is how far in metres past the track edge the centre of a car must be to count as off the track.
const offTrackMargin = 1.5

// VsCar is the time we gain or lose in each section against another car, in seconds per section, averaged over the
// last laps: negative means we are faster there. Null where no value is known.
type VsCar struct {
	Name  string     `json:"name"`
	Delta []*float64 `json:"delta"`
	Laps  int        `json:"laps"`
}

// PitLaneState is the pit lane on the map: entry, exit, the boxes and the box of our car.
type PitLaneState struct {
	Entry  *pitlane.Point  `json:"entry,omitempty"`
	Exit   *pitlane.Point  `json:"exit,omitempty"`
	Boxes  []pitlane.Point `json:"boxes"`
	OurBox *pitlane.Point  `json:"our_box,omitempty"`
}

// TrackStats is how our car spends a lap along the track, section by section; section i covers the distance from
// i*L/Segments to (i+1)*L/Segments of the lap.
type TrackStats struct {
	Segments int `json:"segments"`
	// Laps is the number of laps the values of our car are averaged over.
	Laps   int           `json:"laps"`
	Ahead  *VsCar        `json:"ahead,omitempty"`
	Behind *VsCar        `json:"behind,omitempty"`
	Best   []*float64    `json:"best,omitempty"`  // our recent laps minus our best lap
	Fuel   []*float64    `json:"fuel,omitempty"`  // litres used in each section
	Speed  []*float64    `json:"speed,omitempty"` // mean speed in m/s
	Brake  []*float64    `json:"brake,omitempty"` // strongest brake pedal, 0..1
	Pits   *PitLaneState `json:"pits,omitempty"`
}

func list(p segments.Profile) []*float64 {
	any := false
	out := make([]*float64, len(p))
	for i, v := range p {
		out[i] = v
		any = any || v != nil
	}
	if !any {
		return nil
	}
	return out
}

// PitSource tells what is known about the pit lane.
type PitSource interface {
	Lane(track string) *pitlane.Lane
	StoppedAt(vehicle string, id int) *pitlane.Point
}

// trackStats collects the section values of our car and the pit lane; nil when there is nothing to show yet.
func (e *Engine) trackStats(session, track string, car Car, tl float64, opp Opponents) *TrackStats {
	ts := &TrackStats{Segments: segments.N}
	any := false
	if e.Field != nil && tl > 0 {
		ours := e.Field.Laps(carKey(car.VehicleName, car.CarID))
		mine := segments.MeanTime(ours, statLaps)
		ts.Laps = min(len(ours), statLaps)
		vs := func(o *Opponent) *VsCar {
			if o == nil {
				return nil
			}
			theirs := e.Field.Laps(carKey(o.VehicleName, o.CarID))
			d := list(segments.Diff(mine, segments.MeanTime(theirs, statLaps)))
			if d == nil {
				return nil
			}
			name := o.Driver
			if name == "" {
				name = o.VehicleName
			}
			return &VsCar{Name: name, Delta: d, Laps: min(len(theirs), statLaps)}
		}
		ts.Ahead, ts.Behind = vs(opp.Ahead), vs(opp.Behind)
		if best := e.Field.Best(carKey(car.VehicleName, car.CarID)); best != nil {
			ts.Best = list(segments.Diff(mine, segments.Of(best)))
		}
		any = any || ts.Ahead != nil || ts.Behind != nil || ts.Best != nil
	}
	if e.Own != nil && tl > 0 {
		laps := e.Own.Laps(carKey(car.VehicleName, car.CarID))
		ts.Fuel = list(segments.MeanFuel(laps, statLaps))
		ts.Speed = list(segments.Speeds(segments.MeanTime(laps, statLaps), tl))
		ts.Brake = list(segments.MaxBrake(laps, statLaps))
		any = any || ts.Fuel != nil || ts.Speed != nil || ts.Brake != nil
	}
	if e.Pits != nil {
		if l := e.Pits.Lane(track); l != nil {
			ts.Pits = &PitLaneState{Entry: l.Entry, Exit: l.Exit, Boxes: l.Boxes, OurBox: e.Pits.StoppedAt(car.VehicleName, car.CarID)}
			any = true
		}
	}
	if !any {
		return nil
	}
	return ts
}

// offTrack is true when the car is clearly beyond the edge of the track and not in the pit lane.
func offTrack(lateral, edge *float64, inPits bool) bool {
	return !inPits && lateral != nil && edge != nil && *edge > 0 && math.Abs(*lateral) > *edge+offTrackMargin
}
