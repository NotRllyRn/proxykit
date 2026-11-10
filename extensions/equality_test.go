package extensions

import "testing"

func TestInstallationEqual(t *testing.T) {
	original := Installation{
		Manifest: Manifest{ManifestVersion: 1, APIVersion: 1, ID: "example", Version: "1.0.0", Entry: "main.ts"},
		Source:   "https://github.com/example/repository", Commit: "commit",
		Build: &SourceBuild{Compiler: "typescript", CompilerSHA256: "compiler hash", TreeSHA256: "tree hash"},
	}
	for _, tc := range []struct {
		name   string
		change func(*Installation)
	}{
		{"manifest version", func(r *Installation) { r.Manifest.ManifestVersion++ }},
		{"API version", func(r *Installation) { r.Manifest.APIVersion++ }},
		{"ID", func(r *Installation) { r.Manifest.ID = "other" }},
		{"version", func(r *Installation) { r.Manifest.Version = "2.0.0" }},
		{"entry", func(r *Installation) { r.Manifest.Entry = "other.ts" }},
		{"source", func(r *Installation) { r.Source += "/tree/main" }},
		{"commit", func(r *Installation) { r.Commit = "other commit" }},
		{"compiler", func(r *Installation) { r.Build.Compiler = "other compiler" }},
		{"compiler hash", func(r *Installation) { r.Build.CompilerSHA256 = "other hash" }},
		{"tree hash", func(r *Installation) { r.Build.TreeSHA256 = "other hash" }},
		{"missing build", func(r *Installation) { r.Build = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := original
			build := *original.Build
			copy.Build = &build
			if !original.Equal(copy) || !copy.Equal(original) {
				t.Fatal("equal provenance at different addresses must match")
			}
			tc.change(&copy)
			if original.Equal(copy) || copy.Equal(original) {
				t.Fatal("changed installation must not match")
			}
		})
	}
	original.Build = nil
	if !original.Equal(original) {
		t.Fatal("equal prebuilt installations must match")
	}
}
