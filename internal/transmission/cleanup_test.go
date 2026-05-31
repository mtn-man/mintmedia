package transmission

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newBlockingRPCServer starts a test Transmission RPC server that handles the
// CSRF exchange immediately but blocks all subsequent (real) RPC calls until
// the returned unblock function is called.
//
// Callers should defer in this order so LIFO ensures the handler is unblocked
// before the server is closed:
//
//	ts, unblock := newBlockingRPCServer(t)
//	defer ts.Close()
//	defer unblock()
func newBlockingRPCServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	done := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		select {
		case <-done:
		case <-time.After(30 * time.Second):
		}
	}))
	return ts, func() { close(done) }
}

func TestRemoveCompleted_NoArgumentsKeyReturnsZero(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Omit "arguments" key entirely — some servers or proxies may do this.
		_, _ = w.Write([]byte(`{"result":"success"}`))
	}))
	defer ts.Close()

	c := &Client{Host: hostOf(ts)}
	removed, err := c.RemoveCompleted(context.Background())
	if err != nil {
		t.Fatalf("RemoveCompleted() error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestRemoveCompleted_RemovesOnlyCompletedIDs(t *testing.T) {
	torrents := []map[string]interface{}{
		{"id": 1, "percentDone": 1.0},
		{"id": 2, "percentDone": 0.99},
		{"id": 3, "percentDone": 1.0},
	}

	var mu sync.Mutex
	var removedIDs []int

	ts := newRPCServer(t, func(method string, args json.RawMessage) interface{} {
		switch method {
		case "torrent-get":
			return map[string]interface{}{"torrents": torrents}
		case "torrent-remove":
			var a struct {
				IDs []int `json:"ids"`
			}
			_ = json.Unmarshal(args, &a)
			mu.Lock()
			removedIDs = append(removedIDs, a.IDs...)
			mu.Unlock()
			return nil
		}
		return nil
	})

	c := &Client{Host: hostOf(ts)}
	removed, err := c.RemoveCompleted(context.Background())
	if err != nil {
		t.Fatalf("RemoveCompleted() error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed = %d, want 2", removed)
	}

	mu.Lock()
	ids := removedIDs
	mu.Unlock()

	if len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("removedIDs = %v, want [1, 3]", ids)
	}
}

func TestRemoveCompleted_NoCompletedReturnsZero(t *testing.T) {
	torrents := []map[string]interface{}{
		{"id": 9, "percentDone": 0.72},
	}

	ts := newRPCServer(t, func(method string, args json.RawMessage) interface{} {
		if method == "torrent-get" {
			return map[string]interface{}{"torrents": torrents}
		}
		t.Errorf("unexpected method %q; torrent-remove must not be called when none are complete", method)
		return nil
	})

	c := &Client{Host: hostOf(ts)}
	removed, err := c.RemoveCompleted(context.Background())
	if err != nil {
		t.Fatalf("RemoveCompleted() error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
}

func TestRemoveCompleted_HonorsDeadlineContext(t *testing.T) {
	// LIFO defer order: unblock() fires before ts.Close() so the handler
	// goroutine exits cleanly and the server shuts down without a 5-second hang.
	ts, unblock := newBlockingRPCServer(t)
	defer ts.Close()
	defer unblock()

	c := &Client{Host: hostOf(ts)}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	removed, err := c.RemoveCompleted(ctx)
	if err == nil {
		t.Fatal("expected deadline error, got nil")
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline") {
		t.Fatalf("expected deadline exceeded; got: %v", err)
	}
}

func TestRemoveCompleted_HonorsCanceledContext(t *testing.T) {
	ts, unblock := newBlockingRPCServer(t)
	defer ts.Close()
	defer unblock()

	c := &Client{Host: hostOf(ts)}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	removed, err := c.RemoveCompleted(ctx)
	if err == nil {
		t.Fatal("expected canceled error, got nil")
	}
	if removed != 0 {
		t.Fatalf("removed = %d, want 0", removed)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled; got: %v", err)
	}
}
