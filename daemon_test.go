package main

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
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
	oldWatch, oldReconcile := watchIPv6Routes, reconcileNow
	t.Cleanup(func() { watchIPv6Routes, reconcileNow = oldWatch, oldReconcile })
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	watchIPv6Routes = func(ctx context.Context, _ NormalizedConfig, ready chan<- struct{}, _ chan<- struct{}) error {
		close(ready)
		<-ctx.Done()
		return nil
	}
	reconcileNow = func(context.Context, NormalizedConfig) error {
		calls.Add(1)
		cancel()
		return nil
	}
	if err := Run(ctx, NormalizedConfig{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("reconcile calls = %d, want 1", calls.Load())
	}
}

func TestRunPropagatesWatcherFailureBeforeReadiness(t *testing.T) {
	oldWatch := watchIPv6Routes
	t.Cleanup(func() { watchIPv6Routes = oldWatch })
	watchIPv6Routes = func(context.Context, NormalizedConfig, chan<- struct{}, chan<- struct{}) error {
		return errors.New("subscription failed")
	}
	err := Run(context.Background(), NormalizedConfig{})
	if err == nil || !strings.Contains(err.Error(), "subscription failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunPropagatesWatcherFailureAfterReadiness(t *testing.T) {
	oldWatch, oldReconcile := watchIPv6Routes, reconcileNow
	t.Cleanup(func() { watchIPv6Routes, reconcileNow = oldWatch, oldReconcile })
	watchIPv6Routes = func(_ context.Context, _ NormalizedConfig, ready chan<- struct{}, _ chan<- struct{}) error {
		close(ready)
		return errors.New("socket failed")
	}
	reconcileNow = func(context.Context, NormalizedConfig) error { return nil }
	err := Run(context.Background(), NormalizedConfig{})
	if err == nil || !strings.Contains(err.Error(), "socket failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestRunDoesNotReconcileCanceledContext(t *testing.T) {
	oldWatch, oldReconcile := watchIPv6Routes, reconcileNow
	t.Cleanup(func() { watchIPv6Routes, reconcileNow = oldWatch, oldReconcile })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	watchIPv6Routes = func(context.Context, NormalizedConfig, chan<- struct{}, chan<- struct{}) error {
		return nil
	}
	var calls atomic.Int32
	reconcileNow = func(context.Context, NormalizedConfig) error {
		calls.Add(1)
		return nil
	}
	if err := Run(ctx, NormalizedConfig{}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatalf("reconcile calls = %d, want 0", calls.Load())
	}
}
