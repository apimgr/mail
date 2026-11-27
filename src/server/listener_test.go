package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/apimgr/mail/src/config"
)

func TestStartWithListenerServesProvidedSocket(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := New(&config.Config{}, nil)
	if err := srv.StartWithListener(listener); err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	client := &http.Client{Timeout: time.Second}
	response, err := client.Get("http://" + listener.Addr().String() + "/")
	if err != nil {
		t.Fatalf("request on the supplied listener: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusOK)
	}
}

func TestCreateListenerRequiresIPAndValidPort(t *testing.T) {
	if _, err := CreateListener("localhost", 8080); err == nil {
		t.Fatal("hostname listen address was accepted")
	}
	for _, port := range []int{0, -1, 65536} {
		if _, err := CreateListener("127.0.0.1", port); err == nil {
			t.Errorf("port %d was accepted", port)
		}
	}
	listener, err := CreateListener("[::1]", 0)
	if err == nil {
		_ = listener.Close()
		t.Fatal("port 0 was accepted")
	}
}
