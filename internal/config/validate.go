package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/wimhaanstra/noodge/internal/template"
)

// BuiltinNames are the command names noodge reserves for itself. A config may
// still declare a command with one of these names — it is reachable as
// "noodge run <name>" — but the built-in wins for the bare form.
//
// internal/cli registers exactly these names; keeping the list here lets
// validation warn about a shadowed command without cli and config needing to
// import each other.
var BuiltinNames = []string{
	"completion", "help", "init", "list", "run", "schema", "upgrade", "validate", "version",
}

// commandNameRE is the set of characters a command name may use. Colons are
// allowed because "start:local" is the idiom this tool was built around.
var commandNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9:._-]*$`)

// entryNameRE is what a parallel entry may be called. It is narrower than a
// command name because the name is printed in front of every line of that
// entry's output, so it needs to stay short and unambiguous.
var entryNameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// Validate checks a loaded config for semantic problems: things that parse as
// YAML but do not describe a working set of commands.
//
// Every diagnostic carries a line and column where one could be resolved.
// Errors mean the config cannot be used; warnings are advisory and never block
// execution.
func Validate(f *File) Diagnostics {
	var d Diagnostics
	cfg := f.Config

	if cfg.Version == 0 {
		d = append(d, f.diagKey(SeverityWarning,
			"no version declared, assuming version 1",
			fmt.Sprintf("add %q at the top of the file", fmt.Sprintf("version: %d", FormatVersion)),
			key("version")))
	}

	if len(cfg.Commands) == 0 {
		d = append(d, f.diagKey(SeverityError,
			"no commands declared",
			"a noodge.yaml needs at least one entry under commands:",
			key("commands")))
		return d
	}

	seen := map[string]bool{}
	aliases := map[string]string{}

	for i := range cfg.Commands {
		d = append(d, validateCommand(f, &cfg.Commands[i], seen, aliases)...)
	}

	return d
}

func validateCommand(f *File, nc *NamedCommand, seen map[string]bool, aliases map[string]string) Diagnostics {
	var d Diagnostics
	name := nc.Name
	at := []step{key("commands"), key(name)}

	if !commandNameRE.MatchString(name) {
		d = append(d, f.diagKey(SeverityError,
			fmt.Sprintf("command name %q contains characters that cannot be typed as a command", name),
			"use letters, digits, and any of : . _ -",
			at...))
	}

	if seen[name] {
		d = append(d, f.diagKey(SeverityError,
			fmt.Sprintf("command %q is declared more than once", name), "", at...))
	}
	seen[name] = true

	if slices.Contains(BuiltinNames, name) {
		d = append(d, f.diagKey(SeverityWarning,
			fmt.Sprintf("command %q has the same name as a built-in, so plain 'noodge %s' runs the built-in", name, name),
			fmt.Sprintf("run yours with 'noodge run %s', or rename it", name),
			at...))
	}

	if strings.TrimSpace(nc.Description) == "" {
		d = append(d, f.diagKey(SeverityWarning,
			fmt.Sprintf("command %q has no description", name),
			"the description is what the TUI shows in its right-hand pane",
			at...))
	}

	aliasPath := append(slices.Clone(at), key("aliases"))
	for _, alias := range nc.Aliases {
		if !commandNameRE.MatchString(alias) {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("alias %q contains characters that cannot be typed as a command", alias),
				"", aliasPath...))
			continue
		}
		if owner, ok := aliases[alias]; ok {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("alias %q is already used by command %q", alias, owner),
				"", aliasPath...))
		}
		aliases[alias] = name
	}

	d = append(d, validateCwd(f, nc, at)...)
	d = append(d, validateParams(f, nc, at)...)
	d = append(d, validateSteps(f, nc, at)...)

	return d
}

func validateCwd(f *File, nc *NamedCommand, at []step) Diagnostics {
	if nc.Cwd == "" {
		return nil
	}

	path := append(slices.Clone(at), key("cwd"))

	if filepath.IsAbs(nc.Cwd) {
		return Diagnostics{f.diag(SeverityError,
			fmt.Sprintf("cwd %q must be relative to the noodge.yaml, not absolute", nc.Cwd),
			"an absolute path stops the config working on anyone else's machine",
			path...)}
	}

	// A missing directory is a warning, not an error: it may be created by an
	// earlier command in the same workflow.
	if info, err := os.Stat(filepath.Join(f.Dir, nc.Cwd)); err != nil || !info.IsDir() {
		return Diagnostics{f.diag(SeverityWarning,
			fmt.Sprintf("cwd %q does not exist yet", nc.Cwd), "", path...)}
	}

	return nil
}

func validateParams(f *File, nc *NamedCommand, at []step) Diagnostics {
	var d Diagnostics
	names := map[string]bool{}
	flags := map[string]string{}
	shorts := map[string]string{}

	for i, p := range nc.Params {
		path := append(slices.Clone(at), key("params"), index(i))

		switch {
		case p.Name == "":
			d = append(d, f.diag(SeverityError, "parameter has no name", "", path...))
		case !template.ValidName(p.Name):
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parameter name %q cannot be used in a placeholder", p.Name),
				"start with a letter or underscore, then letters, digits, _ or -",
				append(slices.Clone(path), key("name"))...))
		case names[p.Name]:
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parameter %q is declared twice", p.Name), "",
				append(slices.Clone(path), key("name"))...))
		}
		names[p.Name] = true

		d = append(d, validateFlag(f, p, path, flags, shorts)...)
		d = append(d, validateParamType(f, p, path)...)

		if strings.TrimSpace(p.Description) == "" {
			d = append(d, f.diag(SeverityWarning,
				fmt.Sprintf("parameter %q has no description", p.Name),
				"the description is shown beside the field in the TUI",
				path...))
		}

		if p.Required && p.Default != nil {
			d = append(d, f.diag(SeverityWarning,
				fmt.Sprintf("parameter %q is required but also has a default, so the default can never apply", p.Name),
				"drop required: true, or drop the default",
				append(slices.Clone(path), key("default"))...))
		}

		if p.Pattern != "" {
			if _, err := regexp.Compile(p.Pattern); err != nil {
				d = append(d, f.diag(SeverityError,
					fmt.Sprintf("parameter %q has an invalid pattern: %v", p.Name, err), "",
					append(slices.Clone(path), key("pattern"))...))
			}
		}
	}

	return d
}

func validateFlag(f *File, p Param, path []step, flags, shorts map[string]string) Diagnostics {
	var d Diagnostics
	flagPath := append(slices.Clone(path), key("flag"))

	switch {
	case p.Flag == "":
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("parameter %q has no flag", p.Name),
			fmt.Sprintf("add flag: --%s", p.Name), path...))

	case !strings.HasPrefix(p.Flag, "--"):
		// This is the confusing one, so the hint spells out the distinction
		// between how you type it and how the wrapped tool receives it.
		hint := fmt.Sprintf("write it as --%s. This is only how you type it to noodge; "+
			"a step is still free to write %s {{%s}} to pass it on with a single dash",
			strings.TrimLeft(p.Flag, "-"), p.Flag, p.Name)
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("flag %q must start with two dashes", p.Flag), hint, flagPath...))

	case strings.ContainsAny(p.Flag, " \t"):
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("flag %q cannot contain spaces", p.Flag), "", flagPath...))

	default:
		if owner, ok := flags[p.Flag]; ok {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("flag %q is already used by parameter %q", p.Flag, owner), "", flagPath...))
		}
		flags[p.Flag] = p.Name
	}

	if p.Short == "" {
		return d
	}

	shortPath := append(slices.Clone(path), key("short"))
	trimmed := strings.TrimPrefix(p.Short, "-")

	switch {
	case !strings.HasPrefix(p.Short, "-") || strings.HasPrefix(p.Short, "--"):
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("short flag %q must be written with a single dash", p.Short),
			fmt.Sprintf("write it as -%s", strings.TrimLeft(p.Short, "-")), shortPath...))
	case len([]rune(trimmed)) != 1:
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("short flag %q must be a single character", p.Short), "", shortPath...))
	default:
		if owner, ok := shorts[p.Short]; ok {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("short flag %q is already used by parameter %q", p.Short, owner), "", shortPath...))
		}
		shorts[p.Short] = p.Name
	}

	return d
}

func validateParamType(f *File, p Param, path []step) Diagnostics {
	var d Diagnostics
	t := p.ResolvedType()

	if !slices.Contains(ParamTypes, t) {
		names := make([]string, 0, len(ParamTypes))
		for _, pt := range ParamTypes {
			names = append(names, string(pt))
		}
		return Diagnostics{f.diag(SeverityError,
			fmt.Sprintf("parameter %q has unknown type %q", p.Name, t),
			"valid types are "+strings.Join(names, ", "),
			append(slices.Clone(path), key("type"))...)}
	}

	if t == TypeEnum && len(p.Values) == 0 {
		d = append(d, f.diag(SeverityError,
			fmt.Sprintf("parameter %q is an enum but lists no values", p.Name),
			"add a values: list of the allowed values", path...))
	}
	if t != TypeEnum && len(p.Values) > 0 {
		d = append(d, f.diag(SeverityWarning,
			fmt.Sprintf("parameter %q lists values but is not an enum, so they are ignored", p.Name),
			"add type: enum to enforce them",
			append(slices.Clone(path), key("values"))...))
	}

	if p.Default != nil {
		if msg := defaultMismatch(t, p.Values, p.Default); msg != "" {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parameter %q: %s", p.Name, msg), "",
				append(slices.Clone(path), key("default"))...))
		}
	}

	return d
}

// defaultMismatch returns a message when a default cannot be a value of the
// declared type, or an empty string when it is fine.
func defaultMismatch(t ParamType, values []string, def any) string {
	switch t {
	case TypeBool:
		if _, ok := def.(bool); !ok {
			return fmt.Sprintf("default %v is not true or false", def)
		}

	case TypeInt:
		switch v := def.(type) {
		case int, int64, uint64:
		case float64:
			if v != float64(int64(v)) {
				return fmt.Sprintf("default %v is not a whole number", def)
			}
		default:
			return fmt.Sprintf("default %v is not a number", def)
		}

	case TypeNumber:
		switch def.(type) {
		case int, int64, uint64, float64:
		default:
			return fmt.Sprintf("default %v is not a number", def)
		}

	case TypeEnum:
		s, ok := def.(string)
		if !ok {
			return fmt.Sprintf("default %v is not one of the listed values", def)
		}
		if len(values) > 0 && !slices.Contains(values, s) {
			return fmt.Sprintf("default %q is not one of the listed values (%s)", s, strings.Join(values, ", "))
		}
	}

	return ""
}

func validateSteps(f *File, nc *NamedCommand, at []step) Diagnostics {
	var d Diagnostics

	if len(nc.Steps) == 0 {
		return Diagnostics{f.diagKey(SeverityError,
			fmt.Sprintf("command %q has no steps, so there is nothing to run", nc.Name),
			"add a steps: list", at...)}
	}

	stepsPath := append(slices.Clone(at), key("steps"))

	declared := map[string]Param{}
	for _, p := range nc.Params {
		declared[p.Name] = p
	}
	used := map[string]bool{}

	for i, s := range nc.Steps {
		path := append(slices.Clone(stepsPath), index(i))

		if s.IsZero() {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("step %d is empty", i+1), "", path...))
			continue
		}

		if s.IsParallel() {
			d = append(d, validateGroup(f, s, i, path)...)
		}

		for _, text := range stepTexts(s) {
			refs, err := template.Parse(text)
			if err != nil {
				d = append(d, f.diag(SeverityError,
					fmt.Sprintf("step %d: %v", i+1, err), "", path...))
				continue
			}

			for _, ref := range refs {
				if ref.Kind == template.KindArgs {
					continue
				}

				p, ok := declared[ref.Name]
				if !ok {
					d = append(d, f.diag(SeverityError,
						fmt.Sprintf("step %d refers to {{%s}}, but command %q declares no parameter called %q",
							i+1, ref.Name, nc.Name, ref.Name),
						"add it under params:, or correct the name", path...))
					continue
				}
				used[ref.Name] = true

				if ref.Kind == template.KindValue && !p.Required && p.Default == nil {
					d = append(d, f.diag(SeverityWarning,
						fmt.Sprintf("step %d uses {{%s}}, but %q is optional with no default, so it expands to nothing",
							i+1, ref.Name, ref.Name),
						fmt.Sprintf("if you meant the flag and its value, use {{flag %s}}, which disappears entirely when unset", ref.Name),
						path...))
				}
			}
		}
	}

	for i, p := range nc.Params {
		if p.Name != "" && !used[p.Name] {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parameter %q is declared but no step uses it", p.Name),
				fmt.Sprintf("add {{flag %s}} to a step, or remove the parameter", p.Name),
				append(slices.Clone(at), key("params"), index(i))...))
		}
	}

	return d
}

// validateGroup checks the structure of a parallel group.
func validateGroup(f *File, s Step, index int, path []step) Diagnostics {
	var d Diagnostics

	if len(s.Parallel) == 0 {
		return Diagnostics{f.diag(SeverityError,
			fmt.Sprintf("step %d is a parallel group with no entries", index+1),
			"list the things to run at once under parallel:", path...)}
	}

	seen := map[string]bool{}

	for _, entry := range s.Parallel {
		switch {
		case !entryNameRE.MatchString(entry.Name):
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("step %d has a parallel entry named %q, which cannot be used as a label", index+1, entry.Name),
				"use letters, digits, and any of . _ -", path...))

		case seen[entry.Name]:
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("step %d declares the parallel entry %q twice", index+1, entry.Name),
				"", path...))
		}
		seen[entry.Name] = true

		if entry.Step.IsZero() {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parallel entry %q has nothing to run", entry.Name), "", path...))
		}

		// Nesting would mean groups inside groups, each with its own failure
		// and output rules. One level covers running services together, and
		// anything more is better expressed as a command that this one calls.
		if entry.Step.IsParallel() {
			d = append(d, f.diag(SeverityError,
				fmt.Sprintf("parallel entry %q nests another parallel group", entry.Name),
				"move the inner group into its own command and call it from here", path...))
		}
	}

	if len(s.Parallel) == 1 {
		d = append(d, f.diag(SeverityWarning,
			fmt.Sprintf("step %d is a parallel group with one entry, which runs it the same as an ordinary step", index+1),
			"", path...))
	}

	return d
}

// stepTexts returns every command text in a step, including the entries of a
// parallel group, so placeholder checking covers all of them.
func stepTexts(s Step) []string {
	switch {
	case s.IsParallel():
		var out []string
		for _, entry := range s.Parallel {
			out = append(out, stepTexts(entry.Step)...)
		}
		return out
	case s.IsArgv():
		return s.Argv
	default:
		return []string{s.Line}
	}
}
