//go:build windows

package main

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/sys/windows/svc"
)

// runAsWindowsService reports whether this process was started by the
// Windows Service Control Manager and, if so, runs the server under SCM
// control until it asks the service to stop. It returns false immediately
// for an ordinary interactive run (double-clicked, or from a console),
// which then falls through to the normal signal-driven path in main.
func runAsWindowsService(run func(ctx context.Context)) bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}
	if err := svc.Run("Ferrum", &windowsService{run: run}); err != nil {
		slog.Error("windows service failed", "error", err)
	}
	return true
}

type windowsService struct {
	run func(ctx context.Context)
}

func (s *windowsService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepts = svc.AcceptStop | svc.AcceptShutdown

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		s.run(ctx)
		close(done)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepts}
	for {
		select {
		case req := <-r:
			switch req.Cmd {
			case svc.Interrogate:
				changes <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second): // matches the httpServer.Shutdown budget in runServer
				}
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case <-done:
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}
