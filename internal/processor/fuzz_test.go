package processor

import "testing"

func FuzzParseShowFromName(f *testing.F) {
	seeds := [][2]string{
		{"Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to]", "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"},
		{"Sherlock.S04E00.The.Abominable.Bride.2016.1080p.x265", "Sherlock.S04E00.The.Abominable.Bride.2016.1080p.x265.mkv"},
		{"The.Boys.S03E01.2160p.WEB-DL", "The.Boys.S03E01.2160p.WEB-DL.mkv"},
		{"Survivor S40 Season 40", "Survivor.S40E09.HDTV.x264.mkv"},
		{"Show Season 1-4", "show.s01e01.mkv"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, baseName, fileName string) {
		showName, _, season, episode, err := parseShowFromName(nil, baseName, fileName)
		if err != nil {
			return
		}
		if showName == "" {
			t.Errorf("succeeded but returned empty showName (baseName=%q fileName=%q)", baseName, fileName)
		}
		if season < 0 {
			t.Errorf("succeeded but returned negative season %d (baseName=%q fileName=%q)", season, baseName, fileName)
		}
		if episode < 0 {
			t.Errorf("succeeded but returned negative episode %d (baseName=%q fileName=%q)", episode, baseName, fileName)
		}
	})
}

func FuzzParseMovieFromName(f *testing.F) {
	seeds := [][2]string{
		{"Get.Smart.2008.1080p.BluRay", "Get.Smart.2008.1080p.BluRay.mkv"},
		{"Dune.Part.Two.2024.2160p.WEB-DL", "Dune.Part.Two.2024.2160p.WEB-DL.mkv"},
		{"Inception.2010.1080p.BluRay.x265", "Inception.2010.1080p.BluRay.x265.mkv"},
		{"NoYearHere", "NoYearHere.mkv"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s[0], s[1])
	}

	f.Fuzz(func(t *testing.T, baseName, fileName string) {
		title, _, err := parseMovieFromName(nil, baseName, fileName)
		if err != nil {
			return
		}
		if title == "" {
			t.Errorf("succeeded but returned empty title (baseName=%q fileName=%q)", baseName, fileName)
		}
	})
}
