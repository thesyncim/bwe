# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-01-22)

**Core value:** Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior
**Current focus:** v1.2 E2E Testing - Comprehensive automated testing infrastructure

## Current Position

Phase: 6 - Test Infrastructure Foundation
Plan: 03 of 3 - PHASE COMPLETE
Status: Phase complete
Last activity: 2026-01-23 - Completed quick task 004: Add real E2E BWE test

Progress: [############################..] 56% (v1.0 + v1.1 complete, v1.2 Phase 6 COMPLETE)

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
- Phase 5 COMPLETE (3/3 plans)
- All 11 requirements verified (EXT-01 through VAL-04)
- Duration: 8 min total

**v1.2 Status:**
- Phases 6-10 planned (15 requirements across 5 phases)
- Phase 6: COMPLETE (3/3 plans)

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
- **[05-02]** Custom parse functions fully removed (not deprecated) - new project has no backwards compatibility concerns
- **[05-03]** VAL-04 (Chrome interop) requires manual verification with browser
- **[05-03]** Build error in chrome-interop fixed as prerequisite for VAL-01
- **[06-01]** Server binds to :0 by default for test portability (avoids port conflicts)
- **[06-01]** Non-blocking Start() returns actual bound address
- **[06-01]** Graceful shutdown via context.Context
- **[06-02]** Rod v0.116.2 for browser automation (simpler API than chromedp)
- **[06-02]** WebRTC Chrome flags: use-fake-device-for-media-stream, use-fake-ui-for-media-stream
- **[06-02]** BrowserClient wraps Rod with Navigate, Eval, WaitStable, Close methods
- **[06-03]** Build-tagged e2e/ package isolated from go test ./...
- **[06-03]** TestMain cleanup catches orphaned browsers from panics/failures
- **[06-03]** BrowserClient.Navigate() must cancel timeout on correct page reference

### v1.2 Research Context

Key findings from research/SUMMARY.md:

**Stack:**
- Rod v0.116.2 for browser automation (simpler than chromedp, zero-alloc design)
- Toxiproxy v2.12.0 for network simulation (TCP proxy with latency/bandwidth/jitter)
- Docker chromedp/headless-shell for CI Chrome
- Pion vnet for UDP simulation (Toxiproxy is TCP-only)

**Architecture:**
- `e2e/` directory with `//go:build e2e` tags
- `pkg/bwe/testutil/browser.go` - BrowserClient wrapper
- `pkg/bwe/testutil/network.go` - Toxiproxy helpers
- Refactor `cmd/chrome-interop/` to importable server package

**Critical pitfalls to address:**
- Chrome version pinning in CI (prevent version mismatch)
- Browser cleanup with defer patterns (prevent orphaned processes)
- ICE timeouts increased for CI (30s disconnected, 60s failed)
- Deterministic network simulation (seeded randomness)

### Pending Todos

None yet.

### Blockers/Concerns

None - v1.2 roadmap ready for Phase 6 planning.

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 001 | Fix BWE still working when REMB disabled (remove TWCC) | 2026-01-23 | 4e13282 | [001-fix-bwe-still-works-when-remb-disabled](./quick/001-fix-bwe-still-works-when-remb-disabled/) |
| 002 | SDP munging to remove transport-cc (force REMB-only) | 2026-01-23 | 6aedfda | [002-sdp-munge-remove-transport-cc](./quick/002-sdp-munge-remove-transport-cc/) |
| 003 | Fix REMB logging in chrome-interop (OnREMB callback) | 2026-01-23 | 922c120 | [003-fix-remb-logging](./quick/003-fix-remb-logging/) |
| 004 | Add real E2E BWE test (TestChrome_BWERespondsToREMB) | 2026-01-23 | 5cba923 | [004-add-real-e2e-bwe-test](./quick/004-add-real-e2e-bwe-test/) |

## Session Continuity

Last session: 2026-01-23
Stopped at: Completed quick task 004 - Real E2E BWE test
Resume file: None
Next action: Begin Phase 7 (Network Simulation)

---

## Quick Reference

**Project Status:** v1.0 COMPLETE - v1.1 COMPLETE - v1.2 IN PROGRESS

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

**v1.1 COMPLETE (Phase 5):**
- Use pion/rtp.AbsSendTimeExtension (EXT-01) [COMPLETED in 05-01]
- Use pion/rtp.AbsCaptureTimeExtension (EXT-02) [COMPLETED in 05-01]
- Remove custom ParseAbsSendTime() (EXT-03) [COMPLETED - fully removed]
- Remove custom ParseAbsCaptureTime() (EXT-04) [COMPLETED - fully removed]
- Retain UnwrapAbsSendTime() (KEEP-01) [VERIFIED in 05-03]
- Retain FindExtensionID() helpers (KEEP-02) [VERIFIED in 05-03]
- Retain custom inter-group delay calculation (KEEP-03) [VERIFIED in 05-03]
- All existing tests pass (VAL-01) [VERIFIED in 05-03]
- No allocation regression (VAL-02) [VERIFIED in 05-03]
- 24-hour soak test passes (VAL-03) [VERIFIED in 05-03]
- Chrome interop still works (VAL-04) [MANUAL - requires browser]

**v1.2 IN PROGRESS (Phases 6-10):**

| Phase | Goal | Requirements | Status |
|-------|------|--------------|--------|
| 6 | Test Infrastructure Foundation | (foundational) | COMPLETE (3/3) |
| 7 | Network Simulation | NET-01, NET-02, NET-03, NET-04 | Pending |
| 8 | Browser Automation | BROWSER-01, BROWSER-02, BROWSER-03 | Pending |
| 9 | Integration Tests | INT-01, INT-02, INT-03, INT-04 | Pending |
| 10 | CI Integration | CI-01, CI-02, CI-03, CI-04 | Pending |

## v1.0 MILESTONE COMPLETE

All validation requirements verified:

| Requirement | Status | Plan | Description |
|-------------|--------|------|-------------|
| PERF-01 | PASS | 04-01 | 0 allocs/op for core estimator |
| VALID-01 | PASS | 04-02 | Reference trace infrastructure |
| VALID-02 | PASS | 04-04 | Chrome interop (REMB accepted) |
| VALID-03 | PASS | 04-03 | TCP fairness (no starvation) |
| VALID-04 | PASS | 04-05 | 24-hour soak (no leaks/panics) |

## v1.1 MILESTONE COMPLETE

**Phase 5 Progress:**

| Plan | Name | Status | Duration |
|------|------|--------|----------|
| 05-01 | Pion extension parsing | COMPLETE | 2 min |
| 05-02 | Deprecation comments | COMPLETE | 2 min |
| 05-03 | Validation | COMPLETE | 4 min |

**v1.1 Requirements Verified:**

| Requirement | Status | Plan | Description |
|-------------|--------|------|-------------|
| EXT-01 | PASS | 05-01 | Use pion/rtp.AbsSendTimeExtension |
| EXT-02 | PASS | 05-01 | Use pion/rtp.AbsCaptureTimeExtension |
| EXT-03 | PASS | 05-02 | Remove ParseAbsSendTime() |
| EXT-04 | PASS | 05-02 | Remove ParseAbsCaptureTime() |
| KEEP-01 | PASS | 05-03 | UnwrapAbsSendTime unchanged |
| KEEP-02 | PASS | 05-03 | FindExtensionID helpers unchanged |
| KEEP-03 | PASS | 05-03 | Inter-group delay calculation unchanged |
| VAL-01 | PASS | 05-03 | All tests pass (behavioral equivalence) |
| VAL-02 | PASS | 05-03 | 0 allocs/op maintained |
| VAL-03 | PASS | 05-03 | 1349 wraparounds, <4MB heap |
| VAL-04 | MANUAL | 05-03 | Chrome interop (browser required) |

---

## Phase 6 COMPLETE

**Phase 6 Progress:**

| Plan | Name | Status | Duration |
|------|------|--------|----------|
| 06-01 | Server package refactor | COMPLETE | 3 min |
| 06-02 | BrowserClient wrapper | COMPLETE | 2 min |
| 06-03 | E2E test scaffolding | COMPLETE | 4 min |

**Phase 6 Deliverables:**

- Importable server package at `bwe/cmd/chrome-interop/server`
- BrowserClient wrapper at `pkg/bwe/testutil/browser.go`
- Build-tagged e2e/ package with TestChrome_CanConnect smoke test
- TestMain cleanup for orphaned browser processes

---

*Last updated: 2026-01-23 - Quick task 004 complete*
