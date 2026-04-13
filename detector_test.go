package alertdetector

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"
	"time"

	"github.com/gunter-q12/resample"
)

const testSampleRate = 48000

// checkInterval matches the polling rate used in root-firmware
var checkInterval = 2 * time.Second

// loadWAV reads a 16-bit mono WAV file, resampling to testSampleRate if needed
func loadWAV(t *testing.T, path string) []int16 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to load %s: %v", path, err)
	}

	srcRate := int(binary.LittleEndian.Uint32(data[24:28]))

	// Find "data" chunk
	var pcm []byte
	for i := 12; i < len(data)-8; i++ {
		if string(data[i:i+4]) == "data" {
			size := int(binary.LittleEndian.Uint32(data[i+4 : i+8]))
			pcm = data[i+8 : i+8+size]
			break
		}
	}

	// Resample if needed
	if srcRate != testSampleRate {
		var buf bytes.Buffer
		r, err := resample.New(&buf, resample.FormatInt16, srcRate, testSampleRate, 1)
		if err != nil {
			t.Fatalf("Failed to create resampler: %v", err)
		}
		if _, err := r.Write(pcm); err != nil {
			t.Fatalf("Failed to resample: %v", err)
		}
		pcm = buf.Bytes()
	}

	samples := make([]int16, len(pcm)/2)
	for i := range samples {
		samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
	}
	return samples
}

func newDetector() *Detector {
	return New(testSampleRate)
}

// Scale the volume by a factor (e.g. 0.5 -> 50% volume)
func scaleVolume(samples []int16, factor float64) []int16 {
	out := make([]int16, len(samples))
	for i, s := range samples {
		v := float64(s) * factor
		if v > math.MaxInt16 {
			v = math.MaxInt16
		} else if v < math.MinInt16 {
			v = math.MinInt16
		}
		out[i] = int16(v)
	}
	return out
}

func feedAndDetect(d *Detector, samples []int16, trackStop bool) (detected, stopped bool) {
	step := int(checkInterval.Seconds()) * testSampleRate
	for offset := d.inputSamples; offset <= len(samples); offset += step {
		window := samples[offset-d.inputSamples : offset]

		if d.Analyze(window) != nil {
			detected = true
			// If we don't care about where it stops detecting, return on the first positive detection
			if !trackStop {
				return
			}
		} else if detected {
			// If previously detected but no longer at this point, return as stopped
			stopped = true
			return
		}
	}
	return
}

type audioTestCase struct {
	file   string
	detect bool
}

var audioTests = []audioTestCase{
	// --- Should detect ---
	{"smoke_alarm", true},
	{"fire_alarm", true},
	{"air_raid_siren", true},
	{"firetruck_siren", true},
	{"air_raid_siren_children_playing", true},
	{"fire_alarm_with_occasional_lawnmower", true},
	{"emergency_siren_sweden", true},
	{"house_alarm", true},
	{"modern_smoke_alarm_and_running", true},
	{"abc_siren", true},
	{"police_siren", true},
	{"car_alarm_in_public", true},
	{"quiet_fire_alarm", true},
	{"fast_smoke_alarm_in_rain", true},
	{"alarm_door", true},
	{"australia_alarm_sews", true},
	{"belgium_nuclear_siren", true},
	{"evacuation_siren", true},
	{"martinshorn_siren", true},
	{"nuclear_alarm_horn", true},
	{"switzerland_siren", true},
	{"bell_fire_alarm", true},
	{"air_raid_siren_shanghai", true},
	{"evacuation_alarm", true},
	{"evacuation_alarm_with_people_screaming", true},
	{"chevrolet_car_alarm", true},
	{"distant_police_cars", true},
	{"distant_siren", true},
	{"distant_smoke_alarm_slight_wind", true},
	{"facility_evacuation_alarm", true},

	// --- Should NOT detect ---
	{"rain", false},
	{"opera_singer", false},
	{"cafe_ambience", false},
	{"music", false},
	{"door_slam", false},
	{"lawnmower", false},
	{"talking", false},
	{"car_passing", false},
	{"metal_drag", false},
	{"stuff_breaking", false},
	{"trumpet", false},
	{"water_sewer", false},
	{"baby_crying", false},
	{"chopping", false},
	{"creaking", false},
	{"door_code_input", false},
	{"jackhammer", false},
	{"piano", false},
	{"crickets", false},
	{"loud_birds", false},
	{"human_whistle_melody", false},
	{"mountain_whistle", false},
	{"cat_meowing", false},
	{"church_bells", false},
	{"circular_saw", false},
	{"dog_barking", false},
	{"edm_dance", false},
	{"mosquito", false},
	{"train_takeoff", false},
	{"ship_fog_horn", false},
	{"supersaw_synth", false},
	{"synth_chords", false},
	{"telephone_wait", false},
	{"drums", false},
	{"egg_timer", false},
	{"hi_hat_solo", false},
	{"robot_voice", false},
	{"airplane_austria_ambience", false},
	{"audio_logo_tv_show", false},
	{"bright_pop", false},
	{"dark_pop", false},
	{"fabric_dragging", false},
	{"intense_drinking", false},
	{"plane_engine", false},
	{"pop_song_bright_female_voice", false},
	{"scratching", false},
	{"silence", false},
	{"steps_on_roof", false},
	{"tape_rewind", false},
	{"tv_static", false},
	{"wind_chimes", false},
	{"chiptune", false},
	{"toddlers_playing_laughing", false},
	{"tuba", false},
	{"digital_alarm_clock", false},
	{"distant_lion_roar", false},
	{"distant_music_band", false},
	{"distant_swimming_pool", false},
	{"underground_echo", false},
}

// detectAudio runs Analyze or feedAndDetect based on clip length
func detectAudio(d *Detector, samples []int16) bool {
	if len(samples) > d.inputSamples {
		detected, _ := feedAndDetect(d, samples, false)
		return detected
	}
	return d.Analyze(samples) != nil
}

// TestAudioDetection runs each test file at normal volume
func TestAudioDetection(t *testing.T) {
	for _, tc := range audioTests {
		t.Run(tc.file, func(t *testing.T) {
			d := newDetector()
			samples := loadWAV(t, "testdata/"+tc.file+".wav")
			detected := detectAudio(d, samples)
			if tc.detect && !detected {
				t.Errorf("expected detection for %s", tc.file)
			}
			if !tc.detect && detected {
				t.Errorf("unexpected detection for %s", tc.file)
			}
		})
	}
}

// TestAudioDetection_VolumeScaling runs at 0.5x and 1.5x volume
func TestAudioDetection_VolumeScaling(t *testing.T) {
	for _, vol := range []struct {
		name   string
		factor float64
	}{
		{"half_volume", 0.5},
		{"loud_volume", 1.5},
	} {
		t.Run(vol.name, func(t *testing.T) {
			for _, tc := range audioTests {
				t.Run(tc.file, func(t *testing.T) {
					d := newDetector()
					samples := scaleVolume(loadWAV(t, "testdata/"+tc.file+".wav"), vol.factor)
					detected := detectAudio(d, samples)
					if tc.detect && !detected {
						t.Errorf("expected detection for %s at %s", tc.file, vol.name)
					}
					if !tc.detect && detected {
						t.Errorf("unexpected detection for %s at %s", tc.file, vol.name)
					}
				})
			}
		})
	}
}


// scaleSpeed resamples to simulate speed change (factor > 1 = faster/higher pitch)
func scaleSpeed(samples []int16, factor float64) []int16 {
	out := make([]int16, int(float64(len(samples))/factor))
	for i := range out {
		idx := int(float64(i) * factor)
		if idx >= len(samples) {
			idx = len(samples) - 1
		}
		out[i] = samples[idx]
	}
	return out
}

// TestAudioDetection_SpeedScaling tests at 0.9x and 1.1x speed
func TestAudioDetection_SpeedScaling(t *testing.T) {
	for _, s := range []struct {
		name   string
		factor float64
	}{{"slow_0.9x", 0.9}, {"slow_0.95x", 0.95}, {"fast_1.05x", 1.05}, {"fast_1.1x", 1.1}} {
		t.Run(s.name, func(t *testing.T) {
			for _, tc := range audioTests {
				t.Run(tc.file, func(t *testing.T) {
					d := newDetector()
					detected := detectAudio(d, scaleSpeed(loadWAV(t, "testdata/"+tc.file+".wav"), s.factor))
					if tc.detect && !detected {
						t.Errorf("expected detection at %s", s.name)
					}
					if !tc.detect && detected {
						t.Errorf("unexpected detection at %s", s.name)
					}
				})
			}
		})
	}
}

var alarmThenQuietTests = []struct {
	name  string
	alarm string
	quiet string
}{
	{"AirRaidSirenThenMusic", "air_raid_siren", "music"},
	{"BelgiumNuclearSirenThenTalking", "belgium_nuclear_siren", "talking"},
	{"BellFireAlarmThenLawnmower", "bell_fire_alarm", "lawnmower"},
}

// Test transition from sound A to B, where B is an alert
func TestMixedTransitions_AlarmThenQuiet(t *testing.T) {
	for _, tc := range alarmThenQuietTests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetector()
			alarm := loadWAV(t, "testdata/"+tc.alarm+".wav")
			quiet := loadWAV(t, "testdata/"+tc.quiet+".wav")
			mixed := append(alarm, quiet...)
			mixed = append(mixed, quiet...)

			detected, stopped := feedAndDetect(d, mixed, true)
			if !detected {
				t.Errorf("expected detection during %s", tc.alarm)
			}
			if !stopped {
				t.Errorf("expected detection to stop during %s", tc.quiet)
			}
		})
	}
}

var quietThenAlarmTests = []struct {
	name  string
	quiet string
	alarm string
}{
	{"RainThenAirRaidSiren", "rain", "air_raid_siren"},
	{"CafeThenBellFireAlarm", "cafe_ambience", "bell_fire_alarm"},
	{"MusicThenBelgiumNuclearSiren", "music", "belgium_nuclear_siren"},
	{"TalkingThenAirRaidSiren", "talking", "air_raid_siren"},
	{"LawnmowerThenEmergencySirenSweden", "lawnmower", "emergency_siren_sweden"},
}

// Test transition from sound A to B, where A is an alert and B should not detect
func TestMixedTransitions_QuietThenAlarm(t *testing.T) {
	for _, tc := range quietThenAlarmTests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetector()
			quiet := loadWAV(t, "testdata/"+tc.quiet+".wav")
			alarm := loadWAV(t, "testdata/"+tc.alarm+".wav")
			mixed := append(quiet, alarm...)
			mixed = append(mixed, alarm...)

			if detected, _ := feedAndDetect(d, mixed, false); !detected {
				t.Errorf("expected detection for %s after %s", tc.alarm, tc.quiet)
			}
		})
	}
}
