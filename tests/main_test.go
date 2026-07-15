package tests

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"v6pfxnatd/app"
)

func TestRunAcceptsConfigFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := app.RunCLI(context.Background(), []string{"-c", "/tmp/config.toml"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "decode config") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := app.RunCLI(context.Background(), []string{"--version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr=%q", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != app.Version {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunRejectsUnexpectedArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := app.RunCLI(context.Background(), []string{"extra"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unexpected argument") || !strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
