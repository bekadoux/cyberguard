package httpapi

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServerLimits(t *testing.T) {
	t.Parallel()
	server := NewServer("127.0.0.1:8080", NewRouter(testLogger(), nil, time.Second), testLogger())
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 || server.MaxHeaderBytes <= 0 {
		t.Fatal("server limits must be configured")
	}
}

func TestServeGracefulShutdown(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	server := NewServer(listener.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	}), testLogger())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, server, listener, 2*time.Second) }()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	requested := make(chan error, 1)
	go func() {
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = errors.New("unexpected response status")
			}
		}
		requested <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-served:
		t.Fatalf("shutdown did not wait for the active request: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	// Release the handler without closing the channel twice in cleanup.
	release <- struct{}{}
	select {
	case err := <-requested:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not complete")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestServeClosesConnectionsAfterShutdownTimeout(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	server := NewServer(listener.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}), testLogger())
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- Serve(ctx, server, listener, 50*time.Millisecond) }()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	requested := make(chan struct{})
	go func() {
		defer close(requested)
		response, err := client.Get("http://" + listener.Addr().String())
		if err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-served:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected shutdown timeout, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown exceeded its timeout")
	}
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("connection was not closed")
	}
}

func TestServeReportsListenerFailure(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
	server := NewServer(listener.Addr().String(), NewRouter(testLogger(), nil, time.Second), testLogger())
	err = Serve(context.Background(), server, listener, time.Second)
	if err == nil {
		t.Fatal("expected listener error")
	}
}
