// internal/metadata/ffmpeg_tagger.go
package metadata

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxCapturedStderr bounds how much of ffmpeg's stderr tailWriter retains.
// ffmpeg's stderr is normally just a rolling progress line, but scene-release
// files with malformed timestamps can make it repeat a per-packet warning
// hundreds of thousands of times during a stream-copy remux -- capturing that
// unbounded would let a single problem file balloon process memory. Only the
// tail is useful for the error message anyway.
const maxCapturedStderr = 64 * 1024

// tailWriter retains only the last max bytes written to it.
type tailWriter struct {
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	n := len(p)
	if len(p) >= w.max {
		w.buf = append(w.buf[:0], p[len(p)-w.max:]...)
		return n, nil
	}
	// Shift the retained tail down in place rather than re-slicing from the
	// front: re-slicing shrinks cap() along with the offset, so a flood of
	// small writes (e.g. ffmpeg repeating a per-packet warning) would force a
	// fresh allocation and full copy on nearly every call. Shifting in place
	// reuses the same backing array for the writer's whole lifetime.
	if overflow := len(w.buf) + len(p) - w.max; overflow > 0 {
		copy(w.buf, w.buf[overflow:])
		w.buf = w.buf[:len(w.buf)-overflow]
	}
	w.buf = append(w.buf, p...)
	return n, nil
}

func (w *tailWriter) String() string {
	return string(w.buf)
}

// FFmpegTagger rewrites a media file's embedded container "title" metadata
// tag by shelling out to ffmpeg. It remuxes (stream copy, no re-encode) into
// a fresh temp file in the same directory as the source, so a failure at any
// point leaves the source completely untouched. WriteTitleToFile hands that
// temp file back to the caller (Apply moves it straight into the library);
// WriteTitle is the in-place variant that atomically renames the temp over
// the source -- the same same-filesystem-then-atomic-rename pattern
// transfer.RenameOrCopy uses.
type FFmpegTagger struct {
	ffmpegPath string
}

// NewFFmpegTagger resolves ffmpeg on PATH once at construction time. PATH
// presence is knowable upfront (unlike a network dependency), so this is a
// one-time deterministic check rather than something retried per file.
func NewFFmpegTagger() (*FFmpegTagger, error) {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found on PATH: %w", err)
	}
	return &FFmpegTagger{ffmpegPath: path}, nil
}

// WriteTitleToFile remuxes src into a fresh sibling temp file (same
// directory, same filesystem) with its container "title" metadata tag
// rewritten to title, and returns that temp file's path. src is never
// modified. On any failure the temp file is removed and "" is returned; on
// success the caller owns the returned path and is responsible for moving or
// removing it.
func (t *FFmpegTagger) WriteTitleToFile(ctx context.Context, src, title string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	dir := filepath.Dir(src)
	ext := filepath.Ext(src)

	// The temp name doesn't need to resemble the original filename -- only
	// the extension matters for ffmpeg's muxer autodetection -- so it isn't
	// built from the (possibly very long, scene-release-style) original
	// basename, which could otherwise push the temp path past common
	// filesystem filename-length limits.
	tmpFile, err := os.CreateTemp(dir, ".mmtag-tmp-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close temp file: %w", err)
	}

	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmp)
		}
	}()

	args := []string{
		"-y",
		"-i", src,
		"-map", "0",
		"-c", "copy",
		"-metadata", "title=" + title,
	}
	// use_metadata_tags is a private AVOption of the mov/mp4/m4v muxer
	// family; it has no meaning for matroska, so it's only added for the
	// containers it actually applies to.
	if needsMovMetadataFlag(ext) {
		args = append(args, "-movflags", "use_metadata_tags")
	}
	args = append(args, tmp)

	// src and title are always derived internally from a resolved Plan
	// (MainSourcePath, DestRadix), never external input.
	cmd := exec.CommandContext(ctx, t.ffmpegPath, args...) //nolint:gosec // src/title come from an internally-computed Plan, not external input
	stderr := &tailWriter{max: maxCapturedStderr}
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Restore the permissive mode CreateTemp doesn't grant (it always
	// creates at 0600) so the media server can still read the tagged file
	// once it lands in the library -- matches transfer.copyThenReplace's
	// identical chmod before its own atomic rename.
	_ = os.Chmod(tmp, 0o644) //nolint:gosec // library files need group+other read for the media server

	ok = true
	return tmp, nil
}

// WriteTitle rewrites src's container title metadata tag to title in place,
// via a remux into a sibling temp file (WriteTitleToFile) followed by an
// atomic rename over src. A failure at any point leaves src completely
// untouched.
func (t *FFmpegTagger) WriteTitle(ctx context.Context, src, title string) error {
	tmp, err := t.WriteTitleToFile(ctx, src, title)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, src); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename temp file to destination: %w", err)
	}
	return nil
}
