package alertdetector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"slices"
	"sort"
	"testing"
)

// Sample creation ------------------------------------------------------------------------------------

var ambienceFiles = []string{
	"cafe_ambience", "rain", "suburban_garden_ambience_baseline",
	"airplane_austria_ambience", "distant_music_band",
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
		low, high := math.Inf(1), math.Inf(-1)
		for _, p := range positives {
			v := metricValue(p, idx)
			low, high = min(low, v), max(high, v)
		}
		if t, ok := midpointSplit(negatives, idx, low, true); ok {
			candidates = append(candidates, bound{idx, true, t})
		}
		if t, ok := midpointSplit(negatives, idx, high, false); ok {
			candidates = append(candidates, bound{idx, false, t})
		}
	}
	return candidates
}

// midpointSplit picks the threshold halfway to the nearest negative outside
// the positive range, below=true -> min bound, below=false -> max bound
func midpointSplit(negatives []Metrics, idx int, edge float64, below bool) (float64, bool) {
	nearest := math.Inf(1)
	if below {
		nearest = math.Inf(-1)
	}
	for _, n := range negatives {
		v := metricValue(n, idx)
		if below && v < edge && v > nearest {
			nearest = v
		} else if !below && v > edge && v < nearest {
			nearest = v
		}
	}
	if math.IsInf(nearest, 0) {
		return 0, false
	}
	// Round so the positive edge stays on the accepting side after quantizing
	// Min bound (v >= t): round down so t <= edge
	// Max bound (v <  t): round up   so t >  edge
	mid := (edge + nearest) / 2
	t := math.Ceil(mid*1000) / 1000
	if below {
		t = math.Floor(mid*1000) / 1000
	}
	if t <= 0.001 {
		return 0, false
	}
	return t, true
}

// keepPassing returns only the samples that pass the bound
func keepPassing(samples []Metrics, b bound) []Metrics {
	out := make([]Metrics, 0, len(samples))
	for _, s := range samples {
		if b.passes(s) {
			out = append(out, s)
		}
	}
	return out
}

// findBounds greedily picks bounds accepting every positive while rejecting
// the most remaining negatives, returns the bounds and the leak count
func findBounds(positives, negatives []Metrics) (chosen []bound, leaks int) {
	// Keep only candidates that accept every positive - positives don't
	// change across iterations so we filter once
	var usable []bound
	for _, c := range candidateBounds(positives, negatives) {
		if len(keepPassing(positives, c)) == len(positives) {
			usable = append(usable, c)
		}
	}

	remainingNegs := append([]Metrics(nil), negatives...)
	for len(remainingNegs) > 0 {
		bestIdx := -1
		var bestKept []Metrics
		bestRejected := 0
		for i, c := range usable {
			kept := keepPassing(remainingNegs, c)
			if rejected := len(remainingNegs) - len(kept); rejected > bestRejected {
				bestIdx, bestRejected, bestKept = i, rejected, kept
			}
		}
		if bestIdx < 0 {
			break
		}
		chosen = append(chosen, usable[bestIdx])
		remainingNegs = bestKept
	}
	return chosen, len(remainingNegs)
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

// groupFiles seeds one rule per file then merges any pair whose joint rule
// still rejects every negative, first-match wins, sorted for determinism
func groupFiles(positives, negatives map[string][]Metrics) []ruleGroup {
	allNegatives := flatten(negatives)

	sortedFiles := make([]string, 0, len(positives))
	for file := range positives {
		sortedFiles = append(sortedFiles, file)
	}
	sort.Strings(sortedFiles)

	// Seed with one singleton group per file
	groups := make([]ruleGroup, 0, len(sortedFiles))
	for _, file := range sortedFiles {
		bounds, leaks := findBounds(samplesFor([]string{file}, positives), allNegatives)
		groups = append(groups, ruleGroup{[]string{file}, bounds, leaks})
	}

	// Repeatedly scan all pairs and merge the first one whose joint rule
	// has zero training leaks - stop when no more merges are possible
	for {
		mergedThisPass := false
		for i := 0; i < len(groups) && !mergedThisPass; i++ {
			for j := i + 1; j < len(groups); j++ {
				combinedFiles := slices.Concat(groups[i].files, groups[j].files)
				bounds, leaks := findBounds(samplesFor(combinedFiles, positives), allNegatives)
				if leaks != 0 {
					continue
				}
				groups[i] = ruleGroup{combinedFiles, bounds, 0}
				groups = append(groups[:j], groups[j+1:]...)
				mergedThisPass = true
				break
			}
		}
		if !mergedThisPass {
			break
		}
	}
	return groups
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
