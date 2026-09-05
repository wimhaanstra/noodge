package config

import (
	"os"
	"path/filepath"
	"testing"
)

// loadInline writes a config to a temp file and loads it, for tests that need a
// config that is not one of the fixtures.
func loadInline(t *testing.T, doc string) *File {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

func TestGroupKey(t *testing.T) {
	cases := map[string]string{
		"dev":                 "dev",
		"dev:api":             "dev",
		"admin:promote:azure": "admin",
		"setup":               "setup",
		"check:scripts":       "check",
	}
	for name, want := range cases {
		if got := GroupKey(name); got != want {
			t.Errorf("GroupKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidateGroupsRejectsDuplicatesAndEmpties(t *testing.T) {
	f := loadInline(t, `version: 1
groups:
  - prefix: dev
  - prefix: dev
  - title: no prefix here
commands:
  dev:
    description: Runs it.
    steps:
      - echo hi
`)
	d := Validate(f)

	if _, ok := findDiag(d, "declared more than once"); !ok {
		t.Errorf("a duplicate prefix should be an error:\n%s", d.Error())
	}
	if _, ok := findDiag(d, "group has no prefix"); !ok {
		t.Errorf("a group without a prefix should be an error:\n%s", d.Error())
	}
	if !d.HasErrors() {
		t.Error("duplicate and empty prefixes are errors")
	}
}

func TestValidateGroupsWarnsOnUnknownPrefix(t *testing.T) {
	f := loadInline(t, `version: 1
groups:
  - prefix: nope
    title: Nothing here
commands:
  dev:
    description: Runs it.
    steps:
      - echo hi
`)
	d := Validate(f)

	diag, ok := findDiag(d, "matches no command")
	if !ok {
		t.Fatalf("a prefix that names no command should warn:\n%s", d.Error())
	}
	if diag.Severity != SeverityWarning {
		t.Errorf("an unknown prefix is a warning, not an error")
	}
	if d.HasErrors() {
		t.Errorf("an unknown prefix must not block execution:\n%s", d.Errors().Error())
	}
}
