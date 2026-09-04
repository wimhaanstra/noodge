package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/invopop/jsonschema"
)

// Step is one command line in a command's sequence.
//
// Written as a string it is run through a shell, so pipes, redirects and
// environment expansion all work. Written as a list it is executed directly
// with those arguments and no shell is involved at all, which is the safer
// form when a parameter value is untrusted.
type Step struct {
	// Line is set when the step was written as a string.
	Line string
	// Argv is set when the step was written as a list of arguments.
	Argv []string
}

// IsArgv reports whether the step was written in the shell-free list form.
func (s Step) IsArgv() bool { return s.Argv != nil }

// IsZero reports whether the step carries no command at all.
func (s Step) IsZero() bool { return s.Line == "" && len(s.Argv) == 0 }

// String renders the step for display. It is not shell-quoted and must not be
// used to build a command line.
func (s Step) String() string {
	if s.IsArgv() {
		return strings.Join(s.Argv, " ")
	}
	return s.Line
}

// errStepShape is returned when a step is neither a string nor a list.
var errStepShape = errors.New("a step must be a string (run through a shell) or a list of arguments (run directly)")

// UnmarshalYAML accepts either form.
func (s *Step) UnmarshalYAML(b []byte) error {
	var argv []string
	if err := yaml.Unmarshal(b, &argv); err == nil {
		s.Argv = argv
		s.Line = ""
		return nil
	}

	var line string
	if err := yaml.Unmarshal(b, &line); err != nil {
		return errStepShape
	}
	s.Line = line
	s.Argv = nil
	return nil
}

// JSONSchema describes the two accepted shapes for editor autocomplete.
func (Step) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Title:       "Step",
		Description: "One command in the sequence. A string is run through a shell; a list of arguments is executed directly with no shell.",
		OneOf: []*jsonschema.Schema{
			{Type: "string"},
			{Type: "array", Items: &jsonschema.Schema{Type: "string"}, MinItems: ptr(uint64(1))},
		},
	}
}

// UnmarshalYAML preserves the order commands were declared in.
func (c *Commands) UnmarshalYAML(b []byte) error {
	var ms yaml.MapSlice
	if err := yaml.Unmarshal(b, &ms); err != nil {
		return err
	}

	out := make(Commands, 0, len(ms))
	for _, item := range ms {
		name, ok := item.Key.(string)
		if !ok {
			return fmt.Errorf("command names must be text, but found %v", item.Key)
		}

		raw, err := yaml.Marshal(item.Value)
		if err != nil {
			return fmt.Errorf("command %q: %w", name, err)
		}

		var cmd Command
		if err := yaml.UnmarshalWithOptions(raw, &cmd, yaml.Strict()); err != nil {
			return fmt.Errorf("command %q: %w", name, err)
		}

		out = append(out, NamedCommand{Name: name, Command: cmd})
	}

	*c = out
	return nil
}

// JSONSchema describes commands as a plain mapping of name to command, which
// is what an author writes, rather than as the ordered slice we decode into.
func (Commands) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:                 "object",
		Description:          "The runnable commands, keyed by the name you type after `noodge`.",
		AdditionalProperties: &jsonschema.Schema{Ref: "#/$defs/Command"},
	}
}

func ptr[T any](v T) *T { return &v }
