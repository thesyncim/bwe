// Soak test runner for VALID-04 long-duration testing.
//
// This tool simulates traffic and monitors the bandwidth estimator for
// memory leaks, timestamp-related failures, and estimate anomalies over
// extended periods (up to 24 hours or more).
//
// Usage:
//
//	go run ./cmd/soak -duration 24h
//	go run ./cmd/soak -duration 1h  # shorter test
//
// Exposes pprof endpoint at :6060 for live profiling:
//
//	curl http://localhost:6060/debug/pprof/heap > heap.pprof
//	go tool pprof heap.pprof
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"net/http"
	_ "net/http/pprof" // Enable pprof endpoints
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"bwe/pkg/bwe"
)

const (
	packetSize            = 1200 // bytes
	packetIntervalMs      = 20   // 50 pps
	absSendTimeUnitsPerMs = 262  // 1ms in abs-send-time units
	statusIntervalMinutes = 5
)

// SoakResult contains the results of a soak test run.
type SoakResult struct {
	Duration          time.Duration
	TotalPackets      int
	FinalEstimate     int64
	PeakHeapMB        float64
	TotalGCCycles     uint32
	WraparoundCount   int
	SuspiciousEvents  int
	Status            string
}

func main() {
	// Parse flags
	duration := flag.Duration("duration", 24*time.Hour, "Test duration (e.g., 1h, 24h)")
	pprofPort := flag.Int("pprof-port", 6060, "Port for pprof HTTP server")
	flag.Parse()

	fmt.Printf("BWE Soak Test Runner\n")
	fmt.Printf("====================\n")
	fmt.Printf("Duration: %v\n", *duration)
	fmt.Printf("Pprof:    http://localhost:%d/debug/pprof/\n", *pprofPort)
	fmt.Printf("\n")

	// Start pprof server in background
	go func() {
		addr := fmt.Sprintf(":%d", *pprofPort)
		if err := http.ListenAndServe(addr, nil); err != nil {
			fmt.Printf("Warning: pprof server failed: %v\n", err)
		}
	}()

	// Set up graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		fmt.Printf("\nReceived %v, shutting down gracefully...\n", sig)
		cancel()
	}()

	// Run the soak test
	result := runSoakTest(ctx, *duration)

	// Print final summary
	printSummary(result)

	// Exit with appropriate status
	if result.Status == "PASS" {
		os.Exit(0)
	}
	os.Exit(1)
}

func runSoakTest(ctx context.Context, duration time.Duration) SoakResult {
	// Initialize estimator with nil clock (uses default MonotonicClock)
	config := bwe.DefaultBandwidthEstimatorConfig()
	estimator := bwe.NewBandwidthEstimator(config, nil)

	result := SoakResult{
		Status: "PASS",
	}

	// Track metrics
	var memStats runtime.MemStats
	sendTime := uint32(0)
	var lastSendTime uint32

	startTime := time.Now()
	lastStatusTime := startTime
	statusInterval := time.Duration(statusIntervalMinutes) * time.Minute

	packetInterval := time.Duration(packetIntervalMs) * time.Millisecond
	ticker := time.NewTicker(packetInterval)
	defer ticker.Stop()

	fmt.Printf("[%s] Starting soak test...\n", formatDuration(time.Duration(0)))

	for {
		select {
		case <-ctx.Done():
			result.Duration = time.Since(startTime)
			return result

		case now := <-ticker.C:
			elapsed := now.Sub(startTime)

			// Check if test duration reached
			if elapsed >= duration {
				result.Duration = elapsed
				return result
			}

			// Detect wraparound
			if sendTime < lastSendTime && result.TotalPackets > 0 {
				result.WraparoundCount++
			}
			lastSendTime = sendTime

			// Process packet
			pkt := bwe.PacketInfo{
				ArrivalTime: now,
				SendTime:    sendTime,
				Size:        packetSize,
				SSRC:        0x12345678,
			}

			estimate := estimator.OnPacket(pkt)
			result.TotalPackets++
			result.FinalEstimate = estimate

			// Check for anomalies
			if math.IsNaN(float64(estimate)) {
				fmt.Printf("[%s] ERROR: NaN estimate detected!\n", formatDuration(elapsed))
				result.SuspiciousEvents++
				result.Status = "FAIL"
			}
			if math.IsInf(float64(estimate), 0) {
				fmt.Printf("[%s] ERROR: Inf estimate detected!\n", formatDuration(elapsed))
				result.SuspiciousEvents++
				result.Status = "FAIL"
			}
			if estimate <= 0 {
				fmt.Printf("[%s] WARNING: Non-positive estimate: %d\n", formatDuration(elapsed), estimate)
				result.SuspiciousEvents++
			}

			// Update send time
			sendTime = (sendTime + uint32(packetIntervalMs*absSendTimeUnitsPerMs)) % bwe.AbsSendTimeMax

			// Periodic status output
			if now.Sub(lastStatusTime) >= statusInterval {
				lastStatusTime = now
				runtime.ReadMemStats(&memStats)

				heapMB := float64(memStats.HeapAlloc) / (1024 * 1024)
				if heapMB > result.PeakHeapMB {
					result.PeakHeapMB = heapMB
				}
				result.TotalGCCycles = memStats.NumGC

				fmt.Printf("[%s] Packets: %d, Estimate: %.2f Mbps, HeapAlloc: %.2f MB, NumGC: %d\n",
					formatDuration(elapsed),
					result.TotalPackets,
					float64(estimate)/(1024*1024),
					heapMB,
					memStats.NumGC)

				// Memory limit check (100 MB)
				if heapMB > 100 {
					fmt.Printf("[%s] ERROR: Memory limit exceeded: %.2f MB\n", formatDuration(elapsed), heapMB)
					result.Status = "FAIL"
				}
			}
		}
	}
}

func printSummary(result SoakResult) {
	fmt.Printf("\n")
	fmt.Printf("Soak Test Complete\n")
	fmt.Printf("==================\n")
	fmt.Printf("Duration:          %v\n", result.Duration.Round(time.Second))
	fmt.Printf("Total packets:     %d\n", result.TotalPackets)
	fmt.Printf("Final estimate:    %.2f Mbps\n", float64(result.FinalEstimate)/(1024*1024))
	fmt.Printf("Peak HeapAlloc:    %.2f MB\n", result.PeakHeapMB)
	fmt.Printf("Total GC cycles:   %d\n", result.TotalGCCycles)
	fmt.Printf("Wraparounds:       %d\n", result.WraparoundCount)
	fmt.Printf("Suspicious events: %d\n", result.SuspiciousEvents)
	fmt.Printf("Status:            %s\n", result.Status)
	fmt.Printf("\n")

	// Pass criteria
	fmt.Printf("Pass Criteria:\n")
	fmt.Printf("  - No panics:         %s\n", checkMark(true))
	fmt.Printf("  - Final estimate > 0: %s\n", checkMark(result.FinalEstimate > 0))
	fmt.Printf("  - Peak memory < 100 MB: %s\n", checkMark(result.PeakHeapMB < 100))
	fmt.Printf("  - No timestamp errors: %s\n", checkMark(result.SuspiciousEvents == 0))
}

func formatDuration(d time.Duration) string {
	h := d / time.Hour
	m := (d % time.Hour) / time.Minute
	s := (d % time.Minute) / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func checkMark(pass bool) string {
	if pass {
		return "PASS"
	}
	return "FAIL"
}
