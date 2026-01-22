# Pitfalls Research: GCC Receiver-Side BWE

**Domain:** WebRTC Congestion Control / Bandwidth Estimation
**Researched:** 2026-01-22
**Confidence:** HIGH (verified against IETF draft, libwebrtc source, academic papers)

---

## Algorithm Pitfalls

### Critical: Static Threshold Causes Starvation

**What goes wrong:** Using a fixed threshold in the overuse detector causes GCC flows to be starved when competing with TCP traffic. The delay-based controller becomes too sensitive or too insensitive depending on network conditions.

**Why it happens:** The original GCC algorithm used a static threshold. Academic research (Carlucci et al., 2017) proved this causes starvation because TCP's aggressive loss-based control dominates when the threshold is too small, and GCC becomes unresponsive when it's too large.

**Consequences:**
- Bandwidth estimate drops to near-zero when TCP traffic is present
- Recovery can take 10+ minutes or never recover
- Unfair bandwidth sharing between GCC flows

**Prevention:**
- Implement the adaptive threshold from IETF draft-ietf-rmcat-gcc-02
- Formula: `del_var_th(i) = del_var_th(i-1) + (t(i)-t(i-1)) * K(i) * (|m(i)| - del_var_th(i-1))`
- Use asymmetric coefficients: K_u = 0.01, K_d = 0.00018 (K_u > K_d is critical)
- Clamp threshold between 6ms and 600ms
- Skip update when `|m(i)| - del_var_th(i)` exceeds 15ms (outlier filtering)

**Detection:** Test with concurrent TCP traffic (iperf). If your GCC flow gets starved, the adaptive threshold is broken or missing.

**Phase:** Core algorithm implementation (Phase 1)

---

### Critical: Wrong Delay Gradient Estimation

**What goes wrong:** The inter-group delay variation (m(i)) is calculated incorrectly, leading to false overuse/underuse signals.

**Why it happens:**
- Confusing arrival time deltas with timestamp deltas
- Not properly grouping packets into timestamp groups (video frames)
- Using wrong units (mixing milliseconds with RTP timestamp units)

**Consequences:**
- Bandwidth oscillates wildly (sawtooth pattern)
- False congestion detection triggers unnecessary rate cuts
- Never converges to stable estimate

**Prevention:**
- Timestamp groups should be based on RTP timestamp, not arrival time
- Group length should be ~5ms (kTimestampGroupLengthTicks in libwebrtc)
- Calculate inter-arrival delta: `(arrival[i] - arrival[i-1]) - (timestamp[i] - timestamp[i-1]) / clockrate`
- Filter using trendline or Kalman filter, not raw values

**Detection:** Compare delay gradient output against libwebrtc with same packet trace. Large divergence indicates calculation error.

**Phase:** Core algorithm implementation (Phase 1)

---

### High: Incorrect AIMD Rate Control State Machine

**What goes wrong:** The rate control state machine transitions incorrectly, causing bandwidth estimate to oscillate or get stuck.

**Why it happens:** Misunderstanding the three-state machine (Increase, Decrease, Hold) and its transition rules. Common mistakes:
- Always decreasing on overuse (should transition to Decrease state, not continuously decrease)
- Not distinguishing multiplicative vs additive increase phases
- Wrong alpha factor for decrease (should be ~0.85, not arbitrary value)

**Consequences:**
- Bandwidth undershoots repeatedly after congestion
- Slow convergence (minutes instead of seconds)
- Unfair bandwidth allocation between flows

**Prevention:**
Implement exact state machine from IETF draft:

| Signal    | Hold       | Increase   | Decrease   |
|-----------|------------|------------|------------|
| Overuse   | -> Decrease| -> Decrease| (stay)     |
| Normal    | -> Increase| (stay)     | -> Hold    |
| Underuse  | (stay)     | -> Hold    | -> Hold    |

- Multiplicative increase: 8% per second when far from convergence
- Additive increase: ~0.5 packets per RTT when near convergence
- Multiplicative decrease: multiply by 0.85 on overuse

**Detection:** Log state transitions. Healthy flow should cycle: Increase -> Hold -> Decrease -> Hold -> Increase...

**Phase:** Rate control implementation (Phase 2)

---

### High: Burst Grouping Failures

**What goes wrong:** Packets sent in bursts (due to pacing or network behavior) are incorrectly treated as separate arrivals, causing false congestion detection.

**Why it happens:** Not implementing burst detection logic from libwebrtc's `inter_arrival.cc`. Burst packets arrive within 5ms of each other but have different RTP timestamps.

**Consequences:**
- Spurious overuse signals from normal bursty traffic
- Bandwidth estimate drops unnecessarily
- Poor performance with bursty video encoders

**Prevention:**
- Implement `BelongsToBurst()` logic:
  - propagation_delta = timestamp_delta - arrival_delta
  - If propagation_delta < 0 and arrival_delta <= 5ms and total burst < 100ms, group as burst
- Constants: kBurstDeltaThresholdMs = 5, kMaxBurstDurationMs = 100

**Detection:** Test with bursty traffic pattern. If bandwidth drops significantly more than with smooth traffic at same rate, burst grouping is broken.

**Phase:** Inter-arrival calculation (Phase 1)

---

### Medium: Overuse Time Threshold Ignored

**What goes wrong:** Overuse signal is generated immediately when threshold is crossed, rather than waiting for sustained overuse.

**Why it happens:** Missing the `overuse_time_th` parameter (10ms) that requires overuse condition to persist before signaling.

**Consequences:**
- Transient network jitter causes unnecessary rate decreases
- Bandwidth estimate is overly reactive
- Poor performance on jittery but adequate networks

**Prevention:**
- Only signal overuse after condition persists for `overuse_time_th` (default 10ms)
- Reset timer when condition clears
- This filters out transient spikes

**Phase:** Overuse detector (Phase 1)

---

## Timing/Clock Pitfalls

### Critical: Timestamp Wraparound Handling

**What goes wrong:** 32-bit RTP timestamps wrap around after ~13 hours at 90kHz clock rate (video) or ~6 hours at 48kHz (audio). Incorrect wraparound handling causes massive spurious delay calculations.

**Why it happens:** Naive subtraction of timestamps without considering wraparound produces huge negative or positive deltas.

**Consequences:**
- Sudden massive "congestion" detection after 6-13 hours
- Bandwidth estimate crashes to minimum
- Call quality degrades severely for long sessions

**Prevention:**
- Use wraparound-safe comparison: treat delta > 2^31 as wraparound
- libwebrtc uses: "a diff which is bigger than half the timestamp interval (32 bits) must be due to reordering"
- Implement as: `if delta > 0x7FFFFFFF { delta -= 0x100000000 }`

**Detection:** Run long-duration tests (>12 hours). Monitor for sudden bandwidth drops.

**Phase:** Core timestamp handling (Phase 1)

---

### Critical: Absolute Send Time 24-bit Wraparound

**What goes wrong:** The 24-bit absolute send time extension wraps every 64 seconds. Not handling this causes massive timing errors.

**Why it happens:** abs-send-time uses 6.18 fixed-point format in 24 bits, giving 64-second range.

**Consequences:**
- Every 64 seconds, potential for spurious delay spikes
- Intermittent bandwidth drops on long calls

**Prevention:**
- Track expected time and detect wraparound
- Formula: `abs_send_time_24 = (ntp_timestamp_64 >> 14) & 0x00ffffff`
- When new timestamp < old timestamp by large margin, add 2^24

**Detection:** Test calls longer than 2-3 minutes. Watch for periodic bandwidth estimation errors every ~64 seconds.

**Phase:** Extension parsing (Phase 1)

---

### High: Monotonic vs Wall Clock Confusion in Go

**What goes wrong:** Using `time.Time` comparisons that accidentally strip monotonic readings, causing incorrect elapsed time calculations.

**Why it happens:** Go's `time.Now()` includes both wall clock and monotonic clock. Certain operations (UTC(), In(), Round(), Truncate()) strip the monotonic component.

**Consequences:**
- Clock synchronization events (NTP adjustments) cause timing discontinuities
- Elapsed time calculations become wrong by seconds
- Spurious congestion detection after system clock changes

**Prevention:**
- Use `time.Since(start)` for elapsed time, not `time.Now().Sub(start)` after transformations
- Never apply UTC(), In(), Round(), Truncate() to times used for duration calculation
- Consider storing arrival times as monotonic offsets from session start
- If you need to strip monotonic: `t = t.Round(0)` (do this intentionally, not accidentally)

**Detection:** Change system clock during test. If bandwidth estimate goes haywire, you have wall clock leakage.

**Phase:** All timing code (ongoing)

---

### High: Arrival Time vs System Time Divergence

**What goes wrong:** System time and packet arrival timestamps diverge, causing estimation errors that compound over time.

**Why it happens:** libwebrtc tracks this with `kArrivalTimeOffsetThresholdMs`. When system time and arrival time drift apart (due to clock drift or scheduling delays), the estimation becomes unreliable.

**Consequences:**
- Gradual degradation of estimate accuracy
- Eventually, complete misestimation

**Prevention:**
- Track difference between system time and computed arrival time
- Reset state when divergence exceeds threshold (libwebrtc uses ~3 seconds)
- Log warnings when reset occurs for debugging

**Phase:** Inter-arrival state management (Phase 1)

---

### Medium: Clock Rate Conversion Errors

**What goes wrong:** RTP timestamp deltas are converted to time deltas using wrong clock rate, causing systematic under/over-estimation of delays.

**Why it happens:** Different media types use different clock rates (video: typically 90kHz, audio: 48kHz or others). Using wrong rate or hardcoding a value causes errors.

**Consequences:**
- Systematic bias in delay estimation
- Works for one media type but fails for others

**Prevention:**
- Get clock rate from SDP negotiation
- Video is almost always 90000 Hz but don't hardcode
- Audio varies: 48000, 44100, 16000, 8000 common
- Conversion: `time_delta_ms = timestamp_delta * 1000 / clockrate`

**Phase:** Media type handling (Phase 2)

---

## Performance Pitfalls

### Critical: Per-Packet Allocations

**What goes wrong:** Allocating memory for each incoming RTP packet causes GC pressure that introduces latency spikes.

**Why it happens:** Creating new structs, slices, or maps for each packet in the hot path.

**Consequences:**
- GC pauses cause jitter in processing
- At high packet rates (>1000 pps), GC overhead dominates
- Unpredictable latency spikes affect estimation accuracy

**Prevention:**
- Use `sync.Pool` for packet metadata structures
- Pre-allocate slices with expected capacity
- Avoid closures in hot paths (they allocate)
- Pass buffers into functions rather than returning new slices
- Profile with `go tool pprof` focusing on allocs

**Detection:** Run with `GODEBUG=gctrace=1`. Watch for frequent GC at high packet rates. Target: <1 alloc per packet in steady state.

**Phase:** Performance optimization (Phase 3)

---

### High: Lock Contention on Estimation State

**What goes wrong:** Multiple goroutines contend on locks protecting estimation state, causing latency and throughput problems.

**Why it happens:** Typical pattern: one goroutine receives packets and updates state, another reads estimates for REMB generation. Naive mutex usage creates contention.

**Consequences:**
- Packet processing latency increases under load
- REMB feedback delayed, reducing control loop effectiveness
- Throughput bottleneck

**Prevention:**
- Use read-write locks (`sync.RWMutex`) where appropriate
- Consider lock-free designs with atomic operations for simple counters
- Batch state updates rather than per-packet locking
- Keep critical sections minimal

**Detection:** Profile with `go tool pprof` for mutex contention. Look for goroutines blocked on mutex in traces.

**Phase:** Concurrency design (Phase 1-2)

---

### Medium: Time Function Call Overhead

**What goes wrong:** Calling `time.Now()` for every packet adds measurable overhead at high packet rates.

**Why it happens:** `time.Now()` involves system calls on some platforms.

**Consequences:**
- At 10,000+ packets/second, time calls become significant overhead
- Adds microseconds per packet

**Prevention:**
- Batch timestamp queries: get time once per group of packets
- Consider coarser timing (per-millisecond buckets)
- Cache current time with periodic refresh for non-critical paths

**Detection:** Benchmark with and without time calls. Significant at >10k pps.

**Phase:** Performance optimization (Phase 3)

---

## Interop Pitfalls

### Critical: REMB Packet Format Errors

**What goes wrong:** REMB packets are malformed and rejected by libwebrtc/Chrome, causing bandwidth estimate to be ignored.

**Why it happens:**
- Wrong FMT (should be 15) or PT (should be 206)
- Missing "REMB" magic bytes
- Incorrect mantissa/exponent encoding
- Wrong SSRC handling

**Consequences:**
- Chrome/libwebrtc silently ignores REMB
- Sender never receives feedback
- Quality degrades due to no congestion control

**Prevention:**
REMB format must be exact:
```
- PT = 206 (PSFB)
- FMT = 15
- Unique identifier: 0x52454D42 ("REMB")
- Num SSRC: number of SSRCs (typically 1)
- BR Exp: 6 bits (0-63)
- BR Mantissa: 18 bits
- Bitrate = mantissa * 2^exp
```

**Detection:** Capture REMB packets in Wireshark. Verify structure matches draft-alvestrand-rmcat-remb-03. Test against Chrome and check webrtc-internals shows received REMB.

**Phase:** REMB generation (Phase 2)

---

### High: Absolute Capture Time Extension Parsing

**What goes wrong:** Absolute capture time extension is parsed incorrectly or not found in packets.

**Why it happens:**
- Extension not negotiated in SDP
- Wrong extension ID (must match SDP negotiated value)
- Parsing errors in 64-bit NTP timestamp format
- Not handling interpolation when extension is absent

**Consequences:**
- Falls back to less accurate timing
- Cross-stream synchronization breaks
- Bandwidth estimation less accurate

**Prevention:**
- Parse extension ID from SDP (not hardcoded)
- Format: 64-bit NTP timestamp (32.32 fixed point, seconds since 1900)
- When absent, interpolate from RTP timestamp and last known capture time
- Send at least every second to mitigate clock drift

**Detection:** Log when extension is present/absent. Verify parsed values are reasonable NTP timestamps.

**Phase:** Extension parsing (Phase 1)

---

### High: Absolute Send Time Extension Parsing

**What goes wrong:** The 24-bit absolute send time value is parsed or converted incorrectly.

**Why it happens:**
- Endianness errors (big-endian on wire)
- Wrong bit extraction
- Conversion to milliseconds uses wrong formula

**Consequences:**
- Timing completely wrong
- Bandwidth estimation fails

**Prevention:**
- Parse as big-endian 24-bit value
- Format: 6.18 fixed-point (6 bits seconds, 18 bits fraction)
- Conversion: `seconds = value / (1 << 18)`
- Reference: `abs_send_time_24 = (ntp_timestamp_64 >> 14) & 0x00ffffff`

**Detection:** Compare parsed values against Chrome's webrtc-internals. Should match closely.

**Phase:** Extension parsing (Phase 1)

---

### Medium: SSRC Handling in Multi-Stream Scenarios

**What goes wrong:** With multiple video streams (simulcast), REMB is sent for wrong SSRC or calculations mix up streams.

**Why it happens:** Each stream has its own SSRC. Estimation may need to be per-stream or aggregate. Firefox has known bugs with REMB SSRC selection.

**Consequences:**
- REMB feedback applies to wrong stream
- One stream starved while another is fine
- Incorrect total bandwidth calculation

**Prevention:**
- Track which SSRCs are being estimated
- REMB packet includes list of SSRCs it applies to
- Consider whether estimate is per-stream or aggregate
- Test with simulcast scenarios

**Phase:** Multi-stream support (Phase 3)

---

### Medium: Chrome Behavior Quirks

**What goes wrong:** Implementation works in isolation but fails with Chrome due to undocumented behavior.

**Why it happens:**
- Chrome doesn't send REMB for receive-only peer connections (no local stream)
- TWCC compound packet handling issues
- Specific timing expectations

**Consequences:**
- Works with Firefox but not Chrome, or vice versa
- Interop failures in production

**Prevention:**
- Test extensively with Chrome specifically
- Monitor webrtc-internals for feedback reception
- Review Chrome-specific issues on bugs.chromium.org/p/webrtc

**Phase:** Interop testing (Phase 3)

---

## Testing Pitfalls

### Critical: Not Comparing Against libwebrtc Reference

**What goes wrong:** Implementation "works" but produces different estimates than libwebrtc, causing interop problems.

**Why it happens:** Subtle algorithm differences compound over time. GCC is complex enough that small errors cause large divergence.

**Consequences:**
- Different behavior than expected by senders
- May over or under-estimate compared to Chrome
- Hard to debug customer issues

**Prevention:**
- Record packet traces (arrival time, RTP timestamp, extension values)
- Feed same trace to libwebrtc and your implementation
- Compare delay gradients, threshold values, rate estimates over time
- Use Chrome's RTC event log visualizer for comparison
- Acceptable divergence: <10% after stabilization

**Detection:** Run side-by-side comparison tests. Log intermediate values (delay gradient, threshold, state) for debugging.

**Phase:** Validation (all phases)

---

### High: Testing Only With Smooth Traffic

**What goes wrong:** Tests use smooth, constant-rate traffic that doesn't exercise edge cases.

**Why it happens:** Easy to generate smooth traffic. Real traffic is bursty, variable, and lossy.

**Consequences:**
- Works in lab, fails in production
- Edge cases (burst, loss, reordering) not covered

**Prevention:**
Test with:
- Bursty traffic (video keyframes cause bursts)
- Packet loss (2%, 5%, 10%)
- Packet reordering
- Competing TCP traffic
- Variable bandwidth (tc netem)
- Long duration (>12 hours for wraparound)

**Phase:** Test development (all phases)

---

### High: No Network Impairment Testing

**What goes wrong:** Only tested on localhost or LAN, not under realistic network conditions.

**Why it happens:** Setting up network impairment is extra effort.

**Consequences:**
- Algorithm works on perfect network
- Fails under real-world conditions (loss, delay, jitter)

**Prevention:**
Use network impairment tools:
```bash
# Linux tc/netem
tc qdisc add dev eth0 root netem delay 50ms 10ms loss 2%

# macOS Network Link Conditioner (System Preferences)
# Or dnctl/pfctl
```

Test scenarios:
- 100ms RTT, 0% loss (good wifi)
- 200ms RTT, 2% loss (mobile)
- 50ms RTT, 5% loss (congested)
- Variable delay (jitter)

**Phase:** Integration testing (Phase 3)

---

### Medium: Missing Long-Duration Tests

**What goes wrong:** Tests run for seconds/minutes. Bugs manifest after hours.

**Why it happens:** Long tests are slow and expensive. CI/CD prefers fast tests.

**Consequences:**
- Timestamp wraparound bugs (6-13 hours)
- Memory leaks
- State accumulation issues

**Prevention:**
- Run 24-hour soak tests periodically
- Accelerate time in unit tests where possible
- Monitor memory usage over time
- Include wraparound-specific unit tests with simulated timestamps

**Phase:** Stability testing (Phase 4)

---

## Prevention Strategies Summary

### Phase 1: Core Algorithm
1. Implement adaptive threshold (K_u = 0.01, K_d = 0.00018)
2. Implement burst grouping (5ms threshold, 100ms max)
3. Handle 32-bit timestamp wraparound
4. Handle 24-bit abs-send-time wraparound
5. Use monotonic time for all duration calculations
6. Pre-allocate data structures, use sync.Pool

### Phase 2: Rate Control & REMB
1. Implement correct AIMD state machine
2. Generate spec-compliant REMB packets
3. Parse extensions from SDP-negotiated IDs
4. Test REMB reception in Chrome webrtc-internals

### Phase 3: Integration & Performance
1. Profile for allocations (target <1 per packet)
2. Test with network impairment
3. Test with competing TCP traffic
4. Test multi-stream (simulcast) scenarios
5. Compare against libwebrtc reference

### Phase 4: Stability
1. Run 24-hour soak tests
2. Test timestamp wraparound explicitly
3. Monitor memory for leaks
4. Validate interop with Chrome, Firefox, Safari

---

## Sources

**IETF Standards:**
- [draft-ietf-rmcat-gcc-02](https://datatracker.ietf.org/doc/html/draft-ietf-rmcat-gcc-02) - Google Congestion Control Algorithm
- [draft-alvestrand-rmcat-remb-03](https://datatracker.ietf.org/doc/html/draft-alvestrand-rmcat-remb-03) - REMB Packet Format

**Academic Papers:**
- [Congestion Control for Web Real-Time Communication (Carlucci et al.)](https://c3lab.poliba.it/images/c/c4/Gcc-TNET.pdf) - Adaptive threshold research
- [Analysis and Design of GCC](https://c3lab.poliba.it/images/6/65/Gcc-analysis.pdf) - Algorithm analysis

**Implementation References:**
- [libwebrtc inter_arrival.cc](https://github.com/webrtc-uwp/webrtc/blob/master/modules/remote_bitrate_estimator/inter_arrival.cc) - Burst grouping, reordering
- [pion/rtp abssendtimeextension.go](https://github.com/pion/rtp/blob/master/abssendtimeextension.go) - Go extension parsing
- [pion/interceptor TWCC issues](https://github.com/pion/interceptor/issues/159) - Known bugs

**WebRTC Documentation:**
- [Absolute Capture Time](https://webrtc.googlesource.com/src/+/refs/heads/main/docs/native-code/rtp-hdrext/abs-capture-time/README.md)
- [Absolute Send Time](https://webrtc.github.io/webrtc-org/experiments/rtp-hdrext/abs-send-time/)
- [webrtcHacks - Bandwidth Probing](https://webrtchacks.com/probing-webrtc-bandwidth-probing-why-and-how-in-gcc/)

**Go Performance:**
- [Go Memory Optimization](https://goperf.dev/01-common-patterns/gc/) - GC pressure reduction
- [Go time package](https://pkg.go.dev/time) - Monotonic clock handling
- [VictoriaMetrics - Monotonic Clock](https://victoriametrics.com/blog/go-time-monotonic-wall-clock/) - Wall vs monotonic time
