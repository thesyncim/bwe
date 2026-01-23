---
phase: quick
plan: 005
type: execute
wave: 1
depends_on: []
files_modified:
  - README.md
autonomous: true

must_haves:
  truths:
    - "README clearly explains what BWE is and its purpose"
    - "README shows how to install and use the library"
    - "README includes architecture diagram showing component flow"
    - "README demonstrates both standalone and Pion integration usage"
  artifacts:
    - path: "README.md"
      provides: "Project documentation"
      min_lines: 200
  key_links:
    - from: "README.md"
      to: "pkg/bwe/"
      via: "code examples"
      pattern: "bwe\\.NewBandwidthEstimator"
---

<objective>
Create a state-of-the-art README.md for the BWE (Bandwidth Estimation) project.

Purpose: Provide clear documentation for developers wanting to use receiver-side GCC bandwidth estimation in their WebRTC applications, with both standalone core library usage and Pion interceptor integration.

Output: A comprehensive README.md covering installation, architecture, usage examples, API highlights, performance characteristics, and testing.
</objective>

<execution_context>
@~/.claude/get-shit-done/workflows/execute-plan.md
@~/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@go.mod
@pkg/bwe/bandwidth_estimator.go
@pkg/bwe/estimator.go
@pkg/bwe/interceptor/doc.go
@pkg/bwe/interceptor/interceptor.go
@pkg/bwe/benchmark_test.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Create comprehensive README.md</name>
  <files>README.md</files>
  <action>
Create README.md with the following structure:

**Header section:**
- Project name: BWE (Bandwidth Estimation)
- One-line description: Go implementation of Google Congestion Control (GCC) receiver-side bandwidth estimation for WebRTC
- Badges: Go Reference (pkg.go.dev), Go Report Card, License (detect from repo or use MIT placeholder)

**Features section (bullet list):**
- Delay-based estimation with Kalman and Trendline filter options
- Overuse detector with adaptive threshold
- AIMD (Additive Increase Multiplicative Decrease) rate controller
- REMB packet generation and scheduling
- Pion WebRTC interceptor integration
- Zero allocations in hot path (0 allocs/op)
- Tested against real Chrome browser (E2E)

**Architecture section:**
Create ASCII diagram showing the GCC pipeline:
```
RTP Packets → InterArrival Calculator → Delay Filter → Overuse Detector → Rate Controller → REMB
                    (burst grouping)    (Kalman/Trendline) (adaptive threshold)   (AIMD)
```

Explain the two layers:
1. Core library (`pkg/bwe/`) - standalone, no Pion dependencies
2. Pion interceptor (`pkg/bwe/interceptor/`) - integrates with Pion WebRTC

**Installation section:**
```bash
go get bwe
```
Note: Module is `bwe` per go.mod

**Quick Start section:**
Two examples:

1. Standalone Core Library:
```go
import "bwe/pkg/bwe"

// Create estimator with defaults
estimator := bwe.NewBandwidthEstimator(bwe.DefaultBandwidthEstimatorConfig(), nil)

// Process each RTP packet
estimate := estimator.OnPacket(bwe.PacketInfo{
    ArrivalTime: time.Now(),
    SendTime:    sendTimeFromRTPExtension,
    Size:        packetSize,
    SSRC:        ssrc,
})
fmt.Printf("Current estimate: %d bps\n", estimate)
```

2. Pion Interceptor Integration:
```go
import (
    "github.com/pion/interceptor"
    "github.com/pion/webrtc/v4"
    bweint "bwe/pkg/bwe/interceptor"
)

// Create interceptor registry
i := &interceptor.Registry{}

// Add BWE interceptor
factory, err := bweint.NewBWEInterceptorFactory(
    bweint.WithInitialBitrate(500_000),  // 500 kbps
    bweint.WithMinBitrate(100_000),      // 100 kbps
    bweint.WithMaxBitrate(5_000_000),    // 5 Mbps
)
i.Add(factory)

// Create API with interceptors
api := webrtc.NewAPI(
    webrtc.WithMediaEngine(m),
    webrtc.WithInterceptorRegistry(i),
)
```

**Configuration section:**
Show key configuration options:
- BandwidthEstimatorConfig (DelayConfig, RateStatsConfig, RateControllerConfig)
- FilterType options (FilterKalman vs FilterTrendline)
- Rate controller limits (InitialBitrate, MinBitrate, MaxBitrate)
- REMB interval

**Performance section:**
- Zero allocations in steady-state packet processing (0 allocs/op)
- All hot path operations verified via benchmarks
- Run benchmarks: `go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...`

**Testing section:**
```bash
# Unit tests
go test ./pkg/bwe/...

# E2E tests (requires Chrome)
go test -tags=e2e ./e2e/...

# Benchmarks
go test -bench=. -benchmem ./pkg/bwe/...
```

**Requirements section:**
- Go 1.25+
- Sender must include abs-send-time or abs-capture-time RTP header extension
- For Pion integration: pion/webrtc v4, pion/interceptor

**How it works section (brief):**
1. InterArrival Calculator groups packets into bursts, measures delay variation
2. Delay Filter (Kalman or Trendline) smooths noisy delay measurements
3. Overuse Detector detects congestion via adaptive threshold
4. Rate Controller applies AIMD to adjust bandwidth estimate
5. REMB Scheduler sends bandwidth feedback to sender

**License section:**
Check if LICENSE file exists, otherwise use placeholder

**Contributing section:**
Brief note about running tests before PRs
  </action>
  <verify>
- File exists: `test -f README.md`
- Contains key sections: `grep -q "Installation" README.md && grep -q "Quick Start" README.md && grep -q "Architecture" README.md`
- Contains code examples: `grep -q "NewBandwidthEstimator" README.md`
- Contains performance info: `grep -q "0 allocs/op" README.md`
  </verify>
  <done>
README.md exists with:
- Clear project description
- Architecture diagram
- Installation instructions
- Quick start code examples (standalone + Pion)
- Configuration documentation
- Performance characteristics
- Testing instructions
  </done>
</task>

</tasks>

<verification>
1. README.md exists at repository root
2. All major sections present (Features, Architecture, Installation, Quick Start, Configuration, Performance, Testing)
3. Code examples are syntactically correct Go
4. ASCII architecture diagram renders correctly
</verification>

<success_criteria>
- README.md is comprehensive (200+ lines)
- Includes both standalone and Pion integration examples
- Documents performance characteristics (0 allocs/op)
- Provides clear path from installation to usage
</success_criteria>

<output>
After completion, create `.planning/quick/005-add-state-of-art-readme/005-SUMMARY.md`
</output>
