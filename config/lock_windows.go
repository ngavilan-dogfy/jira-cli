//go:build windows

package config

import "time"

// LockProfile is a no-op on Windows (no flock); refreshes may race there.
func LockProfile(name string, timeout time.Duration) (func(), error) {
	return func() {}, nil
}
