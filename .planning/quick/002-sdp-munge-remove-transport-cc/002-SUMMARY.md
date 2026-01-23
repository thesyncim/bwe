# Quick Task 002 Summary

## SDP Munging to Remove transport-cc

**Date:** 2026-01-23
**Status:** COMPLETE

## Problem

Even after removing TWCC interceptors from the server (quick-001), Chrome's bandwidth estimation (`availableOutgoingBitrate`) still worked independently of REMB. This made it impossible to validate that our REMB implementation was actually driving Chrome's sending rate.

## Root Cause

Chrome's `RTCPeerConnection.createOffer()` includes **both** feedback types in SDP:
- `a=rtcp-fb:XX goog-remb` (receiver-side BWE)
- `a=rtcp-fb:XX transport-cc` (sender-side BWE)

When both are present, **Chrome prefers transport-cc** for sender-side bandwidth estimation, ignoring REMB entirely.

## Solution

Added SDP munging in the browser JavaScript to remove transport-cc before SDP negotiation:

```javascript
function removeTransportCC(sdp) {
    return sdp
        .replace(/a=rtcp-fb:\d+ transport-cc\r?\n/g, '')
        .replace(/a=extmap:\d+ http:\/\/www\.ietf\.org\/id\/draft-holmer-rmcat-transport-wide-cc-extensions-01\r?\n/g, '');
}
```

Applied to:
1. Local SDP after `createOffer()`, before `setLocalDescription()`
2. Answer SDP from server, before `setRemoteDescription()`

## Files Changed

| File | Change |
|------|--------|
| `cmd/chrome-interop/server/html.go` | Add removeTransportCC() function and integrate into WebRTC flow |

## Verification

Human verified that:
- Browser console shows SDP munging logs
- webrtc-internals shows no `transport-cc` in negotiated SDP
- `goog-remb` is still present
- `availableOutgoingBitrate` now correlates with server's REMB estimates

## Combined with Quick-001

Together, quick-001 (server-side TWCC removal) and quick-002 (client-side SDP munging) ensure:
- Server doesn't send TWCC feedback packets
- Chrome doesn't expect TWCC feedback
- Chrome must rely solely on REMB for bandwidth estimation

## Commit

`6aedfda` - fix(chrome-interop): add SDP munging to remove transport-cc
