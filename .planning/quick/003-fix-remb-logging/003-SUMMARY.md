---
id: "003"
type: quick
subsystem: interceptor
tags: [remb, callback, logging, pion]

key-files:
  modified:
    - pkg/bwe/interceptor/interceptor.go
    - pkg/bwe/interceptor/factory.go
    - cmd/chrome-interop/server/handler.go

key-decisions:
  - "OnREMB callback invoked after REMB sent, not before (ensures logging only on success)"
  - "Callback receives bitrate and SSRCs directly from the REMB packet"

duration: 3min
completed: 2026-01-23
---

# Quick Task 003: Fix REMB Logging Summary

**Added OnREMB callback to BWEInterceptor for reliable REMB logging in chrome-interop server**

## Performance

- **Duration:** 3 min
- **Completed:** 2026-01-23
- **Tasks:** 3
- **Files modified:** 3

## Accomplishments
- Added `WithOnREMB` callback option to BWEInterceptor
- Added `WithFactoryOnREMB` callback option to BWEInterceptorFactory
- Replaced broken `rembLogger` wrapper with working callback approach
- Removed 50 lines of dead code (rembLogger, loggingInterceptorFactory)

## Task Commits

Each task was committed atomically:

1. **Task 1: Add OnREMB callback to BWEInterceptor** - `7d453d3` (feat)
2. **Task 2: Use OnREMB callback in chrome-interop server** - `922c120` (refactor)
3. **Task 3: Verify the fix works end-to-end** - (no code changes, verification only)

## Files Modified
- `pkg/bwe/interceptor/interceptor.go` - Added onREMB field, WithOnREMB option, callback invocation in maybeSendREMB
- `pkg/bwe/interceptor/factory.go` - Added onREMB field, WithFactoryOnREMB option, pass to interceptor
- `cmd/chrome-interop/server/handler.go` - Replaced broken wrapper with callback, removed dead code

## Problem Solved

The original `rembLogger` wrapper didn't work because:
1. `BWEInterceptor.BindRTCPWriter(writer)` stores `writer` in `i.rtcpWriter`
2. `maybeSendREMB` uses `i.rtcpWriter.Write(pkts, nil)` directly
3. The wrapper returned from `BindRTCPWriter` was never called

The new `OnREMB` callback is invoked inside `maybeSendREMB` after the REMB is written, guaranteeing it sees every REMB packet.

## Decisions Made
- Callback invoked after successful Write (not before) to ensure we only log REMBs that were actually sent
- Callback receives same data as the REMB packet (bitrate, SSRCs) for easy logging

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
None

## Verification
- `go test ./pkg/bwe/... ./cmd/chrome-interop/...` - All tests pass
- `go build ./...` - Compiles without errors
- Manual: Run `go run ./cmd/chrome-interop` and connect browser to see REMB logging

---
*Quick Task: 003-fix-remb-logging*
*Completed: 2026-01-23*
