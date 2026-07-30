// internal/metadata/ffmpeg_tagger_test.go
package metadata

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeFakeFFmpeg writes a tiny POSIX shell script standing in for ffmpeg,
// so the temp-file-then-rename bookkeeping in WriteTitle can be tested
// without invoking a real ffmpeg binary. When succeed is true, it writes
// "tagged" to its last argument (the temp output path WriteTitle builds) and
// exits 0; otherwise it exits 1 without touching that path, mimicking an
// ffmpeg failure.
func writeFakeFFmpeg(t *testing.T, dir string, succeed bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg stand-in is a POSIX shell script; not supported on windows")
	}
	script := "#!/bin/sh\n"
	if succeed {
		// `for out; do :; done` is a portable POSIX idiom for "the last
		// positional argument" -- ffmpeg's own output path is always last.
		script += "for out; do :; done\nprintf 'tagged' > \"$out\"\n"
	} else {
		script += "echo 'fake ffmpeg failure' >&2\nexit 1\n"
	}
	path := filepath.Join(dir, "fake-ffmpeg.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture script, not a real credential/secret
		t.Fatalf("write fake ffmpeg script: %v", err)
	}
	return path
}

// writeArgRecordingFakeFFmpeg writes a fake ffmpeg that records its full
// argument list (one per line) to recordPath, then behaves like a
// successful ffmpeg run (writes "tagged" to its last argument).
func writeArgRecordingFakeFFmpeg(t *testing.T, dir, recordPath string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg stand-in is a POSIX shell script; not supported on windows")
	}
	script := "#!/bin/sh\n" +
		"for a; do printf '%s\\n' \"$a\" >> \"" + recordPath + "\"; done\n" +
		"for out; do :; done\n" +
		"printf 'tagged' > \"$out\"\n"
	path := filepath.Join(dir, "fake-ffmpeg-recording.sh")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture script, not a real credential/secret
		t.Fatalf("write fake ffmpeg script: %v", err)
	}
	return path
}

// leftoverTempFiles returns any WriteTitle temp-file names still present in
// dir, so tests can assert cleanup happened on both the success and failure
// paths.
func leftoverTempFiles(t *testing.T, dir string) []string {
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

// TestTailWriter_CapsRetainedBytesToTail guards against the unbounded
// stderr-capture memory blowup this type replaced: a subprocess that repeats
// a warning across an entire file (e.g. ffmpeg re-emitting a per-packet
// non-monotonic-DTS warning on a scene-release rip with malformed
// timestamps) must not grow the buffer past max, and what's retained must be
// the most recent bytes, not the first ones -- the tail is what's useful in
// an error message.
func TestTailWriter_CapsRetainedBytesToTail(t *testing.T) {
	w := &tailWriter{max: 16}
	for _, chunk := range []string{"0123456789", "abcdefghij", "ZZZZZ"} {
		if _, err := w.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write(%q): %v", chunk, err)
		}
	}
	if got := w.String(); got != "9abcdefghijZZZZZ" || len(got) != 16 {
		t.Fatalf("String() = %q, want the last 16 bytes written across all chunks", got)
	}
	if len(w.buf) > w.max {
		t.Fatalf("buf grew to %d bytes, want capped at max=%d", len(w.buf), w.max)
	}
}

// TestFFmpegTagger_WriteTitle_BoundsCapturedStderrOnFailure guards against
// the memory-usage bug this fix addresses directly: a chatty failing ffmpeg
// must not make WriteTitle's returned error balloon with the subprocess's
// entire stderr output.
func TestFFmpegTagger_WriteTitle_BoundsCapturedStderrOnFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Get Smart (2008).mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	script := "#!/bin/sh\n" +
		"i=0\n" +
		"while [ $i -lt 20000 ]; do\n" +
		"  echo 'repeated warning: non-monotonic DTS' >&2\n" +
		"  i=$((i + 1))\n" +
		"done\n" +
		"exit 1\n"
	scriptPath := filepath.Join(dir, "chatty-fake-ffmpeg.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil { //nolint:gosec // test fixture script, not a real credential/secret
		t.Fatalf("write fake ffmpeg script: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("fake ffmpeg stand-in is a POSIX shell script; not supported on windows")
	}

	tagger := &FFmpegTagger{ffmpegPath: scriptPath}
	err := tagger.WriteTitle(context.Background(), path, "Get Smart (2008)")
	if err == nil {
		t.Fatalf("expected WriteTitle to return an error")
	}
	if got := len(err.Error()); got > maxCapturedStderr+1024 {
		t.Fatalf("error message is %d bytes, want bounded near maxCapturedStderr=%d", got, maxCapturedStderr)
	}
}

func TestFFmpegTagger_WriteTitle_SuccessRenamesOverOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Get Smart (2008).mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	tagger := &FFmpegTagger{ffmpegPath: writeFakeFFmpeg(t, dir, true)}
	if err := tagger.WriteTitle(context.Background(), path, "Get Smart (2008)"); err != nil {
		t.Fatalf("WriteTitle: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path after WriteTitle: %v", err)
	}
	if string(got) != "tagged" {
		t.Fatalf("path contents = %q, want %q (fake ffmpeg output renamed into place)", got, "tagged")
	}
	if tmp := leftoverTempFiles(t, dir); len(tmp) != 0 {
		t.Fatalf("leftover temp file(s) after success: %v", tmp)
	}
}

func TestFFmpegTagger_WriteTitle_FailureLeavesOriginalUntouchedAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Get Smart (2008).mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	tagger := &FFmpegTagger{ffmpegPath: writeFakeFFmpeg(t, dir, false)}
	if err := tagger.WriteTitle(context.Background(), path, "Get Smart (2008)"); err == nil {
		t.Fatalf("expected WriteTitle to return an error")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read path after failed WriteTitle: %v", err)
	}
	if string(got) != "original" {
		t.Fatalf("original file was modified on ffmpeg failure: got %q", got)
	}
	if tmp := leftoverTempFiles(t, dir); len(tmp) != 0 {
		t.Fatalf("leftover temp file(s) after failure: %v", tmp)
	}
}

// TestFFmpegTagger_WriteTitle_SetsPermissiveModeOnSuccess guards against the
// permission regression os.CreateTemp introduces: it always creates the temp
// file at mode 0600 regardless of umask, and WriteTitle must restore
// group/other read before renaming it over the original, or every tagged
// file silently becomes unreadable by a media server running as another
// user (the same class of fix already applied to library destination dirs).
func TestFFmpegTagger_WriteTitle_SetsPermissiveModeOnSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Get Smart (2008).mkv")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	tagger := &FFmpegTagger{ffmpegPath: writeFakeFFmpeg(t, dir, true)}
	if err := tagger.WriteTitle(context.Background(), path, "Get Smart (2008)"); err != nil {
		t.Fatalf("WriteTitle: %v", err)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat path after WriteTitle: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o644 {
		t.Fatalf("mode after WriteTitle = %o, want 0644 (os.CreateTemp creates at 0600; WriteTitle must restore group/other read)", perm)
	}
}

// TestFFmpegTagger_WriteTitle_LongFilenameDoesNotExceedNameLimit guards
// against ENAMETOOLONG: the temp filename must not be built from the
// (possibly very long, scene-release-style) original basename, since
// appending a temp suffix on top of an already-long name can exceed common
// filesystem filename-length limits.
func TestFFmpegTagger_WriteTitle_LongFilenameDoesNotExceedNameLimit(t *testing.T) {
	dir := t.TempDir()
	longBase := strings.Repeat("A.Very.Long.Scene.Release.Name.With.Many.Tags-", 4) + "GROUP"
	path := filepath.Join(dir, longBase+".mkv")
	if len(filepath.Base(path)) < 190 {
		t.Fatalf("test fixture filename too short to exercise the long-name path: %d bytes", len(filepath.Base(path)))
	}
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatalf("seed original file: %v", err)
	}

	tagger := &FFmpegTagger{ffmpegPath: writeFakeFFmpeg(t, dir, true)}
	if err := tagger.WriteTitle(context.Background(), path, "New Title (2008)"); err != nil {
		t.Fatalf("WriteTitle failed for long filename %q (%d bytes): %v", filepath.Base(path), len(filepath.Base(path)), err)
	}
}

// TestFFmpegTagger_WriteTitle_MovflagsGatedByContainer guards against
// passing the mov/mp4/m4v-only -movflags use_metadata_tags option to a
// muxer it doesn't apply to (matroska has no use for it).
func TestFFmpegTagger_WriteTitle_MovflagsGatedByContainer(t *testing.T) {
	tests := []struct {
		ext      string
		wantFlag bool
	}{
		{".mp4", true},
		{".m4v", true},
		{".mov", true},
		{".mkv", false},
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "Get Smart (2008)"+tt.ext)
			if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
				t.Fatalf("seed original file: %v", err)
			}
			recordPath := filepath.Join(dir, "args.txt")

			tagger := &FFmpegTagger{ffmpegPath: writeArgRecordingFakeFFmpeg(t, dir, recordPath)}
			if err := tagger.WriteTitle(context.Background(), path, "Get Smart (2008)"); err != nil {
				t.Fatalf("WriteTitle: %v", err)
			}

			recorded, err := os.ReadFile(recordPath)
			if err != nil {
				t.Fatalf("read recorded args: %v", err)
			}
			gotFlag := strings.Contains(string(recorded), "use_metadata_tags")
			if gotFlag != tt.wantFlag {
				t.Fatalf("-movflags use_metadata_tags present = %v, want %v for extension %q (recorded args:\n%s)", gotFlag, tt.wantFlag, tt.ext, recorded)
			}
		})
	}
}

// TestFFmpegTagger_WriteTitle_Integration exercises the real ffmpeg command
// end to end against a tiny synthetic fixture generated on the fly (rather
// than a committed binary sample), skipping when ffmpeg/ffprobe aren't
// available in the test environment.
func TestFFmpegTagger_WriteTitle_Integration(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not found on PATH")
	}
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not found on PATH")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "sample.mp4")

	gen := exec.Command(ffmpegPath, //nolint:gosec // ffmpegPath resolved via exec.LookPath, args are fixed test fixture args
		"-f", "lavfi", "-i", "testsrc=duration=1:size=64x64:rate=1",
		"-metadata", "title=Stale Embedded Title",
		"-y", src,
	)
	var genStderr bytes.Buffer
	gen.Stderr = &genStderr
	if err := gen.Run(); err != nil {
		t.Fatalf("generate fixture: %v: %s", err, genStderr.String())
	}

	tagger, err := NewFFmpegTagger()
	if err != nil {
		t.Fatalf("NewFFmpegTagger: %v", err)
	}

	const wantTitle = "Get Smart (2008)"
	if err := tagger.WriteTitle(context.Background(), src, wantTitle); err != nil {
		t.Fatalf("WriteTitle: %v", err)
	}

	out, err := exec.Command(ffprobePath, //nolint:gosec // ffprobePath resolved via exec.LookPath, args are fixed test fixture args
		"-v", "error",
		"-show_entries", "format_tags=title",
		"-of", "default=nw=1:nk=1",
		src,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != wantTitle {
		t.Fatalf("title tag = %q, want %q", got, wantTitle)
	}
}
