//go:build !windows

package invocation

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

func TestInspectionCancellationTerminatesSignatureDescendants(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "descendant")
	pidFile := filepath.Join(t.TempDir(), "signature-child.pid")
	t.Setenv("QFEI_INSPECTION_CHILD_PID", pidFile)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0])
	done := make(chan Result, 1)
	go func() { done <- inspectCommand(ctx, command) }()
	var childPID int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			childPID, _ = strconv.Atoi(string(data))
			if childPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID <= 0 {
		cancel()
		<-done
		t.Fatal("signature child did not start")
	}
	// Last-resort cleanup only for our known test child if this regression fails.
	terminated := false
	defer func() {
		if !terminated {
			_ = syscall.Kill(childPID, syscall.SIGKILL)
		}
	}()
	cancel()
	select {
	case report := <-done:
		assertUnavailableInspection(t, report)
	case <-time.After(2 * time.Second):
		t.Fatal("helper cancellation did not return")
	}
	if command.ProcessState == nil {
		t.Fatal("helper not reaped")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(childPID, 0), syscall.ESRCH) {
			terminated = true
			return
		}
		// Linux containers may retain killed orphans as zombies until PID 1 reaps
		// them. They are terminated and cannot continue signature work.
		if child, err := process.NewProcess(int32(childPID)); err == nil {
			if statuses, err := child.Status(); err == nil {
				for _, status := range statuses {
					if status == "zombie" || status == "Z" {
						terminated = true
						return
					}
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("signature subprocess survived helper cancellation")
}
