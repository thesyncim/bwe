# Requirements: GCC Receiver-Side BWE v1.1

**Defined:** 2026-01-22
**Core Value:** Adopt Pion's native types to reduce maintenance and prepare for upstream contribution

## v1.1 Requirements

Requirements for this refactoring milestone.

### Extension Parsing

- [ ] **EXT-01**: Use `pion/rtp.AbsSendTimeExtension` for parsing abs-send-time from RTP packets
- [ ] **EXT-02**: Use `pion/rtp.AbsCaptureTimeExtension` for parsing abs-capture-time from RTP packets
- [ ] **EXT-03**: Remove custom `ParseAbsSendTime()` function (replaced by Pion)
- [ ] **EXT-04**: Remove custom `ParseAbsCaptureTime()` function (replaced by Pion)

### Preserve Critical Logic

- [ ] **KEEP-01**: Retain `UnwrapAbsSendTime()` for 64-second timestamp wraparound handling
- [ ] **KEEP-02**: Retain `FindExtensionID()` helpers for SDP-based extension ID discovery
- [ ] **KEEP-03**: Retain custom inter-group delay calculation (Pion has no equivalent)

### Validation

- [ ] **VAL-01**: All existing tests pass after refactor
- [ ] **VAL-02**: Benchmark shows no allocation regression in hot path (0 allocs/op for core)
- [ ] **VAL-03**: 24-hour soak test passes (timestamp wraparound validation)
- [ ] **VAL-04**: Chrome interop still works (REMB accepted)

## Future Requirements

Deferred to future milestones.

### Cleanup

- **CLEAN-01**: Remove deprecated parsing functions after 30-day validation period
- **CLEAN-02**: Document Pion type usage patterns for upstream contribution

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
| EXT-01 | Phase 5 | Pending |
| EXT-02 | Phase 5 | Pending |
| EXT-03 | Phase 5 | Pending |
| EXT-04 | Phase 5 | Pending |
| KEEP-01 | Phase 5 | Pending |
| KEEP-02 | Phase 5 | Pending |
| KEEP-03 | Phase 5 | Pending |
| VAL-01 | Phase 5 | Pending |
| VAL-02 | Phase 5 | Pending |
| VAL-03 | Phase 5 | Pending |
| VAL-04 | Phase 5 | Pending |

**Coverage:**
- v1.1 requirements: 11 total
- Mapped to phases: 11
- Unmapped: 0 ✓

---
*Requirements defined: 2026-01-22*
*Last updated: 2026-01-22 after v1.1 research complete*
