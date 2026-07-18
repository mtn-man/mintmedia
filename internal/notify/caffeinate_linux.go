//go:build linux

package notify

import "os/exec"

// inhibitCmd returns the systemd-inhibit invocation used to prevent idle
// sleep, or nil on distros without systemd (e.g. Alpine/OpenRC) so the
// caller treats sleep inhibition as unsupported rather than failing to
// exec a missing binary.
func inhibitCmd() *exec.Cmd {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return nil
	}
	return exec.Command("systemd-inhibit",
		"--what=idle:sleep",
		"--who=mintmedia",
		"--why=processing media",
		"--mode=delay",
		"sleep", "infinity",
	)
}
