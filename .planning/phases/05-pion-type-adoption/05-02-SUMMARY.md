---
phase: 05-pion-type-adoption
plan: 02
subsystem: api
tags: [deprecation, go, godoc, pion, rtp, migration]

# Dependency graph
requires:
  - phase: 05-01
    provides: Pion extension parsing in interceptor
provides:
  - Deprecation comments on ParseAbsSendTime() pointing to pion/rtp
  - Deprecation comments on ParseAbsCaptureTime() pointing to pion/rtp
  - v1.2 removal timeline documented
affects: [05-03, future-migration-docs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Go deprecation comments with Deprecated: prefix for godoc"
    - "Migration examples in deprecation comments"

key-files:
  created: []
  modified:
    - pkg/bwe/timestamp.go

key-decisions:
  - "v1.2 removal timeline: signals stability until next minor release"
  - "Migration examples in comments: helps users migrate immediately"

patterns-established:
  - "Deprecation comment pattern: Deprecated: Use X instead. Removed in vY."
  - "Include code examples in deprecation comments for clear migration path"

# Metrics
duration: 2min
completed: 2026-01-22
---

# Phase 5 Plan 02: Mark Custom Parsing Functions as Deprecated Summary

**Deprecation comments added to ParseAbsSendTime() and ParseAbsCaptureTime() with v1.2 removal timeline and Pion migration examples**

## Performance

- **Duration:** 2 min
- **Started:** 2026-01-22T22:33:00Z
- **Completed:** 2026-01-22T22:35:54Z
- **Tasks:** 3
- **Files modified:** 1

## Accomplishments

- Added "Deprecated:" godoc marker to ParseAbsSendTime() pointing to pion/rtp.AbsSendTimeExtension
- Added "Deprecated:" godoc marker to ParseAbsCaptureTime() pointing to pion/rtp.AbsCaptureTimeExtension
- Verified all KEEP requirements preserved (UnwrapAbsSendTime, duration converters unchanged)
- All existing timestamp tests pass (no functional changes)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add deprecation comment to ParseAbsSendTime** - `7608185` (docs)
2. **Task 2: Add deprecation comment to ParseAbsCaptureTime** - `39f8bad` (docs)
3. **Task 3: Verify all timestamp tests pass and KEEP requirements preserved** - (verification only, no commit needed)

## Files Modified

- `pkg/bwe/timestamp.go` - Added deprecation comments to ParseAbsSendTime() and ParseAbsCaptureTime()

## Decisions Made

- **v1.2 removal timeline:** Signals to callers they have until the next minor release to migrate
- **Migration examples in comments:** Include working code snippets showing exact replacement pattern
- **Comments only, no functional changes:** Deprecation is purely documentation to preserve backward compatibility

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

None

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Deprecation comments in place for EXT-03 and EXT-04
- Ready for 05-03 validation phase
- All existing functionality preserved (KEEP-01 verified)
- No breaking changes introduced

---
*Phase: 05-pion-type-adoption*
*Completed: 2026-01-22*
