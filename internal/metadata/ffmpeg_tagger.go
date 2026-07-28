// internal/metadata/ffmpeg_tagger.go
package metadata

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FFmpegTagger rewrites a media file's embedded container "title" metadata
// tag by shelling out to ffmpeg. It remuxes (stream copy, no re-encode) into
// a temp file in the same directory as the target, then atomically renames
// over it -- the same same-filesystem-then-atomic-rename pattern
// transfer.RenameOrCopy uses, so a failure at any point leaves the original
// file completely untouched.
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

// WriteTitle rewrites path's container title metadata tag to title in place.
func (t *FFmpegTagger) WriteTitle(ctx context.Context, path, title string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)

	// The temp name doesn't need to resemble the original filename -- only
	// the extension matters for ffmpeg's muxer autodetection -- so it isn't
	// built from the (possibly very long, scene-release-style) original
	// basename, which could otherwise push the temp path past common
	// filesystem filename-length limits.
	tmpFile, err := os.CreateTemp(dir, ".mmtag-tmp-*"+ext)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := tmpFile.Name()
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close temp file: %w", err)
	}

	cleanupTmp := true
	defer func() {
		if cleanupTmp {
			_ = os.Remove(tmp)
		}
	}()

	args := []string{
		"-y",
		"-i", path,
		"-map", "0",
		"-c", "copy",
		"-metadata", "title=" + title,
	}
	// use_metadata_tags is a private AVOption of the mov/mp4/m4v muxer
	// family; it has no meaning for matroska, so it's only added for the
	// containers it actually applies to.
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov":
		args = append(args, "-movflags", "use_metadata_tags")
	}
	args = append(args, tmp)

	// path and title are always derived internally from a resolved Plan
	// (DestMainPath's pre-move source, DestRadix), never external input.
	cmd := exec.CommandContext(ctx, t.ffmpegPath, args...) //nolint:gosec // path/title come from an internally-computed Plan, not external input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	// Restore the permissive mode CreateTemp doesn't grant (it always
	// creates at 0600) so the media server can still read the tagged file --
	// matches transfer.copyThenReplace's identical chmod before its own
	// atomic rename.
	_ = os.Chmod(tmp, 0o644) //nolint:gosec // library files need group+other read for the media server

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp file to destination: %w", err)
	}
	cleanupTmp = false
	return nil
}
