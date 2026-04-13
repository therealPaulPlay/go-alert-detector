# Alert detector

Go library for detecting fire alarms, sirens, smoke detectors, police sirens,
and other warning and emergency sounds in PCM audio streams.

Uses a rule-based approach for high performance, fine-tuned against ~100 audio
samples covering varied real-world conditions including distance, echo, and
background noise.

> [!WARNING]
> The [LICENSE](LICENSE) only covers the actual code, not the audio files inside `/testdata`.

## Usage

```go
import alertdetector "github.com/PaulPlay/go-alert-detector"

detector := alertdetector.New(48000) // Sample rate in Hz

// Feed it mono 16-bit signed PCM samples as []int16
// Ordering etc. doesn't matter, Analyze is stateless
result := detector.Analyze(samples)
if result != nil {
    fmt.Printf("Alert: %s\n", result.MatchedRule)
    fmt.Printf("Metrics: %+v\n", result.Metrics)
}
```

On detection, the result contains the matched rule name and all computed
signal metrics (spectral purity, tonality, oscillation patterns, etc).

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