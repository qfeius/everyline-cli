//go:build !windows

package invocation

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureInspectionCommand(command *exec.Cmd) {
	// codesign/plutil inherit this private group. Cancel the entire group, not
	// just the Go helper, so a stalled signature subprocess is not orphaned.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}
