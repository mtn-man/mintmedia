package transmission

import (
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
func newRPCServer(t *testing.T, handle func(method string, args json.RawMessage) any) *httptest.Server {
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
