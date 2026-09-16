package invocation

import (
	"reflect"
	"testing"
)

func TestWorkBuddyBundledGitBrokenAncestry(t *testing.T) {
	// Captured v6 samples: Node survives but sh's parent PID has exited.
	for _, sample := range []struct{ node, shell, missing int32 }{
		{11228, 17768, 12836}, {3524, 1236, 18292},
	} {
		chain := []Process{
			{Depth: 0, PID: sample.node, PPID: sample.shell, Name: "node.exe", Executable: `C:\Users\sample\.workbuddy\binaries\node\versions\22.22.2-3\node.exe`},
			{Depth: 1, PID: sample.shell, PPID: sample.missing, Name: "sh.exe", Executable: `C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\1.2.0\usr\bin\sh.exe`},
		}
		before := Analyze(chain, nil)
		if before.AgentSourceType != "unknown" {
			t.Fatalf("sample no longer exercises fallback: %+v", before)
		}
		before.Platform, before.Processes = "windows", chain
		before.Warnings = []string{"ancestor process does not exist"}
		got := windowsWorkBuddyToolchainFallback(before)
		if got.AgentSourceType != "workbuddy" || got.Confidence != "low" || got.RuleID != "client.workbuddy.bundled-git-ancestor" || got.MatchedProcess.PID != sample.shell {
			t.Fatalf("broken chain: %+v", got)
		}
		if !reflect.DeepEqual(got.Warnings, before.Warnings) {
			t.Fatal("lost diagnostic")
		}
	}
}

func TestWorkBuddyBundledGitPathBoundaries(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{`C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\1.2.0\usr\bin\sh.exe`, true},
		{`D:\用户 空格\.workbuddy\binaries\PortableGit\versions\future-version\bin\bash.exe`, true},
		{`C:/Users/sample/.WORKBUDDY-AI/binaries/PORTABLEGIT/versions/2.0/usr/bin/BASH.EXE`, true},
		{`C:\Program Files\Git\usr\bin\sh.exe`, false},
		{`C:\Users\sample\.workbuddy-ai\binaries\node\versions\22\node.exe`, false},
		{`C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\1\usr\bin\sh.exe.fake`, false},
		{`C:\Users\sample\.workbuddy-ai-fake\binaries\PortableGit\versions\1\usr\bin\sh.exe`, false},
		{`C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\..\usr\bin\sh.exe`, false},
		{`C:\Users\..\.workbuddy-ai\binaries\PortableGit\versions\1\usr\bin\sh.exe`, false},
		{`C:\Users\sample\.workbuddy-ai\other\PortableGit\versions\1\usr\bin\sh.exe`, false},
		{`.workbuddy-ai/binaries/PortableGit/versions/1/usr/bin/sh.exe`, false},
		{`/home/sample/.workbuddy-ai/binaries/PortableGit/versions/1/usr/bin/sh.exe`, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			r := Analyze(nil, nil)
			r.Platform = "windows"
			r.Processes = []Process{{Depth: 1, Executable: tc.path}}
			got := windowsWorkBuddyToolchainFallback(r)
			if (got.AgentSourceType == "workbuddy") != tc.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestWorkBuddyBundledGitPreservesExistingResults(t *testing.T) {
	path := `C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\1.2.0\usr\bin\sh.exe`
	for _, platform := range []string{"windows", "darwin", "linux"} {
		for _, source := range []string{"unknown", "codex", "doubao", "doubaoWork", "doubaoWorkmates", "workbuddy"} {
			for _, evidence := range []string{"none", "windows_authenticode", "conflicting_process_evidence"} {
				r := Analyze(nil, nil)
				r.Platform = platform
				r.AgentSourceType = source
				r.EvidenceType = evidence
				r.Processes = []Process{{Depth: 1, Executable: path}}
				if platform == "windows" && source == "unknown" && evidence == "none" {
					continue
				}
				if got := windowsWorkBuddyToolchainFallback(r); !reflect.DeepEqual(got, r) {
					t.Fatalf("modified existing result: %+v => %+v", r, got)
				}
			}
		}
	}
	r := Analyze(nil, nil)
	r.Platform = "windows"
	r.Processes = []Process{{Depth: 0, Executable: path}}
	if got := windowsWorkBuddyToolchainFallback(r); !reflect.DeepEqual(got, r) {
		t.Fatal("current executable is not ancestor evidence")
	}
}

func TestWorkBuddyBundledGitPreservesRuntimeConflict(t *testing.T) {
	r := Analyze(nil, nil)
	r.Platform = "windows"
	r.Processes = []Process{{Depth: 1, Executable: `C:\Users\sample\.workbuddy-ai\binaries\PortableGit\versions\1\usr\bin\sh.exe`}}
	for _, warning := range []string{"conflicting Windows runtime environment markers", "conflicting runtime product markers"} {
		r.Warnings = []string{warning}
		if got := windowsWorkBuddyToolchainFallback(r); !reflect.DeepEqual(got, r) {
			t.Fatalf("overrode runtime conflict: %+v", got)
		}
	}
}
