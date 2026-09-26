//go:build !windows

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// LockProfile takes an exclusive lock shared by every jira process using
// the profile, so parallel agents never refresh the OAuth token at once
// (Atlassian refresh tokens rotate: the loser of a race is logged out).
// It waits up to timeout and returns the function that releases it.
func LockProfile(name string, timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(ProfileDir(), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(ProfileDir(), name+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK || time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("profile %q is locked by another jira process: %w", name, err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
