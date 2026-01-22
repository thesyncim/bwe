# Requirements: GCC Receiver-Side BWE v1.1

**Defined:** 2026-01-22
**Core Value:** Adopt Pion's native types to reduce maintenance and prepare for upstream contribution

## v1.1 Requirements

Requirements for this refactoring milestone.

### Extension Parsing

- [x] **EXT-01**: Use `pion/rtp.AbsSendTimeExtension` for parsing abs-send-time from RTP packets
- [x] **EXT-02**: Use `pion/rtp.AbsCaptureTimeExtension` for parsing abs-capture-time from RTP packets
- [x] **EXT-03**: Remove custom `ParseAbsSendTime()` function (replaced by Pion)
- [x] **EXT-04**: Remove custom `ParseAbsCaptureTime()` function (replaced by Pion)

### Preserve Critical Logic

- [x] **KEEP-01**: Retain `UnwrapAbsSendTime()` for 64-second timestamp wraparound handling
- [x] **KEEP-02**: Retain `FindExtensionID()` helpers for SDP-based extension ID discovery
- [x] **KEEP-03**: Retain custom inter-group delay calculation (Pion has no equivalent)

### Validation

- [x] **VAL-01**: All existing tests pass after refactor
- [x] **VAL-02**: Benchmark shows no allocation regression in hot path (0 allocs/op for core)
- [x] **VAL-03**: 24-hour soak test passes (timestamp wraparound validation)
- [x] **VAL-04**: Chrome interop still works (REMB accepted) — requires manual verification

## Future Requirements

Deferred to future milestones.

### Documentation

- **DOC-01**: Document Pion type usage patterns for upstream contribution

## Out of Scope

| Feature | Reason |
|---------|--------|
| Architecture changes | Current boundary is correct per research |
| REMB marshalling changes | Already uses Pion types |
| Wraparound logic changes | Custom implementation validated, Pion lacks this |
| New features | This is a refactoring milestone only |

## Traceability

| Requirement | Phase | Status |
|-------------|-------|--------|
| EXT-01 | Phase 5 | Complete |
| EXT-02 | Phase 5 | Complete |
| EXT-03 | Phase 5 | Complete |
| EXT-04 | Phase 5 | Complete |
| KEEP-01 | Phase 5 | Complete |
| KEEP-02 | Phase 5 | Complete |
| KEEP-03 | Phase 5 | Complete |
| VAL-01 | Phase 5 | Complete |
| VAL-02 | Phase 5 | Complete |
| VAL-03 | Phase 5 | Complete |
| VAL-04 | Phase 5 | Complete |

**Coverage:**
- v1.1 requirements: 11 total
- Mapped to phases: 11
- Completed: 11 ✓

---
*Requirements defined: 2026-01-22*
*Last updated: 2026-01-22 after Phase 5 execution complete*
