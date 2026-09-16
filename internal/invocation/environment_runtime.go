package invocation

import (
	"os"
	"path/filepath"
	"strings"
)

// Runtime evidence is attribution, not authentication. Only fill an otherwise
// unknown result; preserve existing attribution and conflicting evidence.
// has reports nonempty variable presence, without retaining or logging values.
func runtimeEnvironmentFallback(result Result, identities []ApplicationIdentity, has func(string) bool, getenv func(string) string) Result {
	if result.AgentSourceType != "unknown" || result.EvidenceType != "none" {
		return result
	}
	// Existing Windows identity and filesystem-backed fallback rules run first.
	if result.Platform != "darwin" && result.Platform != "linux" && result.Platform != "windows" {
		return result
	}
	all := func(keys ...string) bool {
		for _, key := range keys {
			if !has(key) {
				return false
			}
		}
		return true
	}
	office := all("DOUBAO_OFFICE_AGENT_NAME", "DOUBAO_OFFICE_EDITION", "DOUBAO_OFFICE_MARKET", "DOUBAO_OFFICE_PLATFORM_APP_ID")
	workbuddy := all("WORKBUDDY_CONFIG_DIR", "CODEBUDDY_BROKERED_SHELL_ENV", "CODEBUDDY_NODE_BIN", "BASH_ENV")
	aily := all("AILY_WORKDIR", "AILY_WORKSPACE", "LARKSUITE_CLI_AGENT_NAME")
	agentos := all("CODEBUDDY_AGENTOS_SESSION_ID", "CODEBUDDY_SESSION_BIZ_SOURCE", "CLIENT_INFO_IDE_TYPE")
	if (office && workbuddy) || (aily && (office || workbuddy || agentos)) || (office && agentos) {
		result.Warnings = append(result.Warnings, "conflicting runtime product markers")
		return result
	}
	if result.Platform == "darwin" && office {
		for _, identity := range identities {
			// Feishu alone is not Doubao Work. Require its verified main bundle
			// together with the Office runtime markers seen in the local sample.
			if identity.SignatureValid && strings.EqualFold(identity.BundleID, "com.electron.lark") &&
				strings.EqualFold(identity.TeamID, "XY6NLV7YTS") {
				result = matchedRuntime(result, "doubaoWork", "client.doubao_work.feishu-runtime", "macos_signed_host_runtime",
					"matched verified Feishu identity and Doubao Work runtime markers")
				result.Application = copyApplicationIdentity(identity)
				result.MatchedProcess = processAtDepth(result.Processes, identity.ProcessDepth)
				return result
			}
		}
	}
	if result.Platform == "darwin" && workbuddy {
		return matchedRuntime(result, "workbuddy", "client.workbuddy.brokered-runtime", "macos_runtime_environment",
			"matched WorkBuddy product and brokered shell markers; process ancestry did not identify a client")
	}
	if result.Platform == "windows" && office {
		for _, identity := range identities {
			if identity.SignatureValid && windowsFeishuExecutable(identity.ExecutablePath) &&
				matchesCertificateSHA256(identity.CertificateSHA256, []string{windowsFeishuCertificate}) {
				result = matchedRuntime(result, "doubaoWork", "client.doubao_work.feishu-runtime", "windows_signed_host_runtime",
					"matched verified Feishu executable and Doubao Work runtime markers")
				result.Application = copyApplicationIdentity(identity)
				result.MatchedProcess = processAtDepth(result.Processes, identity.ProcessDepth)
				return result
			}
		}
	}
	// Host identity is optional corroboration. Product-specific runtime markers
	// plus an observed host ancestor still identify the embedded product when
	// certificate rotation, bundle changes or signature inspection fail.
	if office && (result.Platform == "darwin" || result.Platform == "windows") {
		for _, process := range result.Processes {
			path := normalizeExecutable(process.Executable)
			host := result.Platform == "windows" && windowsFeishuExecutable(path)
			if result.Platform == "darwin" {
				host = strings.Contains(path, "/lark.app/contents/") || strings.Contains(path, "/feishu.app/contents/")
			}
			if process.Depth > 0 && host {
				result = matchedRuntime(result, "doubaoWork", "client.doubao_work.feishu-runtime-path", "host_path_runtime_environment",
					"matched Feishu host ancestor path and Doubao Work runtime markers")
				result.MatchedProcess = processAtDepth(result.Processes, process.Depth)
				return result
			}
		}
	}
	if result.Platform == "linux" && office && all("DOUBAO_SANDBOX_TYPE", "AIO_CLI_BIN_DIR") {
		return matchedRuntime(result, "doubaoWork", "client.doubao_work.cloud-runtime", "linux_runtime_environment",
			"matched Doubao Work Office and cloud runtime markers")
	}
	if result.Platform == "linux" && getenv != nil {
		// Both observed Web editions have the same labels. Attribute to
		// WorkBuddy without claiming to distinguish their region/edition.
		if agentos && all("AGENTOS_RUNTIME_ID") &&
			strings.TrimSpace(getenv("CLIENT_INFO_IDE_TYPE")) == "WorkBuddy_Web" &&
			strings.TrimSpace(getenv("CLIENT_INFO_PLATFORM")) == "web_agents" &&
			strings.TrimSpace(getenv("CODEBUDDY_SESSION_BIZ_SOURCE")) == "agent-server" &&
			strings.TrimSpace(getenv("CODEBUDDY_CODE_ENTRYPOINT")) == "sdk-go" {
			return matchedRuntime(result, "workbuddy", "client.workbuddy.web-runtime", "linux_runtime_environment",
				"matched WorkBuddy Web product labels and AgentOS runtime markers")
		}
		// This is the observed Workmates runtime profile, not a claim that
		// every Aily agent is Workmates. Require label, task directories and
		// the execution-service ancestry together; keep confidence low.
		if aily && all("AILY_CLI_RUNTIME_CONTEXT_SCENE", "AILY_CLI_SURFACE") &&
			strings.TrimSpace(getenv("LARKSUITE_CLI_AGENT_NAME")) == "aily_agent" &&
			hasAncestorName(result.Processes, "python-server") && hasAncestorName(result.Processes, "runtime-agent") &&
			workmatesRuntimePaths(getenv) {
			return matchedRuntime(result, "doubaoWorkmates", "client.doubao_workmates.cloud-runtime", "linux_runtime_environment",
				"matched observed Workmates Aily label, task directories and execution-service ancestry")
		}
	}
	if aily || agentos {
		result.Warnings = append(result.Warnings, "recognized shared agent runtime markers, but product identity needs a discriminator")
	}
	return result
}

func hasAncestorName(chain []Process, name string) bool {
	for _, process := range chain {
		if process.Depth > 0 && process.Name == name {
			return true
		}
	}
	return false
}

func workmatesRuntimePaths(getenv func(string) string) bool {
	workdir, workspace := strings.TrimSpace(getenv("AILY_WORKDIR")), strings.TrimSpace(getenv("AILY_WORKSPACE"))
	if !filepath.IsAbs(workdir) || !filepath.IsAbs(workspace) {
		return false
	}
	workdir, workspace = filepath.Clean(workdir), filepath.Clean(workspace)
	root := filepath.Dir(workspace)
	task := filepath.Base(workdir)
	if filepath.Base(root) != ".aily" || filepath.Base(workspace) != "workspace" ||
		filepath.Dir(workdir) != filepath.Join(root, "workdir") || !strings.HasPrefix(task, "task_") || len(task) <= len("task_") {
		return false
	}
	for _, path := range []string{workdir, workspace} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// Windows Feishu is a hosting application, not itself a Doubao Work rule.
// The signature collector inspects it; attribution also requires Office markers.
const windowsFeishuCertificate = "491a37249970e2212d85d929450b2b49f962b99b605684ae58e8b0fbe311c11c"

func windowsFeishuExecutable(path string) bool {
	return strings.HasSuffix(normalizeExecutable(path), "/feishu/app/feishu.exe")
}

func matchedRuntime(result Result, source, rule, evidence, reason string) Result {
	result.AgentSourceType = source
	result.RuleID = rule
	result.EvidenceType = evidence
	// Even with a signed hosting application, the embedded product selector
	// comes from its environment, so it does not inherit signature confidence.
	result.Confidence = "low"
	result.Reason = reason
	return result
}
