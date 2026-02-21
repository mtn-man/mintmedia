package notify

import "testing"

func TestNormalizeDoneNotificationMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "EmptyDefaultsToPerFile", input: "", want: DoneNotificationPerFile},
		{name: "WhitespaceDefaultsToPerFile", input: "  ", want: DoneNotificationPerFile},
		{name: "PerFile", input: "per_file", want: DoneNotificationPerFile},
		{name: "PerJobUppercase", input: "PER_JOB", want: DoneNotificationPerJob},
		{name: "OffMixedCase", input: "OfF", want: DoneNotificationOff},
		{name: "Invalid", input: "loud", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeDoneNotificationMode(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeDoneNotificationMode() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeDoneNotificationMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDoneSoundCount(t *testing.T) {
	tests := []struct {
		name             string
		mode             string
		appliedMainCount int
		want             int
	}{
		{name: "PerFile_ThreeApplied", mode: DoneNotificationPerFile, appliedMainCount: 3, want: 3},
		{name: "PerJob_ThreeApplied", mode: DoneNotificationPerJob, appliedMainCount: 3, want: 1},
		{name: "Off_ThreeApplied", mode: DoneNotificationOff, appliedMainCount: 3, want: 0},
		{name: "PerFile_ZeroApplied", mode: DoneNotificationPerFile, appliedMainCount: 0, want: 0},
		{name: "InvalidMode_FallsBackToPerFile", mode: "loud", appliedMainCount: 2, want: 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DoneSoundCount(tc.mode, tc.appliedMainCount)
			if got != tc.want {
				t.Fatalf("DoneSoundCount() = %d, want %d", got, tc.want)
			}
		})
	}
}
