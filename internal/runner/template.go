package runner

import (
	"fmt"
	"strings"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/template"
)

// Value is a parameter after parsing, coercion and defaulting.
type Value struct {
	// Param is the declaration this value belongs to.
	Param config.Param
	// Set reports whether the parameter has a value at all, whether the user
	// supplied it or it came from a default. An unset optional parameter is
	// what makes {{flag x}} disappear.
	Set bool
	// Str is the value in the form it is substituted in.
	Str string
	// Bool is the value of a bool parameter.
	Bool bool
}

// Values is every parameter of one command, keyed by name.
type Values map[string]Value

// ExpandLine substitutes placeholders into a shell step.
//
// Parameter values are quoted; pass-through arguments are not. That asymmetry
// is deliberate: a parameter value is data, and quoting is what keeps it data,
// while pass-through arguments are text the user typed at their own prompt for
// this one invocation, so they keep their shell metacharacters.
func ExpandLine(line string, vals Values, args []string, style QuoteStyle) (string, error) {
	refs, err := template.Parse(line)
	if err != nil {
		return "", err
	}

	var (
		b    strings.Builder
		last int
	)

	for _, ref := range refs {
		b.WriteString(line[last:ref.Start])
		last = ref.End

		text, err := expandRef(ref, vals, args, style, true)
		if err != nil {
			return "", err
		}

		if text == "" {
			// "a {{flag x}} b" must not become "a  b" when x is unset, so an
			// empty expansion takes the space in front of it with it.
			trimTrailingSpace(&b)
			continue
		}
		b.WriteString(text)
	}
	b.WriteString(line[last:])

	return strings.TrimSpace(b.String()), nil
}

// ExpandArgv substitutes placeholders into a step written in list form.
//
// Nothing is quoted here: these arguments are handed to the process directly
// and never touch a shell, which is what makes this the safe form.
//
// An element that is nothing but a placeholder can expand to several
// arguments, or to none: {{flag host}} becomes two elements, {{args}} becomes
// as many as were passed, and an unset optional parameter drops the element
// entirely. An element with text around the placeholder always stays one
// argument, because "--host={{host}}" is meant to arrive glued together.
func ExpandArgv(argv []string, vals Values, args []string) ([]string, error) {
	out := make([]string, 0, len(argv))

	for _, elem := range argv {
		refs, err := template.Parse(elem)
		if err != nil {
			return nil, err
		}

		if len(refs) == 1 && refs[0].Start == 0 && refs[0].End == len(elem) {
			parts, err := expandRefToArgs(refs[0], vals, args)
			if err != nil {
				return nil, err
			}
			out = append(out, parts...)
			continue
		}

		var (
			b    strings.Builder
			last int
		)
		for _, ref := range refs {
			b.WriteString(elem[last:ref.Start])
			last = ref.End

			text, err := expandRef(ref, vals, args, StylePosix, false)
			if err != nil {
				return nil, err
			}
			b.WriteString(text)
		}
		b.WriteString(elem[last:])

		out = append(out, b.String())
	}

	return out, nil
}

// expandRef renders one placeholder as text.
func expandRef(ref template.Ref, vals Values, args []string, style QuoteStyle, quote bool) (string, error) {
	q := func(s string) string {
		if quote {
			return Quote(style, s)
		}
		return s
	}

	if ref.Kind == template.KindArgs {
		// Pass-through arguments are already shell text as the user typed it.
		return strings.Join(args, " "), nil
	}

	v, ok := vals[ref.Name]
	if !ok {
		return "", fmt.Errorf("no parameter called %q", ref.Name)
	}

	isBool := v.Param.ResolvedType() == config.TypeBool

	switch ref.Kind {
	case template.KindFlag:
		switch {
		case isBool && v.Bool:
			return v.Param.Flag, nil
		case isBool:
			return "", nil
		case v.Set:
			return v.Param.Flag + " " + q(v.Str), nil
		default:
			return "", nil
		}

	default: // template.KindValue
		switch {
		case isBool && v.Bool:
			return q("true"), nil
		case isBool:
			return "", nil
		case v.Set:
			return q(v.Str), nil
		default:
			return "", nil
		}
	}
}

// expandRefToArgs renders a placeholder that makes up a whole argv element,
// where one placeholder can become several arguments or none.
func expandRefToArgs(ref template.Ref, vals Values, args []string) ([]string, error) {
	if ref.Kind == template.KindArgs {
		return args, nil
	}

	v, ok := vals[ref.Name]
	if !ok {
		return nil, fmt.Errorf("no parameter called %q", ref.Name)
	}

	isBool := v.Param.ResolvedType() == config.TypeBool

	switch ref.Kind {
	case template.KindFlag:
		switch {
		case isBool && v.Bool:
			return []string{v.Param.Flag}, nil
		case isBool:
			return nil, nil
		case v.Set:
			return []string{v.Param.Flag, v.Str}, nil
		default:
			return nil, nil
		}

	default: // template.KindValue
		switch {
		case isBool && v.Bool:
			return []string{"true"}, nil
		case isBool, !v.Set:
			return nil, nil
		default:
			return []string{v.Str}, nil
		}
	}
}

// trimTrailingSpace drops the whitespace an empty expansion would otherwise
// leave behind. Steps are short, so rebuilding the buffer costs nothing.
func trimTrailingSpace(b *strings.Builder) {
	s := b.String()
	trimmed := strings.TrimRight(s, " \t")
	if trimmed == s {
		return
	}
	b.Reset()
	b.WriteString(trimmed)
}
