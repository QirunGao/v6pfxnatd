package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestParseCLI(t *testing.T) {
	var output bytes.Buffer
	opts, err := parseCLI([]string{"-c", "/tmp/config.toml"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	if opts.configPath != "/tmp/config.toml" || opts.version {
		t.Fatalf("opts = %+v", opts)
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnexpectedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
