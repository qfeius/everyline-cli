//go:build !darwin && !windows

package invocation

func platformApplicationIdentities([]Process) ([]ApplicationIdentity, []string) {
	return nil, nil
}
