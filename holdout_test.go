package alertdetector

import (
	"fmt"
	"testing"
)

// Holdout tests evaluate the current alertRules against files the optimizer
// has never seen, measuring generalization
//
// How to add a holdout test:
//   1. Drop the wav file into testdata/
//   2. Add an entry below - {"<filename_without_ext>", true} for alerts that
//      should be detected, false for sounds that should not
//   3. Do NOT also add the file to audioTests in detector_test.go - a file
//      that is in the training set defeats the purpose of a holdout
//
// How to run (the -v is required so the result counts actually print):
//   go test -v -run TestHoldout

var holdoutTests = []audioTestCase{
	// --- Should detect ---
	{"biohazard_alarm", true},
	{"canada_air_raid_siren", true},
	{"eas_alarm_austria", true},
	{"eas_alarm_bermuda", true},
	{"eas_alarm_taiwan", true},
	{"eas_alarm_usa", true},
	{"house_alarm_with_door_opening", true},
	{"multiple_tornado_sirens", true},
	{"phone_eas_alarm_japan", true},
	{"police_siren_passing", true},
	{"russia_air_raid_siren", true},
	{"scratchy_fire_alarm", true},
	{"uk_ambulance_siren", true},

	// --- Should NOT detect ---
	{"african_penguins", false},
	{"computer_cd_drive", false},
	{"electric_guitar", false},
	{"elephant_trumpets", false},
	{"handsaw", false},
	{"jelly", false},
	{"singing_soprano", false},
	{"toilet_flush", false},
	{"wiping_window_squeaks", false},
	{"writing_and_turning_pages", false},
}

// TestHoldout evaluates the current alertRules against files the optimizer
// has never seen, reporting true/false positive/negative counts
func TestHoldout(t *testing.T) {
	if len(holdoutTests) == 0 {
		t.Skip("no holdout tests defined")
	}
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
	m := New(testSampleRate).computeMetrics(samples)
	for _, r := range rules {
		if r.match(m) {
			return true
		}
	}
	return false
}
