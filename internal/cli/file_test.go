package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile writes content to a named file in a fresh temp directory and
// returns the full path. The directory gets a .git so that, if discovery were
// ever reached, it could not escape into a real project above the temp dir.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const fileConfig = `version: 1
name: elsewhere
commands:
  greet:
    description: Says hello from the other file.
    steps:
      - echo hello
`

func TestFileFlagLoadsANamedConfig(t *testing.T) {
	path := writeFile(t, "custom.yaml", fileConfig)

	// Run from an unrelated empty directory to prove discovery is not what
	// found the file.
	stdout, stderr, code := run(t, t.TempDir(), "--file", path, "list")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "greet") {
		t.Errorf("--file should have loaded the named config:\n%s", stdout)
	}
}

func TestFileFlagEqualsForm(t *testing.T) {
	path := writeFile(t, "custom.yaml", fileConfig)

	stdout, _, code := run(t, t.TempDir(), "--file="+path, "greet", "--dry-run")
	if code != ExitOK {
		t.Fatal(stdout)
	}
	if !strings.Contains(stdout, "echo hello") {
		t.Errorf("--file=path should reach the command:\n%s", stdout)
	}
}

// --file wins over a noodge.yaml sitting in the working directory.
func TestFileFlagOverridesDiscovery(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  local:
    description: The project's own command.
    steps:
      - echo local
`)
	other := writeFile(t, "other.yaml", fileConfig)

	stdout, stderr, code := run(t, dir, "--file", other, "list")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "greet") {
		t.Errorf("--file should override the local noodge.yaml:\n%s", stdout)
	}
	if strings.Contains(stdout, "local") {
		t.Errorf("the local command must not appear when --file points elsewhere:\n%s", stdout)
	}
}

func TestFileFlagMissingFileErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")

	_, stderr, code := run(t, t.TempDir(), "--file", missing, "list")
	if code != ExitConfig {
		t.Fatalf("a missing --file should exit %d, got %d", ExitConfig, code)
	}
	if !strings.Contains(stderr, "nope.yaml") {
		t.Errorf("the error should name the file it could not read:\n%s", stderr)
	}
}
