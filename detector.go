package alertdetector

import (
	_ "embed"
	"encoding/json"
	"math"
)

//go:embed rules.json
var rulesJSON []byte

const (
	segmentMs   = 250 // Sub-segment size for envelope analysis
	refSegments = 32  // Reference window for length-independent envelope metrics
)

// Metrics holds all computed signal features for the analyzed audio
type Metrics struct {
	MaxZCR          float64 // highest zero-crossing rate among segments
	HighPitchRatio  float64 // fraction of segments with ZCR above high-pitch threshold
	OverallTonality float64 // crossing regularity of all segments (low = tonal)
	SpectralPurity  float64 // energy concentration around dominant frequency (high = pure tone)
	MidHighRatio    float64 // energy in 800-6000Hz vs total (high = alarm-like pitch range)
	BandFocus       float64 // energy concentration in one band (high = narrow-band signal)
	OscCV           float64 // coefficient of variation of RMS oscillations
	EnvRegularity   float64 // envelope rhythm regularity (low = regular like alarm)
	Oscillations    float64 // rate of significant direction changes in envelope
}

// Result is returned by Analyze when an alert is detected
type Result struct {
	Metrics Metrics // all computed signal features
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
	m := d.computeMetrics(samples)
	for _, r := range alertRules {
		if r.match(m) {
			return &Result{Metrics: m}
		}
	}
	return nil
}

// computeMetrics runs the full feature-extraction pipeline and returns a Metrics struct
func (d *Detector) computeMetrics(samples []int16) Metrics {
	seg := d.computeSegments(normalizeSamples(samples, rms(samples)))
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

// rule defines min/max bounds for each metric (0 = don't check)
// A rule matches when all non-zero bounds are satisfied
// JSON field names match the Metrics struct so rules.json stays readable
type rule struct {
	MinMaxZCR          float64 `json:"MinMaxZCR,omitempty"`
	MaxMaxZCR          float64 `json:"MaxMaxZCR,omitempty"`
	MinHighPitchRatio  float64 `json:"MinHighPitchRatio,omitempty"`
	MaxHighPitchRatio  float64 `json:"MaxHighPitchRatio,omitempty"`
	MinOverallTonality float64 `json:"MinOverallTonality,omitempty"`
	MaxOverallTonality float64 `json:"MaxOverallTonality,omitempty"`
	MinSpectralPurity  float64 `json:"MinSpectralPurity,omitempty"`
	MaxSpectralPurity  float64 `json:"MaxSpectralPurity,omitempty"`
	MinMidHighRatio    float64 `json:"MinMidHighRatio,omitempty"`
	MaxMidHighRatio    float64 `json:"MaxMidHighRatio,omitempty"`
	MinBandFocus       float64 `json:"MinBandFocus,omitempty"`
	MaxBandFocus       float64 `json:"MaxBandFocus,omitempty"`
	MinOscCV           float64 `json:"MinOscCV,omitempty"`
	MaxOscCV           float64 `json:"MaxOscCV,omitempty"`
	MinEnvRegularity   float64 `json:"MinEnvRegularity,omitempty"`
	MaxEnvRegularity   float64 `json:"MaxEnvRegularity,omitempty"`
	MinOscillations    float64 `json:"MinOscillations,omitempty"`
	MaxOscillations    float64 `json:"MaxOscillations,omitempty"`
}

func (r rule) match(m Metrics) bool {
	return (r.MinMaxZCR == 0 || m.MaxZCR >= r.MinMaxZCR) &&
		(r.MaxMaxZCR == 0 || m.MaxZCR < r.MaxMaxZCR) &&
		(r.MinHighPitchRatio == 0 || m.HighPitchRatio >= r.MinHighPitchRatio) &&
		(r.MaxHighPitchRatio == 0 || m.HighPitchRatio < r.MaxHighPitchRatio) &&
		(r.MinOverallTonality == 0 || m.OverallTonality >= r.MinOverallTonality) &&
		(r.MaxOverallTonality == 0 || m.OverallTonality < r.MaxOverallTonality) &&
		(r.MinSpectralPurity == 0 || m.SpectralPurity >= r.MinSpectralPurity) &&
		(r.MaxSpectralPurity == 0 || m.SpectralPurity < r.MaxSpectralPurity) &&
		(r.MinMidHighRatio == 0 || m.MidHighRatio >= r.MinMidHighRatio) &&
		(r.MaxMidHighRatio == 0 || m.MidHighRatio < r.MaxMidHighRatio) &&
		(r.MinBandFocus == 0 || m.BandFocus >= r.MinBandFocus) &&
		(r.MaxBandFocus == 0 || m.BandFocus < r.MaxBandFocus) &&
		(r.MinOscCV == 0 || m.OscCV >= r.MinOscCV) &&
		(r.MaxOscCV == 0 || m.OscCV < r.MaxOscCV) &&
		(r.MinEnvRegularity == 0 || m.EnvRegularity >= r.MinEnvRegularity) &&
		(r.MaxEnvRegularity == 0 || m.EnvRegularity < r.MaxEnvRegularity) &&
		(r.MinOscillations == 0 || m.Oscillations >= r.MinOscillations) &&
		(r.MaxOscillations == 0 || m.Oscillations < r.MaxOscillations)
}

// alertRules is loaded from the embedded rules.json at package init
var alertRules = func() []rule {
	var rules []rule
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		panic("alertdetector: failed to parse embedded rules.json: " + err.Error())
	}
	return rules
}()
