# Phase 5: Pion Type Adoption - Research

**Researched:** 2026-01-22
**Domain:** RTP Extension Type Refactoring (pion/rtp v1.10.0)
**Confidence:** HIGH

## Summary

Phase 5 refactors the BWE implementation to use Pion's native RTP extension parsing types (`AbsSendTimeExtension`, `AbsCaptureTimeExtension`) while preserving validated behavior and the zero-allocation hot path. The current implementation already uses Pion for REMB marshalling (since Phase 2), so this extends an established pattern.

The refactoring is straightforward: replace custom byte-parsing functions (`ParseAbsSendTime()`, `ParseAbsCaptureTime()`) with Pion's `Unmarshal()` methods. However, critical wraparound handling logic (`UnwrapAbsSendTime()`) and extension ID discovery helpers (`FindExtensionID()`) must be preserved as Pion provides no equivalent.

**Primary recommendation:** Use Pion's extension types for parsing, but keep all BWE-specific delta calculation and wraparound logic. Validate zero-allocation property is preserved via existing benchmark suite before merging.

## Standard Stack

The established libraries/tools for this refactoring:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `pion/rtp` | v1.10.0 | RTP extension parsing | Already a dependency, provides `AbsSendTimeExtension`, `AbsCaptureTimeExtension` |
| `pion/rtcp` | v1.2.16 | REMB packet handling | Already in use since Phase 2 |
| `pion/interceptor` | v0.1.43 | Pion integration layer | Already a dependency for interceptor implementation |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `testing` | stdlib | Benchmarks | Verify 0 allocs/op maintained |
| `go build -gcflags="-m"` | toolchain | Escape analysis | Debug any allocation regressions |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Pion's `AbsSendTimeExtension.Timestamp` (uint64) | Keep custom uint32 | Pion uses uint64 for 24-bit value (wastes bits), but difference is negligible and Pion is maintained |
| Pion's `Unmarshal()` method | Keep `ParseAbsSendTime()` | Custom is 3 lines shorter but Pion is battle-tested and maintained upstream |

**Installation:**
```bash
# No new dependencies - pion/rtp v1.10.0 already in go.mod
go mod tidy
```

## Architecture Patterns

### Recommended Refactoring Structure

The changes are localized to two files in the interceptor hot path:

```
pkg/bwe/
├── timestamp.go          # KEEP: UnwrapAbsSendTime(), duration helpers
│                         # REMOVE: ParseAbsSendTime(), ParseAbsCaptureTime()
├── timestamp_test.go     # KEEP: All wraparound tests still valid
├── types.go              # UNCHANGED
└── interceptor/
    ├── interceptor.go    # MODIFY: Use Pion Unmarshal() instead of ParseAbsSendTime()
    ├── extension.go      # KEEP: FindExtensionID() helpers (Pion has no equivalent)
    └── benchmark_test.go # VERIFY: 0 allocs/op maintained
```

### Pattern 1: Stack-Allocated Extension Struct

**What:** Declare `AbsSendTimeExtension` on stack, call `Unmarshal()` with pointer receiver.
**When to use:** Hot path parsing to avoid heap allocation.
**Example:**
```go
// Source: pion/rtp AbsSendTimeExtension.Unmarshal signature
// Hot path in processRTP() - ZERO ALLOCATIONS

var ext rtp.AbsSendTimeExtension  // Stack allocated
if extData := header.GetExtension(absID); len(extData) >= 3 {
    if err := ext.Unmarshal(extData); err == nil {
        sendTime = uint32(ext.Timestamp)  // Cast to uint32 (24-bit value fits)
    }
}
```

### Pattern 2: Preserve Wraparound Logic Separately

**What:** Keep `UnwrapAbsSendTime()` as standalone function operating on uint32 values.
**When to use:** After parsing, when calculating inter-packet deltas.
**Example:**
```go
// UnwrapAbsSendTime stays in timestamp.go - Pion has no equivalent
delta := bwe.UnwrapAbsSendTime(prevSendTime, currSendTime)
deltaSeconds := float64(delta) * bwe.AbsSendTimeResolution
```

### Pattern 3: Deprecation Before Removal

**What:** Mark functions as deprecated in v1.1, remove in v1.2.
**When to use:** Functions being replaced but may have external callers.
**Example:**
```go
// Deprecated: Use rtp.AbsSendTimeExtension.Unmarshal() instead.
// This function will be removed in v1.2.
func ParseAbsSendTime(data []byte) (uint32, error) {
    // ... existing implementation kept for backward compatibility
}
```

### Anti-Patterns to Avoid
- **Removing UnwrapAbsSendTime:** Pion has no wraparound handling - this is critical BWE logic
- **Heap-allocating extension structs:** `new(rtp.AbsSendTimeExtension)` would add allocations to hot path
- **Changing public API types:** Keep `uint32` for send times in PacketInfo - don't propagate Pion's uint64

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| 24-bit big-endian parsing | Custom byte shifts | `rtp.AbsSendTimeExtension.Unmarshal()` | Maintained upstream, handles validation |
| 64-bit UQ32.32 parsing | Custom byte shifts | `rtp.AbsCaptureTimeExtension.Unmarshal()` | Also handles optional clock offset field |
| REMB encoding | Custom mantissa/exponent | `rtcp.ReceiverEstimatedMaximumBitrate` | Already using this, 9 validation checks |
| RTP header parsing | Custom buffer reads | `rtp.Header.Unmarshal()` | Already using this, handles all profiles |

**Key insight:** The "standalone core" principle was relaxed in Phase 2 when `pion/rtcp` was added for REMB. Domain-level dependencies (packet formats, wire protocols) are acceptable in core; infrastructure dependencies (goroutines, state, network) stay in adapter layer.

## Common Pitfalls

### Pitfall 1: Allocation Regression in Hot Path
**What goes wrong:** Switching to Pion types introduces heap allocations, violating 0 allocs/op requirement.
**Why it happens:** Pointer receivers, interface boxing, or heap-allocated structs in hot path.
**How to avoid:**
- Declare `rtp.AbsSendTimeExtension` on stack (not via `new()`)
- Call `Unmarshal()` with pointer receiver on stack-allocated struct
- Run `go test -bench=ZeroAlloc -benchmem` before and after changes
**Warning signs:** `BenchmarkProcessRTP_Allocations` shows >2 allocs/op (currently 2 from sync.Map + atomic.Value)

### Pitfall 2: Breaking Wraparound Logic
**What goes wrong:** Removing or modifying `UnwrapAbsSendTime()` breaks 24-hour soak test.
**Why it happens:** Assumption that Pion handles wraparound (it doesn't).
**How to avoid:**
- Keep `UnwrapAbsSendTime()` unchanged - it's battle-tested through 24-hour soak
- Keep all timestamp_test.go wraparound tests
- Run soak test after refactoring: `go test -run TestSoak24Hour_Accelerated`
**Warning signs:** `TestTimestampWraparound_*` tests fail, estimates spike at 64-second boundaries

### Pitfall 3: Type Width Mismatch
**What goes wrong:** Using Pion's `uint64` Timestamp directly where `uint32` is expected.
**Why it happens:** Pion stores 24-bit value in uint64, current API uses uint32.
**How to avoid:**
- Cast `ext.Timestamp` to uint32 immediately after parsing
- Don't change `PacketInfo.SendTime` type (keep uint32)
- Values are always 0-16777215 (24-bit), so cast is safe
**Warning signs:** Type errors at compile time, or silent overflow if logic changed

### Pitfall 4: Breaking Abs-Capture-Time Clock Offset
**What goes wrong:** Pion's `AbsCaptureTimeExtension.Unmarshal()` may fail on 8-byte payloads if code expects 16.
**Why it happens:** Pion supports variable-length (8 or 16 bytes with optional clock offset).
**How to avoid:**
- Pion handles this correctly - 8 bytes parses Timestamp only, 16 bytes also parses offset
- Current code only uses Timestamp anyway (KEEP-03 says keep custom inter-group delay calc)
- Test with both 8-byte and 16-byte payloads
**Warning signs:** Parsing errors on valid abs-capture-time extensions

### Pitfall 5: Removing Extension ID Helpers
**What goes wrong:** Removing `FindExtensionID()` breaks SDP-based extension discovery.
**Why it happens:** Assumption that Pion provides URI-to-ID lookup (it doesn't).
**How to avoid:**
- Keep KEEP-02: `FindExtensionID()`, `FindAbsSendTimeID()`, `FindAbsCaptureTimeID()`
- These are convenience helpers not provided by Pion
- Extension URIs are matched during SDP negotiation
**Warning signs:** Extension IDs are 0 (not found), packets skipped

## Code Examples

Verified patterns from Pion documentation and existing codebase:

### Current Custom Parsing (TO BE REPLACED)
```go
// Source: pkg/bwe/interceptor/interceptor.go (current)
if ext := header.GetExtension(absID); len(ext) >= 3 {
    sendTime, _ = bwe.ParseAbsSendTime(ext)  // Custom function
}
```

### Pion Extension Parsing (REPLACEMENT)
```go
// Source: pion/rtp AbsSendTimeExtension - verified via pkg.go.dev
// Zero-allocation pattern: stack-allocated struct

if extData := header.GetExtension(absID); len(extData) >= 3 {
    var ext rtp.AbsSendTimeExtension
    if err := ext.Unmarshal(extData); err == nil {
        sendTime = uint32(ext.Timestamp)
    }
}
```

### Pion AbsCaptureTimeExtension Pattern
```go
// Source: pion/rtp AbsCaptureTimeExtension - verified via pkg.go.dev
// Handles both 8-byte (timestamp only) and 16-byte (with clock offset) payloads

if extData := header.GetExtension(captureID); len(extData) >= 8 {
    var ext rtp.AbsCaptureTimeExtension
    if err := ext.Unmarshal(extData); err == nil {
        // ext.Timestamp is uint64 UQ32.32 format
        // ext.EstimatedCaptureClockOffset is *int64 (nil if not present)

        // Convert to abs-send-time scale (existing logic preserved)
        seconds := (ext.Timestamp >> 32) & 0x3F    // 6 bits of seconds (mod 64)
        fraction := (ext.Timestamp >> 14) & 0x3FFFF // 18 bits of fraction
        sendTime = uint32((seconds << 18) | fraction)
    }
}
```

### Preserving Wraparound Logic
```go
// Source: pkg/bwe/timestamp.go - UNCHANGED, Pion has no equivalent
// This is validated through 24-hour soak test (>1350 wraparounds)

func UnwrapAbsSendTime(prev, curr uint32) int64 {
    diff := int32(curr) - int32(prev)
    halfRange := int32(AbsSendTimeMax / 2)  // 32 seconds

    if diff > halfRange {
        diff -= int32(AbsSendTimeMax)
    } else if diff < -halfRange {
        diff += int32(AbsSendTimeMax)
    }
    return int64(diff)
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Custom REMB encoding | `pion/rtcp` REMB | Phase 2 (2026-01-22) | Precedent for domain dependencies in core |
| Custom byte parsing | Pion extension types | Phase 5 (this phase) | Reduces maintenance, gains upstream fixes |
| Single 8-byte abs-capture-time | Variable 8/16-byte with clock offset | Pion support exists | Future-proofing for multi-stream sync |

**Deprecated/outdated:**
- `ParseAbsSendTime()`: Will be deprecated (replaced by Pion Unmarshal)
- `ParseAbsCaptureTime()`: Will be deprecated (replaced by Pion Unmarshal)

## Open Questions

Things that couldn't be fully resolved:

1. **Deprecation Timeline**
   - What we know: Functions will be marked deprecated in v1.1
   - What's unclear: How long to keep deprecated functions before removal
   - Recommendation: Keep for one release cycle (remove in v1.2), matches CLEAN-01 future requirement

2. **Pion v2 Migration**
   - What we know: Pion has v2 packages (`github.com/pion/rtp/v2`)
   - What's unclear: Whether v1 will be maintained, any breaking changes in v2
   - Recommendation: Stay on v1.10.0 for now, v2 migration is separate effort

## Sources

### Primary (HIGH confidence)
- **Pion RTP v1.10.0 Documentation:** [pkg.go.dev/github.com/pion/rtp](https://pkg.go.dev/github.com/pion/rtp) - AbsSendTimeExtension, AbsCaptureTimeExtension APIs
- **Pion RTP Source Code:** [github.com/pion/rtp/blob/master/abssendtimeextension.go](https://github.com/pion/rtp/blob/master/abssendtimeextension.go) - Unmarshal implementation verified
- **Pion RTP Source Code:** [github.com/pion/rtp/blob/master/abscapturetimeextension.go](https://github.com/pion/rtp/blob/master/abscapturetimeextension.go) - Variable-length parsing verified
- **Codebase Analysis:** `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp.go` - Current parsing implementation
- **Codebase Analysis:** `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/interceptor/interceptor.go` - Hot path usage
- **Codebase Analysis:** `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/benchmark_test.go` - 0 allocs/op verification

### Secondary (MEDIUM confidence)
- **Prior Research:** `/Users/thesyncim/GolandProjects/bwe/.planning/research/FEATURES.md` - Pion vs BWE feature comparison
- **Prior Research:** `/Users/thesyncim/GolandProjects/bwe/.planning/research/SUMMARY.md` - v1.1 research synthesis

### Tertiary (LOW confidence)
- **WebSearch:** "pion rtp AbsSendTimeExtension zero allocation" - No specific allocation documentation found, verified through source code inspection

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Pion versions verified in go.mod, APIs stable
- Architecture: HIGH - Pattern already established in Phase 2 (REMB adoption)
- Pitfalls: HIGH - Benchmark suite validates allocations, soak test validates wraparound
- Code examples: HIGH - Verified against Pion source and existing codebase

**Research date:** 2026-01-22
**Valid until:** 2026-03-22 (60 days - stable library, refactoring scope)

---

## Implementation Checklist

Based on research, here are the key implementation steps for planning:

### EXT-01, EXT-02: Adopt Pion Extension Types
- [ ] Replace `bwe.ParseAbsSendTime(ext)` with `rtp.AbsSendTimeExtension.Unmarshal()`
- [ ] Replace `bwe.ParseAbsCaptureTime(ext)` with `rtp.AbsCaptureTimeExtension.Unmarshal()`
- [ ] Use stack-allocated structs to maintain 0 allocs/op
- [ ] Cast `ext.Timestamp` (uint64) to uint32 for API compatibility

### EXT-03, EXT-04: Remove Custom Functions
- [ ] Mark `ParseAbsSendTime()` as deprecated with godoc comment
- [ ] Mark `ParseAbsCaptureTime()` as deprecated with godoc comment
- [ ] Keep implementations for backward compatibility in v1.1
- [ ] Plan removal for v1.2 (per CLEAN-01)

### KEEP-01, KEEP-02, KEEP-03: Preserve Critical Logic
- [ ] Do NOT modify `UnwrapAbsSendTime()` - critical wraparound handling
- [ ] Do NOT modify `FindExtensionID()` helpers - Pion has no equivalent
- [ ] Do NOT modify inter-group delay calculation - Pion has no equivalent

### VAL-01, VAL-02, VAL-03, VAL-04: Validation
- [ ] Run `go test ./...` - all existing tests must pass
- [ ] Run `go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...` - verify 0 allocs/op
- [ ] Run `go test -run TestSoak24Hour_Accelerated` - verify wraparound handling
- [ ] Run Chrome interop test (`cmd/chrome-interop`) - verify REMB accepted
