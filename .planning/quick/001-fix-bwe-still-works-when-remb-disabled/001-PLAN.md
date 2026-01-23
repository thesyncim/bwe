---
phase: quick
plan: 001
type: execute
wave: 1
depends_on: []
files_modified:
  - cmd/chrome-interop/server/handler.go
autonomous: true

must_haves:
  truths:
    - "When REMB is disabled, Chrome's availableOutgoingBitrate stops adapting"
    - "Chrome sender-side BWE only works when our REMB feedback is active"
  artifacts:
    - path: "cmd/chrome-interop/server/handler.go"
      provides: "Manual interceptor configuration without TWCC"
      contains: "ConfigureRTCPReports"
  key_links:
    - from: "cmd/chrome-interop/server/handler.go"
      to: "REMB feedback"
      via: "bweinterceptor controls Chrome BWE"
      pattern: "bweFactory|loggingFactory"
---

<objective>
Fix BWE still working when REMB is disabled by removing TWCC feedback from the server.

**Root Cause Analysis:**
The chrome-interop server calls `webrtc.RegisterDefaultInterceptors()` which internally calls `ConfigureTWCCSender()`. This:
1. Registers `transport-cc` header extension
2. Registers TWCC feedback type
3. Adds `twcc.NewSenderInterceptor()` which generates TWCC feedback

Chrome's sender-side BWE uses TWCC feedback independently of REMB. When you "disable" REMB, Chrome's BWE still works because the server is still sending TWCC feedback.

**Fix:** Replace `RegisterDefaultInterceptors` with manual configuration that excludes TWCC, so Chrome's BWE depends solely on our REMB feedback.

Purpose: Ensure Chrome's bandwidth estimation relies entirely on our REMB implementation for proper interop testing.
Output: Modified handler.go that uses manual interceptor configuration without TWCC.
</objective>

<execution_context>
@~/.claude/get-shit-done/workflows/execute-plan.md
@~/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/STATE.md
@cmd/chrome-interop/server/handler.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Replace RegisterDefaultInterceptors with manual interceptor configuration</name>
  <files>cmd/chrome-interop/server/handler.go</files>
  <action>
Replace the call to `webrtc.RegisterDefaultInterceptors(m, i)` with manual configuration that excludes TWCC:

1. Remove the line: `webrtc.RegisterDefaultInterceptors(m, i)`

2. Add manual interceptor configuration that includes only:
   - RTCP reports (Sender/Receiver reports) - call `webrtc.ConfigureRTCPReports(i)`
   - Simulcast extension headers - call `webrtc.ConfigureSimulcastExtensionHeaders(m)`
   - Stats interceptor - call `webrtc.ConfigureStatsInterceptor(i)`
   - NACK generator - create `nack.NewGeneratorInterceptor()` and register NACK feedback

3. Do NOT include:
   - `webrtc.ConfigureTWCCSender()` - this enables Chrome's sender-side BWE
   - `twcc.NewSenderInterceptor()` or `twcc.NewHeaderExtensionInterceptor()`

4. Add a comment explaining why TWCC is intentionally excluded:
   ```go
   // IMPORTANT: Do NOT use RegisterDefaultInterceptors or ConfigureTWCCSender.
   // Those enable TWCC feedback which allows Chrome's sender-side BWE to work
   // independently of our REMB. For proper REMB-only testing, Chrome must
   // rely solely on our receiver-side bandwidth estimates.
   ```

5. The NACK responder is already added (line 73-79), but ensure NACK generator is also added for proper retransmission handling. Also register NACK feedback types on the MediaEngine:
   ```go
   m.RegisterFeedback(webrtc.RTCPFeedback{Type: "nack"}, webrtc.RTPCodecTypeVideo)
   m.RegisterFeedback(webrtc.RTCPFeedback{Type: "nack", Parameter: "pli"}, webrtc.RTPCodecTypeVideo)
   ```

Note: Import `github.com/pion/interceptor/pkg/nack` is already present. May need to add import for `report` package if not present.
  </action>
  <verify>
1. `go build ./cmd/chrome-interop/...` compiles successfully
2. `go vet ./cmd/chrome-interop/...` passes
3. Review handler.go to confirm:
   - No call to RegisterDefaultInterceptors or ConfigureTWCCSender
   - RTCP reports, stats, NACK configured
   - Comment explains TWCC exclusion
  </verify>
  <done>
chrome-interop server no longer enables TWCC feedback, forcing Chrome to rely solely on REMB for bandwidth estimation.
  </done>
</task>

<task type="checkpoint:human-verify" gate="blocking">
  <what-built>Modified chrome-interop server to exclude TWCC feedback</what-built>
  <how-to-verify>
1. Start the server: `go run ./cmd/chrome-interop`
2. Open Chrome and navigate to the server URL (shown in terminal)
3. Open Chrome DevTools -> webrtc-internals (chrome://webrtc-internals)
4. Start video streaming in the test page
5. With REMB enabled (default):
   - Verify `availableOutgoingBitrate` in stats adapts based on network conditions
   - Look for REMB packets in server logs
6. Disable REMB (if there's a toggle, or stop/restart without BWE interceptor):
   - Verify `availableOutgoingBitrate` does NOT adapt (stays static or shows high value)
   - This confirms Chrome is no longer getting congestion feedback

Key observation: When TWCC is removed, Chrome's BWE should only respond to REMB. If REMB is disabled, BWE should stop working entirely.
  </how-to-verify>
  <resume-signal>Type "verified" if Chrome BWE depends on REMB, or describe issues found</resume-signal>
</task>

</tasks>

<verification>
- [ ] `go build ./cmd/chrome-interop/...` passes
- [ ] `go vet ./cmd/chrome-interop/...` passes
- [ ] No TWCC-related code in handler.go
- [ ] Chrome's BWE responds only to REMB feedback (manual verification)
</verification>

<success_criteria>
- Chrome's `availableOutgoingBitrate` adapts when REMB is enabled
- Chrome's `availableOutgoingBitrate` stops adapting when REMB is disabled
- Server logs show REMB packets being sent
- No TWCC feedback negotiated in SDP exchange
</success_criteria>

<output>
After completion, create `.planning/quick/001-fix-bwe-still-works-when-remb-disabled/001-SUMMARY.md`
</output>
