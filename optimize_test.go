package alertdetector

import (
	"fmt"
	"math"
	"testing"
)

// Sample creation ------------------------------------------------------------------------------------

var ambienceFiles = []string{
	"cafe_ambience", "rain", "suburban_garden_ambience_baseline",
	"airplane_austria_ambience", "distant_music_band", "toddlers_playing_laughing",
}

// analyzeMetrics computes all metrics for the samples (matches Analyze's impl)
func analyzeMetrics(s []int16) Metrics {
	d := New(testSampleRate)
	seg := d.computeSegments(normalizeSamples(s, rms(s)))
	return Metrics{
		MaxZCR:          seg.maxZCR,
		HighPitchRatio:  seg.highPitchRatio,
		OverallTonality: seg.overallTonality,
		SpectralPurity:  seg.spectralPurity,
		MidHighRatio:    seg.midHighRatio,
		BandFocus:       seg.bandFocus,
		OscCV:           chunkedCV(seg.segRMS),
		EnvRegularity:   chunkedEnvReg(seg.segRMS),
		Oscillations:    max(countSwings(seg.segRMS), countSwings(seg.segZCR)),
	}
}

// mixAmbience overlays ambience onto foreground at passed RMS ratio (0.25 = ambience 25% of foreground RMS)
// ambience is looped if shorter than the foregroun
func mixAmbience(foreground, ambience []int16, ratio float64) []int16 {
	fgRMS := rms(foreground)
	ambRMS := rms(ambience)
	if fgRMS < 1 || ambRMS < 1 {
		return foreground
	}
	scale := fgRMS * ratio / ambRMS

	out := make([]int16, len(foreground))
	for i := range foreground {
		a := float64(ambience[i%len(ambience)]) * scale
		v := float64(foreground[i]) + a
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		out[i] = int16(v)
	}
	return out
}

// trimToDuration returns the first `seconds` of samples, or the full slice if already shorter
func trimToDuration(samples []int16, seconds int) []int16 {
	limit := seconds * testSampleRate
	if len(samples) <= limit {
		return samples
	}
	return samples[:limit]
}

// addSample stores metric result in positive or negative bucket depending on `detect`
func addSample(tc audioTestCase, m Metrics, positives map[string][]Metrics, negatives *[]Metrics) {
	if tc.detect {
		positives[tc.file] = append(positives[tc.file], m)
	} else {
		*negatives = append(*negatives, m)
	}
}

// computeAudioSamples generates variants of each test file across four
// independent axes (speed, volume, duration, ambience) without combining them
// Returns samples split into positives (alarms) and negatives
func computeAudioSamples(t *testing.T) (positivesByFile map[string][]Metrics, negatives []Metrics) {
	t.Helper()
	speeds := []float64{0.9, 0.925, 0.95, 0.975, 1.025, 1.05, 1.075, 1.1}
	volumes := []float64{0.5, 1.5}
	durations := []int{8, 12, 16, 20}

	// Pre-load ambiences once
	ambiences := make([][]int16, len(ambienceFiles))
	for i, f := range ambienceFiles {
		ambiences[i] = loadWAV(t, "testdata/"+f+".wav")
	}

	positivesByFile = make(map[string][]Metrics)
	for _, tc := range audioTests {
		raw := loadWAV(t, "testdata/"+tc.file+".wav")

		// Base sample (unmodified)
		addSample(tc, analyzeMetrics(raw), positivesByFile, &negatives)

		// Speed variants
		for _, speed := range speeds {
			addSample(tc, analyzeMetrics(scaleSpeed(raw, speed)), positivesByFile, &negatives)
		}

		// Volume variants
		for _, vol := range volumes {
			addSample(tc, analyzeMetrics(scaleVolume(raw, vol)), positivesByFile, &negatives)
		}

		// Duration variants
		for _, dur := range durations {
			addSample(tc, analyzeMetrics(trimToDuration(raw, dur)), positivesByFile, &negatives)
		}

		// Ambience overlay variants at 0.25 (audible but foreground still dominant)
		for _, amb := range ambiences {
			addSample(tc, analyzeMetrics(mixAmbience(raw, amb, 0.25)), positivesByFile, &negatives)
		}
	}
	return
}

// Bound calculation -----------------------------------------------------------------------------------

// metricAccessor pairs a metric name with a function to extract it from Metrics
type metricAccessor struct {
	name string
	get  func(Metrics) float64
}

var allMetrics = []metricAccessor{
	{"MaxZCR", func(m Metrics) float64 { return m.MaxZCR }},
	{"HighPitchRatio", func(m Metrics) float64 { return m.HighPitchRatio }},
	{"OverallTonality", func(m Metrics) float64 { return m.OverallTonality }},
	{"SpectralPurity", func(m Metrics) float64 { return m.SpectralPurity }},
	{"MidHighRatio", func(m Metrics) float64 { return m.MidHighRatio }},
	{"BandFocus", func(m Metrics) float64 { return m.BandFocus }},
	{"OscCV", func(m Metrics) float64 { return m.OscCV }},
	{"EnvRegularity", func(m Metrics) float64 { return m.EnvRegularity }},
	{"Oscillations", func(m Metrics) float64 { return m.Oscillations }},
}

// bound is one side of a threshold on one metric — either "value must be >= threshold"
// (isMin=true) or "value must be < threshold" (isMin=false)
type bound struct {
	metricIdx int
	isMin     bool
	threshold float64
}

func (b bound) passes(m Metrics) bool {
	v := allMetrics[b.metricIdx].get(m)
	if b.isMin {
		return v >= b.threshold
	}
	return v < b.threshold
}

// candidateBounds returns min/max threshold candidates placed at the midpoint
// between the positive range edge and the nearest negative outside that range
func candidateBounds(positives, negatives []Metrics) []bound {
	var candidates []bound
	for metricIdx, ma := range allMetrics {
		posLow, posHigh := extent(positives, ma.get)
		if math.IsInf(posLow, 1) {
			continue
		}
		if t, ok := midpointSplit(negatives, ma.get, posLow, true); ok {
			candidates = append(candidates, bound{metricIdx, true, t})
		}
		if t, ok := midpointSplit(negatives, ma.get, posHigh, false); ok {
			candidates = append(candidates, bound{metricIdx, false, t})
		}
	}
	return candidates
}

// extent returns the min and max of the given getter across samples
func extent(samples []Metrics, get func(Metrics) float64) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, s := range samples {
		v := get(s)
		low, high = min(low, v), max(high, v)
	}
	return
}

// midpointSplit finds the nearest negative on the outside of edge and returns
// a threshold halfway between edge and that negative - below=true is for min bounds, below=false for max bounds
func midpointSplit(negatives []Metrics, get func(Metrics) float64, edge float64, below bool) (float64, bool) {
	nearest := math.Inf(1)
	if below {
		nearest = math.Inf(-1)
	}
	found := false
	for _, n := range negatives {
		v := get(n)
		if below && v < edge && v > nearest {
			nearest, found = v, true
		} else if !below && v > edge && v < nearest {
			nearest, found = v, true
		}
	}
	if !found {
		return 0, false
	}
	t := math.Round(((edge+nearest)/2)*1000) / 1000 // Rounded to 3 digits
	if t <= 0.001 {
		return 0, false
	}
	return t, true
}

// countRejected counts how many negatives fail the given bound
func countRejected(negatives []Metrics, b bound) int {
	n := 0
	for _, neg := range negatives {
		if !b.passes(neg) {
			n++
		}
	}
	return n
}

// allPass reports whether every positive passes the given bound
func allPass(positives []Metrics, b bound) bool {
	for _, p := range positives {
		if !b.passes(p) {
			return false
		}
	}
	return true
}

// findBounds greedily picks bounds that reject the most remaining negatives
// while keeping every positive. Returns the chosen bounds plus how many
// negatives could not be rejected by any available bound
func findBounds(positives, negatives []Metrics) (chosen []bound, unrejected int) {
	candidates := candidateBounds(positives, negatives)
	remaining := append([]Metrics(nil), negatives...)

	for len(remaining) > 0 {
		// Find the candidate that eliminates the most remaining negatives
		bestIdx, bestCount := -1, 0
		for i, c := range candidates {
			if !allPass(positives, c) {
				continue
			}
			if n := countRejected(remaining, c); n > bestCount {
				bestIdx, bestCount = i, n
			}
		}
		if bestIdx < 0 {
			break // No candidate helps further
		}

		chosen = append(chosen, candidates[bestIdx])
		remaining = filterPassing(remaining, candidates[bestIdx])
		candidates = append(candidates[:bestIdx], candidates[bestIdx+1:]...)
	}
	return chosen, len(remaining)
}

// filterPassing returns samples that pass the given bound (the ones that
// weren't rejected)
func filterPassing(samples []Metrics, b bound) []Metrics {
	var kept []Metrics
	for _, s := range samples {
		if b.passes(s) {
			kept = append(kept, s)
		}
	}
	return kept
}

// Build rules based on bounds ----------------------------------------------------------------------------------

// ruleGroup is a set of files that share one rule
type ruleGroup struct {
	files      []string
	bounds     []bound
	unrejected int // if > 0 -> the rule fails to reject certain files
}

// buildSharedRule builds a rule covering all positive variants of the given
// files, returning the bounds and the count of negatives still unrejected
func buildSharedRule(files []string, positives map[string][]Metrics, negatives []Metrics) ([]bound, int) {
	var combined []Metrics
	for _, f := range files {
		combined = append(combined, positives[f]...)
	}
	return findBounds(combined, negatives)
}

// groupFiles greedily merges positive files into shared rule groups, adding
// candidates only if they preserve zero-FP separation
func groupFiles(positives map[string][]Metrics, negatives []Metrics) []ruleGroup {
	grouped := make(map[string]bool)
	var groups []ruleGroup

	for startFile := range positives {
		if grouped[startFile] {
			continue
		}

		members := []string{startFile}
		bounds, unrejected := buildSharedRule(members, positives, negatives)
		for candidateFile := range positives {
			if grouped[candidateFile] || candidateFile == startFile {
				continue
			}
			if b, u := buildSharedRule(append(members, candidateFile), positives, negatives); u == 0 {
				members = append(members, candidateFile)
				bounds = b
				unrejected = u
			}
		}

		for _, f := range members {
			grouped[f] = true
		}
		groups = append(groups, ruleGroup{members, bounds, unrejected})
	}
	return groups
}

// Main entry point ------------------------------------------------------------------------------

// TestOptimizeRules derives optimal detection rules from the test samples and prints them
func TestOptimizeRules(t *testing.T) {
	positives, negatives := computeAudioSamples(t)

	// Count total positive samples for the log line (positives is
	// map[file][]Metrics — one []Metrics per file with all its variants)
	var posSamples int
	for _, variants := range positives {
		posSamples += len(variants)
	}
	// Count unique negative files to mirror the positive-side report
	negFiles := 0
	for _, tc := range audioTests {
		if !tc.detect {
			negFiles++
		}
	}
	fmt.Printf("\n%d positive files (%d samples), %d negative files (%d samples)\n\n",
		len(positives), posSamples, negFiles, len(negatives))

	groups := groupFiles(positives, negatives)
	var totalLeaks int
	for i, g := range groups {
		status := ""
		if g.unrejected > 0 {
			status = fmt.Sprintf("  [LEAKS %d negatives]", g.unrejected)
			totalLeaks += g.unrejected
		}
		fmt.Printf("Rule group %d (%d files)%s\n", i+1, len(g.files), status)
		fmt.Printf("  Files: %v\n", g.files)
		fmt.Printf("  Bounds:\n")
		for _, b := range g.bounds {
			dir := "min"
			if !b.isMin {
				dir = "max"
			}
			fmt.Printf("    %s %s: %.4f\n", dir, allMetrics[b.metricIdx].name, b.threshold)
		}
		fmt.Println()
	}
	if totalLeaks > 0 {
		fmt.Printf("Total negative leaks across all rules: %d\n", totalLeaks)
	}
}
