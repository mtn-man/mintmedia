package transfer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestCopyThenReplace_WritesFileAndEmitsDone(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("a", 128))

	progress := &progressRecorder{}
	reporter := &stubReporter{}

	xfer := NewRenameOrCopy(Options{
		Progress:  progress.Add,
		Reporter:  reporter,
		PrintDone: true,
	})

	if err := xfer.copyThenReplace(context.Background(), src, dst); err != nil {
		t.Fatalf("copyThenReplace error: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != strings.Repeat("a", 128) {
		t.Fatalf("dest contents mismatch")
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err=%v", err)
	}

	if !progress.Contains("COPY DONE:") {
		t.Fatalf("expected COPY DONE line, got=%v", progress.Lines())
	}

	if !reporter.DoneCalled() {
		t.Fatalf("expected reporter.Done to be called")
	}
	if reporter.DoneSnapshot().Name != filepath.Base(dst) {
		t.Fatalf("Done snapshot name = %q, want %q", reporter.DoneSnapshot().Name, filepath.Base(dst))
	}
}

func TestCopyThenReplace_PrintDoneDisabled(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("b", 64))

	progress := &progressRecorder{}
	reporter := &stubReporter{}

	xfer := NewRenameOrCopy(Options{
		Progress:  progress.Add,
		Reporter:  reporter,
		PrintDone: false,
	})

	if err := xfer.copyThenReplace(context.Background(), src, dst); err != nil {
		t.Fatalf("copyThenReplace error: %v", err)
	}

	if progress.Contains("COPY DONE:") {
		t.Fatalf("unexpected COPY DONE line when PrintDone=false")
	}
	if reporter.DoneCalled() {
		t.Fatalf("unexpected reporter.Done call when PrintDone=false")
	}
}

func TestCopyThenReplace_ContextCanceled(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, "data")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	xfer := NewRenameOrCopy(Options{})
	if err := xfer.copyThenReplace(ctx, src, dst); err == nil {
		t.Fatalf("expected error, got nil")
	}

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected dest to not exist, stat err=%v", err)
	}
}

func TestMove_RenameFastPath(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, strings.Repeat("c", 32))

	xfer := NewRenameOrCopy(Options{})
	if err := xfer.Move(context.Background(), src, dst); err != nil {
		t.Fatalf("Move error: %v", err)
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("expected source to be removed, stat err=%v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if string(got) != strings.Repeat("c", 32) {
		t.Fatalf("dest contents mismatch")
	}
}

func TestMove_DestinationExists(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	dst := filepath.Join(root, "dst.mkv")
	writeFile(t, src, "data")
	writeFile(t, dst, "existing")

	xfer := NewRenameOrCopy(Options{})
	err := xfer.Move(context.Background(), src, dst)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("expected destination exists error, got: %v", err)
	}

	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
}

func TestSameDevice_TempDir(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	writeFile(t, src, "data")

	same, err := sameDevice(src, root)
	if err != nil {
		t.Fatalf("sameDevice error: %v", err)
	}
	if !same {
		t.Fatalf("expected same device for temp dir paths")
	}
}

func TestSameDevice_StatError(t *testing.T) {
	root := t.TempDir()
	_, err := sameDevice(filepath.Join(root, "missing.mkv"), root)
	if err == nil {
		t.Fatalf("expected error for missing source path")
	}
}

func TestMove_SameDeviceRenameFailure_NoFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod semantics differ on windows")
	}

	root := t.TempDir()
	src := filepath.Join(root, "src.mkv")
	writeFile(t, src, strings.Repeat("d", 16))

	dstDir := filepath.Join(root, "dstdir")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatalf("mkdir dest dir: %v", err)
	}
	if err := os.Chmod(dstDir, 0o555); err != nil {
		t.Fatalf("chmod dest dir: %v", err)
	}
	defer func() {
		_ = os.Chmod(dstDir, 0o755)
	}()

	dst := filepath.Join(dstDir, "dst.mkv")
	xfer := NewRenameOrCopy(Options{})
	if err := xfer.Move(context.Background(), src, dst); err == nil {
		t.Fatalf("expected rename error, got nil")
	}

	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("expected destination to not exist, stat err=%v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("expected source to remain, stat err=%v", err)
	}
}

type progressRecorder struct {
	mu    sync.Mutex
	lines []string
}

func (p *progressRecorder) Add(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, line)
}

func (p *progressRecorder) Lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.lines))
	copy(out, p.lines)
	return out
}

func (p *progressRecorder) Contains(substr string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, line := range p.lines {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

type stubReporter struct {
	mu        sync.Mutex
	done      Snapshot
	doneCount int
}

func (s *stubReporter) Update(Snapshot) {}

func (s *stubReporter) Done(sn Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = sn
	s.doneCount++
}

func (s *stubReporter) DoneCalled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.doneCount > 0
}

func (s *stubReporter) DoneSnapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

func writeFile(t *testing.T, path string, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}
