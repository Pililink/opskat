//go:build windows

package command

import "golang.org/x/crypto/ssh"

// watchTerminalResize is a no-op on Windows (SIGWINCH not available).
func watchTerminalResize(session *ssh.Session, fd int) func() {
	return func() {}
}
