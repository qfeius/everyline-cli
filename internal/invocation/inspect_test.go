package invocation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The production binary dispatches the same protocol before normal CLI setup.
// Fault modes exist only in the test executable, never in the shipped CLI.
func TestMain(m *testing.M) {
	switch os.Getenv("QFEI_INSPECTION_TEST_MODE") {
	case "partial-stall", "partial-upgrade":
		report := Analyze([]Process{{Depth: 0}, {Depth: 1, Name: "Electron", Executable: "/Applications/WorkBuddy.app/Contents/MacOS/Electron"}}, nil)
		_ = json.NewEncoder(os.Stdout).Encode(report)
		if os.Getenv("QFEI_INSPECTION_TEST_MODE") == "partial-stall" {
			time.Sleep(time.Hour)
		}
		report.Confidence = "high"
		report.EvidenceType = "macos_code_signature"
		_ = json.NewEncoder(os.Stdout).Encode(report)
		os.Exit(0)
	case "stall":
		time.Sleep(time.Hour)
		os.Exit(0)
	case "malformed":
		fmt.Fprint(os.Stdout, "not-json")
		os.Exit(0)
	case "empty":
		fmt.Fprint(os.Stdout, "{}")
		os.Exit(0)
	case "oversized":
		fmt.Fprint(os.Stdout, strings.Repeat("x", maxInspectionOutput+1))
		os.Exit(0)
	case "failure":
		os.Exit(7)
	case "foreign-product":
		report := Analyze(nil, nil)
		report.ProductCode = "contract"
		_ = json.NewEncoder(os.Stdout).Encode(report)
		os.Exit(0)
	case "descendant":
		command := exec.Command(os.Args[0])
		command.Env = append(os.Environ(), "QFEI_INSPECTION_TEST_MODE=stall")
		// Inherit the helper's process group, like codesign and plutil do.
		if err := command.Start(); err != nil {
			os.Exit(8)
		}
		if err := os.WriteFile(os.Getenv("QFEI_INSPECTION_CHILD_PID"), []byte(strconv.Itoa(command.Process.Pid)), 0600); err != nil {
			os.Exit(9)
		}
		_ = command.Wait()
		os.Exit(0)
	}
	if handled, err := RunInspectionHelper(os.Args[1:], os.Stdout); handled {
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestInspectDefaultBudgetIsFiveSecondsAndDoesNotCancelBusinessContext(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "stall")
	if DefaultInspectionTimeout != 5*time.Second {
		t.Fatalf("budget = %s", DefaultInspectionTimeout)
	}
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := time.Now()
	report := Inspect(parent, DefaultMaxDepth)
	elapsed := time.Since(started)
	assertUnavailableInspection(t, report)
	if elapsed < DefaultInspectionTimeout || elapsed > DefaultInspectionTimeout+2*time.Second {
		t.Fatalf("inspection took %s, want five-second budget plus bounded process cleanup", elapsed)
	}
	if parent.Err() != nil {
		t.Fatalf("inspection cancelled business context: %v", parent.Err())
	}
}

func TestInspectHonorsEarlierDeadlineAndReapsHelper(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "stall")
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		command := exec.CommandContext(ctx, os.Args[0], inspectionHelperFlag, "1")
		started := time.Now()
		report := inspectCommand(ctx, command)
		cancel()
		assertUnavailableInspection(t, report)
		if time.Since(started) > 2*time.Second {
			t.Fatal("inspection ignored earlier deadline")
		}
		if command.ProcessState == nil {
			t.Fatal("helper was not waited/reaped")
		}
		// Wait has already observed termination (ProcessState above). On Windows,
		// Wait releases the process handle, so a later Kill returns EINVAL rather
		// than ErrProcessDone. Do not mistake that released handle for a live helper.
		err := command.Process.Kill()
		if !errors.Is(err, os.ErrProcessDone) && !(runtime.GOOS == "windows" && errors.Is(err, syscall.EINVAL)) {
			t.Fatalf("helper still alive: %v", err)
		}
	}
}

func TestInspectCancelledBeforeStartingAndHelperFailuresAreUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	assertUnavailableInspection(t, Inspect(ctx, 1))
	if time.Since(started) > time.Second {
		t.Fatal("cancelled inspection started expensive work")
	}
	for _, mode := range []string{"malformed", "empty", "oversized", "failure", "foreign-product"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("QFEI_INSPECTION_TEST_MODE", mode)
			assertUnavailableInspection(t, Inspect(context.Background(), 1))
		})
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, filepath.Join(t.TempDir(), "missing-helper"))
	assertUnavailableInspection(t, inspectCommand(ctx, command))
}

func TestInspectionHelperStartsAtOriginalCallerAndDoesNotCache(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "")
	for attempt := 0; attempt < 2; attempt++ {
		report := Inspect(context.Background(), 1)
		if len(report.Processes) != 1 {
			t.Fatalf("helper did not return ancestry: %+v", report)
		}
		if report.Processes[0].PID != int32(os.Getpid()) || report.Processes[0].Depth != 0 {
			t.Fatalf("helper altered original caller: %+v", report.Processes)
		}
		if report.AgentSourceType != "unknown" {
			t.Fatalf("depth-one scan should not identify an ancestor: %+v", report)
		}
	}
}

func TestInspectionHelperDispatchRejectsBadArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"version"}, {"everyline", "search"}} {
		if handled, err := RunInspectionHelper(args, io.Discard); handled || err != nil {
			t.Fatalf("hijacked ordinary command: %v", args)
		}
	}
	for _, args := range [][]string{{inspectionHelperFlag}, {inspectionHelperFlag, "0"}, {inspectionHelperFlag, "129"}, {inspectionHelperFlag, "invalid"}, {inspectionHelperFlag, "1", "extra"}} {
		if handled, err := RunInspectionHelper(args, io.Discard); !handled || err == nil {
			t.Fatalf("accepted invalid helper args: %v", args)
		}
	}
	var output bytes.Buffer
	if handled, err := RunInspectionHelper([]string{inspectionHelperFlag, "1"}, &output); !handled || err != nil {
		t.Fatalf("helper dispatch: %v", err)
	}
	var result Result
	if err := json.NewDecoder(bytes.NewReader(output.Bytes())).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Processes) != 1 || result.Processes[0].PID != int32(os.Getppid()) {
		t.Fatalf("wrong helper root: %+v", result)
	}
}

func TestInspectionOutputIsBoundedEvenWhenCopiedFromPipe(t *testing.T) {
	reader, writer := io.Pipe()
	go func() { _, _ = writer.Write(bytes.Repeat([]byte{'x'}, maxInspectionOutput+123)); _ = writer.Close() }()
	defer reader.Close()
	var output inspectionOutput
	if _, err := io.Copy(&output, reader); err != nil {
		t.Fatal(err)
	}
	if !output.overflow || output.buffer.Len() != maxInspectionOutput {
		t.Fatalf("unbounded output: %d", output.buffer.Len())
	}
}

func assertUnavailableInspection(t *testing.T, report Result) {
	t.Helper()
	if report.ChannelType != "cli" || report.AgentSourceType != "unknown" || report.ProductCode != "contract-review" ||
		report.Confidence != "unknown" || report.EvidenceType != "none" || report.DetectorVersion != DetectorVersion ||
		report.RuleID != "" || report.Application != nil || report.MatchedProcess != nil || len(report.Processes) != 0 {
		t.Fatalf("unsafe/incomplete fallback metadata: %+v", report)
	}
}

func TestInspectionRetainsEvidenceWhenSignatureWorkStalls(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "partial-stall")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], inspectionHelperFlag, "32")
	report := inspectCommand(ctx, cmd)
	if report.AgentSourceType != "workbuddy" || report.Confidence != "medium" || len(report.Warnings) == 0 {
		t.Fatalf("lost preliminary evidence: %+v", report)
	}
	if cmd.ProcessState == nil {
		t.Fatal("helper not reaped")
	}
}

func TestInspectionUsesCompletedIdentityEnrichment(t *testing.T) {
	t.Setenv("QFEI_INSPECTION_TEST_MODE", "partial-upgrade")
	report := Inspect(context.Background(), 32)
	if report.AgentSourceType != "workbuddy" || report.Confidence != "high" {
		t.Fatalf("lost enriched report: %+v", report)
	}
}
