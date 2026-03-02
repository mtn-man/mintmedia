package notify

import (
	"fmt"
	"strings"
)

const (
	// DoneNotificationPerFile plays one done sound per applied main media file.
	DoneNotificationPerFile = "per_file"
	// DoneNotificationPerJob plays at most one done sound per processed job/path.
	DoneNotificationPerJob = "per_job"
	// DoneNotificationOff disables done sounds.
	DoneNotificationOff = "off"
)

// NormalizeDoneNotificationMode validates and normalizes done notification mode.
// Empty input defaults to DoneNotificationPerFile.
func NormalizeDoneNotificationMode(raw string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		return DoneNotificationPerFile, nil
	}
	switch mode {
	case DoneNotificationPerFile, DoneNotificationPerJob, DoneNotificationOff:
		return mode, nil
	default:
		return "", fmt.Errorf(
			"invalid done_notification_mode %q (allowed: %q, %q, %q)",
			raw,
			DoneNotificationPerFile,
			DoneNotificationPerJob,
			DoneNotificationOff,
		)
	}
}

// DoneSoundCount returns how many done sounds to play for applied main media files.
func DoneSoundCount(mode string, appliedMainCount int) int {
	if appliedMainCount <= 0 {
		return 0
	}

	planner := NewDoneSoundPlanner(mode)
	total := 0
	for i := 0; i < appliedMainCount; i++ {
		total += planner.OnAppliedMain()
	}
	total += planner.OnJobComplete()
	return total
}
