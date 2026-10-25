package engine

import (
	"math"

	"github.com/3lefeint/splash/backend/internal/store"
)

const (
	// fitMinLaps is the number of clean laps needed before the model is fitted.
	fitMinLaps = 8
	// fitSlowFactor drops laps slower than this multiple of the median lap time (traffic, mistakes).
	fitSlowFactor = 1.08
	// fitCollinear is the correlation between fuel load and tyre age above which their effects cannot be told apart.
	fitCollinear = 0.97
)

// Fit is the lap time model estimated from the clean laps of the car so far: the lap gets slower by FuelEffect
// seconds per litre in the tank and by DegPerLap seconds per lap of tyre age.
type Fit struct {
	Laps       int      `json:"laps"`
	FuelEffect *float64 `json:"fuel_effect"`
	DegPerLap  *float64 `json:"deg_per_lap"`
	// Base is the fitted lap time with an empty tank on new tyres, R2 the share of the lap time variation explained.
	Base float64 `json:"base"`
	R2   float64 `json:"r2"`
	// Reason is set when no model could be fitted: few_laps or collinear.
	Reason string `json:"reason,omitempty"`
}

// fitModel fits lap time = base + fuelEffect * fuel + degPerLap * tyreAge to the clean laps of one car.
// bounds are the laps after which a new tyre set started (see setBoundaries). skip names laps to leave out, e.g. wet ones.
func fitModel(laps []store.LapRecord, bounds []int, skip func(lap int) bool) *Fit {
	type row struct{ fuel, age, t float64 }
	var rows []row
	for _, r := range laps {
		l := r.Lap
		if l.LapTime == nil || l.FuelStart == nil || l.FuelEnd == nil || l.InLap || l.OutLap || l.FCY || (skip != nil && skip(l.Lap)) {
			continue
		}
		start := 0
		for _, b := range bounds {
			if b < l.Lap && b > start {
				start = b
			}
		}
		rows = append(rows, row{(*l.FuelStart + *l.FuelEnd) / 2, float64(l.Lap - start), *l.LapTime})
	}
	if len(rows) >= 3 {
		ts := make([]float64, len(rows))
		for i, r := range rows {
			ts[i] = r.t
		}
		med := *median(ts)
		kept := rows[:0]
		for _, r := range rows {
			if r.t <= med*fitSlowFactor {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	out := &Fit{Laps: len(rows)}
	if len(rows) < fitMinLaps {
		out.Reason = "few_laps"
		return out
	}
	n := float64(len(rows))
	var mf, ma, mt float64
	for _, r := range rows {
		mf, ma, mt = mf+r.fuel, ma+r.age, mt+r.t
	}
	mf, ma, mt = mf/n, ma/n, mt/n
	var sff, saa, sfa, sft, sat, stt float64
	for _, r := range rows {
		df, da, dt := r.fuel-mf, r.age-ma, r.t-mt
		sff, saa, sfa = sff+df*df, saa+da*da, sfa+df*da
		sft, sat, stt = sft+df*dt, sat+da*dt, stt+dt*dt
	}
	var b, c float64
	haveF, haveA := sff > 1e-6, saa > 1e-6
	switch {
	case haveF && haveA:
		if math.Abs(sfa)/math.Sqrt(sff*saa) > fitCollinear {
			out.Reason = "collinear"
			return out
		}
		det := sff*saa - sfa*sfa
		b = (sft*saa - sat*sfa) / det
		c = (sat*sff - sft*sfa) / det
	case haveF:
		b = sft / sff
	case haveA:
		c = sat / saa
	default:
		out.Reason = "few_laps"
		return out
	}
	out.Base = mt - b*mf - c*ma
	// Stay at zero for what the data could not tell, so a missing effect is not reported as "no effect".
	if haveF {
		out.FuelEffect = &b
	}
	if haveA {
		out.DegPerLap = &c
	}
	if stt > 0 {
		out.R2 = math.Min(math.Max((b*sft+c*sat)/stt, 0), 1)
	}
	return out
}
