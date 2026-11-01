package api

import (
	"context"
	"testing"

	"github.com/3lefeint/splash/backend/internal/pitlane"
)

func TestTheLearnedPitLaneCanBeForgotten(t *testing.T) {
	r := newRig(t)
	e, x := 886.7, 319.0
	r.db.SavePitLane(context.Background(), pitlane.Lane{Track: "ring", EntryDist: &e, ExitDist: &x})
	r.db.SavePitLane(context.Background(), pitlane.Lane{Track: "other", EntryDist: &e, ExitDist: &x})

	if code, _ := r.do(t, "DELETE", "/api/track-pits", ""); code != 400 {
		t.Fatalf("without a track: %d, want 400", code)
	}
	if code, _ := r.do(t, "DELETE", "/api/track-pits?track=ring", ""); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if _, ok, _ := r.db.GetPitLane(context.Background(), "ring"); ok {
		t.Fatal("the lane of the track is still stored")
	}
	if _, ok, _ := r.db.GetPitLane(context.Background(), "other"); !ok {
		t.Fatal("the lane of another track must stay")
	}
	if code, _ := r.do(t, "DELETE", "/api/track-pits?track=ring", ""); code != 204 {
		t.Fatalf("forgetting twice is fine: %d", code)
	}
}
