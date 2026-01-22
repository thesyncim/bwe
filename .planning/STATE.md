# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-01-22)

**Core value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior
**Current focus:** Phase 2 COMPLETE - Ready for Phase 3 Pion Integration

## Current Position

Phase: 2 of 4 (Rate Control & REMB) - COMPLETE
Plan: 6 of 6 in current phase
Status: Phase complete
Last activity: 2026-01-22 - Completed 02-06-PLAN.md (End-to-end integration)

Progress: [████████████░░░░░░░░░░░] 52% (12/23 plans)

## Performance Metrics

**Velocity:**
- Total plans completed: 12
- Average duration: 3.6 min
- Total execution time: 43 min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Foundation | 6/6 | 23 min | 3.8 min |
| 2. Rate Control | 6/6 | 20 min | 3.3 min |
| 3. Pion Integration | 0/6 | - | - |
| 4. Validation | 0/5 | - | - |

**Recent Trend:**
- Last 6 plans: 02-01 (4 min), 02-02 (~2 min), 02-03 (5 min), 02-04 (2 min), 02-05 (3 min), 02-06 (7 min)
- Trend: Phase 2 complete with 6/6 plans executed

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
- **[02-01]** Use slice for RateStats samples (simpler than ring buffer, sufficient for 1s window)
- **[02-01]** Return ok=false when elapsed < 1ms (avoids division precision issues)
- **[02-02]** Multiplicative decrease uses measured incoming rate, not current estimate (GCC spec)
- **[02-02]** Elapsed time capped at 1 second to prevent huge jumps after idle
- **[02-02]** Ratio constraint: estimate <= 1.5 * incomingRate to prevent divergence
- **[02-03]** Use pion/rtcp for REMB encoding (battle-tested mantissa+exponent implementation)
- **[02-04]** Immediate REMB only on decrease, not increase (prioritize congestion response)
- **[02-04]** 3% default threshold balances responsiveness with packet overhead
- **[02-05]** BandwidthEstimator wires components without adding complexity
- **[02-05]** SSRC tracking via map for O(1) deduplication
- **[NEW 02-06]** REMB scheduler is optional via SetREMBScheduler
- **[NEW 02-06]** MaybeBuildREMB includes all tracked SSRCs in REMB packet
- **[NEW 02-06]** lastPacketTime tracking for REMB scheduling convenience

### Pending Todos

None yet.

### Blockers/Concerns

None - Phase 2 complete and ready for Phase 3.

## Session Continuity

Last session: 2026-01-22T16:30:51Z
Stopped at: Completed 02-06-PLAN.md (Phase 2 complete)
Resume file: None

---

## Quick Reference

**Next action:** `/gsd:execute-plan 03-01` (Start Phase 3: Pion Integration)

**Phase 1 COMPLETE:**
- Delay measurement with timestamp parsing [COMPLETED in 01-01]
- Burst grouping for bursty video traffic [COMPLETED in 01-02]
- Kalman filter for noise reduction [COMPLETED in 01-03]
- Trendline estimator as alternative filter [COMPLETED in 01-04]
- Overuse detector with adaptive threshold [COMPLETED in 01-05]
- Pipeline integration with DelayEstimator [COMPLETED in 01-06]

**Phase 2 COMPLETE:**
- Incoming bitrate measurement (RateStats) [COMPLETED in 02-01]
- AIMD rate controller [COMPLETED in 02-02]
- REMB message generation [COMPLETED in 02-03]
- REMB scheduling [COMPLETED in 02-04]
- BandwidthEstimator API [COMPLETED in 02-05]
- End-to-end integration [COMPLETED in 02-06]

**Phase 1 API Surface:**
- `DelayEstimator` - Main entry point
- `OnPacket(PacketInfo) BandwidthUsage` - Process packet, get congestion state
- `SetCallback(StateChangeCallback)` - Get notified on state changes
- `BwNormal`, `BwUnderusing`, `BwOverusing` - Congestion states

**Phase 2 API Surface (COMPLETE):**
- `RateStats` - Sliding window bitrate measurement
- `NewRateStats(config) -> Update(bytes, time) -> Rate(time) -> (bps, ok)`
- `RateController` - AIMD rate control state machine
- `NewRateController(config) -> Update(signal, incomingRate, time) -> estimate`
- `BuildREMB(senderSSRC, bitrate, mediaSSRCs)` - Create REMB packets
- `ParseREMB(data)` - Parse REMB for testing
- `REMBScheduler` - REMB timing control
- `NewREMBScheduler(config) -> MaybeSendREMB(estimate, ssrcs, time) -> (packet, sent, err)`
- `BandwidthEstimator` - Main entry point combining all components
- `NewBandwidthEstimator(config, clock) -> OnPacket(pkt) -> estimate`
- `GetEstimate()`, `GetSSRCs()`, `GetCongestionState()`, `GetRateControlState()`
- `GetIncomingRate()`, `Reset()`
- `SetREMBScheduler(*REMBScheduler)` - Attach REMB scheduler
- `MaybeBuildREMB(time.Time) ([]byte, bool, error)` - Build REMB if needed
- `GetLastPacketTime() time.Time` - Get arrival time of last packet

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
- Standalone core library with no Pion dependencies [02-05]
- Multi-SSRC aggregation: single estimate for all streams [02-06]

**Phase 2 Requirements Verified:**
All 12 Phase 2 requirements verified in TestPhase2_RequirementsVerification:
- CORE-01 through CORE-04 (Standalone API)
- RATE-01 through RATE-04 (AIMD controller)
- REMB-01 through REMB-04 (REMB packets)
