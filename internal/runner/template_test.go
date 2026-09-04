package runner

import (
	"strings"
	"testing"

	"github.com/wimhaanstra/noodge/internal/config"
)

// val builds a set parameter value.
func val(name, flag, s string) Value {
	return Value{
		Param: config.Param{Name: name, Flag: flag},
		Set:   true,
		Str:   s,
	}
}

// unset builds an optional parameter that was never supplied.
func unset(name, flag string) Value {
	return Value{Param: config.Param{Name: name, Flag: flag}}
}

// boolVal builds a bool parameter.
func boolVal(name, flag string, b bool) Value {
	return Value{
		Param: config.Param{Name: name, Flag: flag, Type: config.TypeBool},
		Set:   true,
		Bool:  b,
	}
}

func TestExpandLine(t *testing.T) {
	vals := Values{
		"host":    val("host", "--host", "localhost"),
		"cert":    unset("cert", "--certificate"),
		"verbose": boolVal("verbose", "--verbose", true),
		"quiet":   boolVal("quiet", "--quiet", false),
	}

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "flag form with a value",
			in:   "node app.js {{flag host}}",
			want: "node app.js --host 'localhost'",
		},
		{
			name: "unset optional flag disappears entirely",
			in:   "node app.js {{flag cert}}",
			want: "node app.js",
		},
		{
			name: "no double space is left behind",
			in:   "node app.js {{flag cert}} --end",
			want: "node app.js --end",
		},
		{
			name: "true bool is the flag alone",
			in:   "node app.js {{flag verbose}}",
			want: "node app.js --verbose",
		},
		{
			name: "false bool disappears",
			in:   "node app.js {{flag quiet}}",
			want: "node app.js",
		},
		{
			name: "bare value has no flag",
			in:   "node app.js --host {{host}}",
			want: "node app.js --host 'localhost'",
		},
		{
			name: "a single-dash spelling is just text",
			in:   "node app.js -host {{host}}",
			want: "node app.js -host 'localhost'",
		},
		{
			name: "several placeholders, one unset",
			in:   "node app.js {{flag host}} {{flag cert}} {{flag verbose}}",
			want: "node app.js --host 'localhost' --verbose",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandLine(tt.in, vals, nil, StylePosix)
			if err != nil {
				t.Fatalf("ExpandLine: %v", err)
			}
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestExpandLineArgs(t *testing.T) {
	got, err := ExpandLine("go test ./... {{args}}", Values{}, []string{"-run", "TestFoo"}, StylePosix)
	if err != nil {
		t.Fatal(err)
	}

	// Pass-through arguments are not quoted: they are text the user typed at
	// their own prompt for this one invocation.
	if want := "go test ./... -run TestFoo"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExpandArgv(t *testing.T) {
	vals := Values{
		"host":    val("host", "--host", "local host"),
		"cert":    unset("cert", "--certificate"),
		"verbose": boolVal("verbose", "--verbose", true),
	}

	tests := []struct {
		name string
		in   []string
		args []string
		want []string
	}{
		{
			name: "a whole-element flag becomes two arguments",
			in:   []string{"node", "app.js", "{{flag host}}"},
			want: []string{"node", "app.js", "--host", "local host"},
		},
		{
			name: "an unset optional element vanishes",
			in:   []string{"node", "{{flag cert}}", "app.js"},
			want: []string{"node", "app.js"},
		},
		{
			name: "a true bool is one argument",
			in:   []string{"node", "{{flag verbose}}"},
			want: []string{"node", "--verbose"},
		},
		{
			name: "text around a placeholder keeps it one argument",
			in:   []string{"node", "--host={{host}}"},
			want: []string{"node", "--host=local host"},
		},
		{
			name: "args splice in as separate elements",
			in:   []string{"go", "test", "{{args}}"},
			args: []string{"-run", "TestFoo"},
			want: []string{"go", "test", "-run", "TestFoo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandArgv(tt.in, vals, tt.args)
			if err != nil {
				t.Fatalf("ExpandArgv: %v", err)
			}
			if strings.Join(got, "\x00") != strings.Join(tt.want, "\x00") {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// Nothing is quoted in argv form, because those arguments never reach a shell.
func TestExpandArgvDoesNotQuote(t *testing.T) {
	vals := Values{"value": val("value", "--value", "a && b")}

	got, err := ExpandArgv([]string{"echo", "{{value}}"}, vals, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "a && b" {
		t.Errorf("got %q, want [echo, \"a && b\"]", got)
	}
}

func TestPassthroughPlacement(t *testing.T) {
	exe := helperPath(t)

	t.Run("appended to the last step when no placeholder asks", func(t *testing.T) {
		file := project(t, `version: 1
commands:
  two:
    description: Two steps.
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'echo', 'first']
      - [`+yaml(exe)+`, '-noodge-helper', 'echo', 'second']
`)
		out, err := execute(t, file, "two", nil, []string{"extra"})
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}

		lines := strings.Fields(out)
		if len(lines) != 3 || lines[2] != "extra" {
			t.Errorf("expected extra appended to the last step only, got %q", out)
		}
	})

	t.Run("substituted where the placeholder is", func(t *testing.T) {
		file := project(t, `version: 1
commands:
  two:
    description: Two steps, the first takes the args.
    steps:
      - [`+yaml(exe)+`, '-noodge-helper', 'echo', 'first', '{{args}}']
      - [`+yaml(exe)+`, '-noodge-helper', 'echo', 'second']
`)
		out, err := execute(t, file, "two", nil, []string{"extra"})
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}

		fields := strings.Fields(out)
		if len(fields) != 3 || fields[1] != "extra" {
			t.Errorf("expected extra in the first step, got %q", out)
		}
	})
}
