package util

// AbsFloat64 returns the absolute value of x.
func AbsFloat64(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
