// Package extensions installs prebuilt trusted ESM packages without executing them.
package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var safeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var version = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type Manifest struct {
	ManifestVersion int    `json:"manifestVersion"`
	APIVersion      int    `json:"apiVersion"`
	ID              string `json:"id"`
	Version         string `json:"version"`
	Entry           string `json:"entry"`
}
type Installation struct {
	Manifest Manifest `json:"manifest"`
	Source   string   `json:"source"`
	Commit   string   `json:"commit"`
}
type Source struct{ Owner, Repository, Revision string }

func ParseSource(raw string) (Source, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return Source{}, errors.New("expected https://github.com/owner/repository[/tree/revision]; revision must be one unescaped URL segment")
	}
	p := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(p) != 3 && len(p) != 5 {
		return Source{}, errors.New("GitHub revision must be one URL segment")
	}
	if !segment.MatchString(p[1]) || !segment.MatchString(p[2]) || p[1] == "." || p[2] == "." || strings.Contains(p[1], "..") || strings.Contains(p[2], "..") {
		return Source{}, errors.New("invalid GitHub owner/repository")
	}
	s := Source{Owner: p[1], Repository: p[2], Revision: "HEAD"}
	if len(p) == 5 {
		if p[3] != "tree" || !segment.MatchString(p[4]) || strings.Contains(p[4], "..") {
			return Source{}, errors.New("revision must be a safe single URL segment")
		}
		s.Revision = p[4]
	}
	return s, nil
}
func safePath(s string) bool {
	if s == "" || len(s) > 4096 || !utf8.ValidString(s) || strings.ContainsAny(s, "\\\x00:") || strings.HasPrefix(s, "/") || path.Clean(s) != s {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if p == "." || p == ".." || p == "" {
			return false
		}
	}
	return true
}
func (m Manifest) Validate() error {
	if m.APIVersion != 1 {
		return fmt.Errorf("extension apiVersion %d unsupported or missing; this host requires apiVersion: 1", m.APIVersion)
	}
	if m.ManifestVersion != 1 || !safeID.MatchString(m.ID) || !validVersion(m.Version) || !safePath(m.Entry) || !strings.HasSuffix(m.Entry, ".mjs") {
		return errors.New("invalid v1 manifest: require safe id, semantic version and relative .mjs entry")
	}
	return nil
}
func validVersion(s string) bool {
	if len(s) > 128 || !version.MatchString(s) {
		return false
	}
	coreAndPre := strings.SplitN(strings.SplitN(s, "+", 2)[0], "-", 2)
	for _, p := range strings.Split(coreAndPre[0], ".") {
		if len(p) > 1 && p[0] == '0' {
			return false
		}
	}
	if len(coreAndPre) == 2 {
		for _, p := range strings.Split(coreAndPre[1], ".") {
			if p == "" {
				return false
			}
			numeric := true
			for _, c := range p {
				if c < '0' || c > '9' {
					numeric = false
				}
			}
			if numeric && len(p) > 1 && p[0] == '0' {
				return false
			}
		}
	}
	if p := strings.SplitN(s, "+", 2); len(p) == 2 {
		for _, part := range strings.Split(p[1], ".") {
			if part == "" {
				return false
			}
		}
	}
	return true
}
func strict(data []byte, v any) error {
	if !utf8.Valid(data) {
		return errors.New("invalid UTF-8 JSON")
	}
	// encoding/json accepts duplicate keys; reject them even in nested objects.
	d := json.NewDecoder(strings.NewReader(string(data)))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s := k.(string)
					if seen[s] {
						return fmt.Errorf("duplicate JSON field %q", s)
					}
					seen[s] = true
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			case '[':
				for d.More() {
					if e = walk(); e != nil {
						return e
					}
				}
				_, e = d.Token()
				return e
			default:
				return errors.New("invalid JSON delimiter")
			}
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	d = json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// Installer endpoints may be overridden for private/local fixture servers. Defaults use GitHub HTTPS.
// Store must be host-owned and must not be modified concurrently except through this API.
type Installer struct {
	Store                     string
	Owned                     bool // caller holds the store owner lock for the whole operation
	Client                    *http.Client
	APIBase, ArchiveBase      string
	MaxDownload, MaxExtracted int64
	MaxFiles                  int
}

func (i Installer) defaults() Installer {
	if i.Client == nil {
		i.Client = &http.Client{Timeout: 60 * time.Second}
	}
	if i.APIBase == "" {
		i.APIBase = "https://api.github.com"
	}
	if i.ArchiveBase == "" {
		i.ArchiveBase = "https://codeload.github.com"
	}
	if i.MaxDownload <= 0 {
		i.MaxDownload = 16 << 20
	}
	if i.MaxExtracted <= 0 {
		i.MaxExtracted = 64 << 20
	}
	if i.MaxFiles <= 0 {
		i.MaxFiles = 2048
	}
	return i
}
func (i Installer) get(ctx context.Context, endpoint string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := i.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err == nil && int64(len(b)) > max {
		err = errors.New("download exceeds bound")
	}
	return b, err
}
func (i Installer) lock() (func(), error) {
	if i.Owned {
		return func() {}, nil
	}
	return Lock(i.Store)
}

func validateStore(store string) error {
	if store == "" {
		return errors.New("extension store required")
	}
	if err := os.MkdirAll(store, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(store)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("store must be a real directory")
	}
	return nil
}
func (i Installer) Install(ctx context.Context, source string, trust bool) (Installation, error) {
	if !trust {
		return Installation{}, errors.New("installation requires --trust: extensions execute arbitrary code as the server identity; not sandboxed")
	}
	s, err := ParseSource(source)
	if err != nil {
		return Installation{}, err
	}
	i = i.defaults()
	unlock, err := i.lock()
	if err != nil {
		return Installation{}, err
	}
	defer unlock()
	b, err := i.get(ctx, fmt.Sprintf("%s/repos/%s/%s/commits/%s", i.APIBase, s.Owner, s.Repository, s.Revision), 1<<20)
	if err != nil {
		return Installation{}, err
	}
	var resolved struct {
		SHA string `json:"sha"`
	}
	if err = json.Unmarshal(b, &resolved); err != nil || !commitPattern.MatchString(resolved.SHA) {
		return Installation{}, errors.New("GitHub did not resolve an immutable 40-hex commit")
	}
	sha := strings.ToLower(resolved.SHA)
	archive, err := i.get(ctx, fmt.Sprintf("%s/%s/%s/tar.gz/%s", i.ArchiveBase, s.Owner, s.Repository, sha), i.MaxDownload)
	if err != nil {
		return Installation{}, err
	}
	stage, err := os.MkdirTemp(i.Store, ".stage-")
	if err != nil {
		return Installation{}, err
	}
	defer os.RemoveAll(stage)
	pkg := filepath.Join(stage, "package")
	if err = os.Mkdir(pkg, 0700); err != nil {
		return Installation{}, err
	}
	if err = i.extract(ctx, archive, pkg); err != nil {
		return Installation{}, err
	}
	manifestPath := filepath.Join(pkg, "proxykit-extension.json")
	manifestStat, err := os.Lstat(manifestPath)
	if err != nil {
		return Installation{}, err
	}
	if !manifestStat.Mode().IsRegular() || manifestStat.Size() > 64<<10 {
		return Installation{}, errors.New("manifest must be a regular file of at most 64 KiB")
	}
	b, err = os.ReadFile(manifestPath)
	if err != nil {
		return Installation{}, err
	}
	if len(b) > 64<<10 {
		return Installation{}, errors.New("manifest too large")
	}
	var m Manifest
	if err = strict(b, &m); err != nil {
		return Installation{}, err
	}
	if err = m.Validate(); err != nil {
		return Installation{}, err
	}
	st, err := os.Lstat(filepath.Join(pkg, filepath.FromSlash(m.Entry)))
	if err != nil || !st.Mode().IsRegular() {
		return Installation{}, errors.New("entry is not a regular file")
	}
	result := Installation{Manifest: m, Source: source, Commit: sha}
	b, _ = json.MarshalIndent(result, "", "  ")
	if err = os.WriteFile(filepath.Join(stage, "installation.json"), b, 0600); err != nil {
		return Installation{}, err
	}
	destination := filepath.Join(i.Store, m.ID)
	if _, err = os.Lstat(destination); err == nil {
		return Installation{}, errors.New("already installed; remove before reinstall (updates are not supported)")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Installation{}, err
	}
	if err = ctx.Err(); err != nil {
		return Installation{}, err
	}
	if err = os.Rename(stage, destination); err != nil {
		return Installation{}, err
	}
	state, err := i.State()
	if err == nil {
		state.Revision++
		state.Decisions[m.ID] = Decision{Commit: sha}
		err = i.SaveState(state)
	}
	if err != nil {
		_ = os.RemoveAll(destination)
		return Installation{}, err
	}
	return result, nil
}
func (i Installer) extract(ctx context.Context, archive []byte, dest string) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gz.Close()
	// Also bound raw decompression (including ignored padding/PAX metadata), not just file payloads.
	raw := &io.LimitedReader{R: gz, N: i.MaxExtracted + int64(i.MaxFiles)*2048 + 1024}
	tr := tar.NewReader(raw)
	var total int64
	count := 0
	root := ""
	seen := map[string]bool{}
	// Track actual package objects, including parents created by MkdirAll.
	// Explicit directory headers for an implicit parent do not charge it twice.
	objects := map[string]bool{}
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return e
		}
		count++
		if count > i.MaxFiles {
			return errors.New("archive file count exceeds bound")
		}
		// git archive/codeload may emit a PAX global commit comment before the root.
		// This is metadata, never an extracted special file; ignore only comment records.
		if h.Typeflag == tar.TypeXGlobalHeader {
			if !safePath(h.Name) {
				return errors.New("unsafe PAX metadata name")
			}
			for key, value := range h.PAXRecords {
				if key != "comment" || len(value) > 1024 {
					return errors.New("unsupported global PAX metadata")
				}
			}
			continue
		}
		for key := range h.PAXRecords {
			if strings.HasPrefix(key, "GNU.sparse") {
				return errors.New("sparse archives are forbidden")
			}
		}
		n := strings.TrimSuffix(h.Name, "/")
		if !safePath(n) {
			return fmt.Errorf("unsafe archive path %q", h.Name)
		}
		p := strings.SplitN(n, "/", 2)
		if root == "" {
			root = p[0]
		}
		if p[0] != root {
			return errors.New("archive must contain a single root")
		}
		if h.Typeflag != tar.TypeDir && h.Typeflag != tar.TypeReg {
			return errors.New("archive links and special files are forbidden")
		}
		if h.Typeflag == tar.TypeDir && h.Size != 0 {
			return errors.New("directory payload is forbidden")
		}
		if len(p) == 1 {
			if h.Typeflag != tar.TypeDir {
				return errors.New("archive root must be directory")
			}
			continue
		}
		rel := p[1]
		if seen[rel] {
			return errors.New("duplicate archive path")
		}
		seen[rel] = true
		for object := rel; object != "."; object = path.Dir(object) {
			objects[object] = true
			if len(objects) > i.MaxFiles {
				return errors.New("archive filesystem object count exceeds bound")
			}
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		if h.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(target, 0700); err != nil {
				return err
			}
			continue
		}
		if h.Size < 0 || h.Size > i.MaxExtracted-total {
			return errors.New("extracted size exceeds bound")
		}
		total += h.Size
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			return err
		}
		f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, e = io.CopyN(f, tr, h.Size)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	if _, err = io.Copy(io.Discard, raw); err != nil {
		return err
	}
	if raw.N == 0 {
		return errors.New("raw archive expansion exceeds bound")
	}
	return nil
}
func (i Installer) List() ([]Installation, error) {
	if i.Store == "" {
		return nil, errors.New("extension store required")
	}
	st, err := os.Lstat(i.Store)
	if errors.Is(err, os.ErrNotExist) {
		return []Installation{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("store must be a real directory")
	}
	entries, err := os.ReadDir(i.Store)
	if errors.Is(err, os.ErrNotExist) {
		return []Installation{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []Installation{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if !safeID.MatchString(e.Name()) || !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			return nil, errors.New("invalid installed entry")
		}
		p := filepath.Join(i.Store, e.Name(), "installation.json")
		st, err := os.Lstat(p)
		if err != nil {
			return nil, err
		}
		if !st.Mode().IsRegular() || st.Size() > 64<<10 {
			return nil, errors.New("invalid provenance file")
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var record Installation
		if err = strict(b, &record); err != nil {
			return nil, err
		}
		if err = record.Manifest.Validate(); err != nil {
			return nil, err
		}
		if record.Manifest.ID != e.Name() || !commitPattern.MatchString(record.Commit) {
			return nil, errors.New("invalid installed provenance")
		}
		if _, err := ParseSource(record.Source); err != nil {
			return nil, errors.New("invalid installed source provenance")
		}
		result = append(result, record)
	}
	return result, nil
}
func (i Installer) Remove(id string) error {
	if !safeID.MatchString(id) {
		return errors.New("invalid extension id")
	}
	unlock, err := i.lock()
	if err != nil {
		return err
	}
	defer unlock()
	p := filepath.Join(i.Store, id)
	st, err := os.Lstat(p)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("installed path is not a real directory")
	}
	// Invalidate any old enabled intent before deleting code. Otherwise a crash
	// during reinstall of the same commit could resurrect a stale true decision.
	s, err := i.State()
	if err != nil {
		return err
	}
	s, err = i.Decide(s, id, false)
	if err != nil {
		return err
	}
	if err = i.SaveState(s); err != nil {
		return err
	}
	return os.RemoveAll(p)
}
