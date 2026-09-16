//go:build windows

package invocation

import (
	"os/exec"
	"syscall"
)

func configureInspectionCommand(command *exec.Cmd) {
	// The Windows detector uses in-process Win32 calls, not child commands.
	// CommandContext's Process.Kill terminates even a blocked native check.
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
