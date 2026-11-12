//go:build windows

package sender

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps a console window from flashing up for the child.
func hideWindow(cmd *exec.Cmd) {
	const createNoWindow = 0x08000000
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
