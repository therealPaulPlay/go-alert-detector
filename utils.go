package alertdetector

import (
	"math"
	"slices"
)

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

// countSwings returns the rate of significant direction changes per segment,
// using mean absolute deviation for the threshold to stay length-independent
func countSwings(segRMS []float64) float64 {
	if len(segRMS) < 3 {
		return 0
	}
	// Compute median absolute deviation as a robust local scale
	m := mean(segRMS)
	var sumAbsDev float64
	for _, v := range segRMS {
		sumAbsDev += math.Abs(v - m)
	}
	threshold := sumAbsDev / float64(len(segRMS)) * 1.5
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
	return float64(changes) / float64(len(segRMS))
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

// chunkedCV computes CV across non-overlapping windows of refSegments
// and returns the median, making it stable across different clip lengths
func chunkedCV(vals []float64) float64 {
	if len(vals) <= refSegments {
		return cv(vals)
	}
	var cvs []float64
	for i := 0; i+refSegments <= len(vals); i += refSegments {
		cvs = append(cvs, cv(vals[i:i+refSegments]))
	}
	// Include the tail if it has enough segments
	tail := vals[len(cvs)*refSegments:]
	if len(tail) >= refSegments/2 {
		cvs = append(cvs, cv(tail))
	}
	if len(cvs) == 0 {
		return cv(vals)
	}
	slices.Sort(cvs)
	return cvs[len(cvs)/2]
}

// chunkedEnvReg computes envelopeRegularity across non-overlapping windows
// and returns the median
func chunkedEnvReg(vals []float64) float64 {
	if len(vals) <= refSegments {
		return envelopeRegularity(vals)
	}
	var regs []float64
	for i := 0; i+refSegments <= len(vals); i += refSegments {
		regs = append(regs, envelopeRegularity(vals[i:i+refSegments]))
	}
	tail := vals[len(regs)*refSegments:]
	if len(tail) >= refSegments/2 {
		regs = append(regs, envelopeRegularity(tail))
	}
	if len(regs) == 0 {
		return envelopeRegularity(vals)
	}
	slices.Sort(regs)
	return regs[len(regs)/2]
}

// envelopeAutocorrPeak returns the strongest autocorrelation of segRMS at
// lags in [minLag..maxLag], measuring whether the envelope shape repeats at
// any stable slow period (e.g. beep-beep-beep)
func envelopeAutocorrPeak(segRMS []float64, minLag, maxLag int) float64 {
	if maxLag >= len(segRMS) {
		maxLag = len(segRMS) - 1
	}
	if minLag >= maxLag {
		return 0
	}
	var m float64
	for _, v := range segRMS {
		m += v
	}
	m /= float64(len(segRMS))
	var den float64
	for _, v := range segRMS {
		d := v - m
		den += d * d
	}
	if den < 1e-12 {
		return 0
	}
	var best float64
	for lag := minLag; lag <= maxLag; lag++ {
		var num float64
		for i := 0; i+lag < len(segRMS); i++ {
			num += (segRMS[i] - m) * (segRMS[i+lag] - m)
		}
		if ac := num / den; ac > best {
			best = ac
		}
	}
	return best
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
