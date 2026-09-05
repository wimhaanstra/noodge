package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
)

// Everything in this file runs on a TAB press, and the rules are unusually
// strict because of how the generated completion scripts consume the output.
//
// Cobra's PowerShell script merges stderr into stdout and casts the last line
// to an integer:
//
//	Invoke-Expression -OutVariable out "$RequestComp" 2>&1 | Out-Null
//	[int]$Directive = $Out[-1].TrimStart(':')
//
// So a single stray log line, a YAML parse error or a panic trace either
// becomes a bogus completion candidate or breaks that cast, and TAB then
// misbehaves in a way no user can diagnose. Nothing here may write to stderr,
// return an error, or exit non-zero, and the last line must always be a
// directive.

// completionVerbs are the hidden commands the generated scripts invoke.
var completionVerbs = []string{
	cobra.ShellCompRequestCmd,       // __complete
	cobra.ShellCompNoDescRequestCmd, // __completeNoDesc
}

// directiveNoFileComp is the fallback response: no candidates, and do not fall
// back to offering the user's files. Emitted whenever nothing better can be
// worked out, because it is always a valid answer.
const directiveNoFileComp = ":4"

// IsCompletionRequest reports whether these arguments are a completion
// request. main checks this before doing anything else, so the completion path
// skips every optional piece of start-up.
func IsCompletionRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, verb := range completionVerbs {
		if args[0] == verb {
			return true
		}
	}
	return false
}

// Complete answers a completion request. It always returns 0.
func Complete(args []string) int {
	_, _ = os.Stdout.WriteString(completionResponse(args))
	return 0
}

// completionResponse builds the reply the shell will read.
func completionResponse(args []string) string {
	out := completeQuietly(args)

	// Guarantee the contract no matter what happened above: the last line the
	// shell sees must be a directive it can parse as an integer. Without this,
	// a failure anywhere upstream would break TAB rather than merely offer
	// nothing.
	if !endsWithDirective(out) {
		out += directiveNoFileComp + "\n"
	}

	return out
}

// completeQuietly produces the completion response, swallowing everything that
// could otherwise reach the terminal.
func completeQuietly(args []string) (out string) {
	// The output is buffered rather than streamed so a failure part-way
	// through cannot leave a half-written response, whose last line would not
	// be a directive.
	var buf bytes.Buffer

	defer func() {
		if r := recover(); r != nil {
			// A panic trace on stderr is exactly the failure this path exists
			// to prevent, so it is dropped in silence.
			out = ""
		}
	}()

	env := &Env{Stdout: &buf, Stderr: io.Discard}
	opts := &options{directory: preScanDirectory(args), file: preScanFile(args)}

	// The root is usable even when loading fails: it carries the built-in
	// commands, and those are how you fix a broken config, so they must stay
	// completable. Only the commands from the file are missing.
	root, loadErr := NewRoot(env, opts)

	root.SetOut(&buf)
	root.SetErr(io.Discard)
	root.SetArgs(args)
	_ = root.Execute()

	if loadErr == nil {
		return buf.String()
	}

	// The config is missing or will not parse. A config being edited is
	// exactly when completion matters most, so add back whatever names can be
	// recovered from it alongside the built-ins.
	return withLenientNames(buf.String(), args, opts, env)
}

// withLenientNames merges command names recovered from an unparseable config
// into a response, keeping the directive as the last line.
func withLenientNames(response string, args []string, opts *options, env *Env) string {
	// Only the first word can be completed from a broken config: without a
	// parsed command there is no way to know what flags it takes.
	words := positionalWords(args[1:])
	if len(words) != 1 {
		return response
	}
	prefix := words[0]

	dir, err := opts.startDir(env)
	if err != nil {
		return response
	}

	lines, directive := splitDirective(response)

	seen := map[string]bool{}
	for _, l := range lines {
		seen[strings.SplitN(l, "\t", 2)[0]] = true
	}

	for _, c := range config.LoadLenient(dir) {
		if c.Hidden || seen[c.Name] || !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		if c.Short == "" {
			lines = append(lines, c.Name)
			continue
		}
		lines = append(lines, fmt.Sprintf("%s\t%s", c.Name, c.Short))
	}

	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l)
		b.WriteString("\n")
	}
	b.WriteString(directive)
	b.WriteString("\n")

	return b.String()
}

// valueFlags are the global flags that take a separate value, so the word
// after them is that value rather than a command name.
var valueFlags = map[string]bool{"-C": true, "--directory": true, "--file": true}

// positionalWords drops flags from a partially typed command line, leaving the
// words that could be command names.
//
// Without this, "noodge -C ../other <TAB>" looks like three words rather than
// one, and the completer decides it is too late to offer command names.
func positionalWords(args []string) []string {
	var out []string

	for i := 0; i < len(args); i++ {
		a := args[i]

		if valueFlags[a] {
			i++ // skip the value as well
			continue
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			continue
		}
		out = append(out, a)
	}

	return out
}

// splitDirective separates the candidate lines from the trailing directive.
func splitDirective(response string) (lines []string, directive string) {
	directive = directiveNoFileComp

	for _, l := range strings.Split(strings.TrimRight(response, "\n"), "\n") {
		switch {
		case strings.TrimSpace(l) == "":
		case strings.HasPrefix(l, ":"):
			directive = l
		default:
			lines = append(lines, l)
		}
	}

	return lines, directive
}

// endsWithDirective reports whether the last non-empty line is one.
func endsWithDirective(s string) bool {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		return strings.HasPrefix(lines[i], ":")
	}
	return false
}
