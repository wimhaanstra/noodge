// Package template understands noodge's placeholder syntax.
//
// It is deliberately separate from both config validation and step execution,
// because both need to agree exactly on what a placeholder means: validation
// reports a placeholder naming a parameter that does not exist, and execution
// expands the same text. One parser serves both.
package template

import (
	"fmt"
	"regexp"
	"strings"
)

// ArgsName is the reserved placeholder for pass-through arguments, the ones
// typed after a bare "--" on the noodge command line.
const ArgsName = "args"

// nameRE is the set of characters a parameter name may use. It is deliberately
// narrower than YAML allows, so a name is always a usable shell-safe token.
var nameRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`)

// ValidName reports whether s is a usable parameter name.
func ValidName(s string) bool { return nameRE.MatchString(s) }

// Kind distinguishes the forms a placeholder can take.
type Kind int

const (
	// KindValue is {{name}} — the value alone.
	KindValue Kind = iota
	// KindFlag is {{flag name}} — the flag spelling and the value together,
	// or nothing at all when the parameter is unset.
	KindFlag
	// KindArgs is {{args}} — the pass-through arguments.
	KindArgs
)

// Ref is one placeholder found in a template.
type Ref struct {
	Kind Kind
	// Name is the parameter referred to. Empty for KindArgs.
	Name string
	// Start and End bound the placeholder in the source string, so it can be
	// replaced without re-scanning.
	Start, End int
}

// Parse finds every placeholder in s.
//
// A malformed placeholder is an error rather than literal text: writing
// {{flag}} or {{my param}} is always a mistake, and silently treating it as
// text would produce a command line that is wrong in a hard-to-spot way.
func Parse(s string) ([]Ref, error) {
	var refs []Ref

	for i := 0; i < len(s); {
		open := strings.Index(s[i:], "{{")
		if open < 0 {
			break
		}
		open += i

		close := strings.Index(s[open:], "}}")
		if close < 0 {
			return nil, fmt.Errorf("unclosed placeholder: %q", trimForMessage(s[open:]))
		}
		close += open + 2

		inner := strings.TrimSpace(s[open+2 : close-2])
		ref, err := parseInner(inner)
		if err != nil {
			return nil, err
		}
		ref.Start, ref.End = open, close
		refs = append(refs, ref)

		i = close
	}

	return refs, nil
}

func parseInner(inner string) (Ref, error) {
	switch {
	case inner == "":
		return Ref{}, fmt.Errorf("empty placeholder {{}}")

	case inner == ArgsName:
		return Ref{Kind: KindArgs}, nil

	case inner == "flag":
		return Ref{}, fmt.Errorf("{{flag}} needs a parameter name, for example {{flag host}}")

	case strings.HasPrefix(inner, "flag "):
		name := strings.TrimSpace(strings.TrimPrefix(inner, "flag "))
		if !ValidName(name) {
			return Ref{}, fmt.Errorf("{{flag %s}} is not a valid parameter name", name)
		}
		return Ref{Kind: KindFlag, Name: name}, nil

	case !ValidName(inner):
		return Ref{}, fmt.Errorf("{{%s}} is not a valid placeholder", inner)

	default:
		return Ref{Kind: KindValue, Name: inner}, nil
	}
}

func trimForMessage(s string) string {
	if len(s) > 24 {
		return s[:24] + "…"
	}
	return s
}
