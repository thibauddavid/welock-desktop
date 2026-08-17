package sidecar

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// locate resolves the helper binary to spawn. WELOCK_SIDECAR (a path to a prebuilt
// helper) wins — used in development and by the screenshot harness. Otherwise the
// embedded, opaque binary (binary/binName, defined per-platform in embed_*.go) is
// extracted once to the user cache and reused.
func locate() (string, error) {
	if p := os.Getenv("WELOCK_SIDECAR"); p != "" {
		return p, nil
	}
	return extractEmbedded()
}

// extractEmbedded writes the embedded helper to <UserCacheDir>/WeLock/bin/<name>-<sha>
// with exec permission and returns its path, skipping the write if an identical copy is
// already there (the sha in the name makes a stale binary self-invalidate on upgrade).
// An app writing its own file does not set macOS quarantine, so the helper runs on the
// current unsigned build; a future notarized build should instead ship it inside the
// .app bundle and sign it.
func extractEmbedded() (string, error) {
	if len(binary) == 0 {
		return "", errors.New("sidecar: no embedded helper in this build; set WELOCK_SIDECAR to a helper path")
	}
	sum := sha256.Sum256(binary)
	tag := hex.EncodeToString(sum[:6])

	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "WeLock", "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, binName+"-"+tag)
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(binary)) {
		return path, nil // already extracted
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, binary, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp, 0o700); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}
