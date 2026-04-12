package alertdetector

import "math"

// rms computes the root mean square (RMS) amplitude of PCM samples
// approximates signal energy / loudness over the window
// uses every 4th sample for speed (downsampling) to reduce CPU cost
func rms(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq int64
	n := 0
	for i := 0; i < len(samples); i += 4 {
		v := int64(samples[i])
		sumSq += v * v
		n++
	}
	return math.Sqrt(float64(sumSq) / float64(n))
}

// countSwings counts significant up/down swings of the signal envelope (RMS amplitude)
// swings = direction changes above a dynamic threshold (0.3 -> 30% range)
func countSwings(segRMS []float64) int {
	if len(segRMS) < 3 {
		return 0
	}
	lo, hi := segRMS[0], segRMS[0]
	for _, v := range segRMS {
		lo, hi = min(lo, v), max(hi, v)
	}
	threshold := (hi - lo) * 0.3
	if threshold < 1.0 {
		return 0
	}
	changes := 0
	rising := segRMS[1] > segRMS[0]
	lastExtreme := segRMS[0]
	for i := 2; i < len(segRMS); i++ {
		nowRising := segRMS[i] > segRMS[i-1]
		if nowRising != rising {
			if math.Abs(segRMS[i-1]-lastExtreme) > threshold {
				changes++
			}
			lastExtreme = segRMS[i-1]
			rising = nowRising
		}
	}
	return changes
}

// envelopeRegularity returns the CV of intervals between RMS direction changes
// Low values = regular rhythm (alarm), high values = irregular (speech, random sounds)
func envelopeRegularity(segRMS []float64) float64 {
	if len(segRMS) < 4 {
		return 0
	}
	var positions []int
	rising := segRMS[1] > segRMS[0]
	for i := 2; i < len(segRMS); i++ {
		nowRising := segRMS[i] > segRMS[i-1]
		if nowRising != rising {
			positions = append(positions, i)
			rising = nowRising
		}
	}
	if len(positions) < 3 {
		return 0
	}
	intervals := make([]float64, len(positions)-1)
	for i := range intervals {
		intervals[i] = float64(positions[i+1] - positions[i])
	}
	return cv(intervals)
}

// crossingRegularity returns the CV of intervals between zero-crossings
// Low values indicate a clean periodic tone, high values indicate noise
func crossingRegularity(positions []int) float64 {
	if len(positions) < 3 {
		return 1.0
	}
	intervals := make([]float64, len(positions)-1)
	for i := range intervals {
		intervals[i] = float64(positions[i+1] - positions[i])
	}
	return cv(intervals)
}

// normalizeSamples scales samples to a target RMS so pitch analysis
// is independent of volume
func normalizeSamples(samples []int16, currentRMS float64) []int16 {
	const targetRMS = 8000.0
	if currentRMS < 1.0 {
		return samples
	}
	scale := targetRMS / currentRMS
	if scale > 100 {
		scale = 100
	}
	// Fixed-point: multiply by (scale * 256), then shift right 8
	scaleFP := int32(scale * 256)
	out := make([]int16, len(samples))
	for i, s := range samples {
		v := (int32(s) * scaleFP) >> 8
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		out[i] = int16(v)
	}
	return out
}

// cv returns coefficient of variation (stddev / mean)
func cv(vals []float64) float64 {
	if len(vals) < 2 {
		return 1.0
	}
	m := mean(vals)
	if m < 0.0001 {
		return 1.0
	}
	var variance float64
	for _, v := range vals {
		d := v - m
		variance += d * d
	}
	return math.Sqrt(variance/float64(len(vals))) / m
}

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
