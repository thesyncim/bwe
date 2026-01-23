# Quick Task 004: Add Real E2E BWE Test - Summary

**Status:** COMPLETE
**Date:** 2026-01-23
**Duration:** ~5 min

## One-liner

E2E test proving real Chrome browser BWE responds to REMB packets from Go server.

## What Was Done

### Task 1: Create BWE E2E test
- Created `e2e/bwe_test.go` with `TestChrome_BWERespondsToREMB`
- Test validates complete BWE pipeline with real headless Chrome:
  1. Server starts on random port with BWE interceptor
  2. Chrome connects via WebRTC with fake video stream
  3. Server sends REMB packets based on bandwidth estimation
  4. Chrome reports `availableOutgoingBitrate` in stats
  5. Test validates bitrate is within expected range (50kbps - 6Mbps)

### Task 2: Add helper functions for readability
- `waitForConnectionTestPC()` - Polls connection state with timeout
- `getOutgoingBitrate()` - Retrieves stats via JavaScript eval

## Technical Details

### Key Discovery: Secure Context Required
Chrome restricts `navigator.mediaDevices.getUserMedia()` to secure contexts. The server binds to `[::]:port` (IPv6), but Chrome doesn't recognize this as localhost. Solution: convert to `localhost:port` URL format.

```go
_, port, _ := net.SplitHostPort(addr)
url := "http://localhost:" + port
```

### Self-Contained WebRTC Setup
The test creates its own WebRTC connection via JavaScript eval rather than using the page's `startCall()` function. This avoids issues with the HTML's error handling that calls `stopCall()` on any error.

### SDP Munging for REMB-Only
The test removes transport-cc from SDP to force Chrome into REMB-only mode:
```javascript
sdp = sdp.replace(/a=rtcp-fb:\d+ transport-cc\r?\n/g, '');
sdp = sdp.replace(/a=extmap:\d+ http:\/\/www\.ietf\.org\/id\/draft-holmer-rmcat-transport-wide-cc-extensions-01\r?\n/g, '');
```

## Files Changed

| File | Change |
|------|--------|
| `e2e/bwe_test.go` | New - E2E test for BWE behavior |

## Commits

| Hash | Message |
|------|---------|
| 5cba923 | test(004): add E2E test for BWE REMB behavior |

## Verification

```
=== RUN   TestChrome_BWERespondsToREMB
    bwe_test.go:46: Server started on [::]:56956
    bwe_test.go:64: Navigating to http://localhost:56956
    bwe_test.go:84: Media devices check: getUserMediaExists:true isSecureContext:true
    bwe_test.go:154: WebRTC setup result: connected
    REMB sent: estimate=354666 bps
    REMB sent: estimate=282598 bps
    bwe_test.go:182: Chrome availableOutgoingBitrate: 282598 bps
--- PASS: TestChrome_BWERespondsToREMB (35.20s)
```

## Deviations from Plan

None - plan executed exactly as written.

## Related

- Quick task 003: Fix REMB logging (prerequisite - REMB callback needed for verification)
- Phase 6: Test Infrastructure Foundation (scaffolding used by this test)
