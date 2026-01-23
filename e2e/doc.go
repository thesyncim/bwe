//go:build e2e

// Package e2e provides end-to-end tests for the BWE implementation.
//
// These tests are isolated from the standard test suite via build tags.
// They require a Chrome browser (auto-downloaded by Rod if not present)
// and are intended for CI pipelines or explicit local testing.
//
// Running E2E tests:
//
//	go test -tags=e2e ./e2e/...
//
// Running all tests except E2E:
//
//	go test ./...
//
// E2E tests use:
//   - Rod for browser automation (Chrome DevTools Protocol)
//   - chrome-interop server for WebRTC signaling
//   - BrowserClient from pkg/bwe/testutil for Chrome helpers
//
// Test isolation:
// Each test starts its own server on a random port and launches
// its own browser instance. Tests can run in parallel.
package e2e
