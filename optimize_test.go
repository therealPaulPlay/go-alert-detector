package alertdetector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"testing"
)

// Sample creation ------------------------------------------------------------------------------------

var ambienceFiles = []string{
	"cafe_ambience", "rain", "suburban_garden_ambience_baseline",
	"airplane_austria_ambience", "distant_music_band", "toddlers_playing_laughing",
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

// computeAudioSamples generates variants of each test file across four
// independent axes (speed, volume, duration, ambience) without combining them
// Returns samples split into positives (alarms) and negatives, both keyed by file
func computeAudioSamples(t *testing.T) (positives, negatives map[string][]Metrics) {
	t.Helper()
	speeds := []float64{0.9, 0.925, 0.95, 0.975, 1.025, 1.05, 1.075, 1.1}
	volumes := []float64{0.5, 1.5}
	durations := []int{8, 12, 16, 20}

	// Pre-load ambiences once
	ambiences := make([][]int16, len(ambienceFiles))
	for i, f := range ambienceFiles {
		ambiences[i] = loadWAV(t, "testdata/"+f+".wav")
	}

	d := New(testSampleRate)
	positives = make(map[string][]Metrics)
	negatives = make(map[string][]Metrics)
	for _, tc := range audioTests {
		target := positives
		if !tc.detect {
			target = negatives
		}
		raw := loadWAV(t, "testdata/"+tc.file+".wav")

		add := func(s []int16) { target[tc.file] = append(target[tc.file], d.computeMetrics(s)) }
		add(raw)
		for _, speed := range speeds {
			add(scaleSpeed(raw, speed))
		}
		for _, vol := range volumes {
			add(scaleVolume(raw, vol))
		}
		for _, dur := range durations {
			add(trimToDuration(raw, dur))
		}
		// Ambience overlays at 0.25 (audible but foreground still dominant)
		for _, amb := range ambiences {
			add(mixAmbience(raw, amb, 0.25))
		}
	}
	return
}

// Bound calculation -----------------------------------------------------------------------------------

// metricFields are the names of every float64 field in Metrics, discovered
// via reflection so the optimizer automatically tracks new metrics
var metricFields = func() []string {
	t := reflect.TypeFor[Metrics]()
	names := make([]string, t.NumField())
	for i := range names {
		names[i] = t.Field(i).Name
	}
	return names
}()

// metricValue reads the i-th metric field from m via reflection
func metricValue(m Metrics, i int) float64 {
	return reflect.ValueOf(m).Field(i).Float()
}

// bound is one side of a threshold on one metric — either "value must be >= threshold"
// (isMin=true) or "value must be < threshold" (isMin=false)
type bound struct {
	metricIdx int
	isMin     bool
	threshold float64
}

func (b bound) passes(m Metrics) bool {
	v := metricValue(m, b.metricIdx)
	if b.isMin {
		return v >= b.threshold
	}
	return v < b.threshold
}

// candidateBounds returns min/max threshold candidates placed at the midpoint
// between the positive range edge and the nearest negative outside that range
func candidateBounds(positives, negatives []Metrics) []bound {
	if len(positives) == 0 {
		return nil
	}
	var candidates []bound
	for idx := range metricFields {
		get := func(m Metrics) float64 { return metricValue(m, idx) }
		low, high := math.Inf(1), math.Inf(-1)
		for _, p := range positives {
			v := get(p)
			low, high = min(low, v), max(high, v)
		}
		if t, ok := midpointSplit(negatives, get, low, true); ok {
			candidates = append(candidates, bound{idx, true, t})
		}
		if t, ok := midpointSplit(negatives, get, high, false); ok {
			candidates = append(candidates, bound{idx, false, t})
		}
	}
	return candidates
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

// splitBy partitions samples into those passing and failing the bound
func splitBy(samples []Metrics, b bound) (passing, failing []Metrics) {
	for _, s := range samples {
		if b.passes(s) {
			passing = append(passing, s)
		} else {
			failing = append(failing, s)
		}
	}
	return
}

// findBounds greedily picks bounds that reject the most remaining negatives
// while keeping every positive. Returns the chosen bounds plus how many
// negatives could not be rejected by any available bound
func findBounds(positives, negatives []Metrics) (chosen []bound, unrejected int) {
	// Keep only candidates that pass every positive - positives never change
	// across iterations so we can filter once up front
	var candidates []bound
	for _, c := range candidateBounds(positives, negatives) {
		if pass, _ := splitBy(positives, c); len(pass) == len(positives) {
			candidates = append(candidates, c)
		}
	}

	remaining := append([]Metrics(nil), negatives...)
	for len(remaining) > 0 {
		// Pick the candidate rejecting the most remaining negatives
		bestIdx := -1
		var bestKept []Metrics
		bestRejected := 0
		for i, c := range candidates {
			kept, _ := splitBy(remaining, c)
			rejected := len(remaining) - len(kept)
			if rejected > bestRejected {
				bestIdx, bestRejected, bestKept = i, rejected, kept
			}
		}
		if bestIdx < 0 {
			break // No candidate helps further
		}
		chosen = append(chosen, candidates[bestIdx])
		remaining = bestKept
		candidates = append(candidates[:bestIdx], candidates[bestIdx+1:]...)
	}
	return chosen, len(remaining)
}

// Build rules based on bounds ----------------------------------------------------------------------------------

// ruleGroup is a set of files that share one rule
type ruleGroup struct {
	files      []string
	bounds     []bound
	unrejected int // if > 0 -> the rule fails to reject certain files
}

// flatten collects every metric across all files in the set into a single slice
func flatten(s map[string][]Metrics) []Metrics {
	var out []Metrics
	for _, variants := range s {
		out = append(out, variants...)
	}
	return out
}

// samplesFor collects all positive metric variants for the given files
func samplesFor(files []string, positives map[string][]Metrics) []Metrics {
	var out []Metrics
	for _, f := range files {
		out = append(out, positives[f]...)
	}
	return out
}

// margin returns the total headroom of a rule: for each bound, the distance
// from the threshold to the nearest positive sample on the accepting side
// Larger = more robust against distribution shift
func margin(bounds []bound, samples []Metrics) float64 {
	var total float64
	for _, b := range bounds {
		best := math.Inf(1)
		for _, s := range samples {
			v := metricValue(s, b.metricIdx)
			var d float64
			if b.isMin && v >= b.threshold {
				d = v - b.threshold
			} else if !b.isMin && v < b.threshold {
				d = b.threshold - v
			} else {
				continue
			}
			if d < best {
				best = d
			}
		}
		if !math.IsInf(best, 1) {
			total += best
		}
	}
	return total
}

// groupFiles agglomeratively merges files, starting from singletons and
// repeatedly picking the pair with the fewest-bound zero-FP rule
func groupFiles(positives, negatives map[string][]Metrics) []ruleGroup {
	flatNeg := flatten(negatives)

	// Start with one group per file
	var groups []ruleGroup
	for file := range positives {
		b, u := findBounds(samplesFor([]string{file}, positives), flatNeg)
		groups = append(groups, ruleGroup{[]string{file}, b, u})
	}

	for {
		i, j, merged := findBestMerge(groups, positives, flatNeg)
		if i < 0 {
			break
		}
		groups[i] = merged
		groups = append(groups[:j], groups[j+1:]...)
	}
	return groups
}

// findBestMerge picks the pair whose combined zero-FP rule needs the fewest
// bounds, tie-broken by the largest positive-side margin. i=-1 if none exist
func findBestMerge(groups []ruleGroup, positives map[string][]Metrics, flatNeg []Metrics) (bestI, bestJ int, best ruleGroup) {
	bestI = -1
	var bestMargin float64
	for i := range groups {
		for j := i + 1; j < len(groups); j++ {
			files := slices.Concat(groups[i].files, groups[j].files)
			samples := samplesFor(files, positives)
			b, u := findBounds(samples, flatNeg)
			if u != 0 {
				continue
			}
			m := margin(b, samples)
			if bestI < 0 || len(b) < len(best.bounds) ||
				(len(b) == len(best.bounds) && m > bestMargin) {
				bestI, bestJ, best, bestMargin = i, j, ruleGroup{files, b, 0}, m
			}
		}
	}
	return
}

// boundsToRule converts a slice of bounds into a rule struct by setting the
// corresponding Min<Metric> / Max<Metric> field for each bound
func boundsToRule(bounds []bound) rule {
	var r rule
	v := reflect.ValueOf(&r).Elem()
	for _, b := range bounds {
		prefix := "Max"
		if b.isMin {
			prefix = "Min"
		}
		f := v.FieldByName(prefix + metricFields[b.metricIdx])
		if f.IsValid() {
			f.SetFloat(b.threshold)
		}
	}
	return r
}

// Main entry point ------------------------------------------------------------------------------

// TestOptimizeRules derives optimal detection rules from the test samples,
// prints a human-readable summary, and writes the result to rules.json
func TestOptimizeRules(t *testing.T) {
	positives, negatives := computeAudioSamples(t)
	fmt.Printf("\n%d positive files (%d samples), %d negative files (%d samples)\n\n",
		len(positives), len(flatten(positives)), len(negatives), len(flatten(negatives)))

	groups := groupFiles(positives, negatives)
	rules := make([]rule, len(groups))
	var totalLeaks int
	for i, g := range groups {
		rules[i] = boundsToRule(g.bounds)
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
			fmt.Printf("    %s %s: %.4f\n", dir, metricFields[b.metricIdx], b.threshold)
		}
		fmt.Println()
	}
	if totalLeaks > 0 {
		fmt.Printf("Total negative leaks across all rules: %d\n", totalLeaks)
	}

	// Persist the derived rules so Analyze picks them up on next run
	data, err := json.MarshalIndent(rules, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal rules: %v", err)
	}
	if err := os.WriteFile("rules.json", append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write rules.json: %v", err)
	}
	fmt.Printf("Wrote %d rules to rules.json\n", len(rules))
}
