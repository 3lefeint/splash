package strategy

import "testing"

const h = 3600.0

func TestRescheduleScalesRemainingStints(t *testing.T) {
	in := PlanInput{
		Planned: []PlannedStint{
			{"a", 1 * h}, {"b", 1 * h}, {"c", 1 * h}, {"a", 1 * h},
		},
		Done:    []ActualStint{{"a", 0, 1.1 * h}},
		Current: &CurrentStint{Start: 1.1*h + 60, ExpectedEnd: 2 * h},
		RaceEnd: 4 * h,
		PitLoss: 60,
	}
	res, err := Reschedule(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 4 {
		t.Fatalf("entries=%d", len(res.Entries))
	}
	// Two stints remain after the current one, one pit stop between them:
	// available = 4h - (2h+60) - 60.
	want := (4*h - (2*h + 60) - 60) / 2
	e := res.Entries[2]
	if !near(e.End-e.Start, want) || !near(e.Start, 2*h+60) {
		t.Fatalf("stint 3: %+v want duration %v", e, want)
	}
	last := res.Entries[3]
	if !near(last.End, 4*h) || !near(last.Start, e.End+60) {
		t.Fatalf("last stint must end at race end: %+v", last)
	}
	if !res.Entries[0].Done || !res.Entries[1].Current {
		t.Fatalf("flags wrong: %+v", res.Entries[:2])
	}
	if !near(res.CurrentDeviation, 2*h-(1.1*h+60+1*h)) {
		t.Fatalf("deviation=%v", res.CurrentDeviation)
	}
}

func TestRescheduleKeepsProportions(t *testing.T) {
	in := PlanInput{
		Planned: []PlannedStint{{"a", h}, {"b", 1 * h}, {"c", 2 * h}},
		Current: &CurrentStint{Start: 0, ExpectedEnd: h},
		RaceEnd: h + 2*60 + 3*h, // 3h of driving left for the two remaining stints
		PitLoss: 60,
	}
	res, _ := Reschedule(in)
	b, c := res.Entries[1], res.Entries[2]
	if !near(b.End-b.Start, h) || !near(c.End-c.Start, 2*h) {
		t.Fatalf("b=%v c=%v", b.End-b.Start, c.End-c.Start)
	}
}

func TestRescheduleDriverSwapExplicitPerStint(t *testing.T) {
	in := PlanInput{
		Planned: []PlannedStint{{"a", h}, {"b", h}, {"b", h}, {"b", h}, {"c", h}},
		Current: &CurrentStint{Start: 0, ExpectedEnd: h},
		RaceEnd: 5 * h,
		PitLoss: 0,
	}
	res, _ := Reschedule(in)
	var got []bool
	for _, e := range res.Entries {
		got = append(got, e.DriverSwap)
	}
	want := []bool{false, true, false, false, true} // triple stint of b: no swap at the two inner stops
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("swaps %v want %v", got, want)
		}
	}
}

func TestRescheduleCapsAtMaxStint(t *testing.T) {
	in := PlanInput{
		Planned:  []PlannedStint{{"a", h}, {"b", h}, {"c", h}},
		Current:  &CurrentStint{Start: 0, ExpectedEnd: h},
		RaceEnd:  h + 3*h, // 3h left for two stints without pit loss
		MaxStint: 1.2 * h,
	}
	res, _ := Reschedule(in)
	for _, e := range res.Entries[1:] {
		if e.End-e.Start > 1.2*h+1e-6 {
			t.Fatalf("stint over cap: %+v", e)
		}
	}
	if !near(res.Shortfall, 3*h-2*1.2*h) {
		t.Fatalf("shortfall=%v", res.Shortfall)
	}
}

func TestRescheduleRedistributesAfterCap(t *testing.T) {
	in := PlanInput{
		Planned:  []PlannedStint{{"a", h}, {"b", h}, {"c", 3 * h}, {"d", h}},
		Current:  &CurrentStint{Start: 0, ExpectedEnd: h},
		RaceEnd:  h + 4*h,
		MaxStint: 2 * h,
	}
	res, _ := Reschedule(in)
	if res.Shortfall != 0 {
		t.Fatalf("shortfall=%v", res.Shortfall)
	}
	var total float64
	for _, e := range res.Entries[1:] {
		if e.Reserve {
			continue
		}
		d := e.End - e.Start
		total += d
		if d > 2*h+1e-6 {
			t.Fatalf("over cap: %v", d)
		}
	}
	if !near(total, 4*h) {
		t.Fatalf("total=%v", total)
	}
}

func TestRescheduleErrors(t *testing.T) {
	in := PlanInput{Planned: []PlannedStint{{"a", h}}}
	if _, err := Reschedule(in); err != ErrNoCurrentStint {
		t.Fatalf("err=%v", err)
	}
	in.Done = []ActualStint{{"a", 0, h}}
	if res, err := Reschedule(in); err != nil || len(res.Entries) != 1 {
		t.Fatalf("finished plan: %v %v", res, err)
	}
}

func TestRescheduleCutsStintsBehindTheFinishAndKeepsTwoAsReserve(t *testing.T) {
	var planned []PlannedStint
	for i := 0; i < 16; i++ {
		planned = append(planned, PlannedStint{"d", 65 * 60})
	}
	// the race has 3 h left after the current stint, the plan covers 17 h
	in := PlanInput{Planned: planned, Current: &CurrentStint{Start: 0, ExpectedEnd: 3600}, RaceEnd: 3600 + 3*3600}
	res, err := Reschedule(in)
	if err != nil {
		t.Fatal(err)
	}
	var needed, reserve int
	var last PlanEntry
	for _, e := range res.Entries[1:] {
		if e.Reserve {
			reserve++
			if !near(e.End-e.Start, 65*60) || e.Start < in.RaceEnd-1e-6 {
				t.Fatalf("reserve %+v", e)
			}
			continue
		}
		needed++
		last = e
		if d := e.End - e.Start; d > 65*60+1e-6 || d < 55*60 {
			t.Fatalf("a needed stint of %v s, planned 3900", d)
		}
	}
	if needed != 3 || reserve != ReserveStints || !near(last.End, in.RaceEnd) {
		t.Fatalf("needed %d reserve %d end %v", needed, reserve, last.End)
	}
	if res.Entries[1].No != 2 || res.Entries[len(res.Entries)-1].No != 2+needed+reserve-1 {
		t.Fatalf("numbering %+v", res.Entries)
	}
}

func TestRescheduleNeedsMoreStintsWhenTheCapLeavesTimeOver(t *testing.T) {
	planned := []PlannedStint{{"a", h}, {"b", 2 * h}, {"c", 2 * h}, {"d", 2 * h}, {"e", 2 * h}}
	in := PlanInput{Planned: planned, Current: &CurrentStint{Start: 0, ExpectedEnd: h}, RaceEnd: h + 5*h, MaxStint: 2 * h}
	res, _ := Reschedule(in)
	var needed int
	for _, e := range res.Entries[1:] {
		if !e.Reserve {
			needed++
		}
	}
	if needed != 3 || res.Shortfall != 0 { // 5 h at most 2 h each: three stints
		t.Fatalf("needed %d shortfall %v", needed, res.Shortfall)
	}
}
