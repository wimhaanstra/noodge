package tui

import (
	"strings"

	"github.com/wimhaanstra/noodge/internal/config"
)

// Result is what the browser hands back.
//
// It is a command name and the arguments to run it with, rather than anything
// already executed, because the browser's job ends the moment a choice is
// made. The caller feeds these straight back into the same command tree a
// typed invocation goes through, so there is exactly one implementation of
// parameter coercion, validation and execution — the browser cannot drift away
// from what the command line does.
type Result struct {
	// Command is the chosen command's name. Empty when the user cancelled.
	Command string
	// Args are the flags to run it with, ready to append after the name.
	Args []string
}

// Chosen reports whether the user picked something.
func (r Result) Chosen() bool { return r.Command != "" }

// CommandLine renders the equivalent invocation, so the browser teaches the
// command line rather than hiding it.
func (r Result) CommandLine() string {
	if !r.Chosen() {
		return ""
	}

	parts := make([]string, 0, len(r.Args)+2)
	parts = append(parts, "noodge", r.Command)

	for _, a := range r.Args {
		parts = append(parts, quoteForDisplay(a))
	}
	return strings.Join(parts, " ")
}

// quoteForDisplay wraps an argument in quotes when it would otherwise be
// ambiguous to read or to retype.
func quoteForDisplay(s string) string {
	if s == "" {
		return `""`
	}
	if strings.ContainsAny(s, " \t\"'&|<>") {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return s
}

// visible returns the commands the browser lists, which excludes the ones the
// config marked hidden.
func visible(cmds config.Commands) []config.NamedCommand {
	out := make([]config.NamedCommand, 0, len(cmds))
	for _, nc := range cmds {
		if !nc.Hidden {
			out = append(out, nc)
		}
	}
	return out
}
