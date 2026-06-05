package main

import "testing"

func TestResolveModePolicy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		enableProcessing bool
		plan             string
		process          string
		processDrop      bool
		daemon           bool
		wantErr          error
		wantPlan         string
		wantProcess      string
		wantProcessDrop  bool
		wantDaemon       bool
		wantExplicit     int
	}{
		{
			name:             "enabled defaults to process-drop",
			enableProcessing: true,
			wantProcessDrop:  true,
			wantExplicit:     0,
		},
		{
			name:             "disabled no mode is smoke-test path",
			enableProcessing: false,
			wantProcessDrop:  false,
			wantExplicit:     0,
		},
		{
			name:             "disabled explicit mode errors",
			enableProcessing: false,
			plan:             "/tmp/input",
			wantErr:          errProcessingDisabled,
		},
		{
			name:             "multiple explicit modes error",
			enableProcessing: true,
			plan:             "/tmp/one",
			daemon:           true,
			wantErr:          errConflictingModes,
		},
		{
			name:             "enabled single explicit mode preserved",
			enableProcessing: true,
			process:          "/tmp/media",
			wantProcess:      "/tmp/media",
			wantProcessDrop:  false,
			wantExplicit:     1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveModePolicy(
				tt.plan,
				tt.process,
				tt.processDrop,
				tt.daemon,
				tt.enableProcessing,
			)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if err != tt.wantErr {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveModePolicy returned error: %v", err)
			}
			if got.PlanPath != tt.wantPlan {
				t.Fatalf("PlanPath = %q, want %q", got.PlanPath, tt.wantPlan)
			}
			if got.ProcessPath != tt.wantProcess {
				t.Fatalf("ProcessPath = %q, want %q", got.ProcessPath, tt.wantProcess)
			}
			if got.ProcessDrop != tt.wantProcessDrop {
				t.Fatalf("ProcessDrop = %v, want %v", got.ProcessDrop, tt.wantProcessDrop)
			}
			if got.Daemon != tt.wantDaemon {
				t.Fatalf("Daemon = %v, want %v", got.Daemon, tt.wantDaemon)
			}
			if got.ExplicitCount != tt.wantExplicit {
				t.Fatalf("ExplicitCount = %d, want %d", got.ExplicitCount, tt.wantExplicit)
			}
		})
	}
}
