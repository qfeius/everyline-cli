package invocation

import (
	"reflect"
	"runtime"
	"testing"
)

// Minimal non-sensitive projections of the 2026-09-11 Mac/Linux samples.
// No PID, username, token, session ID value, or user path is needed for replay.
var officeSampleKeys = []string{"DOUBAO_OFFICE_AGENT_NAME", "DOUBAO_OFFICE_EDITION", "DOUBAO_OFFICE_MARKET", "DOUBAO_OFFICE_PLATFORM_APP_ID"}
var workbuddySampleKeys = []string{"WORKBUDDY_CONFIG_DIR", "CODEBUDDY_BROKERED_SHELL_ENV", "CODEBUDDY_NODE_BIN", "BASH_ENV", "CLAUDE_SESSION_ID"}

func samplePresence(keys []string) func(string) bool {
	return func(key string) bool {
		for _, present := range keys {
			if key == present {
				return true
			}
		}
		return false
	}
}

func TestReplayMacSignedIdentityAndFeishuSample(t *testing.T) {
	chain := []Process{
		{Depth: 0, Name: "python"},
		{Depth: 1, Name: "bash", Executable: "/bin/bash"},
		{Depth: 2, Name: "shell_session_host", Executable: "/Applications/Lark.app/Contents/Frameworks/Lark Framework.framework/Libraries/shell_session_host"},
	}
	lark := ApplicationIdentity{ProcessDepth: 2, BundlePath: "/Applications/Lark.app", BundleID: "com.electron.lark", TeamID: "XY6NLV7YTS", SignatureValid: true}
	before := Analyze(chain, []ApplicationIdentity{lark})
	if before.AgentSourceType != "unknown" {
		t.Fatalf("Feishu alone must not identify Doubao Work: %+v", before)
	}
	before.Platform, before.Processes = "darwin", chain
	got := runtimeEnvironmentFallback(before, []ApplicationIdentity{lark}, samplePresence(officeSampleKeys), nil)
	if got.AgentSourceType != "doubaoWork" || got.Confidence != "low" || got.Application == nil || got.MatchedProcess == nil || got.RuleID != "client.doubao_work.feishu-runtime" {
		t.Fatalf("Feishu sample not recognized: %+v", got)
	}
	for _, identity := range []ApplicationIdentity{
		{BundlePath: lark.BundlePath, BundleID: lark.BundleID, TeamID: lark.TeamID},
		{BundlePath: lark.BundlePath, BundleID: lark.BundleID, TeamID: "WRONG", SignatureValid: true},
	} {
		if got := runtimeEnvironmentFallback(before, []ApplicationIdentity{identity}, samplePresence(officeSampleKeys), nil); got.AgentSourceType != "doubaoWork" || got.Confidence != "low" || got.EvidenceType != "host_path_runtime_environment" {
			t.Fatalf("Feishu path fallback lost on identity change: %+v", got)
		}
	}
	// The standalone Mac sample already matches the old signature registry.
	doubao := ApplicationIdentity{ProcessDepth: 2, BundlePath: "/Applications/DoubaoWork.app", BundleID: "com.work.pc.doubao", TeamID: "96L78H6LMH", SignatureValid: true}
	standalone := Analyze(chain, []ApplicationIdentity{doubao})
	standalone.Platform = "darwin"
	if standalone.AgentSourceType != "doubaoWork" || standalone.Confidence != "high" {
		t.Fatalf("standalone identity regression: %+v", standalone)
	}
	if got := runtimeEnvironmentFallback(standalone, nil, samplePresence(officeSampleKeys), nil); !reflect.DeepEqual(got, standalone) {
		t.Fatal("runtime evidence changed a verified standalone result")
	}
}

func TestRuntimeSampleReplayAndNegativeControls(t *testing.T) {
	cloudOffice := append(append([]string{}, officeSampleKeys...), "DOUBAO_SANDBOX_TYPE", "AIO_CLI_BIN_DIR")
	for _, tc := range []struct {
		name, platform, source string
		keys                   []string
	}{
		{"workbuddy local CN", "darwin", "workbuddy", workbuddySampleKeys},
		{"workbuddy local international same runtime", "darwin", "workbuddy", workbuddySampleKeys},
		{"office cloud including Feishu entry", "linux", "doubaoWork", cloudOffice},
		{"Windows Office without signed host", "windows", "unknown", cloudOffice},
		{"generic Linux", "linux", "unknown", []string{"HOME", "SESSION_ID", "container"}},
		{"Claude compatibility markers alone", "darwin", "unknown", []string{"CLAUDE_SESSION_ID", "CLAUDE_PROJECT_DIR"}},
		{"Aily is not uniquely Workmates", "linux", "unknown", []string{"AILY_WORKDIR", "AILY_WORKSPACE", "LARKSUITE_CLI_AGENT_NAME"}},
		{"CodeBuddy AgentOS needs product value", "linux", "unknown", []string{"CODEBUDDY_AGENTOS_SESSION_ID", "CODEBUDDY_SESSION_BIZ_SOURCE", "CLIENT_INFO_IDE_TYPE"}},
		{"conflicting products", "darwin", "unknown", append(append([]string{}, officeSampleKeys...), workbuddySampleKeys...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := Analyze(nil, nil)
			before.Platform = tc.platform
			before.Warnings = []string{"ancestry unavailable"}
			got := runtimeEnvironmentFallback(before, nil, samplePresence(tc.keys), nil)
			if got.AgentSourceType != tc.source || got.Warnings[0] != before.Warnings[0] {
				t.Fatalf("unexpected result: %+v", got)
			}
			if tc.source != "unknown" && got.Confidence != "low" {
				t.Fatalf("environment evidence overstated: %+v", got)
			}
		})
	}
	for index := range workbuddySampleKeys[:4] {
		keys := append([]string{}, workbuddySampleKeys[:index]...)
		keys = append(keys, workbuddySampleKeys[index+1:]...)
		before := Analyze(nil, nil)
		before.Platform = "darwin"
		if got := runtimeEnvironmentFallback(before, nil, samplePresence(keys), nil); got.AgentSourceType != "unknown" {
			t.Fatalf("partial broker markers accepted without %s", workbuddySampleKeys[index])
		}
	}
}

func TestReplayWindowsFeishuAndWorkBuddyInternational(t *testing.T) {
	feishuPath := `C:\Users\sample\AppData\Local\Feishu\app\Feishu.exe`
	chain := []Process{{Depth: 0, Name: "cli"}, {Depth: 1, Name: "powershell.exe"}, {Depth: 2, Name: "Feishu.exe", Executable: feishuPath}}
	identity := ApplicationIdentity{ProcessDepth: 2, ExecutablePath: feishuPath, SignatureValid: true, CertificateSHA256: "491a37249970e2212d85d929450b2b49f962b99b605684ae58e8b0fbe311c11c"}
	before := Analyze(chain, []ApplicationIdentity{identity})
	before.Platform, before.Processes = "windows", chain
	if before.AgentSourceType != "unknown" {
		t.Fatalf("Feishu alone misclassified: %+v", before)
	}
	got := runtimeEnvironmentFallback(before, []ApplicationIdentity{identity}, samplePresence(officeSampleKeys), nil)
	if got.AgentSourceType != "doubaoWork" || got.EvidenceType != "windows_signed_host_runtime" || got.MatchedProcess == nil || got.Confidence != "low" {
		t.Fatalf("Feishu sample: %+v", got)
	}
	for _, variant := range []ApplicationIdentity{
		{ExecutablePath: feishuPath, SignatureValid: false, CertificateSHA256: identity.CertificateSHA256},
		{ExecutablePath: feishuPath, SignatureValid: true, CertificateSHA256: "wrong"},
		{ExecutablePath: `C:\other\other.exe`, SignatureValid: true, CertificateSHA256: identity.CertificateSHA256},
	} {
		if got := runtimeEnvironmentFallback(before, []ApplicationIdentity{variant}, samplePresence(officeSampleKeys), nil); got.AgentSourceType != "doubaoWork" || got.Confidence != "low" || got.EvidenceType != "host_path_runtime_environment" {
			t.Fatalf("Feishu path fallback lost on identity change: %+v", got)
		}
	}
	if got := runtimeEnvironmentFallback(before, []ApplicationIdentity{identity}, samplePresence(nil), nil); got.AgentSourceType != "unknown" {
		t.Fatal("Feishu without Office accepted")
	}
	if !windowsFeishuExecutable(feishuPath) || windowsFeishuExecutable(`C:\Feishu\app\Feishu.exe.fake`) {
		t.Fatal("Feishu collector path mismatch")
	}

	workbuddyPath := `C:\Users\sample\AppData\Local\Programs\WorkBuddyAI\WorkBuddyAI.exe`
	chain[2] = Process{Depth: 2, Name: "WorkBuddyAI.exe", Executable: workbuddyPath}
	if rule := matchingWindowsRule(chain[2]); rule == nil || rule.ID != "client.workbuddy_international" {
		t.Fatal("international executable not scheduled for signature inspection")
	}
	identity = ApplicationIdentity{ProcessDepth: 2, ExecutablePath: workbuddyPath, SignatureValid: true, CertificateSHA256: "a5260c88f699b19bd6ed100bc08120b4fd872930ee7538c3d210eb14081a0f45"}
	got = Analyze(chain, []ApplicationIdentity{identity})
	if got.AgentSourceType != "workbuddy" || got.Confidence != "high" || got.RuleID != "client.workbuddy_international.authenticode" || got.EvidenceType != "windows_authenticode" {
		t.Fatalf("international sample: %+v", got)
	}
	identity.CertificateSHA256 = "a7d0aff6774068a4f37485b7e61cbf9d31b65190aaedfe8cb79ebd3c65cbce76"
	if got := Analyze(chain, []ApplicationIdentity{identity}); got.AgentSourceType != "workbuddy" || got.EvidenceType != "process_executable_path" || got.Confidence != "medium" {
		t.Fatalf("cross-release certificate should use path attribution: %+v", got)
	}
}

func TestRuntimeFallbackPreservesAllExistingDecisions(t *testing.T) {
	for _, source := range []string{"codex", "doubao", "doubaoWork", "workbuddy"} {
		before := Result{AgentSourceType: source, EvidenceType: "macos_code_signature", Confidence: "high", Platform: "darwin"}
		got := runtimeEnvironmentFallback(before, nil, func(string) bool { t.Fatal("unexpected environment lookup"); return true }, nil)
		if !reflect.DeepEqual(before, got) {
			t.Fatal(got)
		}
	}
	for _, evidence := range []string{"macos_code_signature_mismatch", "windows_authenticode_mismatch", "windows_package_identity_mismatch"} {
		before := Result{AgentSourceType: "unknown", EvidenceType: evidence, Platform: "darwin"}
		got := runtimeEnvironmentFallback(before, nil, func(string) bool { t.Fatal("unexpected environment lookup"); return true }, nil)
		if !reflect.DeepEqual(before, got) {
			t.Fatal(got)
		}
	}
}

func TestRuntimeFallbackInspectionWiring(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux cloud fixture")
	}
	for _, key := range workbuddySampleKeys {
		t.Setenv(key, "")
	}
	for _, key := range []string{"AILY_WORKDIR", "AILY_WORKSPACE", "LARKSUITE_CLI_AGENT_NAME"} {
		t.Setenv(key, "")
	}
	keys := append(append([]string{}, officeSampleKeys...), "DOUBAO_SANDBOX_TYPE", "AIO_CLI_BIN_DIR")
	for _, key := range keys {
		t.Setenv(key, "sample-marker")
	}
	got := inspectProcess(-1, 32)
	if got.AgentSourceType != "doubaoWork" || got.RuleID != "client.doubao_work.cloud-runtime" {
		t.Fatalf("inspection omitted runtime fallback: %+v", got)
	}
	for _, missing := range keys {
		t.Run(missing, func(t *testing.T) {
			t.Setenv(missing, "")
			if got := inspectProcess(-1, 32); got.AgentSourceType != "unknown" {
				t.Fatalf("incomplete cloud evidence accepted: %+v", got)
			}
		})
	}
}

func TestFeishuRuntimePathRequiresBothProductAndHost(t *testing.T) {
	for _, platform := range []string{"darwin", "windows"} {
		path := "/Applications/Lark.app/Contents/MacOS/Lark"
		if platform == "windows" {
			path = `C:\Users\sample\AppData\Local\Feishu\app\Feishu.exe`
		}
		for _, tc := range []struct {
			name, path string
			depth      int
			keys       []string
			want       string
		}{
			{"both", path, 1, officeSampleKeys, "doubaoWork"},
			{"host alone", path, 1, nil, "unknown"},
			{"runtime alone", "/bin/bash", 1, officeSampleKeys, "unknown"},
			{"current process is not host", path, 0, officeSampleKeys, "unknown"},
			{"similar bundle", "/Applications/Lark.app.fake/Contents/MacOS/Lark", 1, officeSampleKeys, "unknown"},
		} {
			t.Run(platform+"/"+tc.name, func(t *testing.T) {
				before := Analyze(nil, nil)
				before.Platform = platform
				before.Processes = []Process{{Depth: tc.depth, Executable: tc.path}}
				got := runtimeEnvironmentFallback(before, nil, samplePresence(tc.keys), nil)
				if got.AgentSourceType != tc.want {
					t.Fatalf("got %+v, want %s", got, tc.want)
				}
			})
		}
	}
}
