package invocation

import "testing"

func TestAnalyzePrefersVerifiedApplicationIdentity(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 10, Name: "node", Executable: "/usr/local/bin/node"},
		{Depth: 2, PID: 10, PPID: 1, Name: "codex", Executable: "/Applications/ChatGPT.app/Contents/Resources/codex"},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 2, BundlePath: "/Applications/ChatGPT.app", BundleID: "com.openai.codex", TeamID: "2DC432GLL2", SignatureValid: true},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "codex" || result.EvidenceType != "macos_code_signature" || result.Confidence != "high" {
		t.Fatalf("Analyze() = %#v", result)
	}
	if result.MatchedProcess == nil || result.MatchedProcess.Depth != 2 {
		t.Fatalf("matched process = %#v", result.MatchedProcess)
	}
}

func TestAnalyzeRecognizesVerifiedDoubaoWorkIdentity(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "DoubaoWork", Executable: "/Applications/DoubaoWork.app/Contents/MacOS/DoubaoWork"},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 1, BundlePath: "/Applications/DoubaoWork.app", BundleID: "com.work.pc.doubao", TeamID: "96L78H6LMH", SignatureValid: true},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "doubaoWork" || result.EvidenceType != "macos_code_signature" || result.Confidence != "high" {
		t.Fatalf("Analyze() = %#v", result)
	}
	if result.RuleID != "client.doubao_work.signed-bundle" {
		t.Fatalf("rule id = %q", result.RuleID)
	}
}

func TestAnalyzeRetainsPathForChangedSigningTeam(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "codebuddy", Executable: "/Applications/WorkBuddy.app/Contents/Resources/codebuddy"},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 1, BundlePath: "/Applications/WorkBuddy.app", BundleID: "com.workbuddy.workbuddy", TeamID: "UNTRUSTED", SignatureValid: true},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "workbuddy" || result.EvidenceType != "process_executable_path" || result.Confidence != "medium" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeUsesWindowsPackageIdentity(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "Codex.exe", Executable: `C:\\Program Files\\WindowsApps\\OpenAI.Codex_1.0.0_x64__2p2nqsd0c76g0\\Codex.exe`},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 1, ExecutablePath: chain[1].Executable, PackageFamilyName: "OpenAI.Codex_2p2nqsd0c76g0"},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "codex" || result.EvidenceType != "windows_package_identity" || result.Confidence != "high" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeUsesWindowsAuthenticodeIdentity(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "Doubao.exe", Executable: `C:\\Users\\lucas\\AppData\\Local\\Doubao\\Doubao.exe`},
	}
	identities := []ApplicationIdentity{
		{
			ProcessDepth:      1,
			ExecutablePath:    chain[1].Executable,
			Publisher:         "北京春田知韵科技有限公司",
			CertificateSHA256: "F0:5E:61:00:36:ED:DB:25:4D:1D:93:44:B8:24:AC:96:9C:CE:3E:5B:56:58:48:5C:D2:16:77:67:62:32:62:DC",
			SignatureValid:    true,
		},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "doubao" || result.EvidenceType != "windows_authenticode" || result.Confidence != "high" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeUsesDoubaoWorkWindowsAuthenticodeIdentity(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "DoubaoWork.exe", Executable: `C:\\Users\\lucas\\AppData\\Local\\DoubaoWork\\DoubaoWork.exe`},
	}
	identities := []ApplicationIdentity{
		{
			ProcessDepth:      1,
			ExecutablePath:    chain[1].Executable,
			Publisher:         "北京春田知韵科技有限公司",
			CertificateSHA256: "F0:5E:61:00:36:ED:DB:25:4D:1D:93:44:B8:24:AC:96:9C:CE:3E:5B:56:58:48:5C:D2:16:77:67:62:32:62:DC",
			SignatureValid:    true,
		},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "doubaoWork" || result.EvidenceType != "windows_authenticode" || result.Confidence != "high" {
		t.Fatalf("Analyze() = %#v", result)
	}
	if result.RuleID != "client.doubao_work.authenticode" {
		t.Fatalf("rule id = %q", result.RuleID)
	}
}

func TestAnalyzeRetainsPathForRotatedWindowsCertificate(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "Doubao.exe", Executable: `C:\\Users\\lucas\\AppData\\Local\\Doubao\\Doubao.exe`},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 1, ExecutablePath: chain[1].Executable, CertificateSHA256: "untrusted", SignatureValid: true},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "doubao" || result.EvidenceType != "process_executable_path" || result.Confidence != "medium" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeRetainsPathForChangedWindowsPackage(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "Codex.exe", Executable: `C:\\Program Files\\WindowsApps\\OpenAI.Codex_1.0.0_x64__2p2nqsd0c76g0\\Codex.exe`},
	}
	identities := []ApplicationIdentity{
		{ProcessDepth: 1, ExecutablePath: chain[1].Executable, PackageFamilyName: "Untrusted.Codex_123"},
	}

	result := Analyze(chain, identities)
	if result.AgentSourceType != "codex" || result.EvidenceType != "process_executable_path" || result.Confidence != "medium" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeUsesExecutablePathWhenNoPlatformIdentityIsAvailable(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "host", Executable: `C:\\Program Files\\Doubao\\host.exe`},
	}

	result := Analyze(chain, nil)
	if result.AgentSourceType != "doubao" || result.EvidenceType != "process_executable_path" || result.Confidence != "medium" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeUsesProcessNameAsLowConfidenceFallback(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "Doubao.exe", Executable: `C:\\unknown\\host.exe`},
	}

	result := Analyze(chain, nil)
	if result.AgentSourceType != "doubao" || result.EvidenceType != "process_name" || result.Confidence != "low" {
		t.Fatalf("Analyze() = %#v", result)
	}
}

func TestAnalyzeReturnsUnknownForUnregisteredAncestry(t *testing.T) {
	chain := []Process{
		{Depth: 0, PID: 30, PPID: 20, Name: "everyline-cli"},
		{Depth: 1, PID: 20, PPID: 1, Name: "zsh", Executable: "/bin/zsh"},
	}

	result := Analyze(chain, nil)
	if result.AgentSourceType != "unknown" || result.EvidenceType != "none" || result.Confidence != "unknown" {
		t.Fatalf("Analyze() = %#v", result)
	}
}
