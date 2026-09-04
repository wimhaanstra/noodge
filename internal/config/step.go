package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/invopop/jsonschema"
)

// Step is one entry in a command's sequence.
//
// Written as a string it is run through a shell, so pipes, redirects and
// environment expansion all work. Written as a list it is executed directly
// with those arguments and no shell is involved at all, which is the safer
// form when a parameter value is untrusted. Written as a parallel group it
// starts several things at once and waits for them together.
type Step struct {
	// Line is set when the step was written as a string.
	Line string
	// Argv is set when the step was written as a list of arguments.
	Argv []string

	// Parallel is set when the step was written as a parallel group. Its
	// entries all start at once and run until one fails or all finish.
	//
	// Entries are named rather than listed, because the name is what labels
	// their output. Derived labels were tried first and were useless in
	// practice: three services each starting with "node" all came out as
	// "node", "node2", "node3".
	Parallel []ParallelEntry
	// isGroup distinguishes a group that declared no entries, which is worth
	// a proper diagnostic, from a step that is not a group at all.
	isGroup bool
	// Prefix labels each line of a parallel group's output with the entry it
	// came from. Nil means the default, which is on.
	Prefix *bool
}

// ParallelEntry is one named member of a parallel group. The name labels its
// output and is how it is referred to in messages.
type ParallelEntry struct {
	// Name is the key it was declared under.
	Name string
	// Step is what to run.
	Step Step
}

// IsArgv reports whether the step was written in the shell-free list form.
func (s Step) IsArgv() bool { return s.Argv != nil }

// IsParallel reports whether the step is a group that runs its entries at once.
func (s Step) IsParallel() bool { return s.isGroup }

// Prefixed reports whether a parallel group's output should be labelled.
func (s Step) Prefixed() bool { return s.Prefix == nil || *s.Prefix }

// IsZero reports whether the step carries no command at all.
func (s Step) IsZero() bool {
	return s.Line == "" && len(s.Argv) == 0 && !s.isGroup
}

// String renders the step for display. It is not shell-quoted and must not be
// used to build a command line.
func (s Step) String() string {
	switch {
	case s.IsParallel():
		parts := make([]string, 0, len(s.Parallel))
		for _, e := range s.Parallel {
			parts = append(parts, e.Name)
		}
		return "parallel: " + strings.Join(parts, " | ")
	case s.IsArgv():
		return strings.Join(s.Argv, " ")
	default:
		return s.Line
	}
}

// errStepShape is returned when a step is none of the accepted forms.
var errStepShape = errors.New(
	"a step must be a string (run through a shell), a list of arguments (run directly), " +
		"or a parallel: group of named entries")

// UnmarshalYAML accepts every form a step can take.
func (s *Step) UnmarshalYAML(b []byte) error {
	var argv []string
	if err := yaml.Unmarshal(b, &argv); err == nil {
		s.Argv = argv
		s.Line = ""
		return nil
	}

	var line string
	if err := yaml.Unmarshal(b, &line); err == nil {
		s.Line = line
		s.Argv = nil
		return nil
	}

	// A group is read straight from the syntax tree rather than decoded into a
	// Go value, for two reasons. Decoding a nested mapping loses its order —
	// the entries came back alphabetised, so a group's colours and its
	// dry-run listing stopped matching the order they were written in. And
	// re-serialising an entry to decode it separately is unsafe: a value like
	//   api: node -e "a: b"
	// renders back as YAML that parses as a mapping rather than as the string
	// it started as.
	file, err := parser.ParseBytes(b, 0)
	if err != nil {
		return errStepShape
	}

	body := docBody(file)
	if body == nil || len(mappingValues(body)) == 0 {
		return errStepShape
	}
	return s.decodeGroup(body)
}

// decodeGroup reads the mapping form of a step from the syntax tree.
func (s *Step) decodeGroup(n ast.Node) error {
	var (
		entries []ParallelEntry
		prefix  *bool
		seen    bool
	)

	for _, mv := range mappingValues(n) {
		switch keyText(mv.Key) {
		case "parallel":
			pairs := mappingValues(mv.Value)
			if len(pairs) == 0 {
				return errors.New("parallel: must be a mapping of name to what to run, for example\n" +
					"  parallel:\n    api: node api.js\n    worker: node worker.js")
			}

			for _, entry := range pairs {
				st, err := stepFromNode(entry.Value)
				if err != nil {
					return fmt.Errorf("parallel entry %q: %w", keyText(entry.Key), err)
				}
				entries = append(entries, ParallelEntry{Name: keyText(entry.Key), Step: st})
			}
			seen = true

		case "prefix":
			b, ok := mv.Value.(*ast.BoolNode)
			if !ok {
				return errors.New("prefix: must be true or false")
			}
			v := b.Value
			prefix = &v

		default:
			return fmt.Errorf("unknown field %q in a parallel group; only parallel and prefix are allowed",
				keyText(mv.Key))
		}
	}

	if !seen {
		return errStepShape
	}

	s.isGroup = true
	s.Parallel = entries
	s.Prefix = prefix
	return nil
}

// stepFromNode builds a step from a syntax tree node, without going back
// through YAML text.
func stepFromNode(n ast.Node) (Step, error) {
	switch v := n.(type) {
	case *ast.SequenceNode:
		argv := make([]string, 0, len(v.Values))
		for _, elem := range v.Values {
			argv = append(argv, nodeText(elem))
		}
		return Step{Argv: argv}, nil

	case *ast.MappingNode, *ast.MappingValueNode:
		// A group inside a group. Decoded so validation can report it against
		// the right line, rather than rejected here with no position.
		var nested Step
		if err := nested.decodeGroup(n); err != nil {
			return Step{}, err
		}
		return nested, nil

	default:
		return Step{Line: nodeText(n)}, nil
	}
}

// JSONSchema describes the accepted shapes for editor autocomplete.
func (Step) JSONSchema() *jsonschema.Schema {
	return &jsonschema.Schema{
		Title: "Step",
		Description: "One entry in the sequence. A string is run through a shell; a list of " +
			"arguments is executed directly with no shell; a parallel group starts its " +
			"entries at once.",
		OneOf: []*jsonschema.Schema{
			{Type: "string"},
			{Type: "array", Items: &jsonschema.Schema{Type: "string"}, MinItems: ptr(uint64(1))},
			parallelSchema(),
		},
	}
}

func parallelSchema() *jsonschema.Schema {
	props := jsonschema.NewProperties()
	props.Set("parallel", &jsonschema.Schema{
		Type: "object",
		Description: "Things to start at once, keyed by a name that labels their output. " +
			"The group ends when one fails or all of them finish.",
		AdditionalProperties: &jsonschema.Schema{Ref: "#/$defs/Step"},
		MinProperties:        ptr(uint64(1)),
	})
	props.Set("prefix", &jsonschema.Schema{
		Type: "boolean",
		Description: "Label each output line with the entry it came from. On by default. " +
			"Labelling requires capturing the output through a pipe, which makes most " +
			"programs turn their colours off; set this to false to let them write " +
			"straight to the terminal instead.",
	})

	return &jsonschema.Schema{
		Type:                 "object",
		Properties:           props,
		Required:             []string{"parallel"},
		AdditionalProperties: jsonschema.FalseSchema,
	}
}

// UnmarshalYAML preserves the order commands were declared in.
//
// Each command is decoded from its own syntax tree node rather than by
// re-encoding it to YAML first. The earlier round-trip went through a Go map,
// which preserved the order of the commands themselves but silently sorted the
// keys of every mapping nested inside one — so a parallel group's entries came
// back alphabetised however they were written.
func (c *Commands) UnmarshalYAML(b []byte) error {
	file, err := parser.ParseBytes(b, 0)
	if err != nil {
		return err
	}

	body := docBody(file)
	if body == nil {
		return nil
	}

	pairs := mappingValues(body)
	out := make(Commands, 0, len(pairs))

	for _, mv := range pairs {
		name := keyText(mv.Key)

		var cmd Command
		if err := yaml.NodeToValue(mv.Value, &cmd, yaml.Strict()); err != nil {
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
