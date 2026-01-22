---
phase: 04-optimization-validation
plan: 02
subsystem: validation
tags: [testing, validation, divergence, reference-trace]
requires:
  - 02-06 (BandwidthEstimator API)
provides:
  - Reference trace replay infrastructure
  - VALID-01 divergence test harness
  - Synthetic trace generation
affects:
  - 04-03 (further validation testing)
  - Future Chrome RTC event log integration
tech-stack:
  added: []
  patterns:
    - PacketProcessor callback pattern (avoids import cycles)
    - Synthetic trace generation for testing without reference data
key-files:
  created:
    - pkg/bwe/testutil/reference_trace.go
    - pkg/bwe/validation_test.go
    - testdata/reference_congestion.json
  modified: []
decisions:
  - "Use PacketProcessor callback interface to avoid import cycles between bwe and testutil"
  - "Synthetic traces skip strict VALID-01 threshold checks but still exercise infrastructure"
  - "Reference estimates of 0 are skipped in divergence calculation (warmup period)"
metrics:
  duration: 9 min
  completed: 2026-01-22
---

# Phase 04 Plan 02: Reference Trace Validation Summary

Reference trace replay infrastructure and VALID-01 divergence test harness for comparing bandwidth estimates against libwebrtc reference values.

## What Was Built

### Reference Trace Infrastructure (`pkg/bwe/testutil/reference_trace.go`)

1. **TracedPacket struct**
   - `ArrivalTimeUs`: Packet arrival time in microseconds
   - `SendTime`: 24-bit abs-send-time from RTP header
   - `Size`: Packet size in bytes
   - `SSRC`: Stream identifier
   - `ReferenceEstimate`: Expected estimate from libwebrtc (0 = unknown)

2. **ReferenceTrace struct**
   - `Name`: Trace identifier
   - `Description`: Network scenario description
   - `Packets`: Ordered list of traced packets

3. **LoadTrace(path)**: Load reference trace from JSON file

4. **Replay(processor, clock)**: Replay trace through any estimator
   - Uses `PacketProcessor` callback to avoid import cycles
   - Advances mock clock to match packet arrival times
   - Returns slice of bandwidth estimates

5. **CalculateDivergence(estimates, trace, warmup)**: Compare estimates
   - Skips warmup packets (estimator convergence period)
   - Skips packets with ReferenceEstimate = 0
   - Returns max/avg divergence percentages

6. **GenerateSyntheticTrace(count, interval, size, ssrc)**: Generate test traces
   - Phase 1 (40%): Stable network
   - Phase 2 (30%): Congestion (increasing delay)
   - Phase 3 (30%): Recovery (decreasing delay)

### Sample Reference Trace (`testdata/reference_congestion.json`)

- 500 packets over ~10 seconds at 20ms intervals
- Synthetic reference estimates (placeholders for real libwebrtc data)
- Congestion scenario with stable, congestion, recovery phases
- Ready for replacement with Chrome RTC event log data

### Divergence Validation Tests (`pkg/bwe/validation_test.go`)

1. **TestEstimateDivergence_ReferenceComparison**
   - Loads reference trace from testdata
   - Replays through BandwidthEstimator
   - Calculates divergence against reference estimates
   - VALID-01: Max divergence < 10% (enforced for real data only)
   - Detects synthetic traces and skips strict validation

2. **TestEstimateDivergence_GeneratedTrace**
   - Uses programmatically generated traces
   - Verifies behavior patterns (positive estimates, sanity checks)
   - Logs congestion response observations

3. **TestEstimateDivergence_InfrastructureValidation**
   - Validates LoadTrace functionality
   - Validates packet structure
   - Validates arrival time monotonicity
   - Validates CalculateDivergence with known values

## Verification Results

```
VALID-01 Infrastructure Ready:
- reference_trace.go exists with LoadTrace, Replay, CalculateDivergence
- Sample reference trace JSON exists in testdata/
- validation_test.go exists with divergence tests
- Tests pass (skip strict validation for synthetic data)
- All existing tests still pass
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Import cycle between bwe and testutil**
- **Found during:** Task 1 initial implementation
- **Issue:** testutil imported bwe for Replay method signature, creating cycle
- **Fix:** Introduced PacketProcessor callback interface, testutil no longer imports bwe
- **Files modified:** pkg/bwe/testutil/reference_trace.go
- **Commit:** 9900540

## How to Use Real Reference Data

When real libwebrtc comparison data becomes available:

1. **Capture Chrome RTC event logs:**
   - Go to `chrome://webrtc-internals` during a call
   - Download the event log after the call

2. **Extract bandwidth estimates:**
   ```bash
   rtc_event_log_visualizer --input=rtc_event.log --output=estimates.json
   ```

3. **Convert to trace format:**
   - Map Chrome packet data to `TracedPacket` structure
   - Include actual Chrome bandwidth estimates as `ReferenceEstimate`
   - Remove "synthetic" or "placeholder" from description

4. **Run validation:**
   - Test will now enforce VALID-01 threshold (10% max divergence)
   - Adjust warmup period if needed based on trace characteristics

## Technical Notes

- **Warmup period**: First 20% of packets skipped (estimator convergence)
- **Reference estimate 0**: Indicates no comparison value available
- **Divergence formula**: `abs(our - ref) / ref * 100%`
- **Synthetic detection**: Traces with "synthetic" or "placeholder" in description

## Commits

| Task | Commit | Description |
|------|--------|-------------|
| 1 | 9f0ee46 | Reference trace replay infrastructure |
| 2 | 971f050 | Sample reference trace JSON |
| 3 | 9900540 | VALID-01 divergence tests |

## Next Phase Readiness

**Ready for:**
- 04-03: Additional validation testing
- Real Chrome RTC event log integration when data available
- VALID-01 compliance verification with actual libwebrtc data

**Prerequisites satisfied:**
- Trace loading and replay infrastructure functional
- Divergence calculation verified with known values
- Test harness ready for real reference data
