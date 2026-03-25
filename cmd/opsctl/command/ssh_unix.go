//go:build !windows

package command

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/term"
)

// watchTerminalResize starts a goroutine that watches for SIGWINCH signals
// and sends window-change requests to the SSH session.
// Returns a stop function to clean up.
func watchTerminalResize(session *ssh.Session, fd int) func() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sigCh:
				w, h, err := term.GetSize(fd)
				if err == nil {
					if err := session.WindowChange(h, w); err != nil {
						logger.Default().Warn("SSH window change", zap.Error(err))
					}
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(sigCh)
		close(done)
	}
}
