# Domain Pitfalls: E2E Testing for WebRTC Bandwidth Estimation

**Domain:** End-to-end testing infrastructure for WebRTC/BWE projects
**Researched:** 2026-01-22
**Confidence:** HIGH (based on Pion documentation, WebRTC community patterns, CI/CD best practices)

---

## Executive Summary

Adding E2E testing to a WebRTC project introduces pitfalls across three domains: **browser automation** (headless Chrome flakiness), **network simulation** (non-deterministic conditions), and **CI infrastructure** (resource constraints). The most critical mistakes involve timing assumptions, Chrome version mismatches, and non-deterministic test conditions that lead to flaky tests.

**Key insight:** WebRTC E2E tests are inherently more complex than traditional E2E tests because they involve:
1. Real-time media streams (timing-sensitive)
2. ICE connection establishment (network-dependent)
3. Congestion control feedback loops (BWE behavior verification)
4. Browser-specific RTCP handling (Chrome interop)

---

## Critical Pitfalls

Mistakes that cause test infrastructure rewrites or block CI entirely.

### Critical 1: Chrome/ChromeDriver Version Mismatch

**What goes wrong:** Tests fail with `SessionNotCreatedError: session not created: This version of ChromeDriver only supports Chrome version __`.

**Why it happens:**
- GitHub Actions runners update Chrome automatically
- Local development uses different Chrome version than CI
- ChromeDriver pinned in project but Chrome not pinned
- Multiple CI runners have different Chrome versions

**Consequences:**
- Complete CI failure with no useful diagnostic output
- Flaky failures when some runners have updated Chrome, others haven't
- Developer confusion: "Tests pass locally but fail in CI"

**Warning signs:**
- SessionNotCreatedError in CI logs
- Tests pass on some CI runs but not others
- Tests pass locally but fail in CI consistently

**Prevention:**

1. **Pin Chrome version explicitly in CI:**
   ```yaml
   # GitHub Actions
   - uses: browser-actions/setup-chrome@v1
     with:
       chrome-version: stable  # or specific: "122.0.6261.94"
   - uses: nanasess/setup-chromedriver@v2
   ```

2. **Use version matching in code:**
   ```go
   // Check version compatibility at test startup
   func TestMain(m *testing.M) {
       if err := checkChromeChromeDriverCompat(); err != nil {
           log.Fatalf("Chrome/ChromeDriver mismatch: %v", err)
       }
       os.Exit(m.Run())
   }
   ```

3. **Document version requirements in README:**
   ```markdown
   ## E2E Test Requirements
   - Chrome >= 120 (for WebRTC features)
   - ChromeDriver matching Chrome major version
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup) - establish CI configuration early.

**Sources:**
- [Mux: Lessons learned building headless chrome as a service](https://www.mux.com/blog/lessons-learned-building-headless-chrome-as-a-service)
- [webrtc.org Testing Documentation](https://webrtc.org/getting-started/testing)

---

### Critical 2: ICE Connection Timeouts in CI

**What goes wrong:** WebRTC connections fail to establish, timing out with `iceConnectionState: failed` or hanging indefinitely.

**Why it happens:**
- Default ICE timeouts too short for slow CI runners
- GitHub Actions runners have limited network bandwidth
- CI environments may have restricted networking
- Race between ICE gathering and answer delivery

**Consequences:**
- Flaky tests that pass locally but fail in CI
- Long test runs waiting for timeout expiration
- False negatives: BWE code is correct but tests fail

**Warning signs:**
- `iceConnectionState` stuck on "checking"
- Tests timeout after exactly 30 seconds (browser default)
- Connection works locally, fails in CI
- "Failed to set remote answer sdp" errors

**Prevention:**

1. **Increase ICE timeouts for CI:**
   ```go
   config := webrtc.Configuration{
       ICEServers: []webrtc.ICEServer{},
   }

   // For CI, set generous timeouts via SettingEngine
   se := webrtc.SettingEngine{}
   se.SetICETimeouts(
       30*time.Second,  // disconnectedTimeout
       60*time.Second,  // failedTimeout
       5*time.Second,   // keepAliveInterval
   )
   ```

2. **Use local-only ICE for unit tests:**
   ```go
   // Skip STUN/TURN gathering - only use host candidates
   se.SetNetworkTypes([]webrtc.NetworkType{
       webrtc.NetworkTypeUDP4,
   })
   se.SetIPFilter(func(ip net.IP) bool {
       return ip.IsLoopback()  // Only localhost
   })
   ```

3. **Add explicit connection wait with logging:**
   ```go
   // Wait for connection with diagnostic output
   connected := make(chan struct{})
   pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
       log.Printf("Connection state: %s", state)
       if state == webrtc.PeerConnectionStateConnected {
           close(connected)
       }
   })

   select {
   case <-connected:
       // Success
   case <-time.After(60 * time.Second):
       t.Fatalf("Connection timeout, last ICE state: %s", pc.ICEConnectionState())
   }
   ```

4. **Detect CI environment and adjust:**
   ```go
   func isCI() bool {
       return os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") == "true"
   }

   func getConnectionTimeout() time.Duration {
       if isCI() {
           return 60 * time.Second  // Generous for CI
       }
       return 10 * time.Second  // Fast for local
   }
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup) - connection configuration is foundational.

**Sources:**
- [Pion WebRTC: Slow connection times issue #460](https://github.com/pion/webrtc/issues/460)
- [AWS Kinesis WebRTC Troubleshooting](https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/troubleshooting.html)

---

### Critical 3: Runaway Headless Browser Processes

**What goes wrong:** Browser instances accumulate, consuming all system memory, eventually causing CI runner crash or test hangs.

**Why it happens:**
- Test failures leave browser processes running
- Browser cleanup not in `defer` blocks
- Panic recovery doesn't clean up browsers
- Parallel test runs spawn more browsers than resources allow

**Consequences:**
- CI runners run out of memory
- Subsequent test runs fail due to port conflicts
- GitHub Actions job termination with no logs
- "Chrome crashed" errors with no diagnostic output

**Warning signs:**
- Test timeouts increasing over time
- CI jobs killed without error message
- `ps aux | grep chrome` shows orphaned processes
- Memory usage increases during test suite

**Prevention:**

1. **Always use defer for cleanup:**
   ```go
   func TestBrowserInterop(t *testing.T) {
       ctx, cancel := chromedp.NewContext(context.Background())
       defer cancel()  // CRITICAL: always cleanup

       // Also handle browser process explicitly
       defer chromedp.Cancel(ctx)

       // Test code here
   }
   ```

2. **Set process-level timeout:**
   ```go
   // Limit browser lifetime regardless of test outcome
   ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
   defer cancel()

   allocCtx, allocCancel := chromedp.NewExecAllocator(ctx,
       chromedp.Flag("headless", true),
       chromedp.Flag("disable-gpu", true),
   )
   defer allocCancel()
   ```

3. **Implement cleanup in test teardown:**
   ```go
   func TestMain(m *testing.M) {
       code := m.Run()

       // Force cleanup any orphaned Chrome processes
       killOrphanedChromes()

       os.Exit(code)
   }

   func killOrphanedChromes() {
       // Linux/macOS
       exec.Command("pkill", "-f", "use-fake-ui-for-media-stream").Run()
   }
   ```

4. **Limit parallel browser tests:**
   ```go
   // In test file
   var browserSemaphore = make(chan struct{}, 2)  // Max 2 concurrent browsers

   func TestWithBrowser(t *testing.T) {
       browserSemaphore <- struct{}{}
       defer func() { <-browserSemaphore }()

       // Browser test code
   }
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup) - resource management is foundational.

**Sources:**
- [Mux: Lessons learned building headless chrome as a service](https://www.mux.com/blog/lessons-learned-building-headless-chrome-as-a-service)
- [ZenRows: Chromedp Golang Tutorial](https://www.zenrows.com/blog/chromedp)

---

### Critical 4: Non-Deterministic Network Simulation

**What goes wrong:** Network simulation produces different results each run, making tests flaky and debugging impossible.

**Why it happens:**
- Random timing in packet loss/delay injection
- System clock variations affect delay measurements
- tc/netem rules applied inconsistently
- Interaction with system network stack varies by load

**Consequences:**
- Tests pass 80% of the time, fail 20%
- Cannot reproduce failures locally
- BWE behavior changes appear as test failures
- Developers disable flaky tests rather than fixing

**Warning signs:**
- Test results vary on identical code
- Retry helps but doesn't reliably fix
- Tests more flaky on busy CI runners
- "Works on my machine" syndrome

**Prevention:**

1. **Use Pion vnet for deterministic simulation:**
   ```go
   // Pion vnet provides reproducible network behavior
   router := vnet.NewRouter()

   // Create virtual networks with controlled delay
   net1 := vnet.NewNet(&vnet.NetConfig{
       StaticIPs: []string{"192.168.0.1"},
   })
   router.AddNet(net1)

   // Add delay filter for controlled latency
   delayFilter := vnet.NewDelayFilter(net1, 50*time.Millisecond)
   ```

2. **Separate deterministic from probabilistic tests:**
   ```go
   // Deterministic: Exact delay, no randomness
   func TestBWE_FixedDelay(t *testing.T) {
       // Use vnet with fixed 100ms delay
       // Expect specific bandwidth estimate
   }

   // Probabilistic: Mark as potentially flaky
   func TestBWE_RandomLoss(t *testing.T) {
       if testing.Short() {
           t.Skip("Skipping probabilistic test in short mode")
       }
       // Use statistical assertions
       // Pass if 95% of runs succeed
   }
   ```

3. **Use seeded random for reproducibility:**
   ```go
   type NetworkSimulator struct {
       rng *rand.Rand
   }

   func NewNetworkSimulator(seed int64) *NetworkSimulator {
       return &NetworkSimulator{
           rng: rand.New(rand.NewSource(seed)),
       }
   }

   func TestWithSeed(t *testing.T) {
       // Log seed so failures can be reproduced
       seed := time.Now().UnixNano()
       t.Logf("Test seed: %d (use this to reproduce)", seed)
       sim := NewNetworkSimulator(seed)
   }
   ```

4. **Statistical assertions instead of exact values:**
   ```go
   // BAD: Exact value expectation
   assert.Equal(t, 1_000_000, estimate)  // Will fail randomly

   // GOOD: Range-based assertion
   assert.InDelta(t, 1_000_000, estimate, 100_000)  // 10% tolerance

   // BETTER: Statistical assertion over multiple runs
   estimates := runMultipleTimes(10)
   mean := average(estimates)
   assert.InDelta(t, 1_000_000, mean, 50_000)
   ```

**Phase to address:** Phase 2 (Network Simulation) - design for determinism from the start.

**Sources:**
- [Pion transport vnet documentation](https://github.com/pion/transport/tree/master/vnet)
- [Pion WebRTC vnet tests](https://github.com/pion/webrtc/blob/master/vnet_test.go)

---

## High-Severity Pitfalls

Mistakes that cause significant debugging time or unreliable tests.

### High 1: Missing Chrome Flags for WebRTC

**What goes wrong:** Tests fail because Chrome requires manual camera/microphone permission, or uses real devices which aren't available in CI.

**Why it happens:**
- Developers don't know required flags
- Flags changed between Chrome versions
- Different flags needed for different test types
- Headless mode has different requirements than headed

**Prevention:**

1. **Standard flags for headless WebRTC testing:**
   ```go
   opts := []chromedp.ExecAllocatorOption{
       chromedp.Flag("headless", true),
       chromedp.Flag("disable-gpu", true),
       chromedp.Flag("no-sandbox", true),  // Required in Docker/CI
       chromedp.Flag("disable-dev-shm-usage", true),  // Prevents crashes

       // WebRTC-specific flags
       chromedp.Flag("use-fake-ui-for-media-stream", true),  // Auto-grant permissions
       chromedp.Flag("use-fake-device-for-media-stream", true),  // Use test pattern
       chromedp.Flag("autoplay-policy", "no-user-gesture-required"),

       // Optional: Use specific test media
       chromedp.Flag("use-file-for-fake-video-capture", "/path/to/test.y4m"),
       chromedp.Flag("use-file-for-fake-audio-capture", "/path/to/test.wav"),
   }
   ```

2. **Create reusable allocator factory:**
   ```go
   func NewWebRTCTestContext(t *testing.T) (context.Context, context.CancelFunc) {
       opts := getWebRTCChromeOpts()
       allocCtx, _ := chromedp.NewExecAllocator(context.Background(), opts...)
       ctx, cancel := chromedp.NewContext(allocCtx)
       t.Cleanup(cancel)
       return ctx, cancel
   }
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup) - establish Chrome configuration early.

**Sources:**
- [webrtc.org Testing](https://webrtc.org/getting-started/testing)
- [Daily.co: Headless WebRTC Testing](https://www.daily.co/blog/how-to-make-a-headless-robot-to-test-webrtc-in-your-daily-app/)

---

### High 2: ICE Candidate Race Conditions

**What goes wrong:** Tests fail intermittently because ICE candidates aren't exchanged in the expected order.

**Why it happens:**
- ICE gathering is asynchronous
- Trickle ICE has race between signaling and ICE pings
- Answer delivered before all candidates gathered
- Candidates generated faster than signaling can exchange

**Warning signs:**
- `iceConnectionState` transitions to "connected" then immediately to "checking"
- Connection works sometimes, fails randomly
- Different failure rate on fast vs slow machines

**Prevention:**

1. **Use complete ICE gathering before signaling:**
   ```go
   // Wait for ICE gathering to complete before exchanging SDP
   gatherComplete := webrtc.GatheringCompletePromise(pc)

   offer, _ := pc.CreateOffer(nil)
   pc.SetLocalDescription(offer)

   <-gatherComplete  // Wait for all candidates

   // Now exchange pc.LocalDescription() which contains all candidates
   ```

2. **Handle trickle ICE correctly:**
   ```go
   // If using trickle ICE, handle candidate timing
   pc.OnICECandidate(func(c *webrtc.ICECandidate) {
       if c == nil {
           return  // Gathering complete
       }
       // Queue candidates until remote description is set
       candidateQueue = append(candidateQueue, c)
   })

   // After setting remote description, add queued candidates
   pc.SetRemoteDescription(answer)
   for _, c := range candidateQueue {
       pc.AddICECandidate(c.ToJSON())
   }
   ```

3. **Log state transitions for debugging:**
   ```go
   pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
       t.Logf("ICE state: %s at %v", state, time.Now())
   })
   pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
       t.Logf("Connection state: %s at %v", state, time.Now())
   })
   ```

**Phase to address:** Phase 3 (Pion Integration Tests) - critical for PeerConnection tests.

**Sources:**
- [Pion WebRTC issue #2578: ICEConnectionState changes may be reported incorrectly](https://github.com/pion/webrtc/issues/2578)
- [BlogGeek.me: ICE candidates and active connections](https://bloggeek.me/webrtc-ice-connection/)

---

### High 3: REMB Verification Without Chrome Internals Access

**What goes wrong:** Tests claim Chrome received REMB, but actually can't verify it programmatically.

**Why it happens:**
- `chrome://webrtc-internals` is not accessible via DevTools Protocol
- No direct API to read RTCP stats from JavaScript
- getStats() doesn't expose received REMB details
- Testing requires indirect inference

**Consequences:**
- False confidence: tests pass but REMB might not be working
- Chrome interop issues caught late (production, not CI)
- Manual verification still required despite "automated" tests

**Prevention:**

1. **Use sender-side bitrate adaptation as proxy:**
   ```javascript
   // In Chrome page, monitor outgoing bitrate
   pc.getSenders().forEach(sender => {
       const params = sender.getParameters();
       // REMB receipt causes encoder to adjust
       // Track: params.encodings[0].maxBitrate
   });

   // Poll and verify bitrate decreases when congestion simulated
   ```

2. **Log REMB sending from server side:**
   ```go
   // Already implemented in chrome-interop server
   // Verify test by checking server logs for "REMB sent" messages
   ```

3. **Capture RTCP with packet interception:**
   ```go
   // Use Pion interceptor to log all outgoing RTCP
   type RTCPLogger struct {
       interceptor.Interceptor
       rembsSent int
   }

   func (l *RTCPLogger) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
       return interceptor.RTCPWriterFunc(func(pkts []rtcp.Packet, attrs interceptor.Attributes) (int, error) {
           for _, pkt := range pkts {
               if _, ok := pkt.(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
                   l.rembsSent++
               }
           }
           return writer.Write(pkts, attrs)
       })
   }
   ```

4. **Behavioral verification instead of RTCP inspection:**
   ```go
   // Verify BWE behavior indirectly:
   // 1. Inject congestion (delay/loss)
   // 2. Verify REMB values decrease (server-side)
   // 3. Verify Chrome encoder bitrate decreases (getStats)
   ```

**Phase to address:** Phase 3 (Chrome Interop Tests) - design verification strategy carefully.

**Sources:**
- [Flussonic: TWCC Server-Side Implementation](https://flussonic.com/blog/news/transport-cc)
- [httptoolkit: Intercepting WebRTC traffic](https://httptoolkit.com/blog/intercepting-webrtc-traffic/)

---

### High 4: GitHub Actions Resource Limits

**What goes wrong:** Tests timeout or fail due to GitHub Actions runner constraints.

**Why it happens:**
- Free tier: 2 vCPU, 7GB RAM
- WebRTC + Chrome is resource-intensive
- Multiple parallel tests exhaust resources
- Long-running tests hit 6-hour job limit

**Warning signs:**
- Tests slower in CI than locally
- Random timeouts on resource-intensive tests
- Job cancelled without clear error
- Memory-related Chrome crashes

**Prevention:**

1. **Limit test parallelism:**
   ```yaml
   # In GitHub Actions workflow
   - name: Run E2E tests
     run: go test -parallel 1 -timeout 30m ./e2e/...
   ```

2. **Use build matrix for isolation:**
   ```yaml
   strategy:
     matrix:
       test-group: [browser, network, integration]
     fail-fast: false

   steps:
     - run: go test ./e2e/${{ matrix.test-group }}/...
   ```

3. **Separate fast and slow tests:**
   ```go
   func TestSlowBrowserInterop(t *testing.T) {
       if os.Getenv("SLOW_TESTS") != "true" {
           t.Skip("Skipping slow test, set SLOW_TESTS=true")
       }
   }
   ```

4. **Monitor resource usage:**
   ```yaml
   - name: Check resources before test
     run: |
       free -h
       nproc
       df -h
   ```

**Phase to address:** Phase 4 (CI Integration) - design for GitHub Actions constraints.

**Sources:**
- [GitHub Actions: Usage limits](https://docs.github.com/en/actions/learn-github-actions/usage-limits-billing-and-administration)
- [GitHub Discussion #26595: Actions timeout](https://github.com/orgs/community/discussions/26595)

---

## Medium-Severity Pitfalls

Mistakes that cause debugging headaches or test brittleness.

### Medium 1: Xvfb Configuration for Headless Linux

**What goes wrong:** Chrome fails to start with "Unable to open X display" even with `--headless`.

**Why it happens:**
- Older Chrome versions required X even in headless mode
- Some Chrome features still need framebuffer
- Docker images may lack display server
- New headless mode (`--headless=new`) has different requirements

**Prevention:**

1. **Use Xvfb in CI:**
   ```yaml
   - name: Install Xvfb
     run: sudo apt-get install -y xvfb

   - name: Run tests with Xvfb
     run: xvfb-run --auto-servernum go test ./e2e/...
   ```

2. **Or use new headless mode (Chrome 112+):**
   ```go
   chromedp.Flag("headless", "new"),  // New headless mode
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup).

---

### Medium 2: tc/netem Not Available in CI

**What goes wrong:** Network simulation tests fail because `tc` command not available or requires root.

**Why it happens:**
- GitHub Actions runners don't have tc by default
- tc requires CAP_NET_ADMIN capability
- Docker containers need privileged mode for tc
- macOS runners don't have tc at all

**Prevention:**

1. **Use Pion vnet instead of tc for Go tests:**
   ```go
   // vnet works without special permissions
   router := vnet.NewRouter()
   // Add delay, loss at application level
   ```

2. **If tc is required, use Docker with privileges:**
   ```yaml
   services:
     network-test:
       image: your-test-image
       cap_add:
         - NET_ADMIN
   ```

3. **Detect tc availability and skip:**
   ```go
   func TestWithTC(t *testing.T) {
       if _, err := exec.LookPath("tc"); err != nil {
           t.Skip("tc not available")
       }
       if os.Getuid() != 0 {
           t.Skip("tc requires root")
       }
   }
   ```

**Phase to address:** Phase 2 (Network Simulation) - choose simulation approach based on CI constraints.

**Sources:**
- [tc-netem man page](https://man7.org/linux/man-pages/man8/tc-netem.8.html)
- [WebRTC.ventures: Simulating unstable networks](https://webrtc.ventures/2024/06/how-do-you-simulate-unstable-networks-for-testing-live-event-streaming-applications/)

---

### Medium 3: Test Video/Audio File Format Issues

**What goes wrong:** Chrome rejects custom test media files with no useful error.

**Why it happens:**
- Wrong video format (must be Y4M, not MP4/WebM)
- Wrong audio format (must be WAV)
- File path issues in headless mode
- Resolution/framerate incompatible with encoder

**Prevention:**

1. **Use correct formats:**
   ```bash
   # Convert video to Y4M
   ffmpeg -i input.mp4 -pix_fmt yuv420p output.y4m

   # Convert audio to WAV
   ffmpeg -i input.mp3 -ar 48000 -ac 1 output.wav
   ```

2. **Use Chrome's built-in test patterns if custom media not needed:**
   ```go
   // Just use --use-fake-device-for-media-stream without file path
   // Chrome provides green-and-black test pattern
   ```

3. **Verify file accessibility:**
   ```go
   chromedp.Flag("use-file-for-fake-video-capture",
       mustAbsPath(t, "testdata/test.y4m")),
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup).

---

### Medium 4: BWE Convergence Time Assumptions

**What goes wrong:** Tests fail because they don't wait long enough for BWE to stabilize.

**Why it happens:**
- GCC/BWE has intentionally slow ramp-up (300kbps start)
- Bandwidth probing takes multiple RTTs
- AIMD decrease is fast but increase is slow
- CI timing makes convergence slower

**Warning signs:**
- Tests pass with long sleeps, fail with short ones
- Bandwidth estimate stuck at initial value
- Inconsistent estimates between runs

**Prevention:**

1. **Wait for convergence, not fixed time:**
   ```go
   func waitForConvergence(t *testing.T, estimator *bwe.BandwidthEstimator, target uint64, timeout time.Duration) {
       deadline := time.Now().Add(timeout)
       var lastEstimate uint64

       for time.Now().Before(deadline) {
           estimate := estimator.GetEstimate()
           if isWithinTolerance(estimate, target, 0.1) && estimate == lastEstimate {
               return  // Converged
           }
           lastEstimate = estimate
           time.Sleep(100 * time.Millisecond)
       }
       t.Fatalf("BWE did not converge: got %d, want ~%d", lastEstimate, target)
   }
   ```

2. **Use relative assertions:**
   ```go
   // BAD: Expect exact value after 5s
   time.Sleep(5 * time.Second)
   assert.Equal(t, 1_000_000, estimate)

   // GOOD: Expect trend direction
   initialEstimate := estimator.GetEstimate()
   simulateCongestion(100 * time.Millisecond)
   time.Sleep(5 * time.Second)
   finalEstimate := estimator.GetEstimate()
   assert.Less(t, finalEstimate, initialEstimate)
   ```

**Phase to address:** Phase 3 (Integration Tests) - design test assertions for BWE dynamics.

**Sources:**
- [webrtchacks: Probing WebRTC Bandwidth Probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/)
- [Meta Engineering: Optimizing RTC bandwidth estimation](https://engineering.fb.com/2024/03/20/networking-traffic/optimizing-rtc-bandwidth-estimation-machine-learning/)

---

### Medium 5: Flaky Test Retry Masking Real Issues

**What goes wrong:** CI is configured to retry failed tests, masking genuine flakiness that should be fixed.

**Why it happens:**
- Easy workaround for flaky tests
- Retries pass often enough to ignore
- Root cause investigation deferred
- Technical debt accumulates

**Prevention:**

1. **Track flaky test frequency:**
   ```yaml
   - name: Run tests with flaky tracking
     run: |
       go test -v ./e2e/... 2>&1 | tee test.log
       grep "FAIL" test.log >> flaky-history.log || true
   ```

2. **Limit retries and require investigation:**
   ```yaml
   - name: Run E2E tests
     uses: nick-invision/retry@v2
     with:
       max_attempts: 2  # Only 1 retry
       command: go test ./e2e/...

   - name: Report flakiness
     if: failure()
     run: echo "::warning::Test required retry - investigate flakiness"
   ```

3. **Quarantine persistently flaky tests:**
   ```go
   func TestFlaky(t *testing.T) {
       t.Skip("QUARANTINED: Tracking issue #123")
   }
   ```

**Phase to address:** Phase 4 (CI Integration) - establish flakiness policy early.

---

## Low-Severity Pitfalls

Mistakes that cause minor inconvenience but are easily fixed.

### Low 1: Logs Lost on Test Failure

**What goes wrong:** Test fails but Chrome/server logs aren't captured, making debugging impossible.

**Prevention:**

1. **Capture browser console on failure:**
   ```go
   chromedp.ListenTarget(ctx, func(ev interface{}) {
       if ev, ok := ev.(*runtime.EventConsoleAPICalled); ok {
           t.Logf("Console: %s", ev.Args[0].Value)
       }
   })
   ```

2. **Dump state on test failure:**
   ```go
   t.Cleanup(func() {
       if t.Failed() {
           t.Logf("PeerConnection state: %s", pc.ConnectionState())
           t.Logf("ICE state: %s", pc.ICEConnectionState())
           // Dump any captured RTCP/RTP stats
       }
   })
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup).

---

### Low 2: Hardcoded Ports Causing Conflicts

**What goes wrong:** Tests fail with "address already in use" when running in parallel or after failed cleanup.

**Prevention:**

1. **Use dynamic port allocation:**
   ```go
   listener, _ := net.Listen("tcp", "localhost:0")
   port := listener.Addr().(*net.TCPAddr).Port
   listener.Close()
   // Use port for test server
   ```

2. **Include unique identifier in server:**
   ```go
   server := &http.Server{
       Addr: fmt.Sprintf("localhost:%d", getAvailablePort()),
   }
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup).

---

### Low 3: Test Data Not Cleaned Between Runs

**What goes wrong:** Tests interfere with each other due to shared state from previous runs.

**Prevention:**

1. **Use t.TempDir() for test artifacts:**
   ```go
   func TestWithFiles(t *testing.T) {
       dir := t.TempDir()  // Auto-cleaned after test
       // Write test files to dir
   }
   ```

2. **Reset singleton state:**
   ```go
   func TestMain(m *testing.M) {
       code := m.Run()
       resetGlobalState()  // Clean up any singletons
       os.Exit(code)
   }
   ```

**Phase to address:** Phase 1 (Test Infrastructure Setup).

---

## Phase-Specific Warnings

| Phase | Likely Pitfall | Mitigation |
|-------|----------------|------------|
| Phase 1: Infrastructure | Chrome version mismatch | Pin versions in CI workflow |
| Phase 1: Infrastructure | Runaway browser processes | Use defer + cleanup in TestMain |
| Phase 2: Network Sim | Non-deterministic simulation | Use Pion vnet, not tc |
| Phase 2: Network Sim | tc not available | Detect and skip gracefully |
| Phase 3: Integration | ICE connection timeouts | Increase timeouts for CI |
| Phase 3: Integration | REMB verification | Use behavioral tests + logging |
| Phase 3: Integration | BWE convergence time | Wait for convergence, not time |
| Phase 4: CI | Resource limits | Limit parallelism, use matrix |
| Phase 4: CI | Flaky test masking | Track flakiness, limit retries |

---

## Testing Checklist Before CI Integration

Before declaring E2E test infrastructure complete:

### Infrastructure Validation
- [ ] Chrome/ChromeDriver version pinned and matching
- [ ] All Chrome flags for WebRTC configured
- [ ] Browser cleanup verified (no orphaned processes)
- [ ] Port allocation is dynamic
- [ ] Test artifacts use t.TempDir()

### Network Simulation Validation
- [ ] Deterministic mode works (Pion vnet)
- [ ] Probabilistic mode has seeded random
- [ ] tc-based tests skip gracefully when unavailable
- [ ] Simulation parameters documented

### Connection Validation
- [ ] ICE timeouts configured for CI
- [ ] Connection state logging enabled
- [ ] Complete ICE gathering before signaling
- [ ] Trickle ICE handled correctly if used

### BWE Validation
- [ ] Convergence waits are condition-based, not time-based
- [ ] REMB sending is logged and verifiable
- [ ] Behavioral assertions, not exact values
- [ ] Tolerance bounds documented

### CI Validation
- [ ] Resource limits documented
- [ ] Parallelism limited appropriately
- [ ] Flaky test policy established
- [ ] Failure artifacts captured (logs, state)

---

## Sources

**Official Documentation:**
- [webrtc.org: Testing WebRTC Applications](https://webrtc.org/getting-started/testing)
- [Pion transport vnet](https://github.com/pion/transport/tree/master/vnet)
- [tc-netem man page](https://man7.org/linux/man-pages/man8/tc-netem.8.html)

**Community Patterns:**
- [Mux: Lessons learned building headless chrome as a service](https://www.mux.com/blog/lessons-learned-building-headless-chrome-as-a-service)
- [Daily.co: Headless WebRTC Testing](https://www.daily.co/blog/how-to-make-a-headless-robot-to-test-webrtc-in-your-daily-app/)
- [webrtchacks: Probing WebRTC Bandwidth Probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/)

**Pion-Specific:**
- [Pion WebRTC issue #460: Slow connection times](https://github.com/pion/webrtc/issues/460)
- [Pion WebRTC issue #712: Enhance tests using vnet](https://github.com/pion/webrtc/issues/712)
- [Pion WebRTC issue #2578: ICE state race conditions](https://github.com/pion/webrtc/issues/2578)

**CI/CD:**
- [GitHub Actions timeout discussion](https://github.com/orgs/community/discussions/26595)
- [ZenRows: Chromedp Golang Tutorial](https://www.zenrows.com/blog/chromedp)

**Confidence:** HIGH - Based on Pion documentation, WebRTC community patterns, and established CI best practices.
