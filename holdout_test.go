package alertdetector

import (
	"fmt"
	"testing"
)

// holdoutTests are files the optimizer has never seen
// Used to evaluate generalization of the current production rules
var holdoutTests = []audioTestCase{
	// --- Should detect ---
	{"air_raid_siren_devon", true},
	{"bombtest_siren", true},
	{"fast_smoke_alarm_variation", true},
	{"house_alarm_berlin_with_cars", true},
	{"siren_france", true},
	{"smoke_alarm_slight_echo", true},

	// --- Should NOT detect ---
	{"alan_walker_recreation", false},
	{"cars_honking", false},
	{"guitar", false},
	{"hiphop_with_vocals", false},
	{"orchestra_warmup", false},
	{"space_orchestra", false},
	{"violin_dark", false},
	{"violin_pain", false},
}

// TestHoldout evaluates the current alertRules against files the optimizer
// has never seen, reporting true/false positive/negative counts
func TestHoldout(t *testing.T) {
	var tp, fp, tn, fn int
	var fpFiles, fnFiles []string
	for _, tc := range holdoutTests {
		samples := loadWAV(t, "testdata/"+tc.file+".wav")
		detected := analyzeWithRules(samples, alertRules)
		switch {
		case tc.detect && detected:
			tp++
		case tc.detect && !detected:
			fn++
			fnFiles = append(fnFiles, tc.file)
		case !tc.detect && detected:
			fp++
			fpFiles = append(fpFiles, tc.file)
		case !tc.detect && !detected:
			tn++
		}
	}
	fmt.Printf("\nTrue positives:  %d/%d\n", tp, tp+fn)
	fmt.Printf("True negatives:  %d/%d\n", tn, tn+fp)
	fmt.Printf("False positives: %d %v\n", fp, fpFiles)
	fmt.Printf("False negatives: %d %v\n", fn, fnFiles)
}

// analyzeWithRules computes metrics and checks whether any of the given
// rules match — used by holdout tests to evaluate rule sets directly
func analyzeWithRules(samples []int16, rules []rule) bool {
	m := analyzeMetrics(samples)
	for _, r := range rules {
		if r.match(m) {
			return true
		}
	}
	return false
}
