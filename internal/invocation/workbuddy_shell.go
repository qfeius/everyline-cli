package invocation

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// workbuddyShellEnvironment checks a normalized shell entrypoint path.
func workbuddyShellEnvironment(path string) bool {
	if path == "" {
		return false
	}
	shim := filepath.Dir(path)
	safeDelete := strings.EqualFold(filepath.Base(path), "safe-delete-bash-env.sh") && strings.EqualFold(filepath.Base(shim), "safe-bin")
	if safeDelete {
		shim = filepath.Dir(shim)
	} else if !strings.EqualFold(filepath.Base(path), "shell-runtime-bash-env.sh") {
		return false
	}
	vendor := filepath.Dir(shim)
	if !strings.EqualFold(filepath.Base(shim), "shim") || !strings.EqualFold(filepath.Base(vendor), "vendor") {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	// The observed safe-delete entrypoint lives in the installed WorkBuddy
	// package. Its full product path supplies low-confidence attribution even
	// when the broker does not expose the legacy product.json metadata.
	if safeDelete {
		cli := filepath.Dir(vendor)
		unpacked := filepath.Dir(cli)
		resources := filepath.Dir(unpacked)
		productDir := strings.ToLower(filepath.Base(filepath.Dir(resources)))
		if strings.EqualFold(filepath.Base(cli), "cli") &&
			strings.EqualFold(filepath.Base(unpacked), "app.asar.unpacked") &&
			strings.EqualFold(filepath.Base(resources), "resources") &&
			(productDir == "workbuddy" || productDir == "workbuddyai") {
			return true
		}
	}
	file, err := os.Open(filepath.Join(filepath.Dir(vendor), "product.json"))
	if err != nil {
		return false
	}
	defer file.Close()
	// The installed product file includes extensive UI config (~384 KiB).
	// Bound the read while allowing that real runtime metadata.
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return false
	}
	var product struct {
		ProductName    string `json:"productName"`
		Authentication struct {
			ID string `json:"id"`
		} `json:"authentication"`
	}
	if json.Unmarshal(data, &product) != nil {
		return false
	}
	return product.ProductName == "WorkBuddy" && product.Authentication.ID == "workbuddy-desktop"
}
