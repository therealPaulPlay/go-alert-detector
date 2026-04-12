# Alert detector

Go library for detecting fire alarms, sirens, smoke detectors, police sirens,
and other warning and emergency sounds in PCM audio streams.

Uses a rule-based approach for high performance, fine-tuned against ~100 audio
samples covering varied real-world conditions including distance, echo, and
background noise.

## Usage

```go
import alertdetector "github.com/PaulPlay/go-alert-detector"

// Sample rate (Hz), baseline size (number of past RMS values to average)
detector := alertdetector.New(48000, 30)

// Feed consecutive audio windows as mono 16-bit signed PCM ([]int16)
// Detection works from the first call and improves as the baseline grows
result := detector.Analyze(samples)
if result != nil {
    fmt.Printf("Alert: %s\n", result.MatchedRule)
    fmt.Printf("Metrics: %+v\n", result.Metrics)
}
```

`Analyze` returns `nil` when no alert is detected or when the baseline
is still empty (first call seeds it). On detection, the result contains
the matched rule name and all computed signal metrics.

### Baseline

The detector maintains a rolling average of RMS values from non-alert
audio to establish the noise floor. The second argument to `New` controls
how many entries this buffer holds – for example, with a live microphone polled every
2 seconds, `30` covers about a minute.

### Raw PCM bytes

If your source provides raw bytes rather than `[]int16`, decode first:

```go
func decodePCM(data []byte) []int16 {
    n := len(data) / 2
    samples := make([]int16, n)
    for i := range n {
        samples[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
    }
    return samples
}
```

### Window size

`detector.InputSize()` returns the ideal number of samples per call
(8 seconds worth at the configured sample rate)

> [!WARNING]
> The [LICENSE](LICENSE) only covers the actual code, not the test `.wav` audio files.