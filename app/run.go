package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
)

const (
	defaultConfigPath = "/etc/v6pfxnatd/config.toml"
	Version           = "0.2.0"
)

type cliOptions struct {
	configPath string
	version    bool
}

func RunCLI(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	opts, err := parseCLI(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		usage(stderr)
		return 2
	}
	if opts.version {
		fmt.Fprintln(stdout, Version)
		return 0
	}

	cfg, err := LoadConfig(opts.configPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	logger := newLogger(cfg.Logging, stdout)
	slog.SetDefault(logger)
	logger.Info("service started", "version", Version)
	if err := Run(ctx, cfg); err != nil {
		logger.Error("service stopped", "error", err)
		return 1
	}
	logger.Info("service stopped")
	return 0
}

func parseCLI(args []string, output io.Writer) (cliOptions, error) {
	fs := flag.NewFlagSet("v6pfxnatd", flag.ContinueOnError)
	fs.SetOutput(output)
	configPath := fs.String("c", defaultConfigPath, "config file")
	showVersion := fs.Bool("version", false, "print version")
	if err := fs.Parse(args); err != nil {
		return cliOptions{}, err
	}
	if fs.NArg() != 0 {
		return cliOptions{}, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	return cliOptions{configPath: *configPath, version: *showVersion}, nil
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage:")
	fmt.Fprintln(w, "  v6pfxnatd [-c /path/config.toml]")
	fmt.Fprintln(w, "  v6pfxnatd --version")
}

func newLogger(cfg LoggingConfig, output io.Writer) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	if cfg.Format == "json" {
		return slog.New(slog.NewJSONHandler(output, opts))
	}
	return slog.New(slog.NewTextHandler(output, opts))
}
