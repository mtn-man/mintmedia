package transmission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/mtn-man/mintmedia/internal/magnet"
)

// Client makes JSON-RPC calls to a Transmission daemon over HTTP.
// It is safe for concurrent use.
type Client struct {
	// Host in "host:port" form, e.g. "localhost:9091" or "100.x.x.x:9091".
	Host string

	// Optional auth in "user:pass" form for HTTP Basic auth.
	Auth string

	mu        sync.Mutex
	sessionID string // Transmission CSRF token; cached and refreshed on 409
}

func (c *Client) validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("transmission host is empty")
	}
	if auth := strings.TrimSpace(c.Auth); auth != "" && !strings.Contains(auth, ":") {
		return errors.New("transmission auth must be in \"user:pass\" form")
	}
	return nil
}

func (c *Client) rpcURL() string {
	host := strings.TrimSpace(c.Host)
	raw := host
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "http://" + host + "/transmission/rpc"
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "http"
	}
	return scheme + "://" + u.Host + "/transmission/rpc"
}

type rpcRequest struct {
	Method    string      `json:"method"`
	Arguments interface{} `json:"arguments,omitempty"`
}

type rpcResponse struct {
	Result    string          `json:"result"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// rpc sends a Transmission JSON-RPC call, handling the CSRF session-ID handshake.
// On a 409 response the session ID is refreshed and the request is retried once.
func (c *Client) rpc(ctx context.Context, method string, args interface{}) (json.RawMessage, error) {
	body, err := json.Marshal(rpcRequest{Method: method, Arguments: args})
	if err != nil {
		return nil, fmt.Errorf("marshal rpc request: %w", err)
	}

	c.mu.Lock()
	sid := c.sessionID
	c.mu.Unlock()

	resp, err := c.doRequest(ctx, body, sid)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusConflict {
		newSID := resp.Header.Get("X-Transmission-Session-Id")
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()

		c.mu.Lock()
		c.sessionID = newSID
		c.mu.Unlock()

		resp, err = c.doRequest(ctx, body, newSID)
		if err != nil {
			return nil, err
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("transmission rpc: unexpected status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var result rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode rpc response: %w", err)
	}
	if result.Result != "success" {
		return nil, fmt.Errorf("transmission rpc %s: %s", method, result.Result)
	}
	return result.Arguments, nil
}

func (c *Client) doRequest(ctx context.Context, body []byte, sessionID string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.rpcURL(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		req.Header.Set("X-Transmission-Session-Id", sessionID)
	}
	if auth := strings.TrimSpace(c.Auth); auth != "" {
		parts := strings.SplitN(auth, ":", 2)
		req.SetBasicAuth(parts[0], parts[1])
	}
	return http.DefaultClient.Do(req)
}

// AddMagnet adds a magnet URI to Transmission via the torrent-add RPC method.
func (c *Client) AddMagnet(ctx context.Context, magnetURI string) error {
	if err := c.validate(); err != nil {
		return err
	}

	info, err := magnet.Parse(magnetURI)
	if err != nil {
		magnetURI = strings.TrimSpace(magnetURI)
		switch {
		case errors.Is(err, magnet.ErrEmpty):
			return errors.New("magnet is empty")
		case errors.Is(err, magnet.ErrInvalidURI):
			return err
		case errors.Is(err, magnet.ErrNotMagnet):
			return fmt.Errorf("not a magnet URI: %q", magnetURI)
		case errors.Is(err, magnet.ErrMissingBTIH):
			return fmt.Errorf("magnet missing btih: %q", magnetURI)
		case errors.Is(err, magnet.ErrBTIHTooShort):
			return fmt.Errorf("magnet btih too short: %q", magnetURI)
		default:
			return err
		}
	}

	_, err = c.rpc(ctx, "torrent-add", map[string]string{
		"filename": info.URI,
	})
	if err != nil {
		return fmt.Errorf("transmission add failed (host=%s): %w", c.Host, err)
	}
	return nil
}
