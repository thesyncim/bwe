// Chrome Interop Test Server
//
// This server creates a Pion WebRTC endpoint that receives video from Chrome
// and generates REMB packets for bandwidth estimation. Use this to verify
// VALID-02: REMB packets are accepted by Chrome and visible in webrtc-internals.
package main

import (
	"fmt"
	"log"

	"bwe/cmd/chrome-interop/server"
)

func main() {
	// Print welcome message
	fmt.Println(`
Chrome Interop Test Server
==========================
1. Open chrome://webrtc-internals in Chrome
2. Open http://localhost:8080 in another tab
3. Click "Start Call"
4. Check webrtc-internals for "remb" in inbound-rtp stats

Server ready on :8080`)

	// Create server with fixed port for CLI use
	cfg := server.Config{Addr: ":8080"}
	srv, err := server.NewServer(cfg)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	// Start server
	addr, err := srv.Start()
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	log.Printf("Listening on %s", addr)

	// Block forever
	select {}
}
