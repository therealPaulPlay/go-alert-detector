# Alert detector

Go library for detecting fire alarms, sirens, house alarms, smoke detectors 
and other warning and emergency sounds in PCM audio streams.

Uses a rule-based approach for high performance, fine-tuned against ~200 audio
samples covering varied real-world conditions including distance, echo, and
background noise.

> [!WARNING]
> The [LICENSE](LICENSE) only covers the actual code, not the audio files inside `/testdata`.

## Usage

```go
import alertdetector "github.com/therealPaulPlay/go-alert-detector"

detector := alertdetector.New(48000) // Sample rate in Hz

// Pass a buffer of mono 16-bit signed PCM samples
// Analyze is stateless, ordering between calls doesn't matter
result := detector.Analyze(buffer)
if result != nil {
    fmt.Printf("Alert detected\n")
    fmt.Printf("Metrics: %+v\n", result.Metrics)
}
```

On detection, the result contains all computed signal metrics used by the
rules.

### Raw PCM bytes

If your source provides raw bytes rather than `[]int16`, decode first:

```go
func decodePCM(data []byte) []int16 {
    n := len(data) / 2
    buffer := make([]int16, n)
    for i := range n {
        buffer[i] = int16(binary.LittleEndian.Uint16(data[i*2:]))
    }
    return buffer
}
```

### Window size

The detector works on audio buffers of varying length, but metrics are most
stable when the buffer covers at least one full siren sweep cycle (~8 seconds).
Feeding 8-20 second buffers is recommended.