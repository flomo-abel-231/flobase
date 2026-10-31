//go:build windows

package local

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps docker commands from flashing a console window when the
// desktop app runs them.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}
