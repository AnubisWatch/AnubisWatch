package core

import "time"

// TimeRange is the half-open interval [Start, End).
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// CheckTally counts a soul's checks within a TimeRange: how many ran, how
// many found the soul alive, and their summed latency in milliseconds.
type CheckTally struct {
	Checks       int
	Up           int
	LatencySumMs float64
}
