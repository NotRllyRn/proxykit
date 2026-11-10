package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NotRllyRn/proxykit/sdk"
)

const compilerHash = "10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3"
const buildDir = ".proxykit-build"

//go:embed compiler/typescript-5.9.3.tgz
var compilerArchive []byte

//go:embed compiler/compile.cjs
var compilerDriver []byte

type SourceBuild struct {
	Compiler       string `json:"compiler"`
	CompilerSHA256 string `json:"compilerSHA256"`
	TreeSHA256     string `json:"treeSHA256"`
}

func sourceEntry(entry string) bool {
	return strings.HasSuffix(entry, ".mts") || strings.HasSuffix(entry, ".ts")
}
func (r Installation) RuntimeEntry() string {
	if sourceEntry(r.Manifest.Entry) {
		return filepath.Join(buildDir, strings.TrimSuffix(r.Manifest.Entry, filepath.Ext(r.Manifest.Entry))+".mjs")
	}
	return r.Manifest.Entry
}

// treeDigest bounds and authenticates both original package and compiled runtime/assets.
// It rejects all links/special files, even if they were introduced after extraction.
func treeDigest(root string, maxBytes int64, maxFiles int) (string, error) {
	var paths []string
	var total int64
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == root {
			if !d.IsDir() || d.Type()&os.ModeSymlink != 0 {
				return errors.New("invalid package root")
			}
			return nil
		}
		if len(paths) >= maxFiles {
			return errors.New("package artifact count exceeds bound")
		}
		rel, e := filepath.Rel(root, p)
		if e != nil || !safePath(filepath.ToSlash(rel)) {
			return errors.New("unsafe artifact path")
		}
		paths = append(paths, rel)
		if d.IsDir() {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() {
			return errors.New("package artifacts must be regular files")
		}
		total += st.Size()
		if total > maxBytes {
			return errors.New("package artifacts exceed size bound")
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		p := filepath.Join(root, rel)
		st, e := os.Lstat(p)
		if e != nil {
			return "", e
		}
		if st.IsDir() {
			fmt.Fprintf(h, "d:%d:%s:", len(rel), filepath.ToSlash(rel))
			continue
		}
		fmt.Fprintf(h, "f:%d:%s:%d:", len(rel), filepath.ToSlash(rel), st.Size())
		f, e := os.Open(p)
		if e != nil {
			return "", e
		}
		_, e = io.Copy(h, f)
		ce := f.Close()
		if e != nil {
			return "", e
		}
		if ce != nil {
			return "", ce
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (r Installation) Verify(root string) error {
	if !sourceEntry(r.Manifest.Entry) {
		if r.Build != nil {
			return errors.New("unexpected source build provenance")
		}
		return nil
	}
	if r.Build == nil || r.Build.Compiler != "TypeScript 5.9.3" || r.Build.CompilerSHA256 != compilerHash {
		return errors.New("missing or unsupported source compiler provenance")
	}
	digest, e := treeDigest(root, 128<<20, 8192)
	if e != nil {
		return e
	}
	if digest != r.Build.TreeSHA256 {
		return errors.New("source package integrity mismatch")
	}
	return nil
}

// cappedLog consumes all output without retaining unbounded diagnostics or blocking a child.
type cappedLog struct{ b []byte }

func (w *cappedLog) Write(p []byte) (int, error) {
	n := len(p)
	if len(w.b) < 16384 {
		keep := 16384 - len(w.b)
		if keep > n {
			keep = n
		}
		w.b = append(w.b, p[:keep]...)
	}
	return n, nil
}
func (i Installer) compile(ctx context.Context, pkg string) (*SourceBuild, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The child runs in the host tool directory, not the caller's working directory.
	var err error
	pkg, err = filepath.Abs(pkg)
	if err != nil {
		return nil, err
	}
	for _, reserved := range []string{buildDir, ".sdk"} {
		if _, e := os.Lstat(filepath.Join(pkg, reserved)); !errors.Is(e, os.ErrNotExist) {
			return nil, errors.New("reserved source build path")
		}
	}
	tools, e := os.MkdirTemp(filepath.Dir(pkg), "compiler-")
	if e != nil {
		return nil, e
	}
	defer os.RemoveAll(tools)
	if fmt.Sprintf("%x", sha256.Sum256(compilerArchive)) != compilerHash {
		return nil, errors.New("embedded compiler integrity mismatch")
	}
	gz, e := gzip.NewReader(bytes.NewReader(compilerArchive))
	if e != nil {
		return nil, e
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		name := strings.TrimPrefix(h.Name, "package/lib/")
		if name == h.Name || (name != "typescript.js" && !(strings.HasPrefix(name, "lib.") && strings.HasSuffix(name, ".d.ts"))) {
			continue
		}
		if filepath.Base(name) != name || h.Typeflag != tar.TypeReg || h.Size > 16<<20 {
			return nil, errors.New("invalid embedded compiler artifact")
		}
		b, e := io.ReadAll(tr)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(tools, name), b, 0600); e != nil {
			return nil, e
		}
	}
	if e = os.WriteFile(filepath.Join(tools, "compile.cjs"), compilerDriver, 0600); e != nil {
		return nil, e
	}
	out := filepath.Join(pkg, buildDir)
	if e = os.MkdirAll(filepath.Join(out, ".sdk"), 0700); e != nil {
		return nil, e
	}
	for _, name := range []string{"index.mjs", "index.d.mts", "http.d.mts"} {
		b, e := sdk.Files.ReadFile(name)
		if e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(out, ".sdk", name), b, 0600); e != nil {
			return nil, e
		}
	}
	// Mirror assets beside emitted modules so new URL('./asset', import.meta.url)
	// has the same relative layout. Never copy build configuration into runtime.
	e = filepath.WalkDir(pkg, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if p == out {
			return filepath.SkipDir
		}
		if p == pkg {
			return nil
		}
		rel, _ := filepath.Rel(pkg, p)
		target := filepath.Join(out, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		if strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".mts") || d.Name() == "package.json" {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(target, b, 0600)
	})
	if e != nil {
		return nil, e
	}
	timeout := i.CompileTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if timeout < 0 || timeout > 2*time.Minute {
		return nil, errors.New("invalid compiler timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	node := i.NodeBinary
	if node == "" {
		node = "node"
	}
	cmd := exec.CommandContext(ctx, node, "--max-old-space-size=256", "--max-semi-space-size=16", filepath.Join(tools, "compile.cjs"), pkg, out)
	// NODE_OPTIONS/NODE_PATH must not preload arbitrary code into compilation.
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "NODE_OPTIONS=") && !strings.HasPrefix(v, "NODE_PATH=") {
			cmd.Env = append(cmd.Env, v)
		}
	}
	cmd.Dir = tools
	cmd.WaitDelay = 100 * time.Millisecond
	isolateCompiler(cmd)
	log := &cappedLog{}
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("TypeScript source compilation failed: %w: %s", e, log.b)
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	digest, e := treeDigest(pkg, 128<<20, 8192)
	if e != nil {
		return nil, e
	}
	return &SourceBuild{Compiler: "TypeScript 5.9.3", CompilerSHA256: compilerHash, TreeSHA256: digest}, nil
}
