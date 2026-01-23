# Quick Task 001 Summary

## Fix BWE Still Working When REMB Disabled

**Date:** 2026-01-23
**Status:** COMPLETE

## Problem

When REMB was disabled in the chrome-interop test server, Chrome's bandwidth estimation (`availableOutgoingBitrate`) still worked correctly and detected network drops. This made it impossible to validate that our REMB implementation was actually being used.

## Root Cause

The server called `webrtc.RegisterDefaultInterceptors()` which enables **TWCC (Transport-Wide Congestion Control)** feedback. Chrome's sender-side BWE uses TWCC independently of REMB, so:

- With TWCC enabled: Chrome gets bandwidth feedback via TWCC packets, regardless of REMB
- Without TWCC: Chrome must rely solely on REMB for bandwidth estimation

## Solution

Replaced `RegisterDefaultInterceptors()` with manual interceptor configuration that explicitly excludes TWCC:

**Included interceptors:**
- RTCP reports (Sender/Receiver reports)
- Stats interceptor
- Simulcast extension headers
- NACK generator and responder
- BWE interceptor with REMB

**Excluded:**
- Transport-CC (TWCC) feedback

## Files Changed

| File | Change |
|------|--------|
| `cmd/chrome-interop/server/handler.go` | Replace RegisterDefaultInterceptors with manual config excluding TWCC |

## Verification

Human verified that:
- Chrome's `availableOutgoingBitrate` now correlates with REMB estimates
- SDP contains no `transport-cc` extension or `transport-wide-cc-01` feedback type
- Disabling REMB now properly disables BWE adaptation

## Commit

`4e13282` - fix(chrome-interop): remove TWCC to isolate REMB testing
