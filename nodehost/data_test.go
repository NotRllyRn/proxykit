package nodehost

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NotRllyRn/proxykit/extensions"
)

// Exercise the installed SDK and actual Node setup, not a mock SetupAPI.
func TestInstalledDataDirLifecycle(t *testing.T) {
	store := t.TempDir()
	sdk, err := os.ReadFile("../sdk/index.mjs")
	if err != nil {
		t.Fatal(err)
	}
	source := `import {defineHTTP,pass} from './sdk.mjs';
import fs from 'node:fs'; import path from 'node:path';
export default defineHTTP(api=>{
 if(!path.isAbsolute(api.dataDir)||!fs.statSync(api.dataDir).isDirectory()) throw Error('missing host directory');
 if((fs.statSync(api.dataDir).mode&0o777)!==0o700) throw Error('not private');
 const config=path.join(api.dataDir,'config.json');
 if(!fs.existsSync(config)) fs.writeFileSync(config,'initialized',{mode:0o600});
 if(fs.readFileSync(config,'utf8')!=='initialized') throw Error('lost configuration');
 const count=path.join(api.dataDir,'setups');
 fs.appendFileSync(count,'setup\n',{mode:0o600});
 api.cleanup(()=>fs.appendFileSync(path.join(api.dataDir,'cleanups'),'cleanup\n',{mode:0o600}));
 api.on({id:'same-display-label',stage:'request',bodyMode:'none'},()=>pass());
});`
	install := func(id, version string) {
		installFixture(t, store, id, source)
		p := filepath.Join(store, id, "package", "sdk.mjs")
		if e := os.WriteFile(p, sdk, 0600); e != nil {
			t.Fatal(e)
		}
		// Model a newly installed version while keeping the unique manifest ID.
		p = filepath.Join(store, id, "installation.json")
		b, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		var record extensions.Installation
		if e = json.Unmarshal(b, &record); e != nil {
			t.Fatal(e)
		}
		record.Manifest.Version = version
		if version == "2.0.0" {
			record.Commit = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}
		b, _ = json.Marshal(record)
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		b, _ = json.Marshal(record.Manifest)
		if e = os.WriteFile(filepath.Join(store, id, "package", "proxykit-extension.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
		i := extensions.Installer{Store: store, Owned: true}
		s, e := i.State()
		if e != nil {
			t.Fatal(e)
		}
		s.Decisions[id] = extensions.Decision{Commit: record.Commit, Enabled: false}
		if e = i.SaveState(s); e != nil {
			t.Fatal(e)
		}
	}
	install("example-one", "1.0.0")
	install("example-two", "1.0.0")
	i := extensions.Installer{Store: store, Owned: true}
	s, err := i.State()
	if err != nil {
		t.Fatal(err)
	}
	s, err = i.Decide(s, "example-one", true)
	if err != nil {
		t.Fatal(err)
	}
	if err = i.SaveState(s); err != nil {
		t.Fatal(err)
	}
	m, _ := runtimeProxy(t, store, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, 4)
	check := func(id, file, want string) {
		t.Helper()
		p := filepath.Join(store, ".data", id, file)
		b, e := os.ReadFile(p)
		if e != nil || string(b) != want {
			t.Fatalf("%s = %q, want %q: %v", p, b, want, e)
		}
	}
	change := func(id string, enabled bool) {
		t.Helper()
		if e := m.Change(context.Background(), id, enabled); e != nil {
			t.Fatal(e)
		}
		waitRetired(t, m)
	}
	change("example-two", true)
	check("example-one", "setups", "setup\n") // unrelated activation reuses worker
	check("example-two", "setups", "setup\n") // same label, distinct ID and files
	change("example-one", false)
	check("example-one", "cleanups", "cleanup\n")
	change("example-one", true)
	check("example-one", "setups", "setup\nsetup\n")
	change("example-one", false)
	if err = m.Remove("example-one"); err != nil {
		t.Fatal(err)
	}
	check("example-one", "config.json", "initialized")
	install("example-one", "2.0.0")
	change("example-one", true)
	check("example-one", "setups", "setup\nsetup\nsetup\n")
	if err = m.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	m2, _ := runtimeProxy(t, store, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, 4)
	defer m2.Close(context.Background())
	check("example-one", "setups", "setup\nsetup\nsetup\nsetup\n")
	check("example-one", "config.json", "initialized")
	check("example-two", "setups", "setup\nsetup\n")
}

func TestDataDirFailureRollsBackActivation(t *testing.T) {
	store := t.TempDir()
	installFixture(t, store, "example", `export default api=>{throw Error('must not reach setup')}`)
	disableFixture(t, store, "example")
	m, _ := runtimeProxy(t, store, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), time.Second, 4)
	if err := os.Mkdir(filepath.Join(store, ".data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(store, ".data", "example")); err != nil {
		t.Fatal(err)
	}
	before, err := m.Status()
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Change(context.Background(), "example", true); err == nil {
		t.Fatal("unsafe storage must fail activation")
	}
	after, err := m.Status()
	if err != nil || after.Generation != before.Generation || after.Revision != before.Revision {
		t.Fatalf("failed activation changed pipeline/intent: before=%+v after=%+v error=%v", before, after, err)
	}
	s, err := (extensions.Installer{Store: store}).State()
	if err != nil || s.Decisions["example"].Enabled {
		t.Fatalf("failed activation enabled extension: %+v %v", s, err)
	}
}

func TestDataDirFailureDoesNotRunSetup(t *testing.T) {
	for _, setupFails := range []bool{false, true} {
		t.Run(fmt.Sprint(setupFails), func(t *testing.T) {
			store := t.TempDir()
			marker := filepath.Join(t.TempDir(), "ran")
			source := fmt.Sprintf(`import fs from 'node:fs';import path from 'node:path';export default api=>{fs.writeFileSync(%q,'ran');fs.writeFileSync(path.join(api.dataDir,'state'),'preserve',{mode:0o600});throw Error('setup failed')}`, marker)
			installFixture(t, store, "example", source)
			if !setupFails {
				if err := os.Symlink(t.TempDir(), filepath.Join(store, ".data")); err != nil {
					t.Fatal(err)
				}
			}
			h, err := Open(context.Background(), Options{Store: store, Gateway: "http://localhost:8080"})
			if err == nil {
				closeHost(t, h)
				t.Fatal("expected setup/storage failure")
			}
			_, ran := os.Stat(marker)
			if (ran == nil) != setupFails {
				t.Fatalf("setup ran: %v", ran)
			}
			if setupFails {
				if b, err := os.ReadFile(filepath.Join(store, ".data", "example", "state")); err != nil || string(b) != "preserve" {
					t.Fatalf("failed setup must retain data: %q %v", b, err)
				}
			}
		})
	}
}
