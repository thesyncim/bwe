---
type: quick
id: "004"
title: "Add real E2E BWE test"
files_modified:
  - e2e/bwe_test.go
estimated_context: 25%
---

<objective>
Add an E2E test that validates actual BWE behavior, not just infrastructure.

Current `TestChrome_CanConnect` only verifies the server starts and browser loads.
The new test must verify that REMB packets are being sent by the server and that
Chrome's sender adjusts its bandwidth estimate in response.

Purpose: Prove the BWE implementation actually works end-to-end with a real browser.
Output: New test file `e2e/bwe_test.go` with `TestChrome_BWERespondsToREMB`.
</objective>

<context>
@e2e/browser_test.go (existing smoke test pattern)
@cmd/chrome-interop/server/handler.go (server sends REMB at 500kbps initial, 1s interval)
@cmd/chrome-interop/server/html.go (page has startCall() function, pc is global)
@pkg/bwe/testutil/browser.go (BrowserClient with Navigate, Eval, Page methods)
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create BWE E2E test</name>
  <files>e2e/bwe_test.go</files>
  <action>
Create new test file `e2e/bwe_test.go` with `TestChrome_BWERespondsToREMB`:

1. Setup (copy pattern from browser_test.go):
   - Start server with DefaultConfig
   - Launch browser with DefaultBrowserConfig
   - Navigate to server URL
   - Defer cleanup for both

2. Click "Start Call" programmatically:
   - Use `page.MustElement("#startBtn").MustClick()` to click the button
   - This triggers the `startCall()` JavaScript function

3. Wait for WebRTC connection:
   - Poll `pc.connectionState` via page.Eval until "connected"
   - Use a timeout loop (e.g., 10 seconds max, 200ms intervals)
   - Fail test if connection never establishes

4. Wait for REMB to take effect:
   - Sleep 2-3 seconds after connection to allow REMB to be sent/received
   - Server sends REMB every 1 second (WithFactoryREMBInterval)

5. Get availableOutgoingBitrate from Chrome:
   - Use page.Eval to call pc.getStats()
   - Look for candidate-pair report with nominated=true
   - Extract availableOutgoingBitrate field
   - JavaScript snippet:
     ```javascript
     () => {
       return new Promise((resolve) => {
         pc.getStats().then(stats => {
           let bitrate = null;
           stats.forEach(report => {
             if (report.type === 'candidate-pair' && report.nominated) {
               bitrate = report.availableOutgoingBitrate;
             }
           });
           resolve(bitrate);
         });
       });
     }
     ```

6. Validate BWE behavior:
   - Bitrate must be non-nil (Chrome is reporting stats)
   - Bitrate should be in reasonable range (100kbps - 5Mbps, server's min/max)
   - Log the actual bitrate for debugging
   - If bitrate is near 500kbps (initial), REMB is being respected

Use build tag `//go:build e2e` to match existing test.
  </action>
  <verify>
Run: `go test -tags=e2e ./e2e -run TestChrome_BWERespondsToREMB -v`
Test passes and logs the bandwidth estimate value.
  </verify>
  <done>
TestChrome_BWERespondsToREMB passes, proving:
1. WebRTC connection establishes from headless Chrome
2. Chrome reports availableOutgoingBitrate in stats
3. Bitrate is within expected range (influenced by REMB)
  </done>
</task>

<task type="auto">
  <name>Task 2: Add helper functions for readability</name>
  <files>e2e/bwe_test.go</files>
  <action>
Extract reusable helpers within bwe_test.go to keep test readable:

1. `waitForConnection(t *testing.T, page *rod.Page, timeout time.Duration) error`
   - Polls pc.connectionState until "connected"
   - Returns error if timeout exceeded

2. `getOutgoingBitrate(page *rod.Page) (float64, error)`
   - Executes the getStats() JavaScript
   - Parses and returns the bitrate value
   - Returns error if stats unavailable

These helpers can stay private to the test file. If needed elsewhere later,
they can be moved to testutil package.
  </action>
  <verify>
Test still passes: `go test -tags=e2e ./e2e -run TestChrome_BWERespondsToREMB -v`
  </verify>
  <done>
bwe_test.go has clean test structure with helper functions.
Test is readable and maintainable.
  </done>
</task>

</tasks>

<verification>
```bash
# Run the new test
go test -tags=e2e ./e2e -run TestChrome_BWERespondsToREMB -v

# Run all E2E tests to ensure no regression
go test -tags=e2e ./e2e -v
```
</verification>

<success_criteria>
- [ ] `e2e/bwe_test.go` exists with `TestChrome_BWERespondsToREMB`
- [ ] Test clicks "Start Call" and establishes WebRTC connection
- [ ] Test retrieves `availableOutgoingBitrate` from Chrome stats
- [ ] Test validates bitrate is non-zero and within expected range
- [ ] Test passes consistently (no flakes)
- [ ] Existing `TestChrome_CanConnect` still passes
</success_criteria>
