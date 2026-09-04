package config

import (
	"bufio"
	"bytes"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// The lenient loader exists for one caller: the hidden __complete command that
// Cobra's generated completion scripts invoke on every TAB press.
//
// That path has requirements the strict loader cannot meet. Cobra's PowerShell
// script merges stderr into stdout and casts the last output line to an
// integer, so a single stray parse error either becomes a bogus completion
// candidate or breaks the cast, and TAB then misbehaves in a way no user can
// diagnose. Nothing here may write to stderr, return an error, or panic — and
// a half-written noodge.yaml must still complete the commands it does contain,
// because a config being edited is exactly when completion is most useful.

// LenientCommand is the minimum a completion candidate needs.
type LenientCommand struct {
	// Name is the command name to offer.
	Name string
	// Short is the first line of the description, shown beside the candidate
	// by shells that support it. Empty when unknown.
	Short string
	// Hidden commands are recovered but should not be offered.
	Hidden bool
}

// LoadLenient recovers whatever commands it can from the config above dir.
//
// It never returns an error and never writes anywhere. An empty result means
// "offer nothing extra", which is always a safe answer.
func LoadLenient(dir string) (cmds []LenientCommand) {
	defer func() {
		// A panic here would print a stack trace to stderr, which is the exact
		// failure this whole path exists to avoid.
		if r := recover(); r != nil {
			cmds = nil
		}
	}()

	path, err := Discover(dir)
	if err != nil {
		return nil
	}

	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	if found := lenientFromAST(src); len(found) > 0 {
		return found
	}
	return lenientFromText(src)
}

// lenientFromAST reads command names from a parseable document.
func lenientFromAST(src []byte) []LenientCommand {
	file, err := parser.ParseBytes(src, 0)
	if err != nil {
		return nil
	}

	commands := findValue(file, key("commands"))
	if commands == nil {
		return nil
	}

	var out []LenientCommand
	for _, mv := range mappingValues(commands) {
		name := keyText(mv.Key)
		if name == "" {
			continue
		}

		cmd := LenientCommand{Name: name}
		if desc := pair(mv.Value, "description"); desc != nil {
			cmd.Short = firstLine(nodeText(desc.Value))
		}
		if hidden := pair(mv.Value, "hidden"); hidden != nil {
			cmd.Hidden = strings.EqualFold(nodeText(hidden.Value), "true")
		}
		out = append(out, cmd)
	}

	return out
}

// commandKeyRE matches a command key in a text scan: two spaces of indent,
// a name, a colon, and nothing else of substance on the line.
var commandKeyRE = regexp.MustCompile(`^  ([a-zA-Z0-9][a-zA-Z0-9:._-]*):\s*(#.*)?$`)

// lenientFromText recovers command names by scanning lines, for the case where
// the document does not parse at all. It is deliberately crude: it only has to
// be right often enough to keep TAB useful while a file is being edited.
func lenientFromText(src []byte) []LenientCommand {
	var (
		out       []LenientCommand
		inCommand bool
	)

	scanner := bufio.NewScanner(bytes.NewReader(src))
	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inCommand = strings.HasPrefix(strings.TrimSpace(line), "commands:")
			continue
		}
		if !inCommand {
			continue
		}
		if m := commandKeyRE.FindStringSubmatch(line); m != nil {
			out = append(out, LenientCommand{Name: m[1]})
		}
	}

	return out
}

// nodeText returns a scalar node's text, or an empty string for anything else.
func nodeText(n ast.Node) string {
	if n == nil {
		return ""
	}
	if s, ok := n.(*ast.StringNode); ok {
		return s.Value
	}
	if tok := n.GetToken(); tok != nil {
		return tok.Value
	}
	return ""
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
