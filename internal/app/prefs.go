package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// prefs holds device-local preferences that must SURVIVE logout — unlike StoredSession
// (session.json), which Clear() wipes on sign-out. It lives beside the session file at
// os.UserConfigDir()/WeLock/prefs.json. The install id is a stable anonymous analytics
// identity; the disabled flag is the analytics opt-out.
type prefs struct {
	InstallID         string `json:"installID"`
	AnalyticsDisabled bool   `json:"analyticsDisabled"`
}

// prefsStore persists prefs as JSON on the local filesystem.
type prefsStore struct {
	path string
}

// newPrefsStore locates prefs.json under the user's OS config directory.
func newPrefsStore() (*prefsStore, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &prefsStore{path: filepath.Join(dir, "WeLock", "prefs.json")}, nil
}

// Load reads the prefs. A missing file is not an error: it returns zero prefs and nil.
func (p *prefsStore) Load() (prefs, error) {
	var pr prefs
	data, err := os.ReadFile(p.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return pr, nil
		}
		return pr, err
	}
	if len(data) == 0 {
		return pr, nil
	}
	if err := json.Unmarshal(data, &pr); err != nil {
		return pr, err
	}
	return pr, nil
}

// Save writes the prefs (create dir 0700, file 0600).
func (p *prefsStore) Save(pr prefs) error {
	if err := os.MkdirAll(filepath.Dir(p.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pr, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p.path, data, 0o600)
}

// newInstallID returns a random 128-bit hex id — an anonymous analytics identity with NO
// link to the WeLock account or the session device id. Empty on the (unreachable) rand
// failure, which callers treat as "no analytics identity".
func newInstallID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
