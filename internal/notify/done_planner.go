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

// DoneSoundPlanner is the single source of truth for done-sound timing policy.
type DoneSoundPlanner struct {
	mode             string
	appliedMainCount int
}

// NewDoneSoundPlanner normalizes mode and falls back to per-file when invalid.
func NewDoneSoundPlanner(mode string) DoneSoundPlanner {
	normalized, err := NormalizeDoneNotificationMode(mode)
	if err != nil {
		normalized = DoneNotificationPerFile
	}
	return DoneSoundPlanner{mode: normalized}
}

// OnAppliedMain records one applied main result and returns sounds to play immediately.
func (p *DoneSoundPlanner) OnAppliedMain() int {
	p.appliedMainCount++
	if p.mode == DoneNotificationPerFile {
		return 1
	}
	return 0
}

// OnJobComplete returns deferred sounds to play once per processed job/path.
func (p *DoneSoundPlanner) OnJobComplete() int {
	if p.mode == DoneNotificationPerJob && p.appliedMainCount > 0 {
		return 1
	}
	return 0
}

// HasAppliedMain reports whether any main file was applied in the job.
func (p *DoneSoundPlanner) HasAppliedMain() bool {
	return p.appliedMainCount > 0
}
