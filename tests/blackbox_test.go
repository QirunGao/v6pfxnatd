package tests

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"v6pfxnatd/app"
)

var blackBoxBinary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "v6pfxnatd-blackbox-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	name := "v6pfxnatd"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	blackBoxBinary = filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-buildvcs=false", "-o", blackBoxBinary, "../cmd/v6pfxnatd")
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(dir, "gocache"))
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "black-box build failed: %v\n%s", err, output)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	if err := os.RemoveAll(dir); err != nil && code == 0 {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}
	os.Exit(code)
}

func TestBlackBoxVersion(t *testing.T) {
	cmd := exec.Command(blackBoxBinary, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command failed: %v: %s", err, output)
	}
	if strings.TrimSpace(string(output)) != app.Version {
		t.Fatalf("output = %q", output)
	}
}

func TestBlackBoxReportsConfigReadError(t *testing.T) {
	cmd := exec.Command(blackBoxBinary, "-c", "config.toml")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("command succeeded")
	}
	if !strings.Contains(string(output), "decode config:") {
		t.Fatalf("output = %q", output)
	}
}

func TestBlackBoxRejectsUnknownConfigField(t *testing.T) {
	path := writeConfig(t, validConfigText+"\nunknown = true\n")
	cmd := exec.Command(blackBoxBinary, "-c", path)
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("command succeeded")
	}
	if !strings.Contains(string(output), "unknown field") {
		t.Fatalf("output = %q", output)
	}
}
