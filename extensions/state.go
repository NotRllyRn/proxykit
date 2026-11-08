package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrStoreBusy means a live owner may be reachable through protected control.
var ErrStoreBusy = errors.New("extension store busy")

type Decision struct {
	Commit  string `json:"commit"`
	Enabled bool   `json:"enabled"`
}
type State struct {
	Version   int                 `json:"version"`
	Revision  uint64              `json:"revision"`
	Decisions map[string]Decision `json:"decisions"`
}
type InventoryRecord struct {
	ID           string
	Installation Installation
	Diagnostic   string
}

// Inventory never executes code and preserves individual malformed records.
func (i Installer) Inventory() ([]InventoryRecord, error) {
	entries, err := os.ReadDir(i.Store)
	if errors.Is(err, os.ErrNotExist) {
		return []InventoryRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	var result []InventoryRecord
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		row := InventoryRecord{ID: entry.Name()}
		var e error
		if !safeID.MatchString(row.ID) || !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			e = errors.New("invalid installed entry")
		} else {
			p := filepath.Join(i.Store, row.ID, "installation.json")
			var b []byte
			b, e = readRegular(p, 64<<10)
			if e == nil {
				e = strict(b, &row.Installation)
			}
			r := row.Installation
			if e == nil && (r.Manifest.ID != row.ID || !commitPattern.MatchString(r.Commit)) {
				e = errors.New("invalid installed provenance")
			}
			if e == nil {
				_, e = ParseSource(r.Source)
			}
			if e == nil {
				e = r.Manifest.Validate()
			}
			if e == nil {
				_, e = readRegular(filepath.Join(i.Store, row.ID, "package", filepath.FromSlash(r.Manifest.Entry)), 64<<20)
			}
		}
		if e != nil {
			row.Diagnostic = e.Error()
		}
		result = append(result, row)
	}
	return result, nil
}
func readRegular(p string, max int64) ([]byte, error) {
	st, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New("expected bounded regular file")
	}
	return os.ReadFile(p)
}
func (i Installer) State() (State, error) {
	s := State{Version: 1, Decisions: map[string]Decision{}}
	b, e := readRegular(filepath.Join(i.Store, ".enabled.json"), 1<<20)
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if e = strict(b, &s); e != nil {
		return s, e
	}
	if s.Version != 1 || s.Decisions == nil {
		return s, errors.New("invalid enabled state")
	}
	return s, nil
}

// SaveState must be called under the owner lock. Rename is the commit point.
// Sync the temporary file before rename; no fallible work follows publication.
func (i Installer) SaveState(s State) error {
	b, e := json.Marshal(s)
	if e != nil {
		return e
	}
	if len(b) > 1<<20 {
		return errors.New("enabled state exceeds 1 MiB bound")
	}
	f, e := os.CreateTemp(i.Store, ".enabled-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	return os.Rename(name, filepath.Join(i.Store, ".enabled.json"))
}
func (i Installer) Enabled(s State) ([]Installation, error) {
	rows, e := i.Inventory()
	if e != nil {
		return nil, e
	}
	var enabled []Installation
	for _, row := range rows {
		d, ok := s.Decisions[row.ID]
		if !ok || d.Commit != row.Installation.Commit {
			return nil, fmt.Errorf("%s: unresolved legacy enabled decision; explicitly disable or reinstall", row.ID)
		}
		if !d.Enabled {
			continue
		}
		if row.Diagnostic != "" {
			return nil, fmt.Errorf("%s: %s", row.ID, row.Diagnostic)
		}
		enabled = append(enabled, row.Installation)
	}
	return enabled, nil
}
func (i Installer) Decide(s State, id string, enabled bool) (State, error) {
	rows, e := i.Inventory()
	if e != nil {
		return s, e
	}
	for _, row := range rows {
		if row.ID == id {
			if enabled && row.Diagnostic != "" {
				return s, fmt.Errorf("%s: %s", id, row.Diagnostic)
			}
			copyState := State{Version: 1, Revision: s.Revision + 1, Decisions: map[string]Decision{}}
			for k, v := range s.Decisions {
				copyState.Decisions[k] = v
			}
			copyState.Decisions[id] = Decision{row.Installation.Commit, enabled}
			return copyState, nil
		}
	}
	return s, fmt.Errorf("extension %s not installed", id)
}
