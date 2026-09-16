package invocation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkBuddyCertificateRotationAttribution(t *testing.T) {
	for _, name := range []string{"WorkBuddy", "WorkBuddyAI"} {
		for _, valid := range []bool{false, true} {
			chain := []Process{{Depth: 0, Name: "cli"}, {Depth: 1, Name: "sandbox-cli.exe", Executable: `C:\Programs\` + name + `\resources\app.asar.unpacked\cli\vendor\sandbox\5.5.5\sandbox-cli.exe`}, {Depth: 2, Name: name + ".exe", Executable: `C:\Programs\` + name + `\` + name + `.exe`}}
			ids := []ApplicationIdentity{{ProcessDepth: 1, ExecutablePath: chain[1].Executable, SignatureValid: valid, CertificateSHA256: "helper-certificate"}, {ProcessDepth: 2, ExecutablePath: chain[2].Executable, SignatureValid: valid, CertificateSHA256: "rotated-certificate"}}
			got := Analyze(chain, ids)
			if got.AgentSourceType != "workbuddy" || got.Confidence != "medium" || got.EvidenceType != "process_executable_path" {
				t.Fatalf("%s valid=%v: %+v", name, valid, got)
			}
			if name == "WorkBuddyAI" {
				ids[1].SignatureValid = true
				ids[1].CertificateSHA256 = "a5260c88f699b19bd6ed100bc08120b4fd872930ee7538c3d210eb14081a0f45"
				got = Analyze(chain, ids)
				if got.AgentSourceType != "workbuddy" || got.Confidence != "high" || got.MatchedProcess.Depth != 2 {
					t.Fatalf("registered main must beat helper mismatch: %+v", got)
				}
			}
		}
	}
}

func TestWorkBuddySafeDeleteShellAttribution(t *testing.T) {
	for _, product := range []string{"WorkBuddy", "WorkBuddyAI", "OtherApp"} {
		path := filepath.Join(t.TempDir(), product, "resources", "app.asar.unpacked", "cli", "vendor", "shim", "safe-bin", "safe-delete-bash-env.sh")
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if workbuddyShellEnvironment(path) {
			t.Fatal("nonexistent script accepted")
		}
		if err := os.WriteFile(path, []byte("# shell"), 0600); err != nil {
			t.Fatal(err)
		}
		want := product != "OtherApp"
		if got := workbuddyShellEnvironment(path); got != want {
			t.Fatalf("%s: got %v want %v", product, got, want)
		}
	}
	path := filepath.Join(t.TempDir(), "cli", "vendor", "shim", "shell-runtime-bash-env.sh")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if workbuddyShellEnvironment(path) {
		t.Fatal("legacy path without metadata accepted")
	}
	productPath := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(path))), "product.json")
	if err := os.WriteFile(productPath, []byte(`{"productName":"WorkBuddy","authentication":{"id":"workbuddy-desktop"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if !workbuddyShellEnvironment(path) {
		t.Fatal("legacy product metadata no longer accepted")
	}
}

func TestMacWorkBuddyIdentityChanges(t *testing.T) {
	chain := []Process{{Depth: 0}, {Depth: 1, Name: "Electron", Executable: "/Applications/WorkBuddy.app/Contents/MacOS/Electron"}}
	for _, id := range []string{"com.workbuddy.workbuddy", "com.tencent.workbuddy.mac", "future.bundle"} {
		for _, valid := range []bool{true, false} {
			got := Analyze(chain, []ApplicationIdentity{{ProcessDepth: 1, BundlePath: "/Applications/WorkBuddy.app", BundleID: id, TeamID: "FN2V63AD2J", SignatureValid: valid}})
			want := "medium"
			if valid && id != "future.bundle" {
				want = "high"
			}
			if got.AgentSourceType != "workbuddy" || got.Confidence != want {
				t.Fatalf("id=%s valid=%v: %+v", id, valid, got)
			}
		}
	}
}

func TestConflictingProductPathsStayUnknown(t *testing.T) {
	got := Analyze([]Process{{Depth: 0}, {Depth: 1, Executable: "/Applications/WorkBuddy.app/Contents/MacOS/Electron"}, {Depth: 2, Executable: "/Applications/Doubao.app/Contents/MacOS/Electron"}}, nil)
	if got.AgentSourceType != "unknown" || got.EvidenceType != "conflicting_process_evidence" {
		t.Fatalf("ambiguous attribution: %+v", got)
	}
}
