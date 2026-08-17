package app

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// StoredSession is the small blob persisted to disk between runs: the stable
// device id plus the current token pair. It is written to
// os.UserConfigDir()/WeLock/session.json with 0700 dir / 0600 file permissions.
type StoredSession struct {
	DeviceID     string `json:"deviceID"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// store persists a StoredSession as JSON on the local filesystem.
type store struct {
	path string
}

// newStore locates the session file under the user's OS config directory.
func newStore() (*store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	return &store{path: filepath.Join(dir, "WeLock", "session.json")}, nil
}

// Load reads the persisted session. A missing file is not an error: it returns a
// zero StoredSession and nil.
func (s *store) Load() (StoredSession, error) {
	var ss StoredSession
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ss, nil
		}
		return ss, err
	}
	if len(data) == 0 {
		return ss, nil
	}
	if err := json.Unmarshal(data, &ss); err != nil {
		return ss, err
	}
	return ss, nil
}

// Save writes the session atomically-ish (create dir 0700, file 0600).
func (s *store) Save(ss StoredSession) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ss, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0o600)
}

// Clear removes the persisted session. A missing file is not an error.
func (s *store) Clear() error {
	err := os.Remove(s.path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
