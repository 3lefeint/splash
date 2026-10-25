package strategy

// GapTrend returns the trend of a gap series in seconds per lap as the least
// squares slope over the last n values (oldest first). The gap is the positive
// time distance to an opponent, so a negative trend means the gap is closing.
// ok is false with fewer than two values.
func GapTrend(gaps []float64, n int) (slope float64, ok bool) {
	if n < 2 {
		n = 2
	}
	if len(gaps) > n {
		gaps = gaps[len(gaps)-n:]
	}
	m := float64(len(gaps))
	if m < 2 {
		return 0, false
	}
	var sx, sy, sxy, sxx float64
	for i, g := range gaps {
		x := float64(i)
		sx += x
		sy += g
		sxy += x * g
		sxx += x * x
	}
	return (m*sxy - sx*sy) / (m*sxx - sx*sx), true
}
