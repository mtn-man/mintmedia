package magnet

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	// BTIHPrefix is the expected xt query prefix for magnet links.
	BTIHPrefix = "urn:btih:"
	// MinBTIHLen is the minimum acceptable hash fragment length.
	MinBTIHLen = 8
)

// Sentinel errors returned by Parse.
var (
	ErrEmpty        = errors.New("magnet is empty")
	ErrInvalidURI   = errors.New("invalid magnet URI")
	ErrNotMagnet    = errors.New("not a magnet URI")
	ErrMissingBTIH  = errors.New("magnet missing btih")
	ErrBTIHTooShort = errors.New("magnet btih too short")
)

// Info contains parsed magnet metadata used across modules.
type Info struct {
	URI      string
	BTIH     string
	DN       string
	Trackers int
}

// Parse validates a magnet URI and returns parsed metadata.
func Parse(raw string) (Info, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Info{}, ErrEmpty
	}

	u, err := url.Parse(raw)
	if err != nil {
		return Info{}, fmt.Errorf("%w: %w", ErrInvalidURI, err)
	}
	if strings.ToLower(u.Scheme) != "magnet" {
		return Info{}, ErrNotMagnet
	}

	q := u.Query()
	xt := q.Get("xt")
	if !strings.HasPrefix(xt, BTIHPrefix) {
		return Info{}, ErrMissingBTIH
	}

	h := strings.TrimSpace(strings.TrimPrefix(xt, BTIHPrefix))
	if len(h) < MinBTIHLen {
		return Info{}, ErrBTIHTooShort
	}

	return Info{
		URI:      raw,
		BTIH:     h,
		DN:       q.Get("dn"),
		Trackers: len(q["tr"]),
	}, nil
}

// IsValid reports whether raw is a valid magnet URI.
func IsValid(raw string) bool {
	_, err := Parse(raw)
	return err == nil
}

// ShortBTIH returns a shortened hash fragment for logging.
func ShortBTIH(btih string, maxLen int) string {
	btih = strings.TrimSpace(btih)
	if btih == "" || maxLen <= 0 {
		return ""
	}
	if len(btih) <= maxLen {
		return btih
	}
	return btih[:maxLen]
}
