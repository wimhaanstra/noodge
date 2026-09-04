// Package runner expands a command's steps and runs them.
package runner

import (
	"fmt"
	"path/filepath"
	"strings"
)

// QuoteStyle is how a value must be quoted to survive a particular shell.
type QuoteStyle int

const (
	// StylePosix is sh, bash, zsh: single quotes, with an embedded quote
	// written as '\''.
	StylePosix QuoteStyle = iota
	// StyleCmd is cmd.exe: double quotes, with an embedded quote doubled.
	StyleCmd
	// StylePowerShell is powershell and pwsh: single quotes, with an embedded
	// quote doubled.
	StylePowerShell
)

// Shell is the interpreter a string step is handed to.
type Shell struct {
	// Argv is the interpreter and the flags that make it take a command
	// string, for example ["cmd", "/c"] or ["sh", "-c"].
	Argv []string
	// Style is how values must be quoted for this interpreter.
	Style QuoteStyle
}

// Name is the interpreter's executable name, without directory or extension.
func (s Shell) Name() string {
	if len(s.Argv) == 0 {
		return ""
	}
	base := filepath.Base(s.Argv[0])
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
}

// ParseShell reads a shell override such as "pwsh -NoProfile -Command".
//
// The quoting style is inferred from the executable name, because getting it
// wrong is not a cosmetic problem: it is the difference between a value being
// data and a value being executable.
func ParseShell(spec string) (Shell, error) {
	fields := strings.Fields(spec)
	if len(fields) == 0 {
		return Shell{}, fmt.Errorf("shell is empty")
	}

	sh := Shell{Argv: fields}

	switch sh.Name() {
	case "cmd":
		sh.Style = StyleCmd
	case "powershell", "pwsh":
		sh.Style = StylePowerShell
	default:
		sh.Style = StylePosix
	}

	return sh, nil
}

// Quote renders v so the shell passes it to the program as one literal
// argument, whatever it contains.
//
// This is applied to every parameter value, with no per-parameter opt-out.
// Values arrive from a terminal and from CI, and a step written as a string
// goes through a shell, so without this "--host 'a && shutdown /s'" would be
// a command, not a value.
func Quote(style QuoteStyle, v string) string {
	switch style {
	case StyleCmd:
		return quoteCmd(v)
	case StylePowerShell:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	default:
		return "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
	}
}

// cmdMeta are the characters cmd.exe acts on rather than passes through.
const cmdMeta = "^&|<>()\"!"

// quoteCmd encodes a value for cmd.exe in two stages.
//
// The difficulty is that two different parsers see the same text, and they
// disagree about escaping. cmd.exe escapes an embedded quote as "", while the
// C runtime in the program being launched escapes it as \". Satisfying either
// one alone breaks the other: "" keeps cmd safe but the receiving program
// splits the value into several arguments, and \" delivers the value intact
// but leaves cmd's quote tracking open, so a & in the value starts a second
// command.
//
// The way out is to stop cmd entering its quoted state at all. First the value
// is quoted the way the C runtime expects, then every character cmd treats
// specially is prefixed with a caret — the quotes included. cmd sees no quotes,
// so every metacharacter is escaped and none can act; what survives its pass is
// exactly the C runtime form, and the program receives one intact argument.
//
// One documented gap remains: cmd expands %VAR% before caret processing, and
// there is no escape for that on a command line. A value containing %PATH% is
// substituted rather than passed through. It cannot run anything — expansion
// produces text, not a command — but it is a surprise, so use the argv step
// form for values that may contain a percent sign, or override the shell.
func quoteCmd(v string) string {
	var b strings.Builder
	b.Grow(len(v) + 8)

	for _, r := range crtQuote(v) {
		if strings.ContainsRune(cmdMeta, r) {
			b.WriteByte('^')
		}
		b.WriteRune(r)
	}

	return b.String()
}

// crtQuote wraps a value the way the Microsoft C runtime parses argv: the
// whole value in quotes, an embedded quote as \", and any run of backslashes
// immediately before a quote doubled.
func crtQuote(v string) string {
	var (
		b         strings.Builder
		backslash int
	)
	b.Grow(len(v) + 2)
	b.WriteByte('"')

	for i := 0; i < len(v); i++ {
		switch c := v[i]; c {
		case '\\':
			backslash++
			b.WriteByte(c)

		case '"':
			// Only backslashes that precede a quote are doubled, which is why
			// they are counted rather than escaped as they are seen.
			b.WriteString(strings.Repeat(`\`, backslash))
			backslash = 0
			b.WriteString(`\"`)

		default:
			backslash = 0
			b.WriteByte(c)
		}
	}

	// A trailing backslash would otherwise escape the closing quote.
	b.WriteString(strings.Repeat(`\`, backslash))
	b.WriteByte('"')

	return b.String()
}
