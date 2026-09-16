package invocation

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func cloudSampleEnvironment(values map[string]string) (func(string) bool, func(string) string) {
	return func(key string) bool { return strings.TrimSpace(values[key]) != "" }, func(key string) string { return values[key] }
}

func webSampleLabels() map[string]string {
	return map[string]string{
		"CLIENT_INFO_IDE_TYPE": "WorkBuddy_Web", "CLIENT_INFO_PLATFORM": "web_agents",
		"CODEBUDDY_SESSION_BIZ_SOURCE": "agent-server", "CODEBUDDY_CODE_ENTRYPOINT": "sdk-go",
		"CODEBUDDY_AGENTOS_SESSION_ID": "presence-only", "AGENTOS_RUNTIME_ID": "presence-only",
	}
}

func cloudUnknown() Result {
	result := Analyze(nil, nil)
	result.Platform = "linux"
	return result
}

func TestReplayWorkBuddyWebBothEditions(t *testing.T) {
	for _, edition := range []string{"domestic", "international"} {
		t.Run(edition, func(t *testing.T) {
			values := webSampleLabels()
			has, get := cloudSampleEnvironment(values)
			// The collector may check session-variable presence, but must not
			// read its value (or any token) for classification.
			getLabel := func(key string) string {
				switch key {
				case "CLIENT_INFO_IDE_TYPE", "CLIENT_INFO_PLATFORM", "CODEBUDDY_SESSION_BIZ_SOURCE", "CODEBUDDY_CODE_ENTRYPOINT":
					return get(key)
				default:
					t.Fatalf("unexpected value lookup %s", key)
					return ""
				}
			}
			got := runtimeEnvironmentFallback(cloudUnknown(), nil, has, getLabel)
			if got.AgentSourceType != "workbuddy" || got.RuleID != "client.workbuddy.web-runtime" || got.Confidence != "low" {
				t.Fatalf("%s Web sample: %+v", edition, got)
			}
			for key := range values {
				saved := values[key]
				values[key] = ""
				if got := runtimeEnvironmentFallback(cloudUnknown(), nil, has, getLabel); got.AgentSourceType != "unknown" {
					t.Fatalf("accepted missing %s: %+v", key, got)
				}
				values[key] = saved
			}
			for _, key := range []string{"CLIENT_INFO_IDE_TYPE", "CLIENT_INFO_PLATFORM", "CODEBUDDY_SESSION_BIZ_SOURCE", "CODEBUDDY_CODE_ENTRYPOINT"} {
				saved := values[key]
				values[key] = "different-product"
				if got := runtimeEnvironmentFallback(cloudUnknown(), nil, has, getLabel); got.AgentSourceType != "unknown" {
					t.Fatalf("accepted different %s: %+v", key, got)
				}
				values[key] = saved
			}
		})
	}
}

func TestReplayWorkmatesAndRejectGenericAily(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".aily")
	workdir, workspace := filepath.Join(root, "workdir", "task_sample"), filepath.Join(root, "workspace")
	for _, dir := range []string{workdir, workspace} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	values := map[string]string{
		"AILY_WORKDIR": workdir, "AILY_WORKSPACE": workspace, "LARKSUITE_CLI_AGENT_NAME": "aily_agent",
		"AILY_CLI_RUNTIME_CONTEXT_SCENE": "presence-only", "AILY_CLI_SURFACE": "presence-only",
	}
	has, get := cloudSampleEnvironment(values)
	before := cloudUnknown()
	before.Processes = []Process{{Depth: 0, Name: "cli"}, {Depth: 1, Name: "bash"}, {Depth: 2, Name: "python-server"}, {Depth: 3, Name: "supervisord"}, {Depth: 4, Name: "runtime-agent"}, {Depth: 5, Name: "dumb-init"}}
	got := runtimeEnvironmentFallback(before, nil, has, get)
	if got.AgentSourceType != "doubaoWorkmates" || got.RuleID != "client.doubao_workmates.cloud-runtime" || got.Confidence != "low" {
		t.Fatalf("Workmates replay: %+v", got)
	}
	for key := range values {
		saved := values[key]
		values[key] = ""
		if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
			t.Fatalf("accepted missing %s", key)
		}
		values[key] = saved
	}
	for _, path := range []string{"relative/workdir/task_example", filepath.Join(root, "workdir", "task_missing"), workspace, filepath.Join(root, "other", "task_sample")} {
		values["AILY_WORKDIR"] = path
		if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
			t.Fatalf("bad path accepted: %s", path)
		}
	}
	other := filepath.Join(t.TempDir(), ".aily", "workdir", "task_sample")
	if err := os.MkdirAll(other, 0700); err != nil {
		t.Fatal(err)
	}
	values["AILY_WORKDIR"] = other
	if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
		t.Fatal("different workspace roots accepted")
	}
	values["AILY_WORKDIR"] = workdir
	values["LARKSUITE_CLI_AGENT_NAME"] = "other_agent"
	if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
		t.Fatal("generic Aily agent accepted")
	}
	values["LARKSUITE_CLI_AGENT_NAME"] = "aily_agent"
	for _, name := range []string{"python-server", "runtime-agent"} {
		candidate := before
		candidate.Processes = nil
		for _, p := range before.Processes {
			if p.Name != name {
				candidate.Processes = append(candidate.Processes, p)
			}
		}
		if got := runtimeEnvironmentFallback(candidate, nil, has, get); got.AgentSourceType != "unknown" {
			t.Fatalf("accepted without %s ancestry", name)
		}
	}
	for key, value := range webSampleLabels() {
		values[key] = value
	}
	if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
		t.Fatal("conflicting cloud products accepted")
	}
}

func TestCloudLabelsRespectExistingDecisionAndPlatform(t *testing.T) {
	has, get := cloudSampleEnvironment(webSampleLabels())
	for _, platform := range []string{"darwin", "windows"} {
		before := cloudUnknown()
		before.Platform = platform
		if got := runtimeEnvironmentFallback(before, nil, has, get); got.AgentSourceType != "unknown" {
			t.Fatalf("Web cloud matched %s", platform)
		}
	}
	for _, before := range []Result{
		{AgentSourceType: "codex", EvidenceType: "process_name", Platform: "linux"},
		{AgentSourceType: "unknown", EvidenceType: "macos_code_signature_mismatch", Platform: "linux"},
	} {
		got := runtimeEnvironmentFallback(before, nil, has, func(string) string { t.Fatal("unexpected product lookup"); return "" })
		if !reflect.DeepEqual(before, got) {
			t.Fatal("changed existing decision")
		}
	}
}

func TestWebCloudInspectionWiring(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux cloud fixture")
	}
	for _, key := range append(append([]string{}, officeSampleKeys...), "AILY_WORKDIR", "AILY_WORKSPACE", "LARKSUITE_CLI_AGENT_NAME") {
		t.Setenv(key, "")
	}
	for key, value := range webSampleLabels() {
		t.Setenv(key, value)
	}
	got := inspectProcess(-1, 32)
	if got.AgentSourceType != "workbuddy" || got.RuleID != "client.workbuddy.web-runtime" {
		t.Fatalf("Web cloud wiring: %+v", got)
	}
}
