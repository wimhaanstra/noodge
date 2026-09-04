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
