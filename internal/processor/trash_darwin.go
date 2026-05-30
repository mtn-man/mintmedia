//go:build darwin

package processor

import "path/filepath"

func resolveTrashDir() (string, error) {
	home, err := trashHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".Trash"), nil
}

func resolvePreferredTrashDir(_ string) (string, error) {
	return resolveTrashDir()
}
