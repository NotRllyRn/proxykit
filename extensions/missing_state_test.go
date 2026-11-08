package extensions

import (
	"strings"
	"testing"
)

func TestMissingEnabledInstallationFailsClosed(t *testing.T) {
	i := Installer{Store: t.TempDir()}
	s := State{Version: 1, Decisions: map[string]Decision{"required-policy": {Commit: strings.Repeat("a", 40), Enabled: true}}}
	if _, err := i.Enabled(s); err == nil || !strings.Contains(err.Error(), "required-policy: enabled installation is missing") {
		t.Fatalf("missing enabled protection must not become an empty pipeline: %v", err)
	}
	s.Decisions["required-policy"] = Decision{Commit: strings.Repeat("a", 40), Enabled: false}
	if records, err := i.Enabled(s); err != nil || len(records) != 0 {
		t.Fatalf("removed disabled installation must remain harmless: %v %v", records, err)
	}
}
