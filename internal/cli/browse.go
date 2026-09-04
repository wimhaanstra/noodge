package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/wimhaanstra/noodge/internal/tui"
)

// DetectTTY reports whether the browser can be shown.
//
// Called by main rather than worked out inside the command tree, so tests
// drive the same code paths without ever opening a terminal program.
func DetectTTY() bool {
	return term.IsTerminal(os.Stdout.Fd()) && term.IsTerminal(os.Stdin.Fd())
}

// runBrowse shows the browser, then runs whatever the user chose.
//
// The browser itself never executes anything. It hands back a command name and
// the arguments to run it with, and those go through the same command tree a
// typed invocation goes through — so the command inherits the real terminal,
// keeps its colours and its ability to prompt, and reports its own exit code.
func runBrowse(env *Env, opts *options) error {
	file, err := opts.load(env)
	if err != nil {
		return err
	}

	result, err := tui.Run(file)
	if err != nil {
		return err
	}
	if !result.Chosen() {
		return nil
	}

	// Show the equivalent invocation, so the browser teaches the command line
	// rather than replacing it. It goes to stderr to keep the command's own
	// output clean for a pipe.
	fmt.Fprintf(env.Stderr, "%s\n\n", result.CommandLine())

	root, err := NewRoot(env, opts)
	if err != nil {
		return err
	}
	root.SetArgs(append([]string{result.Command}, result.Args...))

	return root.Execute()
}

// noteIfMintty explains why the browser did not open under Git Bash.
//
// A native Windows binary run under mintty is handed a pipe rather than a
// console, so it cannot tell it is talking to a terminal and cannot enter raw
// mode. Listing is the right fallback, but silently listing looks like a bug,
// so say what happened.
func noteIfMintty(env *Env) {
	if os.Getenv("MSYSTEM") == "" || DetectTTY() {
		return
	}

	fmt.Fprintln(env.Stderr,
		"noodge: not opening the browser - Git Bash gives a native program a pipe rather than a console.")
	fmt.Fprintln(env.Stderr,
		"  hint: run 'winpty noodge', or use Windows Terminal or PowerShell directly.")
}
