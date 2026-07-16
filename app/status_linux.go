//go:build linux

package app

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteOperationalStatus(path string, contents []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".status-*")
	if err != nil {
		return fmt.Errorf("create temporary status file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	defer temporary.Close()

	if _, err := temporary.Write(contents); err != nil {
		return fmt.Errorf("write temporary status file: %w", err)
	}
	if err := temporary.Chmod(0444); err != nil {
		return fmt.Errorf("set status file mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary status file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace status file: %w", err)
	}
	return nil
}
