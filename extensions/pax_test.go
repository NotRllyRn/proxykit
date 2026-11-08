package extensions

import (
	"archive/tar"
	"context"
	"testing"
)

func TestGitArchiveGlobalComment(t *testing.T) {
	items := append([]item{{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"comment": sha}}}, validItems()...)
	i, _ := fixture(t, archive(t, items))
	if _, err := i.Install(context.Background(), "https://github.com/example/repository", true); err != nil {
		t.Fatal("git archive global metadata rejected", err)
	}
}
func TestGlobalPathMetadataRejected(t *testing.T) {
	items := append([]item{{name: "pax_global_header", kind: tar.TypeXGlobalHeader, pax: map[string]string{"path": "../../escape"}}}, validItems()...)
	i, _ := fixture(t, archive(t, items))
	if _, err := i.Install(context.Background(), "https://github.com/example/repository", true); err == nil {
		t.Fatal("global path metadata accepted")
	}
}
