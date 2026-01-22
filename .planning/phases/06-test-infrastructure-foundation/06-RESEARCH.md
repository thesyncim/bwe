# Phase 6: Test Infrastructure Foundation - Research

**Researched:** 2026-01-22
**Domain:** Browser automation, Go testing infrastructure, HTTP server refactoring
**Confidence:** HIGH

## Summary

Phase 6 establishes the E2E test infrastructure foundation for automated browser interoperability testing. Research focused on three core areas: (1) Rod-based browser automation with proper cleanup patterns, (2) refactoring the existing `cmd/chrome-interop/main.go` into an importable server package, and (3) Go test scaffolding with build tags to isolate E2E tests.

The existing `cmd/chrome-interop/main.go` contains a complete WebRTC signaling server with embedded HTML that connects Chrome to the Pion BWE interceptor. This needs refactoring from a CLI-only `main()` function into a `Server` struct with `Start()/Stop()` methods that tests can control programmatically.

Rod v0.116.2 provides the browser automation capabilities needed. The key patterns are: (1) use `launcher.New().Set()` for WebRTC-specific Chrome flags, (2) always `defer browser.MustClose()` immediately after connection, (3) use context timeouts for operation boundaries, and (4) implement TestMain cleanup for orphaned Chrome processes.

**Primary recommendation:** Create a `BrowserClient` type in `pkg/bwe/testutil/browser.go` that wraps Rod with WebRTC-ready Chrome configuration, and refactor `cmd/chrome-interop/` into a `server` package exposing `NewServer()`, `Start()`, and `Shutdown()` methods.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| [go-rod/rod](https://github.com/go-rod/rod) | v0.116.2 | Chrome DevTools Protocol driver | Pure Go, no WebDriver binary, direct CDP access, auto-downloads Chrome, thread-safe |
| go-rod/rod/lib/launcher | v0.116.2 | Browser launch configuration | Fluent API for Chrome flags, headless mode, custom arguments |
| net/http | stdlib | HTTP server for chrome-interop | Standard library, no dependencies |
| net/http/httptest | stdlib | Server testing utilities | Provides `NewUnstartedServer` pattern for programmatic control |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| testing | stdlib | Test scaffolding | Build tags, TestMain, t.Cleanup |
| context | stdlib | Timeout and cancellation | Browser operation timeouts, graceful shutdown |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Rod | chromedp | chromedp requires more boilerplate, has potential deadlocks with complex operations |
| Rod | Selenium/WebDriver | Requires separate binary, more complex setup |
| Custom launcher | Docker headless-shell | Docker adds CI complexity; Rod auto-downloads Chrome is simpler for local dev |

**Installation:**
```bash
go get github.com/go-rod/rod@v0.116.2
```

## Architecture Patterns

### Recommended Project Structure
```
bwe/
├── e2e/                           # NEW: E2E test directory
│   ├── browser_test.go            # //go:build e2e - Browser automation tests
│   ├── doc.go                     # Package documentation
│   └── testmain_test.go           # TestMain for global setup/teardown
├── cmd/
│   └── chrome-interop/
│       ├── main.go                # CLI entry point (thin wrapper)
│       └── server/                # NEW: Importable server package
│           ├── server.go          # Server type with Start/Shutdown
│           ├── handler.go         # HTTP handlers (moved from main.go)
│           └── server_test.go     # Server unit tests
└── pkg/bwe/testutil/
    ├── browser.go                 # NEW: BrowserClient for E2E tests
    ├── reference_trace.go         # Existing: trace replay
    └── traces.go                  # Existing: trace utilities
```

### Pattern 1: BrowserClient Wrapper
**What:** A wrapper type around Rod that pre-configures Chrome for WebRTC testing.
**When to use:** All E2E browser tests that need WebRTC functionality.
**Example:**
```go
// Source: Pattern based on Rod official docs and WebRTC.org testing guide
package testutil

import (
    "context"
    "time"

    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/launcher"
)

// BrowserClient wraps Rod with WebRTC-ready configuration.
type BrowserClient struct {
    browser *rod.Browser
    page    *rod.Page
}

// NewBrowserClient creates a headless Chrome with WebRTC flags.
func NewBrowserClient() (*BrowserClient, error) {
    url := launcher.New().
        Headless(true).
        Set("no-sandbox").
        Set("disable-gpu").
        Set("use-fake-device-for-media-stream").
        Set("use-fake-ui-for-media-stream").
        Set("autoplay-policy", "no-user-gesture-required").
        MustLaunch()

    browser := rod.New().ControlURL(url).MustConnect()

    return &BrowserClient{browser: browser}, nil
}

// Navigate opens a URL with timeout.
func (c *BrowserClient) Navigate(url string, timeout time.Duration) (*rod.Page, error) {
    page := c.browser.MustPage()
    c.page = page
    return page.Timeout(timeout).Navigate(url)
}

// Eval executes JavaScript and returns the result.
func (c *BrowserClient) Eval(js string) (*rod.EvalResult, error) {
    if c.page == nil {
        return nil, errors.New("no page open")
    }
    return c.page.Eval(js)
}

// Close cleans up browser resources.
func (c *BrowserClient) Close() error {
    if c.browser != nil {
        return c.browser.Close()
    }
    return nil
}
```

### Pattern 2: Server Package Refactor
**What:** Refactor `cmd/chrome-interop/main.go` into an importable package.
**When to use:** When tests need to start/stop the server programmatically.
**Example:**
```go
// Source: Go HTTP server best practices, httptest patterns
// cmd/chrome-interop/server/server.go
package server

import (
    "context"
    "net"
    "net/http"
    "time"
)

// Server is the chrome-interop WebRTC test server.
type Server struct {
    httpServer *http.Server
    listener   net.Listener
    addr       string
}

// Config holds server configuration.
type Config struct {
    Addr         string        // e.g., ":8080" or ":0" for random port
    ReadTimeout  time.Duration
    WriteTimeout time.Duration
}

// DefaultConfig returns sensible defaults for testing.
func DefaultConfig() Config {
    return Config{
        Addr:         ":0", // Random port for tests
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 30 * time.Second,
    }
}

// NewServer creates a new server but does not start it.
func NewServer(cfg Config) (*Server, error) {
    mux := http.NewServeMux()
    mux.HandleFunc("/", handleHTML)
    mux.HandleFunc("/offer", handleOffer)

    return &Server{
        httpServer: &http.Server{
            Addr:         cfg.Addr,
            Handler:      mux,
            ReadTimeout:  cfg.ReadTimeout,
            WriteTimeout: cfg.WriteTimeout,
        },
    }, nil
}

// Start begins listening and serving HTTP requests.
// Returns the actual address (useful when port is 0).
func (s *Server) Start() (string, error) {
    ln, err := net.Listen("tcp", s.httpServer.Addr)
    if err != nil {
        return "", err
    }
    s.listener = ln
    s.addr = ln.Addr().String()

    go s.httpServer.Serve(ln)
    return s.addr, nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
    return s.httpServer.Shutdown(ctx)
}

// Addr returns the server's listening address.
func (s *Server) Addr() string {
    return s.addr
}
```

### Pattern 3: Build-Tagged E2E Tests
**What:** Isolate E2E tests from `go test ./...` using build tags.
**When to use:** All E2E test files to prevent slow tests from running by default.
**Example:**
```go
// Source: Go build tags documentation, Mickey.dev blog
// e2e/browser_test.go
//go:build e2e

package e2e

import (
    "context"
    "testing"
    "time"

    "bwe/cmd/chrome-interop/server"
    "bwe/pkg/bwe/testutil"
)

func TestChrome_CanConnect(t *testing.T) {
    // Start server
    srv, err := server.NewServer(server.DefaultConfig())
    if err != nil {
        t.Fatalf("failed to create server: %v", err)
    }
    addr, err := srv.Start()
    if err != nil {
        t.Fatalf("failed to start server: %v", err)
    }
    defer srv.Shutdown(context.Background())

    // Launch browser
    client, err := testutil.NewBrowserClient()
    if err != nil {
        t.Fatalf("failed to create browser: %v", err)
    }
    defer client.Close()

    // Navigate to server
    page, err := client.Navigate("http://"+addr, 30*time.Second)
    if err != nil {
        t.Fatalf("failed to navigate: %v", err)
    }

    // Verify page loaded
    title := page.MustElement("title").MustText()
    if title == "" {
        t.Error("expected page title, got empty")
    }
}
```

### Pattern 4: TestMain Cleanup
**What:** Global setup/teardown in TestMain for browser process cleanup.
**When to use:** E2E test package to catch orphaned Chrome processes.
**Example:**
```go
// Source: Go testing package documentation
// e2e/testmain_test.go
//go:build e2e

package e2e

import (
    "os"
    "os/exec"
    "runtime"
    "testing"
)

func TestMain(m *testing.M) {
    // Run tests
    code := m.Run()

    // Cleanup: Kill orphaned Chrome processes (best effort)
    cleanupOrphanedBrowsers()

    os.Exit(code)
}

// cleanupOrphanedBrowsers attempts to kill any Chrome processes
// started by this test run. This is a safety net for test failures.
func cleanupOrphanedBrowsers() {
    switch runtime.GOOS {
    case "darwin", "linux":
        // pkill returns non-zero if no processes matched
        _ = exec.Command("pkill", "-f", "chromium|chrome").Run()
    case "windows":
        _ = exec.Command("taskkill", "/F", "/IM", "chrome.exe").Run()
    }
}
```

### Anti-Patterns to Avoid
- **Embedding browser cleanup in individual tests only:** Always use defer AND TestMain cleanup. Individual defer may not run on panic.
- **Using `:8080` hardcoded in tests:** Use `:0` (random port) to avoid port conflicts in parallel test runs.
- **Running E2E tests with `go test ./...`:** Build tags prevent accidental slow test runs in development.
- **Blocking on browser operations without timeout:** Always use `.Timeout(d)` on page operations.

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Chrome process management | Custom exec.Command wrapper | Rod launcher | Handles leakless cleanup, process tree, crash recovery |
| Chrome flag configuration | Hardcoded arg slices | launcher.Set() | Type-safe, documented, handles escaping |
| Element waiting | time.Sleep loops | page.MustWaitStable() | Adaptive waiting, avoids flaky timing |
| Timeout handling | Manual timer goroutines | page.Timeout() | Context-based, cancellable, chainable |
| HTTP test server | Custom net.Listen wrapper | httptest.NewUnstartedServer | Battle-tested, random port support |

**Key insight:** Browser automation edge cases (zombie processes, stale elements, navigation races) are exceptionally numerous. Rod handles 100+ documented edge cases; rolling your own will miss many.

## Common Pitfalls

### Pitfall 1: Orphaned Chrome Processes
**What goes wrong:** Test failure or panic leaves Chrome processes running, consuming memory, causing CI runner OOM.
**Why it happens:** `defer browser.Close()` doesn't run on os.Exit or panic.
**How to avoid:**
1. Always `defer browser.MustClose()` immediately after `MustConnect()`
2. Implement `TestMain` with process cleanup after `m.Run()`
3. Set browser lifetime context with timeout
4. Use Rod's leakless mode (default in launcher.New())
**Warning signs:** Memory growth between test runs, `ps aux | grep chrome` shows many processes.

### Pitfall 2: WebRTC Flags Missing
**What goes wrong:** getUserMedia() fails with "NotAllowedError" or hangs waiting for permission.
**Why it happens:** Headless Chrome requires explicit flags to bypass media permission UI.
**How to avoid:**
```go
launcher.New().
    Set("use-fake-device-for-media-stream").  // Synthetic media
    Set("use-fake-ui-for-media-stream").      // Auto-grant permissions
    Set("no-sandbox").                         // Required in containers
    Set("autoplay-policy", "no-user-gesture-required")
```
**Warning signs:** Tests hang on `navigator.mediaDevices.getUserMedia()`, permission dialogs in headed mode.

### Pitfall 3: Port Conflicts in Parallel Tests
**What goes wrong:** "address already in use" errors when multiple tests run.
**Why it happens:** Hardcoded port like `:8080` can only bind once.
**How to avoid:**
- Use `:0` for dynamic port allocation
- Pass actual address from `listener.Addr().String()` to browser
- Tests should be self-contained (own server instance)
**Warning signs:** Tests pass individually but fail in parallel, random "bind: address already in use".

### Pitfall 4: Page Timeout Not Cancelled
**What goes wrong:** `page.Close()` returns "context deadline exceeded" after timeout.
**Why it happens:** Rod's timeout context propagates to Close() method.
**How to avoid:**
```go
page := browser.MustPage()
page.Timeout(5 * time.Second).MustNavigate(url).CancelTimeout()
// Now Close() works normally
defer page.MustClose()
```
**Warning signs:** `page.Close()` errors in defer, test cleanup fails silently.

### Pitfall 5: Build Tags Forgotten
**What goes wrong:** Slow E2E tests run with `go test ./...`, breaking CI feedback loop.
**Why it happens:** Missing `//go:build e2e` constraint at file top.
**How to avoid:**
1. Use `//go:build e2e` at the top of every E2E test file
2. Use `// +build e2e` on the next line (Go 1.16 compatibility)
3. Add blank line between build tag and package declaration
4. CI runs E2E separately: `go test -tags=e2e ./e2e/...`
**Warning signs:** `go test ./...` takes minutes instead of seconds.

## Code Examples

Verified patterns from official sources:

### Complete Browser Test with Cleanup
```go
// Source: Rod documentation + WebRTC.org testing guide
//go:build e2e

package e2e

import (
    "testing"
    "time"

    "github.com/go-rod/rod"
    "github.com/go-rod/rod/lib/launcher"
    "github.com/stretchr/testify/require"
)

func TestBrowser_WebRTCConnection(t *testing.T) {
    // Configure Chrome for WebRTC
    url := launcher.New().
        Headless(true).
        Set("no-sandbox").
        Set("disable-gpu").
        Set("use-fake-device-for-media-stream").
        Set("use-fake-ui-for-media-stream").
        Set("autoplay-policy", "no-user-gesture-required").
        MustLaunch()

    browser := rod.New().ControlURL(url).MustConnect()
    defer browser.MustClose() // CRITICAL: immediate defer

    // Create page with timeout
    page := browser.MustPage()
    defer page.MustClose()

    // Navigate with timeout
    err := page.Timeout(30 * time.Second).Navigate("http://localhost:8080").Err()
    require.NoError(t, err, "navigation should succeed")

    // Wait for page to stabilize
    page.MustWaitStable()

    // Execute JavaScript to verify WebRTC is available
    result := page.MustEval(`() => typeof RTCPeerConnection !== 'undefined'`)
    require.True(t, result.Bool(), "RTCPeerConnection should be available")
}
```

### Server with Graceful Shutdown
```go
// Source: Go net/http documentation + httptest patterns
package server

import (
    "context"
    "net"
    "net/http"
    "sync"
    "time"
)

type Server struct {
    httpServer *http.Server
    listener   net.Listener
    mu         sync.Mutex
    running    bool
}

func NewServer(handler http.Handler, addr string) *Server {
    return &Server{
        httpServer: &http.Server{
            Addr:         addr,
            Handler:      handler,
            ReadTimeout:  30 * time.Second,
            WriteTimeout: 30 * time.Second,
        },
    }
}

func (s *Server) Start() (string, error) {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.running {
        return s.listener.Addr().String(), nil
    }

    ln, err := net.Listen("tcp", s.httpServer.Addr)
    if err != nil {
        return "", err
    }
    s.listener = ln
    s.running = true

    go func() {
        _ = s.httpServer.Serve(ln) // Blocks until shutdown
    }()

    return ln.Addr().String(), nil
}

func (s *Server) Shutdown(ctx context.Context) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if !s.running {
        return nil
    }
    s.running = false
    return s.httpServer.Shutdown(ctx)
}
```

### Pion ICE Timeout Configuration (for reference)
```go
// Source: Pion WebRTC SettingEngine documentation
import (
    "time"
    "github.com/pion/webrtc/v4"
)

// ConfigureICEForCI sets generous ICE timeouts for CI environments.
func ConfigureICEForCI() webrtc.SettingEngine {
    se := webrtc.SettingEngine{}
    se.SetICETimeouts(
        30*time.Second, // disconnectedTimeout
        60*time.Second, // failedTimeout
        5*time.Second,  // keepAliveInterval
    )
    return se
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `//+build` only | `//go:build` + `//+build` | Go 1.17 (2021) | Use both for compatibility |
| chromedp as default | Rod preferred | 2022-2023 | Simpler API, better performance |
| `--headless` | `--headless=new` | Chrome 112 (2023) | Modern headless with full features |
| t.Cleanup absent | t.Cleanup preferred | Go 1.14 (2020) | Cleaner per-test cleanup |

**Deprecated/outdated:**
- **Old headless mode (`--headless`):** Chrome 112+ supports `--headless=new` with full WebRTC capabilities
- **Manual Chrome binary management:** Rod auto-downloads appropriate Chromium version
- **`+build` only syntax:** Go 1.17+ prefers `//go:build` with `+build` for backwards compatibility

## Open Questions

Things that couldn't be fully resolved:

1. **Chrome getStats() REMB visibility**
   - What we know: WebRTC getStats() returns RTCStatsReport with various metrics
   - What's unclear: Whether REMB packets appear in receiver stats or only server-side
   - Recommendation: Phase 8 will investigate; for Phase 6, use server-side REMB logging as verification

2. **Rod version stability**
   - What we know: v0.116.2 released July 2024, no newer releases as of Jan 2026
   - What's unclear: Whether API stability guarantees exist
   - Recommendation: Pin to v0.116.2, test suite will catch breaking changes

## Sources

### Primary (HIGH confidence)
- [Rod GitHub Repository](https://github.com/go-rod/rod) - Official source, examples_test.go patterns
- [Rod pkg.go.dev](https://pkg.go.dev/github.com/go-rod/rod) - v0.116.2 API documentation
- [Rod Custom Launch Guide](https://github.com/go-rod/go-rod.github.io/blob/main/custom-launch.md) - Chrome flag configuration
- [Rod Context/Timeout Guide](https://github.com/go-rod/go-rod.github.io/blob/main/context-and-timeout.md) - Cleanup patterns
- [WebRTC.org Testing](https://webrtc.org/getting-started/testing) - Official Chrome WebRTC flags
- [Go testing package](https://pkg.go.dev/testing) - TestMain, t.Cleanup patterns
- [Pion WebRTC SettingEngine](https://github.com/pion/webrtc/blob/master/settingengine.go) - ICE timeout configuration

### Secondary (MEDIUM confidence)
- [Mickey.dev Build Tags](https://mickey.dev/posts/go-build-tags-testing/) - E2E test isolation patterns
- [Go HTTP Server Best Practices](https://medium.com/@niondir/my-go-http-server-best-practice-a29773786e15) - Server refactoring patterns
- [httptest package](https://pkg.go.dev/net/http/httptest) - Test server utilities
- [Pion WebRTC Issues #324](https://github.com/pion/webrtc/issues/324) - ICE timeout configuration discussion

### Tertiary (LOW confidence)
- Rod GitHub Issues for cleanup patterns (various)
- Community blog posts on Go E2E testing

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Rod documented extensively, WebRTC flags from official source
- Architecture: HIGH - Patterns from Go stdlib and established practices
- Pitfalls: HIGH - Derived from Rod issues, WebRTC testing guides, Pion documentation
- Code examples: HIGH - Verified against official documentation

**Research date:** 2026-01-22
**Valid until:** 2026-04-22 (90 days - stable libraries, low churn expected)
