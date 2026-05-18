package oc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/guilhermehto/cogitator/internal/config"
)

func TestSubscribeEventsUnwrapsEnvelopeAndSkipsSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/global/event" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprintf(w, "data: {\"directory\":\"/tmp\",\"project\":\"p\",\"payload\":{\"id\":\"e1\",\"type\":\"sync\",\"properties\":{}}}\n\n")
		_, _ = fmt.Fprintf(w, "data: {\"directory\":\"/tmp\",\"project\":\"p\",\"payload\":{\"id\":\"e2\",\"type\":\"session.status\",\"properties\":{\"sessionID\":\"s1\",\"status\":{\"type\":\"busy\",\"message\":\"\"}}}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, config.Default())
	out := make(chan Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- c.SubscribeEvents(context.Background(), out)
	}()

	select {
	case evt := <-out:
		if evt.Type != "session.status" {
			t.Fatalf("expected sync to be filtered; got %q", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribe exit")
	}
}

func TestSubscribeEventsAcceptsFlatEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprintf(w, "data: {\"id\":\"e3\",\"type\":\"server.connected\",\"properties\":{}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, config.Default())
	out := make(chan Event, 4)
	done := make(chan error, 1)
	go func() {
		done <- c.SubscribeEvents(context.Background(), out)
	}()

	select {
	case evt := <-out:
		if evt.Type != "server.connected" {
			t.Fatalf("expected flat envelope to decode, got %q", evt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for subscribe exit")
	}
}

func TestSleepBackoffBoundedByConfig(t *testing.T) {
	cfg := config.Default()
	cfg.EventBackoffMax = 20 * time.Millisecond

	start := time.Now()
	SleepBackoff(context.Background(), cfg, 100)
	elapsed := time.Since(start)

	if elapsed > 200*time.Millisecond {
		t.Fatalf("backoff exceeded bound: %v", elapsed)
	}
}
