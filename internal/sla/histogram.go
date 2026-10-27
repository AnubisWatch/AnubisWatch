package sla

import "math"

// Latency percentiles come from a fixed-size log-linear histogram: adding a
// sample is O(1) and merging two tallies is O(histBuckets), whatever the
// number or spread of latencies, so a long workspace report costs memory
// and time proportional to its souls, never to its checks.
//
// Values below histMinMs share bucket 0 (reported as 0). Above it, bucket
// i covers [histMinMs·γ^(i-1), histMinMs·γ^i) with γ = 1.02 and reports the
// geometric midpoint, so a percentile is within ±1% of the true value.
// Values above the last bucket (about 21 minutes) are clamped into it.
const (
	histMinMs   = 0.01
	histGamma   = 1.02
	histBuckets = 1000
)

var histLogGamma = math.Log(histGamma)

// RelativeError is the largest relative error of a reported percentile.
const RelativeError = histGamma - 1

type histogram struct {
	counts []int // nil until the first sample; then histBuckets long
	total  int
}

func histIndex(valueMs float64) int {
	if !(valueMs >= histMinMs) { // also NaN and negatives
		return 0
	}
	i := int(math.Floor(math.Log(valueMs/histMinMs)/histLogGamma)) + 1
	if i >= histBuckets {
		i = histBuckets - 1
	}
	return i
}

// histValue is the representative value of bucket i.
func histValue(i int) float64 {
	if i == 0 {
		return 0
	}
	return histMinMs * math.Pow(histGamma, float64(i)-0.5)
}

func (h *histogram) add(valueMs float64, count int) {
	if count <= 0 {
		return
	}
	if h.counts == nil {
		h.counts = make([]int, histBuckets)
	}
	h.counts[histIndex(valueMs)] += count
	h.total += count
}

func (h *histogram) merge(o *histogram) {
	if o == nil || o.total == 0 {
		return
	}
	if h.counts == nil {
		h.counts = make([]int, histBuckets)
	}
	for i, c := range o.counts {
		h.counts[i] += c
	}
	h.total += o.total
}

// quantiles returns the nearest-rank p-th percentiles, ok false when empty.
func (h *histogram) quantiles(ps ...float64) ([]float64, bool) {
	if h.total == 0 {
		return nil, false
	}
	out := make([]float64, len(ps))
	for k, p := range ps {
		rank := int(math.Ceil(p * float64(h.total)))
		if rank < 1 {
			rank = 1
		}
		cumulative := 0
		for i, c := range h.counts {
			cumulative += c
			if cumulative >= rank {
				out[k] = histValue(i)
				break
			}
		}
	}
	return out, true
}
