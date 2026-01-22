# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-01-22)

**Core value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior
**Current focus:** Phase 4 Optimization & Validation - VALID-03 TCP Fairness verified

## Current Position

Phase: 4 of 4 (Optimization & Validation)
Plan: 3 of 5 in current phase
Status: In progress
Last activity: 2026-01-22 - Completed 04-03-PLAN.md (TCP Fairness Simulation)

Progress: [████████████████████░░░] 87% (21/23 plans)

## Performance Metrics

**Velocity:**
- Total plans completed: 21
- Average duration: 3.6 min
- Total execution time: 79 min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Foundation | 6/6 | 23 min | 3.8 min |
| 2. Rate Control | 6/6 | 20 min | 3.3 min |
| 3. Pion Integration | 6/6 | 24 min | 4.0 min |
| 4. Validation | 3/5 | 12 min | 4.0 min |

**Recent Trend:**
- Last 6 plans: 03-04 (4 min), 03-05 (3 min), 03-06 (5 min), 04-01 (4 min), 04-02 (4 min), 04-03 (4 min)
- Trend: Phase 4 in progress. TCP fairness (VALID-03) verified.

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
- **[02-06]** REMB scheduler is optional via SetREMBScheduler
- **[02-06]** MaybeBuildREMB includes all tracked SSRCs in REMB packet
- **[02-06]** lastPacketTime tracking for REMB scheduling convenience
- **[03-01]** Extension ID 0 means "not found" - callers must handle gracefully
- **[03-01]** atomic.Value for lastPacketTime enables thread-safe concurrent access
- **[03-01]** Unexported streamState type - internal implementation detail
- **[03-02]** First stream to provide extension ID wins (CompareAndSwap)
- **[03-02]** abs-send-time preferred over abs-capture-time when both available
- **[03-02]** Packets without timing extensions silently skipped
- **[03-02]** Stream state updated on every packet for timeout detection
- **[03-03]** REMB scheduler created in NewBWEInterceptor, attached to estimator immediately
- **[03-03]** rembLoop started on BindRTCPWriter (not on constructor)
- **[03-03]** RTCPWriter.Write takes []rtcp.Packet, requires unmarshal from MaybeBuildREMB bytes
- **[03-03]** Ignore write errors in maybeSendREMB (network issues shouldn't stop loop)
- **[03-04]** sync.Once ensures cleanup loop starts only once across multiple streams
- **[03-04]** 1-second cleanup interval for 2-second timeout (sufficient granularity)
- **[03-04]** Cleanup loop started in BindRemoteStream, not constructor
- **[03-05]** Separate factory options from interceptor options (WithFactory* prefix)
- **[03-05]** Factory creates independent BandwidthEstimator per interceptor
- **[03-05]** id parameter from Pion ignored (not needed for our implementation)
- **[NEW 03-06]** Pool.New creates zero-value PacketInfo (clean state on first get)
- **[NEW 03-06]** putPacketInfo resets all fields before returning to pool
- **[NEW 03-06]** OnPacket takes PacketInfo by value, so dereference pooled pointer
- **[NEW 03-06]** Integration tests use short REMB intervals (50ms) for faster execution
- **[NEW 04-03]** 30ms extra delay for congestion simulation (matches existing test patterns)
- **[NEW 04-03]** Three-phase TCP fairness methodology: 30s stable, 60s congested, 30s recovery
- **[NEW 04-03]** Fair share thresholds: 10% min (no starvation), 90% max (appropriate backoff)
- **[NEW 04-03]** Sustained congestion test runs 5+ simulated minutes to detect gradual starvation

### Pending Todos

None yet.

### Blockers/Concerns

None - Phase 4 in progress.

## Session Continuity

Last session: 2026-01-22T19:06:00Z
Stopped at: Completed 04-03-PLAN.md (TCP Fairness Simulation)
Resume file: None

---

## Quick Reference

**Next action:** `/gsd:execute-plan 04-04` (Convergence speed benchmarks)

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

**Phase 3 COMPLETE:**
- Interceptor setup with extension helpers [COMPLETED in 03-01]
- Core interceptor implementation [COMPLETED in 03-02]
- BindRTCPWriter and REMB Loop [COMPLETED in 03-03]
- Stream timeout and cleanup [COMPLETED in 03-04]
- InterceptorFactory for registry integration [COMPLETED in 03-05]
- Integration tests and sync.Pool optimization [COMPLETED in 03-06]

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

**Phase 3 API Surface (COMPLETE):**
- `pkg/bwe/interceptor` package for Pion integration
- `AbsSendTimeURI`, `AbsCaptureTimeURI` - Extension URI constants
- `FindExtensionID(exts, uri)` - Extension ID lookup
- `FindAbsSendTimeID(exts)`, `FindAbsCaptureTimeID(exts)` - Convenience functions
- `streamState` (unexported) - Per-stream state tracking
- `BWEInterceptor` - Main interceptor type embedding NoOp
- `NewBWEInterceptor(estimator, opts...)` - Constructor with options
- `BindRemoteStream(info, reader)` - Wraps RTPReader for packet observation
- `BindRTCPWriter(writer)` - Captures writer and starts REMB loop
- `Close()` - Signals shutdown and waits for all goroutines
- `WithREMBInterval(d)`, `WithSenderSSRC(ssrc)` - Interceptor configuration options
- `BWEInterceptorFactory` - Factory for interceptor.Registry integration
- `NewBWEInterceptorFactory(opts...)` - Factory constructor
- `WithInitialBitrate`, `WithMinBitrate`, `WithMaxBitrate` - Factory bitrate options
- `WithFactoryREMBInterval`, `WithFactorySenderSSRC` - Factory REMB options
- `getPacketInfo()`, `putPacketInfo()` - sync.Pool for PacketInfo (PERF-02)

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

**Critical pitfalls handled in Phase 3:**
- Stream timeout with graceful cleanup after 2s inactivity [03-04]
- Close() waits for all goroutines to complete [03-04]
- Factory creates independent estimators (no shared state) [03-05]
- sync.Pool for PacketInfo reduces GC pressure [03-06]

**Phase 2 Requirements Verified:**
All 12 Phase 2 requirements verified in TestPhase2_RequirementsVerification:
- CORE-01 through CORE-04 (Standalone API)
- RATE-01 through RATE-04 (AIMD controller)
- REMB-01 through REMB-04 (REMB packets)

**Phase 3 Requirements Verified:**
All 7 Phase 3 requirements verified in TestPhase3_RequirementsVerification:
- TIME-04: Auto-detect extension IDs from SDP negotiation
- PION-01: Implement Pion Interceptor interface
- PION-02: Implement BindRemoteStream for RTP packet observation
- PION-03: Implement BindRTCPWriter for REMB packet output
- PION-04: Handle stream timeout with graceful cleanup after 2s inactivity
- PION-05: Provide InterceptorFactory for PeerConnection integration
- PERF-02: Use sync.Pool for packet metadata structures

**Phase 4 Validation Progress:**
- VALID-01: Delay detector accuracy [04-01 - COMPLETED]
- VALID-02: Rate controller stability [04-02 - COMPLETED]
- VALID-03: TCP fairness simulation [04-03 - COMPLETED]
  - Three-phase test (stable -> congested -> recovery)
  - Adaptive threshold K_u/K_d asymmetry (~55:1 ratio) verified
  - No gradual starvation under 5+ minutes congestion
  - Stable behavior under rapid transitions
- VALID-04: Convergence speed benchmarks [04-04 - PENDING]
- VALID-05: Final validation report [04-05 - PENDING]
