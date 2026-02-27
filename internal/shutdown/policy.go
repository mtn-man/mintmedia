package shutdown

import (
	"strconv"
	"strings"
	"time"
)

const (
	defaultGraceDuration = 10 * time.Minute
	defaultForceTimeout  = 15 * time.Second
)

// Policy defines graceful and forced shutdown windows.
type Policy struct {
	Grace time.Duration
	Force time.Duration
}

// Hooks allows callers to emit policy-specific logs without coupling output text.
type Hooks struct {
	OnWaitStart    func(grace time.Duration)
	OnGraceElapsed func(force time.Duration)
}

// Result reports the shutdown path selected by Drain.
type Result struct {
	GraceElapsed bool
	TimedOut     bool
}

// ResolvePolicy applies default durations when values are not set.
func ResolvePolicy(grace, force time.Duration) Policy {
	if grace <= 0 {
		grace = defaultGraceDuration
	}
	if force <= 0 {
		force = defaultForceTimeout
	}
	return Policy{
		Grace: grace,
		Force: force,
	}
}

// Drain executes a graceful-then-forced shutdown wait policy.
func Drain(policy Policy, hasInFlight bool, wait func(timeout time.Duration) bool, forceCancel func(), hooks Hooks) Result {
	policy = ResolvePolicy(policy.Grace, policy.Force)

	if hasInFlight && hooks.OnWaitStart != nil {
		hooks.OnWaitStart(policy.Grace)
	}
	if wait(policy.Grace) {
		return Result{}
	}

	result := Result{GraceElapsed: true}
	if hooks.OnGraceElapsed != nil {
		hooks.OnGraceElapsed(policy.Force)
	}
	if forceCancel != nil {
		forceCancel()
	}
	if wait(policy.Force) {
		return result
	}

	result.TimedOut = true
	return result
}

// FormatDurationCompact renders human-readable h/m/s durations without trailing zero units.
func FormatDurationCompact(d time.Duration) string {
	if d < 0 {
		return "-" + FormatDurationCompact(-d)
	}
	if d < time.Second {
		return d.String()
	}

	d = d.Round(time.Second)
	hours := d / time.Hour
	d -= hours * time.Hour
	minutes := d / time.Minute
	d -= minutes * time.Minute
	seconds := d / time.Second

	var b strings.Builder
	if hours > 0 {
		b.WriteString(strconv.FormatInt(int64(hours), 10))
		b.WriteByte('h')
	}
	if minutes > 0 {
		b.WriteString(strconv.FormatInt(int64(minutes), 10))
		b.WriteByte('m')
	}
	if seconds > 0 || (hours == 0 && minutes == 0) {
		b.WriteString(strconv.FormatInt(int64(seconds), 10))
		b.WriteByte('s')
	}
	return b.String()
}
