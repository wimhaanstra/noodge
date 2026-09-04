package config

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T, name string) *File {
	t.Helper()
	f, err := Load(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("Load(%s): %v", name, err)
	}
	return f
}

func TestLoadPreservesCommandOrder(t *testing.T) {
	f := loadFixture(t, "valid.yaml")

	want := []string{"start", "start:local", "release"}
	got := f.Config.Commands.Names()

	if len(got) != len(want) {
		t.Fatalf("got %d commands %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadDecodesCommandFields(t *testing.T) {
	f := loadFixture(t, "valid.yaml")

	if f.Config.Name != "my-api" {
		t.Errorf("name: got %q, want my-api", f.Config.Name)
	}
	if f.Config.Env["NODE_ENV"] != "development" {
		t.Errorf("file env: got %q, want development", f.Config.Env["NODE_ENV"])
	}

	sl, ok := f.Config.Commands.Get("start:local")
	if !ok {
		t.Fatal("start:local not found")
	}

	if len(sl.Params) != 3 {
		t.Fatalf("params: got %d, want 3", len(sl.Params))
	}
	if sl.Params[0].Default != "localhost" {
		t.Errorf("host default: got %v, want localhost", sl.Params[0].Default)
	}
	if sl.Params[1].Short != "-c" {
		t.Errorf("certificate short: got %q, want -c", sl.Params[1].Short)
	}
	if !sl.Params[1].Required {
		t.Error("certificate should be required")
	}
	if sl.Params[2].ResolvedType() != TypeBool {
		t.Errorf("verbose type: got %q, want bool", sl.Params[2].ResolvedType())
	}
	if sl.Env["NODE_ENV"] != "local" {
		t.Errorf("command env: got %q, want local", sl.Env["NODE_ENV"])
	}
	if len(sl.Aliases) != 1 || sl.Aliases[0] != "sl" {
		t.Errorf("aliases: got %v, want [sl]", sl.Aliases)
	}

	start, _ := f.Config.Commands.Get("start")
	if !strings.Contains(start.Description, "Hot-reloads on save") {
		t.Errorf("multi-line description lost: %q", start.Description)
	}
}

func TestLoadDecodesBothStepForms(t *testing.T) {
	f := loadFixture(t, "valid.yaml")

	rel, ok := f.Config.Commands.Get("release")
	if !ok {
		t.Fatal("release not found")
	}
	if !rel.Hidden {
		t.Error("release should be hidden")
	}
	if len(rel.Steps) != 3 {
		t.Fatalf("steps: got %d, want 3", len(rel.Steps))
	}

	if rel.Steps[0].IsArgv() {
		t.Error("step 1 should be a shell string")
	}
	if rel.Steps[0].Line != "npm run build" {
		t.Errorf("step 1: got %q", rel.Steps[0].Line)
	}

	if !rel.Steps[2].IsArgv() {
		t.Fatal("step 3 should be argv form")
	}
	wantArgv := []string{"npm", "pack", "--pack-destination", "./dist"}
	if len(rel.Steps[2].Argv) != len(wantArgv) {
		t.Fatalf("step 3 argv: got %v, want %v", rel.Steps[2].Argv, wantArgv)
	}
	for i := range wantArgv {
		if rel.Steps[2].Argv[i] != wantArgv[i] {
			t.Errorf("step 3 argv[%d]: got %q, want %q", i, rel.Steps[2].Argv[i], wantArgv[i])
		}
	}
}

func TestLoadRejectsFutureVersion(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "future.yaml"))
	if err == nil {
		t.Fatal("expected an error for version 99")
	}

	var ve *VersionError
	if !errors.As(err, &ve) {
		t.Fatalf("got %T (%v), want *VersionError", err, err)
	}
	if ve.Found != 99 || ve.Supported != FormatVersion {
		t.Errorf("got found=%d supported=%d", ve.Found, ve.Supported)
	}
	if !strings.Contains(err.Error(), "noodge upgrade") {
		t.Errorf("message should point at the upgrade: %q", err.Error())
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "noodge.yaml"), `version: 1
commands:
  build:
    describtion: typo
    steps:
      - go build ./...
`)

	_, err := Load(filepath.Join(dir, "noodge.yaml"))
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	if !strings.Contains(err.Error(), "describtion") {
		t.Errorf("error should name the offending field: %v", err)
	}
}
