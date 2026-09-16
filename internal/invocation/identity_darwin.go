//go:build darwin

package invocation

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func platformApplicationIdentities(chain []Process) ([]ApplicationIdentity, []string) {
	if len(chain) < 2 {
		return nil, nil
	}

	identities := make([]ApplicationIdentity, 0)
	warnings := make([]string, 0)
	seen := make(map[string]struct{})

	for _, current := range chain[1:] {
		for _, bundlePath := range enclosingAppBundles(current.Executable) {
			key := strings.ToLower(bundlePath)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			identity, err := inspectSignedBundle(bundlePath, current.Depth)
			if err != nil {
				warnings = append(warnings, err.Error())
			}
			if identity.BundlePath != "" {
				identities = append(identities, identity)
			}
		}
	}
	return identities, warnings
}

func enclosingAppBundles(executable string) []string {
	cleaned := filepath.Clean(strings.TrimSpace(executable))
	if cleaned == "." || cleaned == "" {
		return nil
	}

	lower := strings.ToLower(cleaned)
	bundles := make([]string, 0, 2)
	for offset := 0; offset < len(lower); {
		index := strings.Index(lower[offset:], ".app")
		if index < 0 {
			break
		}
		end := offset + index + len(".app")
		if end == len(lower) || lower[end] == filepath.Separator {
			bundles = append(bundles, cleaned[:end])
		}
		offset = end
	}
	return bundles
}

func inspectSignedBundle(bundlePath string, processDepth int) (ApplicationIdentity, error) {
	// Host attribution verifies signed code identity, not mutable resources:
	// WorkBuddy's Python shim may create __pycache__ inside the signed bundle.
	// Keep code verification and Analyze's bundle/team match; -d alone is not
	// signature verification.
	verify := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--ignore-resources", "--verbose=2", bundlePath)
	if output, err := verify.CombinedOutput(); err != nil {
		return ApplicationIdentity{
			ProcessDepth: processDepth,
			BundlePath:   bundlePath,
		}, fmt.Errorf("cannot verify application signature for %s: %s", bundlePath, compactCommandError(err, output))
	}

	details := exec.Command("/usr/bin/codesign", "-d", "--verbose=4", bundlePath)
	output, err := details.CombinedOutput()
	if err != nil {
		return ApplicationIdentity{
			ProcessDepth:   processDepth,
			BundlePath:     bundlePath,
			SignatureValid: true,
		}, fmt.Errorf("cannot read application signature for %s: %s", bundlePath, compactCommandError(err, output))
	}

	fields := parseCodesignDetails(output)
	identity := ApplicationIdentity{
		ProcessDepth:   processDepth,
		BundlePath:     bundlePath,
		BundleID:       fields["Identifier"],
		TeamID:         fields["TeamIdentifier"],
		SignatureValid: true,
	}
	identity.Version = readBundleVersion(bundlePath)
	return identity, nil
}

func parseCodesignDetails(output []byte) map[string]string {
	fields := make(map[string]string)
	for _, line := range bytes.Split(output, []byte{'\n'}) {
		key, value, found := bytes.Cut(line, []byte{'='})
		if !found {
			continue
		}
		fields[strings.TrimSpace(string(key))] = strings.TrimSpace(string(value))
	}
	return fields
}

func readBundleVersion(bundlePath string) string {
	infoPath := filepath.Join(bundlePath, "Contents", "Info.plist")
	command := exec.Command("/usr/bin/plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", infoPath)
	output, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func compactCommandError(err error, output []byte) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	if len(message) > 240 {
		message = message[:240] + "..."
	}
	return message
}
