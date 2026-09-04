package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// findDiag returns the first diagnostic whose message contains substr.
func findDiag(d Diagnostics, substr string) (Diagnostic, bool) {
	for _, x := range d {
		if strings.Contains(x.Message, substr) {
			return x, true
		}
	}
	return Diagnostic{}, false
}

func TestValidateCleanConfig(t *testing.T) {
	f := loadFixture(t, "valid.yaml")
	d := Validate(f)

	if d.HasErrors() {
		t.Fatalf("valid.yaml should have no errors, got:\n%s", d.Errors().Error())
	}
}

func TestValidateReportsPositions(t *testing.T) {
	f := loadFixture(t, "invalid.yaml")
	d := Validate(f)

	if !d.HasErrors() {
		t.Fatal("invalid.yaml should produce errors")
	}

	tests := []struct {
		name    string
		substr  string
		wantVal string // text expected on the reported line
	}{
		{"single-dash flag", "must start with two dashes", "-host"},
		{"enum without values", "lists no values", "mode"},
		{"unknown placeholder", "declares no parameter", "nosuch"},
		{"unused parameter", "no step uses it", "unused"},
		{"missing steps", "has no steps", "nosteps"},
	}

	lines := strings.Split(string(f.Source), "\n")

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diag, ok := findDiag(d, tt.substr)
			if !ok {
				t.Fatalf("no diagnostic matching %q in:\n%s", tt.substr, d.Error())
			}
			if diag.Line <= 0 {
				t.Fatalf("diagnostic has no line: %s", diag.String())
			}
			if diag.Line > len(lines) {
				t.Fatalf("line %d is past the end of the file", diag.Line)
			}

			got := lines[diag.Line-1]
			if !strings.Contains(got, tt.wantVal) {
				t.Errorf("reported line %d is %q, which does not contain %q",
					diag.Line, got, tt.wantVal)
			}
		})
	}
}

func TestValidateSingleDashHintExplainsTheDistinction(t *testing.T) {
	f := loadFixture(t, "invalid.yaml")
	d := Validate(f)

	diag, ok := findDiag(d, "must start with two dashes")
	if !ok {
		t.Fatal("expected the single-dash diagnostic")
	}

	// The confusion this hint exists to prevent is "but the tool I am wrapping
	// needs one dash" — so the hint has to say that still works.
	if !strings.Contains(diag.Hint, "--host") {
		t.Errorf("hint should suggest the corrected spelling: %q", diag.Hint)
	}
	if !strings.Contains(diag.Hint, "single dash") {
		t.Errorf("hint should explain the wrapped tool can still take one dash: %q", diag.Hint)
	}
}

func TestValidateDescriptionsAreWarningsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  build:
    steps:
      - go build ./...
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Validate(f)

	if d.HasErrors() {
		t.Fatalf("a missing description must never be an error, got:\n%s", d.Errors().Error())
	}
	if _, ok := findDiag(d.Warnings(), "has no description"); !ok {
		t.Errorf("expected a warning about the missing description, got:\n%s", d.Error())
	}
}

func TestValidateWarnsOnBuiltinShadowing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  list:
    description: Lists the widgets.
    steps:
      - echo widgets
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Validate(f)

	if d.HasErrors() {
		t.Fatalf("shadowing a built-in must not be an error, got:\n%s", d.Errors().Error())
	}

	diag, ok := findDiag(d.Warnings(), "same name as a built-in")
	if !ok {
		t.Fatalf("expected a shadowing warning, got:\n%s", d.Error())
	}
	if !strings.Contains(diag.Hint, "noodge run list") {
		t.Errorf("hint should name the escape hatch: %q", diag.Hint)
	}
}

func TestValidateOptionalBareValueWarns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  serve:
    description: Serves.
    params:
      - name: host
        flag: --host
        description: The host.
    steps:
      - node app.js --host {{host}}
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Validate(f)

	if d.HasErrors() {
		t.Fatalf("unexpected errors:\n%s", d.Errors().Error())
	}
	diag, ok := findDiag(d.Warnings(), "expands to nothing")
	if !ok {
		t.Fatalf("expected the dangling-flag warning, got:\n%s", d.Error())
	}
	if !strings.Contains(diag.Hint, "{{flag host}}") {
		t.Errorf("hint should suggest the flag form: %q", diag.Hint)
	}
}

func TestValidateDefaultTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  serve:
    description: Serves.
    params:
      - name: port
        flag: --port
        type: int
        default: not-a-number
        description: The port.
    steps:
      - node app.js {{flag port}}
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := Validate(f)

	if _, ok := findDiag(d.Errors(), "is not a number"); !ok {
		t.Errorf("expected a default type error, got:\n%s", d.Error())
	}
}

func TestParallelGroupParsesInDeclaredOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  dev:
    description: Runs the stack.
    steps:
      - parallel:
          zebra: node z.js
          apple: node a.js
          mango: ["node", "m.js"]
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if d := Validate(f); d.HasErrors() {
		t.Fatalf("unexpected errors:\n%s", d.Errors().Error())
	}

	nc, _ := f.Config.Commands.Get("dev")
	step := nc.Steps[0]

	if !step.IsParallel() {
		t.Fatal("expected a parallel group")
	}

	var got []string
	for _, e := range step.Parallel {
		got = append(got, e.Name)
	}
	if strings.Join(got, ",") != "zebra,apple,mango" {
		t.Errorf("order not preserved: got %v", got)
	}

	// The list form still works inside a group.
	if !step.Parallel[2].Step.IsArgv() {
		t.Error("the third entry should have decoded as an argv step")
	}
	if !step.Prefixed() {
		t.Error("prefixing should default to on")
	}
}

func TestParallelPrefixCanBeTurnedOff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  dev:
    description: Raw output.
    steps:
      - parallel:
          api: node a.js
        prefix: false
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	nc, _ := f.Config.Commands.Get("dev")
	if nc.Steps[0].Prefixed() {
		t.Error("prefix: false should turn labelling off")
	}
}

// A quoted value containing a colon must stay a string. An earlier
// implementation re-serialised each entry in order to decode it, and this is
// the case that broke: the re-rendered text parsed back as a mapping.
//
// An unquoted value containing ": " is invalid YAML in the first place, and
// the parser rejects it with a position, which is the right answer.
func TestParallelEntryKeepsAColonInItsValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "noodge.yaml")
	writeFile(t, path, `version: 1
commands:
  dev:
    description: Tricky value.
    steps:
      - parallel:
          api: "sh -c 'echo port: 3000'"
`)

	f, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	nc, _ := f.Config.Commands.Get("dev")
	entry := nc.Steps[0].Parallel[0]

	if entry.Step.IsParallel() || entry.Step.Line == "" {
		t.Fatalf("value was not kept as a string: %+v", entry.Step)
	}
	if !strings.Contains(entry.Step.Line, "port: 3000") {
		t.Errorf("value altered: %q", entry.Step.Line)
	}
}

func TestParallelGroupProblems(t *testing.T) {
	tests := []struct {
		name   string
		doc    string
		substr string
	}{
		{
			name: "nested group",
			doc: `      - parallel:
          outer:
            parallel:
              inner: echo hi
`,
			substr: "nests another parallel group",
		},
		{
			// The parser rejects this before validation sees it, with a
			// message that names both lines. Kept as a test because the
			// behaviour matters, not because of which layer produces it.
			name: "duplicate entry name",
			doc: `      - parallel:
          api: echo one
          api: echo two
`,
			substr: "already defined",
		},
		{
			name: "unusable entry name",
			doc: `      - parallel:
          "my service": echo hi
`,
			substr: "cannot be used as a label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "noodge.yaml")
			writeFile(t, path, `version: 1
commands:
  dev:
    description: Broken group.
    steps:
`+tt.doc)

			f, err := Load(path)
			if err != nil {
				// Some of these are rejected while decoding, which is also a
				// refusal to run; the message still has to name the problem.
				if !strings.Contains(err.Error(), tt.substr) {
					t.Fatalf("load error should mention %q, got: %v", tt.substr, err)
				}
				return
			}

			d := Validate(f)
			if _, ok := findDiag(d.Errors(), tt.substr); !ok {
				t.Errorf("expected an error mentioning %q, got:\n%s", tt.substr, d.Error())
			}
		})
	}
}
