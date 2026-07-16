package transmission

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newRPCServer starts a test Transmission RPC server that handles the CSRF
// session-ID handshake and dispatches each RPC call to handle.
// handle receives the method name and raw arguments JSON; its return value is
// marshalled as the "arguments" field in the success response.
func newRPCServer(t *testing.T, handle func(method string, args json.RawMessage) interface{}) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		var req struct {
			Method    string          `json:"method"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		respArgs := handle(req.Method, req.Arguments)
		argsJSON, _ := json.Marshal(respArgs)
		if argsJSON == nil {
			argsJSON = json.RawMessage(`{}`)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":"success","arguments":%s}`, argsJSON)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// hostOf returns the host:port of a test server (strips the http:// scheme).
func hostOf(ts *httptest.Server) string {
	return strings.TrimPrefix(ts.URL, "http://")
}

func TestRPCURL(t *testing.T) {
	cases := []struct {
		host string
		want string
	}{
		{"localhost:9091", "http://localhost:9091/transmission/rpc"},
		{"192.0.2.10:9091", "http://192.0.2.10:9091/transmission/rpc"},
		// path accidentally included -- must be stripped
		{"localhost:9091/transmission/rpc", "http://localhost:9091/transmission/rpc"},
		{"localhost:9091/", "http://localhost:9091/transmission/rpc"},
		// explicit scheme preserved
		{"http://localhost:9091", "http://localhost:9091/transmission/rpc"},
		{"https://localhost:9091", "https://localhost:9091/transmission/rpc"},
		// scheme + accidental path
		{"http://localhost:9091/transmission/rpc", "http://localhost:9091/transmission/rpc"},
	}
	for _, tc := range cases {
		c := &Client{Host: tc.host}
		if got := c.rpcURL(); got != tc.want {
			t.Errorf("rpcURL(%q) = %q, want %q", tc.host, got, tc.want)
		}
	}
}

func TestAddMagnet_RequiresHost(t *testing.T) {
	c := &Client{Host: ""}
	err := c.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host error, got: %v", err)
	}
}

func TestAddMagnet_RejectsMalformedAuth(t *testing.T) {
	c := &Client{Host: "localhost:9091", Auth: "tokenonly"}
	err := c.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc")
	if err == nil {
		t.Fatal("expected error for auth without colon")
	}
	if !strings.Contains(err.Error(), "user:pass") {
		t.Fatalf("expected user:pass format error, got: %v", err)
	}
}

func TestAddMagnet_RejectsNonMagnet(t *testing.T) {
	c := &Client{Host: "localhost:9091"}
	err := c.AddMagnet(context.Background(), "https://example.com/not-a-magnet")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not a magnet") {
		t.Fatalf("expected not-a-magnet error, got: %v", err)
	}
}

func TestAddMagnet_SendsTorrentAddRPC(t *testing.T) {
	var gotMethod string
	var gotFilename string

	ts := newRPCServer(t, func(method string, args json.RawMessage) interface{} {
		gotMethod = method
		var a map[string]string
		_ = json.Unmarshal(args, &a)
		gotFilename = a["filename"]
		return nil
	})

	mag := "magnet:?xt=urn:btih:45df42358b3a764e393e5dce02ab05683704a0c1&dn=test.mkv"
	c := &Client{Host: hostOf(ts)}
	if err := c.AddMagnet(context.Background(), mag); err != nil {
		t.Fatalf("AddMagnet error: %v", err)
	}
	if gotMethod != "torrent-add" {
		t.Fatalf("method = %q, want %q", gotMethod, "torrent-add")
	}
	if !strings.Contains(gotFilename, "45df42358b3a764e393e5dce02ab05683704a0c1") {
		t.Fatalf("filename = %q, want magnet containing btih", gotFilename)
	}
}

func TestAddMagnet_SendsBasicAuth(t *testing.T) {
	var gotUser, gotPass string
	var authOK bool

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if ok {
			gotUser, gotPass, authOK = u, p, true
		}
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":"success","arguments":{}}`)
	}))
	defer ts.Close()

	mag := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=test"
	c := &Client{Host: hostOf(ts), Auth: "user:pass"}
	if err := c.AddMagnet(context.Background(), mag); err != nil {
		t.Fatalf("AddMagnet error: %v", err)
	}
	if !authOK || gotUser != "user" || gotPass != "pass" {
		t.Fatalf("BasicAuth = (%q, %q, %v), want (user, pass, true)", gotUser, gotPass, authOK)
	}
}

func TestAddMagnet_PropagatesRPCError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Transmission-Session-Id") == "" {
			w.Header().Set("X-Transmission-Session-Id", "test-session-id")
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"result":"something went wrong","arguments":{}}`)
	}))
	defer ts.Close()

	mag := "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	c := &Client{Host: hostOf(ts)}
	err := c.AddMagnet(context.Background(), mag)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "something went wrong") {
		t.Fatalf("expected rpc error message in error; got: %v", err)
	}
}
