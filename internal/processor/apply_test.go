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

// TestApply_MultiEpisodeDir_DuplicateSiblingBlocksCleanup covers a
// season-pack batch where one episode is a pre-existing duplicate (skipped,
// left in place) and its sibling is new (applied normally). DeleteEmptyInputDir
// is only set on the last-planned sibling (see plan()), so if the duplicate
// skip weren't tracked across the whole batch, the successful sibling's
// cleanup would trash the input directory -- taking the still-unmoved
// duplicate file down with it. It must not: the input directory must survive
// with the duplicate's source file still inside it.
func TestApply_MultiEpisodeDir_DuplicateSiblingBlocksCleanup(t *testing.T) {
	t.Parallel()

	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Deadwood.S01")
	mkdirAll(t, inputDir)

	ep1 := "Deadwood.S01E01.1080p.HEVC.x265-MeGusta.mkv"
	ep2 := "Deadwood.S01E02.1080p.HEVC.x265-MeGusta.mkv"
	ep1Src := filepath.Join(inputDir, ep1)
	ep2Src := filepath.Join(inputDir, ep2)
	writeFile(t, ep1Src, strings.Repeat("m", 64))
	writeFile(t, ep2Src, strings.Repeat("m", 64))

	// Episode 1 already exists in the library; episode 2 is new.
	writeFile(t, filepath.Join(p.cfg.ShowsDir, "Deadwood", "Season 01", "Deadwood - S01E01.mkv"), "already here")

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

	var dupResult, appliedResult *Result
	for i := range results {
		if results[i].Plan.Duplicate {
			dupResult = &results[i]
		} else {
			appliedResult = &results[i]
		}
	}
	if dupResult == nil || appliedResult == nil {
		t.Fatalf("expected one duplicate and one applied result, got: %+v", results)
		return
	}
	if dupResult.Applied {
		t.Fatalf("duplicate result Applied = true, want false")
	}
	if !appliedResult.Applied {
		t.Fatalf("non-duplicate result Applied = false, want true")
	}

	// The directory must survive, still containing episode 1's unmoved file.
	if _, err := os.Stat(inputDir); err != nil {
		t.Fatalf("input dir should survive (duplicate sibling left in place): %v", err)
	}
	if _, err := os.Stat(ep1Src); err != nil {
		t.Fatalf("episode 1 source should remain untouched: %v", err)
	}
	if _, err := os.Stat(ep2Src); !os.IsNotExist(err) {
		t.Fatalf("episode 2 source should have been moved, stat err=%v", err)
	}
}

// TestApply_Duplicate_SkipsWithoutMoving covers the Plan-time-detected
// duplicate path: applyOne must report a graceful skip and must never touch
// the Transferer at all for a plan with Duplicate already set.
func TestApply_Duplicate_SkipsWithoutMoving(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, "dummy")

	existing := filepath.Join(p.cfg.MoviesDir, "Get Smart (2008)", "Get Smart (2008).mkv")
	writeFile(t, existing, "already here")

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}

	p.xfer = failIfCalledTransferer{t: t}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Applied {
		t.Fatalf("Applied = true, want false")
	}
	if !res.Handled {
		t.Fatalf("Handled = false, want true")
	}
	if !strings.Contains(res.Reason, "already in library") {
		t.Fatalf("Reason = %q, want it to mention the library conflict", res.Reason)
	}

	// The source file must be untouched -- a skipped duplicate is not moved,
	// not deleted.
	if _, err := os.Stat(mainSrc); err != nil {
		t.Fatalf("source should remain in place: %v", err)
	}
}

// TestApply_FuzzyDuplicate_ReasonCitesExistingFolder covers the fuzzy
// (tier 1) duplicate case: the skip reason must cite the actual existing
// library folder that caused the match (pl.DuplicateMatchPath), not
// pl.DestMainPath, since that's the incoming file's own never-created path
// and would be misleading here (it names a folder spelled differently from
// what's actually in the library).
func TestApply_FuzzyDuplicate_ReasonCitesExistingFolder(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	mainName := "Amelie.2001.1080p.BluRay.x264-GROUP.mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, "dummy")

	existing := filepath.Join(p.cfg.MoviesDir, "Amélie (2001)", "Amélie (2001).mkv")
	writeFile(t, existing, "already here")

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if !pl.Duplicate {
		t.Fatalf("Duplicate = false, want true")
	}

	p.xfer = failIfCalledTransferer{t: t}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Applied {
		t.Fatalf("Applied = true, want false")
	}
	if strings.Contains(res.Reason, "Amelie (2001)") {
		t.Fatalf("Reason = %q, must not cite the incoming file's own never-created path", res.Reason)
	}
	if !strings.Contains(res.Reason, "Amélie (2001)") {
		t.Fatalf("Reason = %q, want it to cite the existing library folder", res.Reason)
	}
}

// TestApply_DuplicateRace_DowngradesToGracefulSkip covers the TOCTOU case:
// Plan saw no duplicate (pl.Duplicate == false), but another job/batch item
// claimed the destination before this Move ran. The real transfer.RenameOrCopy
// refuses to overwrite and returns a transfer.ErrDestinationExists-wrapped
// error; applyOne must downgrade that to the same graceful skip Result as
// the Plan-time-detected case rather than propagating a hard error.
func TestApply_DuplicateRace_DowngradesToGracefulSkip(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, "dummy")

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (nothing exists yet at plan time)")
	}

	// Simulate another job winning the race between Plan and Apply.
	writeFile(t, pl.DestMainPath, "claimed by another job")

	p.xfer = transfer.NewRenameOrCopy(transfer.Options{})

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Applied {
		t.Fatalf("Applied = true, want false")
	}
	if !res.Handled {
		t.Fatalf("Handled = false, want true")
	}
	if !strings.Contains(res.Reason, "already in library") {
		t.Fatalf("Reason = %q, want it to mention the library conflict", res.Reason)
	}

	// The source file must be untouched -- the race loser doesn't get moved.
	if _, err := os.Stat(mainSrc); err != nil {
		t.Fatalf("source should remain in place: %v", err)
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

// TestApply_MetadataTagger_WriteTitleCalledBeforeMove covers the reordering
// decision: WriteTitleToFile must run against pl.MainSourcePath (the
// pre-move, drop-folder location) before the main-media move, not
// pl.DestMainPath afterward -- doing it before the move keeps ffmpeg's remux
// on local disk instead of a possibly network-mounted destination. It also
// covers the post-move bookkeeping: the retitled temp file is what lands at
// pl.DestMainPath, and the untouched drop-folder original is removed once
// that move succeeds.
func TestApply_MetadataTagger_WriteTitleCalledBeforeMove(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		mainName string
	}{
		{"Movie", "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"},
		{"Show", "Deadwood.S01E01.1080p.HEVC.x265-MeGusta[EZTVx.to].mkv"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := newTestProcessorWithExecDeps(t)

			tagger := &fakeMetaTagger{}
			p.metaTagger = tagger

			mainSrc := filepath.Join(p.cfg.DropFolder, tc.mainName)
			writeFile(t, mainSrc, strings.Repeat("m", 64))

			pl, err := planOne(t, p, mainSrc)
			if err != nil {
				t.Fatalf("Plan() error: %v", err)
			}

			results, err := p.Apply(context.Background(), []Plan{pl})
			if err != nil {
				t.Fatalf("Apply() error: %v", err)
			}
			if len(results) != 1 || !results[0].Applied {
				t.Fatalf("expected 1 applied result, got %+v", results)
			}

			if len(tagger.calls) != 1 {
				t.Fatalf("expected 1 WriteTitleToFile call, got %d", len(tagger.calls))
			}
			call := tagger.calls[0]
			if call.Path != pl.MainSourcePath {
				t.Fatalf("WriteTitleToFile src = %q, want pl.MainSourcePath %q", call.Path, pl.MainSourcePath)
			}
			if call.Title != pl.DestRadix {
				t.Fatalf("WriteTitleToFile title = %q, want pl.DestRadix %q", call.Title, pl.DestRadix)
			}
			if !call.SourceExistedYet {
				t.Fatalf("WriteTitleToFile ran after the main-media move -- source no longer existed at call time")
			}
			if _, err := os.Stat(pl.DestMainPath); err != nil {
				t.Fatalf("retitled file should have landed at pl.DestMainPath: %v", err)
			}
			if _, err := os.Stat(pl.MainSourcePath); !os.IsNotExist(err) {
				t.Fatalf("drop-folder original should be gone after a successful tagged move, stat err = %v", err)
			}
			if leftover := leftoverTagTempFiles(t, p.cfg.DropFolder); len(leftover) != 0 {
				t.Fatalf("retitled temp file(s) left behind in drop folder: %v", leftover)
			}
		})
	}
}

// TestApply_MetadataTagger_WriteTitleErrorIsNonFatal covers the non-blocking
// contract: a WriteTitle failure must log and continue, never prevent the
// main-media move or flip Result.Applied to false.
func TestApply_MetadataTagger_WriteTitleErrorIsNonFatal(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	tagger := &fakeMetaTagger{err: errors.New("forced ffmpeg failure for test")}
	p.metaTagger = tagger

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
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
	if !results[0].Applied {
		t.Fatalf("Applied = false, want true -- a WriteTitle failure must not block the move")
	}
	if len(tagger.calls) != 1 {
		t.Fatalf("expected 1 WriteTitle call, got %d", len(tagger.calls))
	}
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("main media should still have been moved despite the tag failure: %v", err)
	}
}

// TestApply_MetadataTagger_WriteTitleFailure_EmitsConsoleWarn mirrors
// TestApply_AssociatedMoveFailure_EmitsConsoleWarn: a WriteTitle failure must
// actually reach the console, not just get swallowed as a silent no-op.
func TestApply_MetadataTagger_WriteTitleFailure_EmitsConsoleWarn(t *testing.T) {
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
	tagger := &fakeMetaTagger{err: errors.New("forced ffmpeg failure for test")}
	pr, err := New(cfg, &osRenameTransferer{}, tagger, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	impl := pr.(*processorImpl)

	mainSrc := filepath.Join(drop, "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv")
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, impl, mainSrc)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	if _, err := impl.Apply(context.Background(), []Plan{pl}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	out := consoleBuf.String()
	if !strings.Contains(out, "metadata title tag not updated") {
		t.Fatalf("expected console warning about the metadata tag failure; got:\n%s", out)
	}
	if !strings.Contains(out, filepath.Base(pl.MainSourcePath)) {
		t.Fatalf("expected main source filename in console warning; got:\n%s", out)
	}
}

// TestApply_MetadataTagger_NilTaggerNeverCalled covers the feature-disabled
// default: a nil metaTagger must never be dereferenced, and Apply must
// proceed exactly as it did before this feature existed.
func TestApply_MetadataTagger_NilTaggerNeverCalled(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)
	// p.metaTagger is nil by default (feature disabled) -- if apply.go ever
	// called through it unconditionally, this would panic on a nil interface.

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
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
	if len(results) != 1 || !results[0].Applied {
		t.Fatalf("expected 1 applied result, got %+v", results)
	}
	if _, err := os.Stat(pl.DestMainPath); err != nil {
		t.Fatalf("main media should have been moved: %v", err)
	}
}

// TestApply_MetadataTagger_UnsupportedExtensionSkipsCall covers the
// extension-allowlist gate: an unsupported container (.avi here) is an
// expected, normal case that must skip WriteTitle silently, not attempt and
// fail it.
func TestApply_MetadataTagger_UnsupportedExtensionSkipsCall(t *testing.T) {
	t.Parallel()
	p := newTestProcessorWithExecDeps(t)

	tagger := &fakeMetaTagger{}
	p.metaTagger = tagger

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.avi"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	writeFile(t, mainSrc, strings.Repeat("m", 64))

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.MainExt != ".avi" {
		t.Fatalf("MainExt = %q, want .avi", pl.MainExt)
	}

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 || !results[0].Applied {
		t.Fatalf("expected 1 applied result, got %+v", results)
	}
	if len(tagger.calls) != 0 {
		t.Fatalf("expected no WriteTitle calls for an unsupported extension, got %d", len(tagger.calls))
	}
}

// TestApply_MetadataTagger_SkipsWriteTitleWhenDestinationClaimedConcurrently
// covers the fast-path stat recheck: if another job has already claimed
// pl.DestMainPath by the time this plan reaches the tagging step (the same
// race TestApply_DuplicateRace_DowngradesToGracefulSkip covers for the Move
// call itself), WriteTitleToFile is skipped entirely -- no point spawning
// ffmpeg for a plan about to become a duplicate skip.
func TestApply_MetadataTagger_SkipsWriteTitleWhenDestinationClaimedConcurrently(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)

	tagger := &fakeMetaTagger{}
	p.metaTagger = tagger

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	const original = "dummy"
	writeFile(t, mainSrc, original)

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (nothing exists yet at plan time)")
	}

	// Simulate another job winning the race between Plan and this Apply call.
	writeFile(t, pl.DestMainPath, "claimed by another job")

	// The naive osRenameTransferer used by newTestProcessorWithExecDeps
	// doesn't check for an existing destination before renaming over it --
	// use the real transferer so the race is actually detected, exactly
	// like TestApply_DuplicateRace_DowngradesToGracefulSkip does.
	p.xfer = transfer.NewRenameOrCopy(transfer.Options{})

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Applied {
		t.Fatalf("Applied = true, want false")
	}
	if !strings.Contains(res.Reason, "already in library") {
		t.Fatalf("Reason = %q, want it to mention the library conflict", res.Reason)
	}

	if len(tagger.calls) != 0 {
		t.Fatalf("expected no WriteTitle calls when the destination is already claimed, got %d", len(tagger.calls))
	}
	got, err := os.ReadFile(mainSrc)
	if err != nil {
		t.Fatalf("read source after Apply: %v", err)
	}
	if string(got) != original {
		t.Fatalf("source file was modified despite being left as a duplicate skip: got %q, want %q", got, original)
	}
}

// TestApply_MetadataTagger_DestinationClaimedDuringRemux_LeavesSourceUntouched
// is the regression guard for the atomic-tag-then-move fix. The stat recheck
// passes (nothing claims the destination until the "remux" is already in
// flight), so WriteTitleToFile runs -- but because it writes a separate temp
// file and never pl.MainSourcePath, a destination claimed mid-remux makes
// the move downgrade to a duplicate skip with the drop-folder original still
// byte-for-byte intact. Before the fix (remux-in-place then move) the
// original was silently left as a retitled remux instead.
func TestApply_MetadataTagger_DestinationClaimedDuringRemux_LeavesSourceUntouched(t *testing.T) {
	t.Parallel()
	p := newTestProcessor(t)
	p.xfer = transfer.NewRenameOrCopy(transfer.Options{})

	mainName := "Get.Smart.2008.1080p.BluRay.x264-GROUP.mkv"
	mainSrc := filepath.Join(p.cfg.DropFolder, mainName)
	const original = "original untouched bytes"
	writeFile(t, mainSrc, original)

	pl, err := planOne(t, p, mainSrc)
	if err != nil {
		t.Fatalf("Plan() error: %v", err)
	}
	if pl.Duplicate {
		t.Fatalf("Duplicate = true, want false (nothing exists yet at plan time)")
	}

	tagger := &fakeMetaTagger{onCall: func() {
		// Another job claims the destination while ffmpeg would still be
		// running -- after the stat recheck, before the move.
		writeFile(t, pl.DestMainPath, "claimed mid-remux by another job")
	}}
	p.metaTagger = tagger

	results, err := p.Apply(context.Background(), []Plan{pl})
	if err != nil {
		t.Fatalf("Apply() error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	res := results[0]
	if res.Applied {
		t.Fatalf("Applied = true, want false -- destination was claimed mid-remux")
	}
	if !strings.Contains(res.Reason, "already in library") {
		t.Fatalf("Reason = %q, want it to mention the library conflict", res.Reason)
	}
	if len(tagger.calls) != 1 {
		t.Fatalf("expected 1 WriteTitleToFile call (the recheck passed), got %d", len(tagger.calls))
	}
	got, err := os.ReadFile(mainSrc)
	if err != nil {
		t.Fatalf("read source after Apply: %v", err)
	}
	if string(got) != original {
		t.Fatalf("drop-folder original was mutated despite the duplicate skip: got %q, want %q", got, original)
	}
	if leftover := leftoverTagTempFiles(t, p.cfg.DropFolder); len(leftover) != 0 {
		t.Fatalf("retitled temp file(s) left behind after the skip: %v", leftover)
	}
}

// leftoverTagTempFiles returns any metadata-tagging temp-file names still
// present in dir, so tests can assert the retitled remux is cleaned up on
// both the move-succeeded and duplicate-skip paths.
func leftoverTagTempFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}
	var tmp []string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".mmtag-tmp-") {
			tmp = append(tmp, e.Name())
		}
	}
	return tmp
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
	pr, err := New(cfg, &osRenameTransferer{}, nil, logger)
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

// failIfCalledTransferer fails the test immediately if Move is ever invoked,
// for asserting a code path that must short-circuit before touching the
// Transferer at all.
type failIfCalledTransferer struct {
	t *testing.T
}

func (f failIfCalledTransferer) Move(_ context.Context, src, dst string) error {
	f.t.Helper()
	f.t.Fatalf("Move(%q, %q) should not have been called", src, dst)
	return nil
}

// fakeMetaTagger is the fake MetadataTagger idiom used by the metadata
// tagging tests, mirroring the fake-Transferer types below. SourceExistedYet
// records whether src still existed on disk at call time (it always should --
// WriteTitleToFile never touches src). onCall, if set, runs after the call
// is recorded but before the temp file is returned, letting a test simulate
// another process claiming the destination while the "remux" is in flight.
type fakeMetaTaggerCall struct {
	Path, Title      string
	SourceExistedYet bool
}

type fakeMetaTagger struct {
	calls  []fakeMetaTaggerCall
	err    error
	onCall func()
}

func (f *fakeMetaTagger) WriteTitleToFile(_ context.Context, src, title string) (string, error) {
	_, statErr := os.Stat(src)
	f.calls = append(f.calls, fakeMetaTaggerCall{
		Path:             src,
		Title:            title,
		SourceExistedYet: statErr == nil,
	})
	if f.onCall != nil {
		f.onCall()
	}
	if f.err != nil {
		return "", f.err
	}
	// Mirror FFmpegTagger: a fresh sibling temp file, never a write to src.
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(src), ".mmtag-tmp-*"+filepath.Ext(src))
	if err != nil {
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
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
