package server

// HTMLPage is the HTML content for the browser UI.
// It provides a simple interface to start/stop WebRTC calls and verify REMB.
const HTMLPage = `<!DOCTYPE html>
<html>
<head>
    <title>BWE Chrome Interop Test</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 800px;
            margin: 50px auto;
            padding: 20px;
            background: #f5f5f5;
        }
        .container {
            background: white;
            padding: 30px;
            border-radius: 8px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
        }
        h1 { color: #333; margin-bottom: 10px; }
        .subtitle { color: #666; margin-bottom: 30px; }
        button {
            background: #4285f4;
            color: white;
            border: none;
            padding: 12px 24px;
            border-radius: 4px;
            cursor: pointer;
            font-size: 16px;
            margin-right: 10px;
        }
        button:hover { background: #3367d6; }
        button:disabled { background: #ccc; cursor: not-allowed; }
        button.stop { background: #ea4335; }
        button.stop:hover { background: #d93025; }
        #status {
            margin: 20px 0;
            padding: 15px;
            border-radius: 4px;
            font-weight: 500;
        }
        .status-waiting { background: #fff3cd; color: #856404; }
        .status-connecting { background: #cce5ff; color: #004085; }
        .status-connected { background: #d4edda; color: #155724; }
        .status-error { background: #f8d7da; color: #721c24; }
        .status-closed { background: #e2e3e5; color: #383d41; }
        #video {
            width: 100%;
            max-width: 640px;
            background: #000;
            border-radius: 4px;
            margin: 20px 0;
        }
        .instructions {
            background: #e8f4fc;
            padding: 20px;
            border-radius: 4px;
            margin-top: 20px;
        }
        .instructions h3 { margin-top: 0; color: #1a73e8; }
        .instructions ol { margin-bottom: 0; }
        .instructions code {
            background: #f1f3f4;
            padding: 2px 6px;
            border-radius: 3px;
            font-family: 'SF Mono', Consolas, monospace;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>BWE Chrome Interop Test</h1>
        <p class="subtitle">VALID-02: Verify REMB packets are accepted by Chrome</p>

        <div>
            <button id="startBtn" onclick="startCall()">Start Call</button>
            <button id="stopBtn" onclick="stopCall()" class="stop" disabled>Stop Call</button>
        </div>

        <div id="status" class="status-waiting">Status: Waiting to start</div>

        <video id="video" autoplay muted playsinline></video>

        <div class="instructions">
            <h3>Verification Steps</h3>
            <ol>
                <li>Open <code>chrome://webrtc-internals</code> in Chrome <strong>BEFORE</strong> starting the call</li>
                <li>Click "Start Call" above</li>
                <li>In webrtc-internals, find the new PeerConnection entry</li>
                <li>Expand "Stats Tables" and look for <code>inbound-rtp</code></li>
                <li>Look for <code>remb</code> or bandwidth-related stats</li>
                <li>Check server console for "REMB sent" messages</li>
            </ol>
        </div>
    </div>

    <script>
        let pc = null;
        let localStream = null;

        function setStatus(message, type) {
            const status = document.getElementById('status');
            status.textContent = 'Status: ' + message;
            status.className = 'status-' + type;
        }

        async function startCall() {
            document.getElementById('startBtn').disabled = true;
            document.getElementById('stopBtn').disabled = false;

            try {
                setStatus('Requesting camera access...', 'connecting');

                // Get user media (or fake media if launched with Chrome flags)
                localStream = await navigator.mediaDevices.getUserMedia({
                    video: { width: 640, height: 480, frameRate: 30 },
                    audio: false
                });

                // Show local video
                document.getElementById('video').srcObject = localStream;

                setStatus('Creating connection...', 'connecting');

                // Create peer connection
                pc = new RTCPeerConnection({
                    iceServers: [] // Local testing, no TURN needed
                });

                // Add tracks
                localStream.getTracks().forEach(track => {
                    pc.addTrack(track, localStream);
                });

                // Handle ICE candidates
                pc.onicecandidate = async (event) => {
                    if (event.candidate === null) {
                        // ICE gathering complete, send offer to server
                        const offer = pc.localDescription;
                        setStatus('Sending offer to server...', 'connecting');

                        try {
                            const response = await fetch('/offer', {
                                method: 'POST',
                                headers: { 'Content-Type': 'application/json' },
                                body: JSON.stringify(offer)
                            });

                            if (!response.ok) {
                                throw new Error('Server returned ' + response.status);
                            }

                            const answer = await response.json();
                            await pc.setRemoteDescription(answer);
                            setStatus('Connected! Check webrtc-internals for REMB stats', 'connected');
                        } catch (err) {
                            setStatus('Failed to connect: ' + err.message, 'error');
                            stopCall();
                        }
                    }
                };

                // Monitor connection state
                pc.onconnectionstatechange = () => {
                    console.log('Connection state:', pc.connectionState);
                    if (pc.connectionState === 'connected') {
                        setStatus('Connected! Check webrtc-internals for REMB stats', 'connected');
                    } else if (pc.connectionState === 'failed') {
                        setStatus('Connection failed', 'error');
                    } else if (pc.connectionState === 'disconnected') {
                        setStatus('Disconnected', 'closed');
                    }
                };

                // Create and set offer
                const offer = await pc.createOffer();
                await pc.setLocalDescription(offer);

            } catch (err) {
                setStatus('Error: ' + err.message, 'error');
                console.error('Error starting call:', err);
                stopCall();
            }
        }

        function stopCall() {
            if (pc) {
                pc.close();
                pc = null;
            }
            if (localStream) {
                localStream.getTracks().forEach(track => track.stop());
                localStream = null;
            }
            document.getElementById('video').srcObject = null;
            document.getElementById('startBtn').disabled = false;
            document.getElementById('stopBtn').disabled = true;
            setStatus('Call ended', 'closed');
        }
    </script>
</body>
</html>`
