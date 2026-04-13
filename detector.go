package alertdetector

import "math"

const (
	segmentMs   = 250 // Sub-segment size for envelope analysis
	refSegments = 32  // Reference window for length-independent envelope metrics
)

// Metrics holds all computed signal features for the analyzed audio
type Metrics struct {
	MaxZCR          float64 // highest zero-crossing rate among segments
	HighPitchRatio  float64 // fraction of segments with ZCR above high-pitch threshold
	PitchRange      float64 // ZCR range among high-pitched segments
	Tonality        float64 // crossing regularity of high-pitched segments (low = clean tone)
	OverallTonality float64 // crossing regularity of all segments (low = tonal)
	SpectralPurity  float64 // energy concentration around dominant frequency (high = pure tone)
	MidHighRatio    float64 // energy in 800-6000Hz vs total (high = alarm-like pitch range)
	BandFocus       float64 // energy concentration in one band (high = narrow-band signal)
	OscCV           float64 // coefficient of variation of RMS oscillations
	ZcrCV           float64 // coefficient of variation of ZCR across segments
	EnvRegularity   float64 // envelope rhythm regularity (low = regular like alarm)
	Oscillations    float64 // rate of significant direction changes in envelope
	RMSOscillations float64 // rate of direction changes in RMS specifically
}

// Result is returned by Analyze when an alert is detected
type Result struct {
	MatchedRule string  // name of the rule that triggered detection
	Metrics     Metrics // all computed signal features
}

type Detector struct {
	sampleRate          int
	samplesPerSeg       int
	iirA1, iirA2, iirA3 float64 // pre-computed IIR filter coefficients
}

// New creates a Detector for the given sample rate
func New(sampleRate int) *Detector {
	if sampleRate <= 0 {
		panic("alertdetector: sample rate must be positive")
	}

	sr := float64(sampleRate)
	return &Detector{
		sampleRate:    sampleRate,
		samplesPerSeg: sampleRate * segmentMs / 1000,
		// Single-pole IIR alpha = 1 - exp(-2*pi*fc/fs)
		iirA1: 1 - math.Exp(-2*math.Pi*400/sr),
		iirA2: 1 - math.Exp(-2*math.Pi*1500/sr),
		iirA3: 1 - math.Exp(-2*math.Pi*4000/sr),
	}
}

// Analyze runs alert detection on a PCM sample buffer
func (d *Detector) Analyze(samples []int16) *Result {
	overallRMS := rms(samples)
	seg := d.computeSegments(normalizeSamples(samples, overallRMS))
	m := Metrics{
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

	for _, r := range alertRules {
		if r.match(m) {
			return &Result{MatchedRule: r.name, Metrics: m}
		}
	}
	return nil
}

// rule defines min/max bounds for each metric (0 = don't check)
// A rule matches when all non-zero bounds are satisfied
type rule struct {
	name                         string
	minMaxZCR, maxMaxZCR         float64
	minHPR, maxHPR               float64
	maxPitchRange                float64
	maxTonality                  float64
	minOverallTon, maxOverallTon float64
	minPurity, maxPurity         float64
	minMidHigh, maxMidHigh       float64
	minBandFocus, maxBandFocus   float64
	minOscCV, maxOscCV           float64
	minZcrCV, maxZcrCV           float64
	maxEnvReg                    float64
	minOsc, maxOsc               float64
	minRmsOsc, maxRmsOsc         float64
}

func (r rule) match(m Metrics) bool {
	return (r.minMaxZCR == 0 || m.MaxZCR >= r.minMaxZCR) &&
		(r.maxMaxZCR == 0 || m.MaxZCR < r.maxMaxZCR) &&
		(r.minHPR == 0 || m.HighPitchRatio >= r.minHPR) &&
		(r.maxHPR == 0 || m.HighPitchRatio < r.maxHPR) &&
		(r.maxPitchRange == 0 || m.PitchRange < r.maxPitchRange) &&
		(r.maxTonality == 0 || m.Tonality < r.maxTonality) &&
		(r.minOverallTon == 0 || m.OverallTonality > r.minOverallTon) &&
		(r.maxOverallTon == 0 || m.OverallTonality < r.maxOverallTon) &&
		(r.minPurity == 0 || m.SpectralPurity > r.minPurity) &&
		(r.maxPurity == 0 || m.SpectralPurity < r.maxPurity) &&
		(r.minMidHigh == 0 || m.MidHighRatio > r.minMidHigh) &&
		(r.maxMidHigh == 0 || m.MidHighRatio < r.maxMidHigh) &&
		(r.minBandFocus == 0 || m.BandFocus > r.minBandFocus) &&
		(r.maxBandFocus == 0 || m.BandFocus < r.maxBandFocus) &&
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
// Generated by TestOptimizeRules
var alertRules = []rule{
	{name: "alarm-tonal", maxOverallTon: 0.759, minPurity: 0.663, maxOscCV: 0.641, maxMidHigh: 0.67, minMidHigh: 0.294, maxTonality: 0.632},
	{name: "alarm-spectral", maxBandFocus: 0.604, minBandFocus: 0.447, minPurity: 0.554, maxPurity: 0.803, maxZcrCV: 0.383, maxOverallTon: 1.489, minOscCV: 0.141, maxTonality: 1.288},
	{name: "alarm-highton", minOverallTon: 1.327, maxOverallTon: 1.664, maxOscCV: 0.45, minMaxZCR: 0.029},
	{name: "siren", minMidHigh: 0.554, minBandFocus: 0.415, minZcrCV: 0.233, maxZcrCV: 0.414, minOscCV: 0.214, maxOverallTon: 0.79, minMaxZCR: 0.055, maxMaxZCR: 0.098},
	{name: "alarm-hpr", minHPR: 0.704, minPurity: 0.62, minOsc: 0.07, maxTonality: 1.556},
	{name: "siren-sweden", maxOsc: 0.098, minPurity: 0.795, minOverallTon: 0.844, maxMaxZCR: 0.114},
	{name: "alarm-stable", maxOscCV: 0.123, maxBandFocus: 0.463},
	{name: "horn", maxOverallTon: 0.455, maxOscCV: 0.22},
}
