# Pitfalls Research: Migration Risks - Pion Type Adoption

**Domain:** Refactoring BWE to use Pion native types
**Researched:** 2026-01-22
**Confidence:** HIGH (based on codebase analysis, benchmark data, Pion documentation)

---

## Executive Summary

This is a **refactor of working code**. The main risk is **breaking something that works**. Current implementation has:
- Comprehensive tests (unit, integration, soak)
- Chrome interop verified
- 0 allocs/op in hot path (core estimator)
- 24-bit timestamp wraparound handling validated

**Migration target areas:**
1. REMB marshalling → `pion/rtcp.ReceiverEstimatedMaximumBitrate`
2. RTP extension parsing → Pion's extension APIs
3. Timestamp handling → Pion utilities where applicable

**Key constraint:** Behavioral compatibility must be preserved. Minor improvements acceptable if Pion handles edge cases better.

---

## Critical Pitfalls

### Critical 1: Allocation Regressions in Hot Path

**What breaks:** Current code achieves 0 allocs/op in `OnPacket()`. Switching to Pion types introduces allocations that regress this.

**Where it happens:**
- **REMB marshalling:** Current `BuildREMB()` shows `1 alloc/op` (24 B/op) for `rtcp.ReceiverEstimatedMaximumBitrate{}.Marshal()`
- **REMB parsing:** Current `ParseREMB()` shows `2 allocs/op` (56 B/op) for `rtcp.Unmarshal()`
- **RTP header parsing:** Current interceptor shows `2 allocs/op` (104 B/op) in `processRTP`

**Why it matters:**
- PERF-01 requirement: <1 alloc/op for steady-state packet processing
- Current baseline: **0 allocs/op** in `BandwidthEstimator.OnPacket`
- At 1000 pps (typical video), even 1 alloc/op = 1000 heap allocations/sec
- GC pressure increases latency, affects estimation accuracy

**Detection:**
```bash
# Before migration
go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...
# Expected: 0 allocs/op across all core benchmarks

# After migration
go test -bench=ZeroAlloc -benchmem ./pkg/bwe/...
# Compare: Any increase in allocs/op is a regression
```

**Prevention:**

1. **For REMB building (non-hot path):** Acceptable to have 1-2 allocs/op since REMB is sent at 1 Hz, not per-packet

2. **For RTP parsing (hot path):** Use Pion's zero-allocation patterns:
   ```go
   // GOOD: Reuse header, no allocation
   var header rtp.Header
   if _, err := header.Unmarshal(raw); err != nil { ... }

   // BAD: Creates new header each time
   header := &rtp.Header{}
   if _, err := header.Unmarshal(raw); err != nil { ... }
   ```

3. **For extension parsing:** Pion's `header.GetExtension(id)` returns a slice into existing buffer - zero allocation if done correctly

4. **Buffer pooling strategy:**
   ```go
   // Current code has sync.Pool for PacketInfo - keep this
   var packetInfoPool = sync.Pool{
       New: func() interface{} { return &bwe.PacketInfo{} },
   }

   // If REMB Marshal/Unmarshal becomes hot path, pool buffers:
   var rembBufPool = sync.Pool{
       New: func() interface{} { return make([]byte, 0, 64) },
   }
   ```

**Benchmark verification points:**
- `BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc`: MUST remain 0 allocs/op
- `BenchmarkDelayEstimator_OnPacket_ZeroAlloc`: MUST remain 0 allocs/op
- `BenchmarkInterArrivalCalculator_AddPacket_ZeroAlloc`: MUST remain 0 allocs/op
- `BenchmarkProcessRTP_Allocations`: Currently 2 allocs/op (104 B) - acceptable as this is interceptor overhead, not estimator

---

### Critical 2: REMB Encoding Precision Changes

**What breaks:** Pion's `ReceiverEstimatedMaximumBitrate` uses `float32` for bitrate, while current implementation uses `uint64`. Precision loss or rounding differences could break Chrome interop.

**Where it happens:**
```go
// Current implementation
type REMBPacket struct {
    Bitrate uint64  // Full 64-bit precision
    ...
}

// Pion implementation
type ReceiverEstimatedMaximumBitrate struct {
    Bitrate float32  // Float32 loses precision above ~16M
    ...
}
```

**Why it matters:**
- **Validated behavior:** Current tests verify bitrate encoding within 1% tolerance for high bitrates (up to 10 Gbps)
- **Float32 precision limit:** ~7 decimal digits of precision
  - At 1 Gbps: float32 has ~2 kbps precision - acceptable
  - At 10 Gbps: float32 has ~20 kbps precision - still acceptable but worse
- **Mantissa/exponent encoding:** REMB uses 6-bit exponent + 18-bit mantissa. Float32 conversion must preserve this

**Detection:**
```go
// Test from remb_test.go that MUST pass
func TestBuildREMB_BitrateEncodingPrecision(t *testing.T) {
    testCases := []struct {
        bitrate  uint64
        maxError float64
    }{
        {"1 Gbps", 1_000_000_000, 0.01},
        {"5 Gbps", 5_000_000_000, 0.01},
        {"10 Gbps", 10_000_000_000, 0.01},
    }
    // All must encode/decode within maxError tolerance
}
```

**Prevention:**

1. **Keep existing test suite:** All `TestBuildREMB_*` tests must pass with Pion types

2. **Verify float32 conversion doesn't lose critical bits:**
   ```go
   // When converting uint64 → float32 for Pion
   bitrate := uint64(5_000_000_000)  // 5 Gbps

   // SAFE: Direct conversion preserves enough precision for REMB
   pionBitrate := float32(bitrate)

   // VERIFY: Round-trip error is acceptable
   recovered := uint64(pionBitrate)
   relError := float64(abs(recovered - bitrate)) / float64(bitrate)
   assert.Less(t, relError, 0.01) // <1% error
   ```

3. **Run existing Chrome interop test:** `TestREMBPacket_ChromeInterop` (if exists) must pass - Chrome must accept and act on REMB

4. **Edge cases to verify:**
   - Zero bitrate: REMB can't represent exact zero (mantissa/exponent encoding)
   - Very low bitrate: 10 kbps
   - Very high bitrate: 10 Gbps
   - All must decode within 1% of original

**Comparison to validate:**
```bash
# Before: Current BuildREMB output
go test -v -run=TestBuildREMB_BitrateEncodingPrecision

# After: Pion-based BuildREMB output
go test -v -run=TestBuildREMB_BitrateEncodingPrecision

# Diff should show same precision tolerance
```

---

### Critical 3: Timestamp Wraparound Behavior Changes

**What breaks:** Current implementation has custom `UnwrapAbsSendTime()` with validated 24-bit wraparound logic. Switching to Pion utilities might have different edge case handling.

**Where it happens:**
```go
// Current implementation (timestamp.go:44-61)
func UnwrapAbsSendTime(prev, curr uint32) int64 {
    diff := int32(curr) - int32(prev)
    halfRange := int32(AbsSendTimeMax / 2)  // 8388608

    if diff > halfRange {
        diff -= int32(AbsSendTimeMax)
    } else if diff < -halfRange {
        diff += int32(AbsSendTimeMax)
    }
    return int64(diff)
}
```

**Why it matters:**
- **Validated:** 24-hour soak test (04-05) specifically validates wraparound at 64-second boundary
- **Critical edge cases tested:**
  - Exactly at boundary: `prev=16777215, curr=0` → `want=1`
  - Just over half range: `prev=0, curr=8388609` → `want=-8388607` (backward wrap)
  - Long duration: Wraparound every 64 seconds for hours
- **Failure mode:** Incorrect wraparound causes massive delay spikes, bandwidth crashes to minimum

**Detection:**
```go
// Tests that MUST pass (from timestamp_test.go)
func TestUnwrapAbsSendTime(t *testing.T) {
    // All 15 test cases must pass, especially:
    // - wraparound forward: prev=16777000, curr=200 → want=416
    // - wraparound backward: prev=200, curr=16777000 → want=-416
    // - exactly at boundary: prev=16777215, curr=0 → want=1
    // - just over half range: prev=0, curr=8388609 → want=-8388607
}

// Long-duration test that MUST pass
func TestTimestampWraparound_24Hour(t *testing.T) {
    // Simulates 24 hours of operation
    // Wraparound happens every 64 seconds
    // Must NOT show spurious delay spikes
}
```

**Prevention:**

1. **If keeping custom implementation:** No change, already validated

2. **If switching to Pion utilities:**
   - Verify Pion has equivalent wraparound handling for 24-bit abs-send-time
   - Run full timestamp test suite against Pion implementation
   - **Fallback:** Keep custom `UnwrapAbsSendTime()` if Pion doesn't handle 24-bit format

3. **Soak test verification:**
   ```bash
   # MUST pass before and after migration
   go test -v -run=TestTimestampWraparound_24Hour
   go test -v -run=TestAcceleratedSoak_TimestampWrap
   ```

4. **Edge case checklist:**
   - [ ] Wraparound at exactly 2^24 boundary
   - [ ] Half-range detection (forward vs backward)
   - [ ] Multiple wraparounds in succession
   - [ ] Large time gaps (>32 seconds)

**Risk assessment:**
- **LOW** if keeping custom timestamp handling
- **MEDIUM** if switching to Pion - requires thorough validation
- **HIGH** if Pion doesn't support 24-bit abs-send-time format (use custom implementation)

---

## High-Severity Pitfalls

### High 1: RTCP Compound Packet Handling in Interceptor

**What breaks:** Current code uses `rtcp.Unmarshal(data)` which returns `[]rtcp.Packet` - may contain multiple packets in compound RTCP. Not handling this breaks REMB sending.

**Where it happens:**
```go
// Current interceptor code (interceptor.go:259-262)
func (i *BWEInterceptor) maybeSendREMB(now time.Time) {
    data, shouldSend, err := i.estimator.MaybeBuildREMB(now)
    // ...
    pkts, err := rtcp.Unmarshal(data)  // Returns []rtcp.Packet
    // Bug potential: pkts might have length > 1 in compound packet
}
```

**Why it matters:**
- RTCP can bundle multiple packets (compound packets)
- `rtcp.Unmarshal()` returns a slice, not a single packet
- Current code assumes single REMB packet, but Pion might add additional packets (e.g., SDES)
- Failing to handle slice properly causes panic or dropped packets

**Detection:**
```go
// Test for compound packet handling
func TestREMB_CompoundPacket(t *testing.T) {
    // Create REMB + SDES compound packet
    remb := &rtcp.ReceiverEstimatedMaximumBitrate{...}
    sdes := &rtcp.SourceDescription{...}

    data, _ := rtcp.Marshal([]rtcp.Packet{remb, sdes})

    // Verify unmarshal handles multiple packets
    pkts, err := rtcp.Unmarshal(data)
    assert.NoError(t, err)
    assert.Len(t, pkts, 2)  // Should detect both
}
```

**Prevention:**

1. **Defensive unmarshal:**
   ```go
   pkts, err := rtcp.Unmarshal(data)
   if err != nil {
       return
   }

   // GOOD: Handle all packets
   _, err = writer.Write(pkts, nil)

   // BAD: Only handles first packet
   // _, err = writer.Write([]rtcp.Packet{pkts[0]}, nil)
   ```

2. **Trust Pion's contract:** `rtcp.Unmarshal()` always returns a slice, even for single packet

3. **Test with actual Chrome RTCP:** Capture real RTCP from Chrome, feed to unmarshal, verify handling

**Current code status:** Appears correct (uses `pkts` directly), but verify no assumptions about slice length

---

### High 2: Extension ID Race During Initialization

**What breaks:** Extension IDs are set when `BindRemoteStream` is called, but packets may arrive before this happens. Processing packets with uninitialized extension IDs causes silent data loss.

**Where it happens:**
```go
// Current interceptor code (interceptor.go:134-139)
func (i *BWEInterceptor) BindRemoteStream(info *interceptor.StreamInfo, reader interceptor.RTPReader) {
    // Extension IDs set here via atomic operations
    if absID := FindAbsSendTimeID(info.RTPHeaderExtensions); absID != 0 {
        i.absExtID.CompareAndSwap(0, uint32(absID))
    }
    // But packets might be processed BEFORE this completes
}
```

**Why it matters:**
- **Timing uncertainty:** No guarantee BindRemoteStream is called before first packet
- **Silent failure:** If extension ID is 0, packet is skipped without error (line 204: `if sendTime == 0 { return }`)
- **Lost calibration:** Early packets are important for initial bandwidth estimate

**Detection:**
```go
// Add logging to detect this condition
func (i *BWEInterceptor) processRTP(raw []byte, ssrc uint32) {
    absID := uint8(i.absExtID.Load())
    captureID := uint8(i.captureExtID.Load())

    if absID == 0 && captureID == 0 {
        // WARNING: Processing packet before extension IDs set
        // This should happen rarely (only for first few packets)
        log.Warn("Extension IDs not initialized, skipping packet")
    }
}
```

**Prevention:**

1. **Current code is correct:** Uses atomic `CompareAndSwap` to handle race

2. **Accept early packet loss:** First few packets before BindRemoteStream are OK to skip

3. **Don't block waiting for IDs:** Would deadlock the RTP pipeline

4. **Monitor in production:** Track how often extension IDs are uninitialized when processRTP is called

**Risk assessment:** LOW - current code handles this correctly, just document the behavior

---

### High 3: REMB Scheduler State Synchronization

**What breaks:** `BandwidthEstimator` and `REMBScheduler` share state. Migrating one without the other causes desync, leading to duplicate REMB sends or missed REMB.

**Where it happens:**
```go
// Current code links estimator and scheduler (interceptor.go:93-98)
rembConfig := bwe.DefaultREMBSchedulerConfig()
rembConfig.Interval = i.rembInterval
rembConfig.SenderSSRC = i.senderSSRC
i.rembScheduler = bwe.NewREMBScheduler(rembConfig)
i.estimator.SetREMBScheduler(i.rembScheduler)  // Links them
```

**Why it matters:**
- **REMBScheduler manages when to send:** Tracks last send time, enforces interval
- **BandwidthEstimator manages what to send:** Current estimate, SSRC list
- **Breaking the link:** If Pion has its own REMB scheduler, might conflict with existing one

**Detection:**
```go
// Test REMB sending rate
func TestREMB_SendingInterval(t *testing.T) {
    // Configure 1s interval
    // Send packets for 5 seconds
    // Verify exactly 5 REMB packets sent (not 0, not 10)

    assert.Equal(t, 5, len(capturedREMBs))
}
```

**Prevention:**

1. **Keep existing architecture:** Don't replace REMBScheduler, only swap REMB marshalling to Pion types

2. **If consolidating with Pion scheduler:**
   - Verify interval handling matches current behavior
   - Ensure no duplicate sends (current + Pion)
   - Test for missed sends (neither fires)

3. **Integration test:**
   ```bash
   # Existing test should pass
   go test -v -run=TestBWEInterceptor_REMBSending
   ```

**Recommendation:** Keep current REMBScheduler, only use Pion for `rtcp.ReceiverEstimatedMaximumBitrate` marshalling

---

## Medium-Severity Pitfalls

### Medium 1: Extension Header Parsing API Differences

**What breaks:** Current code uses `rtp.Header.GetExtension(id)` which returns `[]byte`. Pion might have alternate APIs with different semantics (e.g., returning parsed structs).

**Where it happens:**
```go
// Current code (interceptor.go:182-184)
if ext := header.GetExtension(absID); len(ext) >= 3 {
    sendTime, _ = bwe.ParseAbsSendTime(ext)
}
```

**Why it matters:**
- `GetExtension()` returns a slice into the original packet buffer - zero copy
- If switching to a parsed extension API, might allocate or copy data
- Different APIs might handle one-byte vs two-byte extension profiles differently

**Prevention:**

1. **Current API is correct:** `GetExtension(id) []byte` is the right level of abstraction

2. **Verify zero-copy behavior preserved:**
   ```go
   BenchmarkRTPHeader_GetExtension_Allocations-10  1000000000  0.6556 ns/op  0 B/op  0 allocs/op
   // MUST remain 0 allocs after migration
   ```

3. **Keep custom ParseAbsSendTime/ParseAbsCaptureTime:** They handle raw bytes correctly

**Risk:** LOW - current API is optimal, no reason to change

---

### Medium 2: Error Handling Semantics Changes

**What breaks:** Pion's `rtcp.Unmarshal()` and `rtcp.Marshal()` may return different errors than custom implementation, causing unexpected error handling.

**Where it happens:**
```go
// Current REMB parsing (remb.go:46-49)
pkt := &rtcp.ReceiverEstimatedMaximumBitrate{}
if err := pkt.Unmarshal(data); err != nil {
    return nil, err  // What errors can Pion return here?
}
```

**Why it matters:**
- Current code may have error handling tuned to specific error types
- Pion might return different errors for same conditions
- Could break error recovery logic or logging

**Prevention:**

1. **Review error paths:** Check what errors current tests expect

2. **Test invalid inputs:**
   ```go
   // From remb_test.go
   func TestParseREMB_InvalidData(t *testing.T) {
       testCases := []struct {
           name string
           data []byte
       }{
           {"empty", []byte{}},
           {"too short", []byte{0x8F, 0xCE}},
           {"wrong PT", []byte{...}},
       }
       // All should return errors - verify Pion returns errors too
   }
   ```

3. **Don't over-specify errors:** Just check `err != nil`, not specific error messages

**Risk:** LOW - most code just checks `err != nil`

---

### Medium 3: Bitrate Type Conversions uint64 ↔ float32

**What breaks:** Repeated conversions between `uint64` (internal) and `float32` (Pion) accumulate rounding errors or lose precision.

**Where it happens:**
```go
// Internal estimator uses uint64
estimate := estimator.GetEstimate()  // uint64

// Convert to Pion float32 for REMB
remb := &rtcp.ReceiverEstimatedMaximumBitrate{
    Bitrate: float32(estimate),  // Conversion 1
}

// If round-tripping:
recovered := uint64(remb.Bitrate)  // Conversion 2
// Repeated conversions compound error
```

**Why it matters:**
- Single conversion: acceptable precision loss
- Round-trip conversion: precision loss compounds
- Estimator state should stay uint64 to avoid accumulation

**Prevention:**

1. **One-way conversion only:** Internal state stays uint64, only convert to float32 for Pion marshal

2. **Never store float32 back to estimator:**
   ```go
   // GOOD: One-way conversion
   bitrate := estimator.GetEstimate()  // uint64
   remb := &rtcp.ReceiverEstimatedMaximumBitrate{
       Bitrate: float32(bitrate),
   }

   // BAD: Round-trip conversion
   bitrate := estimator.GetEstimate()
   remb := &rtcp.ReceiverEstimatedMaximumBitrate{
       Bitrate: float32(bitrate),
   }
   estimator.SetEstimate(uint64(remb.Bitrate))  // WRONG
   ```

3. **Test precision preservation:**
   ```bash
   go test -v -run=TestBuildREMB_BitrateEncodingPrecision
   ```

**Risk:** LOW - current architecture doesn't round-trip, just one-way conversion for REMB

---

### Medium 4: Marshal/MarshalTo Allocation Patterns

**What breaks:** Using `rtcp.Marshal()` allocates a new buffer every time. For high-frequency REMB sending, should use `MarshalSize()` + `MarshalTo()` with pooled buffers.

**Where it happens:**
```go
// Pion provides two patterns:

// Pattern 1: Allocates (simple, but 1 alloc)
data, err := pkt.Marshal()

// Pattern 2: Zero-alloc (reuse buffer)
size := pkt.MarshalSize()
buf := bufferPool.Get().([]byte)
if cap(buf) < size {
    buf = make([]byte, size)
}
buf = buf[:size]
pkt.MarshalTo(buf)
```

**Why it matters:**
- REMB is sent at 1 Hz (low frequency) - 1 alloc/send is acceptable
- But if REMB frequency increases (e.g., 10 Hz), allocations become noticeable
- Current `BuildREMB` shows `1 alloc/op (24 B)` - acceptable baseline

**Prevention:**

1. **For v1.1:** Accept 1 alloc/op for REMB marshalling (only 1 Hz)

2. **For future optimization:** Implement buffer pool if REMB frequency increases:
   ```go
   var rembBufPool = sync.Pool{
       New: func() interface{} {
           return make([]byte, 0, 64)  // Typical REMB size
       },
   }

   func buildREMBZeroAlloc(pkt *rtcp.ReceiverEstimatedMaximumBitrate) []byte {
       size := pkt.MarshalSize()
       buf := rembBufPool.Get().([]byte)
       if cap(buf) < size {
           buf = make([]byte, size)
       }
       buf = buf[:size]
       pkt.MarshalTo(buf)
       return buf
   }
   ```

3. **Benchmark comparison:**
   ```bash
   # Current baseline
   BenchmarkBuildREMB-10  53202280  21.34 ns/op  24 B/op  1 allocs/op

   # After Pion migration - should be comparable
   ```

**Risk:** LOW - REMB frequency is low (1 Hz), 1 alloc/op is acceptable

**Sources:**
- [Pion RTCP MarshalSize documentation](https://pkg.go.dev/github.com/pion/rtcp) - MarshalSize returns size for zero-alloc marshaling
- [Pion performance blog](https://pion.ly/blog/sctp-and-rack/) - RACK improvements showing allocation reduction strategies

---

## Low-Severity Pitfalls

### Low 1: Test Coverage Gaps During Refactor

**What breaks:** Refactoring to Pion types might bypass existing tests if tests are too specific to implementation details.

**Prevention:**

1. **Review test suite structure:**
   - Behavior tests (should pass): Test outputs for given inputs
   - Implementation tests (may break): Test internal structures

2. **Keep behavior tests intact:** All validation tests must pass without modification

3. **Update implementation tests:** If tests check specific byte layouts or internal types, update for Pion

**Risk:** LOW - test suite is comprehensive, mostly behavior-focused

---

### Low 2: Dependency Version Pinning

**What breaks:** Pion libraries are at specific versions. Upgrading could introduce breaking changes.

**Prevention:**

1. **Pin versions in go.mod:**
   ```
   github.com/pion/rtcp v1.2.16
   github.com/pion/rtp v1.10.0
   ```

2. **Before upgrading Pion:**
   - Review changelog for breaking changes
   - Run full test suite
   - Re-run benchmarks

**Risk:** LOW - standard dependency management

---

### Low 3: Documentation Drift

**What breaks:** Code comments reference custom implementations. After migration, comments become misleading.

**Prevention:**

1. **Update comments when changing code:**
   ```go
   // OLD comment
   // BuildREMB creates a REMB RTCP packet using manual encoding

   // NEW comment
   // BuildREMB creates a REMB RTCP packet using pion/rtcp
   ```

2. **Review all docstrings:** Search for references to "custom", "manual", "hand-rolled"

**Risk:** LOW - cosmetic issue, doesn't affect behavior

---

## Prevention Strategy Summary

### Phase-Specific Recommendations

**Phase 1: REMB Marshalling Migration**
1. ✅ Replace `BuildREMB` internals with `rtcp.ReceiverEstimatedMaximumBitrate`
2. ✅ Keep REMBPacket wrapper for compatibility
3. ✅ Run all `TestBuildREMB_*` tests - MUST pass
4. ✅ Run benchmark - accept 1 alloc/op (currently 1, so no regression)
5. ✅ Verify Chrome interop - REMB must be accepted

**Phase 2: Extension Parsing (If Applicable)**
1. ⚠️ Keep custom `ParseAbsSendTime/ParseAbsCaptureTime` - already zero-alloc
2. ⚠️ Keep custom `UnwrapAbsSendTime` - validated wraparound logic
3. ✅ Use Pion's `rtp.Header.GetExtension()` - already in use, zero-alloc
4. ✅ Run timestamp wraparound tests - MUST pass

**Phase 3: Integration Validation**
1. ✅ Run full benchmark suite - verify no allocation regressions
2. ✅ Run 24-hour soak test - verify no long-term issues
3. ✅ Run Chrome interop test - verify REMB acceptance
4. ✅ Run validation test - verify estimates within 10% of reference

---

## Testing Checklist

Before marking migration complete:

### Allocation Tests
- [ ] `BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc` → 0 allocs/op
- [ ] `BenchmarkDelayEstimator_OnPacket_ZeroAlloc` → 0 allocs/op
- [ ] `BenchmarkInterArrivalCalculator_AddPacket_ZeroAlloc` → 0 allocs/op
- [ ] `BenchmarkBuildREMB` → ≤1 alloc/op (acceptable for non-hot path)
- [ ] `BenchmarkProcessRTP_Allocations` → ≤2 allocs/op (current baseline)

### Behavioral Tests
- [ ] `TestBuildREMB_BitrateEncodingPrecision` → All bitrates within 1% tolerance
- [ ] `TestUnwrapAbsSendTime` → All 15 cases pass
- [ ] `TestTimestampWraparound_24Hour` → No spurious delay spikes
- [ ] `TestAcceleratedSoak_TimestampWrap` → Wraparound handled correctly
- [ ] `TestREMBPacket_ChromeInterop` → Chrome accepts REMB (if exists)

### Integration Tests
- [ ] `TestBWEInterceptor_REMBSending` → Correct REMB rate (1 Hz)
- [ ] `TestInterceptor_FullPath` → End-to-end packet processing
- [ ] Chrome webrtc-internals shows received REMB

### Long-Duration Tests
- [ ] 24-hour soak test - no memory leaks, no estimation degradation
- [ ] Memory profile - no unexpected allocations accumulating

---

## Migration Rollback Plan

If migration introduces regressions:

1. **Keep old implementation in separate file:** `remb_legacy.go`

2. **Feature flag toggle:**
   ```go
   const usePionREMB = true  // Toggle to rollback

   func BuildREMB(...) {
       if usePionREMB {
           return buildREMBPion(...)
       }
       return buildREMBLegacy(...)
   }
   ```

3. **Revert steps:**
   - Set `usePionREMB = false`
   - Run tests to confirm legacy behavior restored
   - Investigate regression before retry

---

## Sources

**Codebase Analysis:**
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/remb.go` - Current REMB implementation using Pion types
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp.go` - Custom wraparound handling
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/benchmark_test.go` - Allocation baselines
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/validation_test.go` - Behavior validation

**Benchmark Data (2026-01-22):**
- `BenchmarkBandwidthEstimator_OnPacket_ZeroAlloc`: 0 allocs/op ✓
- `BenchmarkBuildREMB`: 1 alloc/op (24 B) - baseline
- `BenchmarkParseREMB`: 2 allocs/op (56 B) - baseline
- `BenchmarkProcessRTP_Allocations`: 2 allocs/op (104 B) - interceptor overhead

**Pion Documentation:**
- [Pion RTCP Package](https://pkg.go.dev/github.com/pion/rtcp) - ReceiverEstimatedMaximumBitrate API
- [Pion RTCP ReceiverEstimatedMaximumBitrate source](https://github.com/pion/rtcp/blob/master/receiver_estimated_maximum_bitrate.go) - Implementation details
- [Pion RTCP MarshalSize documentation](https://pkg.go.dev/github.com/pion/rtcp) - Zero-allocation marshaling pattern
- [Pion RTP Package](https://pkg.go.dev/github.com/pion/rtp) - Header parsing APIs
- [Pion SCTP performance blog](https://pion.ly/blog/sctp-and-rack/) - Allocation reduction strategies

**Test Infrastructure:**
- `.planning/phases/04-optimization-validation/04-05-PLAN.md` - 24-hour soak test specification
- `.planning/ROADMAP.md` - Phase 4 completion with soak test validation

**Confidence:** HIGH - Based on direct codebase inspection, benchmark measurements, and Pion library documentation
