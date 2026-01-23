---
phase: quick
plan: 002
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/chrome-interop/server/html.go
autonomous: true

must_haves:
  truths:
    - "Chrome uses REMB-only for bandwidth estimation"
    - "transport-cc RTCP feedback is not negotiated in SDP"
    - "transport-wide-cc RTP header extension is not negotiated in SDP"
  artifacts:
    - path: "cmd/chrome-interop/server/html.go"
      provides: "SDP munging function that removes transport-cc"
      contains: "removeTransportCC"
  key_links:
    - from: "createOffer()"
      to: "removeTransportCC()"
      via: "munge local description before setLocalDescription"
      pattern: "removeTransportCC.*setLocalDescription"
    - from: "setRemoteDescription()"
      to: "removeTransportCC()"
      via: "munge answer SDP before applying"
      pattern: "removeTransportCC.*setRemoteDescription"
---

<objective>
Add SDP munging to force Chrome to use REMB-only for bandwidth estimation

Purpose: Quick-001 removed TWCC interceptors from the server side, but Chrome still offers transport-cc in its SDP. When both goog-remb and transport-cc are present, Chrome prefers sender-side transport-cc BWE and ignores REMB. This plan adds client-side SDP munging to remove transport-cc lines so Chrome falls back to REMB-only.

Output: Modified html.go with JavaScript SDP munging that removes:
1. `a=rtcp-fb:XX transport-cc` lines (for all payload types)
2. `a=extmap:X http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01` line
</objective>

<execution_context>
@~/.claude/get-shit-done/workflows/execute-plan.md
@~/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@cmd/chrome-interop/server/html.go

Root cause:
- Chrome's default offer includes both `a=rtcp-fb:XX goog-remb` and `a=rtcp-fb:XX transport-cc`
- When both present, Chrome uses sender-side transport-cc BWE, ignoring REMB
- Need to munge SDP to remove transport-cc before setLocalDescription AND setRemoteDescription

Prior related work:
- Quick-001 removed TWCC interceptors from server (4e13282)
- Server now only sends REMB, but Chrome still OFFERS transport-cc
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add SDP munging function to remove transport-cc</name>
  <files>cmd/chrome-interop/server/html.go</files>
  <action>
Add a JavaScript function `removeTransportCC(sdp)` to the HTML page that:
1. Uses regex to remove lines matching `a=rtcp-fb:\d+ transport-cc`
2. Uses regex to remove lines matching `a=extmap:\d+ http://www.ietf.org/id/draft-holmer-rmcat-transport-wide-cc-extensions-01`
3. Returns the munged SDP string

Then modify the WebRTC flow to munge SDP at two points:
1. After createOffer(), before setLocalDescription() - munge the local offer
2. After receiving answer from server, before setRemoteDescription() - munge the answer

Implementation:
```javascript
function removeTransportCC(sdp) {
    // Remove transport-cc RTCP feedback lines
    sdp = sdp.replace(/a=rtcp-fb:\d+ transport-cc\r?\n/g, '');
    // Remove transport-wide-cc RTP header extension
    sdp = sdp.replace(/a=extmap:\d+ http:\/\/www\.ietf\.org\/id\/draft-holmer-rmcat-transport-wide-cc-extensions-01\r?\n/g, '');
    return sdp;
}
```

In startCall():
- After `const offer = await pc.createOffer();`
- Munge: `offer.sdp = removeTransportCC(offer.sdp);`
- Then `await pc.setLocalDescription(offer);`

After receiving answer:
- Before `await pc.setRemoteDescription(answer);`
- Munge: `answer.sdp = removeTransportCC(answer.sdp);`

Add console.log to show munging is happening for debugging.
  </action>
  <verify>
1. `go build ./cmd/chrome-interop/...` compiles
2. Start server: `go run ./cmd/chrome-interop`
3. Open browser to the server URL
4. Check browser console shows SDP munging logs
5. In webrtc-internals, verify the local and remote SDP do NOT contain "transport-cc"
  </verify>
  <done>
- SDP offer sent to server has no transport-cc lines
- SDP answer applied to Chrome has no transport-cc lines
- Chrome falls back to REMB-only bandwidth estimation
- Console shows SDP munging happening
  </done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>SDP munging to remove transport-cc from Chrome WebRTC offer/answer</what-built>
  <how-to-verify>
1. Start the interop server: `go run ./cmd/chrome-interop`
2. Open Chrome to the server URL (e.g., http://localhost:8080)
3. Open chrome://webrtc-internals in another tab FIRST
4. Click "Start Call" on the interop page
5. Check browser console (F12) for:
   - "Munging local SDP to remove transport-cc" log
   - "Munging answer SDP to remove transport-cc" log
6. In webrtc-internals, click on the PeerConnection and examine:
   - Local SDP should NOT contain "transport-cc" or "transport-wide-cc"
   - Remote SDP should NOT contain "transport-cc" or "transport-wide-cc"
   - Should still contain "goog-remb"
7. Check server console shows "REMB sent" messages (our BWE is now active)
  </how-to-verify>
  <resume-signal>Type "approved" if Chrome is using REMB-only, or describe any issues</resume-signal>
</task>

</tasks>

<verification>
- Go build succeeds: `go build ./cmd/chrome-interop/...`
- Server starts without errors
- Browser console shows SDP munging logs
- webrtc-internals shows no transport-cc in negotiated SDP
- Server logs REMB being sent
</verification>

<success_criteria>
- Chrome WebRTC offer has transport-cc removed before sending to server
- Chrome WebRTC answer has transport-cc removed before applying
- Chrome falls back to receiver-side REMB bandwidth estimation
- Our server's BWE interceptor (REMB-only) works as intended
</success_criteria>

<output>
After completion, create `.planning/quick/002-sdp-munge-remove-transport-cc/002-SUMMARY.md`
</output>
