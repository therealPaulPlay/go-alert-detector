package alertdetector

import (
	"fmt"
	"math"
	"testing"
)

// analyzeMetrics computes Metrics for a sample buffer, matching Analyze's logic
func analyzeMetrics(s []int16) Metrics {
	d := New(testSampleRate)
	seg := d.computeSegments(normalizeSamples(s, rms(s)))
	return Metrics{
		MaxZCR:          seg.maxZCR,
		HighPitchRatio:  seg.highPitchRatio,
		PitchRange:      seg.pitchRange,
		Tonality:        seg.tonality,
		OverallTonality: seg.overallTonality,
		SpectralPurity:  seg.spectralPurity,
		MidHighRatio:    seg.midHighRatio,
		BandFocus:       seg.bandFocus,
		OscCV:           chunkedCV(seg.segRMS),
		ZcrCV:           chunkedCV(seg.segZCR),
		EnvRegularity:   chunkedEnvReg(seg.segRMS),
		Oscillations:    max(countSwings(seg.segRMS), countSwings(seg.segZCR)),
		RMSOscillations: countSwings(seg.segRMS),
	}
}

// computeAudioSamples generates every variant (speed × volume × duration) of every
// test file and returns them split into positive (alarm) and negative samples
func computeAudioSamples(t *testing.T) (positivesByFile map[string][]Metrics, negatives []Metrics) {
	t.Helper()
	speeds := []float64{0.9, 0.925, 0.95, 0.975, 1.0, 1.025, 1.05, 1.075, 1.1}
	volumes := []float64{0.5, 1.0, 1.5}
	durations := []int{0, 8, 12, 16, 20} // 0 = full clip

	positivesByFile = make(map[string][]Metrics)
	for _, tc := range audioTests {
		raw := loadWAV(t, "testdata/"+tc.file+".wav")
		for _, speed := range speeds {
			for _, vol := range volumes {
				s := raw
				if speed != 1.0 {
					s = scaleSpeed(s, speed)
				}
				if vol != 1.0 {
					s = scaleVolume(s, vol)
				}
				for _, dur := range durations {
					// If duration is set (non 0), clamp by duration
					clip := s
					if limit := dur * testSampleRate; dur > 0 && len(clip) > limit {
						clip = clip[:limit]
					}

					// Sort by positives and negatives
					m := analyzeMetrics(clip)
					if tc.detect {
						positivesByFile[tc.file] = append(positivesByFile[tc.file], m)
					} else {
						negatives = append(negatives, m)
					}
				}
			}
		}
	}
	return
}

// metricAccessor pairs a metric name with a function to extract it from Metrics
type metricAccessor struct {
	name string
	get  func(Metrics) float64
}

var allMetrics = []metricAccessor{
	{"MaxZCR", func(m Metrics) float64 { return m.MaxZCR }},
	{"HighPitchRatio", func(m Metrics) float64 { return m.HighPitchRatio }},
	{"PitchRange", func(m Metrics) float64 { return m.PitchRange }},
	{"Tonality", func(m Metrics) float64 { return m.Tonality }},
	{"OverallTonality", func(m Metrics) float64 { return m.OverallTonality }},
	{"SpectralPurity", func(m Metrics) float64 { return m.SpectralPurity }},
	{"MidHighRatio", func(m Metrics) float64 { return m.MidHighRatio }},
	{"BandFocus", func(m Metrics) float64 { return m.BandFocus }},
	{"OscCV", func(m Metrics) float64 { return m.OscCV }},
	{"ZcrCV", func(m Metrics) float64 { return m.ZcrCV }},
	{"EnvRegularity", func(m Metrics) float64 { return m.EnvRegularity }},
	{"Oscillations", func(m Metrics) float64 { return m.Oscillations }},
	{"RMSOscillations", func(m Metrics) float64 { return m.RMSOscillations }},
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

// candidateBounds returns every min/max threshold that passes all positives
// and rejects at least one negative, across every metric
func candidateBounds(positives, negatives []Metrics) []bound {
	var candidates []bound
	for metricIdx, ma := range allMetrics {
		// Find the range of this metric across all positives
		low, high := math.Inf(1), math.Inf(-1)
		for _, p := range positives {
			v := ma.get(p)
			low, high = min(low, v), max(high, v)
		}
		if math.IsInf(low, 1) {
			continue
		}

		// A min bound at 95% of the lowest positive keeps all positives passing
		// and rejects negatives below that threshold
		minThresh := round3(low * 0.95)
		if minThresh > 0.001 && countRejected(negatives, bound{metricIdx, true, minThresh}) > 0 {
			candidates = append(candidates, bound{metricIdx, true, minThresh})
		}

		// A max bound at 105% of the highest positive does the mirror
		maxThresh := round3(high * 1.05)
		if maxThresh > 0.001 && countRejected(negatives, bound{metricIdx, false, maxThresh}) > 0 {
			candidates = append(candidates, bound{metricIdx, false, maxThresh})
		}
	}
	return candidates
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

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// ruleGroup is a set of files that can all be captured by one shared rule
type ruleGroup struct {
	files  []string
	bounds []bound
}

// buildSharedRule attempts to construct a single rule (a set of bounds) that
// covers all positive variants of every listed file while rejecting every
// negative. Returns the bounds and ok=true if such a rule exists
func buildSharedRule(files []string, positives map[string][]Metrics, negatives []Metrics) ([]bound, bool) {
	var combined []Metrics
	for _, f := range files {
		combined = append(combined, positives[f]...)
	}
	bounds, missed := findBounds(combined, negatives)
	return bounds, missed == 0
}

// groupFiles merges positive files into shared rule groups. It picks one
// unassigned file to start a new group, then greedily adds any other
// unassigned file that can still share one zero-false-positives rule with the group
func groupFiles(positives map[string][]Metrics, negatives []Metrics) []ruleGroup {
	grouped := make(map[string]bool)
	var groups []ruleGroup

	for startFile := range positives {
		if grouped[startFile] {
			continue
		}

		members := []string{startFile}
		bounds, _ := buildSharedRule(members, positives, negatives)
		for candidateFile := range positives {
			// If that's the file we started with, skip
			if grouped[candidateFile] || candidateFile == startFile {
				continue
			}
			if b, ok := buildSharedRule(append(members, candidateFile), positives, negatives); ok {
				members = append(members, candidateFile)
				bounds = b
			}
		}

		for _, f := range members {
			grouped[f] = true
		}
		groups = append(groups, ruleGroup{members, bounds})
	}
	return groups
}

// TestOptimizeRules derives optimal detection rules from the test samples
// and prints them grouped by which files share each rule
func TestOptimizeRules(t *testing.T) {
	positives, negatives := computeAudioSamples(t)
	fmt.Printf("\n%d positive files, %d negative samples\n\n", len(positives), len(negatives))

	groups := groupFiles(positives, negatives)
	for i, g := range groups {
		fmt.Printf("Rule group %d (%d files)\n", i+1, len(g.files))
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
}
