package processor

import "testing"

func TestDetectResolution(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"bracketed", "Interstellar.2014.[1080p].BluRay.mkv", "1080p"},
		{"bare", "Interstellar.2014.1080p.BluRay.mkv", "1080p"},
		{"uppercase P", "Show.S01E01.2160P.WEB.mkv", "2160p"},
		{"space separated", "Some Movie 2014 720p WEBRip.mkv", "720p"},
		{"dims 4k", "Movie.2020.3840x2160.mkv", "2160p"},
		{"dims 1080", "Movie.2020.1920x1080.mkv", "1080p"},
		{"dims uppercase X", "Movie.2020.1920X1080.mkv", "1080p"},
		{"dims below smallest bucket ignored", "clip.320x240.mkv", ""},
		{"4k alias only", "Movie.2020.4K.BluRay.mkv", "2160p"},
		{"uhd alias only", "Movie.2020.UHD.BluRay.mkv", "2160p"},
		{"redundant 2160p + 4K + UHD collapses to one token", "Movie.2014.2160p.4K.UHD.BluRay.x265.mkv", "2160p"},
		{"contradiction keeps explicit NNNNp token", "Movie.2014.1080p.4K.WEB.mkv", "1080p"},
		{"dual NNNNp tokens prefer highest", "Show.S01E01.720p.1080p.WEB.mkv", "1080p"},
		{"576p", "Show.S03E04.576p.PDTV.mkv", "576p"},
		{"720p", "Show.S03E04.720p.HDTV.x264.mkv", "720p"},
		{"none", "Movie.2014.BluRay.x264-GROUP.mkv", ""},
		{"year in title is not a resolution", "Blade Runner 2049 (2017).mkv", ""},
		{"NxNN season token is not a dims pair", "show.1x01.mkv", ""},
		// Accepted false positive: a title that literally contains a
		// resolution word. Same posture as the media-tag blacklist.
		{"resolution word inside a title (accepted)", "1080p Nit Picking.mkv", "1080p"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectResolution(tt.in); got != tt.want {
				t.Fatalf("detectResolution(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestStripTrailingResolution(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Interstellar (2014) - 1080p", "Interstellar (2014)"},
		{"Show (2016) - S05E08 - 720p", "Show (2016) - S05E08"},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049 (2017)"},
		{"Interstellar (2014)", "Interstellar (2014)"},
		// Not a canonical bucket -- left untouched.
		{"Movie - 4320p", "Movie - 4320p"},
		// Only a trailing suffix is stripped, not a mid-string token.
		{"1080p Nit Picking (2020)", "1080p Nit Picking (2020)"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := stripTrailingResolution(tt.in); got != tt.want {
				t.Fatalf("stripTrailingResolution(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestHeightToBucket(t *testing.T) {
	tests := []struct {
		h    int
		want int
	}{
		{2160, 2160},
		{2000, 1440},
		{1080, 1080},
		{1079, 720},
		{480, 480},
		{479, 0},
		{0, 0},
	}
	for _, tt := range tests {
		if got := heightToBucket(tt.h); got != tt.want {
			t.Fatalf("heightToBucket(%d) = %d, want %d", tt.h, got, tt.want)
		}
	}
}
