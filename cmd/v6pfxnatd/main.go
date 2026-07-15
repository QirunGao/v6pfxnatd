package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"v6pfxnatd/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(app.RunCLI(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
