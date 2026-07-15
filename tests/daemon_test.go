package tests

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	. "v6pfxnatd/app"
)

func TestTriggerCoalesces(t *testing.T) {
	ch := make(chan struct{}, 1)
	Trigger(ch)
	Trigger(ch)
	if len(ch) != 1 {
		t.Fatalf("queued triggers = %d, want 1", len(ch))
	}
}

func TestRunWaitsForWatcherReadinessThenReconciles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	deps := RuntimeDependencies{}
	deps.WatchIPv6Routes = func(ctx context.Context, _ NormalizedConfig, ready chan<- struct{}, _ chan<- struct{}) error {
		close(ready)
		<-ctx.Done()
		return nil
	}
	deps.Reconcile = func(context.Context, NormalizedConfig) error {
		calls.Add(1)
		cancel()
		return nil
	}
	if err := RunWithDependencies(ctx, NormalizedConfig{}, deps); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("reconcile calls = %d, want 1", calls.Load())
	}
}

func TestRunPropagatesWatcherFailureBeforeReadiness(t *testing.T) {
	deps := RuntimeDependencies{
		WatchIPv6Routes: func(context.Context, NormalizedConfig, chan<- struct{}, chan<- struct{}) error {
			return errors.New("subscription failed")
		},
		Reconcile: func(context.Context, NormalizedConfig) error { return nil },
	}
	err := RunWithDependencies(context.Background(), NormalizedConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "subscription failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunPropagatesWatcherFailureAfterReadiness(t *testing.T) {
	deps := RuntimeDependencies{}
	deps.WatchIPv6Routes = func(_ context.Context, _ NormalizedConfig, ready chan<- struct{}, _ chan<- struct{}) error {
		close(ready)
		return errors.New("socket failed")
	}
	deps.Reconcile = func(context.Context, NormalizedConfig) error { return nil }
	err := RunWithDependencies(context.Background(), NormalizedConfig{}, deps)
	if err == nil || !strings.Contains(err.Error(), "socket failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunDoesNotReconcileCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deps := RuntimeDependencies{}
	deps.WatchIPv6Routes = func(context.Context, NormalizedConfig, chan<- struct{}, chan<- struct{}) error {
		return nil
	}
	var calls atomic.Int32
	deps.Reconcile = func(context.Context, NormalizedConfig) error {
		calls.Add(1)
		return nil
	}
	if err := RunWithDependencies(ctx, NormalizedConfig{}, deps); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("reconcile calls = %d, want 0", calls.Load())
	}
}
