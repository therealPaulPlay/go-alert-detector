package alertdetector

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/gunter-q12/resample"
)

const testSampleRate = 48000

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
	{"air_raid_siren_devon", true},
	{"bombtest_siren", true},
	{"fast_smoke_alarm_variation", true},
	{"house_alarm_berlin_with_cars", true},
	{"siren_france", true},
	{"smoke_alarm_slight_echo", true},
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
	{"air_raid_siren_germany", true},
	{"air_siren_usa_terrifying", true},
	{"alarm_siren_echo", true},
	{"ambulance_dutch", true},
	{"ambulance_siren_from_video", true},
	{"ambulance_siren_wailing", true},
	{"android_emergency_alarm", true},
	{"fire_alarm_going_off", true},
	{"fire_alarm_test", true},
	{"fire_alarm_with_voice", true},
	{"firetruck_siren_germany", true},
	{"london_two_ambulance_sirens", true},
	{"multiple_fire_alarms_test", true},
	{"nuclear_evacuation_siren", true},
	{"police_siren_variety", true},
	{"pull_fire_alarm", true},
	{"rusty_fire_alarm", true},
	{"school_fire_alarm_uk", true},
	{"siren_fire_signal_germany", true},
	{"world_war_siren", true},
	{"beeping_house_alarm", true},
	{"cheap_house_alarm", true},
	{"fast_house_intruder_alarm", true},
	{"home_alarm_people_partying", true},
	{"house_alarm_germany", true},
	{"house_alarm_ring_variant", true},
	{"house_alarm_slight_drum_music", true},
	{"old_scantronic_house_alarm", true},
	{"old_school_alarm_with_voice", true},
	{"ring_house_alarm", true},
	{"short_smoke_alarm_test", true},
	{"smoke_alarm_closeup", true},
	{"smoke_detector_bell_alarm", true},
	{"air_raid_siren_phone_recording", true},
	{"ambulance_german_phone_recording", true},
	{"burglar_alarm_phone_recording_noisy", true},
	{"eas_canada_phone_recording", true},
	{"eas_china_phone_recording", true},
	{"eas_malaysia_phone_recording", true},
	{"eas_mexico_phone_recording", true},
	{"eas_usa_phone_recording", true},
	{"fire_alarm_phone_recording", true},
	{"security_alarm_phone_recording", true},
	{"small_siren_phone_recording", true},
	{"war_siren_phone_recording", true},
	{"war_siren_variant_phone_recording", true},

	// --- Should NOT detect ---
	{"rain", false},
	{"thunder", false},
	{"aurora_dreams_female_voice", false},
	{"buffalo_refrain", false},
	{"coldplay_beat_drop", false},
	{"danzinger_refrain", false},
	{"dxve_mask_off_rap", false},
	{"harry_sattelite_refrain", false},
	{"heaven_song_with_clapping", false},
	{"kasi_relaxed_pop", false},
	{"lina_soft_pop", false},
	{"liquido_pop_beat", false},
	{"iphone_alarm_clock", false},
	{"iphone_timer_sound", false},
	{"phone_call_variant", false},
	{"rochelle_beat_drop", false},
	{"sneeze_with_slight_music", false},
	{"souly_drill_beat", false},
	{"tate_mcrae_I_know_love_refrain", false},
	{"tym_hyperpop", false},
	{"tym_hyperpop_variant", false},
	{"zartmann_relaxed_pop", false},
	{"noisy_lithe_recording", false},
	{"apple_peeler", false},
	{"balloon_rub", false},
	{"car_window_open", false},
	{"fireplace", false},
	{"leaves_crunching", false},
	{"puffing_pillow", false},
	{"record_player_needle_on_track", false},
	{"rubber_band_pluck", false},
	{"snoring", false},
	{"violin_blues", false},
	{"window_open", false},
	{"baby_crying_variant", false},
	{"chicken_song", false},
	{"drumroll", false},
	{"fighter_jet", false},
	{"flute", false},
	{"goofy_sound", false},
	{"hyundai_ev_accelerate", false},
	{"karneval_intro", false},
	{"kia_ev_accelerate", false},
	{"kitten_meow", false},
	{"marimba", false},
	{"ncs_electro_light", false},
	{"ncs_jeja_song", false},
	{"playstation_one_startup", false},
	{"shepard_bark", false},
	{"smart_doorbell_chime", false},
	{"vase_breaking", false},
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
	{"alan_walker_recreation", false},
	{"cars_honking", false},
	{"guitar", false},
	{"hiphop_with_vocals", false},
	{"orchestra_warmup", false},
	{"space_orchestra", false},
	{"violin_dark", false},
	{"violin_pain", false},
	{"opera_singer", false},
	{"cafe_ambience", false},
	{"suburban_garden_ambience_baseline", false},
	{"microphone_noise_floor", false},
	{"music", false},
	{"door_slam", false},
	{"lawnmower", false},
	{"talking", false},
	{"car_passing", false},
	{"road_traffic", false},
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

// TestAudioDetection runs each test file at normal volume
func TestAudioDetection(t *testing.T) {
	for _, tc := range audioTests {
		t.Run(tc.file, func(t *testing.T) {
			d := newDetector()
			samples := loadWAV(t, "testdata/audio/"+tc.file+".wav")
			detected := d.Analyze(samples) != nil
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
					samples := scaleVolume(loadWAV(t, "testdata/audio/"+tc.file+".wav"), vol.factor)
					detected := d.Analyze(samples) != nil
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

// TestAudioDetection_SpeedScaling tests across speed variants
func TestAudioDetection_SpeedScaling(t *testing.T) {
	for _, s := range []struct {
		name   string
		factor float64
	}{{"slow_0.9x", 0.9}, {"slow_0.925x", 0.925}, {"slow_0.95x", 0.95}, {"relaxed_0.975x", 0.975}, {"swift_1.025x", 1.025}, {"fast_1.05x", 1.05}, {"fast_1.075x", 1.075}, {"fast_1.1x", 1.1}} {
		t.Run(s.name, func(t *testing.T) {
			for _, tc := range audioTests {
				t.Run(tc.file, func(t *testing.T) {
					d := newDetector()
					detected := d.Analyze(scaleSpeed(loadWAV(t, "testdata/audio/"+tc.file+".wav"), s.factor)) != nil
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

// TestAudioDetection_DurationTrimming tests with clips trimmed to various lengths
func TestAudioDetection_DurationTrimming(t *testing.T) {
	for _, dur := range []struct {
		name    string
		seconds int
	}{{"8s", 8}, {"12s", 12}, {"16s", 16}, {"20s", 20}} {
		t.Run(dur.name, func(t *testing.T) {
			for _, tc := range audioTests {
				t.Run(tc.file, func(t *testing.T) {
					d := newDetector()
					samples := loadWAV(t, "testdata/audio/"+tc.file+".wav")
					maxSamples := dur.seconds * testSampleRate
					if len(samples) > maxSamples {
						samples = samples[:maxSamples]
					}
					detected := d.Analyze(samples) != nil
					if tc.detect && !detected {
						t.Errorf("expected detection for %s at %s", tc.file, dur.name)
					}
					if !tc.detect && detected {
						t.Errorf("unexpected detection for %s at %s", tc.file, dur.name)
					}
				})
			}
		})
	}
}

// windowedDetect slides an 8-second window over the samples and returns
// whether any window triggers detection, and whether detection stops after starting
func windowedDetect(d *Detector, samples []int16, trackStop bool) (detected, stopped bool) {
	windowSize := 8 * testSampleRate
	step := 2 * testSampleRate
	for offset := windowSize; offset <= len(samples); offset += step {
		window := samples[offset-windowSize : offset]
		if d.Analyze(window) != nil {
			detected = true
			if !trackStop {
				return
			}
		} else if detected {
			stopped = true
			return
		}
	}
	return
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

// Test transition from alarm to quiet — should detect then stop
func TestMixedTransitions_AlarmThenQuiet(t *testing.T) {
	for _, tc := range alarmThenQuietTests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetector()
			alarm := loadWAV(t, "testdata/audio/"+tc.alarm+".wav")
			quiet := loadWAV(t, "testdata/audio/"+tc.quiet+".wav")
			mixed := append(alarm, quiet...)
			mixed = append(mixed, quiet...)

			detected, stopped := windowedDetect(d, mixed, true)
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

// Test transition from quiet to alarm — should eventually detect
func TestMixedTransitions_QuietThenAlarm(t *testing.T) {
	for _, tc := range quietThenAlarmTests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDetector()
			quiet := loadWAV(t, "testdata/audio/"+tc.quiet+".wav")
			alarm := loadWAV(t, "testdata/audio/"+tc.alarm+".wav")
			mixed := append(quiet, alarm...)
			mixed = append(mixed, alarm...)

			if detected, _ := windowedDetect(d, mixed, false); !detected {
				t.Errorf("expected detection for %s after %s", tc.alarm, tc.quiet)
			}
		})
	}
}
