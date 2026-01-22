---
phase: 01-foundation-core-pipeline
plan: 05
subsystem: bwe
tags: [gcc, overuse-detection, adaptive-threshold, bandwidth-estimation]

# Dependency graph
requires:
  - phase: 01-01
    provides: BandwidthUsage enum types (BwNormal, BwUnderusing, BwOverusing)
  - phase: 01-03
    provides: Clock interface for monotonic timing
  - phase: 01-04
    provides: MockClock for deterministic testing
provides:
  - OveruseDetector with adaptive threshold for congestion detection
  - StateChangeCallback for application notification on state transitions
  - OveruseConfig with GCC-compliant defaults (K_u=0.01, K_d=0.00018)
  - Signal suppression to prevent false positives
affects: [rate-controller, remb-generator, pion-integration]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Adaptive threshold using asymmetric coefficients for TCP fairness"
    - "Signal suppression when gradient is decreasing"
    - "Sustained overuse requirement before signaling (>=10ms)"

key-files:
  created:
    - pkg/bwe/overuse.go
    - pkg/bwe/overuse_test.go
  modified: []

key-decisions:
  - "Track overuse region separately from hypothesis state using inOveruseRegion flag"
  - "Threshold adaptation uses exponential smoothing: threshold += deltaT * K * (|m| - threshold)"
  - "K_u (0.01) > K_d (0.00018) ensures slow threshold increase but faster decrease for TCP fairness"

patterns-established:
  - "Overuse detection pattern: threshold comparison -> sustained check -> suppression check -> state change"
  - "Callback notification pattern for state machine transitions"

# Metrics
duration: 5min
completed: 2026-01-22
---

# Phase 01 Plan 05: Overuse Detector Summary

**Overuse detector with adaptive threshold, signal suppression, and sustained overuse requirement for GCC-compliant congestion detection**

## Performance

- **Duration:** 5 min
- **Started:** 2026-01-22T15:13:32Z
- **Completed:** 2026-01-22T15:18:19Z
- **Tasks:** 2
- **Files created:** 2

## Accomplishments

- Implemented 3-state overuse detector (Normal/Overusing/Underusing) with adaptive threshold
- Added asymmetric threshold adaptation (K_u=0.01, K_d=0.00018) for TCP fairness
- Implemented sustained overuse requirement (>=10ms) to prevent false positives
- Added signal suppression when gradient is decreasing to avoid oscillation
- Implemented state change callbacks for application notification
- Created comprehensive test suite (530 lines) covering all detection behaviors

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement overuse detector with adaptive threshold** - `cec62c7` (feat)
2. **Task 1 fix: Track overuse region separately from hypothesis** - `be93cc1` (fix)
3. **Task 2: Add comprehensive tests for overuse detection** - `c7d20f6` (test)

## Files Created/Modified

- `pkg/bwe/overuse.go` - OveruseDetector with adaptive threshold, StateChangeCallback, OveruseConfig
- `pkg/bwe/overuse_test.go` - Comprehensive tests for all detection behaviors (530 lines)

## Decisions Made

1. **Track overuse region separately:** Added `inOveruseRegion` flag to properly track potential overuse period even while hypothesis is still BwNormal. This fixes the issue where overuseStart was reset on every detection.

2. **Threshold adaptation formula:** Using exponential smoothing `threshold += deltaT * K * (|m| - threshold)` where K is Ku when above threshold and Kd when below.

3. **Asymmetric coefficients rationale:** K_u (0.01) is much larger than K_d (0.00018) which means:
   - Threshold increases slowly when overuse detected (prevents being too aggressive)
   - Threshold decreases even slower when estimate is below threshold (prevents oscillation)
   - This asymmetry is critical for TCP fairness

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed overuse tracking across multiple detections**
- **Found during:** Task 2 (testing sustained overuse)
- **Issue:** When hypothesis was BwNormal and estimate > threshold, overuseStart was reset on every call, preventing sustained overuse detection from ever succeeding
- **Fix:** Added `inOveruseRegion` flag to track whether we're in a potential overuse period, separate from the hypothesis state
- **Files modified:** pkg/bwe/overuse.go
- **Verification:** All sustained overuse tests now pass
- **Committed in:** be93cc1

---

**Total deviations:** 1 auto-fixed (1 bug)
**Impact on plan:** Bug fix was essential for correct overuse detection behavior. No scope creep.

## Issues Encountered

None - plan executed as specified after fixing the overuse tracking bug.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Overuse detector is complete and ready for integration with rate controller
- All success criteria met:
  - DETECT-01: 3-state detector (Normal/Overusing/Underusing) implemented
  - DETECT-02: Adaptive threshold with K_u=0.01, K_d=0.00018 working
  - DETECT-03: Sustained overuse (>=10ms) required before signaling
  - DETECT-04: State change callbacks provided to application code
- Phase 1 foundation components (timestamp, interarrival, Kalman, trendline, overuse) are complete
- Ready for Phase 1 Plan 06: REMB generation from bandwidth estimates

---
*Phase: 01-foundation-core-pipeline*
*Completed: 2026-01-22*
