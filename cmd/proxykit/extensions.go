package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/NotRllyRn/proxykit/extensions"
)

func extensionStore() (string, error) {
	if value := os.Getenv("PROXYKIT_EXTENSIONS_DIR"); value != "" {
		return value, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "proxykit", "extensions"), nil
}
func runExtension(ctx context.Context, args []string, out io.Writer) error {
	store, err := extensionStore()
	if err != nil {
		return err
	}
	return runExtensionWithInstaller(ctx, args, out, extensions.Installer{Store: store})
}
func runExtensionWithInstaller(ctx context.Context, args []string, out io.Writer, i extensions.Installer) error {
	if len(args) == 0 {
		return errors.New("usage: proxykit extension install <GitHub URL> --trust | list | remove <id>")
	}
	switch args[0] {
	case "install":
		trust := false
		source := ""
		for _, arg := range args[1:] {
			if arg == "--trust" {
				if trust {
					return errors.New("duplicate --trust")
				}
				trust = true
			} else if source == "" {
				source = arg
			} else {
				return errors.New("usage: extension install <GitHub URL> --trust")
			}
		}
		if source == "" {
			return errors.New("GitHub URL required")
		}
		record, err := i.Install(ctx, source, trust)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "Installed %s %s at commit %s. Trusted code is not sandboxed. Restart the server to validate and activate installed hooks; running registrations are unchanged.\n", record.Manifest.ID, record.Manifest.Version, record.Commit)
		return err
	case "list":
		if len(args) != 1 {
			return errors.New("usage: extension list")
		}
		records, err := i.List()
		if err != nil {
			return err
		}
		return json.NewEncoder(out).Encode(records)
	case "remove":
		if len(args) != 2 {
			return errors.New("usage: extension remove <id>")
		}
		if err := i.Remove(args[1]); err != nil {
			return err
		}
		_, err := fmt.Fprintln(out, "Removed", args[1]+". Running servers require restart for changed registrations.")
		return err
	default:
		return errors.New("unknown extension command")
	}
}
