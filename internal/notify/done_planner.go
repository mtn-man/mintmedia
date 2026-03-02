package notify

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
