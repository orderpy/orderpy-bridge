package updater

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const triggerDirDefault = "/run/orderpy-bridge"

// HandleUpdateAction validates payload and writes update-requested.json (same contract as Python bridge).
func HandleUpdateAction(payload map[string]any) (accepted bool, reason string) {
	allowedURL := regexp.MustCompile(`^https://github\.com/orderpy/orderpy-bridge-go/releases/download/v\d+\.\d+\.\d+/[A-Za-z0-9._-]+$`)
	verRe := regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+][A-Za-z0-9.-]+)?$`)
	shaRe := regexp.MustCompile(`^[A-Fa-f0-9]{64}$`)

	target, _ := payload["target_version"].(string)
	if !verRe.MatchString(target) {
		return false, "invalid target_version"
	}
	tarball, _ := payload["tarball_url"].(string)
	minisig, _ := payload["minisig_url"].(string)
	sha, _ := payload["tarball_sha256"].(string)
	if !allowedURL.MatchString(tarball) || !allowedURL.MatchString(minisig) || !shaRe.MatchString(sha) {
		return false, "url_or_sha_invalid"
	}
	dir := os.Getenv("ORDERPY_BRIDGE_RUNTIME_DIR")
	if dir == "" {
		dir = triggerDirDefault
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, "trigger_dir_unavailable"
	}
	body := map[string]any{
		"target_version":   target,
		"tarball_url":      tarball,
		"minisig_url":      minisig,
		"tarball_sha256":   sha,
		"requested_at":     payload["requested_at"],
		"requested_by":     payload["requested_by"],
	}
	if body["requested_by"] == nil {
		body["requested_by"] = "cloud"
	}
	b, err := json.Marshal(body)
	if err != nil {
		return false, "marshal"
	}
	path := filepath.Join(dir, "update-requested.json")
	tmp := filepath.Join(dir, ".update-requested.tmp")
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return false, "trigger_write_failed"
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, "trigger_write_failed"
	}
	return true, ""
}

// ConsumePendingUpdateResult reads and removes update-result.json if present.
func ConsumePendingUpdateResult() map[string]any {
	dir := os.Getenv("ORDERPY_BRIDGE_RUNTIME_DIR")
	if dir == "" {
		dir = triggerDirDefault
	}
	path := filepath.Join(dir, "update-result.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = os.Remove(path)
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// CheckWatchdog synthesizes failed result if trigger is stale (simplified).
func CheckWatchdog(timeoutSec int) bool {
	if timeoutSec <= 0 {
		timeoutSec = 900
	}
	dir := os.Getenv("ORDERPY_BRIDGE_RUNTIME_DIR")
	if dir == "" {
		dir = triggerDirDefault
	}
	tpath := filepath.Join(dir, "update-requested.json")
	rpath := filepath.Join(dir, "update-result.json")
	st, err := os.Stat(tpath)
	if err != nil {
		return false
	}
	if _, err := os.Stat(rpath); err == nil {
		return false
	}
	if time.Since(st.ModTime()) < time.Duration(timeoutSec)*time.Second {
		return false
	}
	body := map[string]any{
		"status":          "failed",
		"target_version": "",
		"error":         "updater_did_not_complete_in_time",
	}
	b, _ := json.Marshal(body)
	_ = os.WriteFile(rpath, b, 0o644)
	_ = os.Remove(tpath)
	return true
}
