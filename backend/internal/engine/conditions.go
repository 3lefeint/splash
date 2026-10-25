package engine

import (
	"context"
	"sync"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

const (
	// weatherEvery is the game time in seconds between two points of the weather history.
	weatherEvery = 60.0
	// weatherPoints is the length of the weather history: four hours at one point a minute.
	weatherPoints = 240
)

// Tyres is the live temperature and pressure picture of the driven car. Rows are the wheels (fl, fr, rl, rr).
type Tyres struct {
	// Surface and Inner hold left, centre and right of the tread in degrees Celsius.
	Surface  [][]float64 `json:"surface,omitempty"`
	Inner    [][]float64 `json:"inner,omitempty"`
	Carcass  []float64   `json:"carcass,omitempty"`
	Pressure []float64   `json:"pressure,omitempty"`
	Brake    []float64   `json:"brake,omitempty"`
	Water    *float64    `json:"water,omitempty"`
	Oil      *float64    `json:"oil,omitempty"`
	Front    string      `json:"compound_front,omitempty"`
	Rear     string      `json:"compound_rear,omitempty"`
}

// Weather is the current weather and how it developed during the session.
type Weather struct {
	Ambient   *float64       `json:"ambient"`
	Track     *float64       `json:"track"`
	Rain      *float64       `json:"rain"`
	Wetness   *float64       `json:"wetness"`
	DarkCloud *float64       `json:"dark_cloud"`
	History   []WeatherPoint `json:"history"`
}

// WeatherPoint is one sample of the history; ET is the game time.
type WeatherPoint struct {
	ET      float64  `json:"et"`
	Ambient *float64 `json:"ambient"`
	Track   *float64 `json:"track"`
	Rain    *float64 `json:"rain"`
	Wetness *float64 `json:"wetness"`
}

// weatherLog keeps the history of one session in memory; the points are stored, so a restart does not lose them.
type weatherLog struct {
	mu      sync.Mutex
	session string
	loaded  bool
	points  []WeatherPoint
}

// weatherHistory adds a point when enough game time has passed and returns a copy of the history. When the
// session is seen for the first time (also after a restart of the backend) the stored points are loaded first.
func (e *Engine) weatherHistory(ctx context.Context, session string, et float64, w *wire.Weather) []WeatherPoint {
	l := &e.weather
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.session != session {
		l.session, l.points, l.loaded = session, nil, false
	}
	if !l.loaded {
		l.loaded = true // a failed read is not retried: the history then starts again from now
		if pts, err := e.Store.WeatherPoints(ctx, session, weatherPoints); err == nil {
			for _, p := range pts {
				l.points = append(l.points, WeatherPoint{ET: p.ET, Ambient: p.Ambient, Track: p.Track, Rain: p.Rain, Wetness: p.Wetness})
			}
		}
	}
	if len(l.points) > 0 && et < l.points[len(l.points)-1].ET {
		l.points = nil
	}
	if w != nil && (len(l.points) == 0 || et-l.points[len(l.points)-1].ET >= weatherEvery) {
		p := WeatherPoint{ET: et, Ambient: w.AmbientTemp, Track: w.TrackTemp, Rain: w.Rain, Wetness: w.Wetness}
		l.points = append(l.points, p)
		if len(l.points) > weatherPoints {
			l.points = l.points[len(l.points)-weatherPoints:]
		}
		sp := store.WeatherPoint{ET: p.ET, Ambient: p.Ambient, Track: p.Track, Rain: p.Rain, Wetness: p.Wetness}
		if err := e.Store.SaveWeatherPoint(ctx, session, sp); err != nil {
			e.Log.Warn("save weather point", "err", err)
		}
	}
	return append([]WeatherPoint(nil), l.points...)
}

func weatherState(w *wire.Weather, history []WeatherPoint) *Weather {
	if w == nil {
		return nil
	}
	return &Weather{Ambient: w.AmbientTemp, Track: w.TrackTemp, Rain: w.Rain, Wetness: w.Wetness,
		DarkCloud: w.DarkCloud, History: history}
}

func tyreState(t *wire.Telemetry) *Tyres {
	if t.TyreSurfaceTemp == nil && t.TyreInnerTemp == nil && t.TyrePressure == nil && t.EngineWaterTemp == nil {
		return nil
	}
	return &Tyres{Surface: t.TyreSurfaceTemp, Inner: t.TyreInnerTemp, Carcass: t.TyreCarcassTemp,
		Pressure: t.TyrePressure, Brake: t.BrakeTemp, Water: t.EngineWaterTemp, Oil: t.EngineOilTemp,
		Front: t.CompoundFront, Rear: t.CompoundRear}
}
