// browser.go provides browser automation utilities for E2E testing.
// It wraps Rod to provide WebRTC-ready Chrome instances.
package testutil

import (
	"errors"
	"fmt"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
)

// BrowserConfig configures Chrome launch options.
type BrowserConfig struct {
	Headless bool          // Run in headless mode (default: true)
	Timeout  time.Duration // Default operation timeout (default: 30s)
}

// DefaultBrowserConfig returns sensible defaults for E2E testing.
func DefaultBrowserConfig() BrowserConfig {
	return BrowserConfig{
		Headless: true,
		Timeout:  30 * time.Second,
	}
}

// BrowserClient wraps Rod with WebRTC-ready Chrome configuration.
type BrowserClient struct {
	browser *rod.Browser
	page    *rod.Page
	timeout time.Duration
}

// NewBrowserClient creates a headless Chrome with WebRTC flags.
// The browser is configured with:
//   - Fake media streams (no real camera/mic required)
//   - Auto-granted media permissions
//   - No sandbox (for container compatibility)
//   - Autoplay without user gesture
func NewBrowserClient(cfg BrowserConfig) (*BrowserClient, error) {
	l := launcher.New().
		Headless(cfg.Headless).
		Set("no-sandbox").
		Set("disable-gpu").
		Set("use-fake-device-for-media-stream").
		Set("use-fake-ui-for-media-stream").
		Set("autoplay-policy", "no-user-gesture-required")

	url, err := l.Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch Chrome: %w", err)
	}

	browser := rod.New().ControlURL(url)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to Chrome: %w", err)
	}

	return &BrowserClient{
		browser: browser,
		timeout: cfg.Timeout,
	}, nil
}

// Navigate opens a URL with timeout.
// Returns the page for further interaction.
func (c *BrowserClient) Navigate(url string) (*rod.Page, error) {
	page := c.browser.MustPage()
	c.page = page

	err := page.Timeout(c.timeout).Navigate(url)
	if err != nil {
		return nil, fmt.Errorf("failed to navigate to %s: %w", url, err)
	}

	// Cancel timeout so Close() works
	page.CancelTimeout()
	return page, nil
}

// Page returns the current page, or nil if none open.
func (c *BrowserClient) Page() *rod.Page {
	return c.page
}

// Eval executes JavaScript and returns the result.
// Requires Navigate() to have been called first.
func (c *BrowserClient) Eval(js string) (interface{}, error) {
	if c.page == nil {
		return nil, errors.New("no page open, call Navigate first")
	}
	result, err := c.page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("eval failed: %w", err)
	}
	return result.Value, nil
}

// WaitStable waits for the page to be stable (no DOM changes).
func (c *BrowserClient) WaitStable() error {
	if c.page == nil {
		return errors.New("no page open")
	}
	return c.page.WaitStable(c.timeout)
}

// Close cleans up browser resources.
// Always call this (via defer) to prevent orphaned Chrome processes.
func (c *BrowserClient) Close() error {
	if c.browser != nil {
		return c.browser.Close()
	}
	return nil
}
