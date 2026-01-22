# Phase 1: Foundation & Core Pipeline - Research

**Researched:** 2026-01-22
**Domain:** WebRTC GCC delay-based receiver-side bandwidth estimation
**Confidence:** HIGH

## Summary

Phase 1 implements the delay measurement, noise filtering, and congestion detection components of Google Congestion Control (GCC) for receiver-side bandwidth estimation. The implementation must handle two distinct timestamp formats (abs-send-time 24-bit and RTP 32-bit), perform packet burst grouping, apply Kalman or trendline filtering to estimate delay gradients, and detect overuse through an adaptive threshold state machine.

The core algorithm follows the IETF draft-ietf-rmcat-gcc specification: packets are grouped by 5ms burst threshold, inter-arrival delay variations are computed, noise is filtered via Kalman filter or trendline estimator, and the overuse detector produces Normal/Overusing/Underusing signals based on sustained threshold violations. Critical to correctness: the adaptive threshold with asymmetric coefficients (K_u=0.01, K_d=0.00018) prevents TCP starvation, and monotonic time must be used throughout to avoid clock drift issues.

Pion already has sender-side GCC in `pion/interceptor/pkg/gcc` which provides reference patterns for Go implementation. The standalone core library approach (no Pion dependency) allows testing the algorithm independently before integration.

**Primary recommendation:** Implement a clean Go port of the IETF draft specification, using Pion's existing `pion/rtp.AbsSendTimeExtension` for timestamp parsing, and following Pion's GCC package patterns for the Kalman filter and overuse detector.

## Standard Stack

The established libraries/tools for this domain:

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Standard library only | Go 1.25 | Core algorithm | No external dependencies needed for math/time operations |
| `github.com/pion/rtp` | Latest | AbsSendTimeExtension parsing | Already implements 24-bit 6.18 fixed-point parsing correctly |

### Supporting

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | v1.9+ | Test assertions | Unit testing with require/assert |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Custom Kalman | `github.com/konimarti/kalman` | External Kalman libraries are generic; GCC needs scalar Kalman with specific parameters - simpler to implement inline |
| Custom timestamp parsing | `pion/rtp.AbsSendTimeExtension` | Pion already has correct implementation; use it |

**Installation:**
```bash
go get github.com/pion/rtp
go get github.com/stretchr/testify
```

## Architecture Patterns

### Recommended Project Structure
```
pkg/
├── bwe/                    # Bandwidth estimation core
│   ├── types.go           # Core types, constants, interfaces
│   ├── timestamp.go       # Timestamp parsing and wraparound
│   ├── interarrival.go    # Inter-arrival calculator with burst grouping
│   ├── kalman.go          # Kalman filter delay estimator
│   ├── trendline.go       # Trendline estimator alternative
│   ├── overuse.go         # Overuse detector with adaptive threshold
│   └── estimator.go       # Main estimator orchestrating components
├── bwe/internal/          # Internal helpers
│   └── clock.go           # Monotonic time abstraction
└── bwe/testutil/          # Test utilities
    └── traces.go          # Synthetic packet trace generators
```

### Pattern 1: Packet Group Accumulator
**What:** Accumulate packets into groups based on 5ms burst threshold before processing
**When to use:** All incoming packets must be grouped before delay calculation
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-02 Section 4.1
type PacketGroup struct {
    FirstSendTime   int64     // First packet send timestamp
    LastSendTime    int64     // Last packet send timestamp
    FirstArriveTime time.Time // First packet arrival (monotonic)
    LastArriveTime  time.Time // Last packet arrival (monotonic)
    Size            int       // Total bytes in group
    NumPackets      int       // Packet count
}

const BurstThresholdMs = 5

func (g *PacketGroup) BelongsToBurst(sendTime int64, arriveTime time.Time) bool {
    arrivalDelta := arriveTime.Sub(g.LastArriveTime)
    return arrivalDelta <= BurstThresholdMs*time.Millisecond
}
```

### Pattern 2: Scalar Kalman Filter
**What:** Single-state Kalman filter for delay gradient estimation
**When to use:** Filtering noise from inter-arrival delay measurements
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-01 Section 5
type KalmanFilter struct {
    estimate      float64 // m_hat(i) - current estimate
    errorCov      float64 // e(i) - error covariance
    processNoise  float64 // q - state noise variance (10^-3)
    measureNoise  float64 // var_v_hat - measurement noise variance
    chi           float64 // exponential smoothing coefficient
}

func NewKalmanFilter() *KalmanFilter {
    return &KalmanFilter{
        estimate:     0,
        errorCov:     0.1,    // e(0) = 0.1
        processNoise: 0.001,  // q = 10^-3
        measureNoise: 1.0,
        chi:          0.01,   // recommended range [0.001, 0.1]
    }
}

func (k *KalmanFilter) Update(measurement float64) float64 {
    // Innovation
    z := measurement - k.estimate

    // Update measurement noise estimate (exponential averaging)
    // Outlier filtering: cap at 3*sqrt(var_v)
    zCapped := z
    if absZ := math.Abs(z); absZ > 3*math.Sqrt(k.measureNoise) {
        zCapped = math.Copysign(3*math.Sqrt(k.measureNoise), z)
    }
    k.measureNoise = math.Max(1.0, k.measureNoise*(1-k.chi) + k.chi*zCapped*zCapped)

    // Kalman gain
    gain := (k.errorCov + k.processNoise) / (k.measureNoise + k.errorCov + k.processNoise)

    // State update
    k.estimate = k.estimate + z*gain

    // Error covariance update
    k.errorCov = (1 - gain) * (k.errorCov + k.processNoise)

    return k.estimate
}
```

### Pattern 3: Adaptive Threshold State Machine
**What:** Three-state detector with asymmetric threshold adaptation
**When to use:** Determining congestion state from filtered delay gradient
**Example:**
```go
// Source: draft-ietf-rmcat-gcc-02 Section 5.4
type BandwidthUsage int

const (
    BwNormal BandwidthUsage = iota
    BwUnderusing
    BwOverusing
)

type OveruseDetector struct {
    threshold       float64       // del_var_th (adaptive)
    lastUpdateTime  time.Time     // for threshold adaptation
    overuseStart    time.Time     // when overuse started
    overuseCount    int           // consecutive overuse detections
    prevEstimate    float64       // m(i-1) for suppression check
    hypothesis      BandwidthUsage
}

const (
    InitialThreshold   = 12.5  // ms
    MinThreshold       = 6.0   // ms
    MaxThreshold       = 600.0 // ms
    Ku                 = 0.01  // threshold increase rate
    Kd                 = 0.00018 // threshold decrease rate
    OveruseTimeThreshMs = 10   // sustained overuse threshold
)
```

### Pattern 4: Monotonic Time Abstraction
**What:** Interface for time operations that only uses monotonic clock
**When to use:** All delay calculations to prevent wall clock drift
**Example:**
```go
// Source: Go time package design
type Clock interface {
    Now() time.Time  // Returns time with monotonic reading
}

type MonotonicClock struct{}

func (MonotonicClock) Now() time.Time {
    return time.Now() // Go time.Now() includes monotonic component
}

// For testing
type MockClock struct {
    current time.Time
}

func (m *MockClock) Now() time.Time { return m.current }
func (m *MockClock) Advance(d time.Duration) { m.current = m.current.Add(d) }
```

### Anti-Patterns to Avoid
- **Using time.Parse or time.Date for timestamps:** These strip monotonic clock reading; always use time.Now() for arrival times
- **Computing delay with wall clock:** Wall clock can jump due to NTP; use time.Sub() which uses monotonic internally
- **Static threshold:** Causes TCP starvation; threshold MUST adapt with K_u/K_d coefficients
- **Processing individual packets:** Must group packets by burst threshold before computing inter-group deltas
- **Ignoring signal suppression:** Must check m(i) < m(i-1) before signaling overuse

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| 24-bit timestamp parsing | Custom parser | `pion/rtp.AbsSendTimeExtension` | Handles 6.18 fixed-point format, wraparound estimation |
| Timestamp wraparound detection | Simple comparison | Half-range comparison (diff < 0x80000000 for 32-bit) | Must handle both forward and backward jumps |
| Linear regression for trendline | Custom least squares | Standard formula with accumulator pattern | Need numerical stability for streaming data |

**Key insight:** The GCC algorithm is precisely specified in the IETF draft. Deviating from the specification causes interoperability issues with Chrome/libwebrtc. Follow the spec exactly for parameters and formulas.

## Common Pitfalls

### Pitfall 1: Wrong Timestamp Wraparound Handling
**What goes wrong:** 24-bit abs-send-time wraps at 64 seconds, 32-bit RTP timestamp wraps at ~13.25 hours (90kHz). Treating wraparound as packet loss or corruption.
**Why it happens:** Simple subtraction without wraparound awareness produces large negative or positive deltas.
**How to avoid:** Use half-range comparison: if delta > half of max value, adjust by full range.
**Warning signs:** Sudden large negative inter-arrival times, bandwidth estimates jumping to zero/max.
```go
// 24-bit wraparound (64 second period)
const AbsSendTimeMax = 1 << 24
func unwrapAbsSendTime(prev, curr uint32) int64 {
    diff := int32(curr) - int32(prev)
    if diff > AbsSendTimeMax/2 {
        diff -= AbsSendTimeMax
    } else if diff < -AbsSendTimeMax/2 {
        diff += AbsSendTimeMax
    }
    return int64(diff)
}
```

### Pitfall 2: Static Threshold Causing TCP Starvation
**What goes wrong:** Using a fixed overuse threshold (e.g., always 12.5ms) causes WebRTC to be too aggressive, starving TCP traffic.
**Why it happens:** Network conditions vary; a threshold appropriate for one condition is too sensitive/insensitive for another.
**How to avoid:** Implement adaptive threshold with asymmetric coefficients (K_u=0.01, K_d=0.00018). Threshold increases slowly when estimate exceeds it, decreases faster when below.
**Warning signs:** TCP connections timing out, unfair bandwidth sharing.

### Pitfall 3: Missing Burst Grouping
**What goes wrong:** Computing inter-arrival delay per-packet instead of per-group produces noisy estimates that oscillate wildly.
**Why it happens:** Video frames are transmitted as packet bursts; individual packet timing within a burst is not meaningful for congestion detection.
**How to avoid:** Group packets arriving within 5ms (default) into groups; compute inter-group delay variations.
**Warning signs:** Delay estimates swinging wildly, overuse detector flickering between states rapidly.

### Pitfall 4: Wall Clock Leakage
**What goes wrong:** Using `time.Parse`, `time.Date`, or serializing/deserializing time values loses the monotonic clock reading, causing incorrect delay calculations when system clock adjusts.
**Why it happens:** Only `time.Now()` in Go carries both wall and monotonic clock readings.
**How to avoid:** Never store arrival times as anything other than `time.Time` from `time.Now()`. Use `time.Since()` and `time.Sub()` for all duration calculations.
**Warning signs:** Negative delays, sudden delay spikes during NTP adjustments.

### Pitfall 5: Ignoring Signal Suppression
**What goes wrong:** Signaling overuse when delay gradient is decreasing, causing unnecessary rate reduction during queue drainage.
**Why it happens:** Checking only threshold crossing without checking gradient direction.
**How to avoid:** Per spec: "if m(i) < m(i-1), over-use will not be signaled even if all the above conditions are met."
**Warning signs:** Rate oscillation during stable conditions, slow recovery after congestion.

## Code Examples

Verified patterns from official sources:

### Inter-Arrival Delay Calculation
```go
// Source: draft-ietf-rmcat-gcc-02 Section 4.2
// d(i) = t(i) - t(i-1) - (T(i) - T(i-1))
// where t = arrival time, T = send time

func (c *InterArrivalCalculator) ComputeDelayVariation(
    prevGroup, currGroup PacketGroup,
) time.Duration {
    // Receive time delta (monotonic)
    receiveDelta := currGroup.LastArriveTime.Sub(prevGroup.LastArriveTime)

    // Send time delta (from abs-send-time, needs unwrapping)
    sendDelta := c.unwrapSendTime(prevGroup.LastSendTime, currGroup.LastSendTime)

    // Inter-arrival delay variation
    return receiveDelta - sendDelta
}
```

### Trendline Estimator (Linear Regression)
```go
// Source: WebRTC trendline_estimator.cc
// Uses least squares: slope = sum((x-x_avg)(y-y_avg)) / sum((x-x_avg)^2)

type TrendlineEstimator struct {
    windowSize     int
    smoothingCoef  float64   // 0.9 default
    thresholdGain  float64   // 4.0 default
    history        []sample  // (arrival_time_ms, smoothed_delay)
    smoothedDelay  float64
    numDeltas      int
}

type sample struct {
    arrivalTime   float64 // ms since start
    smoothedDelay float64 // accumulated smoothed delay
}

func (t *TrendlineEstimator) Update(arrivalTimeMs float64, delayMs float64) float64 {
    // Exponential smoothing of accumulated delay
    t.smoothedDelay = t.smoothingCoef*t.smoothedDelay + (1-t.smoothingCoef)*delayMs

    // Add to history
    t.history = append(t.history, sample{arrivalTimeMs, t.smoothedDelay})
    if len(t.history) > t.windowSize {
        t.history = t.history[1:]
    }

    t.numDeltas++

    // Compute slope via linear regression
    slope := t.linearFitSlope()

    // Modified trend for threshold comparison
    return float64(min(t.numDeltas, 60)) * slope * t.thresholdGain
}

func (t *TrendlineEstimator) linearFitSlope() float64 {
    if len(t.history) < 2 {
        return 0
    }

    var sumX, sumY, sumXX, sumXY float64
    n := float64(len(t.history))

    for _, s := range t.history {
        sumX += s.arrivalTime
        sumY += s.smoothedDelay
        sumXX += s.arrivalTime * s.arrivalTime
        sumXY += s.arrivalTime * s.smoothedDelay
    }

    denom := n*sumXX - sumX*sumX
    if denom == 0 {
        return 0
    }
    return (n*sumXY - sumX*sumY) / denom
}
```

### Adaptive Threshold Update
```go
// Source: draft-ietf-rmcat-gcc-02 Section 5.4
// del_var_th(i) = del_var_th(i-1) + delta_t * K * (|m(i)| - del_var_th(i-1))

func (d *OveruseDetector) updateThreshold(estimate float64, now time.Time) {
    absEstimate := math.Abs(estimate)

    // Time since last update
    deltaT := now.Sub(d.lastUpdateTime).Seconds()
    d.lastUpdateTime = now

    // Select coefficient based on estimate vs threshold
    k := Kd
    if absEstimate > d.threshold {
        k = Ku
    }

    // Update threshold
    d.threshold += deltaT * k * (absEstimate - d.threshold)

    // Clamp to valid range
    d.threshold = math.Max(MinThreshold, math.Min(MaxThreshold, d.threshold))
}
```

### Overuse Detection with Sustained Requirement
```go
// Source: draft-ietf-rmcat-gcc-02 Section 5.3

func (d *OveruseDetector) Detect(estimate float64, now time.Time) BandwidthUsage {
    d.updateThreshold(estimate, now)

    // Check states
    if estimate > d.threshold {
        // Potential overuse - check sustained and suppression
        if d.hypothesis != BwOverusing {
            d.overuseStart = now
            d.overuseCount = 0
        }
        d.overuseCount++

        // Signal suppression: don't signal if estimate is decreasing
        if estimate < d.prevEstimate {
            d.hypothesis = BwNormal
        } else if now.Sub(d.overuseStart) >= OveruseTimeThreshMs*time.Millisecond && d.overuseCount > 1 {
            d.hypothesis = BwOverusing
        }
    } else if estimate < -d.threshold {
        d.hypothesis = BwUnderusing
    } else {
        d.hypothesis = BwNormal
    }

    d.prevEstimate = estimate
    return d.hypothesis
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Two-dimensional Kalman | Scalar Kalman | draft-ietf-rmcat-gcc-01 | Simpler implementation, same accuracy |
| Kalman filter only | Trendline estimator option | WebRTC M59 | Trendline is default in modern WebRTC |
| Static threshold | Adaptive threshold | Original GCC design | Required for TCP fairness |
| Receiver-side BWE (REMB) | Sender-side BWE (TWCC) | WebRTC ~2017 | TWCC is modern default, but REMB still needed for interop |

**Deprecated/outdated:**
- **Two-dimensional Kalman filter:** Simplified to scalar Kalman in draft-01; don't implement the complex version
- **Non-adaptive threshold:** Never use static threshold; causes TCP starvation

## Open Questions

Things that couldn't be fully resolved:

1. **abs-capture-time vs abs-send-time priority**
   - What we know: abs-capture-time is 64-bit UQ32.32, abs-send-time is 24-bit 6.18; abs-capture-time has larger range
   - What's unclear: Do both extensions need to be supported simultaneously, or is abs-send-time sufficient for most interop?
   - Recommendation: Implement abs-send-time first (TIME-01, TIME-02), then abs-capture-time (TIME-03) as alternative; most systems use abs-send-time

2. **Exact trendline parameters in libwebrtc**
   - What we know: Window size ~20, smoothing 0.9, gain 4.0 are documented defaults
   - What's unclear: Are these the exact values used in current Chrome/libwebrtc?
   - Recommendation: Use documented defaults; plan for configurability to tune during validation

3. **Pion GCC package reuse**
   - What we know: `pion/interceptor/pkg/gcc` has Kalman, overuse detector, arrival groups implemented
   - What's unclear: These are sender-side implementations; how much can be directly reused vs adapted?
   - Recommendation: Study the patterns but implement fresh for receiver-side; avoids coupling to sender-side TWCC flow

## Sources

### Primary (HIGH confidence)
- [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02) - IETF specification for GCC algorithm
- [draft-ietf-rmcat-gcc-01](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-01) - Kalman filter equations
- [WebRTC abs-send-time spec](https://webrtc.googlesource.com/src/+/main/docs/native-code/rtp-hdrext/abs-send-time/README.md) - 24-bit timestamp format
- [WebRTC abs-capture-time spec](https://webrtc.googlesource.com/src/+/refs/heads/main/docs/native-code/rtp-hdrext/abs-capture-time/README.md) - 64-bit timestamp format
- [pion/rtp AbsSendTimeExtension](https://pkg.go.dev/github.com/pion/rtp) - Go implementation of timestamp parsing
- [pion/interceptor/pkg/gcc](https://pkg.go.dev/github.com/pion/interceptor/pkg/gcc) - Reference Go GCC implementation (sender-side)

### Secondary (MEDIUM confidence)
- [WebRTC inter_arrival.cc](https://github.com/webrtc-uwp/webrtc/blob/master/modules/remote_bitrate_estimator/inter_arrival.cc) - Burst grouping (5ms threshold, 100ms max burst)
- [jitsi/webrtc trendline_estimator.cc](https://github.com/jitsi/webrtc/blob/M98/modules/congestion_controller/goog_cc/trendline_estimator.cc) - Trendline algorithm details
- [webrtc-uwp overuse_detector.cc](https://github.com/webrtc-uwp/webrtc/blob/master/modules/remote_bitrate_estimator/overuse_detector.cc) - Overuse detection implementation
- [Go time package monotonic design](https://go.googlesource.com/proposal/+/master/design/12914-monotonic.md) - Monotonic time handling

### Tertiary (LOW confidence)
- [VictoriaMetrics Go time blog](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/) - Practical Go monotonic time patterns

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Pion libraries well-documented, no external Kalman needed
- Architecture: HIGH - IETF draft provides precise specification
- Pitfalls: HIGH - Well-documented in spec and libwebrtc source

**Research date:** 2026-01-22
**Valid until:** 2026-03-22 (60 days - stable specification, unlikely to change)
