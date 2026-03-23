package processor

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mtn-Man/mintmedia/internal/logging"
)

func TestProcess_OnResult_StreamedForAppliedPackFiles(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	inputDir := filepath.Join(p.cfg.DropFolder, "The.Copenhagen.Test.S01")
	mkdirAll(t, inputDir)
	writeFile(t, filepath.Join(inputDir, "The.Copenhagen.Test.S01E01.1080p.HEVC.x265.mkv"), "dummy")
	writeFile(t, filepath.Join(inputDir, "The.Copenhagen.Test.S01E02.1080p.HEVC.x265.mkv"), "dummy")

	var streamed []Result
	res, err := p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	if len(streamed) != len(res) {
		t.Fatalf("streamed=%d, returned=%d", len(streamed), len(res))
	}
	for i := range res {
		if !streamed[i].Applied {
			t.Fatalf("streamed[%d].Applied = false, want true", i)
		}
		if streamed[i].Plan.DestMainPath != res[i].Plan.DestMainPath {
			t.Fatalf("streamed[%d].DestMainPath = %q, want %q", i, streamed[i].Plan.DestMainPath, res[i].Plan.DestMainPath)
		}
	}
}

func TestProcess_OnResult_StreamedForHandledSkip(t *testing.T) {
	p := newTestProcessorWithExecDeps(t)

	input := filepath.Join(p.cfg.DropFolder, "notes.txt")
	writeFile(t, input, "not media")

	var streamed []Result
	res, err := p.Process(context.Background(), Request{
		InputPath: input,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(res) != 1 || len(streamed) != 1 {
		t.Fatalf("expected one result (returned=%d streamed=%d)", len(res), len(streamed))
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
	res, err := p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(res) != 2 || len(streamed) != 2 {
		t.Fatalf("expected 2 results (returned=%d streamed=%d)", len(res), len(streamed))
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
	var res []Result
	var err error
	var stderr strings.Builder
	p.logger = newRuntimeLoggerForProcessorTest(t, io.Discard, &stderr)
	res, err = p.Process(context.Background(), Request{
		InputPath: inputDir,
		OnResult: func(r Result) {
			streamed = append(streamed, r)
		},
	})
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	if len(res) != 2 || len(streamed) != 2 {
		t.Fatalf("expected 2 results (returned=%d streamed=%d)", len(res), len(streamed))
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
