package notify

import (
	"context"
	"errors"
	"testing"
)

func TestStartCaffeinate_Success(t *testing.T) {
	fake := &fakeCaffeinateController{}
	stop := StartCaffeinate(func() CaffeinateController { return fake }, CaffeinateHooks{})
	stop()

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
		t.Fatalf("start context should be canceled after stop")
	}
}

func TestStartCaffeinate_StartErrorCallsOnStartWarn(t *testing.T) {
	wantErr := errors.New("start failed")
	fake := &fakeCaffeinateController{startErr: wantErr}

	var gotErr error
	stop := StartCaffeinate(func() CaffeinateController { return fake }, CaffeinateHooks{
		OnStartWarn: func(err error) { gotErr = err },
	})
	stop()

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("OnStartWarn err = %v, want %v", gotErr, wantErr)
	}
	if fake.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", fake.startCalls)
	}
	if fake.stopCalls != 1 {
		t.Fatalf("Stop calls = %d, want 1", fake.stopCalls)
	}
}

func TestStartCaffeinate_UnsupportedCallsOnUnsupportedNotOnStartWarn(t *testing.T) {
	fake := &fakeCaffeinateController{startErr: ErrInhibitUnsupported}

	unsupportedCalled := false
	startWarnCalled := false
	stop := StartCaffeinate(func() CaffeinateController { return fake }, CaffeinateHooks{
		OnUnsupported: func() { unsupportedCalled = true },
		OnStartWarn:   func(error) { startWarnCalled = true },
	})
	stop()

	if !unsupportedCalled {
		t.Fatalf("OnUnsupported called = false, want true")
	}
	if startWarnCalled {
		t.Fatalf("OnStartWarn called = true, want false")
	}
}

func TestStartCaffeinate_StopErrorCallsOnStopWarn(t *testing.T) {
	wantErr := errors.New("stop failed")
	fake := &fakeCaffeinateController{stopErr: wantErr}

	var gotErr error
	stop := StartCaffeinate(func() CaffeinateController { return fake }, CaffeinateHooks{
		OnStopWarn: func(err error) { gotErr = err },
	})
	stop()

	if !errors.Is(gotErr, wantErr) {
		t.Fatalf("OnStopWarn err = %v, want %v", gotErr, wantErr)
	}
}

func TestStartCaffeinate_NilHooksDoNotPanic(t *testing.T) {
	fake := &fakeCaffeinateController{
		startErr: errors.New("start failed"),
		stopErr:  errors.New("stop failed"),
	}
	stop := StartCaffeinate(func() CaffeinateController { return fake }, CaffeinateHooks{})
	stop()
}

type fakeCaffeinateController struct {
	startCtx   context.Context
	startErr   error
	stopErr    error
	startCalls int
	stopCalls  int
}

func (f *fakeCaffeinateController) Start(ctx context.Context) error {
	f.startCtx = ctx
	f.startCalls++
	return f.startErr
}

func (f *fakeCaffeinateController) Stop() error {
	f.stopCalls++
	return f.stopErr
}
