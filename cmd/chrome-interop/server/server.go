// Package server provides an importable HTTP server for the Chrome interop test.
// This allows E2E tests to programmatically start/stop the server without running main().
package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Config holds server configuration options.
type Config struct {
	Addr         string        // Listen address (e.g., ":8080" or ":0" for random port)
	ReadTimeout  time.Duration // HTTP read timeout
	WriteTimeout time.Duration // HTTP write timeout
}

// DefaultConfig returns a configuration suitable for testing.
// Uses ":0" to bind to a random available port.
func DefaultConfig() Config {
	return Config{
		Addr:         ":0",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
}

// Server is an importable HTTP server for WebRTC Chrome interop testing.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
	addr       string
	mu         sync.Mutex
	running    bool
}

// NewServer creates a new server with the given configuration.
// The server is not started until Start() is called.
func NewServer(cfg Config) (*Server, error) {
	mux := http.NewServeMux()

	// Serve HTML page at root
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(HTMLPage))
	})

	// Handle WebRTC offer
	mux.HandleFunc("/offer", HandleOffer)

	httpServer := &http.Server{
		Addr:         cfg.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return &Server{
		httpServer: httpServer,
	}, nil
}

// Start begins listening and serving HTTP requests.
// Returns the actual address the server is listening on (useful when port is 0).
// This method is non-blocking - the server runs in a goroutine.
func (s *Server) Start() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return s.addr, nil
	}

	// Create listener to get actual port
	ln, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return "", fmt.Errorf("failed to listen: %w", err)
	}

	s.listener = ln
	s.addr = ln.Addr().String()
	s.running = true

	// Start serving in background
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			// Log but don't crash - server may have been shut down
		}
	}()

	return s.addr, nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	return s.httpServer.Shutdown(ctx)
}

// Addr returns the address the server is listening on.
// Returns empty string if server is not running.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}
