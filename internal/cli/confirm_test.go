package cli

import (
	"bytes"
	"strings"
	"testing"
)

// confirmConfig has one command that asks before it runs and one that asks
// with a custom prompt. Both just echo, so a test can tell whether they ran.
const confirmConfig = `version: 1
name: sample
commands:
  wipe:
    description: Deletes everything.
    confirm: true
    steps:
      - echo wiped
  deploy:
    description: Ships it.
    confirm: Ship to production?
    steps:
      - echo shipped
  safe:
    description: Harmless.
    confirm: false
    steps:
      - echo safe
`

// runConfirm drives the tree with a chosen terminal state and stdin, which the
// shared run helper does not expose.
func runConfirm(t *testing.T, dir, stdin string, tty bool, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	var out, errb bytes.Buffer
	env := &Env{
		Stdout: &out,
		Stderr: &errb,
		Stdin:  strings.NewReader(stdin),
		Dir:    dir,
		TTY:    tty,
	}
	code = Execute(env, args)
	return out.String(), errb.String(), code
}

func TestConfirmAcceptedRuns(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, stderr, code := runConfirm(t, dir, "y\n", true, "wipe")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "wiped") {
		t.Errorf("command should have run after a yes:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Really run") {
		t.Errorf("a default prompt should have been shown:\n%s", stderr)
	}
}

func TestConfirmDeclinedDoesNotRun(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, stderr, code := runConfirm(t, dir, "n\n", true, "wipe")
	if code != ExitConfig {
		t.Fatalf("declining should exit %d, got %d", ExitConfig, code)
	}
	if strings.Contains(stdout, "wiped") {
		t.Errorf("command must not run after a no:\n%s", stdout)
	}
	if !strings.Contains(stderr, "cancelled") {
		t.Errorf("declining should say so:\n%s", stderr)
	}
}

// The empty answer is a no: a bare Enter must never run a destructive command.
func TestConfirmDefaultsToNo(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, _, code := runConfirm(t, dir, "\n", true, "wipe")
	if code != ExitConfig {
		t.Fatalf("a bare Enter should decline, got exit %d", code)
	}
	if strings.Contains(stdout, "wiped") {
		t.Errorf("a bare Enter must not run the command:\n%s", stdout)
	}
}

func TestConfirmCustomPromptIsShown(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	_, stderr, code := runConfirm(t, dir, "y\n", true, "deploy")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "Ship to production?") {
		t.Errorf("the configured prompt should be shown:\n%s", stderr)
	}
}

func TestConfirmYesFlagSkipsThePrompt(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	// No terminal and no stdin, exactly as CI runs it. --yes is the opt-in.
	stdout, stderr, code := runConfirm(t, dir, "", false, "wipe", "--yes")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "wiped") {
		t.Errorf("--yes should let the command run:\n%s", stdout)
	}
	if strings.Contains(stderr, "Really run") {
		t.Errorf("--yes should not print a prompt:\n%s", stderr)
	}
}

// Without a terminal and without --yes, a command that needs confirmation must
// refuse rather than assume an answer.
func TestConfirmRefusesWithoutATerminal(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, stderr, code := runConfirm(t, dir, "", false, "wipe")
	if code != ExitConfig {
		t.Fatalf("should refuse with exit %d, got %d", ExitConfig, code)
	}
	if strings.Contains(stdout, "wiped") {
		t.Errorf("command must not run unattended:\n%s", stdout)
	}
	if !strings.Contains(stderr, "--yes") {
		t.Errorf("the refusal should point at --yes:\n%s", stderr)
	}
}

// confirm: false is an explicit opt-out and must behave like no confirm at all.
func TestConfirmFalseNeverAsks(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, stderr, code := runConfirm(t, dir, "", false, "safe")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "safe") {
		t.Errorf("confirm: false should run without asking:\n%s", stdout)
	}
	if strings.Contains(stderr, "Really run") {
		t.Errorf("confirm: false must not prompt:\n%s", stderr)
	}
}

// --dry-run inspects; it must never stop to ask, even for a confirm command.
func TestConfirmSkippedUnderDryRun(t *testing.T) {
	dir := projectDir(t, confirmConfig)

	stdout, stderr, code := runConfirm(t, dir, "", false, "wipe", "--dry-run")
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, stderr)
	}
	if !strings.Contains(stdout, "echo wiped") {
		t.Errorf("dry-run should print the plan:\n%s", stdout)
	}
	if strings.Contains(stderr, "Really run") {
		t.Errorf("dry-run must not prompt:\n%s", stderr)
	}
}
