# Architecture Patterns: E2E Testing Infrastructure

**Project:** GCC Receiver-Side BWE - E2E Testing (v1.2)
**Domain:** WebRTC testing, browser automation, network simulation
**Researched:** 2026-01-22
**Confidence:** HIGH

## Executive Summary

The BWE library has a mature testing foundation: unit tests colocated with source files, integration tests in `pkg/bwe/interceptor/integration_test.go`, a `testutil/` package for trace replay, and manual Chrome verification via `cmd/chrome-interop/`. The v1.2 E2E testing milestone needs to **automate** what is currently manual (browser interop) and **extend** testing to cover network conditions.

**Recommended architecture:**
1. Create `e2e/` top-level directory for E2E test infrastructure (separate from unit tests)
2. Use build tags (`//go:build e2e`) to keep E2E tests out of normal `go test` runs
3. Adopt [chromedp](https://github.com/chromedp/chromedp) for browser automation (pure Go, no external dependencies)
4. Create network simulation helpers using tc/netem (Linux) with Docker containers for CI
5. Extend existing `cmd/chrome-interop/` as the test server, add automation hooks

**Key integration points:**
- `pkg/bwe/testutil/` - Extend with E2E helpers (browser client, network conditions)
- `cmd/chrome-interop/` - Evolve from manual demo to headless test server
- `.github/workflows/` - New CI workflow with browser + network simulation tests

## Current Test Architecture

### Existing Test Infrastructure

```
bwe/
+-- pkg/bwe/
|   +-- *_test.go              # Unit tests (colocated with source)
|   |   +-- estimator_test.go     # Core algorithm tests
|   |   +-- validation_test.go    # VALID-01 divergence tests
|   |   +-- soak_test.go          # VALID-04 24-hour accelerated tests
|   |   +-- benchmark_test.go     # Performance tests (0 allocs/op)
|   +-- testutil/
|   |   +-- traces.go             # Synthetic packet trace generators
|   |   +-- reference_trace.go    # Reference trace replay infrastructure
|   +-- interceptor/
|       +-- interceptor_test.go   # Unit tests for interceptor
|       +-- integration_test.go   # Pion interceptor integration tests
|       +-- benchmark_test.go     # Interceptor benchmarks
+-- cmd/chrome-interop/
|   +-- main.go                # Manual Chrome verification server
+-- testdata/
    +-- reference_congestion.json  # Reference trace data
```

### Test Categories (Current)

| Category | Location | Run Command | CI Status |
|----------|----------|-------------|-----------|
| Unit tests | `*_test.go` colocated | `go test ./...` | Auto |
| Benchmarks | `*_test.go` colocated | `go test -bench=.` | Auto |
| Integration | `interceptor/integration_test.go` | `go test ./...` | Auto |
| Soak tests | `soak_test.go` | `go test -run Soak` | Manual (long) |
| Chrome interop | `cmd/chrome-interop/` | Manual browser | Manual |

### What Needs Automation

1. **Chrome interop (VALID-02)** - Currently requires manual: open browser, check webrtc-internals
2. **Network simulation** - Not yet implemented: packet loss, bandwidth variation, latency
3. **Full PeerConnection tests** - Currently only interceptor-level, need full connection
4. **CI integration** - Soak tests and Chrome tests not in CI pipeline

## Recommended Architecture

### Directory Structure

```
bwe/
+-- pkg/bwe/
|   +-- *_test.go              # Unit tests (unchanged)
|   +-- testutil/
|       +-- traces.go          # Existing trace generators
|       +-- reference_trace.go # Existing replay infra
|       +-- browser.go         # NEW: Browser automation helpers
|       +-- network.go         # NEW: Network simulation helpers
+-- e2e/                       # NEW: E2E test directory
|   +-- e2e_test.go            # Test entry point (requires build tag)
|   +-- browser_test.go        # Chrome/browser automation tests
|   +-- network_test.go        # Network condition tests
|   +-- peerconnection_test.go # Full PeerConnection tests
|   +-- testdata/              # E2E-specific test data
|   +-- docker/
|       +-- Dockerfile.chrome  # Chrome headless container
|       +-- docker-compose.yml # Network simulation setup
+-- cmd/chrome-interop/
|   +-- main.go                # Evolve: Add programmatic control
|   +-- server.go              # NEW: Separate server logic
|   +-- api.go                 # NEW: Control API for tests
+-- .github/workflows/
    +-- test.yml               # Existing (if any)
    +-- e2e.yml                # NEW: E2E workflow
```

### Component Boundaries

| Component | Responsibility | Depends On |
|-----------|---------------|------------|
| `pkg/bwe/testutil/browser.go` | Browser automation primitives (chromedp wrapper) | chromedp |
| `pkg/bwe/testutil/network.go` | Network condition helpers (tc/netem wrapper) | Docker/tc (optional) |
| `e2e/browser_test.go` | Chrome REMB verification tests | testutil/browser, cmd/chrome-interop |
| `e2e/network_test.go` | Network simulation tests | testutil/network, pkg/bwe |
| `e2e/peerconnection_test.go` | Full Pion PeerConnection tests | pion/webrtc, pkg/bwe/interceptor |
| `cmd/chrome-interop/server.go` | WebRTC test server (SDP exchange) | pion/webrtc, pkg/bwe/interceptor |

### Data Flow

```
Browser Test Flow:
-----------------
1. e2e/browser_test.go starts cmd/chrome-interop server
2. chromedp launches headless Chrome
3. Chrome navigates to test server, initiates WebRTC
4. BWE interceptor receives RTP, sends REMB
5. chromedp queries webrtc-internals (or injects JS to capture stats)
6. Test asserts REMB values visible in Chrome stats

Network Simulation Flow:
-----------------------
1. e2e/network_test.go configures network conditions (via Docker or tc)
2. Test server runs with BWE interceptor
3. Synthetic RTP stream with applied network impairments
4. BWE algorithm responds to conditions
5. Test asserts estimate adapts appropriately (decrease under loss, etc.)

PeerConnection Test Flow:
------------------------
1. e2e/peerconnection_test.go creates two Pion PeerConnections
2. One peer sends video (with abs-send-time extension)
3. Other peer has BWE interceptor
4. Verify REMB packets sent, estimate converges
5. No browser needed - pure Pion-to-Pion test
```

## Integration Points with Existing Infrastructure

### Integration Point 1: testutil Package

**Current state:** `pkg/bwe/testutil/` provides trace generators and replay infrastructure.

**Extension:**
```go
// pkg/bwe/testutil/browser.go
package testutil

import (
    "context"
    "github.com/chromedp/chromedp"
)

// BrowserClient wraps chromedp for WebRTC testing
type BrowserClient struct {
    ctx    context.Context
    cancel context.CancelFunc
}

// NewBrowserClient creates headless Chrome instance with WebRTC flags
func NewBrowserClient() (*BrowserClient, error)

// StartCall initiates WebRTC call to given server
func (b *BrowserClient) StartCall(serverURL string) error

// GetREMBStats retrieves REMB-related stats from webrtc-internals
func (b *BrowserClient) GetREMBStats() (*REMBStats, error)

// Close terminates browser
func (b *BrowserClient) Close()
```

**Why testutil (not e2e):** Browser helpers are reusable infrastructure that could be used in future testing scenarios. Place utilities in `testutil/`, actual test files in `e2e/`.

### Integration Point 2: cmd/chrome-interop Server

**Current state:** Embedded HTML, hardcoded behavior, manual interaction only.

**Extension:** Separate into reusable server with control API.

```go
// cmd/chrome-interop/server.go
package main

type TestServer struct {
    pc       *webrtc.PeerConnection
    bwe      *bweinterceptor.BWEInterceptor
    stats    *Stats
}

// NewTestServer creates configurable test server
func NewTestServer(opts ...ServerOption) *TestServer

// Serve starts HTTP server on given address
func (s *TestServer) Serve(addr string) error

// GetEstimate returns current BWE estimate (for programmatic access)
func (s *TestServer) GetEstimate() int64

// GetREMBCount returns number of REMB packets sent
func (s *TestServer) GetREMBCount() int
```

**Server as importable package:** Tests can import and control the server directly:
```go
// e2e/browser_test.go
func TestChromeREMBAccepted(t *testing.T) {
    server := chromeinterop.NewTestServer()
    go server.Serve(":8080")
    defer server.Close()

    browser, _ := testutil.NewBrowserClient()
    defer browser.Close()

    browser.StartCall("http://localhost:8080")
    time.Sleep(5 * time.Second)

    stats, _ := browser.GetREMBStats()
    assert.Greater(t, server.GetREMBCount(), 0)
    assert.Greater(t, stats.ReceivedBitrate, 0)
}
```

### Integration Point 3: Existing Test Patterns

**Pattern: Build tags for E2E tests**

The existing codebase uses `testing.Short()` for soak tests. E2E tests should use build tags:

```go
// e2e/e2e_test.go
//go:build e2e

package e2e

// All E2E tests in this package require:
//   go test -tags e2e ./e2e/...
```

**Pattern: Table-driven tests with subtests**

Existing tests use testify/require and t.Run extensively. E2E tests should follow:

```go
func TestNetworkConditions(t *testing.T) {
    conditions := []struct {
        name     string
        loss     float64
        latency  time.Duration
        expected func(estimate int64) bool
    }{
        {"stable", 0, 10*time.Millisecond, func(e int64) bool { return e > 400000 }},
        {"lossy", 0.05, 10*time.Millisecond, func(e int64) bool { return e < 400000 }},
        {"high_latency", 0, 200*time.Millisecond, func(e int64) bool { return e > 0 }},
    }

    for _, tc := range conditions {
        t.Run(tc.name, func(t *testing.T) {
            // Apply network condition
            // Run test
            // Assert
        })
    }
}
```

## Patterns to Follow

### Pattern 1: Chromedp for Browser Automation

**What:** Use chromedp (pure Go Chrome DevTools Protocol client) for browser tests.

**When:** Any test that needs real browser WebRTC behavior.

**Why:**
- Pure Go, no CGO, no external WebDriver
- Direct Chrome DevTools Protocol access
- Can query internal state (webrtc-internals equivalent)
- Works in CI with headless Chrome

**Example:**
```go
func startChromeWithWebRTC(ctx context.Context, url string) (context.Context, context.CancelFunc) {
    opts := append(chromedp.DefaultExecAllocatorOptions[:],
        chromedp.Flag("use-fake-ui-for-media-stream", true),
        chromedp.Flag("use-fake-device-for-media-stream", true),
        chromedp.Flag("allow-file-access-from-files", true),
    )
    allocCtx, cancel := chromedp.NewExecAllocator(ctx, opts...)
    return chromedp.NewContext(allocCtx), cancel
}
```

### Pattern 2: Docker-based Network Simulation

**What:** Use Docker containers with tc/netem for network impairments.

**When:** Testing BWE response to packet loss, bandwidth limits, latency/jitter.

**Why:**
- Isolated network namespace per test
- Reproducible conditions
- Works in CI (GitHub Actions supports Docker)
- No host system modification required

**Example docker-compose.yml:**
```yaml
version: "3.8"
services:
  bwe-server:
    build: .
    networks:
      - impaired

  network-shaper:
    image: alpine
    cap_add:
      - NET_ADMIN
    networks:
      - impaired
    command: >
      sh -c "tc qdisc add dev eth0 root netem delay 100ms loss 5%"

networks:
  impaired:
    driver: bridge
```

### Pattern 3: Pion-to-Pion Integration Tests

**What:** Full PeerConnection tests without browser (Pion as both endpoints).

**When:** Testing interceptor integration, REMB flow, multi-stream scenarios.

**Why:**
- Faster than browser tests
- More control over timing and packets
- Can verify internal state directly
- No browser flakiness

**Example (extends existing integration_test.go pattern):**
```go
func TestPeerConnection_REMBFlow(t *testing.T) {
    // Sender peer (with video track)
    sender, _ := webrtc.NewPeerConnection(config)
    track, _ := webrtc.NewTrackLocalStaticRTP(...)
    sender.AddTrack(track)

    // Receiver peer (with BWE interceptor)
    bweFactory, _ := bweinterceptor.NewBWEInterceptorFactory()
    registry := &interceptor.Registry{}
    registry.Add(bweFactory)
    api := webrtc.NewAPI(webrtc.WithInterceptorRegistry(registry))
    receiver, _ := api.NewPeerConnection(config)

    // Connect peers (SDP exchange)
    offer, _ := sender.CreateOffer(nil)
    sender.SetLocalDescription(offer)
    receiver.SetRemoteDescription(offer)
    answer, _ := receiver.CreateAnswer(nil)
    receiver.SetLocalDescription(answer)
    sender.SetRemoteDescription(answer)

    // Wait for connection + REMB
    time.Sleep(5 * time.Second)

    // Assert REMB sent
    // ...
}
```

## Anti-Patterns to Avoid

### Anti-Pattern 1: Browser Tests for Algorithm Verification

**What:** Using browser tests to verify BWE algorithm correctness.

**Why bad:** Slow, flaky, hard to control conditions precisely.

**Instead:** Use unit tests with MockClock and synthetic traces for algorithm testing. Reserve browser tests only for interop verification (REMB accepted by Chrome).

### Anti-Pattern 2: Host Network Modification

**What:** Using tc/netem directly on the CI host network.

**Why bad:** Affects all network traffic, requires root, hard to clean up on failure.

**Instead:** Use Docker containers with isolated network namespaces.

### Anti-Pattern 3: Hardcoded Timing in E2E Tests

**What:** Using fixed `time.Sleep()` for synchronization.

**Why bad:** Flaky on slow CI machines, wastes time on fast machines.

**Instead:** Use polling with timeout:
```go
require.Eventually(t, func() bool {
    return server.GetREMBCount() > 0
}, 10*time.Second, 100*time.Millisecond)
```

### Anti-Pattern 4: E2E Tests in Main Test Suite

**What:** Running browser/network tests with regular `go test ./...`.

**Why bad:** Slow, requires Chrome, may require Docker, breaks normal development flow.

**Instead:** Use build tags (`//go:build e2e`) and separate CI workflow.

## Suggested Build Order

### Phase 1: Test Infrastructure Foundation

**Goal:** Create E2E directory structure and basic browser automation.

**Tasks:**
1. Create `e2e/` directory with build-tagged test file
2. Add `chromedp` dependency
3. Create `pkg/bwe/testutil/browser.go` with BrowserClient
4. Refactor `cmd/chrome-interop/` into importable server package
5. Create first browser test: `TestChrome_CanConnect`

**Dependencies:** None (extends existing code)

**Validation:** `go test -tags e2e ./e2e/...` passes with Chrome installed

### Phase 2: Chrome REMB Verification (VALID-02 Automation)

**Goal:** Automate the current manual Chrome interop test.

**Tasks:**
1. Extend BrowserClient with `GetREMBStats()` (via JS injection or CDP)
2. Create `e2e/browser_test.go` with REMB verification test
3. Add assertion: REMB visible in Chrome stats
4. Add test: estimate converges to reasonable value

**Dependencies:** Phase 1 complete

**Validation:** `TestChrome_REMBAccepted` passes headlessly

### Phase 3: Network Simulation Infrastructure

**Goal:** Add network condition simulation capability.

**Tasks:**
1. Create `pkg/bwe/testutil/network.go` with condition helpers
2. Create `e2e/docker/` with network simulation containers
3. Create `e2e/network_test.go` with condition tests
4. Test scenarios: stable, lossy, bandwidth-limited, high-latency

**Dependencies:** Docker knowledge, Phase 1 complete

**Validation:** BWE estimate responds appropriately to conditions

### Phase 4: Full PeerConnection Tests

**Goal:** Comprehensive Pion-to-Pion integration tests.

**Tasks:**
1. Create `e2e/peerconnection_test.go`
2. Test scenarios: single stream, multi-stream, stream add/remove
3. Verify REMB SSRCs match active streams
4. Verify estimate stability over time

**Dependencies:** Phase 1 complete (can run parallel with Phase 2-3)

**Validation:** All PeerConnection scenarios pass

### Phase 5: CI Integration

**Goal:** Automated E2E testing in GitHub Actions.

**Tasks:**
1. Create `.github/workflows/e2e.yml`
2. Configure headless Chrome in CI
3. Configure Docker for network simulation
4. Add workflow triggers (PR, nightly)
5. Add status badges to README

**Dependencies:** All previous phases

**Validation:** CI runs E2E tests automatically

## CI Workflow Structure

### Recommended Workflow Design

```yaml
# .github/workflows/e2e.yml
name: E2E Tests

on:
  push:
    branches: [master]
  pull_request:
  schedule:
    - cron: '0 0 * * *'  # Nightly

jobs:
  unit-tests:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - run: go test ./...

  browser-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Install Chrome
        run: |
          sudo apt-get update
          sudo apt-get install -y chromium-browser
      - name: Run browser tests
        run: |
          export CHROME_PATH=/usr/bin/chromium-browser
          go test -tags e2e -v ./e2e/... -run TestChrome

  network-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - name: Run network simulation tests
        run: |
          docker-compose -f e2e/docker/docker-compose.yml up -d
          go test -tags e2e -v ./e2e/... -run TestNetwork
          docker-compose -f e2e/docker/docker-compose.yml down

  peerconnection-tests:
    runs-on: ubuntu-latest
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -tags e2e -v ./e2e/... -run TestPeerConnection

  soak-tests:
    runs-on: ubuntu-latest
    if: github.event_name == 'schedule'  # Nightly only
    needs: unit-tests
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: go test -v ./pkg/bwe/... -run TestSoak24Hour
```

### Workflow Separation Rationale

| Job | Trigger | Duration | Why Separate |
|-----|---------|----------|--------------|
| unit-tests | Always | ~30s | Fast feedback, blocks others |
| browser-tests | Always | ~2min | Needs Chrome, can fail independently |
| network-tests | Always | ~3min | Needs Docker, can fail independently |
| peerconnection-tests | Always | ~1min | Pure Go, reliable |
| soak-tests | Nightly | ~5min | Long running, not blocking |

## Scalability Considerations

| Concern | Current (few tests) | At 20 E2E tests | At 100 E2E tests |
|---------|---------------------|-----------------|------------------|
| Test duration | 1-2 minutes | 5-10 minutes | Parallelize by job |
| Chrome instances | 1 headless | 1 per test file | Pool/reuse |
| Docker containers | On-demand | Cached images | Pre-built images |
| CI cost | Minimal | ~10min/run | Optimize with matrix |

## Sources

### Primary (HIGH confidence)

**Codebase analysis:**
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/interceptor/integration_test.go` - Existing integration test patterns
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/testutil/` - Existing test utilities
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/soak_test.go` - Long-running test patterns
- `/Users/thesyncim/GolandProjects/bwe/cmd/chrome-interop/main.go` - Current manual test server

**Go testing patterns:**
- [testing package - Go Packages](https://pkg.go.dev/testing)
- [Go Unit Testing: Structure & Best Practices](https://www.glukhov.org/post/2025/11/unit-tests-in-go/)
- [Mastering Golang Testing: Integration, Unit, and E2E](https://martinyonathann.medium.com/integration-unit-and-e2e-testing-in-golang-3e957f9920dd)

**Browser automation:**
- [chromedp package - Go Packages](https://pkg.go.dev/github.com/chromedp/chromedp)
- [ChromeDP Tutorial - Rebrowser](https://rebrowser.net/blog/chromedp-tutorial-master-browser-automation-in-go-with-real-world-examples-and-best-practices)
- [Running UI Automation Tests with Go and Chrome on GitHub Actions](https://pradappandiyan.medium.com/running-ui-automation-tests-with-go-and-chrome-on-github-actions-1f56d7c63405)

### Secondary (MEDIUM confidence)

**WebRTC testing:**
- [Testing WebRTC applications - webrtc.org](https://webrtc.org/getting-started/testing)
- [Peer connection interop testing - Pion Discussion](https://github.com/pion/webrtc/discussions/1668)
- [KITE - WebRTC interoperability testing](https://github.com/webrtc/KITE)

**Network simulation:**
- [webrtcperf - WebRTC performance testing](https://github.com/vpalmisano/webrtcperf)
- [Testing WebRTC in constrained networks](https://medium.com/@vpalmisano/testing-webrtc-clients-in-constrained-network-environments-b34fed6d9d1c)
- [slow-network - tc/netem wrapper](https://github.com/j1elo/slow-network)

**CI patterns:**
- [Run headless test with GitHub Actions](https://remarkablemark.org/blog/2020/12/12/headless-test-in-github-actions-workflow/)
- [docker-webrtc-test - Headless browser Docker setup](https://github.com/relekang/docker-webrtc-test)

---
**Research completed:** 2026-01-22
**Ready for roadmap:** Yes
