package transmission

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// Client invokes transmission-remote to add magnets.
// It is intentionally small and CLI-based for macOS/Homebrew setups.
type Client struct {
	// Path to transmission-remote. If empty, defaults to "transmission-remote" (PATH lookup).
	RemotePath string

	// Host in "host:port" form, e.g. "localhost:9091"
	Host string

	// Optional auth in "user:pass" form. Leave empty for no auth.
	Auth string
}

func (c Client) validate() error {
	if strings.TrimSpace(c.Host) == "" {
		return errors.New("transmission host is empty")
	}
	return nil
}

// AddMagnet adds a magnet URI to Transmission via transmission-remote.
// Equivalent CLI shape:
//
//	transmission-remote <host> [-n user:pass] -a <magnet>
func (c Client) AddMagnet(ctx context.Context, magnet string) error {
	if err := c.validate(); err != nil {
		return err
	}

	magnet = strings.TrimSpace(magnet)
	if magnet == "" {
		return errors.New("magnet is empty")
	}

	u, parseErr := url.Parse(magnet)
	if parseErr != nil {
		return fmt.Errorf("invalid magnet URI: %w", parseErr)
	}
	if strings.ToLower(u.Scheme) != "magnet" {
		return fmt.Errorf("not a magnet URI: %q", magnet)
	}
	xt := u.Query().Get("xt")
	const prefix = "urn:btih:"
	if !strings.HasPrefix(xt, prefix) {
		return fmt.Errorf("magnet missing btih: %q", magnet)
	}
	h := strings.TrimSpace(strings.TrimPrefix(xt, prefix))
	if len(h) < 8 {
		return fmt.Errorf("magnet btih too short: %q", magnet)
	}

	remote := strings.TrimSpace(c.RemotePath)
	if remote == "" {
		remote = "transmission-remote"
	}

	args := c.baseArgs()
	// Add magnet: -a <url>
	args = append(args, "-a", magnet)

	cmd := exec.CommandContext(ctx, remote, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Include output for debugging (Transmission errors are often only in stdout/stderr).
		return fmt.Errorf("transmission add failed (host=%s): %w; output: %s", c.Host, err, strings.TrimSpace(string(out)))
	}
	return nil
}
