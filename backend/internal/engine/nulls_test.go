package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/3lefeint/splash/backend/internal/store"
	"github.com/3lefeint/splash/backend/internal/wire"
)

// nilLists walks a value and returns the path of every slice and map that is nil: encoding/json writes those as null,
// and the page calls .map or .length on them. Nil pointers are meant ("no value") and are left alone, and so are the
// elements of a list. New fields are covered without touching this test.
func nilLists(v reflect.Value, path string, out *[]string) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			nilLists(v.Elem(), path, out)
		}
	case reflect.Slice:
		if v.IsNil() {
			*out = append(*out, path)
			return
		}
		for i := 0; i < v.Len(); i++ {
			nilLists(v.Index(i), fmt.Sprintf("%s[%d]", path, i), out)
		}
	case reflect.Map:
		if v.IsNil() {
			*out = append(*out, path)
			return
		}
		for _, k := range v.MapKeys() {
			nilLists(v.MapIndex(k), fmt.Sprintf("%s[%v]", path, k), out)
		}
	case reflect.Array:
		for i := 0; i < v.Len(); i++ {
			nilLists(v.Index(i), fmt.Sprintf("%s[%d]", path, i), out)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			tag := f.Tag.Get("json")
			if !f.IsExported() || tag == "-" {
				continue
			}
			// a nil list or map with omitempty is left out of the JSON, not written as null
			if (v.Field(i).Kind() == reflect.Slice || v.Field(i).Kind() == reflect.Map) && v.Field(i).IsNil() && strings.Contains(tag, "omitempty") {
				continue
			}
			nilLists(v.Field(i), path+"."+f.Name, out)
		}
	}
}

func assertNoNilLists(t *testing.T, name string, v any) {
	t.Helper()
	var bad []string
	nilLists(reflect.ValueOf(v), "State", &bad)
	for _, p := range bad {
		t.Errorf("%s: %s is a nil list or map and would be sent as null", name, p)
	}
}

// states builds the state in the situations a team without data goes through, from nothing to a filled race.
func TestStateHasNoNullListsOrMaps(t *testing.T) {
	ctx := context.Background()
	compute := func(r *rig) State {
		st, err := r.eng.Compute(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	t.Run("a brand-new team", func(t *testing.T) {
		assertNoNilLists(t, "empty", compute(newRig()))
	})
	t.Run("only scoring arrived", func(t *testing.T) {
		r := newRig()
		r.lv.UpdateScoring(sess, 100, wire.Scoring{Track: "monza", SessionType: "race", EndET: 10000})
		assertNoNilLists(t, "scoring only", compute(r))
	})
	t.Run("our car alone, no laps yet", func(t *testing.T) {
		r := newRig()
		r.setLive(100, 80, 1, vehicle(0, "me", "GT3", 1, 0))
		st := compute(r)
		if !st.Ready {
			t.Fatal("expected a ready state")
		}
		assertNoNilLists(t, "alone", st)
	})
	t.Run("our car alone with laps", func(t *testing.T) {
		r := newRig()
		r.laps(6)
		r.setLive(700, 72, 6, vehicle(0, "me", "GT3", 1, 0))
		assertNoNilLists(t, "alone with laps", compute(r))
	})
	t.Run("a field with rivals and laps", func(t *testing.T) {
		r := newRig()
		r.laps(8)
		r.setLive(900, 66, 8, vehicle(0, "me", "GT3", 2, 4), vehicle(1, "rival", "GT3", 1, 0), vehicle(2, "other", "GT3", 3, 7))
		assertNoNilLists(t, "field", compute(r))
	})
	t.Run("pit parameters are set and nobody can pass us", func(t *testing.T) {
		r := newRig()
		r.laps(6)
		r.st.SetParams(ctx, store.Params{Track: "monza", Car: "me", PitThroughGreen: 20, RefuelRate: 2.5, TyreChange: 10})
		r.setLive(700, 40, 6, vehicle(0, "me", "GT3", 1, 0))
		st := compute(r)
		if st.Strategy == nil || st.Strategy.Rejoin == nil {
			t.Fatal("expected a rejoin estimate")
		}
		assertNoNilLists(t, "rejoin", st)
		r.setLive(710, 40, 6, vehicle(0, "me", "GT3", 2, 5), vehicle(1, "rival", "GT3", 1, 0))
		assertNoNilLists(t, "rejoin with a rival", compute(r))
	})
	t.Run("a pit stop is on record", func(t *testing.T) {
		r := newRig()
		r.laps(5)
		yes := true
		r.st.SaveEvent(ctx, sess, 500, wire.Event{Kind: wire.EventPitEntry, CarID: 0, VehicleName: "me"})
		r.st.SaveEvent(ctx, sess, 540, wire.Event{Kind: wire.EventPitExit, CarID: 0, VehicleName: "me", FuelAdded: fp(40), TyresChanged: &yes})
		r.setLive(800, 70, 5, vehicle(0, "me", "GT3", 1, 0))
		assertNoNilLists(t, "after a stop", compute(r))
	})
}

func TestTheWalkFindsWhatItShould(t *testing.T) {
	type inner struct {
		L []int          `json:"l"`
		M map[string]int `json:"m"`
		P *[]int         `json:"p"`
		F []*float64     `json:"f"`
	}
	var bad []string
	nilLists(reflect.ValueOf(struct{ In inner }{inner{F: []*float64{nil}}}), "X", &bad)
	if len(bad) != 2 || bad[0] != "X.In.L" || bad[1] != "X.In.M" {
		t.Fatalf("%v", bad)
	}
	// and the marshalled form of the failures is the null the page chokes on
	b, _ := json.Marshal(struct{ L []int }{})
	if string(b) != `{"L":null}` {
		t.Fatal(string(b))
	}
}
