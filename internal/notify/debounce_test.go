package notify

import (
	"testing"
	"time"
)

func TestDebouncer_FirstCallAlwaysAllowed(t *testing.T) {
	var d Debouncer
	if !d.Allow(time.Now(), 3*time.Second) {
		t.Fatalf("Allow() = false, want true for first call")
	}
}

func TestDebouncer_SuppressesWithinCooldown(t *testing.T) {
	var d Debouncer
	base := time.Now()
	if !d.Allow(base, 3*time.Second) {
		t.Fatalf("Allow() = false, want true for first call")
	}
	if d.Allow(base.Add(1*time.Second), 3*time.Second) {
		t.Fatalf("Allow() = true, want false within cooldown")
	}
	if d.Allow(base.Add(2900*time.Millisecond), 3*time.Second) {
		t.Fatalf("Allow() = true, want false just under cooldown")
	}
}

func TestDebouncer_AllowsAfterCooldownElapses(t *testing.T) {
	var d Debouncer
	base := time.Now()
	if !d.Allow(base, 3*time.Second) {
		t.Fatalf("Allow() = false, want true for first call")
	}
	if !d.Allow(base.Add(3*time.Second), 3*time.Second) {
		t.Fatalf("Allow() = false, want true once cooldown has elapsed")
	}
}

func TestDebouncer_SuppressedCallDoesNotResetWindow(t *testing.T) {
	var d Debouncer
	base := time.Now()
	if !d.Allow(base, 3*time.Second) {
		t.Fatalf("Allow() = false, want true for first call")
	}
	// A suppressed call at +1s should not push the window out to +1s..+4s.
	if d.Allow(base.Add(1*time.Second), 3*time.Second) {
		t.Fatalf("Allow() = true, want false within cooldown")
	}
	if !d.Allow(base.Add(3100*time.Millisecond), 3*time.Second) {
		t.Fatalf("Allow() = false, want true once cooldown has elapsed from the original allowed call")
	}
}
