# Feature Landscape: E2E Testing for BWE

**Domain:** End-to-End Testing for WebRTC Bandwidth Estimation
**Researched:** 2026-01-22
**Confidence:** HIGH (multiple authoritative sources, existing Pion patterns)

---

## Executive Summary

E2E testing for WebRTC bandwidth estimation requires testing across three dimensions: browser interoperability (verifying REMB packets are accepted by real browsers), network simulation (validating behavior under realistic conditions), and integration testing (full Pion PeerConnection chain). The existing BWE test infrastructure provides a solid foundation - unit tests, benchmarks, soak tests, TCP fairness simulation, reference trace harness, and a manual Chrome interop server. E2E testing builds on this to automate browser interaction and add realistic network conditions.

**Key Finding:** The Pion ecosystem provides mature tools for network simulation via `pion/transport/vnet` and an existing BWE test framework at `pion/bwe-test` implementing RFC 8867 test scenarios. Automated browser testing is achievable with headless Chrome via Puppeteer/Playwright, though WebRTC-specific considerations (fake media, webrtc-internals extraction) add complexity.

---

## Existing Test Infrastructure

Before defining E2E features, here is what already exists in the BWE codebase:

| Component | Location | Purpose |
|-----------|----------|---------|
| Unit tests | `*_test.go` files | Component-level validation |
| Benchmark suite | `benchmark_test.go`, `interceptor/benchmark_test.go` | 0 allocs/op validation |
| 24-hour soak test | `soak_test.go` | Timestamp wraparound, memory leaks |
| TCP fairness simulation | `tcp_fairness_test.go` | Three-phase congestion test |
| Reference trace harness | `testutil/reference_trace.go` | libwebrtc divergence comparison |
| Integration tests | `interceptor/integration_test.go` | Factory-Interceptor-Stream chain |
| Manual Chrome interop | `cmd/chrome-interop/main.go` | Web UI for manual REMB verification |

**Gap:** Chrome interop requires manual verification via webrtc-internals. No automated network condition testing exists.

---

## Table Stakes

Features users expect. Missing any of these means E2E testing is incomplete.

### 1. Automated Browser REMB Verification

| Aspect | Detail |
|--------|--------|
| **Why Expected** | Manual Chrome interop server exists but requires human verification. Automated E2E tests must verify REMB packets are accepted by real browsers. |
| **What It Tests** | Chrome/Chromium receives REMB feedback, acknowledges it (no errors), and optionally adapts bitrate |
| **Complexity** | Medium-High |
| **Dependencies** | Headless Chrome, Puppeteer/Playwright or chromedp (Go), fake media streams |

**Acceptance Criteria:**
- [ ] Headless Chrome connects to Pion peer
- [ ] Chrome sends video stream to Pion
- [ ] BWE interceptor generates REMB
- [ ] Test verifies REMB appears in webrtc-internals stats or getStats() API
- [ ] Test passes/fails programmatically (no human needed)

**Test Scenarios:**
1. **Basic REMB acceptance** - Chrome connects, sends video, receives REMB, no errors
2. **REMB value verification** - Extract REMB value from getStats(), verify it's non-zero
3. **Bitrate adaptation** - Apply bandwidth constraint, verify REMB decreases

### 2. Network Condition Simulation

| Aspect | Detail |
|--------|--------|
| **Why Expected** | BWE's core purpose is adapting to network conditions. Must test under realistic impairments. |
| **What It Tests** | Correct estimate behavior under packet loss, bandwidth limits, latency, jitter |
| **Complexity** | Medium |
| **Dependencies** | Pion vnet or tc/netem for real network impairment |

**Acceptance Criteria:**
- [ ] Can simulate packet loss (0-20% range)
- [ ] Can simulate bandwidth caps (500 Kbps to 5 Mbps)
- [ ] Can simulate latency (10ms to 500ms RTT)
- [ ] Can simulate jitter (variable delay)
- [ ] Tests verify correct BWE response to each condition

**Test Scenarios (from RFC 8867):**
1. **Variable Available Capacity** - Bandwidth changes step-wise, verify estimate follows
2. **Congestion with Recovery** - Induce congestion, verify backoff, remove congestion, verify recovery
3. **High Packet Loss** - 10-15% loss, verify estimate remains stable (not oscillating)
4. **High Latency** - 200-500ms RTT, verify estimate converges correctly
5. **Jitter** - Variable delay, verify jitter buffer interaction

### 3. Full Pion PeerConnection Integration

| Aspect | Detail |
|--------|--------|
| **Why Expected** | Unit/integration tests use mock readers/writers. Must verify full WebRTC stack. |
| **What It Tests** | BWE interceptor works correctly within complete PeerConnection lifecycle |
| **Complexity** | Medium |
| **Dependencies** | Pion WebRTC, vnet for isolation |

**Acceptance Criteria:**
- [ ] Create two Pion PeerConnections with BWE interceptor
- [ ] Establish connection via SDP exchange
- [ ] Send video track from one peer to another
- [ ] Verify REMB feedback is generated and received
- [ ] Verify estimate converges to expected value

**Test Scenarios:**
1. **Basic bidirectional** - Both peers send/receive, both generate REMB
2. **Stream add/remove** - Add track mid-call, verify REMB includes new SSRC
3. **Renegotiation** - SDP renegotiation, verify BWE state maintained
4. **Connection recovery** - ICE restart, verify BWE recovers gracefully

### 4. CI Integration

| Aspect | Detail |
|--------|--------|
| **Why Expected** | E2E tests are useless if not run automatically on every commit |
| **What It Tests** | Tests run reliably in CI environment |
| **Complexity** | Low |
| **Dependencies** | GitHub Actions, Docker (for headless Chrome) |

**Acceptance Criteria:**
- [ ] All E2E tests run in GitHub Actions
- [ ] Tests complete within 5 minutes
- [ ] Flaky tests are identified and fixed or quarantined
- [ ] Test results are visible in PR checks

---

## Differentiators

Features that would make testing exceptional. Not expected but valuable.

### 1. Automated Reference Trace Extraction from Chrome

| Aspect | Detail |
|--------|--------|
| **Value Proposition** | Currently VALID-01 uses synthetic reference data. Automated extraction from Chrome RTC event logs would provide real libwebrtc reference values for divergence testing. |
| **Complexity** | High |
| **Benefit** | True libwebrtc behavioral parity validation |

**What It Would Do:**
- Run Chrome in controlled network scenario
- Extract RTC event log (chrome://webrtc-internals download)
- Parse with rtc_event_log_visualizer or custom parser
- Compare BWE estimates against Chrome's estimates
- Quantify divergence percentage

### 2. Performance Regression Detection

| Aspect | Detail |
|--------|--------|
| **Value Proposition** | Catch performance regressions automatically (allocs/op increase, throughput decrease) |
| **Complexity** | Medium |
| **Benefit** | Maintain 0 allocs/op guarantee, prevent latency increases |

**What It Would Do:**
- Run benchmarks in CI
- Compare against baseline (stored in repo or previous commit)
- Fail CI if performance degrades beyond threshold
- Store historical performance data for trend analysis

### 3. Multi-Browser Testing

| Aspect | Detail |
|--------|--------|
| **Value Proposition** | Firefox and Safari may handle REMB differently. Multi-browser testing catches interop issues. |
| **Complexity** | High |
| **Benefit** | True cross-browser compatibility assurance |

**Note:** Firefox and Safari support REMB but have different behaviors. This is a "nice to have" for comprehensive coverage.

### 4. RFC 8867 Full Test Suite

| Aspect | Detail |
|--------|--------|
| **Value Proposition** | Pion's bwe-test implements RFC 8867 congestion control test cases. Full compliance would be authoritative. |
| **Complexity** | High |
| **Benefit** | Standards-compliant BWE validation |

**RFC 8867 Test Cases:**
- Variable Available Capacity (single flow, multiple flows)
- Congested Feedback Link
- Competing Media Flows (same CC algorithm)
- Round Trip Time Fairness
- Media Flow Competing with TCP (long flows, short flows)
- Media Pause and Resume
- Media Flows with Priority
- ECN Usage
- Multiple Bottlenecks

### 5. Live Network Testing

| Aspect | Detail |
|--------|--------|
| **Value Proposition** | vnet simulation may not capture all real-world behaviors. Occasional live network tests provide ground truth. |
| **Complexity** | High (flaky, slow) |
| **Benefit** | Validates simulation accuracy |

**Note:** Live tests should be optional, not blocking CI.

---

## Anti-Features

Features to explicitly NOT build. Common mistakes in this domain.

### 1. Testing Every Browser Version

| Why Avoid | Diminishing returns. Chrome stable + latest is sufficient. Testing Chrome 80-120 is waste of CI time. |
| What to Do Instead | Test latest stable Chrome. Add Firefox only if specific interop bugs emerge. |

### 2. Real tc/netem Network Impairment in CI

| Why Avoid | Requires root/CAP_NET_ADMIN, complex Docker setup, platform-specific. vnet is portable and deterministic. |
| What to Do Instead | Use Pion vnet for CI. tc/netem only for local deep investigation. |

### 3. Visual Video Quality Metrics (VMAF, SSIM)

| Why Avoid | BWE controls bitrate, not video quality. Quality metrics test the encoder, not BWE. |
| What to Do Instead | Test BWE's bitrate estimates and convergence time. Let encoder tests handle quality. |

### 4. Exhaustive Parameter Sweeps

| Why Avoid | Testing every combination of (loss%, bandwidth, latency, jitter) produces thousands of tests. Most add no value. |
| What to Do Instead | Test representative scenarios: stable, congested, recovering, edge cases. |

### 5. End-User UI Testing

| Why Avoid | BWE is a library. Testing button clicks and UI elements is application-level concern. |
| What to Do Instead | Test API behavior via integration tests. |

### 6. Simulating Mobile Networks

| Why Avoid | Mobile network behavior (cell handoff, variable bandwidth) is extremely complex and poorly specified. |
| What to Do Instead | Test generic bandwidth variation. Mobile-specific testing is application concern. |

---

## Feature Dependencies

Dependencies between features and existing infrastructure.

```
                  +-------------------+
                  | Existing:         |
                  | - Unit tests      |
                  | - Benchmarks      |
                  | - Soak tests      |
                  | - TCP fairness    |
                  | - Trace harness   |
                  | - Integration     |
                  | - Manual Chrome   |
                  +--------+----------+
                           |
          +----------------+----------------+
          |                |                |
          v                v                v
   +-----------+    +-----------+    +-----------+
   | Automated |    | Network   |    | Full Pion |
   | Browser   |    | Condition |    | PC Tests  |
   | REMB      |    | Simulation|    |           |
   +-----+-----+    +-----+-----+    +-----+-----+
         |                |                |
         +-------+--------+--------+-------+
                 |                 |
                 v                 v
          +-----------+     +-----------+
          | CI        |     | Perf      |
          | Integration|    | Regression|
          +-----------+     +-----------+
                 |
                 v (Differentiators)
          +-----------+
          | Reference |
          | Trace     |
          | Extraction|
          +-----------+
```

**Dependency Order:**
1. Network simulation (enables controlled testing)
2. Full Pion PC tests (uses network simulation)
3. Automated browser REMB (uses network simulation)
4. CI integration (runs all the above)
5. Reference trace extraction (optional, builds on browser automation)

---

## MVP Recommendation

For E2E testing milestone, prioritize:

### Phase 1: Foundation (Must Have)

1. **Network simulation with vnet** - Enables all other E2E scenarios
2. **Full Pion PeerConnection tests** - Go-only, no browser dependency, validates integration
3. **CI integration** - Run new tests in GitHub Actions

### Phase 2: Browser Interop (Must Have)

4. **Automated Chrome REMB verification** - Replaces manual chrome-interop testing
5. **Network condition scenarios in browser** - Apply vnet conditions to browser tests

### Phase 3: Polish (Nice to Have)

6. **Performance regression detection** - Benchmark comparisons in CI
7. **Reference trace extraction** - Automated libwebrtc comparison

### Defer to Post-E2E:

- Multi-browser testing (Firefox, Safari) - Only if interop bugs emerge
- RFC 8867 full test suite - Large effort, questionable ROI for single-library
- Live network testing - Flaky, use for investigation not CI

---

## Technology Recommendations

### Browser Automation

| Option | Pros | Cons | Recommendation |
|--------|------|------|----------------|
| **chromedp (Go)** | Native Go, no external deps, good for Go projects | Less mature WebRTC support | Consider for simple tests |
| **Puppeteer (Node.js)** | Mature, Chrome-native, excellent WebRTC docs | Requires Node.js | Good choice |
| **Playwright (Node.js)** | Cross-browser, modern API, WebRTC support | Requires Node.js | **Recommended** |
| **Selenium** | Cross-browser, mature | Heavy, slower, outdated API | Avoid |

**Recommendation:** Playwright for browser automation. Consider chromedp if staying pure Go is critical.

### Network Simulation

| Option | Pros | Cons | Recommendation |
|--------|------|------|----------------|
| **Pion vnet** | Pure Go, portable, deterministic, already in ecosystem | Simulation not real impairment | **Recommended for CI** |
| **tc/netem** | Real kernel-level impairment | Linux-only, requires root | Local dev only |
| **Docker network** | Easy rate limiting | Coarse control, no jitter | Avoid |
| **Comcast tool** | Easy to use | Linux/macOS only | Alternative to tc/netem |

**Recommendation:** Pion vnet for CI tests. tc/netem optional for local investigation.

### Test Framework

| Option | Pros | Cons | Recommendation |
|--------|------|------|----------------|
| **Go testing** | Native, no deps, existing pattern | Limited for E2E orchestration | Use for Go-only tests |
| **Testify** | Already in project | Assertion library only | Continue using |
| **Ginkgo/Gomega** | BDD style, good for E2E | Extra dependency | Consider for E2E suite |

**Recommendation:** Continue with Go testing + Testify. Add Ginkgo only if test orchestration becomes complex.

---

## Sources

### WebRTC E2E Testing
- [WebRTC.org Testing Documentation](https://webrtc.org/getting-started/testing) - Official WebRTC testing guidance
- [KITE Test Engine](https://github.com/webrtc/KITE) - WebRTC interoperability testing framework
- [testRTC/Cyara](https://testrtc.com/) - Commercial WebRTC testing platform

### Pion Testing Infrastructure
- [Pion vnet_test.go](https://github.com/pion/webrtc/blob/master/vnet_test.go) - Virtual network testing patterns
- [Pion bwe-test](https://pkg.go.dev/github.com/pion/bwe-test) - RFC 8867 BWE test implementations
- [Pion WebRTC Issue #712](https://github.com/pion/webrtc/issues/712) - vnet enhancement umbrella ticket

### GCC Test Methodology
- [RFC 8867](https://www.rfc-editor.org/rfc/rfc8867.html) - Test Cases for Evaluating RMCAT Proposals
- [C3Lab WebRTC Testbed](https://c3lab.poliba.it/index.php?title=WebRTC_Testbed) - Academic BWE testing methodology
- [GCC Analysis Paper](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf) - Test scenario documentation

### Browser Automation
- [Playwright Documentation](https://playwright.dev/) - Modern browser automation framework
- [chromedp Package](https://github.com/chromedp/chromedp) - Go Chrome DevTools Protocol client
- [WebRTC Testing with Selenium](https://antmedia.io/webrtc-testing-with-selenium/) - Browser automation for WebRTC

### Existing Project Infrastructure
- Local: `/Users/thesyncim/GolandProjects/bwe/cmd/chrome-interop/main.go` - Manual Chrome interop server
- Local: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/tcp_fairness_test.go` - TCP fairness simulation
- Local: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/soak_test.go` - 24-hour soak test
- Local: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/testutil/reference_trace.go` - Trace replay harness
