package invocation

import (
	"fmt"
	"path/filepath"
	"strings"
)

type applicationRule struct {
	ID                        string
	Channel                   string
	BundleID                  string
	BundleIDAliases           []string
	TeamID                    string
	WindowsPackageFamilyNames []string
	WindowsCertificateSHA256  []string
	ExecutableMarkers         []string
	ProcessNames              []string
}

var applicationRules = []applicationRule{
	{
		ID:       "client.doubao",
		Channel:  "doubao",
		BundleID: "com.bot.pc.doubao",
		TeamID:   "96L78H6LMH",
		// Leaf certificate from the official Doubao Windows installer 1.81.6.
		// Registered certificates increase confidence; rotation retains path/name attribution.
		WindowsCertificateSHA256: []string{
			"f05e610036eddb254d1d9344b824ac969cce3e5b5658485cd2167767623262dc",
		},
		ExecutableMarkers: []string{"/applications/doubao.app/", `\doubao\`, `\doubao.exe`},
		ProcessNames:      []string{"doubao", "doubao.exe"},
	},
	{
		ID:       "client.doubao_work",
		Channel:  "doubaoWork",
		BundleID: "com.work.pc.doubao",
		TeamID:   "96L78H6LMH",
		// Leaf certificate shared by the official Doubao Work Windows 2.27.10
		// x64 and ARM64 release packages.
		// Doubao and Doubao Work currently share a publisher certificate, so the
		// executable path/name remains part of the Windows high-confidence match.
		WindowsCertificateSHA256: []string{
			"f05e610036eddb254d1d9344b824ac969cce3e5b5658485cd2167767623262dc",
		},
		ExecutableMarkers: []string{"/applications/doubaowork.app/", `\doubaowork\`, `\doubaowork.exe`},
		ProcessNames:      []string{"doubaowork", "doubaowork.exe"},
	},
	{
		ID:              "client.workbuddy",
		Channel:         "workbuddy",
		BundleID:        "com.workbuddy.workbuddy",
		BundleIDAliases: []string{"com.tencent.workbuddy.mac", "com.workbuddy.workbuddy-ai"},
		TeamID:          "FN2V63AD2J",
		// Leaf certificate from the official WorkBuddy Windows installer 5.3.14.36279234.
		WindowsCertificateSHA256: []string{
			"a7d0aff6774068a4f37485b7e61cbf9d31b65190aaedfe8cb79ebd3c65cbce76",
		},
		ExecutableMarkers: []string{"/applications/workbuddy.app/", "/applications/workbuddy ai.app/", `\workbuddy\`, `\codebuddy\`, `\workbuddy.exe`, `\codebuddy.exe`},
		ProcessNames:      []string{"workbuddy", "workbuddy.exe", "codebuddy", "codebuddy.exe"},
	},
	{
		ID:      "client.workbuddy_international",
		Channel: "workbuddy",
		// Verified WorkBuddyAI.exe in the 2026-09-11 Windows sample.
		// Keep its certificate/path pair separate from the domestic release.
		WindowsCertificateSHA256: []string{"a5260c88f699b19bd6ed100bc08120b4fd872930ee7538c3d210eb14081a0f45"},
		ExecutableMarkers:        []string{`\workbuddyai\`, `\workbuddyai.exe`},
		ProcessNames:             []string{"workbuddyai.exe"},
	},
	{
		ID:       "client.codex",
		Channel:  "codex",
		BundleID: "com.openai.codex",
		TeamID:   "2DC432GLL2",
		// Package identity published by OpenAI for Codex in Microsoft Store product 9PLM9XGG6VKS.
		WindowsPackageFamilyNames: []string{
			"OpenAI.Codex_2p2nqsd0c76g0",
		},
		ExecutableMarkers: []string{"/applications/chatgpt.app/", "/applications/codex.app/", `\chatgpt\`, `\codex\`, `\chatgpt.exe`, `\codex.exe`},
		ProcessNames:      []string{"chatgpt", "chatgpt.exe", "codex", "codex.exe"},
	},
}

func Analyze(chain []Process, identities []ApplicationIdentity) Result {
	result := Result{
		ChannelType:     "cli",
		AgentSourceType: "unknown",
		ProductCode:     ProductCodeEveryline,
		EvidenceType:    "none",
		Confidence:      "unknown",
		DetectorVersion: DetectorVersion,
		Reason:          "no registered client matched the process ancestry",
	}

	// Multiple products in ancestry are ambiguous; do not let signatures choose
	// a different product merely because its verification happened to succeed.
	sources := map[string]bool{}
	for _, current := range ancestorProcesses(chain) {
		for _, rule := range applicationRules {
			if matchesExecutableMarker(normalizeExecutable(current.Executable), rule.ExecutableMarkers) {
				sources[rule.Channel] = true
			}
		}
	}
	if len(sources) > 1 {
		result.EvidenceType = "conflicting_process_evidence"
		result.Reason = "multiple registered products matched ancestor executable paths"
		return result
	}

	for _, identity := range identities {
		if !identity.SignatureValid {
			continue
		}
		for _, rule := range applicationRules {
			if rule.BundleID != "" && rule.TeamID != "" && (strings.EqualFold(identity.BundleID, rule.BundleID) || matchesString(identity.BundleID, rule.BundleIDAliases)) && strings.EqualFold(identity.TeamID, rule.TeamID) {
				result.AgentSourceType = rule.Channel
				result.EvidenceType = "macos_code_signature"
				result.Confidence = "high"
				result.RuleID = rule.ID + ".signed-bundle"
				result.Reason = fmt.Sprintf("matched verified bundle id %s and team id %s", identity.BundleID, identity.TeamID)
				result.Application = copyApplicationIdentity(identity)
				result.MatchedProcess = processAtDepth(chain, identity.ProcessDepth)
				return result
			}
		}
	}

	for _, identity := range identities {
		if identity.PackageFamilyName == "" {
			continue
		}
		for _, rule := range applicationRules {
			if matchesString(identity.PackageFamilyName, rule.WindowsPackageFamilyNames) {
				result.AgentSourceType = rule.Channel
				result.EvidenceType = "windows_package_identity"
				result.Confidence = "high"
				result.RuleID = rule.ID + ".package-family"
				result.Reason = fmt.Sprintf("matched Windows package family name %s", identity.PackageFamilyName)
				result.Application = copyApplicationIdentity(identity)
				result.MatchedProcess = processAtDepth(chain, identity.ProcessDepth)
				return result
			}
		}
	}

	for _, identity := range identities {
		if !identity.SignatureValid || identity.CertificateSHA256 == "" {
			continue
		}
		executable := normalizeExecutable(identity.ExecutablePath)
		for _, rule := range applicationRules {
			if matchesExecutableMarker(executable, rule.ExecutableMarkers) &&
				matchesCertificateSHA256(identity.CertificateSHA256, rule.WindowsCertificateSHA256) {
				result.AgentSourceType = rule.Channel
				result.EvidenceType = "windows_authenticode"
				result.Confidence = "high"
				result.RuleID = rule.ID + ".authenticode"
				result.Reason = fmt.Sprintf("matched a trusted Authenticode certificate for %s", rule.Channel)
				result.Application = copyApplicationIdentity(identity)
				result.MatchedProcess = processAtDepth(chain, identity.ProcessDepth)
				return result
			}
		}
	}

	// Signatures improve attribution confidence; failed or rotated identities
	// never veto independent product path/name evidence.

	for index := 1; index < len(chain); index++ {
		current := chain[index]
		executable := normalizeExecutable(current.Executable)
		for _, rule := range applicationRules {
			if matchesExecutableMarker(executable, rule.ExecutableMarkers) {
				result.AgentSourceType = rule.Channel
				result.EvidenceType = "process_executable_path"
				result.Confidence = "medium"
				result.RuleID = rule.ID + ".executable-path"
				result.Reason = "ancestor executable matched a registered path marker"
				result.MatchedProcess = copyProcess(current)
				return result
			}
		}
	}

	for index := 1; index < len(chain); index++ {
		current := chain[index]
		name := strings.ToLower(strings.TrimSpace(current.Name))
		if name == "" {
			name = strings.ToLower(filepath.Base(current.Executable))
		}
		for _, rule := range applicationRules {
			for _, registered := range rule.ProcessNames {
				if name == strings.ToLower(registered) {
					result.AgentSourceType = rule.Channel
					result.EvidenceType = "process_name"
					result.Confidence = "low"
					result.RuleID = rule.ID + ".process-name"
					result.Reason = fmt.Sprintf("ancestor process name matched %q", registered)
					result.MatchedProcess = copyProcess(current)
					return result
				}
			}
		}
	}

	return result
}

func matchesExecutableMarker(executable string, markers []string) bool {
	candidateWithSeparator := strings.TrimSuffix(executable, "/") + "/"
	for _, marker := range markers {
		normalizedMarker := normalizeExecutable(marker)
		if strings.Contains(executable, normalizedMarker) || strings.Contains(candidateWithSeparator, normalizedMarker) {
			return true
		}
	}
	return false
}

func matchesString(value string, registered []string) bool {
	for _, candidate := range registered {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func matchesCertificateSHA256(value string, registered []string) bool {
	return matchesString(normalizeCertificateSHA256(value), registered)
}

func normalizeCertificateSHA256(value string) string {
	replacer := strings.NewReplacer(":", "", " ", "", "-", "")
	return strings.ToLower(replacer.Replace(strings.TrimSpace(value)))
}

func matchesProcessNameAtDepth(chain []Process, depth int, names []string) bool {
	current := processAtDepth(chain, depth)
	if current == nil {
		return false
	}
	name := strings.TrimSpace(current.Name)
	if name == "" {
		name = filepath.Base(current.Executable)
	}
	return matchesString(name, names)
}

func matchingWindowsRule(current Process) *applicationRule {
	executable := normalizeExecutable(current.Executable)
	name := strings.TrimSpace(current.Name)
	if name == "" {
		name = filepath.Base(current.Executable)
	}
	for index := range applicationRules {
		rule := &applicationRules[index]
		if matchesExecutableMarker(executable, rule.ExecutableMarkers) || matchesString(name, rule.ProcessNames) {
			return rule
		}
	}
	return nil
}

func normalizeExecutable(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), `\`, "/"))
}

func processAtDepth(chain []Process, depth int) *Process {
	for _, current := range chain {
		if current.Depth == depth {
			return copyProcess(current)
		}
	}
	return nil
}

func copyProcess(value Process) *Process {
	copy := value
	return &copy
}

func copyApplicationIdentity(value ApplicationIdentity) *ApplicationIdentity {
	copy := value
	return &copy
}

func ancestorProcesses(chain []Process) []Process {
	if len(chain) < 2 {
		return nil
	}
	return chain[1:]
}
