//go:build !darwin && !linux

package notify

import "os/exec"

func inhibitCmd() *exec.Cmd {
	return nil
}
