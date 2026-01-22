# Roadmap: GCC Receiver-Side BWE

## Overview

This roadmap delivers a Go port of libwebrtc's GCC delay-based receiver-side bandwidth estimator across two milestones: v1.0 (Phases 1-4) implements the complete BWE pipeline from packet observation to REMB generation with Chrome interoperability. v1.1 (Phase 5) refactors the implementation to adopt Pion's native extension parsing types, reducing maintenance burden while preserving validated behavior and performance characteristics.

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3, 4, 5): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

**v1.0 (COMPLETE):**
- [x] **Phase 1: Foundation & Core Pipeline** - Delay measurement, filtering, and congestion detection
- [x] **Phase 2: Rate Control & REMB** - AIMD rate control, REMB output, and core API
- [x] **Phase 3: Pion Integration** - Interceptor implementation and extension parsing
- [x] **Phase 4: Optimization & Validation** - Performance tuning and Chrome interoperability

**v1.1 (CURRENT):**
- [ ] **Phase 5: Pion Type Adoption** - Adopt Pion extension types, validate no regressions

## Phase Details

### Phase 1: Foundation & Core Pipeline

**Goal**: Produce accurate congestion signals (Normal/Overusing/Underusing) from incoming RTP packet streams with delay-based analysis

**Depends on**: Nothing (first phase)

**Requirements**: TIME-01, TIME-02, TIME-03, DELAY-01, DELAY-02, DELAY-03, DELAY-04, FILTER-01, FILTER-02, FILTER-03, DETECT-01, DETECT-02, DETECT-03, DETECT-04, PERF-03

**Success Criteria** (what must be TRUE):
  1. Given RTP packets with abs-send-time extension, the system correctly computes inter-arrival delay variations including proper 64-second wraparound handling
  2. The Kalman filter produces smoothed delay gradient values that track queuing delay trends without oscillating on bursty traffic
  3. The overuse detector transitions between Normal/Overusing/Underusing states appropriately, requiring sustained overuse before signaling state change
  4. Unit tests pass with synthetic packet traces that exercise wraparound, burst grouping, and state transition edge cases
  5. Monotonic time is used consistently throughout delay calculations (no wall clock leakage)

**Plans**: 6 plans in 4 waves

Plans:
- [x] 01-01-PLAN.md — Types, constants, timestamp parsing with 64s wraparound (TIME-01, TIME-02)
- [x] 01-02-PLAN.md — Inter-arrival calculator with burst grouping (DELAY-01, DELAY-02, DELAY-03, DELAY-04)
- [x] 01-03-PLAN.md — Kalman filter delay estimator (FILTER-01, FILTER-02)
- [x] 01-04-PLAN.md — Trendline estimator alternative (FILTER-03)
- [x] 01-05-PLAN.md — Overuse detector with adaptive threshold (DETECT-01, DETECT-02, DETECT-03, DETECT-04)
- [x] 01-06-PLAN.md — Abs-capture-time, integration tests (TIME-03, PERF-03)

---

### Phase 2: Rate Control & REMB

**Goal**: Generate accurate bandwidth estimates and spec-compliant REMB RTCP packets from congestion signals

**Depends on**: Phase 1

**Requirements**: RATE-01, RATE-02, RATE-03, RATE-04, REMB-01, REMB-02, REMB-03, REMB-04, CORE-01, CORE-02, CORE-03, CORE-04

**Success Criteria** (what must be TRUE):
  1. The AIMD rate controller correctly transitions between Increase/Decrease/Hold states based on congestion signals, applying 0.85x multiplicative decrease on overuse
  2. Bandwidth estimates respond appropriately to changing network conditions (increase during underuse, decrease during overuse, hold during normal)
  3. REMB packets are correctly encoded with mantissa+exponent format and can be parsed by standard RTCP libraries
  4. The standalone Estimator API accepts packets and returns bandwidth estimates without any Pion dependencies
  5. Multiple concurrent SSRCs are supported with aggregated bandwidth estimation

**Plans**: 6 plans in 4 waves

Plans:
- [x] 02-01-PLAN.md — Sliding window bitrate measurement (RATE-03)
- [x] 02-02-PLAN.md — AIMD rate controller state machine (RATE-01, RATE-02, RATE-04)
- [x] 02-03-PLAN.md — REMB packet builder using pion/rtcp (REMB-01, REMB-02)
- [x] 02-04-PLAN.md — REMB scheduler with immediate decrease (REMB-03, REMB-04)
- [x] 02-05-PLAN.md — Standalone BandwidthEstimator API (CORE-01, CORE-02, CORE-03)
- [x] 02-06-PLAN.md — Multi-SSRC support and integration tests (CORE-04)

---

### Phase 3: Pion Integration

**Goal**: Provide a working Pion interceptor that observes RTP streams and generates REMB feedback

**Depends on**: Phase 2

**Requirements**: TIME-04, PION-01, PION-02, PION-03, PION-04, PION-05, PERF-02

**Success Criteria** (what must be TRUE):
  1. The interceptor integrates with Pion PeerConnection via standard InterceptorFactory pattern
  2. RTP packets are observed without blocking the media pipeline (interceptor passes through cleanly)
  3. REMB packets are sent at configurable intervals (default 1Hz) via the bound RTCP writer
  4. Extension IDs are auto-detected from SDP negotiation (abs-send-time and abs-capture-time)
  5. Streams timeout gracefully after 2 seconds of inactivity without resource leaks

**Plans**: 6 plans in 5 waves

Plans:
- [x] 03-01-PLAN.md — Dependencies, extension ID helpers, stream state types (TIME-04)
- [x] 03-02-PLAN.md — BWEInterceptor with BindRemoteStream (PION-01, PION-02)
- [x] 03-03-PLAN.md — BindRTCPWriter and REMB loop (PION-03)
- [x] 03-04-PLAN.md — Stream timeout and Close() (PION-04)
- [x] 03-05-PLAN.md — InterceptorFactory for PeerConnection (PION-05)
- [x] 03-06-PLAN.md — sync.Pool optimization and integration tests (PERF-02)

---

### Phase 4: Optimization & Validation

**Goal**: Achieve production-ready performance and validate Chrome/libwebrtc interoperability

**Depends on**: Phase 3

**Requirements**: PERF-01, VALID-01, VALID-02, VALID-03, VALID-04

**Success Criteria** (what must be TRUE):
  1. Steady-state packet processing allocates less than 1 object per packet (verified via benchmark)
  2. Bandwidth estimates diverge less than 10% from libwebrtc under equivalent network conditions
  3. REMB packets are accepted by Chrome and visible in chrome://webrtc-internals
  4. The estimator coexists fairly with TCP traffic (no starvation, appropriate backoff)
  5. 24-hour soak test completes without timestamp-related failures or memory leaks

**Plans**: 5 plans in 3 waves

Plans:
- [x] 04-01-PLAN.md — Allocation profiling and benchmarks (PERF-01)
- [x] 04-02-PLAN.md — Reference trace comparison harness (VALID-01)
- [x] 04-03-PLAN.md — TCP fairness simulation tests (VALID-03)
- [x] 04-04-PLAN.md — Chrome interop test server (VALID-02) [checkpoint]
- [x] 04-05-PLAN.md — 24-hour soak test (VALID-04)

---

### Phase 5: Pion Type Adoption

**Goal**: Refactor BWE implementation to use Pion's native extension parsing types while preserving validated behavior and performance

**Depends on**: Phase 4 (v1.0 complete)

**Requirements**: EXT-01, EXT-02, EXT-03, EXT-04, KEEP-01, KEEP-02, KEEP-03, VAL-01, VAL-02, VAL-03, VAL-04

**Success Criteria** (what must be TRUE):
  1. RTP extension parsing delegates to pion/rtp.AbsSendTimeExtension and pion/rtp.AbsCaptureTimeExtension
  2. Custom ParseAbsSendTime() and ParseAbsCaptureTime() functions are removed from codebase
  3. Critical wraparound logic (UnwrapAbsSendTime) and extension discovery helpers (FindExtensionID) remain unchanged
  4. All existing tests pass without modification (behavioral equivalence verified)
  5. Benchmark suite shows 0 allocs/op for core estimator maintained (no allocation regression)
  6. 24-hour accelerated soak test passes (timestamp wraparound handling preserved)
  7. Chrome interop still works (REMB packets accepted, visible in webrtc-internals)

**Plans**: TBD during planning

---

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3 -> 4 -> 5

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Foundation & Core Pipeline | 6/6 | Complete | 2026-01-22 |
| 2. Rate Control & REMB | 6/6 | Complete | 2026-01-22 |
| 3. Pion Integration | 6/6 | Complete | 2026-01-22 |
| 4. Optimization & Validation | 5/5 | Complete | 2026-01-22 |
| 5. Pion Type Adoption | 0/? | Not started | — |

**v1.0 MILESTONE COMPLETE** - All 23 plans across 4 phases executed successfully (2026-01-22).

**v1.1 MILESTONE** - Phase 5 planning in progress.

---

## Requirement Coverage

**v1.0 (Complete):** All 39 requirements mapped to Phases 1-4:

| Category | Phase 1 | Phase 2 | Phase 3 | Phase 4 | Total |
|----------|---------|---------|---------|---------|-------|
| TIME | 3 | 0 | 1 | 0 | 4 |
| DELAY | 4 | 0 | 0 | 0 | 4 |
| FILTER | 3 | 0 | 0 | 0 | 3 |
| DETECT | 4 | 0 | 0 | 0 | 4 |
| RATE | 0 | 4 | 0 | 0 | 4 |
| REMB | 0 | 4 | 0 | 0 | 4 |
| CORE | 0 | 4 | 0 | 0 | 4 |
| PION | 0 | 0 | 5 | 0 | 5 |
| PERF | 1 | 0 | 1 | 1 | 3 |
| VALID | 0 | 0 | 0 | 4 | 4 |
| **Total** | **15** | **12** | **7** | **5** | **39** |

**v1.1 (Current):** All 11 requirements mapped to Phase 5:

| Category | Phase 5 | Total |
|----------|---------|-------|
| EXT | 4 | 4 |
| KEEP | 3 | 3 |
| VAL | 4 | 4 |
| **Total** | **11** | **11** |

**Combined Coverage:** 50 requirements across 5 phases (39 v1.0 + 11 v1.1)

---

*Roadmap created: 2026-01-22*
*Last updated: 2026-01-22 - v1.1 roadmap added (Phase 5)*
