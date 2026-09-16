//go:build windows

package invocation

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsRuntimeFallback(t *testing.T) {
	root := filepath.Join(t.TempDir(), "自定义 安装目录")
	shim := filepath.Join(root, "renamed-app", "vendor", "shim", "shell-runtime-bash-env.sh")
	product := filepath.Join(root, "renamed-app", "product.json")
	writeFixture(t, shim, "# runtime")
	writeFixture(t, product, `{"productName":"WorkBuddy","authentication":{"id":"workbuddy-desktop"}}`)
	base := filepath.Join(root, "Doubao", "User Data", "sandbox_runtime", "bases", "pack")
	dlc := filepath.Join(root, "Doubao", "User Data", "Default", "sandbox_envs_dir", "envs", "session")
	workBase := strings.Replace(base, "Doubao", "DoubaoWork", 1)
	workDLC := strings.Replace(dlc, "Doubao", "DoubaoWork", 1)
	for _, p := range []string{base, dlc, workBase, workDLC} {
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name, bash, base, dlc, want string
	}{
		{"no markers", "", "", "", "unknown"},
		{"custom WorkBuddy directory", shim, "", "", "workbuddy"},
		{"slash separators", filepath.ToSlash(shim), "", "", "workbuddy"},
		{"Git Bash path", "/" + strings.ToLower(shim[:1]) + filepath.ToSlash(shim[2:]), "", "", "workbuddy"},
		{"Doubao", "", base, dlc, "doubao"},
		{"DoubaoWork", "", workBase, workDLC, "doubaoWork"},
		{"missing paired marker", "", base, "", "unknown"},
		{"different products", "", base, workDLC, "unknown"},
		{"different roots", "", base, strings.Replace(dlc, "Default", "Default/extra", 1), "unknown"},
		{"conflicting clients", shim, base, dlc, "unknown"},
		{"relative path", "vendor/shim/shell-runtime-bash-env.sh", "", "", "unknown"},
		{"missing script", shim + "-missing", "", "", "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := map[string]string{"BASH_ENV": tc.bash, "CUA_BASE_PACK_DIR": tc.base, "CUA_DLC_DIR": tc.dlc}
			before := Analyze(nil, nil)
			before.Warnings = []string{"parent process exited"}
			got := windowsEnvironmentFallback(before, func(k string) string { return env[k] })
			if got.AgentSourceType != tc.want {
				t.Fatalf("%+v", got)
			}
			if len(got.Warnings) == 0 || got.Warnings[0] != "parent process exited" {
				t.Fatal("lost ancestry diagnostics")
			}
			if tc.want != "unknown" {
				if got.Confidence != "low" || got.EvidenceType != "windows_runtime_environment" || got.MatchedProcess != nil || got.Application != nil {
					t.Fatalf("incorrect evidence: %+v", got)
				}
				headers := http.Header{}
				ApplyHeaders(headers, got)
				if headers.Get(HeaderAgentSourceType) != tc.want || headers.Get(HeaderRuleID) == "" || headers.Get(HeaderProductCode) != before.ProductCode {
					t.Fatal(headers)
				}
			}
		})
	}
	// Real WorkBuddy product metadata is about 384 KiB, not a tiny manifest.
	writeFixture(t, product, `{"productName":"WorkBuddy","authentication":{"id":"workbuddy-desktop"},"ui":"`+strings.Repeat("x", 400000)+`"}`)
	if !workbuddyEnvironment(shim) {
		t.Fatal("rejected realistic product metadata size")
	}
	for _, payload := range []string{
		`{"productName":"CodeBuddy","authentication":{"id":"workbuddy-desktop"}}`,
		`{"productName":"WorkBuddy","authentication":{"id":"other"}}`,
		"{bad json", strings.Repeat(" ", 1024*1024+1),
	} {
		writeFixture(t, product, payload)
		if workbuddyEnvironment(shim) {
			t.Fatal("accepted wrong or malformed metadata")
		}
	}
	if err := os.Remove(product); err != nil {
		t.Fatal(err)
	}
	if workbuddyEnvironment(shim) {
		t.Fatal("accepted missing product")
	}
}

func TestEnvironmentPreservesAncestryDecision(t *testing.T) {
	for _, before := range []Result{
		{AgentSourceType: "doubao", EvidenceType: "windows_authenticode", Confidence: "high"},
		{AgentSourceType: "unknown", EvidenceType: "windows_authenticode_mismatch"},
	} {
		got := windowsEnvironmentFallback(before, func(string) string { t.Fatal("should not inspect environment"); return "" })
		if got.AgentSourceType != before.AgentSourceType || got.EvidenceType != before.EvidenceType {
			t.Fatal(got)
		}
	}
}

func TestInspectionHelperUsesRuntimeFallback(t *testing.T) {
	root := t.TempDir()
	shim := filepath.Join(root, "vendor", "shim", "shell-runtime-bash-env.sh")
	writeFixture(t, shim, "# runtime")
	writeFixture(t, filepath.Join(root, "product.json"), `{"productName":"WorkBuddy","authentication":{"id":"workbuddy-desktop"}}`)
	t.Setenv("BASH_ENV", shim)
	t.Setenv("CUA_BASE_PACK_DIR", "")
	t.Setenv("CUA_DLC_DIR", "")
	// A missing starting PID models an inaccessible/exited ancestry. The real
	// helper must retain its warnings and still apply environment evidence.
	got := inspectProcess(-1, 32)
	if got.AgentSourceType != "workbuddy" || got.Platform != "windows" || got.DetectorVersion != DetectorVersion {
		t.Fatalf("helper did not apply fallback: %+v", got)
	}
}

func TestWindowsWorkBuddySafeDeleteFallback(t *testing.T) {
	for _, product := range []string{"WorkBuddy", "WorkBuddyAI"} {
		path := filepath.Join(t.TempDir(), product, "resources", "app.asar.unpacked", "cli", "vendor", "shim", "safe-bin", "safe-delete-bash-env.sh")
		writeFixture(t, path, "# shell")
		got := windowsEnvironmentFallback(Result{AgentSourceType: "unknown", EvidenceType: "none"}, func(key string) string {
			if key == "BASH_ENV" {
				return path
			}
			return ""
		})
		if got.AgentSourceType != "workbuddy" || got.Confidence != "low" || got.RuleID != "client.workbuddy.environment" {
			t.Fatalf("%s fallback: %+v", product, got)
		}
	}
}
