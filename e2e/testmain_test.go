//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestMain(m *testing.M) {
	// Run all tests
	code := m.Run()

	// Cleanup: Kill any orphaned Chrome processes
	// This is a safety net for test failures/panics where
	// defer browser.Close() didn't run
	cleanupOrphanedBrowsers()

	os.Exit(code)
}

// cleanupOrphanedBrowsers attempts to kill Chrome processes that may have
// been left behind by failed tests. This is best-effort cleanup.
//
// In normal operation, each test's defer browser.Close() handles cleanup.
// This function catches edge cases like panics or os.Exit during tests.
func cleanupOrphanedBrowsers() {
	switch runtime.GOOS {
	case "darwin", "linux":
		// pkill returns non-zero if no processes matched, ignore error
		// Target both chromium (Rod downloads) and chrome (system install)
		_ = exec.Command("pkill", "-f", "chromium|chrome").Run()
	case "windows":
		// taskkill returns non-zero if process not found, ignore error
		_ = exec.Command("taskkill", "/F", "/IM", "chrome.exe").Run()
		_ = exec.Command("taskkill", "/F", "/IM", "chromium.exe").Run()
	}
}
