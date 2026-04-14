package alertdetector

import "math"

type segmentStats struct {
	segRMS          []float64
	segZCR          []float64
	maxZCR          float64 // Highest ZCR among any segment
	highPitchRatio  float64 // Fraction of segments with ZCR > highPitchZCR
	overallTonality float64 // Crossing regularity of all segments (low = tonal)
	spectralPurity  float64 // How concentrated energy is around dominant frequency (high = pure tone)
	midHighRatio    float64 // Energy in 800-6000Hz vs total (high = alarm-like pitch range)
	bandFocus       float64 // How concentrated energy is in one band (high = narrow-band signal)
}

// computeSegments splits audio into 250ms segments and extracts features
func (d *Detector) computeSegments(samples []int16) segmentStats {
	var s segmentStats
	var total, highPitchCount int
	var allTonalitySum, acSum float64

	// Physical frequency boundary for the "high-pitched" segment count
	const highPitchHz = 2000

	// ZCR threshold derived from highPitchHz: a sine at f Hz makes 2*f
	// zero crossings per second, so zcr = 2*f/sampleRate
	highPitchZCR := 2 * float64(highPitchHz) / float64(d.sampleRate)

	for i := 0; i+d.samplesPerSeg <= len(samples); i += d.samplesPerSeg {
		seg := samples[i : i+d.samplesPerSeg]
		var sq float64
		var crossings []int
		for j, v := range seg {
			sq += float64(v) * float64(v)
			if j > 0 && ((seg[j-1] >= 0 && v < 0) || (seg[j-1] < 0 && v >= 0)) {
				crossings = append(crossings, j)
			}
		}
		r := math.Sqrt(sq / float64(len(seg)))
		zcr := float64(len(crossings)) / float64(len(seg))
		s.segRMS = append(s.segRMS, r)
		s.segZCR = append(s.segZCR, zcr)
		s.maxZCR = max(s.maxZCR, zcr)
		allTonalitySum += crossingRegularity(crossings)
		acSum += d.peakAutocorrelation(seg)
		total++
		if zcr > highPitchZCR {
			highPitchCount++
		}
	}
	if total > 0 {
		s.highPitchRatio = float64(highPitchCount) / float64(total)
		s.overallTonality = allTonalitySum / float64(total)
		s.spectralPurity = acSum / float64(total)
	}
	s.midHighRatio, s.bandFocus = d.bandEnergy(samples)
	return s
}

// bandEnergy splits audio into 4 bands via single-pole IIR filters
// Returns midHighRatio (energy in ~800-6000Hz) and bandFocus (dominant band share)
func (d *Detector) bandEnergy(samples []int16) (midHighRatio, bandFocus float64) {
	a1, a2, a3 := d.iirA1, d.iirA2, d.iirA3

	var lp1, lp2, lp3 float64
	var e1, e2, e3, e4 float64

	for _, s := range samples {
		v := float64(s)
		lp1 += a1 * (v - lp1)
		lp2 += a2 * (v - lp2)
		lp3 += a3 * (v - lp3)
		b1 := lp1       // < 400Hz
		b2 := lp2 - lp1 // 400-1500Hz
		b3 := lp3 - lp2 // 1500-4000Hz
		b4 := v - lp3   // > 4000Hz
		e1 += b1 * b1
		e2 += b2 * b2
		e3 += b3 * b3
		e4 += b4 * b4
	}

	total := e1 + e2 + e3 + e4
	if total < 1 {
		return 0, 0.25
	}
	midHighRatio = (e2 + e3) / total
	bandFocus = max(e1, max(e2, max(e3, e4))) / total
	return
}

// peakAutocorrelation finds the strongest self-similarity at lags corresponding
// to 100-4000Hz. High = single periodic tone, low = noise/multiple tones
func (d *Detector) peakAutocorrelation(samples []int16) float64 {
	n := len(samples)
	if n < 100 {
		return 0
	}

	const stride = 4

	minLag := d.sampleRate / 4000
	maxLag := min(d.sampleRate/100, n/2)

	var energy int64
	for i := 0; i < n; i += stride {
		v := int64(samples[i])
		energy += v * v
	}
	if energy < 1 {
		return 0
	}

	// Two-pass: coarse scan every Nth lag, then refine ±N around the best
	const coarseStep = 4
	var bestCorr float64
	bestLag := minLag

	for pass := range 2 {
		lo, hi, step := minLag, maxLag, coarseStep
		if pass == 1 {
			lo = max(minLag, bestLag-coarseStep+1)
			hi = min(maxLag, bestLag+coarseStep)
			step = 1
		}
		for lag := lo; lag < hi; lag += step {
			var sum int64
			for j := 0; j < n-lag; j += stride {
				sum += int64(samples[j]) * int64(samples[j+lag])
			}
			if corr := float64(sum) / float64(energy); corr > bestCorr {
				bestCorr = corr
				bestLag = lag
				if bestCorr > 0.85 {
					return bestCorr
				}
			}
		}
	}
	return bestCorr
}
