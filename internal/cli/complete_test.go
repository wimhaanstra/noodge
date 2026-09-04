package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// complete runs a completion request against a directory, the way a shell's
// generated script would.
func complete(t *testing.T, dir string, words ...string) []string {
	t.Helper()

	// A shell always passes an absolute working directory through -C here,
	// because it runs the binary in the directory the user is standing in.
	args := append([]string{"__complete", "-C", dir}, words...)
	out := completionResponse(args)

	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

// candidates strips the trailing directive and returns the names offered.
func candidates(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, ":") || strings.TrimSpace(l) == "" {
			continue
		}
		out = append(out, strings.SplitN(l, "\t", 2)[0])
	}
	return out
}

func hasCandidate(lines []string, want string) bool {
	for _, c := range candidates(lines) {
		if c == want {
			return true
		}
	}
	return false
}

// The single most important property: whatever happens, the last line must be
// something the shell can parse as an integer. Cobra's PowerShell script casts
// it directly, so a missing or malformed directive does not degrade
// completion, it breaks TAB.
func assertEndsWithDirective(t *testing.T, lines []string) {
	t.Helper()

	if len(lines) == 0 {
		t.Fatal("completion produced no output at all")
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, ":") {
		t.Fatalf("last line must be a directive, got %q in:\n%s", last, strings.Join(lines, "\n"))
	}
}

func TestIsCompletionRequest(t *testing.T) {
	tests := []struct {
		args []string
		want bool
	}{
		{[]string{"__complete", ""}, true},
		{[]string{"__completeNoDesc", ""}, true},
		{[]string{"list"}, false},
		{[]string{}, false},
		{[]string{"--dry-run", "__complete"}, false},
	}

	for _, tt := range tests {
		if got := IsCompletionRequest(tt.args); got != tt.want {
			t.Errorf("IsCompletionRequest(%v) = %v, want %v", tt.args, got, tt.want)
		}
	}
}

func TestCompleteOffersConfigCommands(t *testing.T) {
	dir := projectDir(t, echoConfig)
	lines := complete(t, dir, "")

	assertEndsWithDirective(t, lines)
	for _, want := range []string{"serve", "deploy", "chain"} {
		if !hasCandidate(lines, want) {
			t.Errorf("missing %q in:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

func TestCompleteCarriesDescriptions(t *testing.T) {
	dir := projectDir(t, echoConfig)
	lines := complete(t, dir, "")

	for _, l := range lines {
		if strings.HasPrefix(l, "serve\t") {
			if !strings.Contains(l, "Serves the thing") {
				t.Errorf("description missing: %q", l)
			}
			return
		}
	}
	t.Errorf("serve was not offered with a description:\n%s", strings.Join(lines, "\n"))
}

func TestCompleteFiltersByPrefix(t *testing.T) {
	dir := projectDir(t, echoConfig)
	lines := complete(t, dir, "dep")

	assertEndsWithDirective(t, lines)
	if !hasCandidate(lines, "deploy") {
		t.Errorf("deploy should be offered:\n%s", strings.Join(lines, "\n"))
	}
	if hasCandidate(lines, "serve") {
		t.Errorf("serve does not match the prefix:\n%s", strings.Join(lines, "\n"))
	}
}

// An enum's declared values are the config author's list arriving in the shell
// for free.
func TestCompleteOffersEnumValues(t *testing.T) {
	dir := projectDir(t, echoConfig)
	lines := complete(t, dir, "serve", "--mode", "")

	assertEndsWithDirective(t, lines)
	for _, want := range []string{"dev", "prod"} {
		if !hasCandidate(lines, want) {
			t.Errorf("missing enum value %q in:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}

// A config being edited is exactly when completion matters most, so a file
// that does not parse must still complete what it can.
func TestCompleteRecoversFromABrokenConfig(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  deploy:
    description: Ships it.
    steps:
      - ./deploy.sh
  migrate:
    description: Migrates the database.
    steps:
      - ./migrate.sh
      this is not: [valid yaml
`)

	// Confirm the fixture really is broken, or this test proves nothing.
	if _, _, code := run(t, dir, "validate"); code == ExitOK {
		t.Fatal("fixture parses cleanly; it is supposed to be malformed")
	}

	lines := complete(t, dir, "")
	assertEndsWithDirective(t, lines)

	for _, want := range []string{"deploy", "migrate"} {
		if !hasCandidate(lines, want) {
			t.Errorf("should have recovered %q from the broken file:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// The built-ins are how you fix a broken config, so they must survive too.
	if !hasCandidate(lines, "validate") {
		t.Errorf("built-ins should still be offered:\n%s", strings.Join(lines, "\n"))
	}
}

func TestCompleteWithNoConfigOffersBuiltinsOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	lines := complete(t, dir, "")
	assertEndsWithDirective(t, lines)

	if !hasCandidate(lines, "init") {
		t.Errorf("init is the way out of this state, it must be offered:\n%s", strings.Join(lines, "\n"))
	}
}

// Nothing may reach stderr on this path, ever. completionResponse writes only
// to its own buffer, so anything escaping to the real stderr would be a bug
// that only shows up as TAB misbehaving.
func TestCompleteWritesNothingToStderr(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  ok:
    description: Fine.
    steps:
      - echo hi
      broken: [
`)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = original }()

	_ = complete(t, dir, "")

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	if n > 0 {
		t.Errorf("completion wrote %d bytes to stderr:\n%s", n, buf[:n])
	}
}

func TestEndsWithDirective(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{":4\n", true},
		{"build\tBuilds\n:0\n", true},
		{"build\tBuilds\n:0\n\n", true},
		{"build\tBuilds\n", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := endsWithDirective(tt.in); got != tt.want {
			t.Errorf("endsWithDirective(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// Cobra registers the completer for the bare name only, so "noodge.exe <TAB>"
// silently completes nothing in PowerShell. That reads as completion being
// broken rather than as it never having been registered. Cobra #1853.
func TestPowerShellScriptRegistersTheExeAlias(t *testing.T) {
	dir := projectDir(t, echoConfig)

	stdout, stderr, code := run(t, dir, "completion", "powershell")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}

	if !strings.Contains(stdout, "-CommandName 'noodge'") {
		t.Error("the bare name should be registered")
	}
	if !strings.Contains(stdout, "-CommandName 'noodge.exe'") {
		t.Errorf("the .exe alias should be registered too:\n%s", lastLines(stdout, 4))
	}
}

func TestCompletionGeneratesForEveryShell(t *testing.T) {
	dir := projectDir(t, echoConfig)

	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			stdout, stderr, code := run(t, dir, "completion", shell)
			if code != ExitOK {
				t.Fatalf("exit %d\n%s", code, stderr)
			}
			if len(stdout) < 500 {
				t.Errorf("script looks too short (%d bytes)", len(stdout))
			}
			// Every generated script must call back into the binary, since
			// that is what makes the command list per-directory.
			if !strings.Contains(stdout, "__complete") {
				t.Error("script does not call back into noodge")
			}
		})
	}
}

func TestCompletionRejectsAnUnknownShell(t *testing.T) {
	dir := projectDir(t, echoConfig)

	_, _, code := run(t, dir, "completion", "nushell")
	if code != ExitConfig {
		t.Errorf("exit code: got %d, want %d", code, ExitConfig)
	}
}

// Quoting a Windows path with Go's %q would produce C:\\Users\\..., and
// PowerShell has no backslash escapes, so the doubled separators are taken
// literally.
func TestPathQuoting(t *testing.T) {
	const win = `C:\Users\Wim\OneDrive - Work\PowerShell\noodge.completion.ps1`

	if got := psQuote(win); strings.Contains(got, `\\`) {
		t.Errorf("PowerShell quoting must not escape backslashes: %s", got)
	}
	if got, want := psQuote(`it's`), `'it''s'`; got != want {
		t.Errorf("psQuote: got %s, want %s", got, want)
	}
	if got, want := shQuote(`it's`), `'it'\''s'`; got != want {
		t.Errorf("shQuote: got %s, want %s", got, want)
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// On Windows both PowerShell editions are commonly installed and they do not
// share a profile, so installing for the wrong one leaves the user with no
// completion and no explanation. Each edition is nameable.
func TestInstallTargetsNameBothPowerShellEditions(t *testing.T) {
	for _, want := range []string{"pwsh", "windows-powershell"} {
		found := false
		for _, target := range installTargets {
			if target == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%q should be an install target, got %v", want, installTargets)
		}
	}
}

// Both editions run the same script; they differ only in which profile loads
// it, which is an install concern rather than a generator one.
func TestBothPowerShellTargetsGenerateTheSameScript(t *testing.T) {
	dir := projectDir(t, echoConfig)

	base, _, code := run(t, dir, "completion", "powershell")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}

	for _, target := range []string{"pwsh", "windows-powershell"} {
		t.Run(target, func(t *testing.T) {
			// The generator is reached directly: these names are install
			// targets, not arguments to the completion command itself.
			root, err := NewRoot(&Env{Stdout: io.Discard, Stderr: io.Discard, Dir: dir}, &options{directory: dir})
			if err != nil {
				t.Fatal(err)
			}

			got, err := completionScript(root, target)
			if err != nil {
				t.Fatalf("completionScript(%s): %v", target, err)
			}
			if !strings.Contains(got, "-CommandName 'noodge.exe'") {
				t.Error("the .exe alias should be registered for this target too")
			}
			if len(got) != len(base) {
				t.Errorf("script differs in length from the powershell one: %d vs %d", len(got), len(base))
			}
		})
	}
}
