package invocation

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// Called only in the disposable inspection helper. Start at the original CLI,
// not the helper, so adding process isolation does not change identity matching.
func inspectProcess(pid int32, maxDepth int) Result {
	return inspectProcessWithProgress(pid, maxDepth, nil)
}

// Publish cheap evidence before potentially blocking native signature checks.
func inspectProcessWithProgress(pid int32, maxDepth int, publish func(Result)) Result {
	chain, warnings := ancestryFromPID(pid, maxDepth)
	preliminary := completeProcessResult(chain, nil, warnings)
	if publish != nil {
		publish(preliminary)
	}
	identities, identityWarnings := platformApplicationIdentities(chain)
	return completeProcessResult(chain, identities, append(warnings, identityWarnings...))
}

func completeProcessResult(chain []Process, identities []ApplicationIdentity, warnings []string) Result {
	result := Analyze(chain, identities)
	result.Platform = runtime.GOOS
	result.Processes = chain
	result.Warnings = append(result.Warnings, warnings...)
	result = platformEnvironmentFallback(result)
	result = runtimeEnvironmentFallback(result, identities, func(key string) bool {
		value, exists := os.LookupEnv(key)
		return exists && strings.TrimSpace(value) != ""
	}, os.Getenv)
	return windowsWorkBuddyToolchainFallback(result)
}

func Ancestry(maxDepth int) ([]Process, []string) {
	return ancestryFromPID(int32(os.Getpid()), maxDepth)
}

func ancestryFromPID(pid int32, maxDepth int) ([]Process, []string) {
	if maxDepth <= 0 {
		maxDepth = DefaultMaxDepth
	}
	if maxDepth > 128 {
		maxDepth = 128
	}

	chain := make([]Process, 0, maxDepth)
	warnings := make([]string, 0)
	seen := make(map[int32]struct{}, maxDepth)

	for pid > 0 && len(chain) < maxDepth {
		if _, exists := seen[pid]; exists {
			warnings = append(warnings, fmt.Sprintf("process ancestry loop detected at pid %d", pid))
			break
		}
		seen[pid] = struct{}{}

		current, err := process.NewProcess(pid)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot inspect pid %d: %v", pid, err))
			break
		}

		name, _ := current.Name()
		executable, _ := current.Exe()
		ppid, err := current.Ppid()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("cannot read parent pid for pid %d: %v", pid, err))
			ppid = 0
		}

		chain = append(chain, Process{
			Depth:      len(chain),
			PID:        pid,
			PPID:       ppid,
			Name:       strings.TrimSpace(name),
			Executable: strings.TrimSpace(executable),
		})
		pid = ppid
	}

	if len(chain) == maxDepth && chain[len(chain)-1].PPID > 0 {
		warnings = append(warnings, fmt.Sprintf("process ancestry truncated at depth %d", maxDepth))
	}
	return chain, warnings
}
