package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
	"golang.org/x/sys/unix"
)

// 故障注入只通过构建 overlay 替换本机身份检查；实际入口和 helper 生命周期保持生产实现。
func TestProductionBinaryInterruptReapsStalledInspection(t *testing.T) {
	directory := t.TempDir()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(directory, "identity_other.go")
	const stalledIdentity = `package invocation
import ("os"; "strconv"; "time")
func platformApplicationIdentities([]Process) ([]ApplicationIdentity, []string) {
	if err := os.WriteFile(os.Getenv("QFEI_INSPECTION_TEST_PID"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil { panic(err) }
	time.Sleep(time.Minute)
	return nil, nil
}`
	if err := os.WriteFile(replacement, []byte(stalledIdentity), 0600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]any{"Replace": map[string]string{
		filepath.Join(root, "internal/invocation/identity_other.go"): replacement,
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(directory, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	binary := filepath.Join(directory, "everyline-cli")
	build := exec.CommandContext(ctx, "go", "build", "-overlay", overlayPath, "-o", binary, "./cmd/everyline-cli")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		fmt.Fprint(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	configDir := filepath.Join(directory, "profile")
	store := config.NewFileStore(filepath.Join(configDir, "config.json"))
	if err := store.Add(config.Profile{Name: "local", BaseURL: server.URL, TokenURL: server.URL + "/token",
		AppID: "test-app", DefaultOutput: "json"}); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(directory, "helper.pid")
	t.Setenv("EVERYLINE_CONFIG_DIR", configDir)
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "binary-test-token")
	t.Setenv("QFEI_INSPECTION_TEST_PID", pidFile)
	command := exec.CommandContext(ctx, binary, "checklist", "list", "--profile", "local")
	// 模拟终端向前台进程组发送 Ctrl+C；探测 helper 有自己的进程组。
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var helperPID int
	exited := false
	defer func() {
		if !exited {
			_ = command.Process.Kill()
			<-done
		}
		if helperPID > 0 {
			_ = syscall.Kill(-helperPID, syscall.SIGKILL)
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidFile); err == nil {
			helperPID, _ = strconv.Atoi(string(data))
			if helperPID > 0 {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if helperPID <= 0 {
		t.Fatal("stalled inspection helper did not start")
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		exited = true
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) || exitError.ExitCode() != 130 {
			t.Errorf("inspection interrupt should exit after helper cleanup: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("interrupt did not cancel inspection before its five-second timeout")
	}
	if err := syscall.Kill(helperPID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("inspection helper was not reaped: %v", err)
	}
	helperPID = 0
	t.Run("first-interrupt-while-reading-stdin", func(t *testing.T) {
		// 业务输入保持 release 的默认信号行为，第一次 Ctrl+C 即退出。
		command := exec.CommandContext(ctx, binary, "review", "file", "upload", "--stdin", "--name", "test.pdf", "--profile", "local", "--verbose")
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		input, err := command.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		defer input.Close()
		logPath := filepath.Join(directory, "stdin-command.log")
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		command.Stderr = log
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		exited := false
		defer func() {
			if !exited {
				_ = command.Process.Kill()
				<-done
			}
		}()
		deadline := time.Now().Add(3 * time.Second)
		ready := false
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(logPath)
			if err == nil && strings.Contains(string(data), "正在上传合同文件") {
				ready = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ready {
			t.Fatal("command did not reach stdin read")
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			exited = true
			status := command.ProcessState.Sys().(syscall.WaitStatus)
			if err == nil || !status.Signaled() || status.Signal() != syscall.SIGINT {
				t.Fatalf("first interrupt should terminate blocked stdin read: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("first interrupt was swallowed while reading stdin")
		}
	})
	t.Run("first-interrupt-while-reading-app-secret", func(t *testing.T) {
		master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer master.Close()
		if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
			t.Fatal(err)
		}
		number, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
		if err != nil {
			t.Fatal(err)
		}
		slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		defer slave.Close()
		logPath := filepath.Join(directory, "login-command.log")
		log, err := os.Create(logPath)
		if err != nil {
			t.Fatal(err)
		}
		defer log.Close()
		command := exec.CommandContext(ctx, binary, "auth", "login", "--as", "app", "--app-secret-stdin", "--profile", "local")
		command.Stdin, command.Stderr = slave, log
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		exited := false
		defer func() {
			if !exited {
				_ = command.Process.Kill()
				<-done
			}
		}()
		ready := false
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			data, err := os.ReadFile(logPath)
			if err == nil && strings.Contains(string(data), "App secret: ") {
				ready = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !ready {
			t.Fatal("login did not reach terminal password input")
		}
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			exited = true
			status := command.ProcessState.Sys().(syscall.WaitStatus)
			if err == nil || !status.Signaled() || status.Signal() != syscall.SIGINT {
				t.Fatalf("first interrupt should terminate password input: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("first interrupt was swallowed while reading app secret")
		}
	})
	if count := requests.Load(); count != 0 {
		t.Fatalf("cancelled command sent %d business requests", count)
	}
}
