---
id: "003"
type: quick
title: "Fix REMB logging in chrome-interop test server"
files_modified:
  - pkg/bwe/interceptor/interceptor.go
  - pkg/bwe/interceptor/factory.go
  - cmd/chrome-interop/server/handler.go
autonomous: true
---

<objective>
Fix REMB logging in chrome-interop test server by adding an OnREMB callback to BWEInterceptor.

Problem: The current `rembLogger` wrapper doesn't work because BWEInterceptor stores the RTCPWriter input directly and sends REMB via that stored writer, bypassing any wrapper applied to the return value.

Solution: Add an `OnREMB` callback option that gets invoked inside `maybeSendREMB` when REMB is successfully sent. The chrome-interop server uses this callback for logging instead of the broken wrapper approach.
</objective>

<context>
@pkg/bwe/interceptor/interceptor.go
@pkg/bwe/interceptor/factory.go
@cmd/chrome-interop/server/handler.go
</context>

<tasks>

<task type="auto">
  <name>Task 1: Add OnREMB callback to BWEInterceptor</name>
  <files>pkg/bwe/interceptor/interceptor.go, pkg/bwe/interceptor/factory.go</files>
  <action>
1. In interceptor.go:
   - Add `onREMB func(bitrate float32, ssrcs []uint32)` field to BWEInterceptor struct (after senderSSRC field)
   - Add `WithOnREMB(fn func(bitrate float32, ssrcs []uint32)) InterceptorOption` that sets i.onREMB
   - In `maybeSendREMB`, after successful `writer.Write(pkts, nil)` call (line 267), invoke the callback if set:
     ```go
     // Invoke callback if set
     if i.onREMB != nil {
         if remb, ok := pkts[0].(*rtcp.ReceiverEstimatedMaximumBitrate); ok {
             i.onREMB(remb.Bitrate, remb.SSRCs)
         }
     }
     ```

2. In factory.go:
   - Add `onREMB func(bitrate float32, ssrcs []uint32)` field to BWEInterceptorFactory struct
   - Add `WithFactoryOnREMB(fn func(bitrate float32, ssrcs []uint32)) FactoryOption` that sets f.onREMB
   - In `NewInterceptor`, pass `WithOnREMB(f.onREMB)` to NewBWEInterceptor if f.onREMB is not nil
  </action>
  <verify>
    `go build ./pkg/bwe/interceptor/...` compiles without errors
  </verify>
  <done>
    BWEInterceptor has OnREMB callback option, factory has WithFactoryOnREMB option, callback is invoked in maybeSendREMB after REMB is sent
  </done>
</task>

<task type="auto">
  <name>Task 2: Use OnREMB callback in chrome-interop server</name>
  <files>cmd/chrome-interop/server/handler.go</files>
  <action>
1. Remove the broken logging infrastructure:
   - Delete `rembLogger` struct (lines 216-242)
   - Delete `loggingInterceptorFactory` struct (lines 244-255)

2. Update BWE factory creation to use OnREMB callback:
   - Add state tracking for deduplication (inline with factory creation):
     ```go
     var (
         lastEstimate uint64
         estimateMu   sync.Mutex
     )
     ```
   - Add `WithFactoryOnREMB` to the factory options:
     ```go
     bweinterceptor.WithFactoryOnREMB(func(bitrate float32, ssrcs []uint32) {
         estimateMu.Lock()
         defer estimateMu.Unlock()
         if uint64(bitrate) != lastEstimate {
             log.Printf("REMB sent: estimate=%.0f bps, ssrcs=%v", bitrate, ssrcs)
             lastEstimate = uint64(bitrate)
         }
     }),
     ```

3. Replace `loggingFactory` usage with direct factory:
   - Change line 70-71 from `loggingFactory := &loggingInterceptorFactory{factory: bweFactory}; i.Add(loggingFactory)`
   - To: `i.Add(bweFactory)`
  </action>
  <verify>
    `go build ./cmd/chrome-interop/...` compiles without errors
  </verify>
  <done>
    handler.go uses OnREMB callback for logging, broken rembLogger and loggingInterceptorFactory are removed
  </done>
</task>

<task type="auto">
  <name>Task 3: Verify the fix works end-to-end</name>
  <files>-</files>
  <action>
1. Run all tests to ensure no regressions:
   `go test ./pkg/bwe/... ./cmd/chrome-interop/...`

2. Run the chrome-interop server and verify REMB logging works:
   - Start server: `go run ./cmd/chrome-interop`
   - Open browser to localhost:8080
   - Enable camera/video
   - Observe server logs for "REMB sent: estimate=..." messages
  </action>
  <verify>
    `go test ./...` passes, manual test shows REMB logging in server output
  </verify>
  <done>
    All tests pass, REMB packets are logged correctly when chrome-interop server receives video from browser
  </done>
</task>

</tasks>

<verification>
- `go build ./...` succeeds
- `go test ./...` passes
- chrome-interop server logs REMB estimates when receiving video
</verification>

<success_criteria>
- OnREMB callback added to BWEInterceptor and factory
- chrome-interop server uses callback instead of broken wrapper
- REMB packets are logged correctly during live test
</success_criteria>
