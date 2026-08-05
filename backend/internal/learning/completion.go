package learning

const completionThreshold = 0.9

// Completes derives completion solely from the bounded playback position and
// S4's trusted duration for the exact Asset Version.
func Completes(position, duration float64) bool {
	return duration > 0 && position/duration >= completionThreshold
}
