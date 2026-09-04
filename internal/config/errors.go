package config

import (
	"fmt"
	"strings"
)

// Severity distinguishes a problem that stops the config being used from one
// that is merely worth telling the author about.
type Severity int

const (
	// SeverityError means the config cannot be used as written.
	SeverityError Severity = iota
	// SeverityWarning means the config works but something is worth fixing.
	// Missing descriptions are warnings: a half-written config is exactly
	// when you least want an argument with your tooling.
	SeverityWarning
)

func (s Severity) String() string {
	if s == SeverityWarning {
		return "warning"
	}
	return "error"
}

// Diagnostic is one problem found in a config file, located as precisely as
// the source allows.
type Diagnostic struct {
	Severity Severity
	// Message is written for the config author, not for a Go caller.
	Message string
	// Hint is an optional suggestion for how to fix it.
	Hint string
	// File is the path to the noodge.yaml the problem was found in.
	File string
	// Line and Col are 1-based. Both are zero when the position could not be
	// resolved, in which case only the file is reported.
	Line, Col int
}

func (d Diagnostic) String() string {
	var b strings.Builder
	if d.Line > 0 {
		fmt.Fprintf(&b, "%s:%d:%d: ", d.File, d.Line, d.Col)
	} else if d.File != "" {
		fmt.Fprintf(&b, "%s: ", d.File)
	}
	b.WriteString(d.Severity.String())
	b.WriteString(": ")
	b.WriteString(d.Message)
	if d.Hint != "" {
		b.WriteString("\n  hint: ")
		b.WriteString(d.Hint)
	}
	return b.String()
}

// Diagnostics is an ordered collection of problems.
type Diagnostics []Diagnostic

// Errors returns only the diagnostics that stop the config being used.
func (d Diagnostics) Errors() Diagnostics { return d.filter(SeverityError) }

// Warnings returns only the advisory diagnostics.
func (d Diagnostics) Warnings() Diagnostics { return d.filter(SeverityWarning) }

func (d Diagnostics) filter(s Severity) Diagnostics {
	var out Diagnostics
	for _, x := range d {
		if x.Severity == s {
			out = append(out, x)
		}
	}
	return out
}

// HasErrors reports whether any diagnostic stops the config being used.
func (d Diagnostics) HasErrors() bool {
	for _, x := range d {
		if x.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Error renders every diagnostic, one per line, so Diagnostics can be
// returned as an error from loading.
func (d Diagnostics) Error() string {
	parts := make([]string, 0, len(d))
	for _, x := range d {
		parts = append(parts, x.String())
	}
	return strings.Join(parts, "\n")
}
