# Requirements: GCC Receiver-Side BWE

**Defined:** 2026-01-22
**Core Value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### Timestamp Parsing

- [ ] **TIME-01**: Parse abs-send-time 24-bit RTP header extension (6.18 fixed-point format)
- [ ] **TIME-02**: Handle 64-second timestamp wraparound correctly
- [ ] **TIME-03**: Parse abs-capture-time RTP header extension as alternative input
- [ ] **TIME-04**: Auto-detect extension IDs from SDP negotiation

### Delay Measurement

- [ ] **DELAY-01**: Compute inter-arrival time deltas (receive delta vs send delta)
- [ ] **DELAY-02**: Implement packet group aggregation with 5ms burst threshold
- [ ] **DELAY-03**: Handle 32-bit RTP timestamp wraparound (6-13 hour wrap)
- [ ] **DELAY-04**: Support configurable burst threshold parameter

### Noise Filtering

- [ ] **FILTER-01**: Implement Kalman filter for delay gradient estimation per IETF draft
- [ ] **FILTER-02**: Use spec-compliant parameters (q=10^-3, e(0)=0.1)
- [ ] **FILTER-03**: Implement trendline estimator as alternative filter option

### Congestion Detection

- [ ] **DETECT-01**: Implement overuse detector with 3 states (Normal/Overusing/Underusing)
- [ ] **DETECT-02**: Implement adaptive threshold with asymmetric coefficients (K_u=0.01, K_d=0.00018)
- [ ] **DETECT-03**: Require sustained overuse (≥10ms) before signaling state change
- [ ] **DETECT-04**: Provide state change callbacks to application code

### Rate Control

- [x] **RATE-01**: Implement AIMD rate controller (3-state FSM: Increase/Decrease/Hold)
- [x] **RATE-02**: Implement multiplicative decrease (0.85x) on overuse signal
- [x] **RATE-03**: Implement sliding window incoming bitrate measurement
- [x] **RATE-04**: Support configurable AIMD parameters (decrease factor, increase rate)

### REMB Output

- [x] **REMB-01**: Generate spec-compliant REMB RTCP packets (PT=206, FMT=15)
- [x] **REMB-02**: Encode bitrate correctly using mantissa+exponent format
- [x] **REMB-03**: Support configurable REMB send interval (default 1Hz)
- [x] **REMB-04**: Send REMB immediately on significant bandwidth decrease (>=3%)

### Core Library

- [x] **CORE-01**: Implement standalone Estimator API with no Pion dependencies
- [x] **CORE-02**: Provide OnPacket() method accepting arrival time, send time, payload size, SSRC
- [x] **CORE-03**: Provide GetEstimate() method returning bandwidth in bits per second
- [x] **CORE-04**: Support multiple concurrent SSRCs with aggregated estimation

### Pion Integration

- [ ] **PION-01**: Implement Pion Interceptor interface
- [ ] **PION-02**: Implement BindRemoteStream for RTP packet observation
- [ ] **PION-03**: Implement BindRTCPWriter for REMB packet output
- [ ] **PION-04**: Handle stream timeout with graceful cleanup after 2s inactivity
- [ ] **PION-05**: Provide InterceptorFactory for PeerConnection integration

### Performance

- [ ] **PERF-01**: Achieve <1 allocation per packet in steady state
- [ ] **PERF-02**: Use sync.Pool for packet metadata structures
- [ ] **PERF-03**: Use monotonic time correctly (avoid wall clock leakage)

### Validation

- [ ] **VALID-01**: Bandwidth estimate diverges <10% from libwebrtc under same conditions
- [ ] **VALID-02**: REMB packets accepted by Chrome (verified via webrtc-internals)
- [ ] **VALID-03**: Correct behavior when competing with TCP traffic (no starvation)
- [ ] **VALID-04**: Pass 24-hour soak test without timestamp-related failures

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Advanced Filtering

- **FILTER-04**: ML-based parameter auto-tuning
- **FILTER-05**: Hybrid Kalman + trendline estimation

### Extended Protocols

- **PROTO-01**: Support RemoteEstimate extension (Google Meet proprietary)
- **PROTO-02**: Loss-based estimation integration

### Monitoring

- **MON-01**: Prometheus metrics export
- **MON-02**: Detailed estimation state logging

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Send-side BWE / TWCC | Receiver-side only; Pion already has TWCC |
| Loss-based estimation | Sender-side concern; focus on delay-based for v1 |
| Simulcast layer selection | Separate concern from bandwidth estimation |
| Bandwidth probing | Sender-side feature |
| CGO / native code | Must be pure Go for portability |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| TIME-01 | Phase 1 | Pending |
| TIME-02 | Phase 1 | Pending |
| TIME-03 | Phase 1 | Pending |
| TIME-04 | Phase 3 | Pending |
| DELAY-01 | Phase 1 | Pending |
| DELAY-02 | Phase 1 | Pending |
| DELAY-03 | Phase 1 | Pending |
| DELAY-04 | Phase 1 | Pending |
| FILTER-01 | Phase 1 | Pending |
| FILTER-02 | Phase 1 | Pending |
| FILTER-03 | Phase 1 | Pending |
| DETECT-01 | Phase 1 | Pending |
| DETECT-02 | Phase 1 | Pending |
| DETECT-03 | Phase 1 | Pending |
| DETECT-04 | Phase 1 | Pending |
| RATE-01 | Phase 2 | Pending |
| RATE-02 | Phase 2 | Pending |
| RATE-03 | Phase 2 | Pending |
| RATE-04 | Phase 2 | Pending |
| REMB-01 | Phase 2 | Pending |
| REMB-02 | Phase 2 | Pending |
| REMB-03 | Phase 2 | Pending |
| REMB-04 | Phase 2 | Pending |
| CORE-01 | Phase 2 | Pending |
| CORE-02 | Phase 2 | Pending |
| CORE-03 | Phase 2 | Pending |
| CORE-04 | Phase 2 | Pending |
| PION-01 | Phase 3 | Pending |
| PION-02 | Phase 3 | Pending |
| PION-03 | Phase 3 | Pending |
| PION-04 | Phase 3 | Pending |
| PION-05 | Phase 3 | Pending |
| PERF-01 | Phase 4 | Pending |
| PERF-02 | Phase 3 | Pending |
| PERF-03 | Phase 1 | Pending |
| VALID-01 | Phase 4 | Pending |
| VALID-02 | Phase 4 | Pending |
| VALID-03 | Phase 4 | Pending |
| VALID-04 | Phase 4 | Pending |

**Coverage:**
- v1 requirements: 39 total
- Mapped to phases: 39
- Unmapped: 0

---
*Requirements defined: 2026-01-22*
*Last updated: 2026-01-22 after roadmap creation*
