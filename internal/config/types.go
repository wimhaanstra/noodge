// Package config loads, validates and describes noodge.yaml files.
//
// The doc comments on the exported types in this file are the single source of
// truth for the JSON Schema published for editor autocomplete, so they are
// written for a config author rather than for a Go caller.
package config

// FormatVersion is the highest noodge.yaml format version this binary
// understands. A file declaring a higher version is rejected with a message
// telling the user to upgrade rather than with a parse error.
const FormatVersion = 1

// Config is the root of a noodge.yaml file.
type Config struct {
	// Version is the config format version. Currently always 1.
	Version int `json:"version"`

	// Name is a human-readable name for the project, shown in the TUI header.
	Name string `json:"name,omitempty"`

	// Shell overrides the interpreter used to run string steps. Defaults to
	// "cmd /c" on Windows and "sh -c" everywhere else. A per-command shell
	// takes precedence over this.
	Shell string `json:"shell,omitempty"`

	// Env is applied to every command in this file. A command's own env is
	// merged over the top, so a command can override a single variable
	// without restating the rest.
	Env map[string]string `json:"env,omitempty"`

	// Commands are the runnable commands, kept in the order they appear in
	// the file so the TUI can list them the way they were written.
	Commands Commands `json:"commands"`
}

// Commands is an ordered set of named commands. YAML mappings are unordered by
// specification, but a config author reasonably expects the TUI to list their
// commands in the order they wrote them, so the file order is preserved here.
type Commands []NamedCommand

// NamedCommand pairs a command with the key it was declared under.
type NamedCommand struct {
	// Name is the key the command was declared under, for example "start:local".
	Name string `json:"-"`

	Command
}

// Get returns the command declared under name, and whether it existed.
func (c Commands) Get(name string) (*NamedCommand, bool) {
	for i := range c {
		if c[i].Name == name {
			return &c[i], true
		}
	}
	return nil, false
}

// Names returns every declared command name, in file order.
func (c Commands) Names() []string {
	out := make([]string, 0, len(c))
	for i := range c {
		out = append(out, c[i].Name)
	}
	return out
}

// Command is one runnable, documented entry in a noodge.yaml.
type Command struct {
	// Description explains what the command does. This is the long-form text
	// shown in the TUI's right-hand pane, so it can run to several paragraphs.
	Description string `json:"description,omitempty"`

	// Params are the parameters this command accepts. They are validated and
	// type-coerced before any step runs, then substituted into the steps.
	Params []Param `json:"params,omitempty"`

	// Steps are run in order, each as its own process. The command stops at
	// the first step that exits non-zero and reports that exit code. A step
	// may itself be a parallel group, for running several services together.
	Steps []Step `json:"steps"`

	// Output describes what the command produces: what it writes to stdout,
	// what files it leaves behind, what a reader should expect to see. It is
	// documentation only and noodge never verifies it.
	Output string `json:"output,omitempty"`

	// Env is set for this command's steps, merged over the file-level env.
	Env map[string]string `json:"env,omitempty"`

	// Cwd is the working directory for this command's steps, relative to the
	// directory holding the noodge.yaml. Defaults to that directory.
	Cwd string `json:"cwd,omitempty"`

	// Aliases are alternative names this command can be invoked by.
	Aliases []string `json:"aliases,omitempty"`

	// Hidden keeps the command out of the TUI list and out of tab completion.
	// It remains runnable by name. Use it for internal or CI-only commands.
	Hidden bool `json:"hidden,omitempty"`

	// Confirm asks the user to confirm before this command runs. Write true for
	// a default prompt, or a string to use as the prompt itself. Omit it, or
	// write false, to run without asking. Use it for destructive or
	// irreversible commands. Confirmation is skipped by --yes and by --dry-run,
	// and without a terminal to ask at the command refuses unless --yes is given.
	Confirm Confirm `json:"confirm,omitempty"`

	// Shell overrides the interpreter for this command's string steps.
	Shell string `json:"shell,omitempty"`
}

// Confirm controls whether noodge asks the user to confirm before a command
// runs. In a noodge.yaml it is written as either a bool or a string:
//
//	confirm: true                      # ask with a default prompt
//	confirm: Wipe the production DB?    # ask with this prompt
//
// It is meant for destructive or irreversible commands.
type Confirm struct {
	// Required reports whether a confirmation is asked for before running.
	Required bool `json:"-"`
	// Prompt is the question shown. Empty means noodge uses a default.
	Prompt string `json:"-"`
}

// ParamType is the type of a parameter's value.
type ParamType string

// The supported parameter types.
const (
	// TypeString is any text. The default when type is omitted.
	TypeString ParamType = "string"
	// TypeInt is a whole number.
	TypeInt ParamType = "int"
	// TypeNumber is a number that may have a fractional part.
	TypeNumber ParamType = "number"
	// TypeBool is true or false. A bool parameter takes no value on the
	// command line: passing the flag sets it.
	TypeBool ParamType = "bool"
	// TypePath is a filesystem path. Leading ~ is expanded, and a required
	// path is checked for existence before the command runs.
	TypePath ParamType = "path"
	// TypeEnum is one of a fixed list given in values.
	TypeEnum ParamType = "enum"
)

// ParamTypes lists every valid parameter type, for validation messages.
var ParamTypes = []ParamType{TypeString, TypeInt, TypeNumber, TypeBool, TypePath, TypeEnum}

// Param is one parameter a command accepts.
type Param struct {
	// Name is the template variable this parameter fills. A step referring to
	// {{host}} or {{flag host}} is referring to the parameter named "host".
	Name string `json:"name"`

	// Flag is how the parameter is typed on the noodge command line, written
	// in full including the leading dashes, for example "--host". It must use
	// two dashes; single-dash long flags cannot be expressed.
	//
	// This is independent of how the flag reaches the wrapped tool. A step is
	// free to write "-host {{host}}" or "/p:Host={{host}}" instead.
	Flag string `json:"flag"`

	// Short is an optional single-character shorthand, written with one dash,
	// for example "-c".
	Short string `json:"short,omitempty"`

	// Type is the value's type. Defaults to string.
	Type ParamType `json:"type,omitempty"`

	// Description explains what the parameter controls. Shown beside the
	// field in the TUI and in the command's help text.
	Description string `json:"description,omitempty"`

	// Required refuses to run the command unless the parameter is given.
	Required bool `json:"required,omitempty"`

	// Default is used when the parameter is not supplied. A parameter with a
	// default is never unset, so {{flag x}} always expands for it.
	Default any `json:"default,omitempty"`

	// Values is the list of allowed values for an enum parameter. It also
	// supplies the candidates for tab completion.
	Values []string `json:"values,omitempty"`

	// Pattern is an optional regular expression the value must match. It is
	// checked before the value is substituted into any step.
	Pattern string `json:"pattern,omitempty"`
}

// ResolvedType returns the parameter's type, applying the string default.
func (p Param) ResolvedType() ParamType {
	if p.Type == "" {
		return TypeString
	}
	return p.Type
}
