package transmission

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRemoveCompleted_RemovesOnlyCompletedIDs(t *testing.T) {
	tmp := t.TempDir()
	callsFile := filepath.Join(tmp, "calls.log")
	removedFile := filepath.Join(tmp, "removed.log")
	listOutput := strings.TrimSpace(`
ID     Done       Have  ETA           Up    Down  Ratio  Status       Name
   1   100%    1.00 GB  Done         0.0     0.0   0.0   Idle         done-one
   2    99%  900.0 MB  10 min        0.0     0.0   0.0   Downloading  active
   3   100%    2.00 GB  Done         0.0     0.0   0.0   Idle         done-two
Sum:            2.88 GB               0.0     0.0
`)
	script := writeCleanupScript(t, tmp, callsFile, removedFile, listOutput, 0)

	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
		Auth:       "user:pass",
	}

	removed, err := c.RemoveCompleted(context.Background())
	if err != nil {
		t.Fatalf("RemoveCompleted() error: %v", err)
	}
	if removed != 2 {
		t.Fatalf("RemoveCompleted() = %d, want 2", removed)
	}

	callsB, err := os.ReadFile(callsFile)
	if err != nil {
		t.Fatalf("read calls file: %v", err)
	}
	calls := string(callsB)
	if !strings.Contains(calls, "localhost:9091") {
		t.Fatalf("expected host in calls, got %q", calls)
	}
	if !strings.Contains(calls, "-n user:pass") {
		t.Fatalf("expected auth in calls, got %q", calls)
	}
	if !strings.Contains(calls, "-l") {
		t.Fatalf("expected list invocation, got %q", calls)
	}

	removedB, err := os.ReadFile(removedFile)
	if err != nil {
		t.Fatalf("read removed file: %v", err)
	}
	gotIDs := strings.Fields(string(removedB))
	wantIDs := []string{"1", "3"}
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("removed ids count = %d, want %d; ids=%v", len(gotIDs), len(wantIDs), gotIDs)
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("removed id[%d] = %q, want %q; ids=%v", i, gotIDs[i], wantIDs[i], gotIDs)
		}
	}
}

func TestRemoveCompleted_NoCompletedReturnsZero(t *testing.T) {
	tmp := t.TempDir()
	callsFile := filepath.Join(tmp, "calls.log")
	removedFile := filepath.Join(tmp, "removed.log")
	listOutput := strings.TrimSpace(`
ID     Done       Have  ETA           Up    Down  Ratio  Status       Name
   9    72%  720.0 MB  20 min        0.0     0.0   0.0   Downloading  active
Sum:          720.0 MB               0.0     0.0
`)
	script := writeCleanupScript(t, tmp, callsFile, removedFile, listOutput, 0)

	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
	}

	removed, err := c.RemoveCompleted(context.Background())
	if err != nil {
		t.Fatalf("RemoveCompleted() error: %v", err)
	}
	if removed != 0 {
		t.Fatalf("RemoveCompleted() = %d, want 0", removed)
	}

	if _, err := os.Stat(removedFile); !os.IsNotExist(err) {
		t.Fatalf("expected no remove invocations, stat err=%v", err)
	}
}

func TestRemoveCompleted_HonorsDeadlineContext(t *testing.T) {
	tmp := t.TempDir()
	callsFile := filepath.Join(tmp, "calls.log")
	removedFile := filepath.Join(tmp, "removed.log")
	listOutput := strings.TrimSpace(`
ID     Done       Have  ETA           Up    Down  Ratio  Status       Name
   9    72%  720.0 MB  20 min        0.0     0.0   0.0   Downloading  active
Sum:          720.0 MB               0.0     0.0
`)
	// Delay list output long enough that a short context timeout must win.
	script := writeCleanupScript(t, tmp, callsFile, removedFile, listOutput, 2)

	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	removed, err := c.RemoveCompleted(ctx)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if removed != 0 {
		t.Fatalf("RemoveCompleted() = %d, want 0 when list call times out", removed)
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected deadline exceeded error, got: %v", err)
	}
}

func TestRemoveCompleted_HonorsCanceledContext(t *testing.T) {
	tmp := t.TempDir()
	callsFile := filepath.Join(tmp, "calls.log")
	removedFile := filepath.Join(tmp, "removed.log")
	listOutput := strings.TrimSpace(`
ID     Done       Have  ETA           Up    Down  Ratio  Status       Name
   9    72%  720.0 MB  20 min        0.0     0.0   0.0   Downloading  active
Sum:          720.0 MB               0.0     0.0
`)
	script := writeCleanupScript(t, tmp, callsFile, removedFile, listOutput, 0)

	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	removed, err := c.RemoveCompleted(ctx)
	if err == nil {
		t.Fatalf("expected canceled context error, got nil")
	}
	if removed != 0 {
		t.Fatalf("RemoveCompleted() = %d, want 0 with canceled context", removed)
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("expected context canceled error, got: %v", err)
	}
}

func writeCleanupScript(t *testing.T, dir, callsFile, removedFile, listOutput string, listDelaySeconds int) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "tx-cleanup.sh")
	script := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail

printf "%%s\n" "$*" >> %q

for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-l" ]]; then
    if (( %d > 0 )); then
      sleep %d
    fi
cat <<'EOF'
%s
EOF
    exit 0
  fi
done

for ((i=1; i<=$#; i++)); do
  if [[ "${!i}" == "-t" ]]; then
    j=$((i+1))
    printf "%%s\n" "${!j}" >> %q
    exit 0
  fi
done

echo "unexpected invocation: $*" >&2
exit 9
`, callsFile, listDelaySeconds, listDelaySeconds, listOutput, removedFile)

	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write cleanup script: %v", err)
	}
	return scriptPath
}
