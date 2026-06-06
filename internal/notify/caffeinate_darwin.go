//go:build darwin

package notify

import "os/exec"

func inhibitCmd() *exec.Cmd {
	return exec.Command("caffeinate", "-i")
}
