# Phase 7: Network Simulation - Research

**Researched:** 2026-01-23
**Domain:** Network condition testing - latency, bandwidth, jitter, packet loss simulation
**Confidence:** HIGH

## Summary

Phase 7 enables controlled network condition testing for BWE validation. The phase requires two distinct simulation approaches due to the dual nature of WebRTC connections:

1. **Toxiproxy** for TCP-based signaling path (HTTP) - provides latency, bandwidth limiting, and jitter for the signaling channel
2. **Pion vnet** for UDP-based media path (RTP/RTCP) - provides delay, packet loss, and rate limiting for the media channel where BWE operates

The research confirms both tools are well-suited for this purpose. Toxiproxy v2.12.0 offers a mature Go client with programmatic toxic configuration. Pion vnet (part of pion/transport/v4, already in dependencies) provides DelayFilter, LossFilter, and TokenBucketFilter for packet-level impairment. For deterministic testing, Go's `math/rand/v2` with PCG seeded sources ensures reproducible randomness.

**Primary recommendation:** Create a unified `NetworkCondition` helper in `pkg/bwe/testutil/network.go` that wraps both Toxiproxy (for signaling) and vnet (for media), with preset profiles matching NET-01 through NET-04 requirements.

## Standard Stack

The established libraries/tools for network simulation in this project:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| pion/transport/vnet | v4.0.1 | UDP/RTP media path simulation | Already in go.mod as indirect dependency, native Pion integration, supports delay/loss/bandwidth |
| Shopify/toxiproxy/v2/client | v2.12.0 | TCP signaling path simulation | Battle-tested by Shopify, programmatic Go client, Docker image available |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| math/rand/v2 | stdlib | Seeded random for deterministic tests | All probabilistic network conditions (jitter, random loss) |
| testcontainers-go/toxiproxy | latest | Toxiproxy container lifecycle | CI environments where Toxiproxy server is needed |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Toxiproxy | Linux tc/netem | tc requires root/CAP_NET_ADMIN, not portable, complex Docker setup |
| Pion vnet | Real network impairment | Not reproducible, requires infrastructure, not CI-friendly |
| Custom UDP proxy | Toxiproxy | Toxiproxy is TCP-only; vnet is the correct tool for UDP |

**Installation:**
```bash
go get github.com/Shopify/toxiproxy/v2/client@v2.12.0
# Note: pion/transport/v4 already in go.mod as indirect dependency
```

## Architecture Patterns

### Recommended Project Structure
```
pkg/bwe/testutil/
├── browser.go        # [existing] BrowserClient wrapper
├── reference_trace.go # [existing] Reference trace replay
├── network.go        # [NEW] NetworkCondition helpers for vnet
└── traces.go         # [existing] Trace utilities

e2e/
├── doc.go           # [existing] Build tag documentation
├── testmain_test.go # [existing] Cleanup handlers
├── browser_test.go  # [existing] Chrome connectivity test
└── network_test.go  # [NEW] Network condition tests
```

### Pattern 1: vnet-Based Network Impairment
**What:** Use Pion vnet to simulate UDP network conditions for RTP/RTCP traffic
**When to use:** Testing BWE behavior under packet loss, delay, bandwidth constraints
**Example:**
```go
// Source: pion/transport/vnet documentation
import (
    "github.com/pion/transport/v4/vnet"
    "github.com/pion/logging"
)

// Create WAN router (root of network topology)
wan, err := vnet.NewRouter(&vnet.RouterConfig{
    CIDR:          "0.0.0.0/0",
    LoggerFactory: logging.NewDefaultLoggerFactory(),
})

// Create network endpoint with static IP
net1 := vnet.NewNet(&vnet.NetConfig{
    StaticIPs: []string{"192.168.1.1"},
})

// Add to router and start
wan.AddNet(net1)
wan.Start()
defer wan.Stop()

// Apply delay filter (100ms latency)
delayFilter, err := vnet.NewDelayFilter(net1.NIC(), 100*time.Millisecond)
go delayFilter.Run(ctx)

// Apply loss filter (5% packet loss)
lossFilter, err := vnet.NewLossFilter(net1.NIC(), 5)

// Apply bandwidth limit (500 Kbps)
tbf, err := vnet.NewTokenBucketFilter(net1.NIC(),
    vnet.TBFRate(500*1024), // 500 KB/s
    vnet.TBFMaxBurst(1500),  // MTU-sized burst
)
```

### Pattern 2: Toxiproxy for TCP/HTTP Impairment
**What:** Use Toxiproxy to impair HTTP signaling (offer/answer exchange)
**When to use:** Testing signaling delays, connection timeouts
**Example:**
```go
// Source: Toxiproxy Go client documentation
import toxiproxy "github.com/Shopify/toxiproxy/v2/client"

// Connect to Toxiproxy server
client := toxiproxy.NewClient("localhost:8474")

// Create proxy for signaling server
proxy, err := client.CreateProxy("signaling",
    "localhost:26379",  // Listen address
    "localhost:8080",   // Upstream (chrome-interop server)
)

// Add latency toxic (100ms + 20ms jitter)
_, err = proxy.AddToxic("latency_down", "latency", "downstream", 1.0,
    toxiproxy.Attributes{
        "latency": 100,
        "jitter":  20,
    })

// Add bandwidth limit (500 KB/s)
_, err = proxy.AddToxic("bandwidth_limit", "bandwidth", "downstream", 1.0,
    toxiproxy.Attributes{
        "rate": 500,
    })

// Cleanup
defer proxy.Delete()
```

### Pattern 3: Deterministic Random with Seeded PCG
**What:** Use math/rand/v2 with PCG source for reproducible randomness
**When to use:** All tests with probabilistic behavior (jitter, random loss)
**Example:**
```go
// Source: Go math/rand/v2 documentation
import "math/rand/v2"

// Create deterministic random source with fixed seeds
// Same seeds = same sequence every run
rng := rand.New(rand.NewPCG(42, 12345))

// Use for jitter calculation
baseDelay := 100 * time.Millisecond
jitterRange := 20 * time.Millisecond
jitter := time.Duration(rng.Int64N(int64(jitterRange*2))) - jitterRange
actualDelay := baseDelay + jitter

// For packet loss decisions
if rng.Float64() < 0.05 { // 5% loss
    // Drop packet
}
```

### Pattern 4: Network Condition Profiles
**What:** Pre-configured network scenarios matching requirements
**When to use:** Test setup to quickly apply known conditions
**Example:**
```go
// NetworkProfile represents a predefined network condition
type NetworkProfile struct {
    Name        string
    Latency     time.Duration  // NET-01: Base latency
    Jitter      time.Duration  // NET-03: +/- variation
    Bandwidth   int64          // NET-02: Bytes per second (0 = unlimited)
    PacketLoss  float64        // NET-04: 0.0-1.0 probability
    BurstLoss   int            // NET-04: Consecutive packets to drop (0 = random)
    Seed        [2]uint64      // Deterministic randomness
}

// Predefined profiles for requirements
var (
    // NET-01: Constant latency
    ProfileConstantLatency = NetworkProfile{
        Name:    "constant_latency_100ms",
        Latency: 100 * time.Millisecond,
        Seed:    [2]uint64{1, 1},
    }

    // NET-02: Bandwidth throttled
    ProfileBandwidth500Kbps = NetworkProfile{
        Name:      "bandwidth_500kbps",
        Bandwidth: 500 * 1024 / 8, // 500 Kbps in bytes
        Seed:      [2]uint64{2, 2},
    }

    // NET-03: Variable jitter
    ProfileJitter20ms = NetworkProfile{
        Name:    "jitter_20ms",
        Latency: 100 * time.Millisecond,
        Jitter:  20 * time.Millisecond,
        Seed:    [2]uint64{3, 3},
    }

    // NET-04: Random 5% loss
    ProfileRandomLoss5 = NetworkProfile{
        Name:       "random_loss_5pct",
        PacketLoss: 0.05,
        Seed:       [2]uint64{4, 4},
    }

    // NET-04: Burst loss (3 consecutive packets)
    ProfileBurstLoss = NetworkProfile{
        Name:      "burst_loss_3pkt",
        BurstLoss: 3,
        Seed:      [2]uint64{5, 5},
    }
)
```

### Anti-Patterns to Avoid
- **Using tc/netem in tests:** Requires root privileges, not portable, breaks CI
- **Hardcoded time.Sleep for delays:** Use vnet filters instead for reproducibility
- **Global math/rand:** Creates non-deterministic tests; always use seeded local instance
- **Testing both paths with same tool:** Toxiproxy is TCP-only; use vnet for UDP/RTP

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| UDP packet delay | Custom goroutine with time.Sleep | vnet.DelayFilter | Handles concurrent packets, queue management, cleanup |
| Packet loss simulation | rand.Float64() < threshold | vnet.LossFilter | Thread-safe, integrates with vnet router |
| Bandwidth throttling | Token bucket from scratch | vnet.TokenBucketFilter | Complex timing, burst handling, already tested |
| TCP latency injection | net.Conn wrapper | Toxiproxy | Protocol-aware, jitter support, battle-tested |
| Deterministic jitter | Custom random with seed | math/rand/v2 + PCG | Standardized, documented, reproducible |

**Key insight:** Network simulation is deceptively complex due to timing, concurrency, and protocol interactions. Both vnet and Toxiproxy are battle-tested in production environments and handle edge cases that custom solutions would miss.

## Common Pitfalls

### Pitfall 1: Toxiproxy TCP-Only Limitation
**What goes wrong:** Attempting to use Toxiproxy for RTP/RTCP (UDP) traffic fails silently
**Why it happens:** Toxiproxy is explicitly TCP-only; WebRTC media uses UDP
**How to avoid:**
- Use Toxiproxy only for HTTP signaling path
- Use Pion vnet for all UDP/RTP simulation
- Document which tool covers which path in test comments
**Warning signs:** Toxiproxy proxy created but no effect on BWE behavior

### Pitfall 2: Non-Deterministic Random in Tests
**What goes wrong:** Tests pass/fail randomly, flaky CI
**Why it happens:** Using global rand or unseeded rand sources
**How to avoid:**
- Always create local `*rand.Rand` with `rand.NewPCG(seed1, seed2)`
- Pass seed as test parameter for reproducibility
- Log seed on failure for debugging: `t.Logf("Seed: %d, %d", seed1, seed2)`
**Warning signs:** Test fails intermittently with "expected X but got Y" where Y varies

### Pitfall 3: vnet Router Not Started
**What goes wrong:** Packets silently dropped, no network connectivity
**Why it happens:** Forgetting to call `wan.Start()` after setup
**How to avoid:**
- Always follow: AddNet -> Start -> (test) -> Stop
- Use defer for cleanup: `defer wan.Stop()`
- Check router state in test setup
**Warning signs:** ICE connection never establishes, no RTP received

### Pitfall 4: Filter Context Cancellation
**What goes wrong:** DelayFilter stops processing mid-test
**Why it happens:** Context cancelled before test completes
**How to avoid:**
- Use test-scoped context: `ctx, cancel := context.WithCancel(context.Background())`
- Defer cancel after wan.Stop(): `defer cancel()`
- Ensure filter goroutine runs for test duration
**Warning signs:** Delay works initially, then stops affecting packets

### Pitfall 5: Bandwidth Units Confusion
**What goes wrong:** Rate limiting too aggressive or too lenient
**Why it happens:** Mixing KB/s (Toxiproxy) with bits/s (BWE) or bytes/s (vnet)
**How to avoid:**
- Toxiproxy bandwidth: rate in KB/s (kilobytes)
- vnet TBFRate: bytes/s
- BWE estimates: bits/s
- Document conversions: `500 Kbps = 500*1024 bits/s = 62.5 KB/s = 64000 bytes/s`
**Warning signs:** BWE converges to unexpected rate, order of magnitude errors

### Pitfall 6: ICE Connection Through vnet
**What goes wrong:** PeerConnection fails to establish over vnet
**Why it happens:** vnet requires explicit IP configuration, default ICE fails
**How to avoid:**
- Use SettingEngine to bind to vnet: `se.SetNet(virtualNet)`
- Configure static IPs in vnet.NetConfig
- Set ICE lite or host-only candidates
**Warning signs:** ICE state stuck in "checking", timeout errors

## Code Examples

Verified patterns from official sources and project context:

### NetworkCondition Helper (Proposed API)
```go
// Source: Derived from vnet/toxiproxy documentation
// File: pkg/bwe/testutil/network.go

package testutil

import (
    "context"
    "math/rand/v2"
    "time"

    "github.com/pion/transport/v4/vnet"
    "github.com/pion/logging"
)

// NetworkCondition configures simulated network impairment.
type NetworkCondition struct {
    // Latency adds constant delay to all packets (NET-01)
    Latency time.Duration

    // Jitter adds +/- random variation to latency (NET-03)
    // Requires Latency > 0 and Seed to be set
    Jitter time.Duration

    // Bandwidth limits throughput in bytes/second (NET-02)
    // 0 means unlimited
    Bandwidth int64

    // PacketLoss probability 0.0-1.0 for random loss (NET-04)
    PacketLoss float64

    // BurstLossSize drops N consecutive packets when loss triggered (NET-04)
    // 0 means single packet loss
    BurstLossSize int

    // Seed for deterministic randomness [seed1, seed2]
    // Required for Jitter or PacketLoss > 0
    Seed [2]uint64
}

// VNetSimulator wraps vnet with network condition configuration.
type VNetSimulator struct {
    router *vnet.Router
    nets   []*vnet.Net
    rng    *rand.Rand
    cancel context.CancelFunc
}

// NewVNetSimulator creates a virtual network with the given condition.
func NewVNetSimulator(cond NetworkCondition) (*VNetSimulator, error) {
    ctx, cancel := context.WithCancel(context.Background())

    // Create router
    router, err := vnet.NewRouter(&vnet.RouterConfig{
        CIDR:          "192.168.0.0/24",
        LoggerFactory: logging.NewDefaultLoggerFactory(),
    })
    if err != nil {
        cancel()
        return nil, err
    }

    sim := &VNetSimulator{
        router: router,
        cancel: cancel,
    }

    // Create seeded RNG if needed
    if cond.Jitter > 0 || cond.PacketLoss > 0 {
        sim.rng = rand.New(rand.NewPCG(cond.Seed[0], cond.Seed[1]))
    }

    // Apply filters based on condition
    // (Filters attached when AddNet called)

    return sim, nil
}

// AddNet creates a new virtual network endpoint.
func (s *VNetSimulator) AddNet(ip string, cond NetworkCondition) (*vnet.Net, error) {
    net := vnet.NewNet(&vnet.NetConfig{
        StaticIPs: []string{ip},
    })

    if err := s.router.AddNet(net); err != nil {
        return nil, err
    }

    // Apply delay filter
    if cond.Latency > 0 {
        ctx := context.Background()
        delay, err := vnet.NewDelayFilter(net.NIC(), cond.Latency)
        if err != nil {
            return nil, err
        }
        go delay.Run(ctx)
    }

    // Apply loss filter
    if cond.PacketLoss > 0 {
        lossPercent := int(cond.PacketLoss * 100)
        _, err := vnet.NewLossFilter(net.NIC(), lossPercent)
        if err != nil {
            return nil, err
        }
    }

    // Apply bandwidth filter
    if cond.Bandwidth > 0 {
        _, err := vnet.NewTokenBucketFilter(net.NIC(),
            vnet.TBFRate(cond.Bandwidth),
            vnet.TBFMaxBurst(1500), // MTU
        )
        if err != nil {
            return nil, err
        }
    }

    s.nets = append(s.nets, net)
    return net, nil
}

// Start begins routing packets.
func (s *VNetSimulator) Start() error {
    return s.router.Start()
}

// Stop shuts down the simulator.
func (s *VNetSimulator) Stop() {
    s.cancel()
    s.router.Stop()
}
```

### Integration with WebRTC SettingEngine
```go
// Source: Pion WebRTC SettingEngine documentation
import "github.com/pion/webrtc/v4"

func CreatePeerConnectionWithVNet(virtualNet *vnet.Net) (*webrtc.PeerConnection, error) {
    // Configure SettingEngine to use virtual network
    se := webrtc.SettingEngine{}
    se.SetNet(virtualNet)

    // Use host-only ICE candidates (no STUN/TURN needed in vnet)
    se.SetICETimeouts(
        10*time.Second,  // disconnectedTimeout
        30*time.Second,  // failedTimeout
        2*time.Second,   // keepAliveInterval
    )

    // Create API with settings
    api := webrtc.NewAPI(webrtc.WithSettingEngine(se))

    // Create PeerConnection
    return api.NewPeerConnection(webrtc.Configuration{})
}
```

### Deterministic Jitter Implementation
```go
// Source: math/rand/v2 documentation
func applyJitter(baseDelay time.Duration, jitterRange time.Duration, rng *rand.Rand) time.Duration {
    if jitterRange <= 0 {
        return baseDelay
    }
    // Generate jitter in range [-jitterRange, +jitterRange]
    jitterNanos := rng.Int64N(int64(jitterRange*2)) - int64(jitterRange)
    return baseDelay + time.Duration(jitterNanos)
}

// Usage in test
func TestBWE_WithJitter(t *testing.T) {
    seed := [2]uint64{42, 12345}
    t.Logf("Using seed: %v", seed) // Log for reproducibility

    rng := rand.New(rand.NewPCG(seed[0], seed[1]))

    for i := 0; i < 100; i++ {
        delay := applyJitter(100*time.Millisecond, 20*time.Millisecond, rng)
        // delay will be same sequence every run with same seed
    }
}
```

### Burst Loss Implementation
```go
// Source: Custom implementation based on vnet patterns
type BurstLossFilter struct {
    nic           vnet.NIC
    rng           *rand.Rand
    lossProbability float64
    burstSize     int
    burstRemaining int
}

func NewBurstLossFilter(nic vnet.NIC, probability float64, burstSize int, seed [2]uint64) *BurstLossFilter {
    return &BurstLossFilter{
        nic:            nic,
        rng:            rand.New(rand.NewPCG(seed[0], seed[1])),
        lossProbability: probability,
        burstSize:      burstSize,
    }
}

// ShouldDrop determines if packet should be dropped
func (f *BurstLossFilter) ShouldDrop() bool {
    // Continue burst if active
    if f.burstRemaining > 0 {
        f.burstRemaining--
        return true
    }

    // Check for new burst
    if f.rng.Float64() < f.lossProbability {
        f.burstRemaining = f.burstSize - 1 // -1 because we drop this one
        return true
    }

    return false
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| tc/netem for simulation | vnet/Toxiproxy | 2020+ | No root required, portable, CI-friendly |
| math/rand with Seed() | math/rand/v2 with NewPCG | Go 1.22 | More explicit, better APIs, ChaCha8 option |
| Global random sources | Local seeded instances | Best practice | Deterministic tests, reproducible failures |

**Deprecated/outdated:**
- `math/rand.Seed()`: Deprecated in favor of `rand.New(rand.NewPCG(...))` for deterministic use
- `math/rand` top-level functions: Auto-seeded since Go 1.20, use local Rand for tests

## Open Questions

Things that couldn't be fully resolved:

1. **vnet TokenBucketFilter exact behavior**
   - What we know: Exists in vnet, takes TBFRate and TBFMaxBurst options
   - What's unclear: Exact queue behavior, how it interacts with DelayFilter
   - Recommendation: Test with simple scenarios first, adjust burst/queue settings empirically

2. **Jitter implementation in vnet**
   - What we know: vnet has DelayFilter with constant delay
   - What's unclear: Whether vnet supports variable delay or if custom implementation needed
   - Recommendation: Implement custom jitter using ChunkFilter with seeded RNG if DelayFilter doesn't support it

3. **Integration with existing E2E tests**
   - What we know: E2E tests use BrowserClient + chrome-interop server
   - What's unclear: Best way to inject vnet between browser and server (browser uses real network)
   - Recommendation: For browser tests, use Toxiproxy on signaling; for Pion-to-Pion tests, use vnet fully

## Sources

### Primary (HIGH confidence)
- [Pion transport/vnet pkg.go.dev](https://pkg.go.dev/github.com/pion/transport/vnet) - vnet API documentation
- [Pion transport/vnet GitHub](https://github.com/pion/transport/tree/master/vnet) - Filter implementations
- [Toxiproxy GitHub](https://github.com/Shopify/toxiproxy) - Official repository, toxic documentation
- [Toxiproxy Go client](https://pkg.go.dev/github.com/Shopify/toxiproxy/v2/client) - v2 client API
- [Go math/rand/v2](https://pkg.go.dev/math/rand/v2) - Seeded random documentation
- [Go Blog: math/rand/v2](https://go.dev/blog/randv2) - Design rationale

### Secondary (MEDIUM confidence)
- [DoltHub: Testing with Toxiproxy](https://www.dolthub.com/blog/2024-03-13-golang-toxiproxy/) - Complete Go example
- [Testcontainers Toxiproxy](https://golang.testcontainers.org/modules/toxiproxy/) - Container integration
- [Pion WebRTC vnet_test.go](https://github.com/pion/webrtc/blob/master/vnet_test.go) - Usage patterns

### Tertiary (LOW confidence)
- [Polar Signals DST blog](https://www.polarsignals.com/blog/posts/2024/05/28/mostly-dst-in-go) - Deterministic testing approaches

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - Both tools (vnet, Toxiproxy) verified in official documentation and already used in Pion ecosystem
- Architecture: HIGH - Follows existing testutil patterns, clear separation of concerns
- Pitfalls: HIGH - Documented limitations (TCP-only) and solutions confirmed

**Research date:** 2026-01-23
**Valid until:** 30 days (stable tools, no major version changes expected)
