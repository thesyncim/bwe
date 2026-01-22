# Features Research: Pion Types for BWE Refactoring

**Domain:** RTP/RTCP packet handling for BWE
**Researched:** 2026-01-22
**Confidence:** HIGH

## Executive Summary

This research compares Pion's RTCP REMB and RTP extension types against the current BWE implementation to identify what functionality they provide, edge cases they handle, and any gaps. The goal is to inform the refactoring decision for adopting Pion types.

**Key Finding:** Pion types provide robust, battle-tested implementations with comprehensive validation. Current BWE code is already using Pion for REMB but has custom timestamp parsing. Pion's extension types offer significant edge case handling that the current implementation may lack.

---

## REMB Types (pion/rtcp)

### Current BWE Implementation

**Location:** `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/remb.go`

**API Surface:**
- `BuildREMB(senderSSRC, bitrateBps, mediaSSRCs)` - Creates marshaled REMB packet
- `ParseREMB(data)` - Parses REMB packet to custom `REMBPacket` struct
- `REMBPacket.Marshal()` - Wrapper convenience method

**Implementation:** Thin wrapper around `pion/rtcp.ReceiverEstimatedMaximumBitrate`
- Converts `uint64` bitrate to `float32` for Pion
- Wraps Pion struct in custom `REMBPacket` type
- Already delegates all marshaling/unmarshaling to Pion

### Pion REMB API

**Package:** `github.com/pion/rtcp v1.2.16`

**Struct Definition:**
```go
type ReceiverEstimatedMaximumBitrate struct {
    SenderSSRC uint32   // SSRC of sender
    Bitrate    float32  // Estimated maximum bitrate
    SSRCs      []uint32 // SSRC entries which this packet applies to
}
```

**Methods:**
| Method | Purpose | Edge Cases Handled |
|--------|---------|-------------------|
| `Marshal()` | Serialize to bytes | Auto buffer allocation |
| `MarshalSize()` | Get size before alloc | Returns `20 + 4*len(SSRCs)` |
| `MarshalTo(buf)` | Serialize to existing buffer | Bitrate clamping, exponent overflow protection |
| `Unmarshal(buf)` | Deserialize from bytes | 9 validation checks (see below) |
| `Header()` | Get RTCP header | Format=15, Type=206 |
| `String()` | Human-readable format | Unit conversion (b/s to Kb/s, Mb/s) |
| `DestinationSSRC()` | Get SSRC list | Returns []uint32 |

**Edge Cases Handled by Pion:**

1. **Unmarshal Validation (9 checks):**
   - Minimum 20 bytes required (`errPacketTooShort`)
   - Version must equal 2 (`errBadVersion`)
   - Padding must be unset (`errWrongPadding`)
   - Format value must be 15 (`errWrongFeedbackType`)
   - Payload type must be 206 (`errWrongPayloadType`)
   - Media source SSRC must be 0 (`errSSRCMustBeZero`)
   - Identifier must be "REMB" (`errMissingREMBidentifier`)
   - SSRC count must match packet length (`errSSRCNumAndLengthMismatch`)
   - Header size validation (`errHeaderTooSmall`)

2. **Marshal Edge Cases:**
   - Bitrate clamping to max `0x3FFFFp+63`
   - Rejects negative bitrate values (`errInvalidBitrate`)
   - Exponent overflow protection (exponent >= 64 rejected)
   - Mantissa max: `0x7FFFFF` (18-bit)
   - Buffer size enforcement (`errWrongMarshalSize`)

3. **Bitrate Encoding:**
   - Uses mantissa+exponent format per REMB spec
   - 6-bit exponent, 18-bit mantissa
   - Handles floating point to fixed-point conversion correctly

### Current BWE vs Pion REMB

| Feature | Current BWE | Pion |
|---------|-------------|------|
| Basic marshal/unmarshal | Uses Pion underneath | Native implementation |
| Bitrate type | `uint64` wrapper | `float32` (spec-compliant) |
| Validation | Delegated to Pion | 9 comprehensive checks |
| Edge case handling | Via Pion | Bitrate clamping, overflow protection |
| Convenience | Custom `REMBPacket` wrapper | Direct struct usage |
| Testing | Minimal (delegates to Pion) | Battle-tested in production |

**Verdict:** Current implementation is already a thin wrapper. Could be replaced entirely by Pion type with minimal changes.

---

## RTP Extension Types (pion/rtp)

### Current BWE Implementation

**Locations:**
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp.go` - Custom parsing
- `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/interceptor/extension.go` - Extension ID lookup

**API Surface:**

**Abs-Send-Time (24-bit, 6.18 fixed-point):**
- `ParseAbsSendTime(data []byte) (uint32, error)` - Parse 3-byte big-endian
- `AbsSendTimeToDuration(value) time.Duration` - Convert to duration
- `UnwrapAbsSendTime(prev, curr) int64` - Handle 64-second wraparound
- `UnwrapAbsSendTimeDuration(prev, curr) time.Duration` - Wraparound + conversion

**Abs-Capture-Time (64-bit, UQ32.32 format):**
- `ParseAbsCaptureTime(data []byte) (uint64, error)` - Parse 8-byte big-endian
- `AbsCaptureTimeToDuration(value) time.Duration` - Convert UQ32.32 to duration
- `UnwrapAbsCaptureTime(prev, curr) int64` - Simple signed diff (no wrap concern)
- `UnwrapAbsCaptureTimeDuration(prev, curr) time.Duration` - Diff + conversion

**Extension ID Discovery:**
- `FindExtensionID(exts, uri) uint8` - Linear search for URI
- `FindAbsSendTimeID(exts) uint8` - Convenience for abs-send-time
- `FindAbsCaptureTimeID(exts) uint8` - Convenience for abs-capture-time
- URIs: `AbsSendTimeURI`, `AbsCaptureTimeURI` constants

**Edge Cases Handled by Current Code:**

1. **Parsing Validation:**
   - Abs-send-time: Requires minimum 3 bytes, returns `ErrInvalidAbsSendTime`
   - Abs-capture-time: Requires minimum 8 bytes, returns `ErrInvalidAbsCaptureTime`
   - Handles nil input gracefully
   - Extra bytes ignored (only reads required bytes)

2. **Wraparound Handling (Abs-Send-Time):**
   - Half-range comparison (32-second threshold)
   - Detects forward wraparound (apparent negative delta > 32s)
   - Detects backward wraparound (apparent positive delta > 32s)
   - Correct signed delta calculation
   - Well-tested with 15 test cases covering boundary conditions

3. **Tested Edge Cases:**
   - Zero value
   - Minimum/maximum values
   - Boundary crossings (0 ↔ max)
   - Exactly half-range jumps
   - Extra bytes ignored
   - Short/empty/nil inputs

### Pion RTP Extension API

**Package:** `github.com/pion/rtp v1.10.0`

#### 1. AbsSendTimeExtension

**Struct:**
```go
type AbsSendTimeExtension struct {
    Timestamp uint64  // 24-bit timestamp stored in uint64
}
```

**Constants:**
- `absSendTimeExtensionSize = 3` bytes

**Methods:**
| Method | Purpose | Edge Cases |
|--------|---------|-----------|
| `NewAbsSendTimeExtension(time.Time)` | Factory from time | Converts to NTP, shifts right 14 bits |
| `Marshal()` | Serialize to 3 bytes | Auto-allocates buffer |
| `MarshalTo(buf)` | Serialize to buffer | Returns `io.ErrShortBuffer` if <3 bytes |
| `MarshalSize()` | Returns 3 | Fixed size |
| `Unmarshal(rawData)` | Deserialize | Rejects if <3 bytes |
| `Estimate(receive time.Time)` | Reconstruct absolute time | Assumes transmission delay ≤64s |

**Encoding:**
- Big-endian 24-bit: `(data[0]<<16) | (data[1]<<8) | data[2]`
- Same as current BWE implementation

**Additional Functionality Not in Current BWE:**
1. **Factory from `time.Time`:** `NewAbsSendTimeExtension(sendTime)` - converts Go time to NTP
2. **Estimate method:** Reconstructs absolute send time from receive timestamp
   - Combines 24-bit timestamp with upper 34 bits of receive NTP time
   - Adjusts if result exceeds receive time
   - Assumes transmission delay <64 seconds
3. **NTP helpers:** `toNtpTime()`, `toTime()` for epoch conversion
4. **Built-in validation:** Marshal/Unmarshal error on wrong sizes

**What Current BWE Has That Pion Doesn't:**
1. **Wraparound delta calculation:** `UnwrapAbsSendTime()` with half-range logic
2. **Duration conversion helpers:** `AbsSendTimeToDuration()`, `UnwrapAbsSendTimeDuration()`
3. **Domain constants:** `AbsSendTimeMax`, `AbsSendTimeResolution`

**Verdict:** Pion provides time reconstruction, current BWE provides delta calculation. Both are needed for BWE use case.

#### 2. AbsCaptureTimeExtension

**Struct:**
```go
type AbsCaptureTimeExtension struct {
    Timestamp                    uint64   // UQ32.32 format
    EstimatedCaptureClockOffset  *int64   // Optional clock offset
}
```

**Constants:**
- `absCaptureTimeExtensionSize = 8` bytes (minimum)
- `absCaptureTimeExtendedExtensionSize = 16` bytes (with clock offset)

**Methods:**
| Method | Purpose | Edge Cases |
|--------|---------|-----------|
| `NewAbsCaptureTimeExtension(time.Time)` | Factory from time | Converts to UQ32.32 |
| `NewAbsCaptureTimeExtensionWithCaptureClockOffset(time.Time, duration)` | Factory with offset | Handles negative offsets |
| `Marshal()` | Serialize | 8 or 16 bytes depending on offset presence |
| `MarshalTo(buf)` | Serialize to buffer | `io.ErrShortBuffer` if insufficient space |
| `MarshalSize()` | Returns 8 or 16 | Based on offset presence |
| `Unmarshal(rawData)` | Deserialize | Requires ≥8 bytes, optionally reads 16 |
| `CaptureTime()` | Convert to `time.Time` | Uses internal `toTime()` helper |
| `EstimatedCaptureClockOffsetDuration()` | Convert offset to duration | Handles negative values, bit-shifting for fractional seconds |

**Encoding:**
- 64-bit big-endian UQ32.32: upper 32 bits = seconds, lower 32 bits = fraction
- Optional 64-bit clock offset in bytes 8-15
- Unmarshal checks length to determine if offset present

**Additional Functionality Not in Current BWE:**
1. **Clock offset support:** Second field for inter-stream sync
2. **Factory methods:** Create from `time.Time` and offset duration
3. **CaptureTime() method:** Direct conversion to Go time
4. **EstimatedCaptureClockOffsetDuration():** Convert offset to duration
5. **Variable-length encoding:** 8 vs 16 bytes based on offset presence

**What Current BWE Has That Pion Doesn't:**
1. **Duration conversion:** `AbsCaptureTimeToDuration()`
2. **Delta calculation:** `UnwrapAbsCaptureTime()`, `UnwrapAbsCaptureTimeDuration()`
3. **Domain constant:** `AbsCaptureTimeResolution`

**Verdict:** Pion provides richer feature set (clock offset, time conversion). Current BWE focuses on delta calculation for delay estimation.

#### 3. Generic Extension APIs (rtp.Header)

**Methods:**
| Method | Signature | Behavior |
|--------|-----------|----------|
| `GetExtension(id uint8)` | `[]byte` | Returns payload or `nil` if missing/disabled |
| `GetExtensionIDs()` | `[]uint8` | Returns all IDs or `nil` if none |
| `SetExtension(id, payload)` | `error` | Validates, updates existing or appends new |

**Validation in SetExtension:**
- Calls `headerExtensionCheck()` to validate payload vs profile
- Auto-selects profile if no extensions exist:
  - ≤16 bytes → OneByte profile
  - >16 and <256 bytes → TwoByte profile
- Updates existing extension if ID matches
- Appends new extension otherwise

**Edge Cases:**
- All methods gracefully return `nil` for missing extensions (no errors)
- No validation on Get operations
- Linear search through Extensions slice (fine for small lists)
- Recent fixes (2025-2026): Off-by-one errors in OneByte/TwoByte Set methods (v4.2.0)

**Known Limitations:**
- SetExtension doesn't upgrade profile from OneByte to TwoByte (pion/rtp#249)

**What Current BWE Does:**
- Uses Pion's `GetExtension()` via `packet.GetExtension(extensionID)`
- Then manually parses bytes with `ParseAbsSendTime()` or `ParseAbsCaptureTime()`
- Extension ID discovery via custom `FindExtensionID()` helper

---

## Comparison Matrix: Current BWE vs Pion

### REMB Functionality

| Capability | Current BWE | Pion RTCP | Recommendation |
|------------|-------------|-----------|----------------|
| Marshal/Unmarshal | Delegates to Pion | Native | **Replace wrapper with direct Pion usage** |
| Bitrate type | uint64 wrapper | float32 (spec) | **Adopt float32** (simpler, spec-compliant) |
| Validation | Via Pion | 9 checks | **Already using Pion's validation** |
| Edge cases | Via Pion | Clamping, overflow | **Already covered** |
| Convenience | Custom wrapper | Direct struct | **Eliminate wrapper** (unnecessary abstraction) |

### Abs-Send-Time Functionality

| Capability | Current BWE | Pion RTP | Gap Analysis |
|------------|-------------|----------|--------------|
| Parse 3-byte payload | `ParseAbsSendTime()` | `Unmarshal()` | **Equivalent** - same big-endian logic |
| Timestamp storage | `uint32` (24-bit) | `uint64` (stores 24-bit) | **Pion has unnecessary overhead** |
| Create from time.Time | Not supported | `NewAbsSendTimeExtension()` | **Pion advantage** - useful for testing |
| Convert to duration | `AbsSendTimeToDuration()` | Not supported | **BWE advantage** - needed for delay calc |
| Wraparound detection | `UnwrapAbsSendTime()` | Not supported | **BWE critical feature** - 64s wraparound |
| Reconstruct absolute time | Not supported | `Estimate(receive)` | **Pion feature** - not needed for BWE |
| NTP conversion | Not supported | `toNtpTime()`, `toTime()` | **Pion internal** - not exposed |
| Validation | Manual length check | `Unmarshal()` error | **Equivalent** |

**Recommendation:**
- **Keep current parsing logic** - it's simpler (uint32 not uint64) and includes critical wraparound handling
- **Could adopt** Pion's factory method for test utilities
- **Cannot replace** without implementing wraparound logic on top of Pion

### Abs-Capture-Time Functionality

| Capability | Current BWE | Pion RTP | Gap Analysis |
|------------|-------------|----------|--------------|
| Parse 8-byte payload | `ParseAbsCaptureTime()` | `Unmarshal()` | **Equivalent** - same UQ32.32 logic |
| Timestamp storage | `uint64` | `uint64` | **Same** |
| Clock offset support | Not supported | `EstimatedCaptureClockOffset` field | **Pion advantage** - future-proof |
| Create from time.Time | Not supported | Factory methods | **Pion advantage** - useful for testing |
| Convert to time.Time | Not supported | `CaptureTime()` | **Pion advantage** - cleaner API |
| Convert to duration | `AbsCaptureTimeToDuration()` | Not supported | **BWE advantage** - needed for delay calc |
| Delta calculation | `UnwrapAbsCaptureTime()` | Not supported | **BWE advantage** - needed for BWE |
| Offset to duration | Not supported | `EstimatedCaptureClockOffsetDuration()` | **Pion feature** - not used in BWE |
| Variable-length marshal | Not supported | 8 or 16 bytes based on offset | **Pion advantage** - handles extension |
| Validation | Manual length check | `Unmarshal()` length check | **Equivalent** |

**Recommendation:**
- **Consider hybrid approach** - use Pion for parsing (cleaner API, clock offset support), add duration helpers
- **Benefit:** Clock offset field available for future multi-stream sync
- **Cost:** Need to implement delta calculation on top of Pion types

### Extension ID Discovery

| Capability | Current BWE | Pion RTP | Gap Analysis |
|------------|-------------|----------|--------------|
| Find ID by URI | `FindExtensionID()` | Not provided | **BWE custom** - needed |
| URI constants | `AbsSendTimeURI`, `AbsCaptureTimeURI` | Not provided | **BWE custom** - convenience |
| Convenience helpers | `FindAbsSendTimeID()`, etc | Not provided | **BWE custom** - nice to have |

**Recommendation:** **Keep current extension ID helpers** - Pion doesn't provide this abstraction.

---

## Edge Cases: Detailed Comparison

### 1. REMB Packet Validation

**Pion RTCP Handles:**
- Minimum packet size (20 bytes)
- RTCP version validation (must be 2)
- Padding bit validation (must be unset)
- Feedback message type (must be 15)
- Payload type (must be 206)
- Media source SSRC (must be 0)
- REMB identifier string ("REMB")
- SSRC count vs packet length consistency
- Bitrate overflow (clamps to max)
- Negative bitrate rejection
- Exponent overflow (>=64)

**Current BWE:** Fully delegates to Pion, so all above are handled.

**Missing in Current BWE:** Direct access to validation errors (wrapped by Pion).

### 2. Abs-Send-Time Wraparound

**Current BWE Handles:**
- 64-second wraparound detection using half-range comparison
- Forward wrap: timestamp jumps from ~16777000 → 200 (apparent -32s, actually +0.001s)
- Backward wrap: timestamp jumps from 200 → 16777000 (apparent +32s, actually -0.001s)
- Exactly half-range edge case (8388608 units = 32s)
- Boundary crossing (16777215 ↔ 0)

**Pion Does NOT Handle:** No wraparound logic in `AbsSendTimeExtension`. Only parses/encodes raw value.

**Testing Coverage:**
- Current BWE: 15 test cases covering all wraparound scenarios
- Pion: No wraparound tests (not its responsibility)

**Verdict:** **Critical feature missing from Pion.** Current BWE implementation is essential.

### 3. Timestamp Precision

**Abs-Send-Time:**
- Resolution: 1/2^18 seconds = ~3.8 microseconds
- Both implementations handle identically
- Current BWE has explicit `AbsSendTimeResolution` constant

**Abs-Capture-Time:**
- Resolution: 1/2^32 seconds = ~0.23 nanoseconds
- Both implementations handle identically
- Current BWE has explicit `AbsCaptureTimeResolution` constant

### 4. Extension Profile Handling

**Pion Handles:**
- OneByte vs TwoByte profile validation
- Auto-selection based on payload size
- Recent bug fixes (2025-2026) for off-by-one errors

**Current BWE:** Doesn't deal with profiles directly - gets raw bytes from `GetExtension()`.

**Known Pion Limitation:** Cannot upgrade profile from OneByte to TwoByte when adding larger extensions.

### 5. Missing Extension Behavior

**Pion:**
- `GetExtension()` returns `nil` if extension missing
- `GetExtensionIDs()` returns `nil` if no extensions
- No errors thrown for missing extensions

**Current BWE:**
- `FindExtensionID()` returns 0 if URI not found
- ID 0 is invalid per RFC 5285
- Caller checks ID != 0 before using

**Both approaches:** Graceful degradation, no crashes on missing extensions.

---

## Limitations Identified

### Pion Limitations

1. **No wraparound handling for abs-send-time**
   - Critical for BWE use case
   - Would need to implement on top of Pion types

2. **No duration conversion helpers**
   - Must convert timestamps to durations for delay calculation
   - Would need to implement helper functions

3. **No delta calculation**
   - BWE needs time deltas between packets
   - `UnwrapAbsSendTime()` logic would need porting

4. **No extension ID discovery**
   - Pion doesn't provide URI → ID lookup
   - Would need to keep custom `FindExtensionID()` helpers

5. **AbsSendTime uses uint64 for 24-bit value**
   - Wastes 40 bits per timestamp
   - Current BWE uses uint32 (still wastes 8 bits, but less)

6. **Extension profile upgrade limitation**
   - Cannot upgrade from OneByte to TwoByte profile
   - Not an issue for BWE (read-only usage)

### Current BWE Limitations

1. **No clock offset support for abs-capture-time**
   - Pion provides this for multi-stream sync
   - Could be useful for future enhancements

2. **No factory methods for testing**
   - Pion's `NewAbsSendTimeExtension(time.Time)` is cleaner for tests
   - Current tests use raw byte arrays

3. **No time reconstruction for abs-send-time**
   - Pion's `Estimate()` method reconstructs absolute time
   - Not needed for BWE, but could be useful for debugging

4. **Manual parsing vs typed structs**
   - Current code gets raw bytes, then parses manually
   - Pion provides typed extension structs

---

## Recommendations

### REMB: Full Adoption

**Action:** Replace `REMBPacket` wrapper with direct `pion/rtcp.ReceiverEstimatedMaximumBitrate` usage.

**Rationale:**
- Already delegating everything to Pion
- Wrapper adds no value
- Converting uint64 ↔ float32 is unnecessary indirection
- Adopting Pion's float32 is more spec-compliant

**Migration:**
- Change function signatures to use `float32` for bitrate
- Remove `REMBPacket` struct
- Update callers to use Pion struct directly

**Risk:** Low - minimal API surface, well-tested in Pion

### Abs-Send-Time: Keep Current Implementation

**Action:** Keep current parsing and wraparound logic. Optionally adopt Pion's factory for testing.

**Rationale:**
- Critical wraparound handling not in Pion
- Duration conversion helpers are BWE-specific
- Current implementation is well-tested (15 test cases)
- uint32 storage is more efficient than Pion's uint64

**Potential Enhancement:**
- Add `NewAbsSendTimeFromTime(time.Time) uint32` helper inspired by Pion
- Useful for test packet generation

**Risk:** None - keeping battle-tested code

### Abs-Capture-Time: Hybrid Approach

**Action:** Consider using Pion's `AbsCaptureTimeExtension` for parsing, add delta/duration helpers.

**Rationale:**
- Pion provides clock offset field (future-proof)
- Pion has cleaner time conversion API
- Need to add delta calculation anyway
- Variable-length encoding handled by Pion

**Migration Path:**
1. Adopt Pion struct for parsing
2. Implement `UnwrapAbsCaptureTime()` using Pion's Timestamp field
3. Implement `AbsCaptureTimeToDuration()` using Pion's `CaptureTime()`
4. Keep current helpers as methods on Pion type

**Risk:** Medium - need to verify clock offset handling doesn't break parsing

### Extension ID Discovery: Keep Current

**Action:** Keep `FindExtensionID()` and convenience helpers.

**Rationale:**
- Pion doesn't provide this abstraction
- Current implementation is simple and correct
- No advantage to replacing

**Risk:** None

---

## Sources

### Pion RTCP (ReceiverEstimatedMaximumBitrate)
- [Pion RTCP Package Documentation](https://pkg.go.dev/github.com/pion/rtcp)
- [ReceiverEstimatedMaximumBitrate Source Code](https://github.com/pion/rtcp/blob/master/receiver_estimated_maximum_bitrate.go)

### Pion RTP (Extensions)
- [Pion RTP Package Documentation](https://pkg.go.dev/github.com/pion/rtp)
- [AbsSendTimeExtension Source Code](https://github.com/pion/rtp/blob/master/abssendtimeextension.go)
- [AbsCaptureTimeExtension Source Code](https://github.com/pion/rtp/blob/master/abscapturetimeextension.go)
- [Header Extension API](https://github.com/pion/rtp/blob/master/packet.go)
- [Extension Profile Handling](https://github.com/pion/rtp/blob/master/header_extension.go)

### Pion RTP Issues & Fixes
- [Issue #249: SetExtension doesn't upgrade OneByte to TwoByte profile](https://github.com/pion/rtp/issues/249)
- [Release v4.2.0: Off-by-one fixes in extension handling](https://github.com/pion/webrtc/releases/tag/v4.2.0)

### Current BWE Implementation
- Local file: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/remb.go`
- Local file: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp.go`
- Local file: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/timestamp_test.go`
- Local file: `/Users/thesyncim/GolandProjects/bwe/pkg/bwe/interceptor/extension.go`
