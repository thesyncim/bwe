package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/nack"
	"github.com/pion/webrtc/v4"

	bweinterceptor "github.com/thesyncim/bwe/pkg/bwe/interceptor"
)

// HandleOffer handles WebRTC offer requests from the browser.
// It creates a peer connection with BWE interceptor and returns an answer.
func HandleOffer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse incoming offer
	var offer webrtc.SessionDescription
	if err := json.NewDecoder(r.Body).Decode(&offer); err != nil {
		log.Printf("Failed to decode offer: %v", err)
		http.Error(w, "Invalid offer", http.StatusBadRequest)
		return
	}

	// Create media engine with default codecs
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		log.Printf("Failed to register codecs: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Register abs-send-time header extension for BWE
	// This MUST be registered before creating the PeerConnection so it's included in SDP negotiation.
	// Chrome will then include abs-send-time in RTP packets, which our BWE interceptor needs.
	if err := m.RegisterHeaderExtension(webrtc.RTPHeaderExtensionCapability{
		URI: "http://www.webrtc.org/experiments/rtp-hdrext/abs-send-time",
	}, webrtc.RTPCodecTypeVideo); err != nil {
		log.Printf("Failed to register abs-send-time extension: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Create interceptor registry
	i := &interceptor.Registry{}

	// State tracking for REMB logging deduplication
	var (
		lastEstimate uint64
		estimateMu   sync.Mutex
	)

	// Add BWE interceptor factory with OnREMB callback for logging
	bweFactory, err := bweinterceptor.NewBWEInterceptorFactory(
		bweinterceptor.WithInitialBitrate(500_000),          // Start at 500 kbps
		bweinterceptor.WithMinBitrate(100_000),              // Min 100 kbps
		bweinterceptor.WithMaxBitrate(5_000_000),            // Max 5 Mbps
		bweinterceptor.WithFactoryREMBInterval(time.Second), // 1 second interval
		bweinterceptor.WithFactoryOnREMB(func(bitrate float32, ssrcs []uint32) {
			estimateMu.Lock()
			defer estimateMu.Unlock()
			if uint64(bitrate) != lastEstimate {
				log.Printf("REMB sent: estimate=%.0f bps, ssrcs=%v", bitrate, ssrcs)
				lastEstimate = uint64(bitrate)
			}
		}),
	)
	if err != nil {
		log.Printf("Failed to create BWE factory: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	i.Add(bweFactory)

	// IMPORTANT: Do NOT use RegisterDefaultInterceptors or ConfigureTWCCSender.
	// Those enable TWCC feedback which allows Chrome's sender-side BWE to work
	// independently of our REMB. For proper REMB-only testing, Chrome must
	// rely solely on our receiver-side bandwidth estimates.

	// Configure RTCP reports (Sender/Receiver reports) - required for WebRTC
	if err := webrtc.ConfigureRTCPReports(i); err != nil {
		log.Printf("Failed to configure RTCP reports: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Configure stats interceptor for RTP stream statistics
	if err := webrtc.ConfigureStatsInterceptor(i); err != nil {
		log.Printf("Failed to configure stats interceptor: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Configure simulcast extension headers (needed for multi-layer video)
	if err := webrtc.ConfigureSimulcastExtensionHeaders(m); err != nil {
		log.Printf("Failed to configure simulcast headers: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Register NACK feedback types on MediaEngine for SDP negotiation
	m.RegisterFeedback(webrtc.RTCPFeedback{Type: "nack"}, webrtc.RTPCodecTypeVideo)
	m.RegisterFeedback(webrtc.RTCPFeedback{Type: "nack", Parameter: "pli"}, webrtc.RTPCodecTypeVideo)

	// Add NACK generator (receiver-side, requests retransmissions on packet loss)
	generator, err := nack.NewGeneratorInterceptor()
	if err != nil {
		log.Printf("Failed to create NACK generator: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	i.Add(generator)

	// Add NACK responder (sender-side, responds to NACK requests)
	responder, err := nack.NewResponderInterceptor()
	if err != nil {
		log.Printf("Failed to create NACK responder: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	i.Add(responder)

	// Create API with custom media engine and interceptors
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(m),
		webrtc.WithInterceptorRegistry(i),
	)

	// Create peer connection
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{}, // Local testing
	}
	peerConnection, err := api.NewPeerConnection(config)
	if err != nil {
		log.Printf("Failed to create peer connection: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Add transceiver to receive video
	_, err = peerConnection.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	)
	if err != nil {
		log.Printf("Failed to add transceiver: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Log when we receive video track
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("Received video track: codec=%s, ssrc=%d", track.Codec().MimeType, track.SSRC())

		// Log header extensions available (for debugging BWE setup)
		params := receiver.GetParameters()
		log.Printf("Header extensions for track:")
		for _, ext := range params.HeaderExtensions {
			log.Printf("  - ID=%d, URI=%s", ext.ID, ext.URI)
		}

		// Read packets to keep the stream alive and feed BWE
		go func() {
			buf := make([]byte, 1500)
			for {
				_, _, err := track.Read(buf)
				if err != nil {
					log.Printf("Track read ended: %v", err)
					return
				}
				// Packets are automatically processed by BWE interceptor
			}
		}()
	})

	// Log connection state changes
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("Connection state: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			peerConnection.Close()
		}
	})

	// Set remote description (the offer from browser)
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		log.Printf("Failed to set remote description: %v", err)
		http.Error(w, "Invalid offer", http.StatusBadRequest)
		return
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Printf("Failed to create answer: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Set local description
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		log.Printf("Failed to set local description: %v", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	// Wait for ICE gathering to complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	<-gatherComplete

	// Send answer with complete ICE candidates
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peerConnection.LocalDescription())

	log.Println("WebRTC connection established, watching for REMB packets...")
}

