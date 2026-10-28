package api

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
)

func (a *API) getSessions(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Sessions(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

type csvRows struct {
	header []string
	rows   [][]string
}

func fp(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func ff(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func ip(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func nth(v []float64, i int) string {
	if i >= len(v) {
		return ""
	}
	return ff(v[i])
}

func bs(b bool) string { return strconv.FormatBool(b) }

func (a *API) exportSession(w http.ResponseWriter, r *http.Request) {
	id, kind := r.PathValue("id"), r.URL.Query().Get("kind")
	ctx := r.Context()
	var out csvRows
	switch kind {
	case "laps":
		laps, err := a.Store.Laps(ctx, id)
		if err != nil {
			serverError(w, err)
			return
		}
		out.header = []string{"car_id", "vehicle_name", "driver", "lap", "lap_time", "s1", "s2", "s3", "fuel_start", "fuel_end",
			"tyre_age", "wear_fl", "wear_fr", "wear_rl", "wear_rr", "fcy", "in_lap", "out_lap"}
		for _, l := range laps {
			out.rows = append(out.rows, []string{strconv.Itoa(l.CarID), l.VehicleName, l.Driver, strconv.Itoa(l.Lap.Lap),
				fp(l.LapTime), fp(l.S1), fp(l.S2), fp(l.S3), fp(l.FuelStart), fp(l.FuelEnd), ip(l.TyreAge),
				nth(l.Wear, 0), nth(l.Wear, 1), nth(l.Wear, 2), nth(l.Wear, 3), bs(l.FCY), bs(l.InLap), bs(l.OutLap)})
		}
	case "stops":
		stops, err := a.Store.Stops(ctx, id)
		if err != nil {
			serverError(w, err)
			return
		}
		out.header = []string{"car_id", "vehicle_name", "entry_et", "exit_et", "fuel_added", "tyres_changed", "driver_swap",
			"stopped", "work", "fuel_time"}
		for _, s := range stops {
			out.rows = append(out.rows, []string{strconv.Itoa(s.CarID), s.VehicleName, ff(s.EntryET), fp(s.ExitET),
				fp(s.FuelAdded), bs(s.TyresChanged), bs(s.DriverSwap), ff(s.Stopped), ff(s.Work), ff(s.FuelTime)})
		}
	case "conditions":
		list, err := a.Store.LapConditions(ctx, id)
		if err != nil {
			serverError(w, err)
			return
		}
		out.header = []string{"car_id", "vehicle_name", "lap", "et", "control", "ambient", "track", "rain", "wetness",
			"surface_fl", "surface_fr", "surface_rl", "surface_rr", "inner_fl", "inner_fr", "inner_rl", "inner_rr",
			"pressure_fl", "pressure_fr", "pressure_rl", "pressure_rr", "brake_fl", "brake_fr", "brake_rl", "brake_rr",
			"water", "oil", "compound_front", "compound_rear"}
		for _, c := range list {
			row := []string{strconv.Itoa(c.CarID), c.VehicleName, strconv.Itoa(c.Lap), ff(c.ET), ip(c.Control),
				fp(c.Ambient), fp(c.Track), fp(c.Rain), fp(c.Wetness)}
			for _, arr := range [][]float64{c.Surface, c.Inner, c.Pressure, c.Brake} {
				for i := 0; i < 4; i++ {
					row = append(row, nth(arr, i))
				}
			}
			out.rows = append(out.rows, append(row, fp(c.Water), fp(c.Oil), c.CompoundFront, c.CompoundRear))
		}
	default:
		fail(w, http.StatusBadRequest, "kind must be laps, stops or conditions")
		return
	}
	name := strings.Map(func(r rune) rune {
		if r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, id)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="splash-`+name+`-`+kind+`.csv"`)
	cw := csv.NewWriter(w)
	cw.Write(out.header)
	cw.WriteAll(out.rows)
}
