# Chrome Interop Test Server

This server verifies **VALID-02: REMB packets are accepted by Chrome** and visible in `chrome://webrtc-internals`.

## Overview

The Chrome Interop Test Server creates a Pion WebRTC endpoint that:
1. Receives video from Chrome (Chrome sends, server receives)
2. Generates REMB packets using the BWE interceptor
3. Sends REMB back to Chrome for bandwidth adaptation

This end-to-end test proves that the bandwidth estimation implementation produces REMB packets that Chrome's WebRTC stack accepts and processes.

## Prerequisites

- **Chrome browser** (any recent version, 100+)
- **Go 1.21+** installed
- No additional dependencies (WebRTC dependencies are vendored)

## Quick Start

### Build and Run

```bash
# From project root
go run ./cmd/chrome-interop

# Or build a binary
go build -o chrome-interop ./cmd/chrome-interop
./chrome-interop
```

The server prints:

```
Chrome Interop Test Server
==========================
1. Open chrome://webrtc-internals in Chrome
2. Open http://localhost:8080 in another tab
3. Click "Start Call"
4. Check webrtc-internals for "remb" in inbound-rtp stats

Server ready on :8080
```

## Testing Steps

### 1. Open chrome://webrtc-internals FIRST

**Important:** Open `chrome://webrtc-internals` in Chrome **before** starting the call. WebRTC internals only captures stats for connections created after the page was opened.

### 2. Open http://localhost:8080

Open a new tab to `http://localhost:8080`. You'll see the test page with:
- "Start Call" button
- Status indicator
- Video preview (shows your camera or fake video)
- Verification instructions

### 3. Click "Start Call"

Click the button to:
1. Request camera access (or use fake media with Chrome flags)
2. Create a WebRTC connection to the Go server
3. Start sending video

### 4. Wait for "Connected" status

The status should change from "Connecting..." to "Connected! Check webrtc-internals for REMB stats".

### 5. Verify in webrtc-internals

In the `chrome://webrtc-internals` tab:

1. Find the PeerConnection entry (shows URL like `http://localhost:8080`)
2. Click to expand the connection details
3. Scroll to **Stats Tables**
4. Find the **outbound-rtp** section (Chrome's sender stats)
5. Look for these indicators:
   - `availableOutgoingBitrate` - Bandwidth estimate from REMB
   - `retransmittedBytesSent` - May change if BWE triggers quality changes

### 6. Check Server Logs

The server logs show REMB packets being sent:

```
2024/01/22 12:00:01 Received video track: codec=video/VP8, ssrc=123456789
2024/01/22 12:00:02 REMB sent: estimate=500000 bps, ssrcs=[123456789]
2024/01/22 12:00:03 REMB sent: estimate=750000 bps, ssrcs=[123456789]
2024/01/22 12:00:04 REMB sent: estimate=1200000 bps, ssrcs=[123456789]
```

## Expected Results

### Server Side

- Console shows "REMB sent" messages at ~1 second intervals
- Estimates should be reasonable (100 kbps to 5 Mbps range)
- Estimates may vary as the algorithm adapts to network conditions

### Browser Side

- `chrome://webrtc-internals` shows the PeerConnection
- Stats update in real-time (refresh every second)
- `availableOutgoingBitrate` reflects received REMB values
- No WebRTC errors in browser console

## Chrome Launch Flags

For testing without a real camera, launch Chrome with fake media:

### macOS

```bash
/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome \
  --use-fake-ui-for-media-stream \
  --use-fake-device-for-media-stream
```

### Linux

```bash
google-chrome \
  --use-fake-ui-for-media-stream \
  --use-fake-device-for-media-stream
```

### Windows

```cmd
"C:\Program Files\Google\Chrome\Application\chrome.exe" ^
  --use-fake-ui-for-media-stream ^
  --use-fake-device-for-media-stream
```

These flags:
- `--use-fake-ui-for-media-stream`: Auto-allow camera access (no permission prompt)
- `--use-fake-device-for-media-stream`: Use a synthetic video pattern instead of real camera

## Troubleshooting

### "No video" or camera permission denied

**Solution:** Use Chrome flags above, or manually allow camera access when prompted.

### Connection failed

**Possible causes:**
- Port 8080 already in use: `lsof -i :8080` to check
- Firewall blocking: Temporarily disable or allow :8080

**Solution:**
```bash
# Kill process using port 8080
kill $(lsof -t -i :8080)
# Or use a different port (requires code change)
```

### No REMB visible in webrtc-internals

**Possible causes:**
- webrtc-internals opened AFTER the call started
- Looking at wrong stats section

**Solution:**
1. Close the call
2. Refresh webrtc-internals page
3. Start a new call
4. Look in **outbound-rtp** (sender) not inbound-rtp (receiver)

### Server logs show no "REMB sent" messages

**Possible causes:**
- Connection not fully established
- No video track received

**Solution:**
- Check "Connection state" logs in server output
- Ensure video is actually being captured (check browser preview)
- Check browser console for errors

## VALID-02 Pass Criteria

The test passes when ALL of these are true:

- [ ] Server starts without errors on :8080
- [ ] Connection established (status shows "Connected")
- [ ] Server logs show "REMB sent" messages with non-zero estimates
- [ ] `chrome://webrtc-internals` shows the PeerConnection
- [ ] Stats show `availableOutgoingBitrate` or similar bandwidth indication
- [ ] No REMB-related errors in browser or server console

## Architecture

```
+----------------+         HTTP POST /offer        +------------------+
|    Chrome      | -----------------------------> |   Go Server      |
|    Browser     |                                |                  |
|                | <----------------------------- |  Pion WebRTC     |
|  RTCPeerConn   |         SDP Answer             |  PeerConnection  |
|                |                                |                  |
|   Sends VP8    | =============================> |  Receives VP8    |
|   video        |         RTP (video)            |  video track     |
|                |                                |                  |
|   Receives     | <============================= |  BWE Interceptor |
|   REMB         |         RTCP (REMB)            |  sends REMB      |
+----------------+                                +------------------+
```

The BWE Interceptor:
1. Observes incoming RTP packets
2. Extracts abs-send-time extension
3. Feeds timing data to BandwidthEstimator
4. Generates REMB packets at 1 second intervals
5. Sends REMB via RTCP writer

## Related Documentation

- [GCC Algorithm](../../docs/gcc.md) - Google Congestion Control details
- [Pion Interceptors](https://github.com/pion/interceptor) - Interceptor framework
- [chrome://webrtc-internals](https://webrtc.github.io/webrtc-org/testing/) - WebRTC debugging
