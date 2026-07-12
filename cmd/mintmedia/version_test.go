package main

import "testing"

func TestFormatVersionLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{
			name:    "default dev version",
			version: "dev",
			want:    "mintmedia dev\n",
		},
		{
			name:    "injected release version",
			version: "v0.1.0",
			want:    "mintmedia v0.1.0\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := formatVersionLine(tc.version)
			if got != tc.want {
				t.Fatalf("formatVersionLine(%q) = %q, want %q", tc.version, got, tc.want)
			}
		})
	}
}

func TestResolveVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		buildVersion  string
		moduleVersion string
		want          string
	}{
		{
			name:          "injected build version wins over module version",
			buildVersion:  "v1.2.3",
			moduleVersion: "v1.2.2",
			want:          "v1.2.3",
		},
		{
			name:          "default dev build version falls back to module version",
			buildVersion:  defaultVersion,
			moduleVersion: "v1.2.3",
			want:          "v1.2.3",
		},
		{
			name:          "devel module version falls back to build version",
			buildVersion:  defaultVersion,
			moduleVersion: develBuildInfoVersion,
			want:          defaultVersion,
		},
		{
			name:          "empty module version falls back to build version",
			buildVersion:  defaultVersion,
			moduleVersion: "",
			want:          defaultVersion,
		},
		{
			name:          "empty build version with module version uses module version",
			buildVersion:  "",
			moduleVersion: "v1.2.3",
			want:          "v1.2.3",
		},
		{
			name:          "empty build and module version falls back to default dev",
			buildVersion:  "",
			moduleVersion: "",
			want:          defaultVersion,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := resolveVersion(tc.buildVersion, tc.moduleVersion)
			if got != tc.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tc.buildVersion, tc.moduleVersion, got, tc.want)
			}
		})
	}
}
