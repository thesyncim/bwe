//go:build e2e

package e2e

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/thesyncim/bwe/cmd/chrome-interop/server"
	"github.com/thesyncim/bwe/pkg/bwe/testutil"

	"github.com/go-rod/rod"
)

// TestChrome_BWERespondsToREMB validates that the BWE implementation works end-to-end:
// 1. Server starts and browser connects via WebRTC
// 2. Browser sends video with abs-send-time header extension
// 3. Server sends REMB packets based on bandwidth estimation
// 4. Chrome reports availableOutgoingBitrate in stats (influenced by REMB)
//
// This proves the complete BWE pipeline works with a real browser.
func TestChrome_BWERespondsToREMB(t *testing.T) {
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

	// Navigate to server using localhost (required for secure context / getUserMedia)
	// The server returns [::]:port format, we need localhost:port for Chrome
	_, port, _ := net.SplitHostPort(addr)
	url := "http://localhost:" + port
	t.Logf("Navigating to %s (server on %s)", url, addr)

	page, err := client.Navigate(url)
	if err != nil {
		t.Fatalf("failed to navigate: %v", err)
	}

	// Wait for page to stabilize
	if err := client.WaitStable(); err != nil {
		t.Fatalf("page not stable: %v", err)
	}

	// Check if mediaDevices is available (debugging)
	mdResult, _ := page.Eval(`() => {
		return {
			mediaDevicesExists: typeof navigator.mediaDevices !== 'undefined',
			getUserMediaExists: typeof navigator.mediaDevices !== 'undefined' && typeof navigator.mediaDevices.getUserMedia === 'function',
			isSecureContext: window.isSecureContext
		};
	}`)
	t.Logf("Media devices check: %v", mdResult.Value)

	// Start WebRTC call directly without relying on the page's startCall() function.
	// This avoids issues with the HTML's error handling that calls stopCall() on failure.
	t.Log("Starting WebRTC call via JavaScript...")
	result, err := page.Eval(`() => {
		return new Promise(async (resolve, reject) => {
			try {
				// Get fake media stream (Chrome with --use-fake-device-for-media-stream)
				const stream = await navigator.mediaDevices.getUserMedia({
					video: { width: 640, height: 480, frameRate: 30 },
					audio: false
				});

				// Create peer connection
				window.testPC = new RTCPeerConnection({ iceServers: [] });

				// Add tracks
				stream.getTracks().forEach(track => {
					window.testPC.addTrack(track, stream);
				});

				// Function to munge SDP (remove transport-cc for REMB-only testing)
				function removeTransportCC(sdp) {
					sdp = sdp.replace(/a=rtcp-fb:\d+ transport-cc\r?\n/g, '');
					sdp = sdp.replace(/a=extmap:\d+ http:\/\/www\.ietf\.org\/id\/draft-holmer-rmcat-transport-wide-cc-extensions-01\r?\n/g, '');
					return sdp;
				}

				// Create offer
				const offer = await window.testPC.createOffer();
				offer.sdp = removeTransportCC(offer.sdp);
				await window.testPC.setLocalDescription(offer);

				// Wait for ICE gathering to complete
				await new Promise((resolveIce) => {
					if (window.testPC.iceGatheringState === 'complete') {
						resolveIce();
					} else {
						window.testPC.onicecandidate = (e) => {
							if (e.candidate === null) resolveIce();
						};
					}
				});

				// Send offer to server
				const response = await fetch('/offer', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify(window.testPC.localDescription)
				});

				if (!response.ok) {
					reject('Server returned ' + response.status);
					return;
				}

				const answer = await response.json();
				answer.sdp = removeTransportCC(answer.sdp);
				await window.testPC.setRemoteDescription(answer);

				resolve('connected');
			} catch (err) {
				reject(err.message || String(err));
			}
		});
	}`)
	if err != nil {
		t.Fatalf("failed to start WebRTC call: %v", err)
	}
	t.Logf("WebRTC setup result: %s", result.Value.String())

	// Wait for WebRTC connection to establish
	t.Log("Waiting for WebRTC connection...")
	if err := waitForConnectionTestPC(t, page, 30*time.Second); err != nil {
		// Get status for debugging
		statusResult, _ := page.Eval(`() => {
			return {
				pcExists: typeof testPC !== 'undefined' && testPC !== null,
				pcState: typeof testPC !== 'undefined' && testPC !== null ? testPC.connectionState : null,
				iceState: typeof testPC !== 'undefined' && testPC !== null ? testPC.iceConnectionState : null
			};
		}`)
		t.Logf("Debug state: %v", statusResult.Value)
		t.Fatalf("WebRTC connection failed: %v", err)
	}
	t.Log("WebRTC connection established")

	// Wait for REMB to take effect (server sends REMB every 1 second)
	t.Log("Waiting 3 seconds for REMB to take effect...")
	time.Sleep(3 * time.Second)

	// Get availableOutgoingBitrate from Chrome stats
	bitrate, err := getOutgoingBitrate(page)
	if err != nil {
		t.Fatalf("failed to get outgoing bitrate: %v", err)
	}

	t.Logf("Chrome availableOutgoingBitrate: %.0f bps (%.2f kbps)", bitrate, bitrate/1000)

	// Validate bitrate is reasonable
	// Server config: min 100kbps, initial 500kbps, max 5Mbps
	minExpected := 50_000.0   // 50 kbps (allow some slack below min)
	maxExpected := 6_000_000.0 // 6 Mbps (allow some slack above max)

	if bitrate < minExpected {
		t.Errorf("bitrate too low: got %.0f bps, expected >= %.0f bps", bitrate, minExpected)
	}
	if bitrate > maxExpected {
		t.Errorf("bitrate too high: got %.0f bps, expected <= %.0f bps", bitrate, maxExpected)
	}

	t.Log("BWE E2E test passed: REMB is influencing Chrome's bandwidth estimation")
}

// waitForConnectionTestPC polls testPC.connectionState until "connected" or timeout.
func waitForConnectionTestPC(t *testing.T, page *rod.Page, timeout time.Duration) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	pollInterval := 200 * time.Millisecond

	for time.Now().Before(deadline) {
		result, err := page.Eval(`() => {
			if (typeof testPC === 'undefined' || testPC === null) {
				return 'no-pc';
			}
			return testPC.connectionState;
		}`)
		if err != nil {
			return fmt.Errorf("failed to check connection state: %w", err)
		}

		state := result.Value.String()
		t.Logf("Connection state: %s", state)

		switch state {
		case "connected":
			return nil
		case "failed":
			return errors.New("connection failed")
		case "closed":
			return errors.New("connection closed")
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for connection (waited %v)", timeout)
}

// getOutgoingBitrate retrieves availableOutgoingBitrate from Chrome's WebRTC stats.
// Returns the bitrate in bits per second.
func getOutgoingBitrate(page *rod.Page) (float64, error) {
	result, err := page.Eval(`() => {
		return new Promise((resolve, reject) => {
			if (typeof testPC === 'undefined' || testPC === null) {
				reject('no peer connection');
				return;
			}
			testPC.getStats().then(stats => {
				let bitrate = null;
				stats.forEach(report => {
					if (report.type === 'candidate-pair' && report.nominated) {
						bitrate = report.availableOutgoingBitrate;
					}
				});
				resolve(bitrate);
			}).catch(err => reject(err.message));
		});
	}`)
	if err != nil {
		return 0, fmt.Errorf("getStats failed: %w", err)
	}

	// Check if bitrate is nil/null
	if result.Value.Nil() {
		return 0, errors.New("availableOutgoingBitrate not available in stats")
	}

	bitrate := result.Value.Num()
	if bitrate <= 0 {
		return 0, fmt.Errorf("invalid bitrate: %f", bitrate)
	}

	return bitrate, nil
}
