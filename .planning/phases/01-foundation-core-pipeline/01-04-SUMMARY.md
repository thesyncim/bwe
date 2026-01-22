---
phase: 01-foundation-core-pipeline
plan: 04
subsystem: bwe
tags: [trendline, linear-regression, delay-estimation, webrtc, gcc]

# Dependency graph
requires:
  - phase: 01-01
    provides: Core types.go with time.Time for arrival timestamps
provides:
  - TrendlineEstimator as alternative to Kalman filter
  - Linear regression over sliding window for delay trend detection
  - Exponential smoothing for noise reduction
affects: [01-05-overuse-detector, 02-rate-controller]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Sliding window with configurable size"
    - "Least squares linear regression"
    - "Exponential smoothing accumulator"
    - "Sample count cap (60) for stability"

key-files:
  created:
    - pkg/bwe/trendline.go
    - pkg/bwe/trendline_test.go
  modified: []

key-decisions:
  - "Used min(numDeltas, 60) cap per WebRTC reference to prevent runaway values"
  - "Exponential smoothing applied before regression, not after"

patterns-established:
  - "Filter interface pattern: Update(time, value) float64 plus Reset()"
  - "Internal sample struct for history storage"

# Metrics
duration: 3min
completed: 2026-01-22
---

# Phase 01 Plan 04: Trendline Estimator Summary

**Linear regression-based delay trend estimator with sliding window, exponential smoothing, and configurable threshold gain as modern alternative to Kalman filtering**

## Performance

- **Duration:** 3 min
- **Started:** 2026-01-22T15:10:00Z
- **Completed:** 2026-01-22T15:13:00Z
- **Tasks:** 2
- **Files created:** 2

## Accomplishments

- Implemented TrendlineEstimator with linear regression over sliding window
- Added exponential smoothing (default 0.9 coef) for delay accumulation
- Created comprehensive test suite with 13 test cases covering all behaviors
- Both Kalman (01-03) and Trendline filters now available for overuse detector

## Task Commits

Each task was committed atomically:

1. **Task 1: Implement trendline estimator with linear regression** - `5b0803e` (feat)
2. **Task 2: Add tests for trendline estimator** - `04e1249` (test)

## Files Created/Modified

- `pkg/bwe/trendline.go` - TrendlineEstimator with linear regression, exponential smoothing, threshold gain
- `pkg/bwe/trendline_test.go` - 13 test cases (350 lines) verifying all estimator behaviors

## Decisions Made

1. **Sample count cap at 60** - Per WebRTC reference implementation, the numDeltas multiplier is capped at 60 to prevent runaway values during long sessions while still allowing startup ramp-up
2. **Smoothing before regression** - Exponential smoothing is applied to input delay variations before storing in history, matching WebRTC behavior where accumulated smoothed delay is the Y-axis for regression

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both delay filters ready for overuse detector (01-05):
  - Kalman filter (01-03): Better for steady-state tracking
  - Trendline estimator (01-04): Better for rapid change detection
- Overuse detector will select filter based on configuration
- Both output comparable "modified trend" values

---
*Phase: 01-foundation-core-pipeline*
*Completed: 2026-01-22*
