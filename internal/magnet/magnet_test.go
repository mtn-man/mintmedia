package magnet

import (
	"errors"
	"testing"
)

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		in           string
		wantErr      error
		wantBTIH     string
		wantDN       string
		wantTrackers int
	}{
		{
			name:         "valid minimal",
			in:           "magnet:?xt=urn:btih:12345678",
			wantBTIH:     "12345678",
			wantDN:       "",
			wantTrackers: 0,
		},
		{
			name:         "valid with metadata and whitespace",
			in:           "  magnet:?xt=urn:btih:45df42358b3a764e393e5dce02ab05683704a0c1&dn=test.mkv&tr=udp://a&tr=udp://b  ",
			wantBTIH:     "45df42358b3a764e393e5dce02ab05683704a0c1",
			wantDN:       "test.mkv",
			wantTrackers: 2,
		},
		{
			name:    "empty",
			in:      "   ",
			wantErr: ErrEmpty,
		},
		{
			name:    "invalid uri",
			in:      "://bad",
			wantErr: ErrInvalidURI,
		},
		{
			name:    "not magnet scheme",
			in:      "https://example.com/file.torrent",
			wantErr: ErrNotMagnet,
		},
		{
			name:    "missing btih",
			in:      "magnet:?dn=test",
			wantErr: ErrMissingBTIH,
		},
		{
			name:    "wrong xt prefix",
			in:      "magnet:?xt=urn:sha1:abcdefghi",
			wantErr: ErrMissingBTIH,
		},
		{
			name:    "short btih",
			in:      "magnet:?xt=urn:btih:abc",
			wantErr: ErrBTIHTooShort,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := Parse(tc.in)
			if tc.wantErr != nil {
				if err == nil {
					t.Fatalf("Parse() error = nil, want %v", tc.wantErr)
				}
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Parse() error = %v, want errors.Is(..., %v)", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse() unexpected error: %v", err)
			}
			if got.BTIH != tc.wantBTIH {
				t.Fatalf("Parse() BTIH = %q, want %q", got.BTIH, tc.wantBTIH)
			}
			if got.DN != tc.wantDN {
				t.Fatalf("Parse() DN = %q, want %q", got.DN, tc.wantDN)
			}
			if got.Trackers != tc.wantTrackers {
				t.Fatalf("Parse() Trackers = %d, want %d", got.Trackers, tc.wantTrackers)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	t.Parallel()

	if !IsValid("magnet:?xt=urn:btih:12345678") {
		t.Fatalf("IsValid(valid magnet) = false, want true")
	}
	if IsValid("https://example.com/file.torrent") {
		t.Fatalf("IsValid(non-magnet) = true, want false")
	}
	if IsValid("magnet:?xt=urn:btih:abc") {
		t.Fatalf("IsValid(short btih) = true, want false")
	}
}

func TestShortBTIH(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "empty input", in: "", max: 12, want: ""},
		{name: "max non-positive", in: "abcdef", max: 0, want: ""},
		{name: "shorter than max", in: "abcdef", max: 12, want: "abcdef"},
		{name: "equal to max", in: "abcdef", max: 6, want: "abcdef"},
		{name: "longer than max", in: "abcdefghijk", max: 4, want: "abcd"},
		{name: "trim input", in: "  abcdef  ", max: 4, want: "abcd"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ShortBTIH(tc.in, tc.max)
			if got != tc.want {
				t.Fatalf("ShortBTIH(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
			}
		})
	}
}
