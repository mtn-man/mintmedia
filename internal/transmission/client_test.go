package transmission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Helper: create an executable script that writes its argv to a file and exits 0.
func writeArgCaptureScript(t *testing.T, dir string, outFile string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "tx-remote.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail
# Write all args (space-separated) to the output file
printf "%s" "$@" > "` + outFile + `"
exit 0
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return scriptPath
}

// Helper: create an executable script that prints to stderr and exits nonzero.
func writeFailScript(t *testing.T, dir string) string {
	t.Helper()

	scriptPath := filepath.Join(dir, "tx-fail.sh")
	script := `#!/usr/bin/env bash
set -euo pipefail
echo "boom from fake transmission-remote" >&2
exit 42
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fail script: %v", err)
	}
	return scriptPath
}

func TestAddMagnet_RequiresHost(t *testing.T) {
	c := Client{
		RemotePath: "/does/not/matter",
		Host:       "",
	}

	err := c.AddMagnet(context.Background(), "magnet:?xt=urn:btih:abc")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "host") {
		t.Fatalf("expected host error, got: %v", err)
	}
}

func TestAddMagnet_RejectsNonMagnet(t *testing.T) {
	c := Client{
		RemotePath: "/does/not/matter",
		Host:       "localhost:9091",
	}

	err := c.AddMagnet(context.Background(), "https://example.com/not-a-magnet")
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "not a magnet") {
		t.Fatalf("expected not-a-magnet error, got: %v", err)
	}
}

func TestAddMagnet_CallsRemote_WithHostAndAdd(t *testing.T) {
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "argv.txt")
	script := writeArgCaptureScript(t, tmp, outFile)

	magnet := "magnet:?xt=urn:btih:45df42358b3a764e393e5dce02ab05683704a0c1&dn=test.mkv"
	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
		Auth:       "",
	}

	if err := c.AddMagnet(context.Background(), magnet); err != nil {
		t.Fatalf("AddMagnet error: %v", err)
	}

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	got := string(b)

	// Our script writes args with no delimiters other than their natural concatenation.
	// That’s slightly annoying, but sufficient to assert ordering/containment.
	if !strings.Contains(got, "localhost:9091") {
		t.Fatalf("expected host arg; got %q", got)
	}
	if !strings.Contains(got, "-a") {
		t.Fatalf("expected -a; got %q", got)
	}
	if !strings.Contains(got, magnet) {
		t.Fatalf("expected magnet; got %q", got)
	}
}

func TestAddMagnet_CallsRemote_WithAuth(t *testing.T) {
	tmp := t.TempDir()
	outFile := filepath.Join(tmp, "argv.txt")
	script := writeArgCaptureScript(t, tmp, outFile)

	magnet := "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa&dn=test"
	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
		Auth:       "user:pass",
	}

	if err := c.AddMagnet(context.Background(), magnet); err != nil {
		t.Fatalf("AddMagnet error: %v", err)
	}

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	got := string(b)

	if !strings.Contains(got, "localhost:9091") {
		t.Fatalf("expected host arg; got %q", got)
	}
	if !strings.Contains(got, "-n") {
		t.Fatalf("expected -n; got %q", got)
	}
	if !strings.Contains(got, "user:pass") {
		t.Fatalf("expected auth; got %q", got)
	}
	if !strings.Contains(got, "-a") || !strings.Contains(got, magnet) {
		t.Fatalf("expected add magnet; got %q", got)
	}
}

func TestAddMagnet_PropagatesCommandFailureWithOutput(t *testing.T) {
	tmp := t.TempDir()
	script := writeFailScript(t, tmp)

	c := Client{
		RemotePath: script,
		Host:       "localhost:9091",
	}

	err := c.AddMagnet(context.Background(), "magnet:?xt=urn:btih:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if err == nil {
		t.Fatalf("expected error")
	}
	// Ensure stderr output is present for debugging.
	if !strings.Contains(err.Error(), "boom from fake transmission-remote") {
		t.Fatalf("expected stderr in error; got: %v", err)
	}
}
