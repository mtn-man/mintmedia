package notify

import "testing"

func TestDoneSoundPlanner_PerFile(t *testing.T) {
	p := NewDoneSoundPlanner(DoneNotificationPerFile)

	if got := p.OnAppliedMain(); got != 1 {
		t.Fatalf("OnAppliedMain() = %d, want 1", got)
	}
	if got := p.OnAppliedMain(); got != 1 {
		t.Fatalf("OnAppliedMain() = %d, want 1", got)
	}
	if got := p.OnJobComplete(); got != 0 {
		t.Fatalf("OnJobComplete() = %d, want 0", got)
	}
	if !p.HasAppliedMain() {
		t.Fatalf("HasAppliedMain() = false, want true")
	}
}

func TestDoneSoundPlanner_PerJob(t *testing.T) {
	p := NewDoneSoundPlanner(DoneNotificationPerJob)

	if got := p.OnAppliedMain(); got != 0 {
		t.Fatalf("OnAppliedMain() = %d, want 0", got)
	}
	if got := p.OnAppliedMain(); got != 0 {
		t.Fatalf("OnAppliedMain() = %d, want 0", got)
	}
	if got := p.OnJobComplete(); got != 1 {
		t.Fatalf("OnJobComplete() = %d, want 1", got)
	}
}

func TestDoneSoundPlanner_Off(t *testing.T) {
	p := NewDoneSoundPlanner(DoneNotificationOff)

	if got := p.OnAppliedMain(); got != 0 {
		t.Fatalf("OnAppliedMain() = %d, want 0", got)
	}
	if got := p.OnJobComplete(); got != 0 {
		t.Fatalf("OnJobComplete() = %d, want 0", got)
	}
}

func TestDoneSoundPlanner_InvalidFallsBackToPerFile(t *testing.T) {
	p := NewDoneSoundPlanner("loud")

	if got := p.OnAppliedMain(); got != 1 {
		t.Fatalf("OnAppliedMain() = %d, want 1", got)
	}
	if got := p.OnJobComplete(); got != 0 {
		t.Fatalf("OnJobComplete() = %d, want 0", got)
	}
}

func TestDoneSoundPlanner_HasAppliedMainFalseWithoutApplied(t *testing.T) {
	p := NewDoneSoundPlanner(DoneNotificationPerJob)
	if p.HasAppliedMain() {
		t.Fatalf("HasAppliedMain() = true, want false")
	}
}
