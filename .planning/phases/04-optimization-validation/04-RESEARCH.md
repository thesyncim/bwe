# Phase 4: Optimization & Validation - Research

**Researched:** 2026-01-22
**Domain:** Go Performance Optimization, Bandwidth Estimation Validation, WebRTC Interop Testing
**Confidence:** HIGH

## Summary

Phase 4 focuses on achieving production-ready performance and validating interoperability with libwebrtc/Chrome. The five requirements (PERF-01, VALID-01 through VALID-04) require a combination of Go memory profiling/optimization techniques, reference comparison testing against libwebrtc, Chrome interop validation, TCP fairness testing, and long-duration soak tests.

The key challenge is balancing optimization (reducing allocations to <1 per packet) with validation (comparing against libwebrtc behavior). The existing codebase already uses sync.Pool for PacketInfo (PERF-02), but the remaining allocation sources need profiling and optimization. Validation requires building a test harness that can replay packet traces through both this implementation and a reference, then compare the bandwidth estimates.

**Primary recommendation:** Use Go's benchmark infrastructure with `-benchmem` to identify remaining allocations, optimize with escape analysis (`go build -gcflags="-m"`), then validate using packet trace replay against libwebrtc reference and live Chrome interop testing via webrtc-internals.

## Standard Stack

The established libraries/tools for this domain:

### Core (Testing & Profiling)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `testing` (stdlib) | - | Benchmark infrastructure, AllocsPerOp | Official Go testing |
| `runtime/pprof` (stdlib) | - | Memory profiling (alloc_objects, inuse_space) | Official profiling |
| `net/http/pprof` (stdlib) | - | HTTP endpoint for live profiling | Standard for long-running tests |
| `github.com/stretchr/testify` | v1.8+ | Assertions in benchmarks | Already used in project |

### Core (Network Testing)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `tc` / `netem` (Linux) | - | Network impairment (delay, loss, bandwidth) | Standard for network testing |
| `pfctl` / `dnctl` (macOS) | - | Network impairment on macOS | macOS equivalent |
| `iperf3` | v3.x | TCP traffic generation for fairness testing | Industry standard |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `golang.org/x/perf/cmd/benchstat` | latest | Statistical analysis of benchmarks | Comparing optimization runs |
| Chrome + chrome://webrtc-internals | - | REMB validation, live debugging | Interop testing |
| `rtc_event_log_visualizer` | libwebrtc | RTC event log analysis | Reference comparison |
| Wireshark | 4.x | REMB packet inspection | Protocol debugging |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Manual pprof | Continuous profiling (pyroscope) | Overhead vs convenience for long tests |
| iperf3 | tc rate limiting | iperf3 gives realistic TCP competition |
| Chrome testing | Firefox testing | Chrome is primary target, Firefox for validation |

**Installation:**
```bash
# Go tools
go install golang.org/x/perf/cmd/benchstat@latest

# Network tools (macOS)
brew install iperf3

# Network tools (Linux)
sudo apt install iperf3 iproute2
```

## Architecture Patterns

### Recommended Test Structure
```
pkg/bwe/
├── benchmark_test.go          # PERF-01: Allocation benchmarks
├── testutil/
│   └── traces.go              # VALID-01: Packet trace replay utilities
│
└── interceptor/
    ├── benchmark_test.go      # PERF-01: Hot path benchmarks
    ├── validation_test.go     # VALID-01/02: Reference comparison
    └── integration_test.go    # Existing integration tests

cmd/
└── validation/                # NEW: Validation tools
    ├── chrome-interop/        # VALID-02: Chrome testing harness
    ├── tcp-fairness/          # VALID-03: TCP competition testing
    └── soak/                   # VALID-04: Long-duration testing
```

### Pattern 1: Allocation-Focused Benchmarks

**What:** Benchmarks specifically targeting allocation counts, not just speed.
**When to use:** PERF-01 verification - must achieve <1 alloc/op.
**Example:**
```go
// Source: Go testing package documentation
func BenchmarkProcessRTP_Allocations(b *testing.B) {
    b.ReportAllocs()  // CRITICAL: Enable allocation reporting

    // Setup: pre-allocate everything outside the benchmark loop
    interceptor := setupTestInterceptor()
    packet := generateTestPacket()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        interceptor.processRTP(packet, 0x12345678)
    }
}

// Run with: go test -bench=BenchmarkProcessRTP_Allocations -benchmem
// Target output: 0 allocs/op (or <1 average over multiple packets)
```

### Pattern 2: Escape Analysis Verification

**What:** Compile-time check for heap escapes.
**When to use:** Identifying allocation sources for optimization.
**Example:**
```bash
# Source: Go build toolchain
go build -gcflags="-m" ./pkg/bwe/interceptor 2>&1 | grep "escapes to heap"

# Look for:
# - "moved to heap" - variable escapes
# - "escapes to heap" - argument escapes
# - Interface conversions in hot paths
```

### Pattern 3: Differential Profiling

**What:** Compare heap profiles before/after optimization.
**When to use:** Measuring optimization impact.
**Example:**
```go
// Source: runtime/pprof documentation
import "runtime/pprof"

func TestOptimizationImpact(t *testing.T) {
    // Baseline profile
    f1, _ := os.Create("heap.before.pprof")
    pprof.WriteHeapProfile(f1)
    f1.Close()

    // ... run workload ...

    // After profile
    f2, _ := os.Create("heap.after.pprof")
    pprof.WriteHeapProfile(f2)
    f2.Close()
}

// Compare with: go tool pprof --base heap.before.pprof heap.after.pprof
```

### Pattern 4: Packet Trace Replay for Reference Comparison

**What:** Record packet metadata, replay through both implementations, compare estimates.
**When to use:** VALID-01 - bandwidth estimate divergence <10% from libwebrtc.
**Example:**
```go
// Source: Derived from project testutil/traces.go pattern
type PacketTrace struct {
    Packets []TracedPacket
}

type TracedPacket struct {
    ArrivalTimeUs int64   // Microseconds since trace start
    SendTime      uint32  // abs-send-time 24-bit
    Size          int
    SSRC          uint32
}

func (t *PacketTrace) Replay(estimator *bwe.BandwidthEstimator, clock *internal.MockClock) []int64 {
    estimates := make([]int64, len(t.Packets))
    start := clock.Now()

    for i, pkt := range t.Packets {
        // Advance clock to match trace timing
        targetTime := start.Add(time.Duration(pkt.ArrivalTimeUs) * time.Microsecond)
        clock.Set(targetTime)

        estimates[i] = estimator.OnPacket(bwe.PacketInfo{
            ArrivalTime: clock.Now(),
            SendTime:    pkt.SendTime,
            Size:        pkt.Size,
            SSRC:        pkt.SSRC,
        })
    }
    return estimates
}
```

### Pattern 5: Chrome Interop Testing Setup

**What:** Automated Chrome testing with webrtc-internals monitoring.
**When to use:** VALID-02 - verify REMB acceptance.
**Example:**
```go
// Source: Pion WebRTC examples pattern + testrtc.com documentation
func TestChromeREMBAcceptance(t *testing.T) {
    if testing.Short() {
        t.Skip("Chrome interop test requires browser")
    }

    // Start signaling server
    // Start Chrome with: chrome --use-fake-ui-for-media-stream --use-fake-device-for-media-stream
    // Connect to chrome://webrtc-internals before starting call
    // Verify "remb" appears in inbound-rtp stats
}
```

### Pattern 6: TCP Fairness Testing

**What:** Run BWE alongside competing TCP traffic, verify no starvation.
**When to use:** VALID-03 - correct behavior with TCP competition.
**Example:**
```bash
# Source: C3Lab WebRTC Testbed methodology
# Terminal 1: Start iperf3 server
iperf3 -s

# Terminal 2: Start BWE test, then add TCP competition
# Test should show BWE adapts to fair share (~50% bandwidth)
# Not starved (<10%) and not hogging (>90%)

# Terminal 3: Add TCP traffic mid-test
sleep 10 && iperf3 -c localhost -t 60
```

### Pattern 7: Long-Duration Soak Test

**What:** 24-hour test with memory/timestamp monitoring.
**When to use:** VALID-04 - no timestamp failures or memory leaks.
**Example:**
```go
// Source: Go pprof best practices + soak testing methodology
func TestSoak24Hour(t *testing.T) {
    if testing.Short() {
        t.Skip("24-hour soak test")
    }

    // Enable HTTP pprof endpoint
    go func() {
        http.ListenAndServe(":6060", nil)
    }()

    clock := internal.NewMockClock(time.Now())
    estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), clock)

    // Simulate 24 hours of traffic
    duration := 24 * time.Hour
    packetInterval := time.Millisecond

    var memStats runtime.MemStats
    checkInterval := time.Hour
    lastCheck := clock.Now()

    for elapsed := time.Duration(0); elapsed < duration; elapsed += packetInterval {
        // Process packet
        estimator.OnPacket(generatePacket(clock.Now(), elapsed))
        clock.Advance(packetInterval)

        // Periodic health checks
        if clock.Now().Sub(lastCheck) >= checkInterval {
            runtime.ReadMemStats(&memStats)
            t.Logf("Hour %d: HeapAlloc=%d, NumGC=%d, estimate=%d",
                int(elapsed/time.Hour), memStats.HeapAlloc, memStats.NumGC, estimator.GetEstimate())
            lastCheck = clock.Now()

            // Check for unbounded growth
            if memStats.HeapAlloc > 100*1024*1024 { // 100MB threshold
                t.Fatal("Memory leak detected: heap > 100MB")
            }
        }
    }
}
```

### Anti-Patterns to Avoid

- **Benchmarking with allocations in setup:** Move all allocations outside `b.ResetTimer()`
- **Testing only happy path:** Must test timestamp wraparound (6h audio, 13h video)
- **Ignoring interface{} escapes:** fmt.Printf in hot paths causes heap allocations
- **Short soak tests:** 1-hour tests miss 24-hour issues (wraparound, slow leaks)
- **Testing on localhost only:** Must test with realistic network impairment

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Benchmark statistics | Manual average calculation | `benchstat` tool | Handles variance, outliers, comparison |
| Network impairment | Artificial delays in code | `tc netem` / pfctl | Realistic network behavior |
| Memory profiling | Print statements | `runtime/pprof` | Industry standard, visual tools |
| REMB verification | Manual byte inspection | Wireshark + pion/rtcp.Unmarshal | Protocol-aware parsing |
| Reference comparison | Manual Chrome testing | Packet trace replay | Reproducible, automatable |

**Key insight:** The validation requirements need reproducible, automated tests. Manual testing with Chrome is useful for initial validation but doesn't scale for CI/CD or regression testing.

## Common Pitfalls

### Pitfall 1: Benchmarking the Wrong Thing

**What goes wrong:** Benchmark shows 0 allocs but production still allocates.
**Why it happens:** Benchmark doesn't exercise the full hot path, or compiler optimizes away dead code.
**How to avoid:**
- Use `b.N` iterations, not fixed count
- Ensure result is used (assign to package-level variable)
- Profile actual workload, not just microbenchmarks
**Warning signs:** Benchmark allocs differ from pprof allocs.

### Pitfall 2: sync.Pool Returning Dirty Objects

**What goes wrong:** Pool returns object with stale data from previous use.
**Why it happens:** Put() doesn't reset fields, or Pool.New doesn't initialize properly.
**How to avoid:**
- Always reset all fields in putPacketInfo() (already done in codebase)
- Pool.New returns zero-value struct (already done)
- Test that Get() returns clean objects
**Warning signs:** Intermittent test failures with "impossible" values.

### Pitfall 3: Interface Conversions in Hot Path

**What goes wrong:** Passing values to interface{} parameters causes heap allocation.
**Why it happens:** Go must box the value to store in interface.
**How to avoid:**
- Remove fmt.Printf/Sprintf from hot paths
- Use concrete types, not interfaces, in packet processing
- Check escape analysis output for "escapes to heap"
**Warning signs:** Unexplained allocs in benchmarks, escape analysis shows interface escapes.

### Pitfall 4: Reference Comparison Without Time Alignment

**What goes wrong:** Implementation matches reference at T=0, diverges over time.
**Why it happens:** Timing drift accumulates, making comparison invalid.
**How to avoid:**
- Use deterministic mock clock for both implementations
- Align packet arrival times exactly
- Compare at specific packet counts, not wall-clock times
**Warning signs:** Early estimates match, late estimates diverge significantly.

### Pitfall 5: Chrome REMB Not Visible in webrtc-internals

**What goes wrong:** REMB packets sent but Chrome doesn't acknowledge them.
**Why it happens:** Multiple possible causes:
- REMB packet malformed
- Wrong SSRC in REMB
- Chrome not sending media (receive-only)
- webrtc-internals opened after call started
**How to avoid:**
- Open webrtc-internals BEFORE starting the call
- Verify REMB packet structure with Wireshark
- Ensure Chrome is sending media (use fake device)
- Check "remb" in inbound-rtp stats
**Warning signs:** No "remb" entries, "bytesReceived" not changing.

### Pitfall 6: TCP Fairness Test Too Short

**What goes wrong:** Test shows fairness but long calls get starved.
**Why it happens:** GCC's adaptive threshold takes time to converge with TCP.
**How to avoid:**
- Run TCP fairness tests for at least 5 minutes
- Observe bandwidth over time, not just final value
- Verify recovery after TCP stops
**Warning signs:** Initial fairness, gradual starvation over minutes.

### Pitfall 7: Soak Test Without Timestamp Wraparound

**What goes wrong:** 24-hour test passes but fails at hour 6 or 13.
**Why it happens:** RTP timestamp wraps at different rates (48kHz: ~6h, 90kHz: ~13h).
**How to avoid:**
- Use accelerated time to force wraparound during test
- Include explicit wraparound unit tests
- Monitor for estimate spikes at wraparound boundaries
**Warning signs:** Sudden bandwidth drops at 6h or 13h marks.

## Code Examples

Verified patterns from official sources and project context:

### Allocation Benchmark with Verification

```go
// Source: Go testing documentation + project patterns
var benchResult int64 // Package-level to prevent optimization

func BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc(b *testing.B) {
    b.ReportAllocs()

    config := bwe.DefaultBandwidthEstimatorConfig()
    clock := internal.NewMockClock(time.Now())
    estimator := bwe.NewBandwidthEstimator(config, clock)

    // Pre-create packet outside benchmark loop
    sendTime := uint32(0)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        pkt := bwe.PacketInfo{
            ArrivalTime: clock.Now(),
            SendTime:    sendTime,
            Size:        1200,
            SSRC:        0x12345678,
        }
        benchResult = estimator.OnPacket(pkt)
        sendTime += 262 // ~1ms in abs-send-time
        clock.Advance(time.Millisecond)
    }
}

// Verify: go test -bench=ZeroAlloc -benchmem
// Expected: 0 allocs/op
```

### HTTP pprof for Soak Test Monitoring

```go
// Source: net/http/pprof documentation
import (
    "net/http"
    _ "net/http/pprof"
)

func setupProfilingEndpoint() {
    go func() {
        // Access at: http://localhost:6060/debug/pprof/
        // Heap: http://localhost:6060/debug/pprof/heap
        // Allocs: http://localhost:6060/debug/pprof/allocs
        http.ListenAndServe(":6060", nil)
    }()
}

// During soak test, periodically fetch:
// curl http://localhost:6060/debug/pprof/heap > heap.$(date +%H%M).pprof
// Compare: go tool pprof --base heap.0000.pprof heap.0600.pprof
```

### Reference Estimate Comparison

```go
// Source: Project testutil/traces.go + validation best practices
func TestEstimateDivergence(t *testing.T) {
    // Load reference trace with expected estimates
    trace := testutil.LoadTrace("testdata/reference_trace.json")

    // Run through our implementation
    clock := internal.NewMockClock(trace.StartTime)
    estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), clock)

    var maxDivergence float64
    for i, pkt := range trace.Packets {
        clock.Set(pkt.ArrivalTime)
        ourEstimate := estimator.OnPacket(bwe.PacketInfo{
            ArrivalTime: pkt.ArrivalTime,
            SendTime:    pkt.SendTime,
            Size:        pkt.Size,
            SSRC:        pkt.SSRC,
        })

        // Compare with reference (skip initial warmup)
        if i > 100 && pkt.ReferenceEstimate > 0 {
            divergence := math.Abs(float64(ourEstimate-pkt.ReferenceEstimate)) / float64(pkt.ReferenceEstimate)
            if divergence > maxDivergence {
                maxDivergence = divergence
            }
        }
    }

    // VALID-01: <10% divergence
    assert.Less(t, maxDivergence, 0.10, "Estimate divergence should be <10%%")
    t.Logf("Max divergence: %.2f%%", maxDivergence*100)
}
```

### TCP Fairness Test Harness

```go
// Source: C3Lab WebRTC Testbed methodology + project patterns
func TestTCPFairness(t *testing.T) {
    if testing.Short() {
        t.Skip("TCP fairness test requires network setup")
    }

    // Phase 1: BWE alone (30s) - should use most bandwidth
    // Phase 2: BWE + TCP (60s) - should reach fair share
    // Phase 3: BWE alone (30s) - should recover

    clock := internal.NewMockClock(time.Now())
    estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), clock)

    targetBandwidth := int64(2_000_000) // 2 Mbps total link

    // Phase 1: No competition
    phase1Estimate := simulateTraffic(estimator, clock, 30*time.Second, targetBandwidth, false)
    t.Logf("Phase 1 (alone): estimate=%d bps", phase1Estimate)

    // Phase 2: TCP competition (simulated via congestion)
    phase2Estimate := simulateTraffic(estimator, clock, 60*time.Second, targetBandwidth/2, true)
    t.Logf("Phase 2 (with TCP): estimate=%d bps", phase2Estimate)

    // Phase 3: Recovery
    phase3Estimate := simulateTraffic(estimator, clock, 30*time.Second, targetBandwidth, false)
    t.Logf("Phase 3 (recovery): estimate=%d bps", phase3Estimate)

    // VALID-03: No starvation (>10% fair share), appropriate backoff (<90%)
    fairShare := targetBandwidth / 2
    assert.Greater(t, phase2Estimate, fairShare/10, "Should not be starved (<10%% fair share)")
    assert.Less(t, phase2Estimate, targetBandwidth*9/10, "Should back off for TCP")
    assert.Greater(t, phase3Estimate, phase2Estimate, "Should recover after TCP stops")
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Static overuse threshold | Adaptive threshold (K_u/K_d) | GCC 2017 revision | Critical for TCP fairness |
| Manual Chrome testing | Automated packet trace replay | Continuous | Enables CI/CD validation |
| Short benchmarks | Soak tests with pprof | Standard practice | Catches long-term issues |
| REMB-only feedback | TWCC preferred (but REMB still needed) | WebRTC ~2018 | This project targets REMB for interop |

**Deprecated/outdated:**
- **Old RTC event log format:** Google rolled out new format; use `--force-fieldtrials=WebRTC-RtcEventLogNewFormat/Disabled/` for old format
- **Static GCC threshold:** Always use adaptive threshold with K_u = 0.01, K_d = 0.00018

## Open Questions

Things that couldn't be fully resolved:

1. **Reference libwebrtc Estimates**
   - What we know: Need to compare against libwebrtc for VALID-01
   - What's unclear: Best method to extract reference estimates (RTC event log vs live testing)
   - Recommendation: Use Chrome RTC event logs with Philipp Hancke's web visualizer to extract estimates, OR build a libwebrtc test harness that logs estimates

2. **Exact Allocation Sources**
   - What we know: sync.Pool already used for PacketInfo
   - What's unclear: Remaining allocation sources in hot path
   - Recommendation: Profile with `go build -gcflags="-m"` and `go test -bench -memprofile` to identify escapes

3. **TCP Traffic Generation Method**
   - What we know: Need competing TCP for VALID-03
   - What's unclear: Whether to use external iperf3 or Go-based TCP client
   - Recommendation: External iperf3 is more realistic; Go-based is more portable for CI

4. **Soak Test Duration vs CI Time**
   - What we know: 24-hour soak test required (VALID-04)
   - What's unclear: How to fit in CI pipeline
   - Recommendation: Use accelerated mock clock for CI (simulates 24h in minutes), run real 24h test as nightly job

## Sources

### Primary (HIGH confidence)
- [Go testing package](https://pkg.go.dev/testing) - Benchmark infrastructure, ReportAllocs, AllocsPerOp
- [runtime/pprof](https://pkg.go.dev/runtime/pprof) - Memory profiling types and methods
- [Go escape analysis](https://cristiancurteanu.com/why-your-go-code-is-slower-than-it-should-be-a-deep-dive-into-heap-allocations/) - gcflags="-m" usage
- [Pion WebRTC benchmark tools](https://github.com/pion/webrtc-bench) - Reference for WebRTC benchmarking patterns
- [C3Lab GCC Analysis](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf) - TCP fairness testing methodology
- [chrome://webrtc-internals documentation](https://testrtc.com/webrtc-internals-documentation/) - REMB validation in Chrome
- [RTC event log visualizer](https://webrtc.googlesource.com/src/+/lkgr/logging/g3doc/rtc_event_log.md) - Reference estimate extraction

### Secondary (MEDIUM confidence)
- [Datadog Go memory leaks](https://www.datadoghq.com/blog/go-memory-leaks/) - Soak test patterns
- [Soak testing guide 2025](https://www.devzery.com/post/soak-testing-complete-guide-2025) - Duration and metric recommendations
- [webrtcHacks GCC Probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/) - Bandwidth estimation debugging

### Tertiary (LOW confidence)
- WebSearch results for Go allocation optimization patterns - general guidance, verify with profiling

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Official Go packages and established tools
- Architecture patterns: HIGH - Derived from Go documentation and project conventions
- Pitfalls: HIGH - Based on project's existing PITFALLS.md and GCC literature
- Code examples: MEDIUM - Patterns from documentation, need verification with actual implementation

**Research date:** 2026-01-22
**Valid until:** ~30 days (Go testing infrastructure is stable; validation methodology is established)
