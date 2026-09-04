package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// echoConfig declares commands that just report what they were given, so the
// assertions are about noodge's behaviour rather than about a wrapped tool.
const echoConfig = `version: 1
name: sample
commands:
  serve:
    description: Serves the thing.
    params:
      - name: host
        flag: --host
        type: string
        default: localhost
        description: Host to bind.
      - name: port
        flag: --port
        type: int
        description: Port to bind.
      - name: verbose
        flag: --verbose
        short: -v
        type: bool
        description: Chatty.
      - name: mode
        flag: --mode
        type: enum
        values: [dev, prod]
        description: Which mode.
    steps:
      - echo {{flag host}} {{flag port}} {{flag verbose}} {{flag mode}}
  deploy:
    description: Deploys it.
    params:
      - name: target
        flag: --target
        required: true
        description: Where to.
    steps:
      - echo {{flag target}}
  chain:
    description: Two steps.
    steps:
      - echo one
      - echo two
`

func TestDryRunShowsExpandedCommand(t *testing.T) {
	dir := projectDir(t, echoConfig)

	stdout, stderr, code := run(t, dir, "serve", "--port", "8080", "--verbose", "--mode", "dev", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}

	// The default applies, the bool becomes the flag alone, and the enum is
	// checked against its values.
	for _, want := range []string{"--host", "localhost", "--port", "8080", "--verbose", "--mode", "dev"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
}

func TestUnsetOptionalFlagLeavesNothingBehind(t *testing.T) {
	dir := projectDir(t, echoConfig)

	stdout, _, code := run(t, dir, "serve", "--dry-run")
	if code != ExitOK {
		t.Fatal(stdout)
	}

	// --port and --mode were not given and have no default, so neither the
	// flag nor a gap should survive.
	if strings.Contains(stdout, "--port") {
		t.Errorf("an unset flag was emitted:\n%s", stdout)
	}
	if strings.Contains(stdout, "  echo  ") {
		t.Errorf("a double space was left behind:\n%s", stdout)
	}
}

func TestFlagValueForms(t *testing.T) {
	dir := projectDir(t, echoConfig)

	separate, _, _ := run(t, dir, "serve", "--port", "8080", "--dry-run")
	equals, _, _ := run(t, dir, "serve", "--port=8080", "--dry-run")

	if separate != equals {
		t.Errorf("--port 8080 and --port=8080 differ:\n%q\n%q", separate, equals)
	}
	if !strings.Contains(separate, "8080") {
		t.Errorf("value missing:\n%s", separate)
	}
}

func TestTypeCoercionRejectsBadValues(t *testing.T) {
	dir := projectDir(t, echoConfig)

	tests := []struct {
		name   string
		args   []string
		substr string
	}{
		{"int", []string{"serve", "--port", "eighty"}, "whole number"},
		{"enum", []string{"serve", "--mode", "staging"}, "one of dev, prod"},
		{"missing required", []string{"deploy"}, "required flag"},
		{"unknown flag", []string{"serve", "--nosuch"}, "unknown flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, stderr, code := run(t, dir, tt.args...)

			if code != ExitConfig {
				t.Errorf("exit code: got %d, want %d", code, ExitConfig)
			}
			if !strings.Contains(stderr, tt.substr) {
				t.Errorf("stderr should contain %q:\n%s", tt.substr, stderr)
			}
		})
	}
}

// Cobra claims -h for help unless a help flag already exists, and pflag panics
// outright when a config then declares -h for itself. This is the regression
// test for that: it is exactly the sort of thing that comes back as a bug
// report six months later.
func TestConfigMayDeclareDashH(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  serve:
    description: Serves.
    params:
      - name: host
        flag: --host
        short: -h
        description: Host to bind, with the shorthand Cobra normally takes.
    steps:
      - echo {{flag host}}
`)

	stdout, stderr, code := run(t, dir, "serve", "-h", "example.com", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "example.com") {
		t.Errorf("-h was not treated as the config's own flag:\n%s", stdout)
	}
}

// --help must keep working even with -h given away.
func TestLongHelpStillWorksWhenDashHIsTaken(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  serve:
    description: Serves the thing nicely.
    params:
      - name: host
        flag: --host
        short: -h
        description: Host to bind.
    steps:
      - echo {{flag host}}
`)

	stdout, stderr, code := run(t, dir, "serve", "--help")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "Serves the thing nicely") {
		t.Errorf("help did not render:\n%s", stdout)
	}
}

// The help text has to say where -- arguments go, because it is not guessable
// from the config.
func TestHelpExplainsWherePassthroughLands(t *testing.T) {
	dir := projectDir(t, echoConfig)

	stdout, _, _ := run(t, dir, "chain", "--help")
	if !strings.Contains(stdout, "appended to the last step") {
		t.Errorf("help should say where -- arguments land:\n%s", stdout)
	}
}

func TestStrayArgumentSuggestsTheDashDashForm(t *testing.T) {
	dir := projectDir(t, echoConfig)

	_, stderr, code := run(t, dir, "chain", "extra")
	if code != ExitConfig {
		t.Errorf("exit code: got %d, want %d", code, ExitConfig)
	}
	if !strings.Contains(stderr, "--") || !strings.Contains(stderr, "extra") {
		t.Errorf("error should suggest the -- form:\n%s", stderr)
	}
}

// run addresses a config command unambiguously, which is the escape hatch when
// one shares a name with a built-in.
func TestRunReachesAShadowedCommand(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  list:
    description: Lists the widgets, not the commands.
    steps:
      - echo widgets
`)

	builtin, _, code := run(t, dir, "list")
	if code != ExitOK {
		t.Fatal("built-in list should still work")
	}
	if !strings.Contains(builtin, "Lists the widgets") {
		t.Errorf("plain list should run the built-in:\n%s", builtin)
	}

	own, stderr, code := run(t, dir, "run", "list", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(own, "echo widgets") {
		t.Errorf("run list should reach the config command:\n%s", own)
	}
}

func TestRunReachesAHiddenCommand(t *testing.T) {
	dir := projectDir(t, `version: 1
commands:
  secret:
    description: Hidden but runnable.
    hidden: true
    steps:
      - echo shh
`)

	stdout, stderr, code := run(t, dir, "run", "secret", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "echo shh") {
		t.Errorf("hidden commands should be runnable:\n%s", stdout)
	}
}

func TestInitCreatesAWorkingConfig(t *testing.T) {
	dir := t.TempDir()
	// Keep discovery from escaping into a real project above the temp dir.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, code := run(t, dir, "init")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "created") {
		t.Errorf("got %q", stdout)
	}

	// What it writes must actually be valid, or the first thing a new user
	// sees is their own config being rejected.
	_, stderr, code = run(t, dir, "validate")
	if code != ExitOK {
		t.Errorf("the generated config does not validate:\n%s", stderr)
	}

	// And it must refuse to clobber what it just wrote.
	_, stderr, code = run(t, dir, "init")
	if code != ExitConfig {
		t.Error("init should refuse to overwrite an existing config")
	}
	if !strings.Contains(stderr, "already exists") {
		t.Errorf("got %q", stderr)
	}
}
