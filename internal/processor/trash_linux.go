//go:build linux

package processor

import "path/filepath"

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
