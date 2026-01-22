# Stack Research: GCC Receiver-Side BWE

**Project:** Go port of libwebrtc's GCC delay-based receiver-side bandwidth estimator
**Researched:** 2026-01-22
**Focus:** Pure Go implementation generating REMB feedback for Pion WebRTC interop

## Executive Summary

Building a receiver-side GCC bandwidth estimator in Go requires implementing the classic REMB-GCC algorithm (Kalman filter-based delay gradient estimation) since Pion's existing GCC implementation is **sender-side only** using TWCC. This is a greenfield implementation with strong reference material from libwebrtc but no existing Go receiver-side REMB implementation to fork.

---

## Recommended Stack

### Core Runtime

| Technology | Version | Purpose | Confidence |
|------------|---------|---------|------------|
| Go | 1.25.6+ | Runtime with Green Tea GC benefits | HIGH |
| `go.mod` | `go 1.25` | Minimum version requirement | HIGH |

**Rationale:** Go 1.25 provides the experimental Green Tea garbage collector (10-40% GC overhead reduction) which benefits high-packet-rate processing. The project already uses Go 1.25. The go1.25.6 patch (released 2026-01-15) includes important security fixes.

### Pion Ecosystem (Required)

| Package | Version | Purpose | Confidence |
|---------|---------|---------|------------|
| `github.com/pion/rtp` | v1.10.0 | RTP packet parsing, `AbsCaptureTimeExtension` | HIGH |
| `github.com/pion/rtcp` | v1.2.16 | REMB packet generation (`ReceiverEstimatedMaximumBitrate`) | HIGH |
| `github.com/pion/interceptor` | v0.1.43 | Interceptor interface for Pion integration | HIGH |
| `github.com/pion/webrtc/v4` | v4.2.3 | WebRTC integration (if needed for testing) | HIGH |
| `github.com/pion/logging` | latest | Consistent logging with Pion ecosystem | MEDIUM |

**Rationale:**
- `pion/rtp` provides `AbsCaptureTimeExtension` struct with `CaptureTime()` method for extracting absolute capture timestamps from RTP packets - essential for inter-arrival time analysis.
- `pion/rtcp` provides `ReceiverEstimatedMaximumBitrate` struct for generating REMB packets with proper marshaling.
- `pion/interceptor` defines the `Interceptor` interface your implementation must satisfy to plug into Pion WebRTC.

**Source:** [pion/rtp docs](https://pkg.go.dev/github.com/pion/rtp), [pion/rtcp docs](https://pkg.go.dev/github.com/pion/rtcp), [pion/interceptor docs](https://pkg.go.dev/github.com/pion/interceptor)

### Signal Processing / Math

| Package | Version | Purpose | Confidence |
|---------|---------|---------|------------|
| Standard library only | - | Basic Kalman filter math | HIGH |
| `gonum.org/v1/gonum/mat` | v0.17.0 | Matrix operations (optional, if needed) | MEDIUM |

**Rationale:**
- The GCC Kalman filter is a **simple scalar Kalman filter** (1D state), not requiring full matrix libraries. The libwebrtc implementation uses simple scalar operations.
- Pure Go implementation is preferred per project constraints (no CGO).
- If matrix operations become necessary for extended filtering, gonum/mat is the standard Go choice.
- **Recommendation:** Start with standard library; add gonum only if matrix operations prove necessary.

**Source:** [gonum docs](https://pkg.go.dev/gonum.org/v1/gonum)

### Performance Optimization

| Pattern/Package | Purpose | Confidence |
|-----------------|---------|------------|
| `sync.Pool` | Buffer pooling for high packet rates | HIGH |
| `atomic` operations | Lock-free state updates | HIGH |
| Pre-allocated slices | Reduce allocations in hot paths | HIGH |

**Rationale:**
- `sync.Pool` provides 50%+ throughput improvement for packet processing (benchmarks show 150k to 230k packets/second improvement).
- GCC algorithm state (Kalman filter state, arrival groups) should use atomic operations where possible.
- Pre-warm pools during initialization to avoid first-packet latency spikes.

**Source:** [Go sync.Pool mechanics](https://victoriametrics.com/blog/go-sync-pool/)

---

## Go Libraries Detail

### Pion RTP - Header Extension Parsing

```go
import "github.com/pion/rtp"

// Parse absolute capture time from RTP packet
func extractCaptureTime(packet *rtp.Packet, extensionID uint8) (time.Time, bool) {
    ext := packet.GetExtension(extensionID)
    if ext == nil {
        return time.Time{}, false
    }

    var absCaptureTime rtp.AbsCaptureTimeExtension
    if err := absCaptureTime.Unmarshal(ext); err != nil {
        return time.Time{}, false
    }

    return absCaptureTime.CaptureTime(), true
}
```

### Pion RTCP - REMB Generation

```go
import "github.com/pion/rtcp"

// Generate REMB packet
func createREMB(senderSSRC uint32, bitrate float32, mediaSSRCs []uint32) *rtcp.ReceiverEstimatedMaximumBitrate {
    return &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: senderSSRC,
        Bitrate:    bitrate,
        SSRCs:      mediaSSRCs,
    }
}
```

### Pion Interceptor - Integration Interface

```go
import "github.com/pion/interceptor"

// Your BWE must implement this interface
type Interceptor interface {
    BindRTCPReader(reader RTCPReader) RTCPReader
    BindRTCPWriter(writer RTCPWriter) RTCPWriter
    BindLocalStream(info *StreamInfo, writer RTPWriter) RTPWriter
    BindRemoteStream(info *StreamInfo, reader RTPReader) RTPReader
    UnbindLocalStream(info *StreamInfo)
    UnbindRemoteStream(info *StreamInfo)
    Close() error
}
```

---

## Reference Implementations

### Primary: libwebrtc (Chromium)

| Component | Source Path | Purpose |
|-----------|-------------|---------|
| InterArrival | `modules/remote_bitrate_estimator/inter_arrival.cc` | Packet grouping, inter-arrival delta calculation |
| OveruseDetector | `modules/remote_bitrate_estimator/overuse_detector.cc` | Overuse/underuse signal generation |
| AimdRateControl | `modules/remote_bitrate_estimator/aimd_rate_control.cc` | AIMD rate adaptation |
| RemoteBitrateEstimatorAbsSendTime | `modules/remote_bitrate_estimator/remote_bitrate_estimator_abs_send_time.cc` | Main receiver-side BWE class |
| Kalman Filter | Embedded in overuse_detector.cc | Delay gradient estimation |

**Source:** [WebRTC googlesource](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/remote_bitrate_estimator/)

### Secondary: aiortc (Python)

| Component | Source Path | Purpose |
|-----------|-------------|---------|
| RemoteBitrateEstimator | `src/aiortc/rate.py` | Simpler Python implementation |
| RTCRtpReceiver | `src/aiortc/rtcrtpreceiver.py` | REMB integration pattern |

**Why useful:** aiortc's implementation is simpler and more readable than libwebrtc. Good for understanding algorithm flow before diving into C++ complexity.

**Source:** [aiortc GitHub](https://github.com/aiortc/aiortc)

### Existing Pion GCC (Sender-Side Reference)

| Component | Source Path | Purpose |
|-----------|-------------|---------|
| delay_based_bwe.go | `pkg/gcc/delay_based_bwe.go` | Delay estimation patterns in Go |
| kalman.go | `pkg/gcc/kalman.go` | Kalman filter Go implementation |
| overuse_detector.go | `pkg/gcc/overuse_detector.go` | Overuse detection in Go |
| arrival_group_accumulator.go | `pkg/gcc/arrival_group_accumulator.go` | Packet grouping in Go |

**Why useful:** Although sender-side, the core algorithms (Kalman filter, overuse detection, AIMD) are similar. These files show idiomatic Go patterns for GCC components.

**Source:** [pion/interceptor/pkg/gcc](https://github.com/pion/interceptor/tree/master/pkg/gcc)

---

## What NOT to Use

### DO NOT: Use Pion's existing GCC package directly

**Why:** `github.com/pion/interceptor/pkg/gcc` is **sender-side only**. It processes TWCC feedback to estimate bandwidth. It does NOT generate REMB feedback. The `SendSideBWE` type name makes this explicit.

**What to do instead:** Implement a new receiver-side interceptor that generates REMB.

### DO NOT: Use external Kalman filter libraries

**Why:**
1. GCC uses a simple scalar Kalman filter (1D state) - no matrix operations needed
2. External libraries add dependencies for trivial math
3. libwebrtc's Kalman filter is ~50 lines of code
4. Pure Go constraint is easier to satisfy with inline implementation

**What to do instead:** Port the Kalman filter from `pion/interceptor/pkg/gcc/kalman.go` or libwebrtc directly.

### DO NOT: Use TWCC instead of REMB

**Why:** Project requirement is interop with systems expecting REMB-based congestion control. TWCC is a different feedback mechanism (sender-side estimation vs receiver-side).

**When TWCC would be appropriate:** If you control both sender and receiver, TWCC is the modern approach. But for Chrome/libwebrtc receiver interop expecting REMB, you must generate REMB.

### DO NOT: Use CGO or C bindings to libwebrtc

**Why:**
1. Project constraint: pure Go (no CGO)
2. CGO adds build complexity and cross-compilation issues
3. libwebrtc is massive; binding just the BWE components is impractical

### DO NOT: Use `pion/rtp/v2`

**Why:** v2.0.0 is from July 2021 and appears unmaintained. The v1.x line (currently v1.10.0, updated Jan 2026) is actively maintained and has `AbsCaptureTimeExtension`.

**Source:** [pion/rtp v2 docs show 2021 date](https://pkg.go.dev/github.com/pion/rtp/v2)

### DO NOT: Pre-optimize with complex concurrency

**Why:** Profile first. The algorithm is computationally simple (a few multiplications per packet). Bottlenecks are more likely in I/O or memory allocation than CPU.

**What to do instead:** Use `sync.Pool` for buffers, keep hot paths allocation-free, profile under load.

---

## Installation

```bash
# Core Pion packages
go get github.com/pion/rtp@v1.10.0
go get github.com/pion/rtcp@v1.2.16
go get github.com/pion/interceptor@v0.1.43

# For WebRTC integration testing
go get github.com/pion/webrtc/v4@v4.2.3

# Optional: If matrix operations needed later
go get gonum.org/v1/gonum@v0.17.0
```

### go.mod

```go
module multicodecsimulcast

go 1.25

require (
    github.com/pion/rtp v1.10.0
    github.com/pion/rtcp v1.2.16
    github.com/pion/interceptor v0.1.43
    github.com/pion/webrtc/v4 v4.2.3
)
```

---

## Confidence Levels Summary

| Recommendation | Confidence | Reasoning |
|----------------|------------|-----------|
| Pion ecosystem (rtp, rtcp, interceptor) | HIGH | Official docs, actively maintained, version verified |
| Go 1.25+ | HIGH | go.dev release notes, current stable |
| No external Kalman library | HIGH | Algorithm analysis, libwebrtc reference |
| Pure Go implementation | HIGH | Project constraint, existing Pion patterns |
| sync.Pool for performance | HIGH | Documented benchmarks, standard pattern |
| gonum (optional) | MEDIUM | May not be needed; standard library likely sufficient |
| libwebrtc as reference | HIGH | Canonical implementation, well-documented |
| aiortc as secondary reference | MEDIUM | Simpler but Python-specific patterns |

---

## Key Insight: Receiver-Side REMB is a Gap

Pion's current architecture focuses on **sender-side** congestion control using TWCC. There is no existing **receiver-side REMB interceptor** in the Pion ecosystem. This project fills that gap.

The implementation will:
1. Observe incoming RTP packets via `BindRemoteStream`
2. Extract `AbsCaptureTimeExtension` timestamps
3. Run GCC delay-based estimation (inter-arrival analysis, Kalman filter, overuse detection)
4. Generate REMB packets via `BindRTCPWriter`

This matches the classic REMB-GCC architecture from libwebrtc pre-2017 when receiver-side estimation was standard.

---

## Sources

- [Pion RTP Package](https://pkg.go.dev/github.com/pion/rtp) - v1.10.0, Jan 2026
- [Pion RTCP Package](https://pkg.go.dev/github.com/pion/rtcp) - v1.2.16, Oct 2025
- [Pion Interceptor Package](https://pkg.go.dev/github.com/pion/interceptor) - v0.1.43, Jan 2026
- [Pion Interceptor GCC](https://pkg.go.dev/github.com/pion/interceptor/pkg/gcc) - Sender-side reference
- [Go 1.25 Release Notes](https://go.dev/doc/go1.25)
- [WebRTC remote_bitrate_estimator](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/remote_bitrate_estimator/)
- [GCC Algorithm Analysis (C3Lab)](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf)
- [Pion Bandwidth Estimator Issue #25](https://github.com/pion/interceptor/issues/25)
- [aiortc Python Implementation](https://github.com/aiortc/aiortc)
- [Gonum Package](https://pkg.go.dev/gonum.org/v1/gonum) - v0.17.0, Dec 2025
