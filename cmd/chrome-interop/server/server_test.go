package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServerStartStop(t *testing.T) {
	// Create server with random port
	srv, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}

	// Start server
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	// Verify we got a real address (not :0)
	if addr == "" || addr == ":0" {
		t.Errorf("Start() returned invalid address: %q", addr)
	}
	t.Logf("Server started on %s", addr)

	// Verify Addr() returns the same address
	if got := srv.Addr(); got != addr {
		t.Errorf("Addr() = %q, want %q", got, addr)
	}

	// Verify HTTP server is responding
	url := "http://" + addr + "/"
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "BWE Chrome Interop Test") {
		t.Error("Response body doesn't contain expected HTML")
	}

	// Shutdown server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() failed: %v", err)
	}

	// Verify server is stopped (should fail to connect)
	_, err = http.Get(url)
	if err == nil {
		t.Error("Expected connection error after shutdown, but request succeeded")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Addr != ":0" {
		t.Errorf("DefaultConfig().Addr = %q, want %q", cfg.Addr, ":0")
	}
	if cfg.ReadTimeout != 30*time.Second {
		t.Errorf("DefaultConfig().ReadTimeout = %v, want %v", cfg.ReadTimeout, 30*time.Second)
	}
	if cfg.WriteTimeout != 30*time.Second {
		t.Errorf("DefaultConfig().WriteTimeout = %v, want %v", cfg.WriteTimeout, 30*time.Second)
	}
}

func TestServerDoubleStart(t *testing.T) {
	srv, err := NewServer(DefaultConfig())
	if err != nil {
		t.Fatalf("NewServer() failed: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// First start
	addr1, err := srv.Start()
	if err != nil {
		t.Fatalf("First Start() failed: %v", err)
	}

	// Second start should return same address (no error)
	addr2, err := srv.Start()
	if err != nil {
		t.Fatalf("Second Start() failed: %v", err)
	}

	if addr1 != addr2 {
		t.Errorf("Second Start() returned different address: %q vs %q", addr1, addr2)
	}
}
