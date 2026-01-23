# BWE - Bandwidth Estimation for WebRTC

Go implementation of Google Congestion Control (GCC) receiver-side bandwidth estimation for WebRTC.

[![Go Reference](https://pkg.go.dev/badge/github.com/thesyncim/bwe.svg)](https://pkg.go.dev/github.com/thesyncim/bwe)
[![Go Report Card](https://goreportcard.com/badge/github.com/thesyncim/bwe)](https://goreportcard.com/report/github.com/thesyncim/bwe)

## Features

- **Delay-based estimation** with Kalman and Trendline filter options
- **Overuse detector** with adaptive threshold for congestion detection
- **AIMD rate controller** (Additive Increase Multiplicative Decrease)
- **REMB packet generation** and scheduling for sender feedback
- **Pion WebRTC interceptor** for seamless integration
- **Zero allocations** in steady-state packet processing (0 allocs/op)
- **Tested against Chrome** browser for real-world validation

## Architecture

BWE implements the receiver-side GCC algorithm as specified in the IETF draft:

```
                           BWE Pipeline
    +------------------------------------------------------------------+
    |                                                                  |
    |  RTP Packets                                                     |
    |      |                                                           |
    |      v                                                           |
    |  +-------------------+    +---------------+    +---------------+ |
    |  | InterArrival      |--->| Delay Filter  |--->| Overuse       | |
    |  | Calculator        |    | (Kalman or    |    | Detector      | |
    |  | (burst grouping)  |    | Trendline)    |    | (adaptive     | |
    |  +-------------------+    +---------------+    | threshold)    | |
    |                                                +-------+-------+ |
    |                                                        |         |
    |                                                        v         |
    |  +-------------------+    +---------------+    +---------------+ |
    |  | REMB Scheduler    |<---| Rate          |<---| Bandwidth     | |
    |  | (RTCP feedback)   |    | Controller    |    | Usage Signal  | |
    |  +-------------------+    | (AIMD)        |    +---------------+ |
    |          |                +---------------+                      |
    |          v                                                       |
    |    REMB Packet                                                   |
    |                                                                  |
    +------------------------------------------------------------------+
```

The library is organized into two layers:

1. **Core Library** (`pkg/bwe/`) - Standalone GCC implementation with no Pion dependencies. Use this if you need direct control or want to integrate with a different WebRTC stack.

2. **Pion Interceptor** (`pkg/bwe/interceptor/`) - Integrates with Pion WebRTC via the interceptor pattern. Handles RTP header extension parsing, packet observation, and REMB sending automatically.

## Installation

```bash
go get github.com/thesyncim/bwe
```

## Quick Start

### Option 1: Pion Interceptor (Recommended)

The interceptor handles everything automatically: extension parsing, timing extraction, and REMB sending.

```go
import (
    "github.com/pion/interceptor"
    "github.com/pion/webrtc/v4"
    bweint "github.com/thesyncim/bwe/pkg/bwe/interceptor"
)

func setupPeerConnection() (*webrtc.PeerConnection, error) {
    // Create media engine with abs-send-time extension
    m := &webrtc.MediaEngine{}
    if err := m.RegisterDefaultCodecs(); err != nil {
        return nil, err
    }

    // Create interceptor registry
    i := &interceptor.Registry{}

    // Add BWE interceptor factory
    factory, err := bweint.NewBWEInterceptorFactory(
        bweint.WithInitialBitrate(500_000),  // 500 kbps initial
        bweint.WithMinBitrate(100_000),      // 100 kbps minimum
        bweint.WithMaxBitrate(5_000_000),    // 5 Mbps maximum
    )
    if err != nil {
        return nil, err
    }
    i.Add(factory)

    // Create API with interceptors
    api := webrtc.NewAPI(
        webrtc.WithMediaEngine(m),
        webrtc.WithInterceptorRegistry(i),
    )

    return api.NewPeerConnection(webrtc.Configuration{})
}
```

### Option 2: Standalone Core Library

Use the core library directly when you need fine-grained control or integration with non-Pion stacks.

```go
import (
    "fmt"
    "time"
    "github.com/thesyncim/bwe/pkg/bwe"
)

func main() {
    // Create estimator with default configuration
    config := bwe.DefaultBandwidthEstimatorConfig()
    estimator := bwe.NewBandwidthEstimator(config, nil)

    // Process each received RTP packet
    for packet := range incomingPackets {
        estimate := estimator.OnPacket(bwe.PacketInfo{
            ArrivalTime: time.Now(),
            SendTime:    extractAbsSendTime(packet), // 24-bit fixed point
            Size:        len(packet),
            SSRC:        packet.SSRC,
        })
        fmt.Printf("Current estimate: %d bps\n", estimate)
    }
}
```

## Configuration

### Bandwidth Limits

```go
factory, _ := bweint.NewBWEInterceptorFactory(
    bweint.WithInitialBitrate(500_000),   // Starting estimate (default: 300 kbps)
    bweint.WithMinBitrate(100_000),       // Floor (default: 10 kbps)
    bweint.WithMaxBitrate(10_000_000),    // Ceiling (default: 50 Mbps)
)
```

### REMB Interval

```go
factory, _ := bweint.NewBWEInterceptorFactory(
    bweint.WithFactoryREMBInterval(500 * time.Millisecond), // Send REMB 2x/sec
)
```

### REMB Callback

```go
factory, _ := bweint.NewBWEInterceptorFactory(
    bweint.WithFactoryOnREMB(func(bitrate float32, ssrcs []uint32) {
        log.Printf("REMB sent: %.0f bps for SSRCs %v", bitrate, ssrcs)
    }),
)
```

### Delay Filter Selection

The core library supports two filter types:

```go
config := bwe.DefaultBandwidthEstimatorConfig()

// Kalman filter (traditional GCC approach)
config.DelayConfig.FilterType = bwe.FilterKalman

// Trendline filter (modern WebRTC implementation)
config.DelayConfig.FilterType = bwe.FilterTrendline

estimator := bwe.NewBandwidthEstimator(config, nil)
```

## How It Works

BWE implements receiver-side bandwidth estimation using the Google Congestion Control algorithm:

1. **Inter-Arrival Calculator** - Groups packets into bursts based on arrival time clustering. This handles the bursty nature of video traffic where multiple packets arrive nearly simultaneously.

2. **Delay Filter** - Smooths noisy delay measurements using either:
   - **Kalman filter**: Traditional approach from the original GCC specification
   - **Trendline filter**: Modern approach using linear regression over a sliding window

3. **Overuse Detector** - Detects congestion by comparing filtered delay estimates against an adaptive threshold. Outputs three states: Normal, Underusing, or Overusing.

4. **Rate Controller** - Applies AIMD (Additive Increase Multiplicative Decrease) to adjust the bandwidth estimate based on the congestion signal.

5. **REMB Scheduler** - Sends Receiver Estimated Maximum Bitrate feedback to the sender via RTCP, allowing the sender to adjust its encoding bitrate.

## Performance

BWE is designed for high-performance real-time applications:

- **Zero allocations** in steady-state packet processing (0 allocs/op verified)
- **Lock-free** extension ID discovery (atomic operations)
- **Pooled** packet info objects in the interceptor layer
- **Efficient** sliding window implementations

Run benchmarks to verify:

```bash
go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...
```

Expected output:

```
BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc-8   1000000   1050 ns/op   0 B/op   0 allocs/op
BenchmarkDelayEstimator_OnPacket_ZeroAlloc-8      2000000    650 ns/op   0 B/op   0 allocs/op
```

## Testing

```bash
# Unit tests
go test ./pkg/bwe/...

# E2E tests (requires Chrome)
go test -tags=e2e ./e2e/...

# Benchmarks
go test -bench=. -benchmem ./pkg/bwe/...

# Soak test (24-hour simulation)
go test -v -run TestSoak ./pkg/bwe/...
```

## Requirements

- **Go 1.25+**
- **RTP Header Extension**: Sender must include `abs-send-time` or `abs-capture-time` extension
- **For Pion integration**: `pion/webrtc/v4`, `pion/interceptor`

### Enabling abs-send-time in Pion

Register the extension with your MediaEngine for SDP negotiation:

```go
import "github.com/pion/sdp/v3"

m := &webrtc.MediaEngine{}
m.RegisterDefaultCodecs()
m.RegisterHeaderExtension(
    webrtc.RTPHeaderExtensionCapability{URI: sdp.ABSSendTimeURI},
    webrtc.RTPCodecTypeVideo,
)
```

## API Reference

### Core Types

| Type | Description |
|------|-------------|
| `BandwidthEstimator` | Main entry point combining all components |
| `DelayEstimator` | Delay-based congestion detection pipeline |
| `RateController` | AIMD rate control algorithm |
| `REMBScheduler` | REMB packet generation and timing |
| `PacketInfo` | Input packet metadata (arrival time, send time, size, SSRC) |

### Interceptor Types

| Type | Description |
|------|-------------|
| `BWEInterceptorFactory` | Creates interceptors for Pion registry |
| `BWEInterceptor` | Pion interceptor implementation |

### Key Methods

```go
// Create estimator
estimator := bwe.NewBandwidthEstimator(config, clock)

// Process packet (returns current estimate in bps)
estimate := estimator.OnPacket(packetInfo)

// Query state
estimate := estimator.GetEstimate()
state := estimator.GetCongestionState()  // Normal, Underusing, Overusing
rate, ok := estimator.GetIncomingRate()

// Reset state (e.g., after stream switch)
estimator.Reset()
```

## Related Resources

- [draft-ietf-rmcat-gcc](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc) - GCC algorithm specification
- [pion/webrtc](https://github.com/pion/webrtc) - Pure Go WebRTC implementation
- [pion/interceptor](https://github.com/pion/interceptor) - Pion interceptor framework

## Contributing

1. Fork the repository
2. Create your feature branch
3. Run tests: `go test ./...`
4. Run benchmarks to verify 0 allocs/op: `go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...`
5. Submit a pull request

## License

MIT License - see LICENSE file for details.
