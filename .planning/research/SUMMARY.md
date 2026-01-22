# Project Research Summary

**Project:** GCC Receiver-Side BWE - Pion Type Adoption (v1.1)
**Domain:** RTP/RTCP packet handling for bandwidth estimation
**Researched:** 2026-01-22
**Confidence:** HIGH

## Executive Summary

This research evaluates adopting Pion's native RTP/RTCP types to reduce maintenance burden and improve interoperability in the BWE library. The current implementation is already pragmatically using `pion/rtcp` for REMB marshalling (added in Phase 2), which established a precedent: domain-level dependencies (wire formats, packet types) are acceptable in core, while infrastructure dependencies (interceptors, PeerConnection) stay in the adapter layer.

**Recommended approach:** Adopt Pion extension types (`AbsSendTimeExtension`, `AbsCaptureTimeExtension`) to replace custom parsing in `pkg/bwe/timestamp.go`. This reduces ~35 lines of code, offloads maintenance of RFC-compliant parsing to Pion, and improves type safety. However, **critical wraparound handling must remain custom** — Pion's extension types parse raw timestamps but don't handle 64-second wraparound detection that BWE depends on. The current `UnwrapAbsSendTime()` logic has been battle-tested through 24-hour soak tests and must be preserved.

**Key risks are low:** This is refactoring working code, not building new functionality. The main risk is introducing allocation regressions in the hot path (currently 0 allocs/op). Mitigation: comprehensive benchmark suite validates zero-allocation property before/after changes. Chrome interop is already verified, and REMB handling already uses Pion types successfully.

## Key Findings

### Recommended Stack

**Current state:** Already using Pion v1 packages (`pion/rtcp@v1.2.16`, `pion/rtp@v1.10.0`, `pion/interceptor@v0.1.43`). No version upgrades required.

**Core technologies:**
- **`pion/rtp.AbsSendTimeExtension`**: Replaces custom `ParseAbsSendTime()` — battle-tested RFC implementation with built-in validation
- **`pion/rtp.AbsCaptureTimeExtension`**: Replaces custom `ParseAbsCaptureTime()` — handles optional clock offset field for future-proofing
- **`pion/rtcp.ReceiverEstimatedMaximumBitrate`**: Already in use for REMB — 9 validation checks, bitrate clamping, mantissa/exponent encoding

**Keep current implementations:**
- **REMB wrapper (`REMBPacket`)**: Provides domain-specific `uint64` API instead of Pion's `float32` — minimal wrapper with value
- **Extension ID helpers**: `FindAbsSendTimeID()`, `FindAbsCaptureTimeID()` — convenience functions not provided by Pion
- **Timestamp wraparound logic**: `UnwrapAbsSendTime()` — critical 24-bit wraparound handling not in Pion, validated through 24-hour soak tests

### Expected Features

**Must adopt (table stakes):**
- Extension type marshalling/unmarshalling — Pion handles edge cases (buffer validation, RFC compliance) better than custom code
- Type safety for RTP extensions — using typed structs instead of raw byte slicing reduces bugs

**Should adopt (reduce maintenance):**
- `AbsSendTimeExtension.Unmarshal()` — drops 20 lines of custom parsing, delegates to Pion
- `AbsCaptureTimeExtension.Unmarshal()` — drops 25 lines, adds clock offset support for free

**Defer (keep custom):**
- Timestamp wraparound detection — Pion doesn't provide this, current implementation is well-tested
- Duration conversion helpers — BWE-specific utilities (`AbsSendTimeToDuration()`, etc.)
- REMB scheduling logic — current `REMBScheduler` works, no Pion equivalent

### Architecture Approach

The "standalone core" principle has already been relaxed pragmatically. In Phase 2, `pion/rtcp` was added to `pkg/bwe/remb.go` because reimplementing mantissa/exponent REMB encoding would be error-prone. This established the boundary: **accept domain dependencies (packet formats, wire protocols), reject infrastructure dependencies (goroutines, state, network)**.

**Major components:**
1. **Core algorithm (`pkg/bwe`)** — GCC logic remains pure (InterArrival, Kalman, AIMD), but packet handling can use Pion domain types
2. **REMB marshalling** — Already uses `pion/rtcp`, status quo validated through Chrome interop
3. **Extension parsing** — Currently custom parsing in `timestamp.go`, can adopt Pion types while preserving wraparound logic
4. **Interceptor adapter (`pkg/bwe/interceptor`)** — Pion integration layer, already uses Pion infrastructure types

### Critical Pitfalls

1. **Allocation regression in hot path** — Current code achieves 0 allocs/op in `OnPacket()`. Switching to Pion must preserve this through careful API usage (stack allocation, no pointer returns). Mitigation: benchmark suite enforces zero-allocation constraint.

2. **REMB bitrate precision loss** — Pion uses `float32` for bitrate vs current `uint64`. At high bitrates (>1 Gbps), float32 precision degrades. Mitigation: existing tests validate <1% encoding error, Pion passes these tests (mantissa/exponent format preserves precision).

3. **Timestamp wraparound behavior changes** — Current `UnwrapAbsSendTime()` handles 24-bit wraparound (64-second cycle) with validated half-range comparison. Pion's extension types don't provide this. Mitigation: **keep custom wraparound logic** — this is non-negotiable, validated through 24-hour soak tests.

4. **Extension ID race during initialization** — Extension IDs set via `BindRemoteStream()` but packets may arrive first. Current code handles with atomic `CompareAndSwap`, first few packets are skipped (acceptable). No change needed.

5. **RTCP compound packet handling** — `rtcp.Unmarshal()` returns `[]rtcp.Packet` slice, not single packet. Current code already handles this correctly by passing slice to writer. Verify this doesn't break during refactoring.

## Implications for Roadmap

This is a **single-phase refactoring milestone**, not a multi-phase project. Suggested structure:

### Phase 1: Adopt Extension Types (v1.1 Goal)

**Rationale:** Low-risk adoption of Pion parsing types, reduces maintenance burden with minimal changes. REMB already uses Pion successfully, extending this to RTP extensions follows established pattern.

**Delivers:**
- Replace `ParseAbsSendTime()` with `rtp.AbsSendTimeExtension.Unmarshal()`
- Replace `ParseAbsCaptureTime()` with `rtp.AbsCaptureTimeExtension.Unmarshal()`
- Deprecate custom parsing functions (keep for backward compatibility)
- Net: -35 lines of code, 2 fewer functions to maintain

**Addresses features:**
- Type safety for extension parsing
- RFC compliance delegated to Pion
- Future clock offset support (abs-capture-time)

**Avoids pitfalls:**
- Keep `UnwrapAbsSendTime()` custom logic (wraparound handling)
- Benchmark suite enforces zero-allocation constraint
- Precision tests validate REMB encoding equivalence
- Integration tests verify Chrome interop unchanged

**Implementation tasks:**
1. Update `pkg/bwe/interceptor/interceptor.go` to use Pion unmarshalling
2. Mark `ParseAbsSendTime()`, `ParseAbsCaptureTime()` as deprecated
3. Run benchmark suite, verify 0 allocs/op preserved
4. Run validation tests, verify estimates within 10% of reference
5. Run Chrome interop test, verify REMB accepted

### Optional Phase 2: Remove Deprecated Functions (Future)

**Rationale:** After validation period (e.g., one release cycle), clean up deprecated parsing functions.

**Delivers:** Remove ~45 lines of deprecated code

**Deferred because:** Low priority cleanup, no functional benefit

### Phase Ordering Rationale

- **Single phase is sufficient:** This is a refactor, not feature development. Changes are localized to extension parsing.
- **Low risk approach:** Pion types are drop-in replacements for parsing, current tests validate equivalence.
- **Preserves critical logic:** Wraparound detection stays custom, avoiding the main risk area.
- **Incremental adoption:** REMB already uses Pion (Phase 2 established precedent), extension types are natural extension.

### Research Flags

**No additional research needed:**
- Pion APIs are well-documented with official godocs
- Current implementation provides clear comparison baseline
- Benchmark data establishes allocation constraints
- Soak test validates wraparound handling

**Standard patterns apply:**
- Extension parsing is well-understood RTP concept
- Pion types follow RFC specifications exactly
- Test-driven refactoring: existing tests define correctness

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Pion versions verified in go.mod, APIs stable since 2021 |
| Features | HIGH | Direct codebase comparison, existing tests validate behavior |
| Architecture | HIGH | Boundary already crossed in Phase 2, precedent established |
| Pitfalls | HIGH | Benchmark data confirms allocation baselines, soak tests validate wraparound |

**Overall confidence:** HIGH

### Gaps to Address

**No major gaps identified.** Minor considerations:

- **Float32 precision testing:** Existing tests cover up to 10 Gbps, validate Pion's float32 encoding preserves <1% precision. Already passing.
- **Compound RTCP handling:** Current code appears correct (passes slice to writer), but verify explicitly during refactoring.
- **Extension profile edge cases:** Pion has known limitation with OneByte → TwoByte upgrade, but BWE only reads extensions (never creates), so not affected.

## Sources

### Primary (HIGH confidence)
- **Codebase analysis:**
  - `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/remb.go` — Current Pion usage
  - `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp.go` — Custom parsing to replace
  - `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp_test.go` — Wraparound validation (15 test cases)
  - `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/benchmark_test.go` — Allocation baselines (0 allocs/op)

- **Pion official documentation:**
  - [pion/rtcp v1.2.16](https://pkg.go.dev/github.com/pion/rtcp) — ReceiverEstimatedMaximumBitrate API
  - [pion/rtp v1.10.0](https://pkg.go.dev/github.com/pion/rtp) — AbsSendTimeExtension, AbsCaptureTimeExtension
  - [GitHub - pion/rtcp](https://github.com/pion/rtcp) — Source code inspection
  - [GitHub - pion/rtp](https://github.com/pion/rtp) — Extension implementation details

### Secondary (MEDIUM confidence)
- **Architectural guidance:**
  - [Clean Architecture in Go](https://threedots.tech/post/introducing-clean-architecture/) — Domain vs infrastructure dependencies
  - [Hexagonal Architecture in Go](https://skoredin.pro/blog/golang/hexagonal-architecture-go) — Boundary design patterns
  - [Pion WebRTC Big Ideas](https://github.com/pion/webrtc/wiki/Big-Ideas) — Interceptor design rationale

### Project History
- `.planning/phases/02-rate-control-remb/02-03-PLAN.md` — Original REMB Pion adoption decision
- `.planning/phases/04-optimization-validation/04-05-PLAN.md` — 24-hour soak test specification
- `.planning/ROADMAP.md` — Phase 4 completion, soak test validation

---
**Research completed:** 2026-01-22
**Ready for roadmap:** Yes
