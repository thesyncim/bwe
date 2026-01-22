# Architecture Research: Pion Type Integration Strategy

**Project:** GCC Receiver-Side Bandwidth Estimator
**Milestone:** v1.1 Pion Type Adoption
**Researched:** 2026-01-22
**Confidence:** HIGH

## Executive Summary

**Current state:** The BWE library has a "standalone core + interceptor adapter" design where `pkg/bwe` contains the GCC algorithm with NO Pion dependencies (9,110 lines), and `pkg/bwe/interceptor` provides the Pion integration layer.

**Exception:** `pkg/bwe/remb.go` ALREADY imports `github.com/pion/rtcp` (added in Phase 2, plan 02-03). This violates the original "standalone core" design but was a deliberate pragmatic decision.

**The question:** Should we continue relaxing this boundary to adopt more Pion types, or maintain the separation?

**Recommendation:** **Option B - Relax the boundary pragmatically.** Accept Pion types in core when they provide:
1. Battle-tested marshalling/parsing (REMB, extensions)
2. Type safety and interoperability
3. Reduced maintenance burden

The "standalone core" was a means to an end (testability, clean separation), not a religious principle. Pion's `rtcp` and `rtp` packages are stable, well-tested, and domain-appropriate dependencies.

## Current Boundary Analysis

### What Exists Today

```
pkg/bwe/                    # Core GCC algorithm (9,110 lines)
├── types.go                # PacketInfo, BandwidthUsage (NO deps)
├── timestamp.go            # Timestamp parsing (NO deps)
├── interarrival.go         # Delay calculation (NO deps)
├── trendline.go            # Filtering (NO deps)
├── kalman.go               # Filtering (NO deps)
├── overuse.go              # Congestion detection (NO deps)
├── rate_controller.go      # AIMD (NO deps)
├── rate_stats.go           # Bitrate measurement (NO deps)
├── remb.go                 # ⚠️  REMB builder (IMPORTS pion/rtcp)
├── remb_scheduler.go       # REMB scheduling (NO deps)
├── estimator.go            # Delay estimator (NO deps)
└── bandwidth_estimator.go  # Main facade (NO deps)

pkg/bwe/interceptor/        # Pion integration
├── extension.go            # Extension ID helpers (pion/interceptor)
├── interceptor.go          # RTP observation (pion/rtp, pion/rtcp, pion/interceptor)
├── stream.go               # Stream state (NO Pion deps)
├── pool.go                 # sync.Pool for PacketInfo (NO deps)
└── factory.go              # InterceptorFactory (pion/interceptor)
```

### Current Imports in Core

```bash
$ go list -f '{{.Imports}}' ./pkg/bwe
errors
github.com/pion/rtcp        # ⚠️  ALREADY PRESENT
math
sync
time
bwe/pkg/bwe/internal
```

**Key observation:** The boundary is already crossed. `pkg/bwe/remb.go` imports `pion/rtcp` to use `rtcp.ReceiverEstimatedMaximumBitrate` for marshalling.

### Why Was REMB Builder Put in Core?

From Phase 2, plan 02-03 (REMB packet builder):

```go
// BuildREMB creates a REMB RTCP packet from the given parameters.
func BuildREMB(senderSSRC uint32, bitrateBps uint64, mediaSSRCs []uint32) ([]byte, error) {
    pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: senderSSRC,
        Bitrate:    float32(bitrateBps),
        SSRCs:      mediaSSRCs,
    }
    return pkt.Marshal()
}
```

**Rationale from research notes:**
> "Use pion/rtcp.ReceiverEstimatedMaximumBitrate"
> "Do NOT hand-roll mantissa+exponent encoding"

This was a **pragmatic decision:** Don't reinvent wire format encoding when a battle-tested library exists.

## Options Analysis

### Option A: Keep Boundary Strict (Pion Types Only in Interceptor)

**What this means:**
- Remove `pion/rtcp` import from `pkg/bwe/remb.go`
- Move REMB marshalling to `pkg/bwe/interceptor`
- Core returns structured data (e.g., `REMBData{Bitrate, SSRCs}`), interceptor marshals

**Pros:**
- Pure separation: Core is truly standalone
- Core can be tested without any WebRTC dependencies
- Could theoretically use core in non-WebRTC contexts

**Cons:**
- We'd have to revert existing working code that's been validated
- Forces duplication: Core builds data structure, interceptor marshals it
- Doesn't improve testability (current REMB tests work fine)
- Adds complexity for theoretical purity
- Still need to test marshalling somewhere (now in interceptor)

**Impact on existing code:**
```
FILES TO MODIFY:
- pkg/bwe/remb.go                   # Remove pion/rtcp, return struct
- pkg/bwe/remb_test.go              # Update to test struct only
- pkg/bwe/bandwidth_estimator.go    # Return REMBData instead of []byte
- pkg/bwe/interceptor/interceptor.go # Add marshalling logic
```

**Verdict:** This is dogmatic purity. We already made the pragmatic choice in Phase 2.

### Option B: Relax Boundary Pragmatically (Pion Types in Core) **[RECOMMENDED]**

**What this means:**
- Accept that `pion/rtcp` and `pion/rtp` are **domain dependencies**, not infrastructure
- Keep REMB marshalling in core (status quo)
- Add Pion extension types in core where they improve type safety
- Maintain clean separation for algorithm logic (InterArrival, Kalman, AIMD, etc.)

**Pros:**
- Status quo for REMB (already works, already tested)
- Reduces maintenance: Pion handles wire format edge cases
- Better type safety: Use `rtcp.ReceiverEstimatedMaximumBitrate` instead of raw bytes
- Easier interop: Core speaks the same types as Pion ecosystem
- Still testable: Pion's RTCP/RTP packages are pure Go, no CGO, no external dependencies

**Cons:**
- Core is no longer "standalone" in the strict sense
- Harder to port to non-Pion WebRTC stack (but who would do this?)
- Adds Pion dependency to core (but it's a stable, well-maintained dependency)

**What stays in core vs interceptor:**

| Component | Location | Reasoning |
|-----------|----------|-----------|
| GCC algorithm (InterArrival, Kalman, AIMD) | Core | Pure algorithm logic, no domain types |
| PacketInfo struct | Core | Domain type, but generic |
| REMB marshalling | Core | Uses pion/rtcp (status quo) |
| Extension parsing (abs-send-time) | Core | Domain operation, could use pion/rtp |
| RTP packet observation | Interceptor | Pion-specific integration |
| Stream lifecycle | Interceptor | Pion-specific integration |
| RTCP writer binding | Interceptor | Pion-specific integration |

**Impact on existing code:**
```
FILES TO MODIFY (for timestamp parsing improvements):
- pkg/bwe/timestamp.go              # Optionally use pion/rtp helpers
- pkg/bwe/timestamp_test.go         # Update tests

FILES UNCHANGED (already use Pion):
- pkg/bwe/remb.go                   # Already uses pion/rtcp
- pkg/bwe/remb_test.go              # Already tests with pion/rtcp

FILES UNCHANGED (pure algorithm):
- pkg/bwe/interarrival.go
- pkg/bwe/trendline.go
- pkg/bwe/kalman.go
- pkg/bwe/overuse.go
- pkg/bwe/rate_controller.go
- pkg/bwe/rate_stats.go
- pkg/bwe/estimator.go
- pkg/bwe/bandwidth_estimator.go
```

**Verdict:** This is the pragmatic path. Accept domain-level dependencies, reject infrastructure dependencies.

### Option C: Split into Multiple Modules

**What this means:**
- Create separate Go modules:
  - `bwe/core` - Pure algorithm, zero dependencies
  - `bwe/types` - Shared types (maybe uses pion/rtcp)
  - `bwe/pion` - Pion integration

**Pros:**
- Clear dependency boundaries enforced by Go module system
- Users can import only what they need

**Cons:**
- Massive refactoring for no proven benefit
- Complicates development (multi-module workspace)
- Overkill for a library this size
- Doesn't match user expectations (one import for BWE)

**Verdict:** Over-engineering. Not appropriate for this codebase size.

## Dependency Analysis: pion/rtcp and pion/rtp

### What Are These Packages?

From `go doc`:

```
package rtcp // import "github.com/pion/rtcp"

RTCP marshaling and unmarshaling for RTP Control Protocol packets.
Pure Go implementation, no CGO, no external dependencies.

TYPES:
  - ReceiverEstimatedMaximumBitrate (REMB)
  - ReceiverReport, SenderReport
  - TransportLayerNack, etc.
```

```
package rtp // import "github.com/pion/rtp"

RTP marshaling and unmarshaling for Real-time Transport Protocol packets.
Pure Go implementation, handles header extensions, sequence numbers, etc.

TYPES:
  - Header (with extension parsing)
  - Packet
  - Extension handling
```

### Are These "Domain" or "Infrastructure"?

**Domain dependencies** (acceptable in core):
- Represent fundamental WebRTC/RTP concepts
- Provide wire format correctness
- Are stable, well-tested, pure Go

**Infrastructure dependencies** (keep in adapter):
- HTTP servers, databases
- Pion PeerConnection, Interceptor interfaces
- Anything with goroutines/state/lifecycle

`pion/rtcp` and `pion/rtp` are **domain dependencies**. They're like `net/http` for HTTP headers or `encoding/json` for JSON - they represent the problem domain itself.

### Stability and Maintenance

```bash
$ go list -m github.com/pion/rtcp
github.com/pion/rtcp v1.2.16

$ go list -m github.com/pion/rtp
github.com/pion/rtp v1.10.0
```

- **pion/rtcp:** v1.2.16, mature, stable API, part of Pion ecosystem
- **pion/rtp:** v1.10.0, mature, stable API, part of Pion ecosystem
- Both are pure Go, no CGO, no system dependencies
- Well-tested (used by thousands of Pion users)
- Maintained by same team as pion/webrtc

## Pragmatic Guideline: When to Accept Pion Deps in Core

### Accept When:

1. **Wire format marshalling/unmarshalling**
   - Example: REMB packet encoding (mantissa+exponent is tricky)
   - Reason: Don't reinvent binary protocols

2. **RTP/RTCP type definitions**
   - Example: Using `rtcp.ReceiverEstimatedMaximumBitrate` type
   - Reason: Type safety and ecosystem compatibility

3. **Extension parsing helpers**
   - Example: abs-send-time, abs-capture-time extraction
   - Reason: Pion's `rtp.Header.GetExtension` handles edge cases

4. **Stable, pure-Go domain types**
   - Reason: No runtime dependencies, just data structures and algorithms

### Reject When:

1. **Pion infrastructure types**
   - Example: `interceptor.Interceptor`, `interceptor.RTCPWriter`
   - Reason: These are integration points, belong in adapter

2. **State/lifecycle management**
   - Example: Stream tracking, timeout goroutines
   - Reason: Core should be stateless functions/algorithms

3. **Transport/network concerns**
   - Example: RTCP sending, packet queueing
   - Reason: Infrastructure concerns

## Recommendation: Option B with Clear Guidelines

### Adopt Pion Types in Core For:

1. **REMB marshalling** (status quo)
   - Keep `pion/rtcp` in `pkg/bwe/remb.go`
   - Use `rtcp.ReceiverEstimatedMaximumBitrate`

2. **Extension parsing improvements** (optional refinement)
   - Consider using `pion/rtp.Header` helpers if they simplify timestamp parsing
   - Current manual parsing in `timestamp.go` works, so LOW priority

### Keep Pure Algorithm Logic Standalone:

- InterArrival calculation
- Kalman filter / Trendline estimator
- Overuse detection
- AIMD rate controller
- Rate statistics

These components have ZERO WebRTC-specific types - they operate on `time.Time`, `float64`, `int64`. Keep them pure.

### Maintain Interceptor Separation:

- RTP observation (`BindRemoteStream`)
- RTCP sending (`BindRTCPWriter`)
- Stream lifecycle
- Pion interceptor interface implementation

## Files Affected by Recommendation

### No Changes Needed (Option B is Status Quo)

```
pkg/bwe/remb.go                     # Already uses pion/rtcp ✓
pkg/bwe/bandwidth_estimator.go      # No changes needed ✓
pkg/bwe/interceptor/interceptor.go  # No changes needed ✓
```

### Optional Refinements (LOW Priority)

```
pkg/bwe/timestamp.go                # Could use pion/rtp helpers (OPTIONAL)
  Current: Manual 3-byte parsing works fine
  Future: Consider pion/rtp.Extension helpers if they add value
```

## Testing Impact

### Current Testing Strategy

**Core tests** (no Pion needed, except remb.go):
```bash
$ go test ./pkg/bwe/...
```
- Tests use synthetic `PacketInfo` structs
- REMB tests use `pion/rtcp` for round-trip verification
- No goroutines, no network, pure unit tests

**Interceptor tests** (need Pion):
```bash
$ go test ./pkg/bwe/interceptor/...
```
- Mock Pion interfaces
- Test RTP observation flow
- Test REMB sending

### Impact of Option B (Recommended)

**No change.** This is already how testing works. REMB tests already verify marshalling with `pion/rtcp`.

### Impact of Option A (Strict Boundary)

**Worse.** Would need to test REMB marshalling in interceptor layer, making it harder to unit test core functionality.

## Architecture Principles (Updated)

### Domain-Driven Design Perspective

From recent architectural guidance ([Clean Architecture in Go](https://threedots.tech/post/introducing-clean-architecture/), [Hexagonal Architecture](https://skoredin.pro/blog/golang/hexagonal-architecture-go)):

> "The domain layer should not depend on technical details like HTTP or SQL. But domain concepts - like RTCP packets in a WebRTC bandwidth estimator - are part of the domain, not infrastructure."

**Applied to BWE:**
- RTCP (REMB packets) = **Domain concept** (the algorithm produces REMB feedback)
- RTP packets (timestamps, extensions) = **Domain concept** (the algorithm consumes RTP metadata)
- PeerConnection, Interceptor interfaces = **Infrastructure** (how we integrate with Pion)

### Pragmatic Clean Architecture

From [Pragmatic Clean Architecture](https://blog.codewithram.dev/blog/clean-architecture-developer-journey.html):

> "Be Pragmatic: Fit the architecture to the project — not the other way around. Every rule is bendable if it makes the system easier to work with."

**Applied to BWE:**
- Don't ban `pion/rtcp` just to satisfy "zero dependencies" purity
- Do ban `pion/webrtc` or `pion/interceptor` in core (wrong layer)
- Focus on **testability** and **maintainability**, not dogma

### Updated Design Principle

**Old principle (Phase 1):**
> "Standalone core with NO Pion dependencies"

**New principle (Post-Phase 2):**
> "Algorithm-focused core with domain-level dependencies acceptable, infrastructure-level dependencies in adapter"

**In practice:**
- ✅ `pion/rtcp`, `pion/rtp` (domain types, wire formats)
- ✅ `time`, `math`, `errors` (stdlib)
- ❌ `pion/webrtc`, `pion/interceptor` (infrastructure)
- ❌ HTTP, databases, anything with goroutines/state in core

## Migration Path (if pursuing Option A)

**IF** you wanted to enforce strict boundary (not recommended), here's the path:

### Step 1: Define Core Interface

```go
// pkg/bwe/remb.go
package bwe

type REMBData struct {
    SenderSSRC uint32
    Bitrate    uint64
    SSRCs      []uint32
}

func BuildREMBData(senderSSRC uint32, bitrateBps uint64, ssrcs []uint32) REMBData {
    return REMBData{SenderSSRC: senderSSRC, Bitrate: bitrateBps, SSRCs: ssrcs}
}
```

### Step 2: Move Marshalling to Interceptor

```go
// pkg/bwe/interceptor/remb.go
package interceptor

import (
    "bwe/pkg/bwe"
    "github.com/pion/rtcp"
)

func marshalREMB(data bwe.REMBData) ([]byte, error) {
    pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: data.SenderSSRC,
        Bitrate:    float32(data.Bitrate),
        SSRCs:      data.SSRCs,
    }
    return pkt.Marshal()
}
```

### Step 3: Update Estimator API

```go
// pkg/bwe/bandwidth_estimator.go
func (e *BandwidthEstimator) MaybeBuildREMB(now time.Time) (REMBData, bool, error) {
    // Return struct instead of bytes
}
```

### Step 4: Update Interceptor

```go
// pkg/bwe/interceptor/interceptor.go
func (i *Interceptor) maybeSendREMB(now time.Time) {
    data, shouldSend, err := i.estimator.MaybeBuildREMB(now)
    if err != nil || !shouldSend {
        return
    }

    // Marshal here
    bytes, err := marshalREMB(data)
    // ... send
}
```

**Estimated effort:** 4-6 hours (modify 4 files, update all tests, verify behavior unchanged)

**Value proposition:** Theoretical purity, no practical benefit

**Recommendation:** Don't do this. Status quo (Option B) is better.

## Sources

**Go Architecture Patterns:**
- [Managing dependencies - The Go Programming Language](https://go.dev/doc/modules/managing-dependencies)
- [Standard Go Project Layout](https://github.com/golang-standards/project-layout)
- [Organizing a Go module](https://go.dev/doc/modules/layout)

**Clean Architecture & Hexagonal Design:**
- [Clean Architecture in Go: A Practical Guide](https://threedots.tech/post/introducing-clean-architecture/)
- [Hexagonal Architecture in Go](https://skoredin.pro/blog/golang/hexagonal-architecture-go)
- [Pragmatic Clean Architecture](https://blog.codewithram.dev/blog/clean-architecture-developer-journey.html)
- [DDD, Hexagonal, Onion, Clean, CQRS Combined](https://herbertograca.com/2017/11/16/explicit-architecture-01-ddd-hexagonal-onion-clean-cqrs-how-i-put-it-all-together/)

**Pion Architecture:**
- [Pion WebRTC Big Ideas](https://github.com/pion/webrtc/wiki/Big-Ideas)
- [Interceptors Design Discussion](https://github.com/pion/webrtc-v3-design/issues/34)
- [pion/interceptor Package Documentation](https://pkg.go.dev/github.com/pion/interceptor)

**Project History:**
- `.planning/phases/02-rate-control-remb/02-03-PLAN.md` - Original REMB builder plan
- `.planning/PROJECT.md` - Project context and key decisions
- `.planning/research/ARCHITECTURE.md` (v1.0) - Original architecture design

## Conclusion

**Recommendation: Option B - Relax boundary pragmatically.**

The boundary was already crossed in Phase 2 when we added `pion/rtcp` to core for REMB marshalling. This was the right decision then, and it remains the right decision now.

**Guiding principle:**
> Accept domain-level dependencies (`pion/rtcp`, `pion/rtp`), reject infrastructure-level dependencies (`pion/interceptor`, `pion/webrtc`).

**What this means for v1.1 milestone:**
- Keep REMB marshalling in core (status quo)
- Feel free to use Pion types where they improve type safety and reduce maintenance
- Keep algorithm logic pure (InterArrival, Kalman, AIMD)
- Keep integration logic in interceptor layer

**Files to modify:** Likely ZERO. Current architecture already follows this principle.

**Next steps:**
1. Review this research with team
2. Update PROJECT.md to reflect "domain dependencies acceptable" principle
3. Proceed with v1.1 refactoring using Pion types confidently
