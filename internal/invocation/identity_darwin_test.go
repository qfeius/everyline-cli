//go:build darwin

package invocation

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise real codesign resource semantics, not mocked command arguments.
func TestInspectSignedBundleResourceAndCodeChanges(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "WorkBuddy.app")
	executable := filepath.Join(bundle, "Contents", "MacOS", "Host")
	resources := filepath.Join(bundle, "Contents", "Resources")
	for _, dir := range []string{filepath.Dir(executable), resources} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte, mode os.FileMode) {
		t.Helper()
		if err := os.WriteFile(path, data, mode); err != nil {
			t.Fatal(err)
		}
	}
	code, err := os.ReadFile("/bin/echo")
	if err != nil {
		t.Fatal(err)
	}
	write(executable, code, 0755)
	write(filepath.Join(bundle, "Contents", "Info.plist"), []byte(`<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0"><dict>
<key>CFBundleExecutable</key><string>Host</string>
<key>CFBundleIdentifier</key><string>com.workbuddy.workbuddy</string>
<key>CFBundlePackageType</key><string>APPL</string>
</dict></plist>`), 0644)
	write(filepath.Join(resources, "original.txt"), []byte("signed resource"), 0644)
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "com.workbuddy.workbuddy", bundle).CombinedOutput(); err != nil {
		t.Fatalf("sign fixture: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", bundle).CombinedOutput(); err != nil {
		t.Fatalf("verify original fixture: %v: %s", err, output)
	}
	cache := filepath.Join(resources, "__pycache__")
	if err := os.MkdirAll(cache, 0755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(cache, "sitecustomize.cpython-313.pyc"), []byte("runtime cache"), 0644)
	if err := exec.Command("/usr/bin/codesign", "--verify", "--strict", bundle).Run(); err == nil {
		t.Fatal("full resource validation unexpectedly accepted added cache")
	}
	identity, err := inspectSignedBundle(bundle, 1)
	if err != nil || !identity.SignatureValid || identity.BundleID != "com.workbuddy.workbuddy" {
		t.Fatalf("resource change rejected: identity=%+v err=%v", identity, err)
	}
	// An ad-hoc signer must not impersonate the registered WorkBuddy team.
	if result := Analyze(nil, []ApplicationIdentity{identity}); result.AgentSourceType != "unknown" {
		t.Fatalf("unregistered signer accepted: %+v", result)
	}
	code, err = os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	code[len(code)/2] ^= 0xff
	write(executable, code, 0755)
	identity, err = inspectSignedBundle(bundle, 1)
	if err == nil || identity.SignatureValid {
		t.Fatalf("modified code accepted: identity=%+v err=%v", identity, err)
	}
}

func TestEnclosingAppBundles(t *testing.T) {
	got := enclosingAppBundles("/Applications/Doubao.app/Contents/Helpers/Doubao Browser.app/Contents/MacOS/Doubao Browser")
	want := []string{"/Applications/Doubao.app", "/Applications/Doubao.app/Contents/Helpers/Doubao Browser.app"}
	if len(got) != len(want) {
		t.Fatalf("enclosingAppBundles() = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("enclosingAppBundles()[%d] = %q, want %q", index, got[index], want[index])
		}
	}
}

func TestParseCodesignDetails(t *testing.T) {
	fields := parseCodesignDetails([]byte("Executable=/Applications/ChatGPT.app/Contents/MacOS/ChatGPT\nIdentifier=com.openai.codex\nTeamIdentifier=2DC432GLL2\n"))
	if fields["Identifier"] != "com.openai.codex" || fields["TeamIdentifier"] != "2DC432GLL2" {
		t.Fatalf("parseCodesignDetails() = %#v", fields)
	}
}

func TestPlatformApplicationIdentitiesHandlesEmptyChain(t *testing.T) {
	identities, warnings := platformApplicationIdentities(nil)
	if len(identities) != 0 || len(warnings) != 0 {
		t.Fatalf("identities = %#v, warnings = %#v", identities, warnings)
	}
}
