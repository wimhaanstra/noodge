package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/wimhaanstra/noodge/internal/config"
)

// ansi matches the escape sequences lipgloss emits for colour, which would
// otherwise make every text assertion depend on the terminal profile of
// whatever machine the tests run on.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func plain(s string) string { return ansi.ReplaceAllString(s, "") }

const browseConfig = `version: 1
name: sample
commands:
  start:
    description: |
      Starts the API server.

      A second paragraph that belongs in the detail pane.
    steps:
      - node app.js
    output: Server log on stdout.
  start:local:
    description: Starts it behind HTTPS.
    params:
      - name: certificate
        flag: --certificate
        type: string
        required: true
        description: Path to the pfx.
      - name: host
        flag: --host
        default: localhost
        description: Host to bind.
      - name: verbose
        flag: --verbose
        type: bool
        description: Chatty.
      - name: mode
        flag: --mode
        type: enum
        values: [dev, prod]
        description: Which mode.
    steps:
      - node app.js {{flag certificate}} {{flag host}} {{flag verbose}} {{flag mode}}
  secret:
    description: Hidden one.
    hidden: true
    steps:
      - echo shh
`

func loadConfig(t *testing.T, doc string) *config.File {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return file
}

// sized returns a model that has already been told how big the window is,
// which is what makes it render anything at all.
func sized(t *testing.T, doc string) Model {
	t.Helper()

	m := New(loadConfig(t, doc))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return next.(Model)
}

// press sends a key and returns the updated model and whether it asked to quit.
func press(t *testing.T, m Model, key tea.KeyPressMsg) (Model, bool) {
	t.Helper()

	next, cmd := m.Update(key)
	out := next.(Model)

	// A command is opaque, so run it and look at the message it produced. The
	// form communicates by message, so this also delivers formDone.
	for cmd != nil {
		msg := cmd()
		if _, isQuit := msg.(tea.QuitMsg); isQuit {
			return out, true
		}
		if msg == nil {
			break
		}

		var next2 tea.Model
		next2, cmd = out.Update(msg)
		out = next2.(Model)
	}

	return out, false
}

func key(r rune) tea.KeyPressMsg     { return tea.KeyPressMsg{Code: r, Text: string(r)} }
func special(c rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: c} }

func TestBrowserListsVisibleCommands(t *testing.T) {
	m := sized(t, browseConfig)
	view := plain(m.View().Content)

	for _, want := range []string{"start", "start:local"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Errorf("hidden commands must not be listed:\n%s", view)
	}
}

// The detail pane is the whole point of the tool, so it shows the full
// description rather than the summary line the list shows.
func TestDetailPaneShowsFullDocumentation(t *testing.T) {
	m := sized(t, browseConfig)
	view := plain(m.View().Content)

	for _, want := range []string{
		"Starts the API server.",
		"second paragraph",
		"Output",
		"Server log on stdout.",
		"Steps",
		"node app.js",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in:\n%s", want, view)
		}
	}
}

func TestMovingTheCursorChangesTheDetailPane(t *testing.T) {
	m := sized(t, browseConfig)

	m, _ = press(t, m, special(tea.KeyDown))
	view := plain(m.View().Content)

	if !strings.Contains(view, "Starts it behind HTTPS.") {
		t.Errorf("detail pane did not follow the cursor:\n%s", view)
	}
	if !strings.Contains(view, "--certificate") {
		t.Errorf("parameters should be documented in the detail pane:\n%s", view)
	}
}

func TestAltScreenIsRequested(t *testing.T) {
	m := sized(t, browseConfig)
	if !m.View().AltScreen {
		t.Error("the browser should run in the alternate screen buffer")
	}
}

func TestEnterOnACommandWithNoParamsChoosesIt(t *testing.T) {
	m := sized(t, browseConfig)

	m, quit := press(t, m, special(tea.KeyEnter))
	if !quit {
		t.Fatal("expected the browser to finish")
	}

	res := m.Result()
	if res.Command != "start" {
		t.Errorf("command: got %q, want start", res.Command)
	}
	if len(res.Args) != 0 {
		t.Errorf("args: got %v, want none", res.Args)
	}
	if got, want := res.CommandLine(), "noodge start"; got != want {
		t.Errorf("command line: got %q, want %q", got, want)
	}
}

func TestQuitCancels(t *testing.T) {
	m := sized(t, browseConfig)

	m, quit := press(t, m, key('q'))
	if !quit {
		t.Fatal("q should quit")
	}
	if m.Result().Chosen() {
		t.Errorf("cancelling must not choose anything: %+v", m.Result())
	}
}

// This is the hole the form exists to fill: without it, choosing a command
// with a required parameter would exit and fail immediately.
func TestEnterOnACommandWithParamsOpensTheForm(t *testing.T) {
	m := sized(t, browseConfig)

	m, _ = press(t, m, special(tea.KeyDown))
	m, quit := press(t, m, special(tea.KeyEnter))
	if quit {
		t.Fatal("a command with parameters should open the form, not run")
	}

	view := plain(m.View().Content)
	for _, want := range []string{"--certificate", "Path to the pfx", "--host", "--verbose"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q in the form:\n%s", want, view)
		}
	}
}

// Required parameters come first, because they are the ones that stop the
// command running.
func TestFormPutsRequiredParametersFirst(t *testing.T) {
	m := sized(t, browseConfig)
	m, _ = press(t, m, special(tea.KeyDown))
	m, _ = press(t, m, special(tea.KeyEnter))

	if got := m.form.fields[0].param.Name; got != "certificate" {
		t.Errorf("first field: got %q, want certificate", got)
	}
}

func TestFormRefusesToSubmitWithoutARequiredValue(t *testing.T) {
	m := sized(t, browseConfig)
	m, _ = press(t, m, special(tea.KeyDown))
	m, _ = press(t, m, special(tea.KeyEnter))

	m, quit := press(t, m, special(tea.KeyEnter))
	if quit {
		t.Fatal("the form should not submit while a required value is empty")
	}
	if !strings.Contains(plain(m.View().Content), "--certificate is required") {
		t.Errorf("expected the error in:\n%s", plain(m.View().Content))
	}
}

func TestFormProducesTheEquivalentCommandLine(t *testing.T) {
	m := sized(t, browseConfig)
	m, _ = press(t, m, special(tea.KeyDown))
	m, _ = press(t, m, special(tea.KeyEnter))

	// Type a value into the focused (required) field.
	for _, r := range "dev.pfx" {
		m, _ = press(t, m, key(r))
	}

	// Move to --verbose and turn it on, then to --mode and pick the first value.
	m, _ = press(t, m, special(tea.KeyTab)) // host
	m, _ = press(t, m, special(tea.KeyTab)) // verbose
	m, _ = press(t, m, key(' '))
	m, _ = press(t, m, special(tea.KeyTab)) // mode
	m, _ = press(t, m, special(tea.KeyRight))

	m, quit := press(t, m, special(tea.KeyEnter))
	if !quit {
		t.Fatalf("expected the form to submit, view was:\n%s", plain(m.View().Content))
	}

	res := m.Result()
	if res.Command != "start:local" {
		t.Fatalf("command: got %q", res.Command)
	}

	got := res.CommandLine()
	for _, want := range []string{
		"noodge start:local",
		"--certificate dev.pfx",
		"--host localhost", // the default is pre-filled, so it survives
		"--verbose",
		"--mode dev",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

func TestEscapeReturnsFromTheFormToTheList(t *testing.T) {
	m := sized(t, browseConfig)
	m, _ = press(t, m, special(tea.KeyDown))
	m, _ = press(t, m, special(tea.KeyEnter))

	if m.state != stateForm {
		t.Fatal("expected the form")
	}

	m, quit := press(t, m, special(tea.KeyEsc))
	if quit {
		t.Fatal("escape from the form should not quit the browser")
	}
	if m.state != stateList {
		t.Error("escape should return to the list")
	}
	if m.Result().Chosen() {
		t.Error("escaping the form must not choose anything")
	}
}

// A narrow window must not corrupt the layout; it should just show less.
func TestNarrowWindowStillRenders(t *testing.T) {
	m := New(loadConfig(t, browseConfig))
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	m = next.(Model)

	view := plain(m.View().Content)
	if view == "" {
		t.Fatal("nothing rendered")
	}

	for i, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 40 {
			t.Errorf("line %d is %d columns wide, want at most 40:\n%q", i, len([]rune(line)), line)
		}
	}
}
