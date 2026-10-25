package strategy

import "testing"

func TestGapTrend(t *testing.T) {
	cases := []struct {
		name string
		gaps []float64
		n    int
		want float64
		ok   bool
	}{
		{"closing", []float64{10, 9, 8, 7}, 5, -1, true},
		{"growing", []float64{5, 5.5, 6, 6.5}, 5, 0.5, true},
		{"only last n", []float64{100, 50, 10, 9, 8}, 3, -1, true},
		{"constant", []float64{4, 4, 4}, 5, 0, true},
		{"one value", []float64{4}, 5, 0, false},
		{"none", nil, 5, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := GapTrend(c.gaps, c.n)
			if ok != c.ok || !near(got, c.want) {
				t.Fatalf("got %v ok=%v want %v ok=%v", got, ok, c.want, c.ok)
			}
		})
	}
}
