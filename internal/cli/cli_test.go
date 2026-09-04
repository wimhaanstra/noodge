package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes the command tree against a temp directory and returns what it
// wrote and the exit code it would have produced.
func run(t *testing.T, dir string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	code = Execute(&Env{Stdout: &out, Stderr: &errb, Dir: dir}, args)
	return out.String(), errb.String(), code
}

// projectDir writes a noodge.yaml into a fresh temp directory.
func projectDir(t *testing.T, content string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "noodge.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

const sampleConfig = `version: 1
name: my-api
commands:
  start:
    description: |
      Starts the API server.

      Second paragraph that should not appear in the list.
    steps:
      - node app.js
  release:
    description: Builds a release.
    hidden: true
    steps:
      - npm run build
`

func TestVersion(t *testing.T) {
	stdout, _, code := run(t, t.TempDir(), "version")

	if code != ExitOK {
		t.Errorf("exit code: got %d, want %d", code, ExitOK)
	}
	if !strings.HasPrefix(stdout, "noodge ") {
		t.Errorf("got %q", stdout)
	}
}

func TestSchemaPrintsValidJSON(t *testing.T) {
	stdout, _, code := run(t, t.TempDir(), "schema")

	if code != ExitOK {
		t.Fatalf("exit code: got %d", code)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if _, ok := doc["$defs"]; !ok {
		t.Error("schema output has no $defs")
	}
}

func TestListHidesHiddenCommandsAndTrimsDescriptions(t *testing.T) {
	dir := projectDir(t, sampleConfig)
	stdout, _, code := run(t, dir, "list")

	if code != ExitOK {
		t.Fatalf("exit code: got %d", code)
	}
	if !strings.Contains(stdout, "start") {
		t.Error("start should be listed")
	}
	if strings.Contains(stdout, "release") {
		t.Error("hidden commands must not be listed")
	}
	if !strings.Contains(stdout, "Starts the API server.") {
		t.Error("the first line of the description should be shown")
	}
	if strings.Contains(stdout, "Second paragraph") {
		t.Error("only the first line belongs in the list")
	}
}

func TestBareInvocationLists(t *testing.T) {
	dir := projectDir(t, sampleConfig)

	bare, _, _ := run(t, dir)
	list, _, _ := run(t, dir, "list")

	if bare != list {
		t.Errorf("bare noodge should list:\n got: %q\nwant: %q", bare, list)
	}
}

func TestListJSON(t *testing.T) {
	dir := projectDir(t, sampleConfig)
	stdout, _, code := run(t, dir, "list", "--json")

	if code != ExitOK {
		t.Fatalf("exit code: got %d", code)
	}

	var entries []listEntry
	if err := json.Unmarshal([]byte(stdout), &entries); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, stdout)
	}

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (JSON includes hidden commands)", len(entries))
	}
	if entries[0].Name != "start" || entries[1].Name != "release" {
		t.Errorf("file order not preserved: %v", entries)
	}
	if !entries[1].Hidden {
		t.Error("release should be marked hidden")
	}
}

func TestValidateAcceptsAGoodConfig(t *testing.T) {
	dir := projectDir(t, sampleConfig)
	stdout, _, code := run(t, dir, "validate")

	if code != ExitOK {
		t.Fatalf("exit code: got %d", code)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("got %q", stdout)
	}
}

func TestValidateFailsWithExitCodeTwo(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  bad:
    description: Broken.
    params:
      - name: host
        flag: -host
        description: Single-dash flag.
    steps:
      - node app.js {{flag host}}
`)

	_, stderr, code := run(t, dir, "validate")

	if code != ExitConfig {
		t.Errorf("exit code: got %d, want %d", code, ExitConfig)
	}
	if !strings.Contains(stderr, "must start with two dashes") {
		t.Errorf("stderr should carry the diagnostic:\n%s", stderr)
	}
	// The summary is the last thing printed; a second generic error line
	// after it would be noise.
	if strings.Contains(stderr, "noodge: \n") {
		t.Errorf("an empty error line leaked into stderr:\n%s", stderr)
	}
}

// Warnings must never change the exit code, or the advisory-only rule is a
// lie the first time someone puts validate in CI.
func TestValidateWarningsDoNotFail(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  build:
    steps:
      - go build ./...
`)

	stdout, stderr, code := run(t, dir, "validate")

	if code != ExitOK {
		t.Fatalf("exit code: got %d, want %d\n%s", code, ExitOK, stderr)
	}
	if !strings.Contains(stderr, "has no description") {
		t.Errorf("expected the warning on stderr:\n%s", stderr)
	}
	if !strings.Contains(stdout, "is valid") {
		t.Errorf("expected the valid summary on stdout: %q", stdout)
	}
}

// Being somewhere without a noodge.yaml is an answer, not a failure.
func TestNoConfigIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	// Stop discovery escaping the temp directory into a real project above it.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, stderr, code := run(t, dir)

	if code != ExitOK {
		t.Errorf("exit code: got %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stderr, "noodge init") {
		t.Errorf("should suggest a next step:\n%s", stderr)
	}
}

func TestDirectoryFlag(t *testing.T) {
	dir := projectDir(t, sampleConfig)
	elsewhere := t.TempDir()

	stdout, _, code := run(t, elsewhere, "list", "-C", dir)

	if code != ExitOK {
		t.Fatalf("exit code: got %d", code)
	}
	if !strings.Contains(stdout, "start") {
		t.Errorf("-C should have found the other project: %q", stdout)
	}
}
