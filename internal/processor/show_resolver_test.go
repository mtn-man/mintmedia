package processor

import "testing"

func TestParseShowFolderQualifier(t *testing.T) {
	tests := []struct {
		name         string
		folder       string
		wantBase     string
		wantQualifer string
		wantOK       bool
	}{
		{name: "SingleQualifier_Year", folder: "The Bear (2022)", wantBase: "The Bear", wantQualifer: "2022", wantOK: true},
		{name: "SingleQualifier_NonYear", folder: "The Office (UK)", wantBase: "The Office", wantQualifer: "UK", wantOK: true},
		{
			// The fix target: a country-qualifier parenthetical before the
			// year must stay part of the base, not get folded into (or
			// clobber) the qualifier capture.
			name:         "DoubleQualifier_CountryTagThenYear",
			folder:       "The Office (US) (2005)",
			wantBase:     "The Office (US)",
			wantQualifer: "2005",
			wantOK:       true,
		},
		{
			name:         "DoubleQualifier_NonYearThenYear",
			folder:       "Blade Runner (Director's Cut) (1982)",
			wantBase:     "Blade Runner (Director's Cut)",
			wantQualifer: "1982",
			wantOK:       true,
		},
		{name: "NoParenthetical", folder: "The Bear", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, qualifier, ok := parseShowFolderQualifier(tc.folder)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if base != tc.wantBase || qualifier != tc.wantQualifer {
				t.Errorf("base/qualifier = %q/%q, want %q/%q", base, qualifier, tc.wantBase, tc.wantQualifer)
			}
		})
	}
}

func TestParseShowFolderYear(t *testing.T) {
	tests := []struct {
		name     string
		folder   string
		wantBase string
		wantYear string
		wantOK   bool
	}{
		{name: "SingleQualifier_Year", folder: "The Bear (2022)", wantBase: "The Bear", wantYear: "2022", wantOK: true},
		{name: "SingleQualifier_NonYear_NotAYear", folder: "The Office (UK)", wantOK: false},
		{
			name:     "DoubleQualifier_CountryTagThenYear",
			folder:   "The Office (US) (2005)",
			wantBase: "The Office (US)",
			wantYear: "2005",
			wantOK:   true,
		},
		{name: "NoParenthetical", folder: "The Bear", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base, year, ok := parseShowFolderYear(tc.folder)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if base != tc.wantBase || year != tc.wantYear {
				t.Errorf("base/year = %q/%q, want %q/%q", base, year, tc.wantBase, tc.wantYear)
			}
		})
	}
}
