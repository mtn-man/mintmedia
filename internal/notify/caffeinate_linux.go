//go:build linux

package notify

import "os/exec"

func inhibitCmd() *exec.Cmd {
	return exec.Command("systemd-inhibit",
		"--what=idle:sleep",
		"--who=mintmedia",
		"--why=processing media",
		"--mode=delay",
		"sleep", "infinity",
	)
}
