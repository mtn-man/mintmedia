package processor

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtn-man/mintmedia/internal/logging"
)

func TestProcess_OnResult_StreamedForAppliedPackFiles(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "The.Copenhagen.Test.S01")
	mkdirAll(t, inputDir)
	writeFile(t, filepath.Join(inputDir, "The.Copenhagen.Test.S01E01.1080p.HEVC.x265.mkv"), "dummy")
	writeFile(t, filepath.Join(inputDir, "The.Copenhagen.Test.S01E02.1080p.HEVC.x265.mkv"), "dummy")

	var streamed []Result
	err := p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(streamed) != 2 {
		t.Fatalf("expected 2 streamed results, got %d", len(streamed))
	}
	for i := range streamed {
		if !streamed[i].Applied {
			t.Fatalf("streamed[%d].Applied = false, want true", i)
		}
		if streamed[i].Plan.DestMainPath == "" {
			t.Fatalf("streamed[%d].DestMainPath is empty", i)
		}
	}
}

func TestProcess_OnResult_StreamedForHandledSkip(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	input := filepath.Join(p.cfg.DropFolder, "notes.txt")
	writeFile(t, input, "not media")

	var streamed []Result
	err := p.Process(context.Background(), Request{
		InputPath: input,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(streamed) != 1 {
		t.Fatalf("expected 1 streamed result, got %d", len(streamed))
	}
	if streamed[0].Reason != ErrNotMedia.Error() {
		t.Fatalf("Reason = %q, want %q", streamed[0].Reason, ErrNotMedia.Error())
	}
}

func TestProcess_OnResult_StreamedForPartialPackSkip(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04")
	mkdirAll(t, inputDir)
	writeFile(t, filepath.Join(inputDir, "S01E01.mkv"), "dummy")
	writeFile(t, filepath.Join(inputDir, "Episode01.mkv"), "dummy")

	var streamed []Result
	err := p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(streamed) != 2 {
		t.Fatalf("expected 2 streamed results, got %d", len(streamed))
	}
	if !streamed[0].Applied {
		t.Fatalf("streamed[0].Applied = false, want true")
	}
	if streamed[1].Applied {
		t.Fatalf("streamed[1].Applied = true, want false")
	}
	if streamed[1].Reason == "" {
		t.Fatalf("streamed[1].Reason is empty, want parse error message")
	}
}

func TestProcess_OnResult_StreamedForMoviePackPartialSkip_AndWarns(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "The Jason Bourne Collection 2004-2016 1080p BluRay HEVC x265 5.1 BONE")
	mkdirAll(t, inputDir)
	unparseable := filepath.Join(inputDir, "1080p.x265.hevc.bluray.mkv")
	writeFile(t, unparseable, "dummy")
	writeFile(t, filepath.Join(inputDir, "The Bourne Identity 2002 1080p BluRay HEVC x265 5.1 BONE.mkv"), "dummy")

	var streamed []Result
	var stderr strings.Builder
	p.logger = newRuntimeLoggerForProcessorTest(t, io.Discard, &stderr)
	err := p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(streamed) != 2 {
		t.Fatalf("expected 2 streamed results, got %d", len(streamed))
	}

	var appliedCount int
	var skippedCount int
	var sawSkipPath bool
	for i := range streamed {
		if streamed[i].Applied {
			appliedCount++
			continue
		}
		skippedCount++
		if streamed[i].Plan.InputPath == unparseable {
			sawSkipPath = true
		}
	}
	if appliedCount != 1 || skippedCount != 1 {
		t.Fatalf("applied/skipped = %d/%d, want 1/1", appliedCount, skippedCount)
	}
	if !sawSkipPath {
		t.Fatalf("missing skipped reason for unparseable path %q (reasons: %#v)", unparseable, streamed)
	}

	wantWarn := "movie pack skipped (unparseable filename): " + unparseable + ":"
	if !strings.Contains(stderr.String(), wantWarn) {
		t.Fatalf("stderr missing warning %q; got: %q", wantWarn, stderr.String())
	}
}

func TestProcess_PartialSkipWarnedOnceAcrossRepeatedProcessCalls(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "Sherlock.Season.1-4.S01-S04")
	mkdirAll(t, inputDir)
	writeFile(t, filepath.Join(inputDir, "S01E01.mkv"), "dummy")
	unparseable := filepath.Join(inputDir, "Episode01.mkv")
	writeFile(t, unparseable, "dummy")

	var stderr strings.Builder
	p.logger = newRuntimeLoggerForProcessorTest(t, io.Discard, &stderr)

	run := func() []Result {
		var streamed []Result
		err := p.Process(context.Background(), Request{
			InputPath: inputDir,
			OnResult: func(r Result) {
				streamed = append(streamed, r)
			},
		})
		if err != nil {
			t.Fatalf("Process() error: %v", err)
		}
		return streamed
	}

	first := run()
	if len(first) != 2 {
		t.Fatalf("first call: expected 2 streamed results, got %d", len(first))
	}
	warnCount := strings.Count(stderr.String(), unparseable)
	if warnCount == 0 {
		t.Fatalf("first call: expected a warning mentioning %q, got none", unparseable)
	}

	// Re-running Process on the same folder simulates the daemon rescanning
	// a pack that's still receiving files -- the unparseable file is still
	// sitting there untouched, so it would otherwise warn again every time.
	second := run()
	if len(second) != 0 {
		t.Fatalf("second call: expected 0 streamed results (already-warned skip suppressed), got %d: %#v", len(second), second)
	}
	if got := strings.Count(stderr.String(), unparseable); got != warnCount {
		t.Fatalf("second call: expected no additional warning, mention count went from %d to %d", warnCount, got)
	}
}

func newRuntimeLoggerForProcessorTest(t *testing.T, stdout, stderr io.Writer) logging.Logger {
	t.Helper()
	l, err := logging.New(logging.Options{
		Stdout:               stdout,
		Stderr:               stderr,
		ConsoleLevel:         "INFO",
		HistoryLevel:         "WARN",
		HistoryFile:          filepath.Join(t.TempDir(), "history.jsonl"),
		HistoryInfoAllowlist: logging.DefaultHistoryInfoAllowlist(),
	})
	if err != nil {
		t.Fatalf("logging.New() error: %v", err)
	}
	return l
}
