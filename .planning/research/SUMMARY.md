# Research Summary: GCC Receiver-Side BWE

**Project:** Go port of libwebrtc's GCC delay-based receiver-side bandwidth estimator
**Researched:** 2026-01-22
**Domain:** WebRTC congestion control using REMB feedback

---

## Executive Summary

This project fills a critical gap in the Pion WebRTC ecosystem: **receiver-side bandwidth estimation using REMB**. While Pion has sender-side GCC using TWCC, there is no receiver-side REMB implementation for interoperability with Chrome/libwebrtc systems expecting classic REMB-based congestion control.

The implementation is a greenfield Go port of libwebrtc's delay-based GCC algorithm. The core algorithm is well-specified in IETF draft-ietf-rmcat-gcc-02 and consists of five interconnected components: inter-arrival analysis, Kalman filter-based delay gradient estimation, adaptive threshold overuse detection, AIMD rate control, and REMB packet generation. The entire pipeline processes incoming RTP packets with absolute capture timestamps to produce bandwidth estimates sent back to the sender via RTCP REMB messages.

**Key technical insight:** This is a medium-high complexity implementation with well-documented reference material. The primary challenges are (1) correctly tuning the adaptive threshold to avoid TCP starvation, (2) handling multiple timestamp wraparound scenarios (24-bit abs-send-time at 64s, 32-bit RTP timestamps at 6-13 hours), and (3) achieving comparable accuracy to libwebrtc through careful parameter tuning and burst grouping. The implementation should be built as a standalone core library with minimal dependencies, wrapped in a Pion interceptor adapter for WebRTC integration.

---

## Stack Recommendations

### Core Technology

| Technology | Version | Rationale |
|------------|---------|-----------|
| **Go** | 1.25.6+ | Green Tea GC provides 10-40% GC overhead reduction, critical for high packet-rate processing (10k+ pps) |
| **Pion RTP** | v1.10.0 | Provides `AbsCaptureTimeExtension` parser with `CaptureTime()` method for timestamp extraction |
| **Pion RTCP** | v1.2.16 | `ReceiverEstimatedMaximumBitrate` struct for REMB packet generation |
| **Pion Interceptor** | v0.1.43 | Interface definition for Pion WebRTC integration via `BindRemoteStream` / `BindRTCPWriter` |

**No external math libraries required.** GCC uses a simple scalar Kalman filter (1D state) that requires only standard library math operations. Starting with standard library and adding gonum/mat only if matrix operations prove necessary later.

### Performance Patterns

| Pattern | Purpose | Expected Impact |
|---------|---------|-----------------|
| `sync.Pool` | Buffer pooling for packet metadata | 50%+ throughput improvement (benchmarked at 150k→230k pps) |
| Atomic operations | Lock-free state updates where possible | Reduce lock contention on multi-core |
| Pre-allocated slices | Avoid allocations in hot paths | Target: <1 allocation per packet |

### Reference Implementations

1. **Primary:** libwebrtc `modules/remote_bitrate_estimator/` (C++) - canonical implementation
2. **Secondary:** aiortc (Python) - simpler, more readable algorithm flow
3. **Go patterns:** Pion's sender-side GCC (`pion/interceptor/pkg/gcc`) - idiomatic Go for Kalman filter, overuse detection, AIMD

---

## Table Stakes Features

These nine components are **required for Chrome/libwebrtc interoperability**. All must be implemented for v1.

### 1. Absolute Send Time Parser
Parse 24-bit RTP header extension containing NTP-derived timestamp (6.18 fixed-point format, ~3.8μs resolution, 64s wraparound). **Critical** for delay measurement.

### 2. Inter-Arrival Time Calculator
Compute delay variation between packet groups: `d(i) = (t(i) - t(i-1)) - (T(i) - T(i-1))` where t=receive time, T=send time. Must handle wraparound correctly.

### 3. Packet Group Aggregation (Burst Grouping)
Group packets sent within 5ms burst interval for collective analysis (reduces noise from bursty video encoders). Constants: `kBurstDeltaThresholdMs = 5ms`, `kMaxBurstDurationMs = 100ms`.

### 4. Arrival-Time Filter (Kalman Filter)
Filter noise from delay measurements to estimate true delay gradient `m(i)`. Parameters from IETF draft: `q = 10^-3`, `e(0) = 0.1`, `chi = 0.1-0.001`.

### 5. Overuse Detector
Compare delay gradient against adaptive threshold to produce congestion state: `kBwNormal`, `kBwOverusing`, `kBwUnderusing`. Requires sustained overuse (≥10ms) before signaling.

### 6. Adaptive Threshold
Dynamically adjust detection threshold to avoid TCP starvation. Formula: `del_var_th(i) = del_var_th(i-1) + Δt * K(i) * (|m(i)| - del_var_th(i-1))` with `K_u = 0.01`, `K_d = 0.00018` (asymmetric is critical).

### 7. AIMD Rate Controller
Compute target bitrate using Additive Increase / Multiplicative Decrease. On overuse: multiply by 0.85. On normal: increase 8%/sec (far from convergence) or additive ~0.5 packets/RTT (near convergence).

### 8. Incoming Bitrate Measurement
Track actual received bitrate using sliding window (0.5-1.0s recommended). Provides `R_hat` for rate controller.

### 9. REMB RTCP Packet Generator
Encode bandwidth estimate as RTCP PSFB message (PT=206, FMT=15, "REMB" identifier, mantissa+exponent encoding: `bitrate_bps = BR_Mantissa * 2^BR_Exp`).

**Complexity assessment:** Medium-High overall. Primary challenge is Kalman filter parameter tuning for Chrome-like behavior. Secondary challenge is timestamp wraparound handling.

---

## Architecture Overview

### Component Structure

The system consists of five major components arranged in a processing pipeline:

```
RTP Packet + Abs Capture Time
            |
            v
+-------------------------+
| 1. Inter-Arrival Filter | → Groups packets, computes delay variation d(i)
+-------------------------+
            |
            v
+-------------------------+
| 2. Delay Estimator      | → Kalman filter produces smoothed gradient m(i)
| (Kalman/Trendline)      |
+-------------------------+
            |
            v
+-------------------------+
| 3. Overuse Detector     | → Compares m(i) to adaptive threshold
| (State Machine)         | → Outputs NORMAL/OVERUSE/UNDERUSE signal
+-------------------------+
            |
            v
+-------------------------+
| 4. AIMD Rate Controller | → AIMD state machine produces bandwidth estimate
|                         | → Uses incoming bitrate R_hat from statistics
+-------------------------+
            |
            v
+-------------------------+
| 5. REMB Generator       | → Packages estimate into REMB RTCP packet
+-------------------------+
```

### Standalone Core vs Pion Adapter

**Design principle:** Core library should be pure algorithm with zero Pion dependencies. This enables unit testing without WebRTC infrastructure and potential reuse in non-Pion contexts.

**Core library API:**
```go
package gcc

type Estimator struct { /* private */ }
func NewEstimator(cfg Config) *Estimator

// Core processing
func (e *Estimator) OnPacket(arrivalTime int64, sendTime uint64, payloadSize int, ssrc uint32)
func (e *Estimator) GetEstimate() uint64  // bits per second
func (e *Estimator) GetSSRCs() []uint32
```

**Pion interceptor adapter:**
```go
package gcc

type Interceptor struct {
    estimator    *Estimator
    rtcpWriter   interceptor.RTCPWriter
    // Timer goroutine: periodically flush REMB
}

func (i *Interceptor) BindRemoteStream(info *StreamInfo, reader RTPReader) RTPReader
func (i *Interceptor) BindRTCPWriter(writer RTCPWriter) RTCPWriter
```

### Data Flow

1. **Packet Reception:** `BindRemoteStream()` captures incoming RTP packets
2. **Timestamp Extraction:** Parse abs-send-time extension from RTP header
3. **Core Processing:** Feed to `Estimator.OnPacket()`
4. **REMB Generation:** Timer goroutine (1Hz default) calls `GetEstimate()` and builds REMB
5. **RTCP Output:** Send REMB via `BindRTCPWriter()`

---

## Critical Pitfalls

These are the top risks that can cause total implementation failure if not addressed:

### 1. Static Threshold Causes TCP Starvation (CRITICAL - Phase 1)

**Problem:** Using fixed threshold in overuse detector causes GCC flows to be starved when competing with TCP traffic. Academic research (Carlucci et al., 2017) proved this.

**Prevention:** Implement adaptive threshold with asymmetric coefficients: `K_u = 0.01`, `K_d = 0.00018` (K_u > K_d is critical). Clamp to [6ms, 600ms]. Skip update when outlier detected (`|m(i)| - del_var_th(i)| > 15ms`).

**Detection:** Test with concurrent TCP traffic (iperf). If GCC flow gets starved, adaptive threshold is broken.

### 2. Timestamp Wraparound Handling (CRITICAL - Phase 1)

**Problem:** Two wraparound scenarios cause massive spurious delay calculations:
- **24-bit abs-send-time wraps every 64 seconds**
- **32-bit RTP timestamp wraps after 6-13 hours** (depends on clock rate)

**Prevention:**
- Wraparound-safe comparison: treat delta > 2^31 as wraparound
- Implementation: `if delta > 0x7FFFFFFF { delta -= 0x100000000 }`
- Track expected time and detect abs-send-time wraparound: when new < old by large margin, add 2^24

**Detection:** Run long-duration tests (>12 hours). Monitor for periodic bandwidth drops every ~64 seconds or sudden crashes after 6-13 hours.

### 3. Wrong Delay Gradient Calculation (CRITICAL - Phase 1)

**Problem:** Inter-group delay variation m(i) calculated incorrectly leads to false overuse/underuse signals. Common mistakes: confusing arrival time deltas with timestamp deltas, not grouping packets properly, mixing time units.

**Prevention:**
- Timestamp groups based on RTP timestamp, not arrival time
- Group length should be ~5ms
- Formula: `(arrival[i] - arrival[i-1]) - (timestamp[i] - timestamp[i-1]) / clockrate`
- Filter using Kalman, not raw values

**Detection:** Compare delay gradient output against libwebrtc with same packet trace. Large divergence indicates calculation error.

### 4. Incorrect AIMD State Machine (HIGH - Phase 2)

**Problem:** Rate control state machine transitions incorrectly, causing bandwidth estimate to oscillate or get stuck.

**Prevention:** Implement exact state machine from IETF draft:

| Signal | Hold | Increase | Decrease |
|--------|------|----------|----------|
| Overuse | → Decrease | → Decrease | (stay) |
| Normal | → Increase | (stay) | → Hold |
| Underuse | (stay) | → Hold | → Hold |

Multiplicative decrease: `A_hat = 0.85 * R_hat` on overuse. Multiplicative increase: 8%/sec far from convergence. Additive increase: ~0.5 packets/RTT near convergence.

**Detection:** Log state transitions. Healthy flow should cycle: Increase → Hold → Decrease → Hold → Increase...

### 5. Burst Grouping Failures (HIGH - Phase 1)

**Problem:** Packets sent in bursts (video keyframes) incorrectly treated as separate arrivals, causing false congestion detection.

**Prevention:** Implement `BelongsToBurst()` logic:
- `propagation_delta = timestamp_delta - arrival_delta`
- If `propagation_delta < 0` and `arrival_delta <= 5ms` and total burst < 100ms, group as burst

**Detection:** Test with bursty traffic. If bandwidth drops significantly more than with smooth traffic at same rate, burst grouping is broken.

### 6. REMB Packet Format Errors (CRITICAL - Phase 2)

**Problem:** Malformed REMB packets are silently ignored by Chrome/libwebrtc, causing no feedback to reach sender.

**Prevention:** REMB format must be exact:
- PT = 206 (PSFB), FMT = 15
- Unique identifier: 0x52454D42 ("REMB")
- Bitrate encoding: `mantissa * 2^exp`

**Detection:** Capture REMB packets in Wireshark. Test against Chrome and verify webrtc-internals shows received REMB.

### 7. Per-Packet Allocations (CRITICAL - Phase 3)

**Problem:** Allocating memory for each RTP packet causes GC pressure that introduces latency spikes. At high packet rates (>1000 pps), GC overhead dominates.

**Prevention:**
- Use `sync.Pool` for packet metadata structures
- Pre-allocate slices with expected capacity
- Avoid closures in hot paths
- Pass buffers into functions rather than returning new slices
- Profile with `go tool pprof` focusing on allocs

**Detection:** Run with `GODEBUG=gctrace=1`. Target: <1 allocation per packet in steady state.

### 8. Monotonic vs Wall Clock in Go (HIGH - Ongoing)

**Problem:** Using `time.Time` operations that strip monotonic readings causes incorrect elapsed time calculations, especially after NTP adjustments.

**Prevention:**
- Use `time.Since(start)` for elapsed time, not `time.Now().Sub(start)` after transformations
- Never apply `UTC()`, `In()`, `Round()`, `Truncate()` to times used for duration calculation
- Store arrival times as monotonic offsets from session start

**Detection:** Change system clock during test. If bandwidth estimate goes haywire, you have wall clock leakage.

---

## Suggested Build Order

Based on dependencies and incremental testing ability:

### Phase 1: Foundation & Core Pipeline (2-3 weeks)

**Goal:** End-to-end delay measurement and basic overuse detection

**Components:**
1. Types and constants (BandwidthUsage enum, GCC-compliant defaults)
2. Absolute send time parser (with 64s wraparound handling)
3. Inter-arrival filter (packet grouping, burst detection, 32-bit wraparound)
4. Delay estimator (scalar Kalman filter, noise variance tracking)
5. Overuse detector with adaptive threshold (K_u=0.01, K_d=0.00018)
6. Rate statistics (sliding window bitrate measurement)

**Deliverable:** Core `Estimator` API that takes packets and produces congestion signals

**Research needs:** None - algorithm is well-specified in IETF draft. Validate against libwebrtc implementation details for edge cases.

**Critical pitfalls to address:**
- Adaptive threshold implementation (#1)
- Both timestamp wraparound scenarios (#2)
- Correct delay gradient calculation (#3)
- Burst grouping logic (#5)
- Monotonic time handling (#8)

### Phase 2: Rate Control & REMB (1-2 weeks)

**Goal:** Complete algorithm with REMB output

**Components:**
1. AIMD rate controller (3-state FSM: Increase/Decrease/Hold)
2. Convergence detection (multiplicative vs additive increase)
3. REMB packet builder (mantissa+exponent encoding)
4. Estimator facade (wire all components together)

**Deliverable:** Standalone library that produces REMB packets from packet stream

**Research needs:** None - AIMD and REMB specs are clear. May need to verify Chrome's exact parameter values.

**Critical pitfalls to address:**
- AIMD state machine correctness (#4)
- REMB packet format compliance (#6)

### Phase 3: Pion Integration (1 week)

**Goal:** Working interceptor for Pion WebRTC

**Components:**
1. Abs-capture-time parser (from Pion RTP extensions)
2. Interceptor implementation (`BindRemoteStream`, `BindRTCPWriter`)
3. REMB sender goroutine (1Hz periodic + immediate on significant decrease)
4. Multi-SSRC support
5. Stream timeout handling (2s timeout for stale streams)

**Deliverable:** `InterceptorFactory` that plugs into Pion PeerConnection

**Research needs:** None - Pion interceptor interface is documented. May need to reference existing interceptors (NACK, TWCC) for patterns.

**Critical pitfalls to address:**
- Per-packet allocations (#7) - use sync.Pool
- Extension parsing from SDP-negotiated IDs

### Phase 4: Optimization & Validation (2-3 weeks)

**Goal:** Chrome-comparable accuracy and production-ready performance

**Activities:**
1. Profile and optimize allocations (target <1 per packet)
2. Add lock-free optimizations where possible
3. Compare against libwebrtc with captured traces (aim for <10% divergence)
4. Test with network impairment (delay, loss, jitter)
5. Test with competing TCP traffic
6. Long-duration soak tests (24 hours)
7. Multi-stream (simulcast) scenarios

**Deliverable:** Benchmarked, validated implementation ready for production

**Research needs:** May need targeted research if performance bottlenecks found or Chrome behavior diverges significantly. Use `/gsd:research-phase` if needed.

**Critical pitfalls to address:**
- Lock contention optimization
- Interop testing with Chrome webrtc-internals
- Long-duration wraparound testing

---

## Research Flags

### Phases Needing Research

**Phase 4 (Validation):** May need `/gsd:research-phase` if:
- Performance profiling reveals unexpected bottlenecks requiring Go-specific optimization research
- Comparison with libwebrtc shows >10% divergence requiring parameter tuning research
- Chrome interop shows undocumented behavior requiring debugging against libwebrtc source

### Phases with Well-Documented Patterns

**Phases 1-3:** Skip additional research. Algorithm is well-specified in:
- IETF draft-ietf-rmcat-gcc-02 (GCC algorithm)
- IETF draft-alvestrand-rmcat-remb (REMB format)
- libwebrtc source code (reference implementation)
- Pion interceptor docs (integration patterns)

The core GCC algorithm has been stable since ~2013. Modern libwebrtc uses sender-side estimation, but receiver-side components remain for REMB compatibility.

---

## Confidence Assessment

| Area | Confidence | Evidence |
|------|------------|----------|
| **Stack (Pion ecosystem)** | HIGH | Official docs, actively maintained packages with verified versions. AbsCaptureTimeExtension confirmed in pion/rtp v1.10.0. |
| **Stack (Go 1.25)** | HIGH | Green Tea GC benefits documented in go.dev release notes. Project already uses Go 1.25. |
| **Features (Component list)** | HIGH | Verified against IETF draft and libwebrtc source structure. All 9 table stakes components map 1:1 to libwebrtc classes. |
| **Features (Algorithm parameters)** | HIGH | Directly from draft-ietf-rmcat-gcc-02 with cross-reference to libwebrtc constants. |
| **Architecture (Component boundaries)** | HIGH | libwebrtc class hierarchy clearly defines component interfaces. Pion interceptor pattern is established. |
| **Architecture (Build order)** | MEDIUM | Based on logical dependency analysis, not validated implementation experience. May need adjustment during implementation. |
| **Pitfalls (Critical issues)** | HIGH | Sourced from academic papers (Carlucci et al.), libwebrtc bug trackers, IETF draft, and Go performance documentation. |
| **Pitfalls (Detection methods)** | MEDIUM | Based on analysis of what could go wrong. May discover additional edge cases during implementation. |

### Gaps to Address During Planning

1. **Exact Chrome parameter values:** IETF draft provides reference values, but Chrome may use slightly different tuning. Validate during Phase 4 comparison testing.

2. **Clock rate handling:** Research confirms video uses 90kHz typically, but exact handling for multi-codec scenarios may need validation during Pion integration.

3. **Simulcast SSRC aggregation:** Research identifies this as important but doesn't specify exact Chrome behavior. May need experimentation during Phase 3.

4. **Performance bottlenecks:** Identified sync.Pool and atomic operations as optimizations, but actual bottlenecks should be profiled rather than assumed. Defer optimization research to Phase 4.

5. **Edge case parameter tuning:** Kalman filter parameters (q, chi, e(0)) are specified but may need tuning for Go float64 vs C++ double precision differences.

---

## Open Questions

### For Validation (Phase 4)

1. **Chrome vs draft divergence:** How much does Chrome's production implementation diverge from IETF draft? Need packet trace comparison.

2. **Optimal REMB sending frequency:** Draft suggests 1Hz, but what triggers immediate sending? "Significant decrease" threshold needs clarification (research found ≥3% but libwebrtc may differ).

3. **Convergence detection parameters:** How is "near convergence" measured exactly? Draft mentions "within 3 standard deviations of previously measured rate" but implementation details unclear.

4. **Multi-stream aggregation strategy:** Should REMB be per-stream or aggregate for simulcast? Research found Firefox has bugs here - need Chrome behavior validation.

### For Pion Integration (Phase 3)

1. **Extension ID negotiation:** How does Pion negotiate abs-send-time extension ID in SDP? Need to verify against existing interceptors.

2. **RTCP compound packet handling:** Should REMB be sent standalone or in compound packet? Research suggests standalone for receiver-side, but verify Chrome expectations.

3. **Graceful shutdown:** How should interceptor handle stream removal and cleanup? Follow existing Pion interceptor patterns.

### Deferred to v2

1. **Trendline estimator support:** Modern libwebrtc uses trendline (linear regression) instead of Kalman for sender-side. Consider as alternative filter for receiver-side?

2. **Loss-based estimation integration:** Combine delay-based with loss-based signals? Research flagged this as anti-feature for v1 (sender-side concern) but may revisit.

3. **ML-based parameter tuning:** Meta and others use ML for BWE optimization. Out of scope for v1 reference implementation.

---

## Sources

### Authoritative Specifications
- [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02) - Google Congestion Control Algorithm (HIGH confidence)
- [draft-alvestrand-rmcat-remb](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb) - REMB RTCP Message Format (HIGH confidence)
- [Absolute Capture Time Extension](https://webrtc.googlesource.com/src/+/refs/heads/main/docs/native-code/rtp-hdrext/abs-capture-time/README.md) (HIGH confidence)
- [Absolute Send Time Extension](https://webrtc.github.io/webrtc-org/experiments/rtp-hdrext/abs-send-time/) (HIGH confidence)

### Reference Implementations
- [libwebrtc remote_bitrate_estimator](https://webrtc.googlesource.com/src/+/refs/heads/main/modules/remote_bitrate_estimator/) - Canonical C++ implementation (HIGH confidence)
- [aiortc rate.py](https://github.com/aiortc/aiortc) - Python implementation, simpler algorithm flow (MEDIUM confidence)
- [pion/interceptor/pkg/gcc](https://github.com/pion/interceptor/tree/master/pkg/gcc) - Go patterns for sender-side GCC (HIGH confidence for Go idioms)

### Academic Research
- [Congestion Control for Web Real-Time Communication (Carlucci et al., 2017)](https://c3lab.poliba.it/images/c/c4/Gcc-TNET.pdf) - Adaptive threshold research (HIGH confidence)
- [Analysis and Design of GCC (C3Lab)](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf) - Algorithm analysis (HIGH confidence)

### Go-Specific
- [Go 1.25 Release Notes](https://go.dev/doc/go1.25) - Green Tea GC benefits (HIGH confidence)
- [Pion RTP Package](https://pkg.go.dev/github.com/pion/rtp) - v1.10.0 (HIGH confidence)
- [Pion RTCP Package](https://pkg.go.dev/github.com/pion/rtcp) - v1.2.16 (HIGH confidence)
- [Pion Interceptor Package](https://pkg.go.dev/github.com/pion/interceptor) - v0.1.43 (HIGH confidence)
- [Go sync.Pool Performance](https://victoriametrics.com/blog/go-sync-pool/) - Benchmarks (MEDIUM confidence)
- [Go Monotonic Time](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/) - Wall vs monotonic clock (HIGH confidence)

---

## Ready for Requirements Definition

This research summary provides the foundation for roadmap creation:

✅ **Technology stack identified:** Pion ecosystem + Go 1.25 + standard library
✅ **Component boundaries defined:** 5 major components with clear interfaces
✅ **Build dependencies mapped:** Phase 1 → 2 → 3 → 4 progression
✅ **Critical risks identified:** 8 pitfalls with prevention strategies
✅ **Confidence levels assessed:** HIGH for algorithm/stack, MEDIUM for build order/edge cases
✅ **Research gaps documented:** Chrome behavior validation and parameter tuning for Phase 4

**Roadmapper can proceed** with structuring phases based on:
- Suggested build order (Foundation → Rate Control → Integration → Validation)
- Critical pitfalls mapped to phases (adaptive threshold in Phase 1, REMB format in Phase 2, etc.)
- Research flags indicating Phase 4 may need `/gsd:research-phase` for Chrome comparison

**Key recommendation for roadmapper:** Build Phase 1 as standalone core library with comprehensive unit tests against IETF draft examples. This de-risks Phases 2-3 which are simpler integrations. Save Chrome comparison for Phase 4 validation rather than blocking early phases.
