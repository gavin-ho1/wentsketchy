package runner

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

func CreatePidFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		pidBytes, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("pidfile: could not read pid file: %w", err)
		}

		pid, err := strconv.Atoi(string(pidBytes))
		if err != nil {
			return fmt.Errorf("pidfile: could not parse pid: %w", err)
		}

		process, err := os.FindProcess(pid)
		if err != nil {
			// On Unix systems, FindProcess always succeeds and returns a Process
			// for the given PID, regardless of whether the process exists.
			// So, this error check is mostly for non-Unix systems.
		} else {
			// Sending signal 0 to a process checks if it exists without killing it.
			if err := process.Signal(syscall.Signal(0)); err == nil {
				return fmt.Errorf("pidfile: process with pid %d already exists", pid)
			}
		}
	}

	pid := os.Getpid()
	if err := os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("pidfile: could not write pid file: %w", err)
	}

	return nil
}

func RemovePidFile(path string) error {
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("pidfile: could not remove pid file: %w", err)
	}
	return nil
}
