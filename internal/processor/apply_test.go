// internal/processor/apply_test.go
package processor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/mtn-man/mintmedia/internal/logging"
	"github.com/mtn-man/mintmedia/internal/transfer"
)

// --- Tests ------------------------------------------------------------------

func TestMain(m *testing.M) {
	homeDir, err := os.MkdirTemp("", "mintmedia-home-*")
	if err != nil {
		os.Exit(1)
	}
	_ = os.Setenv("HOME", homeDir)
	trashDir, err := resolveTrashDir()
	if err != nil {
		_ = os.RemoveAll(homeDir)
		os.Exit(1)
	}
	if err := os.MkdirAll(trashDir, 0o700); err != nil {
		_ = os.RemoveAll(homeDir)
		os.Exit(1)
	}

	code := m.Run()

	_ = os.RemoveAll(homeDir)
	os.Exit(code)
}

func TestApply_MovesMainAndAssociated_DeletesSourceDir(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	// Create a directory input under drop folder
	inputDir := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to]")
	mkdirAll(t, inputDir)

	mainName := "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(inputDir, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 128))

	assocSrc := filepath.Join(inputDir, "Stranger.Things.S05E08.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
	writeFile(t, assocSrc, "subtitle")

	// Plan and Apply
	pl, err := planOne(t, p, inputDir)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if !res.Applied {
		t.Fatalf("Applied = false, want true")
	}

	// Main moved
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("dest main missing (%s): %v", pl.DestMainPath, err)
	}

	// Associated moved (renamed to radix.en.srt)
	wantAssocDestSuffix := pl.DestRadix + ".en.srt"
	foundAssoc := false
	for _, mv := range pl.Associated {
		if strings.HasSuffix(mv.Dest, wantAssocDestSuffix) {
			foundAssoc = true
			if _, err := os.Stat(mv.Dest); err != nil {
				t.Fatalf("dest assoc missing (%s): %v", mv.Dest, err)
			}
		}
	}
	if !foundAssoc {
		t.Fatalf("expected at least one associated move ending with %q", wantAssocDestSuffix)
	}

	// Source directory deleted (policy)
	if _, err := os.Stat(inputDir); !os.IsNotExist(err) {
		t.Fatalf("source dir should be deleted, stat err=%v", err)
	}
}

func TestApply_MultiEpisodeDir_MovesAllAndCleansUp(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "The.Copenhagen.Test.S01")
	mkdirAll(t, inputDir)

	ep1 := "The.Copenhagen.Test.S01E01.1080p.HEVC.x265.mkv"
	ep2 := "The.Copenhagen.Test.S01E02.1080p.HEVC.x265.mkv"
	ep1Src := filepath.Join(inputDir, ep1)
	ep2Src := filepath.Join(inputDir, ep2)
	writeFile(t, ep1Src, strings.Repeat("m", 64))
	writeFile(t, ep2Src, strings.Repeat("m", 64))

	ep1Sub := filepath.Join(inputDir, "The.Copenhagen.Test.S01E01.1080p.HEVC.x265.en.srt")
	ep2Sub := filepath.Join(inputDir, "The.Copenhagen.Test.S01E02.1080p.HEVC.x265.en.srt")
	writeFile(t, ep1Sub, "subtitle")
	writeFile(t, ep2Sub, "subtitle")

	readme := filepath.Join(inputDir, "readme.txt")
	writeFile(t, readme, "ignore")

	plans, err := p.Plan(context.Background(), Request{InputPath: inputDir})
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(plans))
	}

	results, err := p.Apply(context.Background(), plans)
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	for _, res := range results {
		if !res.Applied {
			t.Fatalf("Applied = false, want true")
		}
		if _, err := os.Stat(res.Plan.DestMainPath); err != nil {
			t.Fatalf("dest main missing (%s): %v", res.Plan.DestMainPath, err)
		}

		foundAssoc := false
		for _, mv := range res.Plan.Associated {
			if mv.Kind != "associated" {
				continue
			}
			foundAssoc = true
			if _, err := os.Stat(mv.Dest); err != nil {
				t.Fatalf("dest assoc missing (%s): %v", mv.Dest, err)
			}
		}
		if !foundAssoc {
			t.Fatalf("expected associated move for %s", res.Plan.MainSourcePath)
		}
	}

	if _, err := os.Stat(inputDir); !os.IsNotExist(err) {
		t.Fatalf("source dir should be deleted, stat err=%v", err)
	}
}

func TestApply_FileInput_DoesNotDeleteDropFolder(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	mainName := "The.Copenhagen.Test.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Drop folder root must still exist
	if st, err := os.Stat(p.cfg.DropFolder); err != nil || !st.IsDir() {
		t.Fatalf("drop folder missing or not a dir after Apply: %v", err)
	}
}

func TestApply_AssociatedMoveFailureIsNonFatal(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E07.1080p.HEVC.x265-MeGusta[EZTVx.to]")
	mkdirAll(t, inputDir)

	mainSrc := filepath.Join(inputDir, "Stranger.Things.S05E07.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv")
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	assocSrc := filepath.Join(inputDir, "Stranger.Things.S05E07.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
	writeFile(t, assocSrc, "subtitle")

	pl, err := planOne(t, p, inputDir)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	// Swap in a transferer that fails for the associated src, but succeeds for others.
	failXfer := &failOneTransferer{
		failSrc:  assocSrc,
		delegate: p.xfer,
	}
	p.xfer = failXfer

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() should succeed even if associated move fails; got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Main must be moved
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("dest main missing (%s): %v", pl.DestMainPath, err)
	}
}

func TestApply_AssociatedMoveFailure_SkipsCleanup(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E09.1080p.HEVC.x265-MeGusta[EZTVx.to]")
	mkdirAll(t, inputDir)

	mainSrc := filepath.Join(inputDir, "Stranger.Things.S05E09.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv")
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	assocSrc := filepath.Join(inputDir, "Stranger.Things.S05E09.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
	writeFile(t, assocSrc, "subtitle")

	pl, err := planOne(t, p, inputDir)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if len(pl.Associated) == 0 {
		t.Fatalf("expected at least one associated move")
	}

	// Swap in a transferer that fails for the associated src, but succeeds for others.
	failXfer := &failOneTransferer{
		failSrc:  assocSrc,
		delegate: p.xfer,
	}
	p.xfer = failXfer

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() should succeed even if associated move fails; got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Main must be moved.
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("dest main missing (%s): %v", pl.DestMainPath, err)
	}

	// Input dir and associated file should remain (cleanup skipped).
	if st, err := os.Stat(inputDir); err != nil || !st.IsDir() {
		t.Fatalf("input dir missing after failed associated move: %v", err)
	}
	if _, err := os.Stat(assocSrc); err != nil {
		t.Fatalf("assoc source missing after failed move: %v", err)
	}
}

func TestApply_MainMoveCleanupFailureIsNonFatal(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	mainName := "The.Copenhagen.Test.S01E03.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	p.xfer = cleanupErrorTransferer{}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() should succeed even if cleanup fails; got error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Applied {
		t.Fatalf("Applied = false, want true")
	}

	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("dest main missing (%s): %v", pl.DestMainPath, err)
	}
	if _, err := os.Stat(mainSrc); err != nil {
		t.Fatalf("source missing after cleanup failure: %v", err)
	}
}

func TestApply_MainMoveDiskFull_ReturnsDestinationUnavailableError(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	mainName := "Stranger.Things.S05E09.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	p.xfer = enospcTransferer{}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Applied {
		t.Fatalf("Applied = true, want false")
	}

	var destErr *DestinationUnavailableError
	if !errors.As(err, &destErr) {
		t.Fatalf("expected *DestinationUnavailableError, got: %v", err)
	}
	if destErr.Category != CategoryShow {
		t.Fatalf("Category = %v, want %v", destErr.Category, CategoryShow)
	}
	if !errors.Is(destErr, syscall.ENOSPC) {
		t.Fatalf("expected wrapped error to satisfy errors.Is(syscall.ENOSPC): %v", destErr)
	}

	// Source must remain untouched: the move never actually happened.
	if _, err := os.Stat(mainSrc); err != nil {
		t.Fatalf("source should remain in place after a failed move: %v", err)
	}
}

func TestApply_AssociatedMoveDiskFull_ReturnsDestinationUnavailableError(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Deadwood.S01E04.1080p.HEVC.x265-MeGusta[EZTVx.to]")
	mkdirAll(t, inputDir)

	mainName := "Deadwood.S01E04.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(inputDir, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	assocSrc := filepath.Join(inputDir, "Deadwood.S01E04.1080p.HEVC.x265-MeGusta[EZTVx.to].en.srt")
	writeFile(t, assocSrc, "subtitle")

	pl, err := planOne(t, p, inputDir)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	// Main media moves fine; the associated file hits a full disk.
	p.xfer = &failOneWithENOSPC{failSrc: assocSrc, delegate: &osRenameTransferer{}}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	var destErr *DestinationUnavailableError
	if !errors.As(err, &destErr) {
		t.Fatalf("expected *DestinationUnavailableError, got: %v", err)
	}
	if destErr.Category != CategoryShow {
		t.Fatalf("Category = %v, want %v", destErr.Category, CategoryShow)
	}

	// The main file still applied successfully; only the associated move
	// escalated to a hard error instead of being swallowed as a warning.
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("dest main missing (%s): %v", pl.DestMainPath, err)
	}
}

func TestApply_DestDirPermissionDenied_ReturnsDestinationUnavailableError(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permission bits")
	}
	p := newTestProcessorWithExecDeps(t)

	mainName := "Deadwood.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	// Lock down ShowsDir itself so MkdirAll(pl.DestDir, ...) -- which runs
	// before the Transferer is ever invoked -- fails with permission denied.
	// This is the path a real chmod-000-destination hits first.
	if err := os.Chmod(p.cfg.ShowsDir, 0o000); err != nil {
		t.Fatalf("chmod ShowsDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(p.cfg.ShowsDir, 0o755) })

	results, err := p.Apply(context.Background(), []Plan{pl})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Applied {
		t.Fatalf("Applied = true, want false")
	}

	var destErr *DestinationUnavailableError
	if !errors.As(err, &destErr) {
		t.Fatalf("expected *DestinationUnavailableError, got: %v", err)
	}
	if destErr.Category != CategoryShow {
		t.Fatalf("Category = %v, want %v", destErr.Category, CategoryShow)
	}
	if !errors.Is(destErr, fs.ErrPermission) {
		t.Fatalf("expected wrapped error to satisfy errors.Is(fs.ErrPermission): %v", destErr)
	}
}

func TestApply_RefusesToDeleteDropFolderRoot_WhenInputIsRoot(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	// Put a show file directly in the drop folder
	mainSrc := filepath.Join(p.cfg.DropFolder, "Stranger.Things.S05E06.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv")
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	// Plan using the DROP FOLDER itself as input (directory input).
	// This will choose the main file from within it.
	pl, err := planOne(t, p, p.cfg.DropFolder)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Drop folder root must still exist (cleanup should refuse to delete it).
	if st, err := os.Stat(p.cfg.DropFolder); err != nil || !st.IsDir() {
		t.Fatalf("drop folder missing or not a dir after Apply: %v", err)
	}
}

func TestApply_AssociatedMoveFailure_EmitsConsoleWarn(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	drop := filepath.Join(root, "drop")
	movies := filepath.Join(root, "Movies")
	shows := filepath.Join(root, "Shows")
	mkdirAll(t, drop)
	mkdirAll(t, movies)
	mkdirAll(t, shows)

	var consoleBuf strings.Builder
	logger, err := logging.New(logging.Options{
		Stdout:               &consoleBuf,
		Stderr:               &consoleBuf,
		ConsoleLevel:         "WARN",
		HistoryLevel:         "WARN",
		HistoryFile:          filepath.Join(root, "history.jsonl"),
		HistoryInfoAllowlist: nil,
	})
	if err != nil {
		t.Fatalf("logging.New: %v", err)
	}

	cfg := Config{
		DropFolder:               drop,
		MoviesDir:                movies,
		ShowsDir:                 shows,
		MainMediaExtensions:      []string{".mkv"},
		AssociatedFileExtensions: []string{".srt"},
		MediaTagBlacklist:        []string{"1080p", "x265"},
	}
	pr, err := New(cfg, &osRenameTransferer{}, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	impl := pr.(*processorImpl)

	inputDir := filepath.Join(drop, "Stranger.Things.S05E10.1080p.x265")
	mkdirAll(t, inputDir)
	mainSrc := filepath.Join(inputDir, "Stranger.Things.S05E10.1080p.x265.mkv")
	assocSrc := filepath.Join(inputDir, "Stranger.Things.S05E10.1080p.x265.en.srt")
	writeFile(t, mainSrc, strings.Repeat("m", 64))
	writeFile(t, assocSrc, "subtitle")

	pl, err := planOne(t, impl, inputDir)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	impl.xfer = &failOneTransferer{failSrc: assocSrc, delegate: &osRenameTransferer{}}

	if _, err := impl.Apply(context.Background(), []Plan{pl}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	out := consoleBuf.String()
	if !strings.Contains(out, "associated file(s) not moved") {
		t.Fatalf("expected console warning about associated file failure; got:\n%s", out)
	}
	if !strings.Contains(out, filepath.Base(pl.MainSourcePath)) {
		t.Fatalf("expected main source filename in console warning; got:\n%s", out)
	}
}

// --- Test helpers ------------------------------------------------------------

func newTestProcessorWithExecDeps(t *testing.T) *processorImpl {
	t.Helper()
	p := newTestProcessor(t)
	p.xfer = &osRenameTransferer{}
	return p
}

type osRenameTransferer struct{}

func (tfer *osRenameTransferer) Move(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

type failOneTransferer struct {
	failSrc  string
	delegate Transferer
}

func (f *failOneTransferer) Move(ctx context.Context, src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(f.failSrc) {
		return errors.New("forced transfer failure for test")
	}
	return f.delegate.Move(ctx, src, dst)
}

type failOneWithENOSPC struct {
	failSrc  string
	delegate Transferer
}

func (f *failOneWithENOSPC) Move(ctx context.Context, src, dst string) error {
	if filepath.Clean(src) == filepath.Clean(f.failSrc) {
		return &fs.PathError{Op: "write", Path: dst, Err: syscall.ENOSPC}
	}
	return f.delegate.Move(ctx, src, dst)
}

type enospcTransferer struct{}

func (enospcTransferer) Move(ctx context.Context, _, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return &fs.PathError{Op: "write", Path: dst, Err: syscall.ENOSPC}
}

type cleanupErrorTransferer struct{}

func (t cleanupErrorTransferer) Move(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	return &transfer.CleanupError{
		Src: src,
		Dst: dst,
		Err: errors.New("forced cleanup failure for test"),
	}
}
