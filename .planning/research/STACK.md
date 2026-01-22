# Stack Research: E2E Testing

**Project:** GCC Receiver-Side BWE
**Research Focus:** Technology stack for automated E2E testing
**Researched:** 2026-01-22
**Overall Confidence:** HIGH

## Executive Summary

This research identifies the technology stack needed for automated E2E testing of the BWE library. The recommended stack prioritizes **pure Go solutions** for maintainability, **Rod over chromedp** for browser automation, and **Toxiproxy** for network simulation. GitHub Actions with Docker-based Chrome provides CI integration.

**Key Finding:** The existing codebase has a manual Chrome interop test (`cmd/chrome-interop`). E2E testing automates this with programmatic browser control and network condition simulation.

**Recommendation:** Use Rod for browser automation (simpler API, better performance), Toxiproxy for network simulation (battle-tested, Go client), and chromedp/headless-shell Docker image for CI.

---

## Recommended Stack

### Browser Automation: Rod

| Attribute | Value |
|-----------|-------|
| **Package** | `github.com/go-rod/rod` |
| **Version** | v0.116.2+ |
| **License** | MIT |
| **Purpose** | Chrome automation via DevTools Protocol |

**Why Rod over chromedp:**

| Criterion | Rod | chromedp | Winner |
|-----------|-----|----------|--------|
| JSON decoding | Decode-on-demand | Every message | **Rod** |
| Concurrency | goob-based, no deadlocks | Fixed-size buffer, can deadlock | **Rod** |
| Memory | Lightweight | 1MB overhead per CDP client | **Rod** |
| API style | Chainable, simple | DSL-like tasks, verbose | **Rod** |
| Browser management | Auto-downloads Chrome | Relies on system browser | **Rod** |
| Network inspection | `HijackRequests` built-in | Manual CDP calls | **Rod** |

**WebRTC-specific advantages:**
- `HijackRequests` simplifies debugging signaling
- `WaitStable()` handles async WebRTC connection establishment
- Thread-safe operations for concurrent test execution
- No zombie processes after test crashes (important for CI)

**Installation:**
```bash
go get github.com/go-rod/rod@v0.116.2
```

**Sources:**
- [Rod GitHub](https://github.com/go-rod/rod) - HIGH confidence
- [Rod vs chromedp](https://github.com/go-rod/go-rod.github.io/blob/main/why-rod.md) - HIGH confidence
- [Rod pkg.go.dev](https://pkg.go.dev/github.com/go-rod/rod) - HIGH confidence

---

### Network Simulation: Toxiproxy

| Attribute | Value |
|-----------|-------|
| **Server** | `github.com/Shopify/toxiproxy` |
| **Client** | `github.com/Shopify/toxiproxy/v2/client` |
| **Version** | v2.12.0 |
| **License** | MIT |
| **Purpose** | TCP proxy with network condition simulation |

**Available toxics for BWE testing:**

| Toxic | BWE Test Use Case | Parameters |
|-------|-------------------|------------|
| `latency` | Test delay estimation accuracy | `latency`, `jitter` (ms) |
| `bandwidth` | Test AIMD convergence under bandwidth caps | `rate` (KB/s) |
| `slicer` | Simulate jitter (packet fragmentation) | `average_size`, `size_variation`, `delay` |
| `timeout` | Test stall detection | `timeout` (ms) |
| `reset_peer` | Test recovery from connection loss | `timeout` (ms) |
| `limit_data` | Test behavior at data limits | `bytes` |

**Why Toxiproxy over alternatives:**

| Alternative | Why Not |
|-------------|---------|
| tc/netem (Linux) | OS-specific, requires root, not portable to macOS/CI |
| ooni/netem | No releases, Gvisor complexity, race detector issues on macOS |
| comcast (CLI) | Less programmatic control, wrapper around tc |
| Custom UDP proxy | Complexity; Toxiproxy handles TCP signaling |

**Important limitation:** Toxiproxy is TCP-only. It cannot simulate UDP/RTP packet loss directly. However, for E2E testing:
- WebRTC signaling (HTTP/WebSocket) goes through TCP - Toxiproxy works
- RTP/RTCP uses UDP - simulate via latency/jitter on signaling, or use Pion's internal test utilities for UDP simulation

**Installation:**
```bash
# Go client
go get github.com/Shopify/toxiproxy/v2@v2.12.0

# Server (local development)
brew install toxiproxy  # macOS
# or Docker
docker run -p 8474:8474 ghcr.io/shopify/toxiproxy:2.12.0
```

**Sources:**
- [Toxiproxy GitHub](https://github.com/Shopify/toxiproxy) - HIGH confidence
- [Toxiproxy Go client](https://pkg.go.dev/github.com/Shopify/toxiproxy/v2/client) - HIGH confidence

---

### CI: GitHub Actions with Docker

| Component | Image/Runner | Purpose |
|-----------|--------------|---------|
| **Chrome** | `chromedp/headless-shell:latest` | Headless Chrome for E2E tests |
| **Toxiproxy** | `ghcr.io/shopify/toxiproxy:2.12.0` | Network simulation |
| **Runner** | `ubuntu-latest` | GitHub Actions runner |

**GitHub Actions Service Containers:**

Service containers run alongside test jobs, providing dependencies (Chrome, Toxiproxy) without manual setup.

**Chrome WebRTC flags:**

| Flag | Purpose | Required |
|------|---------|----------|
| `--use-fake-device-for-media-stream` | Synthetic video/audio | Yes |
| `--use-fake-ui-for-media-stream` | Auto-grant media permissions | Yes |
| `--no-sandbox` | Required in Docker | Yes (CI) |
| `--disable-gpu` | Stability in headless mode | Recommended |
| `--headless=new` | Modern headless mode | Yes |

**Optional flags for deterministic testing:**

| Flag | Purpose |
|------|---------|
| `--use-file-for-fake-video-capture=path.y4m` | Use specific video file |
| `--use-file-for-fake-audio-capture=path.wav` | Use specific audio file |

**Sources:**
- [WebRTC.org testing](https://webrtc.github.io/webrtc-org/testing/) - HIGH confidence
- [chromedp/headless-shell](https://github.com/chromedp/docker-headless-shell) - HIGH confidence
- [GitHub Actions Chrome testing](https://pradappandiyan.medium.com/running-ui-automation-tests-with-go-and-chrome-on-github-actions-1f56d7c63405) - MEDIUM confidence

---

## Integration with Existing Codebase

### Current go.mod

```go
go 1.25

require (
    github.com/pion/interceptor v0.1.43
    github.com/pion/rtcp v1.2.16
    github.com/pion/rtp v1.10.0
    github.com/pion/webrtc/v4 v4.2.3
    github.com/stretchr/testify v1.11.1
)
```

### Additions for E2E Testing

```go
require (
    // Browser automation
    github.com/go-rod/rod v0.116.2

    // Network simulation
    github.com/Shopify/toxiproxy/v2 v2.12.0
)
```

### No Conflicts Expected

- Rod: Zero overlapping dependencies with Pion
- Toxiproxy client: Pure Go with minimal dependencies
- Both MIT licensed (compatible with project)

---

## Alternatives Considered

### Browser Automation

| Considered | Verdict | Reason |
|------------|---------|--------|
| **chromedp** | Rejected | Fixed-size buffer deadlocks, heavier memory, no auto browser management |
| **playwright-go** | Rejected | Requires ~50MB Node.js runtime; overkill for single-browser testing |
| **Selenium** | Rejected | Heavyweight, external WebDriver binary, slower startup |

**playwright-go detail:**

playwright-go (v0.5200.1) is powerful but:
- Ships Node.js runtime + Playwright (~50MB binary)
- Architecture: Go -> stdio -> Node.js -> CDP -> Browser
- Overhead not justified for single-browser Chrome testing
- Shines for cross-browser (Chromium, Firefox, WebKit) which we don't need

### Network Simulation

| Considered | Verdict | Reason |
|------------|---------|--------|
| **tc/netem** | Rejected | Linux-only, requires root, not portable to macOS/GitHub Actions |
| **ooni/netem** | Rejected | No official releases, Gvisor complexity, race detector issues on macOS |
| **Custom UDP proxy** | Rejected | High effort; use Pion internal utilities for UDP simulation |

**ooni/netem detail:**

Interesting (userspace TCP/IP via Gvisor) but:
- No official releases (commit-based versioning)
- Requires pinning specific Gvisor commit manually
- Race detector "very slow under macOS and many tests will fail"
- Requires Go 1.20 (project uses Go 1.25)
- Complex architecture for TCP proxy needs

---

## What NOT to Add

| Technology | Reason |
|------------|--------|
| Selenium/WebDriver | Heavyweight, slower, external binary dependency |
| playwright-go | Node.js runtime overhead for single-browser scenario |
| tc/netem directly | Not portable (Linux-only, requires root) |
| BrowserStack/Sauce Labs | External dependency, cost, overkill for library testing |
| Puppeteer via WASM | Experimental, unneeded complexity |
| Custom network emulator | Toxiproxy is battle-tested and sufficient |

---

## Sample Code Patterns

### Rod with WebRTC Flags

```go
import (
    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/launcher"
)

func setupBrowser(t *testing.T) *rod.Browser {
    t.Helper()

    l := launcher.New().
        Headless(true).
        Set("use-fake-device-for-media-stream").
        Set("use-fake-ui-for-media-stream").
        Set("no-sandbox").
        Set("disable-gpu")

    browser := rod.New().ControlURL(l.MustLaunch()).MustConnect()
    t.Cleanup(func() { browser.MustClose() })

    return browser
}

func TestBWEWithChrome(t *testing.T) {
    browser := setupBrowser(t)

    page := browser.MustPage("http://localhost:8080")
    page.MustElement("#startBtn").MustClick()
    page.MustWaitStable()

    // Assert connection established
    status := page.MustElement("#status").MustText()
    assert.Contains(t, status, "Connected")
}
```

### Toxiproxy for Network Conditions

```go
import (
    toxiproxy "github.com/Shopify/toxiproxy/v2/client"
    "testing"
)

func TestBWEUnderLatency(t *testing.T) {
    client := toxiproxy.NewClient("localhost:8474")

    // Create proxy for test server
    proxy, err := client.CreateProxy("webrtc", "localhost:8081", "localhost:8080")
    require.NoError(t, err)
    t.Cleanup(func() { proxy.Delete() })

    // Add 100ms latency with 20ms jitter
    _, err = proxy.AddToxic("latency", "latency", "downstream", 1.0, toxiproxy.Attributes{
        "latency": 100,
        "jitter":  20,
    })
    require.NoError(t, err)

    // Run E2E test through proxy (connect to :8081 instead of :8080)
    runBWETest(t, "http://localhost:8081")
}

func TestBWEUnderBandwidthLimit(t *testing.T) {
    client := toxiproxy.NewClient("localhost:8474")

    proxy, err := client.CreateProxy("webrtc-bw", "localhost:8082", "localhost:8080")
    require.NoError(t, err)
    t.Cleanup(func() { proxy.Delete() })

    // Limit to 500 KB/s
    _, err = proxy.AddToxic("bandwidth", "bandwidth", "downstream", 1.0, toxiproxy.Attributes{
        "rate": 500,
    })
    require.NoError(t, err)

    runBWETest(t, "http://localhost:8082")
}
```

### GitHub Actions Workflow

```yaml
name: E2E Tests

on: [push, pull_request]

jobs:
  e2e:
    runs-on: ubuntu-latest

    services:
      toxiproxy:
        image: ghcr.io/shopify/toxiproxy:2.12.0
        ports:
          - 8474:8474
          - 8080-8090:8080-8090

      chrome:
        image: chromedp/headless-shell:latest
        ports:
          - 9222:9222
        options: --shm-size=2g

    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      - name: Run E2E tests
        run: go test -v -tags=e2e ./...
        env:
          CHROME_WS_URL: ws://localhost:9222
          TOXIPROXY_URL: localhost:8474
```

### Build Tags for E2E Tests

```go
//go:build e2e
// +build e2e

package e2e_test

// E2E tests isolated from unit tests
// Run with: go test -tags=e2e ./...
```

---

## Installation Commands

```bash
# Add E2E testing dependencies
go get github.com/go-rod/rod@v0.116.2
go get github.com/Shopify/toxiproxy/v2@v2.12.0

# Local development: install toxiproxy server
# macOS:
brew install toxiproxy

# Linux:
wget https://github.com/Shopify/toxiproxy/releases/download/v2.12.0/toxiproxy-server-linux-amd64
chmod +x toxiproxy-server-linux-amd64
./toxiproxy-server-linux-amd64

# Or Docker (any platform):
docker run -d -p 8474:8474 --name toxiproxy ghcr.io/shopify/toxiproxy:2.12.0

# For Rod: Chrome is auto-downloaded on first run
# Or use system Chrome: ROD_BROWSER=/path/to/chrome go test -tags=e2e ./...
```

---

## Confidence Assessment

| Component | Confidence | Basis |
|-----------|------------|-------|
| Rod selection | **HIGH** | Official docs, GitHub comparison, pkg.go.dev |
| Toxiproxy selection | **HIGH** | Official docs, Shopify maintenance, Go client docs |
| Chrome flags | **HIGH** | webrtc.org official testing documentation |
| GitHub Actions approach | **MEDIUM** | Community articles, Docker image docs |
| Version numbers | **HIGH** | pkg.go.dev verified (2026-01-22) |

**Overall Confidence: HIGH**

---

## Sources

### Official Documentation (HIGH Confidence)
- [Rod GitHub](https://github.com/go-rod/rod)
- [Rod pkg.go.dev](https://pkg.go.dev/github.com/go-rod/rod) - v0.116.2
- [Toxiproxy GitHub](https://github.com/Shopify/toxiproxy)
- [Toxiproxy Go client](https://pkg.go.dev/github.com/Shopify/toxiproxy/v2/client) - v2.12.0
- [WebRTC.org Testing](https://webrtc.github.io/webrtc-org/testing/)
- [chromedp/headless-shell](https://github.com/chromedp/docker-headless-shell)

### Comparison/Analysis (MEDIUM Confidence)
- [Rod vs chromedp](https://github.com/go-rod/go-rod.github.io/blob/main/why-rod.md)
- [GitHub Actions Chrome testing](https://pradappandiyan.medium.com/running-ui-automation-tests-with-go-and-chrome-on-github-actions-1f56d7c63405)
- [Toxiproxy usage guide](https://www.dolthub.com/blog/2024-03-13-golang-toxiproxy/)

### Verified Versions (2026-01-22)
| Package | Version | Source |
|---------|---------|--------|
| go-rod/rod | v0.116.2 | pkg.go.dev |
| Shopify/toxiproxy/v2 | v2.12.0 | pkg.go.dev |
| chromedp/chromedp | v0.13.2 | GitHub releases |
| playwright-go | v0.5200.1 | GitHub releases |

---

## Recommendation Summary

**Add to project:**

| Package | Version | Purpose |
|---------|---------|---------|
| `github.com/go-rod/rod` | v0.116.2 | Chrome automation |
| `github.com/Shopify/toxiproxy/v2` | v2.12.0 | Network simulation |

**CI Infrastructure:**

| Component | Image | Purpose |
|-----------|-------|---------|
| Chrome | `chromedp/headless-shell:latest` | Headless browser |
| Toxiproxy | `ghcr.io/shopify/toxiproxy:2.12.0` | Network proxy |

**Total additions:** 2 Go dependencies, 2 Docker service containers

**Next Steps:** Proceed to E2E testing implementation with this stack.
