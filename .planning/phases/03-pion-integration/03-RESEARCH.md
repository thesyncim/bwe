# Phase 3: Pion Integration - Research

**Researched:** 2026-01-22
**Domain:** Pion WebRTC Interceptor Framework, RTP Header Extensions, RTCP REMB
**Confidence:** HIGH

## Summary

Phase 3 integrates the existing BandwidthEstimator (from Phase 2) with Pion WebRTC's interceptor framework. The Pion interceptor pattern is well-documented and provides clean hooks for observing RTP traffic and sending RTCP feedback. The core BandwidthEstimator already handles all estimation logic; this phase is purely an adapter layer.

The interceptor must observe incoming RTP packets via `BindRemoteStream`, extract abs-send-time (or abs-capture-time) extension values, feed them to BandwidthEstimator, and send REMB packets via the bound RTCPWriter. The extension IDs are provided via `StreamInfo.RTPHeaderExtensions` from SDP negotiation - no manual ID management needed.

**Primary recommendation:** Implement a thin adapter interceptor that wraps BandwidthEstimator. Use NoOp embedding for interface compliance. Handle stream tracking with sync.Map for concurrent access. Schedule REMB via periodic goroutine using the existing REMBScheduler.

## Standard Stack

The established libraries/tools for this domain:

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/pion/interceptor` | v0.1.43 | Interceptor framework | Official Pion interceptor interface |
| `github.com/pion/rtp` | v1.8+ | RTP packet/header parsing | Standard Pion RTP library |
| `github.com/pion/rtcp` | v1.2.16 | RTCP packet building (already used) | Standard Pion RTCP library |
| `github.com/pion/webrtc/v4` | v4.0+ | PeerConnection integration (for examples) | Official Pion WebRTC |
| `github.com/pion/sdp/v3` | v3.0+ | SDP extension URI constants | Standard SDP library |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `sync` (stdlib) | - | sync.Map, sync.Pool | Thread-safe stream tracking, packet metadata pooling |
| `time` (stdlib) | - | Timers, tickers | REMB scheduling |
| `github.com/pion/logging` | v0.2+ | Pion logging interface | Optional debugging |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| sync.Map for streams | map + sync.RWMutex | sync.Map is optimized for concurrent read-heavy with few writes - perfect for stream tracking |
| time.Ticker for REMB | Manual time.After loops | Ticker is cleaner for periodic operations |
| Embedding NoOp | Implementing all methods | NoOp reduces boilerplate, lets us implement only needed methods |

**Installation:**
```bash
go get github.com/pion/interceptor@v0.1.43
go get github.com/pion/rtp@latest
go get github.com/pion/sdp/v3@latest
```

## Architecture Patterns

### Recommended Project Structure
```
pkg/
├── bwe/                      # Existing core library (Phase 1-2)
│   ├── bandwidth_estimator.go
│   ├── remb.go
│   ├── remb_scheduler.go
│   └── ...
└── bwe/interceptor/          # NEW: Pion integration (Phase 3)
    ├── interceptor.go        # Main BWEInterceptor type
    ├── factory.go            # InterceptorFactory implementation
    ├── stream.go             # Per-stream state tracking
    ├── extension.go          # Extension ID lookup helpers
    └── interceptor_test.go   # Integration tests
```

### Pattern 1: Interceptor with NoOp Embedding

**What:** Embed `interceptor.NoOp` to satisfy the interface, override only the methods we need.
**When to use:** Always for interceptors that don't modify all packet types.
**Example:**
```go
// Source: https://pkg.go.dev/github.com/pion/interceptor
type BWEInterceptor struct {
    interceptor.NoOp  // Satisfies interface, passthrough by default

    estimator     *bwe.BandwidthEstimator
    streams       sync.Map  // SSRC -> *streamState
    rtcpWriter    interceptor.RTCPWriter
    absExtID      uint8     // abs-send-time extension ID (from SDP)
    captureExtID  uint8     // abs-capture-time extension ID (from SDP)

    mu            sync.Mutex
    closed        chan struct{}
    wg            sync.WaitGroup
}

func (i *BWEInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
    // Extract extension IDs from negotiated SDP
    i.updateExtensionIDs(info.RTPHeaderExtensions)

    // Track stream state
    state := &streamState{
        ssrc:       info.SSRC,
        lastPacket: time.Now(),
    }
    i.streams.Store(info.SSRC, state)

    // Return wrapper that observes packets
    return interceptor.RTPReaderFunc(func(b []byte, attrs interceptor.Attributes) (int, interceptor.Attributes, error) {
        n, attrs, err := reader.Read(b, attrs)
        if err == nil && n > 0 {
            i.processRTP(b[:n], info.SSRC, attrs)
        }
        return n, attrs, err
    })
}

func (i *BWEInterceptor) UnbindRemoteStream(info *interceptor.StreamInfo) {
    i.streams.Delete(info.SSRC)
}
```

### Pattern 2: Factory Pattern for PeerConnection Integration

**What:** `InterceptorFactory` creates interceptor instances per PeerConnection.
**When to use:** Required for registering with Pion's interceptor registry.
**Example:**
```go
// Source: https://pkg.go.dev/github.com/pion/interceptor
type BWEInterceptorFactory struct {
    config bwe.BandwidthEstimatorConfig
    opts   []Option
}

func NewBWEInterceptorFactory(opts ...Option) (*BWEInterceptorFactory, error) {
    f := &BWEInterceptorFactory{
        config: bwe.DefaultBandwidthEstimatorConfig(),
    }
    for _, opt := range opts {
        if err := opt(f); err != nil {
            return nil, err
        }
    }
    return f, nil
}

func (f *BWEInterceptorFactory) NewInterceptor(id string) (interceptor.Interceptor, error) {
    i := &BWEInterceptor{
        estimator: bwe.NewBandwidthEstimator(f.config, nil),
        closed:    make(chan struct{}),
    }
    // Apply options
    return i, nil
}
```

### Pattern 3: RTCP Writer Binding with Background Scheduler

**What:** Capture RTCPWriter in BindRTCPWriter, start background goroutine for REMB.
**When to use:** For periodic RTCP feedback (REMB at 1Hz).
**Example:**
```go
// Source: https://github.com/pion/interceptor/blob/master/pkg/report/receiver_interceptor.go
func (i *BWEInterceptor) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter {
    i.mu.Lock()
    i.rtcpWriter = writer
    i.mu.Unlock()

    // Start REMB scheduler goroutine
    i.wg.Add(1)
    go i.rembLoop()

    return writer  // Pass through unchanged
}

func (i *BWEInterceptor) rembLoop() {
    defer i.wg.Done()
    ticker := time.NewTicker(i.rembInterval)
    defer ticker.Stop()

    for {
        select {
        case <-i.closed:
            return
        case now := <-ticker.C:
            i.maybeSendREMB(now)
        }
    }
}

func (i *BWEInterceptor) maybeSendREMB(now time.Time) {
    data, shouldSend, err := i.estimator.MaybeBuildREMB(now)
    if err != nil || !shouldSend {
        return
    }

    i.mu.Lock()
    writer := i.rtcpWriter
    i.mu.Unlock()

    if writer != nil {
        // Note: RTCPWriter.Write takes []rtcp.Packet, but we have raw bytes
        // Need to unmarshal or use a different approach
        pkt, _ := rtcp.Unmarshal(data)
        writer.Write(pkt, nil)
    }
}
```

### Pattern 4: Extension ID Discovery from StreamInfo

**What:** Extract extension IDs from `StreamInfo.RTPHeaderExtensions` populated by SDP negotiation.
**When to use:** In BindRemoteStream to know which extension IDs to parse.
**Example:**
```go
// Source: https://github.com/pion/interceptor/blob/master/streaminfo.go
const (
    AbsSendTimeURI     = "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time"
    AbsCaptureTimeURI  = "http://www.webrtc.org/experiments/rtp-hdrext/abs-capture-time"
)

func (i *BWEInterceptor) updateExtensionIDs(exts []interceptor.RTPHeaderExtension) {
    for _, ext := range exts {
        switch ext.URI {
        case AbsSendTimeURI:
            i.absExtID = uint8(ext.ID)
        case AbsCaptureTimeURI:
            i.captureExtID = uint8(ext.ID)
        }
    }
}
```

### Pattern 5: RTP Packet Processing

**What:** Parse RTP header, extract extension, feed to estimator.
**When to use:** In the RTP reader wrapper from BindRemoteStream.
**Example:**
```go
// Source: https://pkg.go.dev/github.com/pion/rtp
func (i *BWEInterceptor) processRTP(raw []byte, ssrc uint32, attrs interceptor.Attributes) {
    // Get RTP header (may already be parsed in attrs)
    header, err := attrs.GetRTPHeader(raw)
    if err != nil {
        var h rtp.Header
        if _, err := h.Unmarshal(raw); err != nil {
            return
        }
        header = &h
    }

    now := time.Now()  // Use monotonic clock

    // Try abs-send-time first
    var sendTime uint32
    if i.absExtID != 0 {
        if ext := header.GetExtension(i.absExtID); len(ext) >= 3 {
            sendTime, _ = bwe.ParseAbsSendTime(ext)
        }
    }

    // Fallback to abs-capture-time
    if sendTime == 0 && i.captureExtID != 0 {
        if ext := header.GetExtension(i.captureExtID); len(ext) >= 8 {
            captureTime, _ := bwe.ParseAbsCaptureTime(ext)
            // Convert to abs-send-time scale for consistency
            sendTime = uint32((captureTime >> 14) & 0xFFFFFF)
        }
    }

    if sendTime == 0 {
        return  // No timing extension available
    }

    // Update stream state
    if state, ok := i.streams.Load(ssrc); ok {
        s := state.(*streamState)
        s.lastPacket = now
    }

    // Feed to estimator
    pkt := bwe.PacketInfo{
        ArrivalTime: now,
        SendTime:    sendTime,
        Size:        len(raw),
        SSRC:        ssrc,
    }
    i.estimator.OnPacket(pkt)
}
```

### Anti-Patterns to Avoid

- **Blocking RTP path:** Never do heavy computation or blocking I/O in the RTP reader. The reader is called per-packet on the hot path.
- **Manual extension ID management:** Don't hardcode extension IDs. Always get them from StreamInfo.RTPHeaderExtensions.
- **Shared mutable state without sync:** Streams can arrive/depart concurrently. Use sync.Map or proper locking.
- **Not handling Close():** Always stop goroutines and clean up in Close(). Use wait groups to ensure completion.

## Don't Hand-Roll

Problems that look simple but have existing solutions:

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| RTP header parsing | Manual byte parsing | `rtp.Header.Unmarshal()` | Extension handling is complex (one-byte vs two-byte profiles) |
| RTCP REMB building | Manual byte building | Existing `bwe.BuildREMB()` using pion/rtcp | Mantissa/exponent encoding is tricky |
| Extension ID → bytes | Manual offset calculation | `header.GetExtension(id)` | Handles both extension profiles automatically |
| Thread-safe stream map | map + manual locking | `sync.Map` | Optimized for read-heavy concurrent access |
| Periodic scheduling | Manual time tracking | `time.Ticker` | Clean, leak-free periodic operations |

**Key insight:** Pion libraries handle all the complex RTP/RTCP byte-level operations. The interceptor's job is to wire things together, not reimplement parsing.

## Common Pitfalls

### Pitfall 1: Blocking the RTP Pipeline

**What goes wrong:** Interceptor does synchronous I/O or heavy computation in BindRemoteStream reader, causing packet drops.
**Why it happens:** Natural to think "process packet, then continue" but RTP is real-time.
**How to avoid:** Keep RTP path non-blocking. Queue packets for async processing if needed. Estimation math is fast enough to be inline.
**Warning signs:** Increased packet loss, audio/video glitches under load.

### Pitfall 2: Extension ID Race Condition

**What goes wrong:** Extension IDs not set when first packets arrive (BindRemoteStream called after packets start).
**Why it happens:** Assumes extension IDs are available immediately, but SDP negotiation may complete after media starts.
**How to avoid:** Handle zero extension IDs gracefully (skip packet, don't crash). IDs will be populated when StreamInfo arrives.
**Warning signs:** Nil pointer panics, early packets always ignored.

### Pitfall 3: Stream Timeout Without Cleanup

**What goes wrong:** Inactive streams accumulate, causing memory leaks and stale SSRC lists in REMB.
**Why it happens:** UnbindRemoteStream may never be called if connection drops ungracefully.
**How to avoid:** Track `lastPacketTime` per stream, run periodic cleanup goroutine, remove streams inactive >2s.
**Warning signs:** Growing memory over time, REMB packets with stale SSRCs.

### Pitfall 4: REMB Writer Not Bound Yet

**What goes wrong:** REMB send fails silently because rtcpWriter is nil (BindRTCPWriter not called yet).
**Why it happens:** Timing between bind calls is not guaranteed.
**How to avoid:** Check rtcpWriter != nil before sending. Queue/drop REMB if not ready.
**Warning signs:** No REMB packets sent initially, estimation works but sender never receives feedback.

### Pitfall 5: Close() Not Waiting for Goroutines

**What goes wrong:** Test failures, panics on closed channels after Close() returns.
**Why it happens:** Close() returns before background goroutines finish.
**How to avoid:** Use sync.WaitGroup, signal via channel, wait for completion in Close().
**Warning signs:** Race detector failures, test flakiness.

### Pitfall 6: Wrong RTCP Write Interface

**What goes wrong:** Trying to write raw REMB bytes directly, but RTCPWriter expects `[]rtcp.Packet`.
**Why it happens:** Confusion between marshaled bytes and packet types.
**How to avoid:** Either unmarshal REMB bytes to rtcp.Packet, or use rtcp.ReceiverEstimatedMaximumBitrate directly.
**Warning signs:** Type errors at compile time, or packets not being sent.

## Code Examples

Verified patterns from official sources:

### Complete RTP Reader Wrapper

```go
// Source: https://pkg.go.dev/github.com/pion/interceptor
func (i *BWEInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
    // Extract extension IDs
    for _, ext := range info.RTPHeaderExtensions {
        switch ext.URI {
        case "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time":
            atomic.StoreUint32(&i.absExtID, uint32(ext.ID))
        case "http://www.webrtc.org/experiments/rtp-hdrext/abs-capture-time":
            atomic.StoreUint32(&i.captureExtID, uint32(ext.ID))
        }
    }

    // Track stream
    state := &streamState{
        ssrc:       info.SSRC,
        lastPacket: time.Now(),
    }
    i.streams.Store(info.SSRC, state)

    // Return observing reader
    return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
        n, a, err := reader.Read(b, a)
        if err == nil && n > 0 {
            i.observeRTP(b[:n], info.SSRC, a)
        }
        return n, a, err
    })
}
```

### REMB Sending via RTCPWriter

```go
// Source: https://pkg.go.dev/github.com/pion/rtcp
func (i *BWEInterceptor) sendREMB(bitrate uint64, ssrcs []uint32) error {
    i.mu.Lock()
    writer := i.rtcpWriter
    i.mu.Unlock()

    if writer == nil {
        return nil // Not bound yet, skip
    }

    pkt := &rtcp.ReceiverEstimatedMaximumBitrate{
        SenderSSRC: i.senderSSRC,
        Bitrate:    float32(bitrate),
        SSRCs:      ssrcs,
    }

    _, err := writer.Write([]rtcp.Packet{pkt}, nil)
    return err
}
```

### Stream Timeout Cleanup

```go
// Source: Derived from receiver_interceptor.go pattern
func (i *BWEInterceptor) cleanupLoop() {
    defer i.wg.Done()
    ticker := time.NewTicker(time.Second)
    defer ticker.Stop()

    const timeout = 2 * time.Second

    for {
        select {
        case <-i.closed:
            return
        case now := <-ticker.C:
            i.streams.Range(func(key, value any) bool {
                state := value.(*streamState)
                if now.Sub(state.lastPacket) > timeout {
                    i.streams.Delete(key)
                }
                return true
            })
        }
    }
}
```

### Factory with Options Pattern

```go
// Source: https://pkg.go.dev/github.com/pion/interceptor/pkg/gcc (pattern)
type Option func(*BWEInterceptorFactory) error

func WithInitialBitrate(rate int64) Option {
    return func(f *BWEInterceptorFactory) error {
        f.config.RateControllerConfig.InitialBitrate = rate
        return nil
    }
}

func WithREMBInterval(interval time.Duration) Option {
    return func(f *BWEInterceptorFactory) error {
        f.rembInterval = interval
        return nil
    }
}

func NewBWEInterceptorFactory(opts ...Option) (*BWEInterceptorFactory, error) {
    f := &BWEInterceptorFactory{
        config:       bwe.DefaultBandwidthEstimatorConfig(),
        rembInterval: time.Second,
    }
    for _, opt := range opts {
        if err := opt(f); err != nil {
            return nil, err
        }
    }
    return f, nil
}
```

### Integration with PeerConnection

```go
// Source: https://pkg.go.dev/github.com/pion/webrtc/v4
func setupPeerConnection() (*webrtc.PeerConnection, error) {
    // Create media engine with abs-send-time extension
    m := &webrtc.MediaEngine{}
    if err := m.RegisterDefaultCodecs(); err != nil {
        return nil, err
    }
    if err := m.RegisterHeaderExtension(
        webrtc.RTPHeaderExtensionCapability{URI: sdp.ABSSendTimeURI},
        webrtc.RTPCodecTypeVideo,
        webrtc.RTPTransceiverDirectionRecvonly,
    ); err != nil {
        return nil, err
    }

    // Create interceptor registry
    i := &interceptor.Registry{}

    // Register BWE interceptor
    bweFactory, _ := NewBWEInterceptorFactory()
    i.Add(bweFactory)

    // Create API
    api := webrtc.NewAPI(
        webrtc.WithMediaEngine(m),
        webrtc.WithInterceptorRegistry(i),
    )

    return api.NewPeerConnection(webrtc.Configuration{})
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Hardcoded extension IDs | Extension IDs from SDP negotiation | Always | Required for interop |
| Single-stream estimation | Multi-SSRC estimation | Phase 2 | REMB includes all streams |
| Manual REMB bytes | pion/rtcp REMB packet | pion/rtcp | Cleaner, less error-prone |
| Global interceptor state | Per-PeerConnection via Factory | Pion v0.1+ | Required pattern |

**Deprecated/outdated:**
- **Direct RTPWriter.Write():** Use RTCPWriter for RTCP packets (different interface)
- **webrtc/v3:** v4 is current, though v3 patterns still work

## Open Questions

Things that couldn't be fully resolved:

1. **sync.Pool for PacketInfo**
   - What we know: PERF-02 requires sync.Pool for packet metadata
   - What's unclear: Exact allocation pattern in interceptor context
   - Recommendation: Profile first, add Pool if allocations are hot path

2. **SenderSSRC for REMB**
   - What we know: REMB requires a sender SSRC (receiver's SSRC)
   - What's unclear: How to obtain the local PeerConnection's SSRC in interceptor
   - Recommendation: Accept as config option or use 0 (many implementations do)

3. **abs-capture-time → abs-send-time conversion**
   - What we know: abs-capture-time is 64-bit, abs-send-time is 24-bit
   - What's unclear: Exact conversion formula for delay calculation
   - Recommendation: Use abs-capture-time directly for delay if present, convert to duration

## Sources

### Primary (HIGH confidence)
- [Pion Interceptor Package Docs](https://pkg.go.dev/github.com/pion/interceptor) - Interface definitions, patterns
- [Pion RTP Package Docs](https://pkg.go.dev/github.com/pion/rtp) - Header parsing, extension handling
- [Pion SDP Package Docs](https://pkg.go.dev/github.com/pion/sdp/v3) - Extension URI constants
- [interceptor.go source](https://github.com/pion/interceptor/blob/master/interceptor.go) - Interface definition
- [streaminfo.go source](https://github.com/pion/interceptor/blob/master/streaminfo.go) - StreamInfo struct
- [receiver_interceptor.go](https://github.com/pion/interceptor/blob/master/pkg/report/receiver_interceptor.go) - Reference implementation pattern

### Secondary (MEDIUM confidence)
- [GCC Package Docs](https://pkg.go.dev/github.com/pion/interceptor/pkg/gcc) - Pion's send-side GCC implementation
- [mediaengine.go](https://github.com/pion/webrtc/blob/master/mediaengine.go) - Extension registration

### Tertiary (LOW confidence)
- WebSearch results for sync.Pool patterns - general Go patterns, not Pion-specific

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH - All libraries are official Pion packages with stable APIs
- Architecture: HIGH - Patterns derived from Pion's own interceptor implementations
- Pitfalls: HIGH - Common issues documented in Pion issues/discussions

**Research date:** 2026-01-22
**Valid until:** ~30 days (Pion interceptor API is stable)
