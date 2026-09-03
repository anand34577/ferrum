//go:build !windows

package main

import "context"

// runAsWindowsService is a no-op on every platform but Windows: it always
// returns false, so main falls through to the normal signal-driven run
// (SIGTERM from systemd, Ctrl+C, etc.). See service_windows.go.
func runAsWindowsService(func(ctx context.Context)) bool { return false }
