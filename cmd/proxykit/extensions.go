package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"text/tabwriter"

	"github.com/NotRllyRn/proxykit/control"
	"github.com/NotRllyRn/proxykit/extensions"
	"github.com/NotRllyRn/proxykit/nodehost"
)

func extensionStore() (string, error) {
	if value := os.Getenv("PROXYKIT_EXTENSIONS_DIR"); value != "" {
		return value, nil
	}
	base, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(base, "proxykit", "extensions"), nil
}
func runExtension(ctx context.Context, args []string, out io.Writer) error {
	store, e := extensionStore()
	if e != nil {
		return e
	}
	return runExtensionWithInstaller(ctx, args, out, extensions.Installer{Store: store})
}
func runExtensionWithInstaller(ctx context.Context, args []string, out io.Writer, i extensions.Installer) error {
	if len(args) == 0 {
		return errors.New("usage: extension install URL --trust [--activate] | enable ID | disable ID | status [ID] | list | remove ID")
	}
	request := control.Request{Op: args[0]}
	activate := false
	switch request.Op {
	case "install":
		for _, arg := range args[1:] {
			switch arg {
			case "--trust":
				if request.Trust {
					return errors.New("duplicate --trust")
				}
				request.Trust = true
			case "--activate":
				if activate {
					return errors.New("duplicate --activate")
				}
				activate = true
			default:
				if request.Source != "" {
					return errors.New("usage: extension install URL --trust [--activate]")
				}
				request.Source = arg
			}
		}
		if request.Source == "" {
			return errors.New("GitHub URL required")
		}
	case "enable", "disable", "remove":
		if len(args) != 2 {
			return errors.New("extension command requires one ID")
		}
		request.ID = args[1]
	case "status":
		if len(args) > 2 {
			return errors.New("usage: extension status [ID]")
		}
	case "list":
		if len(args) != 1 {
			return errors.New("usage: extension list")
		}
	default:
		return errors.New("unknown extension command")
	}
	unlock, lockErr := extensions.Lock(i.Store)
	if lockErr != nil && !errors.Is(lockErr, extensions.ErrStoreBusy) {
		return lockErr
	}
	online := lockErr != nil
	if !online {
		defer unlock()
		i.Owned = true
	}
	if online {
		callRequest := request
		if request.Op == "list" {
			callRequest.Op = "status"
		}
		response, e := control.Call(ctx, i.Store, callRequest)
		if e != nil {
			return fmt.Errorf("store owned or unavailable; no offline mutation attempted: %w (owner lock: %v)", e, lockErr)
		}
		switch request.Op {
		case "install":
			fmt.Fprintf(out, "Installed %s disabled. Trusted code is not sandboxed.\n", response.Message)
			if activate {
				_, e = control.Call(ctx, i.Store, control.Request{Op: "enable", ID: response.Message})
				if e != nil {
					return fmt.Errorf("installed disabled; activation failed: %w", e)
				}
				fmt.Fprintln(out, "Activated", response.Message)
			}
		case "status", "list":
			if response.Status == nil {
				return errors.New("management status missing")
			}
			return printStatus(out, *response.Status, true, statusID(args))
		default:
			fmt.Fprintln(out, request.Op, request.ID, "committed online")
		}
		return e
	}
	switch request.Op {
	case "install":
		record, e := i.Install(ctx, request.Source, request.Trust)
		if e != nil {
			return e
		}
		fmt.Fprintf(out, "Installed %s %s at commit %s, disabled. Trusted code is not sandboxed.\n", record.Manifest.ID, record.Manifest.Version, record.Commit)
		if activate {
			return errors.New("installed disabled; --activate requires an online server")
		}
		return nil
	case "enable", "disable":
		s, e := i.State()
		if e != nil {
			return e
		}
		s, e = i.Decide(s, request.ID, request.Op == "enable")
		if e != nil {
			return e
		}
		if e = i.SaveState(s); e != nil {
			return e
		}
		fmt.Fprintln(out, request.Op, request.ID, "persisted intent only; server offline")
		return nil
	case "remove":
		if e := i.Remove(request.ID); e != nil {
			return e
		}
		fmt.Fprintln(out, "Removed", request.ID)
		return nil
	case "list":
		// Legacy machine-readable list remains available; status is diagnostic inventory.
		records, e := i.List()
		if e != nil {
			return e
		}
		return json.NewEncoder(out).Encode(records)
	case "status":
		s, e := i.State()
		if e != nil {
			return e
		}
		rows, e := i.Inventory()
		if e != nil {
			return e
		}
		status := nodehost.RuntimeStatus{Revision: s.Revision}
		for _, row := range rows {
			r := nodehost.StatusRow{ID: row.ID, Version: row.Installation.Manifest.Version, API: row.Installation.Manifest.APIVersion, Enabled: "unknown", Incompatible: row.Diagnostic}
			if d, ok := s.Decisions[row.ID]; ok && d.Commit == row.Installation.Commit {
				r.Enabled = fmt.Sprint(d.Enabled)
			}
			status.Rows = append(status.Rows, r)
		}
		return printStatus(out, status, false, statusID(args))
	}
	return nil
}
func statusID(args []string) string {
	if len(args) == 2 && args[0] == "status" {
		return args[1]
	}
	return ""
}
func printStatus(out io.Writer, s nodehost.RuntimeStatus, online bool, id string) error {
	runtime := "server offline; runtime unknown"
	if online {
		runtime = fmt.Sprintf("generation=%d draining=%v forced=%d", s.Generation, s.Draining, s.Forced)
	}
	fmt.Fprintf(out, "revision=%d %s %s\n", s.Revision, runtime, s.Detail)
	table := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "ID\tVERSION\tAPI\tINSTALLED\tENABLED\tACTIVE\tDRAINING\tINCOMPATIBLE\tFAILED\tDETAIL")
	found := id == ""
	for _, r := range s.Rows {
		if id != "" && r.ID != id {
			continue
		}
		found = true
		active, draining := fmt.Sprint(r.Active), fmt.Sprint(r.Draining)
		if !online {
			active = "unknown"
			draining = "unknown"
		}
		fmt.Fprintf(table, "%s\t%s\t%d\ttrue\t%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Version, r.API, r.Enabled, active, draining, r.Incompatible, r.Failed, r.Detail)
	}
	if e := table.Flush(); e != nil {
		return e
	}
	if !found {
		return fmt.Errorf("extension %s not installed", id)
	}
	return nil
}
