package transmission

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Mtn-Man/mintmedia/internal/magnet"
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
func (c Client) AddMagnet(ctx context.Context, magnetURI string) error {
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
	magnetURI = info.URI

	remote := strings.TrimSpace(c.RemotePath)
	if remote == "" {
		remote = "transmission-remote"
	}

	args := c.baseArgs()
	// Add magnet: -a <url>
	args = append(args, "-a", magnetURI)

	cmd := exec.CommandContext(ctx, remote, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Include output for debugging (Transmission errors are often only in stdout/stderr).
		return fmt.Errorf("transmission add failed (host=%s): %w; output: %s", c.Host, err, strings.TrimSpace(string(out)))
	}
	return nil
}
