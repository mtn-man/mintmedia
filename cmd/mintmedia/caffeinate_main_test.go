package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mtn-man/mintmedia/internal/notify"
)

func TestWithCaffeinate_Success(t *testing.T) {
	fake := &fakeMainCaffeinate{}
	oldNewMainCaffeinate := newMainCaffeinate
	newMainCaffeinate = func() notify.CaffeinateController { return fake }
	defer func() { newMainCaffeinate = oldNewMainCaffeinate }()

	called := false
	err := withCaffeinate(func() error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("withCaffeinate() error = %v, want nil", err)
	}
	if !called {
		t.Fatalf("callback called = false, want true")
	}
	if fake.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want 1", fake.stopCalls)
	}
	if fake.startCtx == nil {
		t.Fatalf("start context is nil")
	}
	select {
	case <-fake.startCtx.Done():
	default:
		t.Fatalf("start context should be canceled after return")
	}
}

func TestWithCaffeinate_CallbackErrorStillStops(t *testing.T) {
	fake := &fakeMainCaffeinate{}
	oldNewMainCaffeinate := newMainCaffeinate
	newMainCaffeinate = func() notify.CaffeinateController { return fake }
	defer func() { newMainCaffeinate = oldNewMainCaffeinate }()

	wantErr := errors.New("apply failed")
	err := withCaffeinate(func() error { return wantErr })
	if !errors.Is(err, wantErr) {
		t.Fatalf("withCaffeinate() error = %v, want %v", err, wantErr)
	}
	if fake.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want 1", fake.stopCalls)
	}
}

func TestWithCaffeinate_StartErrorStillRunsCallback(t *testing.T) {
	fake := &fakeMainCaffeinate{
		startErr: errors.New("start failed"),
	}
	oldNewMainCaffeinate := newMainCaffeinate
	newMainCaffeinate = func() notify.CaffeinateController { return fake }
	defer func() { newMainCaffeinate = oldNewMainCaffeinate }()

	called := false
	stderr := captureStderr(t, func() {
		err := withCaffeinate(func() error {
			called = true
			return nil
		})
		if err != nil {
			t.Fatalf("withCaffeinate() error = %v, want nil", err)
		}
	})

	if !called {
		t.Fatalf("callback called = false, want true")
	}
	if fake.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want 1", fake.stopCalls)
	}
	if !strings.Contains(stderr, "WARNING  caffeinate:") {
		t.Fatalf("stderr missing start warning, got %q", stderr)
	}
}

type fakeMainCaffeinate struct {
	startCtx   context.Context
	startErr   error
	stopErr    error
	startCalls int
	stopCalls  int
}

func (f *fakeMainCaffeinate) Start(ctx context.Context) error {
	f.startCtx = ctx
	f.startCalls++
	return f.startErr
}

func (f *fakeMainCaffeinate) Stop() error {
	f.stopCalls++
	return f.stopErr
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()

	_ = w.Close()
	os.Stderr = old
	out := <-done
	_ = r.Close()
	return out
}
