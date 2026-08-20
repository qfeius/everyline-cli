package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewRuntimeUsesKeychainBeforeFileSecretStoreOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Keychain 仅在 darwin 验证")
	}

	directory := t.TempDir()
	securityScript := filepath.Join(directory, "security")
	argsPath := filepath.Join(directory, "security.args")
	stdinPath := filepath.Join(directory, "security.stdin")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$KEYCHAIN_ARGS_PATH\"\ncat > \"$KEYCHAIN_STDIN_PATH\"\n"
	if err := os.WriteFile(securityScript, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEYCHAIN_ARGS_PATH", argsPath)
	t.Setenv("KEYCHAIN_STDIN_PATH", stdinPath)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	configDir := filepath.Join(directory, "config")
	runtime := NewRuntime(configDir, strings.NewReader(""), nil, nil)
	if err := runtime.Secrets.SaveAppSecret("dev", "secret-value"); err != nil {
		t.Fatal(err)
	}

	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("expected security command invocation: %v", err)
	}
	if strings.Contains(string(args), "secret-value") {
		t.Fatalf("security args leaked secret: %q", args)
	}
	stdin, err := os.ReadFile(stdinPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stdin) != "secret-value\n" {
		t.Fatalf("security stdin=%q, want secret through stdin", stdin)
	}
	if _, err := os.Stat(filepath.Join(configDir, "secrets.json")); !os.IsNotExist(err) {
		t.Fatalf("file fallback should not be used after successful Keychain save, stat err=%v", err)
	}
}
