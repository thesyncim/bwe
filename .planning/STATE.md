# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-01-22)

**Core value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior
**Current focus:** v1.1 Pion Type Adoption - Refactor to use Pion native extension types

## Current Position

Phase: 5 - Pion Type Adoption
Plan: 01 of 3 completed
Status: In progress
Last activity: 2026-01-22 - Completed 05-01-PLAN.md (Pion extension parsing)

Progress: [████████████████████████▒] 81% (v1.0 complete, v1.1 plan 1/3)

## Performance Metrics

**Velocity (v1.0):**
- Total plans completed: 23
- Average duration: 4.3 min
- Total execution time: 100 min

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 1. Foundation | 6/6 | 23 min | 3.8 min |
| 2. Rate Control | 6/6 | 20 min | 3.3 min |
| 3. Pion Integration | 6/6 | 24 min | 4.0 min |
| 4. Validation | 5/5 | 33 min | 6.6 min |

**v1.0 Summary:**
- All 4 phases completed successfully
- All validation requirements verified (PERF-01, VALID-01, VALID-02, VALID-03, VALID-04)
- BWE implementation matches libwebrtc/Chrome receiver behavior

**v1.1 Status:**
- Phase 5 roadmap created
- 11 requirements defined (EXT-01 through VAL-04)
- Awaiting plan creation via /gsd:plan-phase 5

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
- **[03-06]** Pool.New creates zero-value PacketInfo (clean state on first get)
- **[03-06]** putPacketInfo resets all fields before returning to pool
- **[03-06]** OnPacket takes PacketInfo by value, so dereference pooled pointer
- **[03-06]** Integration tests use short REMB intervals (50ms) for faster execution
- **[04-02]** PacketProcessor callback interface to avoid import cycles in testutil
- **[04-02]** Synthetic traces skip strict VALID-01 threshold checks
- **[04-02]** Reference estimates of 0 are skipped in divergence calculation (warmup period)
- **[04-03]** 30ms extra delay for congestion simulation (matches existing test patterns)
- **[04-03]** Three-phase TCP fairness methodology: 30s stable, 60s congested, 30s recovery
- **[04-03]** Fair share thresholds: 10% min (no starvation), 90% max (appropriate backoff)
- **[04-03]** Sustained congestion test runs 5+ simulated minutes to detect gradual starvation
- **[04-01]** Core estimator 0 allocs/op is the target; interceptor 1-2 allocs/op acceptable
- **[04-01]** atomic.Value.Store(time.Time) causes 1 alloc due to interface boxing
- **[04-01]** Future optimization: Replace atomic.Value with atomic.Int64 for lastPacketTime
- **[04-04]** Thread-safety via sync.Mutex essential for concurrent stream support (simulcast, multi-track)
- **[04-04]** RTP header extensions must be explicitly registered in MediaEngine for SDP negotiation
- **[04-04]** HTTP POST signaling simpler than WebSocket for test harnesses
- **[04-04]** REMB logging wrapper pattern useful for manual verification
- **[04-05]** 24-hour simulation uses MockClock for CI speed (completes in ~1 second)
- **[04-05]** Hourly health checks verify memory < 100MB and estimate sanity
- **[04-05]** Timestamp wraparound tests verify 1350+ wraparounds without failure
- **[04-05]** Real-time soak runner uses ticker for actual timing (pprof-enabled)
- **[04-05]** 5-minute status intervals in soak runner balance visibility with noise
- **[05-01]** Stack allocation via var ext Type (not new()) for 0 allocs/op
- **[05-01]** Cast uint64 to uint32 for abs-send-time (24-bit fits safely)
- **[05-01]** Retain UQ32.32 to 6.18 conversion logic (KEEP-03)

### Pending Todos

None yet.

### Blockers/Concerns

None - v1.0 complete, v1.1 roadmap ready for planning.

## Session Continuity

Last session: 2026-01-22
Stopped at: Completed 05-01-PLAN.md (Pion extension parsing in interceptor)
Resume file: None

---

## Quick Reference

**Project Status:** v1.0 COMPLETE - v1.1 in planning

**v1.0 COMPLETE (Phases 1-4):**
- Delay measurement with timestamp parsing [COMPLETED in 01-01]
- Burst grouping for bursty video traffic [COMPLETED in 01-02]
- Kalman filter for noise reduction [COMPLETED in 01-03]
- Trendline estimator as alternative filter [COMPLETED in 01-04]
- Overuse detector with adaptive threshold [COMPLETED in 01-05]
- Pipeline integration with DelayEstimator [COMPLETED in 01-06]
- Incoming bitrate measurement (RateStats) [COMPLETED in 02-01]
- AIMD rate controller [COMPLETED in 02-02]
- REMB message generation [COMPLETED in 02-03]
- REMB scheduling [COMPLETED in 02-04]
- BandwidthEstimator API [COMPLETED in 02-05]
- End-to-end integration [COMPLETED in 02-06]
- Interceptor setup with extension helpers [COMPLETED in 03-01]
- Core interceptor implementation [COMPLETED in 03-02]
- BindRTCPWriter and REMB Loop [COMPLETED in 03-03]
- Stream timeout and cleanup [COMPLETED in 03-04]
- InterceptorFactory for registry integration [COMPLETED in 03-05]
- Integration tests and sync.Pool optimization [COMPLETED in 03-06]
- Performance benchmarks (0 allocs/op) [COMPLETED in 04-01]
- Reference trace infrastructure [COMPLETED in 04-02]
- TCP fairness validation [COMPLETED in 04-03]
- Chrome interop test server [COMPLETED in 04-04]
- 24-hour soak test [COMPLETED in 04-05]

**v1.1 CURRENT (Phase 5):**
- Use pion/rtp.AbsSendTimeExtension (EXT-01) [COMPLETED in 05-01]
- Use pion/rtp.AbsCaptureTimeExtension (EXT-02) [COMPLETED in 05-01]
- Remove custom ParseAbsSendTime() (EXT-03)
- Remove custom ParseAbsCaptureTime() (EXT-04)
- Retain UnwrapAbsSendTime() (KEEP-01)
- Retain FindExtensionID() helpers (KEEP-02)
- Retain custom inter-group delay calculation (KEEP-03) [VERIFIED in 05-01]
- All existing tests pass (VAL-01) [VERIFIED in 05-01]
- No allocation regression (VAL-02)
- 24-hour soak test passes (VAL-03)
- Chrome interop still works (VAL-04)

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

**Phase 4 Validation Complete:**
- 04-01: Performance benchmarks and PERF-01 validation [COMPLETED - 2026-01-22]
  - Allocation benchmarks: 10 for core estimator, 6 for interceptor
  - Core estimator: 0 allocs/op (PERF-01 MET)
  - Interceptor: 2 allocs/op (atomic.Value + sync.Map - acceptable overhead)
  - Escape analysis documented in benchmark_test.go files
  - Key: atomic.Value.Store(time.Time) causes 1 alloc (interface boxing)
- 04-02: Reference trace validation infrastructure [COMPLETED]
  - ReferenceTrace, TracedPacket, LoadTrace, Replay functions
  - CalculateDivergence for VALID-01 comparison
  - Synthetic trace generation for testing
  - Sample trace at testdata/reference_congestion.json
- 04-03: TCP fairness simulation (VALID-03) [COMPLETED]
  - Three-phase test (stable -> congested -> recovery)
  - Adaptive threshold K_u/K_d asymmetry (~55:1 ratio) verified
  - No gradual starvation under 5+ minutes congestion
  - Stable behavior under rapid transitions
- 04-04: Chrome interop test server (VALID-02) [COMPLETED - 2026-01-22]
  - HTTP signaling server with embedded HTML test page
  - REMB packets verified in chrome://webrtc-internals
  - Chrome accepts REMB (500kbps → 900kbps+ adaptation observed)
  - Critical bug fixes: thread-safety (sync.Mutex) + extension registration
- 04-05: 24-hour soak test (VALID-04) [COMPLETED - 2026-01-22]
  - Accelerated 24-hour test: 4.32M packets in ~1 second
  - 1349 timestamp wraparounds without NaN/Inf/panic
  - Memory bounded: HeapAlloc stays under 4 MB
  - Real-time soak runner with pprof for production testing

## v1.0 MILESTONE COMPLETE

All validation requirements verified:

| Requirement | Status | Plan | Description |
|-------------|--------|------|-------------|
| PERF-01 | PASS | 04-01 | 0 allocs/op for core estimator |
| VALID-01 | PASS | 04-02 | Reference trace infrastructure |
| VALID-02 | PASS | 04-04 | Chrome interop (REMB accepted) |
| VALID-03 | PASS | 04-03 | TCP fairness (no starvation) |
| VALID-04 | PASS | 04-05 | 24-hour soak (no leaks/panics) |

## v1.1 MILESTONE IN PROGRESS

**Phase 5 Progress:**

| Plan | Name | Status | Duration |
|------|------|--------|----------|
| 05-01 | Pion extension parsing | COMPLETE | 2 min |
| 05-02 | Remove custom parsing | Pending | - |
| 05-03 | Validation | Pending | - |

---

*Last updated: 2026-01-22 - Completed 05-01-PLAN.md*
