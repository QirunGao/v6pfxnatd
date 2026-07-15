package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
)

var (
	watchIPv6Routes = WatchIPv6Routes
	reconcileNow    = Reconcile
)

func Trigger(triggers chan<- struct{}) {
	select {
	case triggers <- struct{}{}:
	default:
	}
}

func Run(ctx context.Context, cfg NormalizedConfig) error {
	triggers := make(chan struct{}, 1)
	watcherReady := make(chan struct{})
	watcherDone := make(chan error, 1)

	go func() {
		watcherDone <- watchIPv6Routes(ctx, cfg, watcherReady, triggers)
	}()

	select {
	case <-watcherReady:
		slog.Debug("route watcher ready")
	case err := <-watcherDone:
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return fmt.Errorf("route watcher: %w", err)
		}
		return errors.New("route watcher stopped before readiness")
	case <-ctx.Done():
		return nil
	}
	if ctx.Err() != nil {
		return nil
	}

	Trigger(triggers)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-watcherDone:
			if ctx.Err() != nil {
				return nil
			}
			if err != nil {
				return fmt.Errorf("route watcher: %w", err)
			}
			return errors.New("route watcher stopped")
		case <-triggers:
			if ctx.Err() != nil {
				return nil
			}
			if err := reconcileNow(ctx, cfg); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				slog.Error("reconcile failed", "error", err)
			}
		}
	}
}
