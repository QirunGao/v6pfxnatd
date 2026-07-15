//go:build !linux

package main

import (
	"context"
	"errors"
)

func WatchIPv6Routes(context.Context, NormalizedConfig, chan<- struct{}, chan<- struct{}) error {
	return errors.New("v6pfxnatd requires Linux rtnetlink and nftables")
}

func Reconcile(context.Context, NormalizedConfig) error {
	return errors.New("v6pfxnatd requires Linux rtnetlink and nftables")
}
