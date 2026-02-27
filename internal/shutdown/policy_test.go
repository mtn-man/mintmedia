package shutdown

import (
	"testing"
	"time"
)

func TestResolvePolicy(t *testing.T) {
	t.Run("Defaults", func(t *testing.T) {
		got := ResolvePolicy(0, 0)
		if got.Grace != 10*time.Minute {
			t.Fatalf("Grace = %s, want %s", got.Grace, 10*time.Minute)
		}
		if got.Force != 15*time.Second {
			t.Fatalf("Force = %s, want %s", got.Force, 15*time.Second)
		}
	})

	t.Run("PassThrough", func(t *testing.T) {
		got := ResolvePolicy(2*time.Minute, 9*time.Second)
		if got.Grace != 2*time.Minute {
			t.Fatalf("Grace = %s, want %s", got.Grace, 2*time.Minute)
		}
		if got.Force != 9*time.Second {
			t.Fatalf("Force = %s, want %s", got.Force, 9*time.Second)
		}
	})
}

func TestDrain_NoInFlight_ImmediateSuccess(t *testing.T) {
	waitCalls := 0
	wait := func(timeout time.Duration) bool {
		waitCalls++
		if timeout != 10*time.Second {
			t.Fatalf("timeout = %s, want %s", timeout, 10*time.Second)
		}
		return true
	}

	waitHookCalled := false
	graceHookCalled := false

	res := Drain(
		Policy{Grace: 10 * time.Second, Force: 5 * time.Second},
		false,
		wait,
		nil,
		Hooks{
			OnWaitStart: func(time.Duration) { waitHookCalled = true },
			OnGraceElapsed: func(time.Duration) {
				graceHookCalled = true
			},
		},
	)

	if waitCalls != 1 {
		t.Fatalf("wait called %d times, want 1", waitCalls)
	}
	if waitHookCalled {
		t.Fatalf("OnWaitStart called, want not called")
	}
	if graceHookCalled {
		t.Fatalf("OnGraceElapsed called, want not called")
	}
	if res.GraceElapsed {
		t.Fatalf("GraceElapsed = true, want false")
	}
	if res.TimedOut {
		t.Fatalf("TimedOut = true, want false")
	}
}

func TestDrain_InFlight_GracefulSuccess(t *testing.T) {
	waitCalls := 0
	wait := func(timeout time.Duration) bool {
		waitCalls++
		if timeout != 12*time.Second {
			t.Fatalf("timeout = %s, want %s", timeout, 12*time.Second)
		}
		return true
	}

	waitHookCalls := 0
	graceHookCalls := 0

	res := Drain(
		Policy{Grace: 12 * time.Second, Force: 7 * time.Second},
		true,
		wait,
		nil,
		Hooks{
			OnWaitStart: func(d time.Duration) {
				waitHookCalls++
				if d != 12*time.Second {
					t.Fatalf("wait hook grace = %s, want %s", d, 12*time.Second)
				}
			},
			OnGraceElapsed: func(time.Duration) { graceHookCalls++ },
		},
	)

	if waitCalls != 1 {
		t.Fatalf("wait called %d times, want 1", waitCalls)
	}
	if waitHookCalls != 1 {
		t.Fatalf("OnWaitStart calls = %d, want 1", waitHookCalls)
	}
	if graceHookCalls != 0 {
		t.Fatalf("OnGraceElapsed calls = %d, want 0", graceHookCalls)
	}
	if res.GraceElapsed {
		t.Fatalf("GraceElapsed = true, want false")
	}
	if res.TimedOut {
		t.Fatalf("TimedOut = true, want false")
	}
}

func TestDrain_GraceElapsed_ForcedSuccess(t *testing.T) {
	waitCalls := 0
	wait := func(timeout time.Duration) bool {
		waitCalls++
		switch waitCalls {
		case 1:
			if timeout != 9*time.Second {
				t.Fatalf("first timeout = %s, want %s", timeout, 9*time.Second)
			}
			return false
		case 2:
			if timeout != 4*time.Second {
				t.Fatalf("second timeout = %s, want %s", timeout, 4*time.Second)
			}
			return true
		default:
			t.Fatalf("unexpected wait call %d", waitCalls)
			return false
		}
	}

	forceCancelCalls := 0
	waitHookCalls := 0
	graceHookCalls := 0

	res := Drain(
		Policy{Grace: 9 * time.Second, Force: 4 * time.Second},
		true,
		wait,
		func() { forceCancelCalls++ },
		Hooks{
			OnWaitStart:    func(time.Duration) { waitHookCalls++ },
			OnGraceElapsed: func(time.Duration) { graceHookCalls++ },
		},
	)

	if waitCalls != 2 {
		t.Fatalf("wait called %d times, want 2", waitCalls)
	}
	if forceCancelCalls != 1 {
		t.Fatalf("forceCancel calls = %d, want 1", forceCancelCalls)
	}
	if waitHookCalls != 1 {
		t.Fatalf("OnWaitStart calls = %d, want 1", waitHookCalls)
	}
	if graceHookCalls != 1 {
		t.Fatalf("OnGraceElapsed calls = %d, want 1", graceHookCalls)
	}
	if !res.GraceElapsed {
		t.Fatalf("GraceElapsed = false, want true")
	}
	if res.TimedOut {
		t.Fatalf("TimedOut = true, want false")
	}
}

func TestDrain_GraceElapsed_ForcedTimeout(t *testing.T) {
	waitCalls := 0
	wait := func(timeout time.Duration) bool {
		waitCalls++
		switch waitCalls {
		case 1:
			if timeout != 8*time.Second {
				t.Fatalf("first timeout = %s, want %s", timeout, 8*time.Second)
			}
			return false
		case 2:
			if timeout != 3*time.Second {
				t.Fatalf("second timeout = %s, want %s", timeout, 3*time.Second)
			}
			return false
		default:
			t.Fatalf("unexpected wait call %d", waitCalls)
			return false
		}
	}

	forceCancelCalls := 0
	graceHookCalls := 0

	res := Drain(
		Policy{Grace: 8 * time.Second, Force: 3 * time.Second},
		true,
		wait,
		func() { forceCancelCalls++ },
		Hooks{
			OnGraceElapsed: func(time.Duration) { graceHookCalls++ },
		},
	)

	if waitCalls != 2 {
		t.Fatalf("wait called %d times, want 2", waitCalls)
	}
	if forceCancelCalls != 1 {
		t.Fatalf("forceCancel calls = %d, want 1", forceCancelCalls)
	}
	if graceHookCalls != 1 {
		t.Fatalf("OnGraceElapsed calls = %d, want 1", graceHookCalls)
	}
	if !res.GraceElapsed {
		t.Fatalf("GraceElapsed = false, want true")
	}
	if !res.TimedOut {
		t.Fatalf("TimedOut = false, want true")
	}
}

func TestFormatDurationCompact(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "TenMinutes", in: 10 * time.Minute, want: "10m"},
		{name: "OneHour", in: 1 * time.Hour, want: "1h"},
		{name: "HourAndMinutes", in: 1*time.Hour + 30*time.Minute, want: "1h30m"},
		{name: "FortyFiveSeconds", in: 45 * time.Second, want: "45s"},
		{name: "SubSecondFallback", in: 500 * time.Millisecond, want: "500ms"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatDurationCompact(tc.in)
			if got != tc.want {
				t.Fatalf("FormatDurationCompact(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
