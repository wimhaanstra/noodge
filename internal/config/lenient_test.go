package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func names(cmds []LenientCommand) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, c.Name)
	}
	return out
}

// fixtureDir copies a testdata fixture into a temp directory under the name
// discovery actually looks for.
func fixtureDir(t *testing.T, fixture string) string {
	t.Helper()
	dir := t.TempDir()
	src, err := filepath.Abs(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	copyFile(t, src, filepath.Join(dir, "noodge.yaml"))
	return dir
}

func TestLoadLenientReadsAValidConfig(t *testing.T) {
	got := names(LoadLenient(fixtureDir(t, "valid.yaml")))

	want := []string{"start", "start:local", "release"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("command %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadLenientCarriesShortDescriptions(t *testing.T) {
	cmds := LoadLenient(fixtureDir(t, "valid.yaml"))

	for _, c := range cmds {
		if c.Name != "start:local" {
			continue
		}
		if c.Short != "Starts the API behind a local HTTPS listener." {
			t.Errorf("short: got %q", c.Short)
		}
		return
	}
	t.Fatal("start:local not found")
}

// The whole point of the lenient loader: a config being edited is exactly when
// completion matters most, so a broken file must still offer what it can.
func TestLoadLenientRecoversFromBrokenYAML(t *testing.T) {
	dir := fixtureDir(t, "broken.yaml")

	// Confirm the fixture really is broken, or this test proves nothing.
	if _, err := Load(filepath.Join(dir, "noodge.yaml")); err == nil {
		t.Fatal("fixture parses cleanly; it is supposed to be malformed")
	}

	got := names(LoadLenient(dir))
	if len(got) == 0 {
		t.Fatal("expected to recover at least one command name")
	}

	joined := strings.Join(got, ",")
	for _, want := range []string{"build", "test"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected to recover %q, got %v", want, got)
		}
	}
}

func TestLoadLenientReturnsNothingWithoutAConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".git", "HEAD"), "ref: refs/heads/main\n")

	if got := LoadLenient(dir); len(got) != 0 {
		t.Errorf("got %v, want nothing", names(got))
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, to, string(b))
}
