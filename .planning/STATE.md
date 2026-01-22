# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-01-22)

**Core value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior
**Current focus:** Phase 2 - Rate Control (Plan 04 complete)

## Current Position

Phase: 2 of 4 (Rate Control & REMB)
Plan: 4 of 6 in current phase
Status: In progress
Last activity: 2026-01-22 - Completed 02-04-PLAN.md (REMB scheduler)

Progress: [██████████░░░░░░░░░░░░░] 43% (10/23 plans)

## Performance Metrics

**Velocity:**
- Total plans completed: 10
- Average duration: 3.3 min
- Total execution time: 33 min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Foundation | 6/6 | 23 min | 3.8 min |
| 2. Rate Control | 4/6 | 10 min | 2.5 min |
| 3. Pion Integration | 0/6 | - | - |
| 4. Validation | 0/5 | - | - |

**Recent Trend:**
- Last 6 plans: 01-05 (5 min), 01-06 (8 min), 02-01 (4 min), 02-02 (~2 min), 02-03 (5 min), 02-04 (2 min)
- Trend: Phase 2 plans averaging faster due to smaller scope

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table.
Recent decisions affecting current work:

- Receiver-side over send-side: Interop requirement - target systems expect REMB
- Delay-based only for v1: Reduce scope, loss-based can be added later
- Standalone core + interceptor adapter: Clean separation allows testing algorithm without Pion
- Use last packet timestamps for inter-group calculations (matches GCC spec)
- Positive delay variation = queue building, negative = queue draining
- Outlier filtering uses capped innovation for variance but uncapped z for state update
- Slow Kalman convergence (500+ iterations) is intentional to avoid noise overreaction
- Trendline numDeltas cap at 60: Prevents runaway values during long sessions
- Exponential smoothing before regression: Matches WebRTC reference behavior
- Track overuse region separately from hypothesis state: Enables proper sustained overuse detection
- Asymmetric K_u/K_d coefficients (0.01/0.00018): Critical for TCP fairness
- Adapter pattern for filter abstraction: delayFilter interface wraps Kalman/Trendline
- Trendline detects TRENDS not absolute delays: constant input yields slope toward zero
- **[NEW 02-01]** Use slice for RateStats samples (simpler than ring buffer, sufficient for 1s window)
- **[NEW 02-01]** Return ok=false when elapsed < 1ms (avoids division precision issues)
- **[NEW 02-02]** Multiplicative decrease uses measured incoming rate, not current estimate (GCC spec)
- **[NEW 02-02]** Elapsed time capped at 1 second to prevent huge jumps after idle
- **[NEW 02-02]** Ratio constraint: estimate <= 1.5 * incomingRate to prevent divergence
- **[NEW 02-03]** Use pion/rtcp for REMB encoding (battle-tested mantissa+exponent implementation)
- **[NEW 02-04]** Immediate REMB only on decrease, not increase (prioritize congestion response)
- **[NEW 02-04]** 3% default threshold balances responsiveness with packet overhead

### Pending Todos

None yet.

### Blockers/Concerns

None yet.

## Session Continuity

Last session: 2026-01-22T16:20:07Z
Stopped at: Completed 02-04-PLAN.md
Resume file: None

---

## Quick Reference

**Next action:** `/gsd:execute-plan 02-05` (Pion Interceptor integration)

**Phase 1 Complete:**
- Delay measurement with timestamp parsing [COMPLETED in 01-01]
- Burst grouping for bursty video traffic [COMPLETED in 01-02]
- Kalman filter for noise reduction [COMPLETED in 01-03]
- Trendline estimator as alternative filter [COMPLETED in 01-04]
- Overuse detector with adaptive threshold [COMPLETED in 01-05]
- Pipeline integration with DelayEstimator [COMPLETED in 01-06]

**Phase 2 Progress:**
- Incoming bitrate measurement (RateStats) [COMPLETED in 02-01]
- AIMD rate controller [COMPLETED in 02-02]
- REMB message generation [COMPLETED in 02-03]
- REMB scheduling [COMPLETED in 02-04]
- Pion Interceptor integration [PENDING 02-05]
- End-to-end integration [PENDING 02-06]

**Phase 1 API Surface:**
- `DelayEstimator` - Main entry point
- `OnPacket(PacketInfo) BandwidthUsage` - Process packet, get congestion state
- `SetCallback(StateChangeCallback)` - Get notified on state changes
- `BwNormal`, `BwUnderusing`, `BwOverusing` - Congestion states

**Phase 2 API Surface (in progress):**
- `RateStats` - Sliding window bitrate measurement
- `NewRateStats(config) -> Update(bytes, time) -> Rate(time) -> (bps, ok)`
- `RateController` - AIMD rate control state machine
- `NewRateController(config) -> Update(signal, incomingRate, time) -> estimate`
- `BuildREMB(senderSSRC, bitrate, mediaSSRCs)` - Create REMB packets
- `ParseREMB(data)` - Parse REMB for testing
- `REMBScheduler` - REMB timing control
- `NewREMBScheduler(config) -> MaybeSendREMB(estimate, ssrcs, time) -> (packet, sent, err)`

**Critical pitfalls handled in Phase 1:**
- Adaptive threshold required (static causes TCP starvation) [HANDLED]
- 24-bit timestamp wraparound at 64s [HANDLED]
- Burst grouping for video traffic [HANDLED]
- Monotonic time only (no wall clock) [HANDLED]

**Critical pitfalls handled in Phase 2:**
- AIMD decrease uses measured_incoming_rate (NOT current estimate) [02-02]
- Rate increase max limited by max_rate_increase_bps_per_second [02-02]
- Underuse -> hold rate (not increase) [02-02]
- REMB mantissa+exponent encoding [HANDLED by pion/rtcp in 02-03]
- Immediate REMB on decrease only (>=3%), not increase [02-04]
