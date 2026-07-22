package notify

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDoneSoundPlayer_PlayCount_PlaysUpToCountTimes(t *testing.T) {
	calls := make(chan struct{}, 10)
	player := DoneSoundPlayer{
		Debounce: &Debouncer{},
		Cooldown: 0,
		Play: func(context.Context, string) error {
			calls <- struct{}{}
			return nil
		},
	}
	player.PlayCount(context.Background(), "/tmp/done.aiff", 3)

	waitForDoneSoundCalls(t, calls, 3, 2*time.Second)
	assertNoExtraDoneSoundCalls(t, calls, 150*time.Millisecond)
}

func TestDoneSoundPlayer_PlayCount_DebouncesRapidTriggers(t *testing.T) {
	calls := make(chan struct{}, 10)
	player := DoneSoundPlayer{
		Debounce: &Debouncer{},
		Cooldown: 200 * time.Millisecond,
		Play: func(context.Context, string) error {
			calls <- struct{}{}
			return nil
		},
	}
	player.PlayCount(context.Background(), "/tmp/done.aiff", 5)

	waitForDoneSoundCalls(t, calls, 1, 2*time.Second)
	assertNoExtraDoneSoundCalls(t, calls, 150*time.Millisecond)

	time.Sleep(250 * time.Millisecond)
	player.PlayCount(context.Background(), "/tmp/done.aiff", 1)
	waitForDoneSoundCalls(t, calls, 1, 2*time.Second)
}

func TestDoneSoundPlayer_PlayCount_TrimsSoundPath(t *testing.T) {
	paths := make(chan string, 1)
	player := DoneSoundPlayer{
		Debounce: &Debouncer{},
		Cooldown: 0,
		Play: func(_ context.Context, path string) error {
			paths <- path
			return nil
		},
	}
	player.PlayCount(context.Background(), "  /tmp/done.aiff  ", 1)

	select {
	case got := <-paths:
		if got != "/tmp/done.aiff" {
			t.Fatalf("play path = %q, want trimmed %q", got, "/tmp/done.aiff")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for play call")
	}
}

func TestDoneSoundPlayer_PlayCount_SkipsBlankSoundPath(t *testing.T) {
	calls := make(chan struct{}, 10)
	debounce := &Debouncer{}
	player := DoneSoundPlayer{
		Debounce: debounce,
		Cooldown: time.Hour,
		Play: func(context.Context, string) error {
			calls <- struct{}{}
			return nil
		},
	}
	player.PlayCount(context.Background(), "   ", 3)
	assertNoExtraDoneSoundCalls(t, calls, 150*time.Millisecond)

	// Blank-path calls must not consume debounce state -- a subsequent real
	// call is still allowed immediately.
	player.PlayCount(context.Background(), "/tmp/done.aiff", 1)
	waitForDoneSoundCalls(t, calls, 1, 2*time.Second)
}

func TestDoneSoundPlayer_PlayCount_JoinsViaWait(t *testing.T) {
	var wg sync.WaitGroup
	played := false
	player := DoneSoundPlayer{
		Debounce: &Debouncer{},
		Cooldown: 0,
		Wait:     &wg,
		Play: func(context.Context, string) error {
			time.Sleep(50 * time.Millisecond)
			played = true
			return nil
		},
	}
	player.PlayCount(context.Background(), "/tmp/done.aiff", 1)
	wg.Wait()

	if !played {
		t.Fatalf("play should have completed before Wait() returned")
	}
}

func TestDoneSoundPlayer_PlayCount_NilWaitDoesNotBlock(t *testing.T) {
	release := make(chan struct{})
	player := DoneSoundPlayer{
		Debounce: &Debouncer{},
		Cooldown: 0,
		Play: func(context.Context, string) error {
			<-release
			return nil
		},
	}
	done := make(chan struct{})
	go func() {
		player.PlayCount(context.Background(), "/tmp/done.aiff", 1)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("PlayCount blocked despite nil Wait")
	}
	close(release)
}

func waitForDoneSoundCalls(t *testing.T, ch <-chan struct{}, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	got := 0
	for got < want {
		select {
		case <-ch:
			got++
		case <-deadline:
			t.Fatalf("timeout waiting for sound calls: got=%d want=%d", got, want)
		}
	}
}

func assertNoExtraDoneSoundCalls(t *testing.T, ch <-chan struct{}, wait time.Duration) {
	t.Helper()
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ch:
		t.Fatalf("unexpected extra sound call")
	case <-timer.C:
	}
}
