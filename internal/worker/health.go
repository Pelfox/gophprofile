package worker

import (
	"errors"
	"fmt"
	"os"
	"time"
)

const (
	workerHealthFilePath    = "/tmp/gophprofile-worker.heartbeat"
	workerHeartbeatInterval = 5 * time.Second
	workerHealthMaxAge      = time.Minute
)

// CheckHealth reports whether the worker consumer loop has updated its
// heartbeat recently enough to be considered healthy.
func CheckHealth() error {
	return checkWorkerHealth(
		workerHealthFilePath,
		workerHealthMaxAge,
		time.Now(),
	)
}

func writeWorkerHeartbeat(path string, now time.Time) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open worker heartbeat file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close worker heartbeat file: %w", err)
	}

	if err := os.Chtimes(path, now, now); err != nil {
		return fmt.Errorf("failed to update worker heartbeat file: %w", err)
	}

	return nil
}

func removeWorkerHeartbeat(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to remove worker heartbeat file: %w", err)
	}

	return nil
}

func checkWorkerHealth(
	path string,
	maxAge time.Duration,
	now time.Time,
) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("worker heartbeat is unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("worker heartbeat path is not a regular file")
	}

	age := now.Sub(info.ModTime())
	if age > maxAge {
		return fmt.Errorf(
			"worker heartbeat is stale: age %s exceeds %s",
			age.Round(time.Second),
			maxAge,
		)
	}

	return nil
}
