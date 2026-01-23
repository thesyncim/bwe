//go:build e2e

package e2e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/thesyncim/bwe/cmd/chrome-interop/server"
	"github.com/thesyncim/bwe/pkg/bwe/testutil"
)

// TestChrome_CanConnect verifies the complete E2E test infrastructure:
// 1. Server can start programmatically on random port
// 2. Browser can launch in headless mode with WebRTC flags
// 3. Browser can navigate to server
// 4. Page loads successfully
// 5. Cleanup works (no orphaned processes)
//
// This is a smoke test - it validates infrastructure, not BWE behavior.
func TestChrome_CanConnect(t *testing.T) {
	// Start server on random port
	cfg := server.DefaultConfig()
	srv, err := server.NewServer(cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			t.Errorf("server shutdown error: %v", err)
		}
	}()

	t.Logf("Server started on %s", addr)

	// Launch browser
	browserCfg := testutil.DefaultBrowserConfig()
	client, err := testutil.NewBrowserClient(browserCfg)
	if err != nil {
		t.Fatalf("failed to create browser: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Errorf("browser close error: %v", err)
		}
	}()

	// Navigate to server
	url := "http://" + addr
	t.Logf("Navigating to %s", url)

	page, err := client.Navigate(url)
	if err != nil {
		t.Fatalf("failed to navigate: %v", err)
	}

	// Wait for page to stabilize
	if err := client.WaitStable(); err != nil {
		t.Fatalf("page not stable: %v", err)
	}

	// Verify page loaded by checking title
	title := page.MustElement("title").MustText()
	if !strings.Contains(title, "BWE") {
		t.Errorf("unexpected page title: got %q, want contains 'BWE'", title)
	}

	// Verify WebRTC is available
	// Use page.Eval directly with proper function wrapper for Rod
	rtcResult, err := page.Eval(`() => typeof RTCPeerConnection !== 'undefined'`)
	if err != nil {
		t.Fatalf("failed to check RTCPeerConnection: %v", err)
	}
	hasRTC := rtcResult.Value.Bool()
	if !hasRTC {
		t.Error("RTCPeerConnection not available in browser")
	}

	t.Log("Smoke test passed: server, browser, and WebRTC all working")
}
