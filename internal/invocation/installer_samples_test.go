package invocation

import (
	"encoding/json"
	"os"
	"testing"
)

// These are static package observations with MODELED install paths, not live
// ancestry or native signature verification. Both CLIs replay the same corpus.
func TestOfficialInstallerSampleAttribution(t *testing.T) {
	var samples []struct {
		Sample     string `json:"sample"`
		Expected   string `json:"expected_source"`
		Executable string `json:"executable_path"`
		Name       string `json:"process_name"`
		Bundle     string `json:"bundle_path"`
		BundleID   string `json:"bundle_id"`
		TeamID     string `json:"team_id"`
	}
	data, err := os.ReadFile("testdata/installer-samples-20260914.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &samples); err != nil {
		t.Fatal(err)
	}
	for _, s := range samples {
		t.Run(s.Sample, func(t *testing.T) {
			chain := []Process{{Depth: 0, Name: "cli"}, {Depth: 1, Name: s.Name, Executable: s.Executable}}
			got := Analyze(chain, nil)
			if got.AgentSourceType != s.Expected || got.Confidence != "medium" {
				t.Fatalf("path fallback: %+v", got)
			}
			if s.Bundle != "" {
				identity := ApplicationIdentity{ProcessDepth: 1, BundlePath: s.Bundle, BundleID: s.BundleID, TeamID: s.TeamID, SignatureValid: true}
				got = Analyze(chain, []ApplicationIdentity{identity})
				if got.AgentSourceType != s.Expected || got.Confidence != "high" {
					t.Fatalf("conditional verified identity: %+v", got)
				}
				identity.BundleID = "future.unknown.identity"
				identity.SignatureValid = false
				got = Analyze(chain, []ApplicationIdentity{identity})
				if got.AgentSourceType != s.Expected || got.Confidence != "medium" {
					t.Fatalf("identity rotation fallback: %+v", got)
				}
			}
		})
	}
}
