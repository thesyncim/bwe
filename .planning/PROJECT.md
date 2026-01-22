# GCC Receiver-Side Bandwidth Estimator

## What This Is

A Go port of libwebrtc's GCC (Google Congestion Control) delay-based receiver-side bandwidth estimator. It observes incoming RTP packets with absolute capture time extension, estimates available bandwidth using inter-arrival jitter analysis, and generates REMB (Receiver Estimated Maximum Bitrate) RTCP feedback packets. Designed to plug into Pion WebRTC for interop with systems expecting REMB-based congestion control.

## Core Value

Generate accurate REMB feedback that matches libwebrtc/Chrome receiver behavior — enabling interop with REMB-expecting WebRTC infrastructure.

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] Parse absolute capture time RTP header extension from incoming packets
- [ ] Compute inter-arrival time deltas (send timestamp delta vs receive timestamp delta)
- [ ] Implement Kalman filter for delay gradient estimation
- [ ] Implement overuse detector (threshold-based state machine: normal/overuse/underuse)
- [ ] Implement AIMD rate controller (Additive Increase Multiplicative Decrease)
- [ ] Generate REMB RTCP packets with estimated bandwidth
- [ ] Standalone core library (algorithm only, no Pion dependency)
- [ ] Pion interceptor adapter (wires RTP observation to RTCP generation)
- [ ] Behavior matches libwebrtc/Chrome receiver under equivalent conditions

### Out of Scope

- Loss-based bandwidth estimation — v1 focuses on delay-based only, loss-based adds complexity
- Send-side BWE / TWCC — this is specifically receiver-side estimation
- Simulcast/SVC layer selection — layer adaptation is a separate concern
- Transport-wide congestion control — not needed, using absolute capture time instead

## Context

**Motivation:** Interoperability with WebRTC systems that expect REMB feedback for bandwidth adaptation. While TWCC (Transport-Wide Congestion Control) has become the modern standard with send-side estimation, many existing SFUs and WebRTC deployments still rely on REMB. Pion doesn't provide receiver-side BWE out of the box.

**Algorithm Reference:**
- RFC 8698 (Google Congestion Control) — the specification
- libwebrtc C++ source — implementation details and tuning

**GCC Delay-Based Components:**
1. **Arrival time filter** — estimates queuing delay trend from inter-arrival times
2. **Overuse detector** — compares delay gradient against adaptive threshold
3. **Rate controller** — AIMD algorithm adjusts estimate based on detector state

**Absolute Capture Time Extension:** RTP header extension that carries sender's capture timestamp, allowing receiver to compute one-way delay variations without clock synchronization (only deltas matter).

## Constraints

- **Pure Go**: No CGO — must be portable and easy to build/deploy
- **Performance**: Must handle high packet rates efficiently (video at 60fps, audio at 50pps per stream, potentially multiple streams)
- **Pion Compatibility**: Interceptor must work with Pion's interceptor chain architecture

## Current Milestone: v1.1 Pion Type Adoption

**Goal:** Refactor BWE implementation to use Pion's native types for marshalling and extension handling — keeping behavior, delegating mechanics to battle-tested code.

**Target areas:**
- REMB marshalling → `pion/rtcp.ReceiverEstimatedMaximumBitrate`
- RTP extension parsing → Pion's extension APIs
- Timestamp handling → Pion utilities where available

**Motivation:** Reduce maintenance, better interop, prepare for upstream contribution, cleaner code

**Behavior constraint:** Minor improvements acceptable if Pion handles edge cases better

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Receiver-side over send-side | Interop requirement — target systems expect REMB | ✓ Good |
| Delay-based only for v1 | Reduce scope, loss-based can be added later | ✓ Good |
| Standalone core + interceptor adapter | Clean separation allows testing algorithm without Pion | ✓ Good |
| Adopt Pion types for v1.1 | Reduce maintenance, prepare for upstream contribution | — Pending |

---
*Last updated: 2026-01-22 after v1.0 milestone complete, starting v1.1*
