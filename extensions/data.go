package extensions

import (
	"errors"
	"os"
	"path/filepath"
)

// DataDir creates or validates the persistent private directory for a manifest ID.
// Installation and activation call this before reporting success or running setup.
// Removal deletes packages, never this directory.
// As with Lock, the store's ancestors must be trusted against replacement.
func (i Installer) DataDir(id string) (string, error) {
	if !safeID.MatchString(id) {
		return "", errors.New("invalid extension id")
	}
	unlock, err := i.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	store, err := filepath.Abs(i.Store)
	if err != nil {
		return "", err
	}
	for _, dir := range []string{store, filepath.Join(store, ".data"), filepath.Join(store, ".data", id)} {
		if err = os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return "", err
		}
		if err = privateDataDir(dir); err != nil {
			return "", err
		}
	}
	return filepath.Join(store, ".data", id), nil
}
