# Features Research: GCC Receiver-Side BWE

**Domain:** WebRTC delay-based bandwidth estimation (receiver-side)
**Researched:** 2026-01-22
**Reference:** RFC draft-ietf-rmcat-gcc-02, libwebrtc source

## Executive Summary

A GCC delay-based receiver-side bandwidth estimator consists of interconnected components that observe RTP packets, analyze inter-arrival timing patterns, detect network congestion, and generate REMB feedback. The core pipeline is: **Packet Reception -> Inter-Arrival Analysis -> Kalman/Trendline Filtering -> Overuse Detection -> Rate Control -> REMB Generation**.

Table stakes components are required for Chrome/libwebrtc interoperability. Differentiators improve estimation quality but are not required for basic operation.

---

## Table Stakes

**Must-have components for a working implementation that interoperates with libwebrtc/Chrome.**

### 1. Absolute Send Time (abs-send-time) Parser
| Aspect | Detail |
|--------|--------|
| **What** | Parse RTP header extension containing 24-bit NTP-derived timestamp |
| **Format** | 6.18 fixed-point, ~3.8us resolution, 64s wraparound |
| **Encoding** | `abs_send_time_24 = (ntp_timestamp_64 >> 14) & 0x00ffffff` |
| **Complexity** | Low |
| **Why critical** | Without abs-send-time, no delay measurement is possible. REMB-based BWE requires this extension. |

**Sources:** [abs-send-time spec](https://webrtc.googlesource.com/src/+/main/docs/native-code/rtp-hdrext/abs-send-time/README.md), [WebRTC header extensions](http://www.rtcbits.com/2023/05/webrtc-header-extensions.html)

### 2. Inter-Arrival Time Calculator
| Aspect | Detail |
|--------|--------|
| **What** | Compute delay variation between packet groups |
| **Formula** | `d(i) = (t(i) - t(i-1)) - (T(i) - T(i-1))` where t=receive time, T=send time |
| **Complexity** | Low-Medium |
| **Why critical** | Core signal that feeds the overuse detector. Delay gradient is the primary congestion indicator. |

**Notes:**
- Must handle abs-send-time 64-second wraparound correctly
- Negative propagation delta indicates burst arrival (packets queued then released)

### 3. Packet Group Aggregation (Burst Grouping)
| Aspect | Detail |
|--------|--------|
| **What** | Group packets sent within burst interval for collective analysis |
| **Threshold** | `kBurstDeltaThresholdMs = 5ms` (packets sent <5ms apart = same group) |
| **Max duration** | `kMaxBurstDurationMs = 100ms` |
| **Complexity** | Low-Medium |
| **Why critical** | Reduces noise by treating bursty sends as single measurement unit. Without this, per-packet jitter overwhelms the signal. |

**Sources:** [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02), [inter_arrival.cc](https://github.com/webrtc-uwp/webrtc/blob/master/modules/remote_bitrate_estimator/inter_arrival.cc)

### 4. Arrival-Time Filter (Kalman or Trendline)
| Aspect | Detail |
|--------|--------|
| **What** | Filter noise from delay measurements to estimate true delay gradient m(i) |
| **Options** | Scalar Kalman filter (REMB-GCC) or Trendline estimator (TFB-GCC) |
| **Output** | Smoothed delay gradient estimate |
| **Complexity** | Medium-High |
| **Why critical** | Raw delay measurements are noisy. Filter extracts signal from noise. |

**Kalman Filter Parameters (from draft):**
| Parameter | Value | Description |
|-----------|-------|-------------|
| q | 10^-3 | State noise variance |
| e(0) | 0.1 | Initial error covariance |
| chi | 0.1 - 0.001 | Measurement noise coefficient |

**Kalman Recursion:**
```
z(i) = d(i) - m_hat(i-1)           // Innovation
m_hat(i) = m_hat(i-1) + z(i) * k(i)  // State update
k(i) = (e(i-1) + q) / (var_v + e(i-1) + q)  // Kalman gain
e(i) = (1 - k(i)) * (e(i-1) + q)   // Error covariance update
```

**Sources:** [draft-ietf-rmcat-gcc-02 Section 5](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02), [OveruseEstimator](https://asplingzhang.github.io/webrtc/RBE-in-WebRTC/)

### 5. Overuse Detector
| Aspect | Detail |
|--------|--------|
| **What** | Compare filtered delay gradient against threshold to determine network state |
| **States** | `kBwNormal`, `kBwOverusing`, `kBwUnderusing` |
| **Complexity** | Medium |
| **Why critical** | Translates delay signal into actionable congestion state that drives rate control. |

**Detection Logic:**
- **Overuse:** `m(i) > threshold` for >= 10ms AND `m(i) >= m(i-1)`
- **Underuse:** `m(i) < -threshold`
- **Normal:** Neither condition met

**Threshold Parameters:**
| Parameter | Value | Description |
|-----------|-------|-------------|
| del_var_th(0) | 12.5ms | Initial threshold |
| Threshold range | [6ms, 600ms] | Clamp bounds |
| overuse_time_th | 10ms | Time in overuse before signaling |

**Sources:** [draft-ietf-rmcat-gcc-02 Section 5.3](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02)

### 6. Adaptive Threshold
| Aspect | Detail |
|--------|--------|
| **What** | Dynamically adjust overuse detection threshold based on conditions |
| **Why needed** | Static threshold causes starvation when competing with TCP flows |
| **Complexity** | Medium |
| **Why critical** | Without adaptation, GCC flows get starved by loss-based (TCP) traffic. |

**Update Formula:**
```
del_var_th(i) = del_var_th(i-1) + delta_t * K(i) * (|m(i)| - del_var_th(i-1))
```
Where:
- `K(i) = K_u` if `|m(i)| >= del_var_th(i-1)`, else `K_d`
- `K_u = 0.01` (increase rate)
- `K_d = 0.00018` (decrease rate)

**Skip condition:** Don't update if `|m(i)| - del_var_th(i) > 15`

**Sources:** [GCC Analysis Paper](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf), [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02)

### 7. AIMD Rate Controller
| Aspect | Detail |
|--------|--------|
| **What** | Compute target bitrate using Additive Increase / Multiplicative Decrease |
| **States** | Increase, Hold, Decrease |
| **Complexity** | Medium |
| **Why critical** | Translates overuse signal into actual bandwidth estimate value. |

**Rate Control Logic:**

| State | Condition | Action |
|-------|-----------|--------|
| Decrease | Overuse detected | `A_hat(i) = 0.85 * R_hat(i)` (multiply by beta) |
| Hold | Overuse->Normal | Maintain current rate |
| Increase (mult) | Far from convergence | `A_hat(i) = 1.08 * A_hat(i-1)` (max 8%/sec) |
| Increase (add) | Near convergence | `A_hat(i) = A_hat(i-1) + alpha * packet_size` |

**Convergence Detection:** Near convergence = within 3 standard deviations of previously measured rate during Decrease state

**Constraint:** `A_hat(i) < 1.5 * R_hat(i)` (don't overshoot measured rate)

**Sources:** [draft-ietf-rmcat-gcc-02 Section 5.5](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02), [AIMD principles](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/)

### 8. Incoming Bitrate Measurement
| Aspect | Detail |
|--------|--------|
| **What** | Track actual received bitrate using sliding window |
| **Window** | 0.5-1.0 seconds recommended |
| **Output** | R_hat(i) - measured incoming bitrate |
| **Complexity** | Low |
| **Why critical** | Rate controller needs current throughput to calculate decrease amount and convergence. |

### 9. REMB RTCP Packet Generator
| Aspect | Detail |
|--------|--------|
| **What** | Encode bandwidth estimate as RTCP PSFB message |
| **Format** | PT=206, FMT=15, "REMB" identifier |
| **Complexity** | Low-Medium |
| **Why critical** | This is the output - how receiver communicates estimate to sender. |

**REMB Packet Structure:**
```
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|V=2|P| FMT=15  |   PT=206      |             length            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                  SSRC of packet sender                        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                  SSRC of media source (0)                     |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Unique identifier 'R' 'E' 'M' 'B'                            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|  Num SSRC     | BR Exp    |  BR Mantissa (18 bits)            |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   SSRC feedback (one per stream)                              |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

**Bitrate Encoding:** `bitrate_bps = BR_Mantissa * 2^BR_Exp`

**Sending Frequency:**
- Every 1 second periodically
- Immediately if `A_hat(i) < 0.97 * A_hat(i-1)` (significant decrease)

**Sources:** [draft-alvestrand-rmcat-remb](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb), [REMB implementation](https://walterfan.github.io/webrtc_note/4.code/webrtc_bwe_remb.html)

---

## Differentiators

**Features that improve quality/accuracy but are not required for basic interop.**

### 1. Pre-filtering for Transient Outages
| Aspect | Detail |
|--------|--------|
| **What** | Detect and handle delay spikes from network outages (not congestion) |
| **How** | Identify burst arrivals after outage ends, merge into single measurement |
| **Benefit** | Prevents false overuse detection during/after brief outages |
| **Complexity** | Medium |

**Detection:** Packets with `propagation_delta < 0` and `arrival_delta <= 5ms` arriving within 100ms burst are merged.

**Sources:** [draft-ietf-rmcat-gcc-02 Section 4](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02)

### 2. Exponential Moving Average for Noise Variance
| Aspect | Detail |
|--------|--------|
| **What** | Adaptive measurement noise estimation for Kalman filter |
| **Formula** | `var_v(i) = max(alpha * var_v(i-1) + (1-alpha) * z(i)^2, 1)` |
| **Benefit** | Better filter tuning across different network conditions |
| **Complexity** | Low |

### 3. Convergence State Tracking
| Aspect | Detail |
|--------|--------|
| **What** | Track whether estimate is near or far from actual capacity |
| **How** | Maintain EMA of bitrate measurements during Decrease states |
| **Benefit** | Enables switch between multiplicative and additive increase |
| **Complexity** | Low-Medium |

### 4. Multiple SSRC Support
| Aspect | Detail |
|--------|--------|
| **What** | Single REMB covering multiple incoming streams |
| **Benefit** | Correct behavior for simulcast/SVC scenarios |
| **Complexity** | Low |

### 5. Stream Timeout Handling
| Aspect | Detail |
|--------|--------|
| **What** | Remove stale stream data after timeout |
| **Threshold** | `kStreamTimeOutMs = 2000ms` |
| **Benefit** | Prevents stale estimates from affecting active streams |
| **Complexity** | Low |

### 6. Minimum/Maximum Bitrate Bounds
| Aspect | Detail |
|--------|--------|
| **What** | Configurable floor and ceiling for estimates |
| **Benefit** | Prevents unreasonable estimates (too low = unusable, too high = overload) |
| **Complexity** | Low |

### 7. Hold State Timer
| Aspect | Detail |
|--------|--------|
| **What** | Delay increase after transitioning from overuse to normal |
| **Benefit** | Allows network to stabilize before ramping up again |
| **Complexity** | Low |

### 8. REMB Throttling
| Aspect | Detail |
|--------|--------|
| **What** | Rate-limit REMB messages to avoid feedback overhead |
| **Benefit** | Reduces RTCP bandwidth consumption |
| **Complexity** | Low |

---

## Anti-Features (v1)

**Deliberately excluded from v1 scope. These add complexity without being required for REMB interop.**

### 1. Loss-Based Estimation
| Reason | Loss-based control runs on sender side in libwebrtc. Receiver-side REMB is purely delay-based. Mixing them adds complexity for marginal benefit when interop target is Chrome. |
|--------|-----|

### 2. Transport-Wide Congestion Control (TWCC)
| Reason | TWCC is sender-side BWE. Project goal is receiver-side REMB. Different protocol, different architecture. Pion already has TWCC interceptor. |
|--------|-----|

### 3. Absolute Capture Time Support
| Reason | abs-capture-time is for A/V sync across SFUs, not bandwidth estimation. Not needed for REMB. |
|--------|-----|

### 4. Bandwidth Probing
| Reason | Probing is sender-side behavior. Receiver just observes and estimates. |
|--------|-----|

### 5. Trendline Estimator (sender-side variant)
| Reason | Modern libwebrtc moved to sender-side trendline with TWCC. For receiver-side REMB interop, the Kalman filter approach is more appropriate. Can add trendline as alternative later if needed. |
|--------|-----|

### 6. RemoteEstimate RTCP Extension
| Reason | Google Meet's proprietary extension (dual upper/lower bounds). Not standardized, not needed for basic Chrome interop. |
|--------|-----|

### 7. Network Condition Classifier (ML-based)
| Reason | Meta and others use ML for BWE tuning. Out of scope for v1 reference implementation. |
|--------|-----|

### 8. RTT Estimation
| Reason | Receiver-side delay-based BWE uses one-way delay, not RTT. RTT would be used for sender-side estimation. |
|--------|-----|

---

## Component Dependencies

```
                    +------------------+
                    | RTP Packet Input |
                    +--------+---------+
                             |
                             v
              +------------------------------+
              | abs-send-time Parser (1)     |
              +------------------------------+
                             |
                             v
              +------------------------------+
              | Packet Group Aggregation (3) |
              +------------------------------+
                             |
                             v
              +------------------------------+
              | Inter-Arrival Calculator (2) |
              +------------------------------+
                             |
                             v
              +------------------------------+
              | Arrival-Time Filter (4)      |<---+
              | (Kalman / Trendline)         |    |
              +------------------------------+    |
                             |                    |
                             v                    |
              +------------------------------+    |
              | Adaptive Threshold (6)       |----+
              +------------------------------+
                             |
                             v
              +------------------------------+
              | Overuse Detector (5)         |
              +------------------------------+
                             |
                             v
    +--------------------+   |   +-------------------------+
    | Bitrate Measure (8)|-->+<--| Convergence Tracker (D3)|
    +--------------------+   |   +-------------------------+
                             v
              +------------------------------+
              | AIMD Rate Controller (7)     |
              +------------------------------+
                             |
                             v
              +------------------------------+
              | REMB Generator (9)           |
              +------------------------------+
                             |
                             v
                    +------------------+
                    | RTCP Output      |
                    +------------------+
```

**Dependency Chain:**
1. abs-send-time Parser -> required by Inter-Arrival Calculator
2. Inter-Arrival Calculator -> required by Arrival-Time Filter
3. Packet Group Aggregation -> processes input for Inter-Arrival Calculator
4. Arrival-Time Filter -> required by Overuse Detector (via Adaptive Threshold)
5. Adaptive Threshold -> feeds threshold to Overuse Detector
6. Overuse Detector -> required by AIMD Rate Controller
7. Incoming Bitrate Measurement -> required by AIMD Rate Controller
8. AIMD Rate Controller -> required by REMB Generator
9. REMB Generator -> final output

---

## Complexity Assessment

| Component | Complexity | Rationale |
|-----------|------------|-----------|
| abs-send-time Parser | **Low** | Simple bit manipulation, 24-bit fixed-point conversion |
| Packet Group Aggregation | **Low-Medium** | State tracking for burst detection, timestamp comparison |
| Inter-Arrival Calculator | **Low-Medium** | Basic arithmetic, but must handle wraparound correctly |
| Arrival-Time Filter (Kalman) | **Medium-High** | Recursive filter with 5+ parameters to tune correctly |
| Adaptive Threshold | **Medium** | Dynamic update logic with clamping and skip conditions |
| Overuse Detector | **Medium** | FSM with timing requirements (10ms duration check) |
| AIMD Rate Controller | **Medium** | State machine with multiplicative/additive modes and convergence detection |
| Incoming Bitrate Measurement | **Low** | Sliding window counter |
| REMB Generator | **Low-Medium** | RTCP packet construction, mantissa/exponent encoding |

**Overall Implementation Estimate:**
- Core pipeline (all table stakes): **Medium-High** complexity
- Primary challenge: Getting Kalman filter parameters tuned correctly for Chrome-like behavior
- Secondary challenge: Ensuring 64-second timestamp wraparound is handled correctly

---

## Recommended Implementation Order

Based on dependencies and testing ability:

1. **Phase 1: Foundation**
   - abs-send-time parser
   - REMB packet generator (can test output format)
   - Incoming bitrate measurement

2. **Phase 2: Core Pipeline**
   - Packet group aggregation
   - Inter-arrival calculator
   - Basic overuse detector (static threshold)

3. **Phase 3: Rate Control**
   - AIMD rate controller
   - Integration: detector -> controller -> REMB

4. **Phase 4: Refinement**
   - Arrival-time filter (Kalman)
   - Adaptive threshold
   - Pre-filtering for transients

5. **Phase 5: Polish**
   - Stream timeout handling
   - Multiple SSRC support
   - REMB throttling

---

## Key Sources

**Authoritative:**
- [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02) - GCC algorithm specification
- [draft-alvestrand-rmcat-remb](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb) - REMB RTCP format
- [abs-send-time spec](https://webrtc.googlesource.com/src/+/main/docs/native-code/rtp-hdrext/abs-send-time/README.md) - Header extension format

**Implementation Reference:**
- [libwebrtc remote_bitrate_estimator](https://chromium.googlesource.com/external/webrtc/+/refs/heads/master/modules/remote_bitrate_estimator/) - C++ source
- [inter_arrival.cc](https://github.com/webrtc-uwp/webrtc/blob/master/modules/remote_bitrate_estimator/inter_arrival.cc) - Packet grouping
- [aimd_rate_control.cc](https://chromium.googlesource.com/external/webrtc/+/refs/heads/master/modules/remote_bitrate_estimator/aimd_rate_control.cc) - Rate controller

**Analysis:**
- [GCC Analysis Paper (C3Lab)](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf) - Deep analysis of algorithm behavior
- [Remote Bitrate Estimator tutorial](https://www.fanyamin.com/webrtc/tutorial/build/html/4.code/remote_bitrate_estimator.html) - Architecture overview

**Pion Reference:**
- [pion/interceptor](https://github.com/pion/interceptor) - Existing Go WebRTC interceptors (TWCC implemented, REMB not)
- [Bandwidth Estimator issue](https://github.com/pion/interceptor/issues/25) - Pion BWE roadmap

---

## Confidence Assessment

| Area | Confidence | Rationale |
|------|------------|-----------|
| Component list | **HIGH** | Verified against IETF draft and libwebrtc source structure |
| Algorithm parameters | **HIGH** | Directly from draft-ietf-rmcat-gcc-02 |
| REMB format | **HIGH** | From draft-alvestrand-rmcat-remb |
| Complexity estimates | **MEDIUM** | Based on algorithm description, not implementation experience |
| Implementation order | **MEDIUM** | Logical dependency analysis, not validated |

**Known gaps:**
- Exact Chrome behavior may differ from draft (libwebrtc evolved significantly)
- Optimal parameter tuning may require experimentation
- Edge cases (very low bitrate, high loss) behavior not fully documented
