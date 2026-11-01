package engine

import (
	"math"
	"sort"

	"github.com/3lefeint/splash/backend/internal/store"
)

// fitCollinear is the correlation between fuel load and tyre age above which their effects cannot be told apart.
const fitCollinear = 0.97

// Reasons a lap is left out of the fit.
const (
	excludeMissing = "missing_data"
	excludeInLap   = "in_lap"
	excludeOutLap  = "out_lap"
	excludeFCY     = "fcy"
	excludeWet     = "wet"
	excludeSlow    = "too_slow"
	excludeManual  = "manual"
)

// FitOptions say which laps count for the model; they come from the parameters of the track and car.
type FitOptions struct {
	// MinLaps is the number of clean laps needed before the model is fitted.
	MinLaps int
	// SlowPercent drops laps this many percent slower than the median lap time (traffic, mistakes), from 3 laps on.
	SlowPercent float64
	// SkipFCY and SkipWet leave out laps under full course yellow and laps on a wet track.
	SkipFCY, SkipWet bool
}

// fitOptions reads the options from the parameters; unset values are the defaults.
func fitOptions(p store.Params) FitOptions {
	p = p.WithDefaults()
	return FitOptions{MinLaps: p.FitMinLaps, SlowPercent: p.FitSlowPercent, SkipFCY: *p.FitSkipFCY, SkipWet: *p.FitSkipWet}
}

// ExcludedLap is a lap that did not count for the model and why: missing_data, in_lap, out_lap, fcy, wet, too_slow or manual (left out by a person).
type ExcludedLap struct {
	Lap    int    `json:"lap"`
	Reason string `json:"reason"`
}

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
	// MinLaps is the number of clean laps the model needs (a setting).
	MinLaps int `json:"min_laps"`
	// Excluded lists the laps of the car that did not count and why. It is filled only while the model cannot be
	// fitted for lack of laps, else empty; never null.
	Excluded []ExcludedLap `json:"excluded"`
}

// fitModel fits lap time = base + fuelEffect * fuel + degPerLap * tyreAge to the clean laps of one car.
// bounds are the laps after which a new tyre set started (see setBoundaries). wet tells the laps driven on a wet
// track; it is used only with opt.SkipWet.
func fitModel(laps []store.LapRecord, bounds []int, wet func(lap int) bool, opt FitOptions) *Fit {
	type row struct {
		lap          int
		fuel, age, t float64
	}
	var rows []row
	excluded := []ExcludedLap{}
	for _, r := range laps {
		l := r.Lap
		reason := ""
		switch {
		case r.Excluded:
			reason = excludeManual
		case l.LapTime == nil || l.FuelStart == nil || l.FuelEnd == nil:
			reason = excludeMissing
		case l.InLap:
			reason = excludeInLap
		case l.OutLap:
			reason = excludeOutLap
		case opt.SkipFCY && l.FCY:
			reason = excludeFCY
		case opt.SkipWet && wet != nil && wet(l.Lap):
			reason = excludeWet
		}
		if reason != "" {
			excluded = append(excluded, ExcludedLap{l.Lap, reason})
			continue
		}
		start := 0
		for _, b := range bounds {
			if b < l.Lap && b > start {
				start = b
			}
		}
		rows = append(rows, row{l.Lap, (*l.FuelStart + *l.FuelEnd) / 2, float64(l.Lap - start), *l.LapTime})
	}
	if len(rows) >= 3 {
		ts := make([]float64, len(rows))
		for i, r := range rows {
			ts[i] = r.t
		}
		med := *median(ts)
		kept := rows[:0]
		for _, r := range rows {
			if r.t <= med*(1+opt.SlowPercent/100) {
				kept = append(kept, r)
			} else {
				excluded = append(excluded, ExcludedLap{r.lap, excludeSlow})
			}
		}
		rows = kept
	}
	out := &Fit{Laps: len(rows), MinLaps: opt.MinLaps, Excluded: []ExcludedLap{}}
	if len(rows) < opt.MinLaps {
		out.Reason = "few_laps"
		sort.Slice(excluded, func(i, j int) bool { return excluded[i].Lap < excluded[j].Lap })
		out.Excluded = excluded
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
