# Architecture Research: GCC Receiver-Side BWE

**Project:** GCC Receiver-Side Bandwidth Estimator
**Researched:** 2026-01-22
**Confidence:** HIGH (based on RFC draft, libwebrtc source structure, Pion interceptor docs)

## Component Overview

The GCC receiver-side bandwidth estimation system consists of five major components arranged in a processing pipeline:

### 1. Arrival Time Filter (Inter-Arrival Module)

**Responsibility:** Compute inter-arrival time deltas and group packets into arrival clusters.

**Input:**
- RTP packet arrival timestamps (local clock)
- Absolute capture timestamps (from RTP header extension)

**Output:**
- Inter-group delay variation: `d(i) = t(i) - t(i-1) - (T(i) - T(i-1))`
- Grouped packet arrivals (packets within 5ms or with negative delay variation are merged)

**Key behaviors:**
- Pre-filters burst arrivals to reduce noise
- Operates on packet groups, not individual packets
- Does NOT require clock synchronization (only deltas matter)

### 2. Delay Gradient Estimator (Kalman Filter / Trendline Estimator)

**Responsibility:** Estimate the queuing delay trend from noisy inter-arrival measurements.

**Input:** Inter-group delay variations from arrival filter

**Output:** Smoothed delay gradient estimate `m(i)` representing congestion level

**Algorithm options:**
- **Kalman filter** (original GCC): Scalar filter with state noise variance q=10^-3
- **Trendline estimator** (modern libwebrtc): Linear regression over sliding window

**Key behaviors:**
- Exponential averaging of measurement noise variance
- Outlier rejection: values exceeding 3*sqrt(variance) are clamped
- Produces tracking error for threshold adaptation

### 3. Overuse Detector (State Machine)

**Responsibility:** Compare delay gradient against adaptive threshold and produce congestion signal.

**Input:**
- Delay gradient `m(i)` from estimator
- Current threshold `del_var_th(i)`

**Output:** Signal enum: `NORMAL`, `OVERUSE`, or `UNDERUSE`

**State machine:**
```
OVERUSE:   m(i) > del_var_th(i) sustained for >= 10ms
UNDERUSE:  m(i) < -del_var_th(i)
NORMAL:    otherwise
```

**Adaptive threshold:**
```
del_var_th(i) = del_var_th(i-1) + delta_t * K(i) * (|m(i)| - del_var_th(i-1))

where:
  K(i) = K_u (0.01)    when |m(i)| > del_var_th(i-1)
  K(i) = K_d (0.00018) when |m(i)| <= del_var_th(i-1)

Initial: 12.5ms, clamped to [6ms, 600ms]
```

### 4. Rate Controller (AIMD)

**Responsibility:** Adjust bandwidth estimate based on detector state using AIMD algorithm.

**Input:**
- Detector signal (OVERUSE/UNDERUSE/NORMAL)
- Measured incoming bitrate `R_hat` (over 0.5-1 second window)

**Output:** Estimated available bandwidth `A_hat` in bits per second

**State machine:**

| State | Condition | Action |
|-------|-----------|--------|
| INCREASE | NORMAL signal | Multiplicative: +8%/sec far from convergence; Additive: +0.5 packet per RTI near convergence |
| DECREASE | OVERUSE signal | `A_hat = 0.85 * R_hat` |
| HOLD | UNDERUSE signal | Maintain current estimate |

**Constraints:**
- `A_hat < 1.5 * R_hat` (prevents estimate diverging from actual rate)
- Response time interval (RTI) = 100ms + RTT

### 5. REMB Generator

**Responsibility:** Package bandwidth estimate into REMB RTCP packet.

**Input:**
- Bandwidth estimate from rate controller
- List of SSRCs to report on

**Output:** REMB RTCP packet bytes

**REMB packet format:**
```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P| FMT=15  |   PT=206      |             length            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                  SSRC of packet sender                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                  SSRC of media source (0)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Unique identifier 'R' 'E' 'M' 'B'                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Num SSRC     | BR Exp    |  BR Mantissa                      |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   SSRC feedback (repeated Num SSRC times)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+

Bitrate = BR_Mantissa * 2^BR_Exp (bits per second)
```

## Data Flow

```
                          STANDALONE CORE LIBRARY
    +--------------------------------------------------------------------+
    |                                                                    |
    |   [RTP Packet + Abs Capture Time]                                  |
    |            |                                                       |
    |            v                                                       |
    |   +------------------+     +------------------------+              |
    |   | 1. Inter-Arrival | --> | 2. Delay Estimator     |              |
    |   |    Filter        |     |    (Kalman/Trendline)  |              |
    |   +------------------+     +------------------------+              |
    |            |                         |                             |
    |            |  packet groups          |  delay gradient m(i)        |
    |            v                         v                             |
    |   +------------------+     +------------------------+              |
    |   | Rate Statistics  |     | 3. Overuse Detector    |              |
    |   | (R_hat)          |     |    (State Machine)     |              |
    |   +------------------+     +------------------------+              |
    |            |                         |                             |
    |            |  incoming bitrate       |  signal (OVER/UNDER/NORMAL) |
    |            v                         v                             |
    |   +------------------------------------------------+               |
    |   | 4. AIMD Rate Controller                        |               |
    |   |    - Increase/Decrease/Hold states             |               |
    |   |    - Produces A_hat (bandwidth estimate)       |               |
    |   +------------------------------------------------+               |
    |            |                                                       |
    |            v  bandwidth estimate                                   |
    |   +------------------+                                             |
    |   | 5. REMB Builder  |                                             |
    |   +------------------+                                             |
    |            |                                                       |
    +------------|-------------------------------------------------------+
                 v  REMB packet bytes

                       PION INTERCEPTOR ADAPTER
    +--------------------------------------------------------------------+
    |                                                                    |
    |   BindRemoteStream() -----------> Core.OnPacket(pkt, arrivalTime)  |
    |                                                                    |
    |   BindRTCPWriter() <------------- Core.GetREMB() -> RTCPWriter     |
    |                                                                    |
    |   Timer goroutine: periodically flush REMB to RTCPWriter           |
    |                                                                    |
    +--------------------------------------------------------------------+
```

## libwebrtc Architecture Reference

### Source Location

```
chromium/src/third_party/webrtc/modules/remote_bitrate_estimator/
```

### Key Files (from BUILD.gn)

| File | Component |
|------|-----------|
| `inter_arrival.h/cc` | Arrival Time Filter |
| `overuse_estimator.h/cc` | Kalman filter for delay gradient |
| `overuse_detector.h/cc` | Threshold comparison state machine |
| `aimd_rate_control.h/cc` | AIMD rate controller |
| `remote_bitrate_estimator_abs_send_time.h/cc` | Main estimator using abs-send-time |
| `remote_bitrate_estimator_single_stream.h/cc` | Per-stream estimation |
| `bwe_defines.h` | Shared constants and enums |
| `remote_bitrate_estimator.h` (include/) | Main interface |

### Class Hierarchy

```
RemoteBitrateEstimator (interface)
    |
    +-- RemoteBitrateEstimatorAbsSendTime
    |       Uses: InterArrival, OveruseEstimator, OveruseDetector, AimdRateControl
    |
    +-- RemoteBitrateEstimatorSingleStream
            Uses: InterArrival, OveruseEstimator, OveruseDetector, AimdRateControl
```

### Key Interface (from remote_bitrate_estimator.h)

```cpp
class RemoteBitrateEstimator : public CallStatsObserver, public Module {
 public:
  enum BandwidthUsage {
    kBwNormal,
    kBwUnderusing,
    kBwOverusing
  };

  // Called for each incoming RTP packet
  virtual void IncomingPacket(int64_t arrival_time_ms,
                              size_t payload_size,
                              const RTPHeader& header) = 0;

  // Remove stream data for SSRC
  virtual void RemoveStream(uint32_t ssrc) = 0;

  // Get latest bandwidth estimate
  virtual bool LatestEstimate(std::vector<uint32_t>* ssrcs,
                              uint32_t* bitrate_bps) const = 0;

  // Set minimum bitrate
  virtual void SetMinBitrate(int min_bitrate_bps) = 0;
};
```

### Modern libwebrtc Notes

Modern libwebrtc has shifted to sender-side estimation using TWCC, with `TrendlineEstimator` (linear regression) replacing the older Kalman filter in `GoogCcNetworkController`. However, the receiver-side components still exist for REMB compatibility.

## Standalone Core vs Interceptor

### Design Principle

The core library should be pure algorithm with no Pion dependencies. This enables:
- Unit testing algorithm logic without WebRTC infrastructure
- Reuse in non-Pion contexts
- Clear API boundary for the interceptor

### Core Library Interface

```go
package gcc

// Config holds estimator configuration
type Config struct {
    MinBitrate      uint64        // Minimum estimate (default: 30kbps)
    MaxBitrate      uint64        // Maximum estimate (default: 10Mbps)
    InitialBitrate  uint64        // Starting estimate (default: 300kbps)
}

// Estimator is the main entry point
type Estimator struct {
    // private fields
}

func NewEstimator(cfg Config) *Estimator

// OnPacket processes an incoming RTP packet
// arrivalTime: local monotonic timestamp in microseconds
// sendTime: absolute capture time from RTP extension (NTP format)
// payloadSize: RTP payload size in bytes
// ssrc: RTP SSRC
func (e *Estimator) OnPacket(arrivalTime int64, sendTime uint64, payloadSize int, ssrc uint32)

// GetEstimate returns current bandwidth estimate in bits per second
func (e *Estimator) GetEstimate() uint64

// GetSSRCs returns list of active SSRCs
func (e *Estimator) GetSSRCs() []uint32

// Reset clears all state
func (e *Estimator) Reset()

// Optional: callback-based notification
type EstimateCallback func(bitrateBps uint64)
func (e *Estimator) OnEstimateChanged(cb EstimateCallback)
```

### Pion Interceptor Adapter

```go
package gcc

import (
    "github.com/pion/interceptor"
    "github.com/pion/rtcp"
)

// InterceptorFactory creates GCC interceptors
type InterceptorFactory struct {
    config Config
}

func NewInterceptorFactory(opts ...Option) (*InterceptorFactory, error)

// Interceptor implements interceptor.Interceptor
type Interceptor struct {
    interceptor.NoOp  // Embed for defaults

    estimator    *Estimator
    rtcpWriter   interceptor.RTCPWriter
    ssrcs        map[uint32]struct{}
    rembInterval time.Duration
}

// BindRemoteStream captures incoming RTP for estimation
func (i *Interceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader

// BindRTCPWriter captures the RTCP output channel
func (i *Interceptor) BindRTCPWriter(writer interceptor.RTCPWriter) interceptor.RTCPWriter

// Close stops the REMB generation goroutine
func (i *Interceptor) Close() error
```

### Absolute Capture Time Parsing

The interceptor needs to parse the abs-capture-time RTP header extension:

```go
// AbsCaptureTime represents the parsed extension
type AbsCaptureTime struct {
    Timestamp   uint64  // NTP timestamp (UQ32.32 format)
    ClockOffset int64   // Optional: estimated clock offset (two's complement)
}

// ParseAbsCaptureTime extracts abs-capture-time from RTP header extensions
// Extension URI: "http://www.webrtc.org/experiments/rtp-hdrext/abs-capture-time"
func ParseAbsCaptureTime(extensions []rtp.Extension, extensionID uint8) (*AbsCaptureTime, bool)
```

## Suggested Build Order

Based on component dependencies, build in this order:

### Phase 1: Foundation (No Dependencies)

**1.1 Types and Constants**
- Define `BandwidthUsage` enum (Normal, Overusing, Underusing)
- Define configuration struct with GCC-compliant defaults
- Define threshold constants (K_u=0.01, K_d=0.00018, initial_threshold=12.5ms)

**1.2 REMB Packet Builder**
- Encode bitrate to mantissa+exponent
- Build REMB packet bytes
- *Why first:* Simple, well-specified, easy to test against Pion's existing RTCP library

### Phase 2: Core Estimation Pipeline

**2.1 Inter-Arrival Filter**
- Track arrival times and send times
- Compute inter-arrival deltas
- Group packets (burst detection)
- *Depends on:* Types

**2.2 Delay Estimator (Kalman Filter)**
- Implement scalar Kalman filter
- Track measurement noise variance
- Produce smoothed delay gradient
- *Depends on:* Types, Inter-Arrival

**2.3 Overuse Detector**
- Compare gradient to adaptive threshold
- Implement threshold adaptation
- State machine for sustained overuse detection
- *Depends on:* Delay Estimator, Types

### Phase 3: Rate Control

**3.1 Rate Statistics**
- Track incoming bitrate over sliding window
- Compute R_hat (measured rate)
- *Depends on:* Types

**3.2 AIMD Rate Controller**
- Implement increase/decrease/hold states
- Integrate detector signals with rate statistics
- Produce final bandwidth estimate
- *Depends on:* Overuse Detector, Rate Statistics

### Phase 4: Integration

**4.1 Estimator Facade**
- Wire all components together
- Expose clean public API
- Handle multi-SSRC aggregation
- *Depends on:* All Phase 2-3 components

**4.2 Abs-Capture-Time Parser**
- Parse RTP header extension
- Handle both 8-byte and 16-byte variants
- *Depends on:* None (can be built in parallel)

### Phase 5: Pion Integration

**5.1 Interceptor Implementation**
- Implement `interceptor.Interceptor` interface
- Bind to remote streams
- Parse abs-capture-time from incoming RTP
- *Depends on:* Estimator Facade, Abs-Capture-Time Parser

**5.2 REMB Sender Goroutine**
- Periodic REMB generation (default: 1 second)
- Bind to RTCPWriter
- Handle shutdown gracefully
- *Depends on:* Interceptor, REMB Builder

### Phase 6: Validation

**6.1 Conformance Testing**
- Capture traces from Chrome/libwebrtc
- Compare estimates under identical packet sequences
- *Depends on:* Full implementation

## Sources

**RFC/Standards:**
- [draft-ietf-rmcat-gcc-02 - Google Congestion Control Algorithm](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02)
- [draft-alvestrand-rmcat-remb - REMB RTCP Message](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb)
- [Absolute Capture Time Extension](https://webrtc.googlesource.com/src/+/refs/heads/main/docs/native-code/rtp-hdrext/abs-capture-time/README.md)

**libwebrtc Documentation:**
- [Remote Bitrate Estimator Tutorial](https://www.fanyamin.com/webrtc/tutorial/build/html/4.code/remote_bitrate_estimator.html)
- [WebRTC GCC Tutorial](https://www.fanyamin.com/webrtc/tutorial/build/html/4.code/webrtc_gcc.html)
- [webrtc-mirror BUILD.gn](https://github.com/pristineio/webrtc-mirror/blob/master/webrtc/modules/remote_bitrate_estimator/BUILD.gn)

**Pion:**
- [pion/interceptor Repository](https://github.com/pion/interceptor)
- [Interceptor Package Documentation](https://pkg.go.dev/github.com/pion/interceptor)
- [Bandwidth Estimator Issue #25](https://github.com/pion/interceptor/issues/25)
