# Phase 2: Rate Control & REMB - Research

**Researched:** 2026-01-22
**Domain:** AIMD rate control, REMB packet generation, sliding window bitrate measurement
**Confidence:** HIGH

## Summary

Phase 2 builds on Phase 1's congestion signals (BwNormal/BwOverusing/BwUnderusing) to produce bandwidth estimates and REMB feedback. The core components are: (1) a sliding window bitrate measurement to track incoming rate, (2) an AIMD rate controller state machine that adjusts estimates based on congestion signals, and (3) REMB packet generation with mantissa+exponent encoding.

The AIMD algorithm follows the GCC specification: multiplicative decrease of 0.85x the measured incoming rate on overuse, additive increase during normal/underuse based on frame rate and RTT. The three states (Increase/Decrease/Hold) transition based on congestion signals: overuse triggers decrease from any state, normal moves hold->increase or decrease->hold, underuse moves to hold.

Pion's `pion/rtcp` package already implements REMB packet encoding/decoding with correct mantissa+exponent format. For multi-SSRC aggregation, receiver-side estimation computes a single bandwidth estimate for the entire session (not per-SSRC), and REMB packets list all affected SSRCs.

**Primary recommendation:** Use Pion's existing `rtcp.ReceiverEstimatedMaximumBitrate` for REMB encoding, implement the AIMD rate controller as a standalone component following libwebrtc's aimd_rate_control.cc patterns, and build a simple sliding window rate calculator modeled on libwebrtc's RateStatistics.

## Standard Stack

The established libraries/tools for this domain:

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/pion/rtcp` | Latest | REMB packet encoding/decoding | Already implements spec-compliant mantissa+exponent encoding |
| Standard library | Go 1.25 | Rate calculation, state machine | No external dependencies needed |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.9+ | Test assertions | Unit testing rate controller |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Pion's REMB | Custom encoding | Pion already has correct implementation, no reason to re-implement |
| Custom rate stats | External moving average lib | Simple sliding window is easy to implement, no dependency needed |

**Installation:**
```bash
go get github.com/pion/rtcp
```

## Architecture Patterns

### Recommended Project Structure
```
pkg/
├── bwe/
│   ├── types.go           # Existing - BandwidthUsage, PacketInfo
│   ├── estimator.go       # Existing - DelayEstimator
│   ├── rate_stats.go      # NEW: Sliding window bitrate measurement
│   ├── rate_controller.go # NEW: AIMD state machine
│   ├── remb.go            # NEW: REMB packet builder
│   └── bandwidth_estimator.go # NEW: Full Estimator API combining all
└── bwe/internal/
    └── clock.go           # Existing - Monotonic clock
```

### Pattern 1: AIMD State Machine
**What:** Three-state finite state machine (Increase/Decrease/Hold) for rate control
**When to use:** Converting congestion signals to bandwidth estimates
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-02 Section 6, libwebrtc aimd_rate_control.cc
type RateControlState int

const (
    RateHold RateControlState = iota
    RateIncrease
    RateDecrease
)

// State transition table from GCC spec:
// Signal \ State | Hold     | Increase | Decrease
// Over-use      | Decrease | Decrease | (stay)
// Normal        | Increase | (stay)   | Hold
// Under-use     | (stay)   | Hold     | Hold

func (s RateControlState) Transition(signal BandwidthUsage) RateControlState {
    switch signal {
    case BwOverusing:
        if s != RateDecrease {
            return RateDecrease
        }
    case BwNormal:
        switch s {
        case RateHold:
            return RateIncrease
        case RateDecrease:
            return RateHold
        }
    case BwUnderusing:
        if s == RateIncrease || s == RateDecrease {
            return RateHold
        }
    }
    return s // Stay in current state
}
```

### Pattern 2: Multiplicative Decrease
**What:** Reduce estimate to 85% of measured incoming rate on overuse
**When to use:** When overuse signal is received
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-02 Section 6.2, libwebrtc aimd_rate_control.cc
const DefaultBeta = 0.85 // Multiplicative decrease factor

func (c *RateController) applyDecrease(incomingRate int64) int64 {
    // A_hat(i) = beta * R_hat(i)
    // Where R_hat(i) is measured incoming bitrate
    newRate := int64(float64(incomingRate) * c.beta)

    // Ensure we don't go below minimum
    if newRate < c.minBitrate {
        newRate = c.minBitrate
    }
    return newRate
}
```

### Pattern 3: Additive Increase
**What:** Gradually increase rate when network is not congested
**When to use:** In Increase state, growing rate toward available capacity
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-02 Section 6.1, libwebrtc aimd_rate_control.cc
func (c *RateController) applyIncrease(elapsed time.Duration) int64 {
    elapsedSec := elapsed.Seconds()

    // Near-max additive increase (within convergence region)
    // Based on expected packet size and response time
    if c.nearMaxRegion() {
        // alpha = 0.5 * min(time_since_last / response_time, 1.0)
        // increase = max(1000, alpha * expected_packet_bits)
        responseTime := 100*time.Millisecond + c.rtt
        alpha := 0.5 * math.Min(elapsedSec/responseTime.Seconds(), 1.0)
        expectedBits := float64(c.currentRate) / 30.0 // ~30 fps assumption
        increase := int64(math.Max(1000, alpha*expectedBits))
        return c.currentRate + increase
    }

    // Multiplicative increase (far from convergence)
    // eta = 1.08^min(elapsed_sec, 1.0)
    // A_hat = eta * A_hat_prev
    eta := math.Pow(1.08, math.Min(elapsedSec, 1.0))
    return int64(eta * float64(c.currentRate))
}
```

### Pattern 4: Sliding Window Rate Measurement
**What:** Track incoming bitrate over a configurable time window
**When to use:** Measuring actual received rate for AIMD calculations
**Example:**
```go
// Source: libwebrtc rtc_base/rate_statistics.cc
type RateStats struct {
    windowSize   time.Duration
    buckets      []bucket
    totalBytes   int64
    oldestTime   time.Time
}

type bucket struct {
    timestamp time.Time
    bytes     int64
}

func (r *RateStats) Update(bytes int64, now time.Time) {
    // Add new sample
    r.buckets = append(r.buckets, bucket{now, bytes})
    r.totalBytes += bytes

    // Remove expired samples
    cutoff := now.Add(-r.windowSize)
    for len(r.buckets) > 0 && r.buckets[0].timestamp.Before(cutoff) {
        r.totalBytes -= r.buckets[0].bytes
        r.buckets = r.buckets[1:]
    }
    if len(r.buckets) > 0 {
        r.oldestTime = r.buckets[0].timestamp
    }
}

func (r *RateStats) Rate(now time.Time) (bitsPerSec int64, ok bool) {
    if len(r.buckets) == 0 {
        return 0, false
    }
    elapsed := now.Sub(r.oldestTime)
    if elapsed <= time.Millisecond {
        return 0, false
    }
    // bits per second = (total_bytes * 8) / elapsed_seconds
    return int64(float64(r.totalBytes*8) / elapsed.Seconds()), true
}
```

### Pattern 5: REMB Mantissa+Exponent Encoding
**What:** Encode bitrate as 6-bit exponent + 18-bit mantissa
**When to use:** Building REMB packets for sender feedback
**Example:**
```go
// Source: draft-alvestrand-rmcat-remb-03, pion/rtcp receiver_estimated_maximum_bitrate.go
// Note: Use pion/rtcp.ReceiverEstimatedMaximumBitrate instead of hand-rolling

import "github.com/pion/rtcp"

func buildREMB(senderSSRC uint32, bitrate uint64, mediaSSRCs []uint32) []byte {
    remb := &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: senderSSRC,
        Bitrate:    float32(bitrate),
        SSRCs:      mediaSSRCs,
    }
    data, _ := remb.Marshal()
    return data
}
```

### Anti-Patterns to Avoid
- **Per-SSRC rate estimates:** Receiver-side estimation produces ONE estimate for the entire session, not per-SSRC
- **Static decrease factor:** The 0.85 factor is configurable via WebRTC field trials; expose as configuration
- **Ignoring incoming rate:** Decrease is based on measured incoming rate, NOT current estimate
- **Immediate increase after decrease:** Must go through Hold state first
- **Unbounded estimate:** Must enforce `A_hat < 1.5 * R_hat` to prevent divergence

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| REMB packet encoding | Custom mantissa/exponent | `pion/rtcp.ReceiverEstimatedMaximumBitrate` | Already handles edge cases, validated with browsers |
| REMB packet parsing | Custom parsing | `pion/rtcp.Unmarshal` | Correctly validates packet structure |
| Exponential moving average | Custom EMA | Simple formula inline | `ema = alpha*new + (1-alpha)*old` is trivial |

**Key insight:** The REMB packet format is tricky with its mantissa+exponent encoding. Pion's implementation is battle-tested and matches Chrome's expectations. The rate controller logic, however, is specific to our delay-based receiver-side approach and should be implemented fresh.

## Common Pitfalls

### Pitfall 1: Decreasing from Estimate Instead of Measured Rate
**What goes wrong:** Using `newRate = 0.85 * currentEstimate` instead of `newRate = 0.85 * measuredIncomingRate`
**Why it happens:** Confusion about what to multiply by 0.85
**How to avoid:** The spec is clear: "A_hat(i) = 0.85 * R_hat(i)" where R_hat is the measured incoming bitrate from the sliding window
**Warning signs:** Estimate decreases too slowly during congestion, never reaches actual sending rate

### Pitfall 2: Missing Hold State
**What goes wrong:** Transitioning directly from Decrease to Increase on Normal signal
**Why it happens:** Simplifying the state machine
**How to avoid:** Follow the exact state transition table. Normal signal from Decrease goes to Hold, not Increase
**Warning signs:** Oscillating bandwidth, unstable video quality

### Pitfall 3: Unbounded Estimate Growth
**What goes wrong:** Estimate grows far beyond actual sending rate during underuse
**Why it happens:** No upper bound on multiplicative increase
**How to avoid:** Enforce `A_hat < 1.5 * R_hat` - estimate cannot exceed 1.5x measured incoming rate
**Warning signs:** Estimate is much higher than actual received bitrate, REMB values unrealistic

### Pitfall 4: REMB Overflow in Mantissa Encoding
**What goes wrong:** Bitrate values that overflow the 18-bit mantissa cause incorrect encoding
**Why it happens:** Not shifting mantissa right and incrementing exponent
**How to avoid:** Use Pion's implementation which handles this correctly
**Warning signs:** Very high bitrates (>1 Gbps) encode incorrectly

### Pitfall 5: Stale Incoming Rate Measurement
**What goes wrong:** Sliding window contains old data after network pause, causing incorrect decrease
**Why it happens:** Not clearing window on stream reset or large gaps
**How to avoid:** Detect gaps (>2s since last packet) and reset rate measurement
**Warning signs:** Incorrect rate after resuming paused stream

### Pitfall 6: REMB Timing Jitter
**What goes wrong:** REMB packets sent erratically, confusing sender
**Why it happens:** Using wall clock or not accounting for processing time
**How to avoid:** Use a ticker for regular REMB interval (default 1Hz), send immediately on significant decrease (>=3%)
**Warning signs:** Sender bitrate oscillates, not responding smoothly to REMB

## Code Examples

Verified patterns from official sources:

### REMB Packet Using Pion
```go
// Source: https://github.com/pion/rtcp/blob/master/receiver_estimated_maximum_bitrate.go
import "github.com/pion/rtcp"

func createREMB(bitrateBps uint64, ssrcs []uint32) ([]byte, error) {
    pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: 0, // Will be set by transport layer
        Bitrate:    float32(bitrateBps),
        SSRCs:      ssrcs,
    }
    return pkt.Marshal()
}

// REMB packet structure:
// - Header: V=2, P=0, FMT=15, PT=206
// - Sender SSRC: 4 bytes
// - Media source SSRC: 4 bytes (always 0)
// - "REMB" identifier: 4 bytes
// - Num SSRC (8 bits) + BR Exp (6 bits) + BR Mantissa (18 bits)
// - SSRC list: 4 bytes each
```

### Complete AIMD Rate Controller Structure
```go
// Source: draft-ietf-rmcat-gcc-02, libwebrtc aimd_rate_control.cc
type RateControllerConfig struct {
    MinBitrate      int64         // Minimum allowed bitrate (default: 10kbps)
    MaxBitrate      int64         // Maximum allowed bitrate (default: 30Mbps)
    InitialBitrate  int64         // Starting bitrate (default: 300kbps)
    Beta            float64       // Multiplicative decrease factor (default: 0.85)
}

func DefaultRateControllerConfig() RateControllerConfig {
    return RateControllerConfig{
        MinBitrate:     10_000,       // 10 kbps
        MaxBitrate:     30_000_000,   // 30 Mbps
        InitialBitrate: 300_000,      // 300 kbps
        Beta:           0.85,
    }
}

type RateController struct {
    config        RateControllerConfig
    state         RateControlState
    currentRate   int64
    lastUpdate    time.Time
    rtt           time.Duration

    // For near-max detection (EMA of decrease rates)
    avgMaxBitrate   float64
    varMaxBitrate   float64
}

func (c *RateController) Update(signal BandwidthUsage, incomingRate int64, now time.Time) int64 {
    c.state = c.state.Transition(signal)
    elapsed := now.Sub(c.lastUpdate)
    c.lastUpdate = now

    switch c.state {
    case RateDecrease:
        // A_hat(i) = beta * R_hat(i)
        c.currentRate = int64(c.config.Beta * float64(incomingRate))
        c.updateAvgMax(c.currentRate) // Track for near-max detection

    case RateIncrease:
        c.currentRate = c.applyIncrease(elapsed)

    case RateHold:
        // No change to rate
    }

    // Enforce bounds
    c.currentRate = clamp(c.currentRate, c.config.MinBitrate, c.config.MaxBitrate)

    // Enforce ratio constraint: A_hat < 1.5 * R_hat
    maxAllowed := int64(1.5 * float64(incomingRate))
    if c.currentRate > maxAllowed && incomingRate > 0 {
        c.currentRate = maxAllowed
    }

    return c.currentRate
}
```

### Multi-SSRC Aggregation
```go
// Source: REMB semantics - single estimate for entire session
type BandwidthEstimator struct {
    delayEstimator  *DelayEstimator  // From Phase 1
    rateController  *RateController
    rateStats       *RateStats
    ssrcs           map[uint32]struct{} // Track seen SSRCs

    estimate        int64
    lastREMBTime    time.Time
    lastREMBValue   int64
}

func (e *BandwidthEstimator) OnPacket(pkt PacketInfo) {
    // Track SSRC for REMB
    e.ssrcs[pkt.SSRC] = struct{}{}

    // Update incoming rate measurement (all SSRCs combined)
    e.rateStats.Update(int64(pkt.Size), pkt.ArrivalTime)

    // Process through delay estimator
    signal := e.delayEstimator.OnPacket(pkt)

    // Update rate controller
    incomingRate, ok := e.rateStats.Rate(pkt.ArrivalTime)
    if ok {
        e.estimate = e.rateController.Update(signal, incomingRate, pkt.ArrivalTime)
    }
}

func (e *BandwidthEstimator) GetEstimate() int64 {
    return e.estimate
}

func (e *BandwidthEstimator) GetSSRCs() []uint32 {
    result := make([]uint32, 0, len(e.ssrcs))
    for ssrc := range e.ssrcs {
        result = append(result, ssrc)
    }
    return result
}
```

### REMB Timing with Immediate Decrease
```go
// Source: REMB-03, REMB-04 requirements
type REMBScheduler struct {
    interval      time.Duration // Default 1 second
    threshold     float64       // Significant decrease threshold (3%)
    lastSent      time.Time
    lastValue     int64
    writer        RTCPWriter
}

func (s *REMBScheduler) MaybeSendREMB(estimate int64, ssrcs []uint32, now time.Time) bool {
    // Check for immediate decrease trigger
    if s.lastValue > 0 {
        decrease := float64(s.lastValue-estimate) / float64(s.lastValue)
        if decrease >= s.threshold {
            // Significant decrease - send immediately
            s.sendREMB(estimate, ssrcs, now)
            return true
        }
    }

    // Check for regular interval
    if now.Sub(s.lastSent) >= s.interval {
        s.sendREMB(estimate, ssrcs, now)
        return true
    }

    return false
}

func (s *REMBScheduler) sendREMB(estimate int64, ssrcs []uint32, now time.Time) {
    pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
        Bitrate: float32(estimate),
        SSRCs:   ssrcs,
    }
    data, _ := pkt.Marshal()
    s.writer.WriteRTCP(data)
    s.lastSent = now
    s.lastValue = estimate
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Receiver-side only (REMB) | Sender-side (TWCC) | WebRTC ~2017 | REMB still needed for interop with older systems |
| Fixed 0.85 decrease | Configurable via field trials | Recent libwebrtc | Allow tuning for specific use cases |
| Single media type | Separate audio/video detectors | Recent libwebrtc | Better handling of mixed streams |

**Deprecated/outdated:**
- **TMMBR/TMMBN:** Replaced by REMB for receiver-estimated bandwidth
- **Receiver-side as default:** Chrome now defaults to sender-side TWCC, but REMB still supported for interop

## Open Questions

Things that couldn't be fully resolved:

1. **Exact near-max region detection**
   - What we know: libwebrtc uses EMA of past decrease rates with variance tracking
   - What's unclear: Exact alpha values and standard deviation multiplier
   - Recommendation: Start with simple approach (always use additive increase), add complexity if needed

2. **REMB immediate decrease threshold**
   - What we know: Requirement says >=3%, but couldn't find this in libwebrtc
   - What's unclear: Whether Chrome actually uses this threshold
   - Recommendation: Implement 3% as specified, make configurable

3. **RTT estimation source**
   - What we know: Additive increase uses RTT for response time calculation
   - What's unclear: How to get RTT on receiver side (no RTCP SR/RR yet)
   - Recommendation: Use conservative default (150ms), allow external RTT injection

## Sources

### Primary (HIGH confidence)
- [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02) - AIMD rate control specification
- [draft-alvestrand-rmcat-remb-03](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb-03) - REMB packet format specification
- [pion/rtcp ReceiverEstimatedMaximumBitrate](https://github.com/pion/rtcp/blob/master/receiver_estimated_maximum_bitrate.go) - Go REMB implementation
- [libwebrtc aimd_rate_control.cc](https://chromium.googlesource.com/external/webrtc/+/refs/heads/master/modules/remote_bitrate_estimator/aimd_rate_control.cc) - Reference AIMD implementation

### Secondary (MEDIUM confidence)
- [libwebrtc rate_statistics.cc](https://chromium.googlesource.com/external/webrtc/+/master/rtc_base/rate_statistics.cc) - Sliding window rate measurement
- [pion/interceptor/pkg/gcc](https://github.com/pion/interceptor/blob/master/pkg/gcc/rate_controller.go) - Pion's sender-side rate controller reference
- [WebRTC for the Curious - Media Communication](https://webrtcforthecurious.com/docs/06-media-communication/) - REMB/TWCC overview

### Tertiary (LOW confidence)
- [webrtchacks GCC probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/) - Bandwidth probing context

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Pion RTCP well-documented, spec is clear
- Architecture: HIGH - GCC spec provides exact state machine and formulas
- Pitfalls: HIGH - Well-documented in spec and libwebrtc source
- REMB encoding: HIGH - Pion implementation verified
- Multi-SSRC: MEDIUM - Aggregation strategy clear but details sparse

**Research date:** 2026-01-22
**Valid until:** 2026-03-22 (60 days - stable specification, REMB deprecated but unchanging)
