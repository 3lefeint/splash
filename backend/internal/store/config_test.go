package store

import "testing"

func TestFitSettingsDefaultsFillUnsetValues(t *testing.T) {
	p := Params{Track: "t", Car: "c"}.WithDefaults()
	if p.FitMinLaps != 5 || p.FitSlowPercent != 8 || p.FitSkipFCY == nil || !*p.FitSkipFCY || p.FitSkipWet == nil || !*p.FitSkipWet {
		t.Fatalf("%+v", p)
	}
	off := false
	q := Params{FitMinLaps: 9, FitSlowPercent: 12, FitSkipFCY: &off}.WithDefaults()
	if q.FitMinLaps != 9 || q.FitSlowPercent != 12 || *q.FitSkipFCY || !*q.FitSkipWet {
		t.Fatalf("explicit values must stay: %+v", q)
	}
}

func TestFitSettingsRanges(t *testing.T) {
	ok := []Params{{}, {FitMinLaps: 3}, {FitMinLaps: 20}, {FitSlowPercent: 2}, {FitSlowPercent: 30}, {FitMinLaps: 5, FitSlowPercent: 8}}
	for _, p := range ok {
		if err := p.ValidateFit(); err != nil {
			t.Errorf("%+v: %v", p, err)
		}
	}
	bad := []Params{{FitMinLaps: 1}, {FitMinLaps: 2}, {FitMinLaps: 21}, {FitMinLaps: -4}, {FitSlowPercent: 1.9}, {FitSlowPercent: 30.1}, {FitSlowPercent: -8}}
	for _, p := range bad {
		if err := p.ValidateFit(); err == nil {
			t.Errorf("%+v should be rejected", p)
		}
	}
}

func TestTheTyreChangeDefaultFillsAnUnsetValueOnly(t *testing.T) {
	if DefaultTyreChange != 17 {
		t.Fatalf("%v", DefaultTyreChange)
	}
	if got := (Params{}).WithDefaults().TyreChange; got != 17 {
		t.Fatalf("unset: %v", got)
	}
	if got := (Params{TyreChange: 12.5}).WithDefaults().TyreChange; got != 12.5 {
		t.Fatalf("explicit: %v", got)
	}
	if (Params{TyreChange: 12.5}).TyreChangeTime() != 12.5 || (Params{}).TyreChangeTime() != 17 {
		t.Fatal("TyreChangeTime")
	}
}
