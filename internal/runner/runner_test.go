package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wimhaanstra/noodge/internal/config"
)

// yaml renders a string as a single-quoted YAML scalar.
//
// Single quotes matter here. A Windows path is full of backslashes, and inside
// a double-quoted YAML scalar those are escape sequences, so an AppData path
// fails to parse with "unknown escape character 'A'". Single-quoted scalars
// take backslashes literally.
func yaml(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// project writes a noodge.yaml and loads it, so tests exercise the real
// decoding path rather than hand-built structs.
func project(t *testing.T, doc string) *config.File {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := config.Load(path)
	if err != nil {
		t.Fatalf("loading fixture: %v", err)
	}
	return file
}

// execute plans and runs a command, returning everything it wrote.
func execute(t *testing.T, file *config.File, name string, vals Values, args []string) (string, error) {
	t.Helper()

	nc, ok := file.Config.Commands.Get(name)
	if !ok {
		t.Fatalf("command %q not found", name)
	}
	if vals == nil {
		vals = Values{}
	}

	var out bytes.Buffer
	req := &Request{
		File:    file,
		Command: nc,
		Values:  vals,
		Args:    args,
		Stdout:  &out,
		Stderr:  &out,
	}

	plan, err := PlanCommand(req)
	if err != nil {
		return out.String(), err
	}

	// Run first, read the buffer after. In a single return statement Go
	// evaluates out.String() before Run has written anything to it.
	runErr := Run(req, plan)
	return out.String(), runErr
}

func TestQuote(t *testing.T) {
	tests := []struct {
		style QuoteStyle
		in    string
		want  string
	}{
		{StylePosix, "plain", "'plain'"},
		{StylePosix, "a b", "'a b'"},
		{StylePosix, "it's", `'it'\''s'`},
		{StylePosix, "a && b", "'a && b'"},
		// A cmd value is C-runtime quoted, then every character cmd acts on is
		// caret-escaped, the quotes included, so cmd never enters quoted state
		// and the program still receives one intact argument.
		{StyleCmd, "plain", `^"plain^"`},
		{StyleCmd, `say "hi"`, `^"say \^"hi\^"^"`},
		{StyleCmd, "a && b", `^"a ^&^& b^"`},
		{StyleCmd, `mid\slash`, `^"mid\slash^"`},
		{StyleCmd, `trailing\`, `^"trailing\\^"`},
		{StylePowerShell, "it's", "'it''s'"},
	}

	for _, tt := range tests {
		if got := Quote(tt.style, tt.in); got != tt.want {
			t.Errorf("Quote(%d, %q) = %q, want %q", tt.style, tt.in, got, tt.want)
		}
	}
}

// The security promise: a parameter value can never become a second command,
// whatever it contains. This runs through the real platform shell, because
// that is the only place the quoting rules are actually tested.
func TestValuesCannotEscapeTheShell(t *testing.T) {
	exe := helperPath(t)
	marker := filepath.Join(t.TempDir(), "breakout.txt")

	// Every metacharacter that could chain, redirect or substitute a command.
	// A percent sign is deliberately absent: cmd expands %VAR% before it looks
	// at quotes, which is an unavoidable quirk of that shell documented on
	// quoteCmd, and which produces text rather than a new command.
	hostile := "a && echo pwned; | `id` $(id) \"quoted\" > " + marker

	step := `"` + exe + `" -noodge-helper echo {{value}}`

	file := project(t, `version: 1
commands:
  echo:
    description: Echoes a value.
    params:
      - name: value
        flag: --value
        description: The value.
    steps:
      - `+yaml(step)+`
`)

	vals := Values{"value": {
		Param: config.Param{Name: "value", Flag: "--value"},
		Set:   true,
		Str:   hostile,
	}}

	out, err := execute(t, file, "echo", vals, nil)
	if err != nil {
		t.Fatalf("run: %v\noutput:\n%s", err, out)
	}

	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a redirect escaped the quoting and created a file")
	}
	// "echo pwned" is part of the value itself, so the word appears in the
	// echoed output either way. What would prove a breakout is a line that is
	// nothing but the word, which is what the chained echo would have printed.
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "pwned" {
			t.Fatalf("a chained command ran:\n%s", out)
		}
	}

	// The value must also arrive intact, not merely harmless.
	if !strings.Contains(out, hostile) {
		t.Errorf("value was altered in transit:\n got: %s\nwant: %s", out, hostile)
	}

	// The helper prints one argument per line, so the value arriving as a
	// single line is the same as it arriving as a single argument.
	if lines := strings.Split(strings.TrimSpace(out), "\n"); len(lines) != 1 {
		t.Fatalf("value was split into %d arguments, want 1:\n%s", len(lines), out)
	}
}

func TestExitCodePassesThroughVerbatim(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  fail:
    description: Exits 3.
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'exit', '3']
`)

	_, err := execute(t, file, "fail", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}

	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("got %T (%v), want *ExitError", err, err)
	}
	if exitErr.Code != 3 {
		t.Errorf("code: got %d, want 3", exitErr.Code)
	}
	if exitErr.Step != 1 {
		t.Errorf("step: got %d, want 1", exitErr.Step)
	}
}

func TestStopsAtFirstFailure(t *testing.T) {
	exe := helperPath(t)
	marker := filepath.Join(t.TempDir(), "third.txt")

	file := project(t, `version: 1
commands:
  three:
    description: Fails at step two.
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'exit', '0']
      - [`+yaml(exe)+`, '-noodge-helper', 'exit', '7']
      - [`+yaml(exe)+`, '-noodge-helper', 'touch', `+yaml(marker)+`]
`)

	_, err := execute(t, file, "three", nil, nil)
	if err == nil {
		t.Fatal("expected an error")
	}

	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("got %T (%v), want *ExitError", err, err)
	}
	if exitErr.Step != 2 || exitErr.Code != 7 {
		t.Errorf("got step %d code %d, want step 2 code 7", exitErr.Step, exitErr.Code)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("step 3 ran after step 2 failed")
	}
}

func TestEnvironmentMergesAndExportsParams(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
env:
  FROM_FILE: file-value
  OVERRIDDEN: file-value
commands:
  show:
    description: Prints environment.
    env:
      OVERRIDDEN: command-value
    params:
      - name: my-host
        flag: --my-host
        default: example.com
        description: A host.
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'env', 'FROM_FILE', 'OVERRIDDEN', 'NOODGE_COMMAND', 'NOODGE_PARAM_MY_HOST']
`)

	vals := Values{"my-host": {
		Param: config.Param{Name: "my-host", Flag: "--my-host"},
		Set:   true,
		Str:   "example.com",
	}}

	out, err := execute(t, file, "show", vals, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	for _, want := range []string{
		"FROM_FILE=file-value",
		"OVERRIDDEN=command-value",
		"NOODGE_COMMAND=show",
		"NOODGE_PARAM_MY_HOST=example.com",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestCwdIsRelativeToTheConfig(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  where:
    description: Prints the working directory.
    cwd: sub
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'cwd']
`)

	if err := os.MkdirAll(filepath.Join(file.Dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, file, "where", nil, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.HasSuffix(strings.TrimSpace(out), "sub") {
		t.Errorf("expected the sub directory, got:\n%s", out)
	}
}
