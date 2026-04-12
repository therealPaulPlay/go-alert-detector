package alertdetector

import "math"

const (
	segmentMs     = 250 // Sub-segment size for envelope analysis
	windowSeconds = 8   // Audio buffer length
	minRatio      = 2.0 // Minimum Root-Mean-Square above baseline (noise gate)
)

// Metrics holds all computed signal features for the analyzed audio
type Metrics struct {
	RMSRatio       float64 // RMS relative to baseline noise floor
	MaxZCR         float64 // highest zero-crossing rate among segments
	HighPitchRatio float64 // fraction of segments with ZCR above high-pitch threshold
	PitchRange     float64 // ZCR range among high-pitched segments
	Tonality       float64 // crossing regularity of high-pitched segments (low = clean tone)
	OverallTonality float64 // crossing regularity of all segments (low = tonal)
	SpectralPurity float64 // energy concentration around dominant frequency (high = pure tone)
	MidHighRatio   float64 // energy in 800-6000Hz vs total (high = alarm-like pitch range)
	BandFocus      float64 // energy concentration in one band (high = narrow-band signal)
	OscCV          float64 // coefficient of variation of RMS oscillations
	ZcrCV          float64 // coefficient of variation of ZCR across segments
	EnvRegularity  float64 // envelope rhythm regularity (low = regular like alarm)
	Oscillations   int     // significant direction changes in envelope
	RMSOscillations int    // direction changes in RMS specifically
}

// Result is returned by Analyze when an alert is detected
type Result struct {
	MatchedRule string  // name of the rule that triggered detection
	Metrics     Metrics // all computed signal features
}

type Detector struct {
	sampleRate          int
	inputSamples        int
	samplesPerSeg       int
	baselineMax         int
	baseline            []float64
	iirA1, iirA2, iirA3 float64 // pre-computed IIR filter coefficients
}

// New creates a Detector for the given sample rate and baseline size
// baselineSize controls how many past RMS values are averaged for the
// noise floor (e.g. 30 for a live mic polled every 2s = ~1 minute)
func New(sampleRate, baselineSize int) *Detector {
	if sampleRate <= 0 {
		panic("alertdetector: sample rate must be positive")
	}
	if baselineSize <= 0 {
		panic("alertdetector: baseline size must be positive")
	}

	sr := float64(sampleRate)
	return &Detector{
		sampleRate:    sampleRate,
		inputSamples:  windowSeconds * sampleRate,
		samplesPerSeg: sampleRate * segmentMs / 1000,
		baselineMax:   baselineSize,
		// Single-pole IIR alpha = 1 - exp(-2*pi*fc/fs)
		iirA1: 1 - math.Exp(-2*math.Pi*400/sr),
		iirA2: 1 - math.Exp(-2*math.Pi*1500/sr),
		iirA3: 1 - math.Exp(-2*math.Pi*4000/sr),
	}
}

// InputSize returns the number of int16 samples required per Analyze call
func (d *Detector) InputSize() int {
	return d.inputSamples
}

// Analyze runs alert detection on a PCM sample buffer
func (d *Detector) Analyze(samples []int16) *Result {
	overallRMS := rms(samples)
	baseRMS, ok := d.baselineRMS()
	if !ok {
		// No baseline yet, seed it and return
		d.updateBaseline(overallRMS, 0, false)
		return nil
	}

	seg := d.computeSegments(normalizeSamples(samples, overallRMS))
	ratio := overallRMS / baseRMS
	m := Metrics{
		RMSRatio:        ratio,
		MaxZCR:          seg.maxZCR,
		HighPitchRatio:  seg.highPitchRatio,
		PitchRange:      seg.pitchRange,
		Tonality:        seg.tonality,
		OverallTonality: seg.overallTonality,
		SpectralPurity:  seg.spectralPurity,
		MidHighRatio:    seg.midHighRatio,
		BandFocus:       seg.bandFocus,
		OscCV:           cv(seg.segRMS),
		ZcrCV:           cv(seg.segZCR),
		EnvRegularity:   envelopeRegularity(seg.segRMS),
		Oscillations:    max(countSwings(seg.segRMS), countSwings(seg.segZCR)),
		RMSOscillations: countSwings(seg.segRMS),
	}

	if ratio > minRatio {
		for _, r := range alertRules {
			if r.match(m) {
				d.updateBaseline(overallRMS, baseRMS, true)
				return &Result{MatchedRule: r.name, Metrics: m}
			}
		}
	}

	d.updateBaseline(overallRMS, baseRMS, false)
	return nil
}

// rule defines min/max bounds for each metric (0 = don't check)
// A rule matches when all non-zero bounds are satisfied
type rule struct {
	name                         string
	minMaxZCR, maxMaxZCR         float64
	minHPR                       float64
	maxPitchRange                float64
	maxTonality                  float64
	minOverallTon, maxOverallTon float64
	minPurity                    float64
	minMidHigh, minBandFocus     float64
	minOscCV, maxOscCV           float64
	minZcrCV, maxZcrCV           float64
	maxEnvReg                    float64
	minOsc, maxOsc               int
	minRmsOsc, maxRmsOsc         int
}

func (r rule) match(m Metrics) bool {
	return (r.minMaxZCR == 0 || m.MaxZCR >= r.minMaxZCR) &&
		(r.maxMaxZCR == 0 || m.MaxZCR < r.maxMaxZCR) &&
		(r.minHPR == 0 || m.HighPitchRatio >= r.minHPR) &&
		(r.maxPitchRange == 0 || m.PitchRange < r.maxPitchRange) &&
		(r.maxTonality == 0 || m.Tonality < r.maxTonality) &&
		(r.minOverallTon == 0 || m.OverallTonality > r.minOverallTon) &&
		(r.maxOverallTon == 0 || m.OverallTonality < r.maxOverallTon) &&
		(r.minPurity == 0 || m.SpectralPurity > r.minPurity) &&
		(r.minMidHigh == 0 || m.MidHighRatio > r.minMidHigh) &&
		(r.minBandFocus == 0 || m.BandFocus > r.minBandFocus) &&
		(r.minOscCV == 0 || m.OscCV > r.minOscCV) &&
		(r.maxOscCV == 0 || m.OscCV < r.maxOscCV) &&
		(r.minZcrCV == 0 || m.ZcrCV > r.minZcrCV) &&
		(r.maxZcrCV == 0 || m.ZcrCV < r.maxZcrCV) &&
		(r.maxEnvReg == 0 || m.EnvRegularity < r.maxEnvReg) &&
		(r.minOsc == 0 || m.Oscillations >= r.minOsc) &&
		(r.maxOsc == 0 || m.Oscillations < r.maxOsc) &&
		(r.minRmsOsc == 0 || m.RMSOscillations >= r.minRmsOsc) &&
		(r.maxRmsOsc == 0 || m.RMSOscillations < r.maxRmsOsc)
}

// Each rule targets a specific alarm archetype, alert fires if any rule matches
var alertRules = []rule{
	{name: "alarm-tonal", minHPR: 0.5, maxPitchRange: 0.15, maxTonality: 1.5, minPurity: 0.65, maxOscCV: 0.7, maxEnvReg: 0.95, maxRmsOsc: 15},
	{name: "alarm-tonal-osc", minHPR: 0.5, maxPitchRange: 0.15, maxTonality: 1.5, minPurity: 0.65, minRmsOsc: 5, maxEnvReg: 0.95, maxRmsOsc: 15},
	{name: "alarm-beat", minHPR: 0.5, maxPitchRange: 0.15, maxTonality: 1.6, maxOscCV: 0.15, maxEnvReg: 0.95, maxRmsOsc: 15},
	{name: "alarm-shifted", minHPR: 0.3, maxPitchRange: 0.15, maxTonality: 1.5, minPurity: 0.85, maxOscCV: 0.7, maxEnvReg: 0.95, maxRmsOsc: 15},
	{name: "alarm-shifted-osc", minHPR: 0.3, maxPitchRange: 0.15, maxTonality: 1.5, minPurity: 0.85, minRmsOsc: 5, maxEnvReg: 0.95, maxRmsOsc: 15},
	{name: "alarm-shifted-beat", minHPR: 0.3, maxPitchRange: 0.15, maxTonality: 1.6, minPurity: 0.85, maxOscCV: 0.15, maxEnvReg: 0.95, maxRmsOsc: 15},

	{name: "siren", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, maxOverallTon: 0.7, maxEnvReg: 0.65, minPurity: 0.65, minOsc: 4, maxOsc: 20},
	{name: "siren-bandfocus", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, maxOverallTon: 0.7, maxEnvReg: 0.75, minPurity: 0.65, minOsc: 4, maxOsc: 20, minBandFocus: 0.42},
	{name: "siren-tonal", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, maxOverallTon: 0.45, maxEnvReg: 0.65, minPurity: 0.65},
	{name: "siren-tonal-bandfocus", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, maxOverallTon: 0.45, maxEnvReg: 0.75, minPurity: 0.65, minBandFocus: 0.42},
	{name: "siren-pure-bandfocus", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, maxOverallTon: 0.7, maxEnvReg: 0.85, minPurity: 0.85, minOsc: 4, maxOsc: 20, minBandFocus: 0.42},
	{name: "siren-distant", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.08, maxEnvReg: 0.5, minPurity: 0.75, maxOverallTon: 0.7, minOsc: 4, maxOsc: 20},
	{name: "siren-doppler-bandfocus", minMaxZCR: 0.035, minOscCV: 0.2, maxZcrCV: 0.15, minMidHigh: 0.5, maxOverallTon: 0.5, minPurity: 0.65, minOsc: 4, maxOsc: 20, maxEnvReg: 0.75, minBandFocus: 0.42},
	{name: "siren-sweep", minMaxZCR: 0.035, minOscCV: 0.2, minZcrCV: 0.2, maxZcrCV: 0.4, minPurity: 0.85, minBandFocus: 0.5, minMidHigh: 0.3, minOverallTon: 0.9, maxOverallTon: 1.2, minOsc: 2},

	{name: "horn", minMaxZCR: 0.035, maxOscCV: 0.2, minPurity: 0.7, maxOverallTon: 1.6},
	{name: "horn-stable", minMaxZCR: 0.035, maxOscCV: 0.075, maxOverallTon: 1.6},
	{name: "horn-tonal", minMaxZCR: 0.035, maxOscCV: 0.25, minPurity: 0.7, maxOverallTon: 0.7},
	{name: "horn-low", minMaxZCR: 0.025, maxMaxZCR: 0.035, minPurity: 0.7, maxOscCV: 0.2, maxOverallTon: 1.6, minMidHigh: 0.25},
	{name: "horn-low-focused", minMaxZCR: 0.025, maxMaxZCR: 0.035, minPurity: 0.7, maxOscCV: 0.2, maxOverallTon: 1.6, minBandFocus: 0.85},
	{name: "horn-low-tonal", minMaxZCR: 0.025, maxMaxZCR: 0.035, minPurity: 0.7, maxOscCV: 0.25, maxOverallTon: 0.7, minMidHigh: 0.25},
}

// baselineRMS returns the noise floor RMS
// Returns ok=false when there are no entries or the floor is silent
func (d *Detector) baselineRMS() (float64, bool) {
	if len(d.baseline) == 0 {
		return 0, false
	}
	baseRMS := mean(d.baseline)
	if baseRMS < 1.0 {
		return 0, false
	}
	return baseRMS, true
}

// updateBaseline adds an RMS entry on non-detection,
// skipping updates when RMS is far from the current level
func (d *Detector) updateBaseline(currentRMS, baseRMS float64, detected bool) {
	if detected {
		return
	}
	// On the very first call there's no baseRMS to compare against
	if baseRMS == 0 || (currentRMS > baseRMS*0.5 && currentRMS < baseRMS*2) {
		if len(d.baseline) >= d.baselineMax {
			d.baseline = d.baseline[1:]
		}
		d.baseline = append(d.baseline, currentRMS)
	}
}

