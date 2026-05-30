//go:build linux

package processor

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// resolveTrashDir returns the XDG Trash files directory (~/.local/share/Trash/files).
// Note: does not write .trashinfo metadata files; most file managers will still
// show trashed items but without original-path/date metadata.
func resolveTrashDir() (string, error) {
	home, err := trashHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "Trash", "files"), nil
}

// resolvePreferredTrashDir returns the best trash directory for sourcePath.
// Prefers a volume-local trash ($TOPDIR/.Trash-$UID/files per the XDG spec) so
// that os.Rename always succeeds without a cross-device copy. Falls back to the
// home trash when the source is on the root filesystem or the mountpoint cannot
// be determined.
func resolvePreferredTrashDir(sourcePath string) (string, error) {
	dir := sourcePath
	if st, err := os.Stat(sourcePath); err == nil && !st.IsDir() {
		dir = filepath.Dir(sourcePath)
	}
	if mountpoint, err := findMountpoint(dir); err == nil && mountpoint != "/" {
		uid := os.Getuid()
		return filepath.Join(mountpoint, fmt.Sprintf(".Trash-%d", uid), "files"), nil
	}
	return resolveTrashDir()
}

// findMountpoint returns the mount point of the filesystem containing path by
// walking up until the device number changes.
func findMountpoint(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	sysStat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("unsupported stat type")
	}
	dev := sysStat.Dev

	current := abs
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return current, nil
		}
		parentInfo, err := os.Lstat(parent)
		if err != nil {
			return "", err
		}
		parentStat, ok := parentInfo.Sys().(*syscall.Stat_t)
		if !ok {
			return "", fmt.Errorf("unsupported stat type for parent")
		}
		if parentStat.Dev != dev {
			return current, nil
		}
		current = parent
	}
}
