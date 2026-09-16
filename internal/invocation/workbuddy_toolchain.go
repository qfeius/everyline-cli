package invocation

import (
	"regexp"
	"strings"
)

// Match only a running ancestor in WorkBuddy's bundled Git distribution.
// Version directories are deliberately unconstrained; this is attribution,
// not verification of the shell or of the installed application.
var workbuddyGitAncestor = regexp.MustCompile(`^[a-z]:/(?:[^/]+/)*\.workbuddy(?:-ai)?/binaries/portablegit/versions/[^/]+/(?:usr/)?bin/(?:sh|bash)\.exe$`)

func windowsWorkBuddyToolchainFallback(result Result) Result {
	// Preserve every existing match, including stronger identities, and do not
	// override a conflict reported by the existing rules.
	if result.Platform != "windows" || result.AgentSourceType != "unknown" || result.EvidenceType != "none" {
		return result
	}
	for _, warning := range result.Warnings {
		if strings.HasPrefix(warning, "conflicting ") {
			return result
		}
	}
	for _, current := range result.Processes {
		if current.Depth <= 0 {
			continue
		}
		executable := normalizeExecutable(current.Executable)
		if !workbuddyGitAncestor.MatchString(executable) {
			continue
		}
		clean := true
		for _, part := range strings.Split(executable, "/") {
			if part == "." || part == ".." {
				clean = false
				break
			}
		}
		if !clean {
			continue
		}
		result.AgentSourceType = "workbuddy"
		result.EvidenceType = "process_executable_path"
		result.Confidence = "low"
		result.RuleID = "client.workbuddy.bundled-git-ancestor"
		result.Reason = "matched a running ancestor in the WorkBuddy bundled Git runtime; main client ancestry unavailable"
		result.MatchedProcess = copyProcess(current)
		return result
	}
	return result
}
