package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const minimalConfig = `version: 1
commands:
  build:
    description: Builds it.
    steps:
      - go build ./...
`

func TestDiscoverWalksUp(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "noodge.yaml"), minimalConfig)

	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(deep)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(root, "noodge.yaml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiscoverPrefersNearest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "noodge.yaml"), minimalConfig)

	nested := filepath.Join(root, "api")
	writeFile(t, filepath.Join(nested, "noodge.yaml"), minimalConfig)

	got, err := Discover(nested)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(nested, "noodge.yaml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiscoverStopsAtGitBoundary(t *testing.T) {
	root := t.TempDir()
	// A stray config above the repository must not be picked up.
	writeFile(t, filepath.Join(root, "noodge.yaml"), minimalConfig)

	repo := filepath.Join(root, "repo")
	writeFile(t, filepath.Join(repo, ".git", "HEAD"), "ref: refs/heads/main\n")

	sub := filepath.Join(repo, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(sub)
	if err == nil {
		t.Fatal("expected no config to be found inside the repository")
	}

	var nf *NotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("got %T (%v), want *NotFoundError", err, err)
	}
}

func TestDiscoverRejectsAmbiguousDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "noodge.yaml"), minimalConfig)
	writeFile(t, filepath.Join(root, "noodge.yml"), minimalConfig)

	_, err := Discover(root)
	if err == nil {
		t.Fatal("expected an error when both file names exist")
	}

	var amb *AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("got %T (%v), want *AmbiguousError", err, err)
	}
}

func TestDiscoverAcceptsYmlExtension(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "noodge.yml"), minimalConfig)

	got, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if want := filepath.Join(root, "noodge.yml"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
