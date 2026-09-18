package main

import (
	"testing"
	"time"
)

func TestBackendAddress(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("BACKEND_ADDR", "")
		if got := backendAddress(); got != ":8080" {
			t.Fatalf("backendAddress() = %q, want %q", got, ":8080")
		}
	})

	t.Run("environment override", func(t *testing.T) {
		t.Setenv("BACKEND_ADDR", "127.0.0.1:18080")
		if got := backendAddress(); got != "127.0.0.1:18080" {
			t.Fatalf("backendAddress() = %q, want %q", got, "127.0.0.1:18080")
		}
	})
}

func TestNewServer(t *testing.T) {
	server := newServer("127.0.0.1:18080")

	if server.Addr != "127.0.0.1:18080" {
		t.Fatalf("Addr = %q", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("Handler is nil")
	}
	if server.ReadHeaderTimeout != 5*time.Second {
		t.Fatalf("ReadHeaderTimeout = %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != 10*time.Second {
		t.Fatalf("ReadTimeout = %s", server.ReadTimeout)
	}
	if server.WriteTimeout != 10*time.Second {
		t.Fatalf("WriteTimeout = %s", server.WriteTimeout)
	}
	if server.IdleTimeout != 60*time.Second {
		t.Fatalf("IdleTimeout = %s", server.IdleTimeout)
	}

}
