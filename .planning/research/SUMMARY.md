# Project Research Summary

**Project:** GCC Receiver-Side Bandwidth Estimator - v1.2 E2E Testing
**Domain:** WebRTC testing infrastructure, browser automation, network simulation
**Researched:** 2026-01-22
**Confidence:** HIGH

## Executive Summary

The BWE library has a solid testing foundation with unit tests, benchmarks, integration tests, and manual Chrome verification. The v1.2 E2E testing milestone aims to **automate browser interoperability testing** and **add realistic network condition simulation**. Research across four dimensions (stack, features, architecture, pitfalls) reveals a clear path forward using pure Go tools.

**Recommended approach:** Use **Rod** for Chrome automation (simpler API, better performance than chromedp, zero-allocation design), **Toxiproxy** for network simulation (battle-tested TCP proxy with Go client), and GitHub Actions with Docker service containers for CI. Create a dedicated `e2e/` directory with build tags to isolate E2E tests from the existing fast unit test suite. Extend the existing `cmd/chrome-interop/` server from manual demo to headless test target.

**Key risks and mitigation:** The dominant failure modes are (1) Chrome/ChromeDriver version mismatches causing CI failures - **mitigate by pinning versions explicitly**, (2) ICE connection timeouts in CI environments - **mitigate with increased timeouts and local-only ICE**, (3) non-deterministic network simulation causing flaky tests - **mitigate by using Pion vnet for deterministic tests**, and (4) runaway browser processes consuming resources - **mitigate with strict cleanup using defer blocks and TestMain teardown**.

## Key Findings

### Recommended Stack

The stack research identified Rod as the superior choice for browser automation over chromedp (decode-on-demand vs every-message JSON decoding, no deadlock potential, lightweight memory footprint). Toxiproxy provides programmatic network impairment via TCP proxy, sufficient for testing signaling path delays while accepting that UDP/RTP simulation requires Pion's internal utilities.

**Core technologies:**
- **Rod (v0.116.2)**: Chrome automation via DevTools Protocol — simpler API than chromedp, auto-downloads Chrome, built-in `HijackRequests` for WebRTC signaling inspection, thread-safe for concurrent tests
- **Toxiproxy (v2.12.0)**: TCP proxy with network condition simulation — provides latency, bandwidth, jitter, timeout toxics for BWE testing, battle-tested by Shopify, pure Go client
- **chromedp/headless-shell (Docker)**: Headless Chrome for CI — official Docker image with necessary WebRTC flags, lightweight, works in GitHub Actions service containers

**Critical flags for WebRTC testing:**
- `--use-fake-device-for-media-stream`: Synthetic video/audio (no real camera/mic)
- `--use-fake-ui-for-media-stream`: Auto-grant media permissions
- `--no-sandbox`: Required in Docker/CI environments
- `--headless=new`: Modern headless mode (Chrome 112+)

**No dependency conflicts:** Rod and Toxiproxy have zero overlap with existing Pion dependencies. Both MIT licensed. Total additions: 2 Go packages, 2 Docker service containers.

### Expected Features

E2E testing fills the automation gap between existing unit/integration tests and manual Chrome verification. The feature research identified four table stakes requirements and several valuable differentiators.

**Must have (table stakes):**
- **Automated browser REMB verification**: Headless Chrome connects, receives REMB feedback, test programmatically verifies acceptance (no human inspection of webrtc-internals)
- **Network condition simulation**: Test BWE behavior under packet loss, bandwidth caps, latency, jitter with reproducible results
- **Full Pion PeerConnection integration**: Two Pion peers with BWE interceptor, verify complete WebRTC stack (not just interceptor-level mocks)
- **CI integration**: All E2E tests run automatically in GitHub Actions on every PR

**Should have (competitive):**
- **Performance regression detection**: Benchmark comparisons in CI to maintain 0 allocs/op guarantee
- **Reference trace extraction from Chrome**: Parse RTC event logs to compare against libwebrtc behavioral baselines (enhances existing VALID-01 synthetic trace testing)

**Defer (v2+):**
- **Multi-browser testing**: Firefox/Safari have different REMB handling, but Chrome coverage is sufficient for v1.2
- **RFC 8867 full compliance**: Complete test suite (9 scenarios) is high effort, defer to post-E2E
- **Live network testing**: Real internet tests are flaky and slow, vnet simulation provides sufficient validation

**Anti-features (explicitly avoid):**
- Testing every Chrome version (test latest stable only)
- Visual quality metrics like VMAF/SSIM (BWE tests bitrate, not encoder quality)
- Exhaustive parameter sweeps (thousands of combinations add no value)
- Real tc/netem in CI (requires root, not portable, use vnet instead)

### Architecture Approach

The architecture research analyzed the existing test infrastructure and defined clear integration points. The BWE codebase already has excellent test patterns: colocated unit tests, `testutil/` package for reusable helpers, integration tests in `interceptor/`, build tags for long-running tests (soak tests), and a manual Chrome interop server.

**Major components:**
1. **`e2e/` directory** (new): Isolated E2E test suite with build tags (`//go:build e2e`), keeps E2E tests out of normal `go test ./...` runs, contains browser, network, and PeerConnection test files
2. **`pkg/bwe/testutil/browser.go`** (new): Reusable browser automation primitives wrapping Rod, provides `BrowserClient` with `StartCall()`, `GetREMBStats()`, `Close()` methods
3. **`pkg/bwe/testutil/network.go`** (new): Network simulation helpers wrapping Toxiproxy client, configures latency/bandwidth/jitter toxics
4. **`cmd/chrome-interop/` evolution**: Refactor from embedded HTML demo to importable server package, add programmatic control API for tests, maintain backward compatibility for manual use
5. **`.github/workflows/e2e.yml`** (new): Separate CI workflow with Docker service containers (Chrome, Toxiproxy), runs after unit tests pass

**Key patterns to follow:**
- **Rod for browser tests**: Pure Go, no WebDriver binary, direct CDP access, works headlessly in CI
- **Pion-to-Pion for integration**: Faster than browser tests, more control, verify internal state directly
- **Docker network namespaces**: Isolated simulation per test, reproducible, no host modification
- **Polling with timeout**: Use `require.Eventually()` not hardcoded `time.Sleep()` for synchronization
- **Build tags for isolation**: E2E tests separate from unit tests, different execution models

**Data flow (browser test):**
1. `e2e/browser_test.go` starts `cmd/chrome-interop` server
2. Rod launches headless Chrome with WebRTC flags
3. Chrome navigates to test server, initiates WebRTC connection
4. BWE interceptor receives RTP, generates REMB feedback
5. Rod queries Chrome stats (via JS injection or CDP) to verify REMB receipt
6. Test asserts: REMB count > 0, estimate converged to expected range

### Critical Pitfalls

The pitfall research identified failure modes across browser automation, network simulation, and CI integration. Four critical issues can block E2E testing entirely if not addressed early.

1. **Chrome/ChromeDriver version mismatch**: Tests fail with `SessionNotCreatedError` when GitHub Actions auto-updates Chrome but ChromeDriver is pinned. **Prevention:** Pin Chrome version explicitly in CI workflow (`browser-actions/setup-chrome@v1`), check compatibility in `TestMain()`, document version requirements in README. **Address in Phase 1.**

2. **ICE connection timeouts in CI**: WebRTC connections fail to establish due to slow runners and network restrictions, tests timeout or hang. **Prevention:** Increase ICE timeouts via `SettingEngine` (30s disconnected, 60s failed), use local-only ICE candidates for unit tests (skip STUN/TURN), detect CI environment and adjust timeouts dynamically. **Address in Phase 1.**

3. **Runaway browser processes**: Test failures leave Chrome instances running, consuming all memory, causing CI runner crashes. **Prevention:** Always use `defer cancel()` for cleanup, set context timeouts on browser lifetime, implement `TestMain` cleanup for orphaned processes, limit parallel browser tests with semaphore. **Address in Phase 1.**

4. **Non-deterministic network simulation**: Random timing in packet loss/delay produces different results each run, making tests flaky. **Prevention:** Use Pion vnet for deterministic simulation (not tc/netem), separate deterministic from probabilistic tests, use seeded random for reproducibility, statistical assertions (ranges) not exact values. **Address in Phase 2.**

**Additional high-severity pitfalls:**
- Missing Chrome WebRTC flags (test pattern requires permissions, fails in headless)
- ICE candidate race conditions (trickle ICE timing issues)
- REMB verification without Chrome internals access (use behavioral tests, not direct RTCP inspection)
- GitHub Actions resource limits (2 vCPU, 7GB RAM insufficient for parallel browser tests)

## Implications for Roadmap

Based on research, the E2E testing milestone should proceed in four phases, following dependency order and risk mitigation priorities. The existing test infrastructure provides a solid foundation, allowing incremental addition of E2E capabilities.

### Phase 1: Test Infrastructure Foundation
**Rationale:** Establish browser automation and CI configuration before writing tests. Addressing critical pitfalls (Chrome version mismatch, browser cleanup, ICE timeouts) early prevents blocking issues later.

**Delivers:**
- `e2e/` directory structure with build-tagged test scaffolding
- `pkg/bwe/testutil/browser.go` with Rod-based `BrowserClient`
- Refactored `cmd/chrome-interop/` as importable server package
- GitHub Actions workflow skeleton with Chrome service container
- First smoke test: `TestChrome_CanConnect` passes headlessly

**Addresses features:**
- Foundation for automated browser REMB verification
- Foundation for CI integration

**Avoids pitfalls:**
- Chrome/ChromeDriver version mismatch (pin in CI early)
- Runaway browser processes (cleanup patterns established)
- ICE connection timeouts (configure timeouts from start)

**Research flag:** Standard patterns, well-documented in Rod/chromedp examples. **Skip phase-specific research.**

### Phase 2: Network Simulation Infrastructure
**Rationale:** Network simulation enables controlled BWE testing before adding browser complexity. Toxiproxy provides TCP proxy for signaling path impairment, sufficient for initial E2E scenarios.

**Delivers:**
- `pkg/bwe/testutil/network.go` with Toxiproxy client helpers
- Toxiproxy Docker service container in CI
- Test scenarios: stable network, latency (100ms+20ms jitter), bandwidth cap (500 KB/s)
- Deterministic simulation using seeded randomness

**Addresses features:**
- Network condition simulation (table stakes)
- Foundation for performance regression detection

**Avoids pitfalls:**
- Non-deterministic network simulation (Toxiproxy + deterministic test design)
- tc/netem unavailable in CI (use Toxiproxy TCP proxy, skip UDP simulation)

**Uses stack:** Toxiproxy v2.12.0 client/server

**Research flag:** Moderate complexity, Toxiproxy usage well-documented but BWE-specific toxic configuration may need experimentation. **Consider light research on toxic tuning.**

### Phase 3: Automated Chrome REMB Verification
**Rationale:** This is the primary automation goal (replaces manual Chrome interop). With infrastructure (Phase 1) and network simulation (Phase 2) in place, browser testing can focus on verification logic.

**Delivers:**
- `e2e/browser_test.go` with REMB acceptance tests
- `BrowserClient.GetREMBStats()` implementation (JS injection or CDP)
- Test scenarios: basic REMB acceptance, REMB value verification, bitrate adaptation under congestion
- Behavioral verification (BWE estimate adapts) since direct RTCP inspection unavailable

**Addresses features:**
- Automated browser REMB verification (table stakes)
- Network condition scenarios applied to browser tests

**Avoids pitfalls:**
- REMB verification without Chrome internals (use server-side logging + behavioral assertions)
- BWE convergence time assumptions (wait for convergence with polling, not fixed sleep)
- Missing Chrome WebRTC flags (reuse Phase 1 flag configuration)

**Uses stack:** Rod v0.116.2, chromedp/headless-shell Docker image

**Implements architecture:** Browser test data flow from ARCHITECTURE.md

**Research flag:** Chrome stats extraction is non-trivial (webrtc-internals not accessible via CDP). **Consider focused research on getStats() API and REMB visibility.**

### Phase 4: Full Pion PeerConnection Integration
**Rationale:** Complete the E2E test matrix with Pion-to-Pion tests. These are faster and more reliable than browser tests, suitable for comprehensive scenario coverage (multi-stream, renegotiation, ICE restart).

**Delivers:**
- `e2e/peerconnection_test.go` with full PeerConnection lifecycle tests
- Test scenarios: basic bidirectional, stream add/remove, SDP renegotiation, connection recovery
- REMB SSRC verification (matches active streams)
- Estimate stability over time (convergence validation)

**Addresses features:**
- Full Pion PeerConnection integration (table stakes)
- Foundation for RFC 8867 test suite (deferred to v2+)

**Avoids pitfalls:**
- ICE candidate race conditions (use complete gathering before SDP exchange)
- BWE convergence time (condition-based waits, statistical assertions)

**Uses stack:** Existing Pion WebRTC v4.2.3, BWE interceptor

**Research flag:** Standard Pion patterns, existing `interceptor/integration_test.go` provides template. **Skip phase-specific research.**

### Phase 5: CI Integration and Polish
**Rationale:** With all test types implemented, finalize CI configuration for automated execution. Add performance regression detection and establish flakiness policy.

**Delivers:**
- Complete `.github/workflows/e2e.yml` with matrix strategy (browser, network, integration)
- Benchmark comparison in CI (detect allocs/op regressions)
- Flaky test tracking and quarantine policy
- Test duration optimization (parallelization limits, caching)

**Addresses features:**
- CI integration (table stakes, completion)
- Performance regression detection (differentiator)

**Avoids pitfalls:**
- GitHub Actions resource limits (limit parallelism to 1, use job matrix for isolation)
- Flaky test retry masking (track frequency, limit retries to 2, require investigation)

**Research flag:** GitHub Actions patterns well-documented. **Skip phase-specific research.**

### Phase Ordering Rationale

- **Phase 1 first**: Infrastructure and tooling are foundational. Without browser automation setup and cleanup patterns, all subsequent phases will encounter blocking issues. Chrome version pinning and ICE timeout configuration prevent CI flakiness.

- **Phase 2 before Phase 3**: Network simulation is simpler (no browser) and enables controlled testing. Validating Toxiproxy configuration without browser complexity reduces debugging surface. Deterministic simulation patterns established here apply to browser tests.

- **Phase 3 and 4 parallel potential**: After Phases 1-2, browser tests (Phase 3) and Pion integration tests (Phase 4) are independent. However, sequential execution recommended to validate browser testing patterns before Pion complexity.

- **Phase 5 last**: CI optimization requires complete test suite to measure duration and flakiness. Performance regression detection needs baseline established from Phases 1-4.

### Research Flags

**Phases needing deeper research during planning:**
- **Phase 2 (Network Simulation)**: Toxiproxy toxic configuration for BWE-specific scenarios (latency/jitter/bandwidth combinations) may require experimentation. Research how to map RFC 8867 network conditions to Toxiproxy parameters.
- **Phase 3 (Chrome REMB Verification)**: Chrome stats API (getStats()) REMB visibility is unclear. Research whether REMB feedback appears in receiver stats, sender stats, or requires custom RTCP interception.

**Phases with standard patterns (skip research-phase):**
- **Phase 1 (Infrastructure)**: Rod documentation comprehensive, chromedp patterns well-established, GitHub Actions Chrome setup widely documented
- **Phase 4 (Pion Integration)**: Existing `interceptor/integration_test.go` provides complete template, Pion vnet usage documented
- **Phase 5 (CI Polish)**: GitHub Actions optimization is standard DevOps practice

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | **HIGH** | Rod and Toxiproxy selections backed by official docs, pkg.go.dev version verification, comparative analysis. Chrome flags from webrtc.org official testing guide. |
| Features | **HIGH** | Feature landscape grounded in existing codebase analysis (manual Chrome interop, unit/integration tests). Table stakes vs differentiators clearly delineated based on WebRTC testing best practices. |
| Architecture | **HIGH** | Architecture built on existing test patterns (testutil package, build tags, integration test structure). Component boundaries follow Go project conventions. Pion vnet usage documented in pion/webrtc repository. |
| Pitfalls | **HIGH** | Pitfalls sourced from Pion GitHub issues, Mux production experience blog post, WebRTC community patterns. Critical vs high vs medium severity validated against CI failure frequency in similar projects. |

**Overall confidence:** **HIGH**

The research synthesizes authoritative sources (official documentation, pkg.go.dev, webrtc.org, Pion repository), production experience reports (Mux headless Chrome service, Daily.co WebRTC testing), and local codebase analysis. All four research dimensions converge on a consistent recommendation: pure Go tools (Rod, Toxiproxy), isolated E2E directory with build tags, extension of existing test infrastructure patterns.

### Gaps to Address

**Gap 1: Chrome getStats() REMB visibility**
- **Issue:** Unclear whether `RTCStatsReport` exposes received REMB packets. Documentation inconsistent across Chrome versions.
- **Handling:** Phase 3 planning should include spike to verify REMB appears in receiver stats. Fallback: use server-side REMB logging + behavioral verification (encoder bitrate adaptation).

**Gap 2: Toxiproxy UDP limitation**
- **Issue:** Toxiproxy only proxies TCP. WebRTC media (RTP/RTCP) uses UDP. Cannot directly simulate UDP packet loss.
- **Handling:** Accept limitation. Use Toxiproxy for signaling path (HTTP/WebSocket) impairment. For UDP simulation in advanced scenarios, use Pion vnet (Phase 4) or defer to manual tc/netem investigation.

**Gap 3: Performance regression threshold tuning**
- **Issue:** What constitutes a "regression"? 1% allocs increase? 5% throughput decrease?
- **Handling:** Defer to Phase 5. Establish baseline from Phases 1-4, then define thresholds empirically based on observed variance.

**Gap 4: Flaky test quarantine policy**
- **Issue:** When to quarantine vs fix? How many retries before declaring test unstable?
- **Handling:** Defer to Phase 5. Implement basic flakiness tracking (log failures), establish policy after observing actual flake frequency.

## Sources

### Primary (HIGH confidence)

**Stack research:**
- [Rod GitHub](https://github.com/go-rod/rod) - Official repository, comprehensive examples
- [Rod pkg.go.dev](https://pkg.go.dev/github.com/go-rod/rod) - v0.116.2 API documentation
- [Toxiproxy GitHub](https://github.com/Shopify/toxiproxy) - Official repository, toxic documentation
- [Toxiproxy Go client](https://pkg.go.dev/github.com/Shopify/toxiproxy/v2/client) - v2.12.0 client API
- [WebRTC.org Testing](https://webrtc.github.io/webrtc-org/testing/) - Official WebRTC testing guidance
- [chromedp/headless-shell](https://github.com/chromedp/docker-headless-shell) - Official Docker image

**Feature research:**
- [WebRTC.org Testing Documentation](https://webrtc.org/getting-started/testing) - Official guidance
- [Pion vnet_test.go](https://github.com/pion/webrtc/blob/master/vnet_test.go) - Virtual network patterns
- [Pion bwe-test](https://pkg.go.dev/github.com/pion/bwe-test) - RFC 8867 implementations
- [RFC 8867](https://www.rfc-editor.org/rfc/rfc8867.html) - RMCAT test case specification

**Architecture research:**
- Local codebase: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/interceptor/integration_test.go`
- Local codebase: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/testutil/reference_trace.go`
- Local codebase: `/Users/thesyncim/GolandProjects/bwe/cmd/chrome-interop/main.go`
- [chromedp pkg.go.dev](https://pkg.go.dev/github.com/chromedp/chromedp) - Browser automation patterns

**Pitfall research:**
- [Mux: Lessons learned building headless chrome as a service](https://www.mux.com/blog/lessons-learned-building-headless-chrome-as-a-service) - Production pitfalls
- [Daily.co: Headless WebRTC Testing](https://www.daily.co/blog/how-to-make-a-headless-robot-to-test-webrtc-in-your-daily-app/) - WebRTC-specific testing challenges
- [Pion WebRTC issue #460](https://github.com/pion/webrtc/issues/460) - Slow connection times, timeout configuration
- [Pion WebRTC issue #2578](https://github.com/pion/webrtc/issues/2578) - ICE state race conditions
- [GitHub Actions: Usage limits](https://docs.github.com/en/actions/learn-github-actions/usage-limits-billing-and-administration) - CI resource constraints

### Secondary (MEDIUM confidence)

- [Rod vs chromedp](https://github.com/go-rod/go-rod.github.io/blob/main/why-rod.md) - Comparative analysis
- [Running UI Automation Tests with Go and Chrome on GitHub Actions](https://pradappandiyan.medium.com/running-ui-automation-tests-with-go-and-chrome-on-github-actions-1f56d7c63405) - CI patterns
- [webrtchacks: Probing WebRTC Bandwidth Probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/) - BWE behavior analysis
- [ZenRows: Chromedp Golang Tutorial](https://www.zenrows.com/blog/chromedp) - Browser automation patterns

### Tertiary (LOW confidence)

- [httptoolkit: Intercepting WebRTC traffic](https://httptoolkit.com/blog/intercepting-webrtc-traffic/) - RTCP inspection techniques
- [WebRTC.ventures: Simulating unstable networks](https://webrtc.ventures/2024/06/how-do-you-simulate-unstable-networks-for-testing-live-event-streaming-applications/) - Network simulation approaches

---
*Research completed: 2026-01-22*
*Ready for roadmap: yes*
