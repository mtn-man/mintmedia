package notify

import (
	"context"
	"strings"
	"sync"
	"time"
)

// DoneSoundPlayer plays "done" notification sounds asynchronously, gated by
// a caller-owned Debouncer so a fast batch of near-simultaneous triggers
// coalesces into distinct, non-overlapping plays instead of a cacophony.
type DoneSoundPlayer struct {
	// Debounce is required; state shared across every PlayCount call that
	// should coalesce against the same cooldown window.
	Debounce *Debouncer
	// Cooldown is the minimum spacing between allowed plays.
	Cooldown time.Duration
	// Play defaults to PlaySound when nil.
	Play func(context.Context, string) error
	// Wait, if non-nil, has Add/Done called around every spawned play
	// goroutine so the caller can block on it before the process exits.
	// Leave nil for fire-and-forget.
	Wait *sync.WaitGroup
}

// PlayCount plays soundPath up to count times, each attempt gated
// independently by Debounce.Allow(time.Now(), Cooldown); every allowed play
// runs in its own goroutine against a context.WithoutCancel(ctx) copy of
// ctx, so playback survives cancellation of the caller's context.
func (p DoneSoundPlayer) PlayCount(ctx context.Context, soundPath string, count int) {
	soundPath = strings.TrimSpace(soundPath)
	if soundPath == "" {
		return
	}
	play := p.Play
	if play == nil {
		play = PlaySound
	}
	base := context.WithoutCancel(ctx)
	for range count {
		if !p.Debounce.Allow(time.Now(), p.Cooldown) {
			continue
		}
		if p.Wait != nil {
			p.Wait.Add(1)
		}
		go func() {
			if p.Wait != nil {
				defer p.Wait.Done()
			}
			_ = play(base, soundPath)
		}()
	}
}
