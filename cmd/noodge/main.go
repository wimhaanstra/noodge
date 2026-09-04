// Command noodge runs the documented commands a project declares in its
// noodge.yaml.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/wimhaanstra/noodge/internal/buildinfo"
	"github.com/wimhaanstra/noodge/internal/cli"
	"github.com/wimhaanstra/noodge/internal/update"
)

func main() {
	args := os.Args[1:]

	// Completion is answered before anything else happens. The generated
	// completion scripts run this on every TAB press and read the last line of
	// output as an integer, so no optional start-up may run here and nothing
	// may reach stderr.
	if cli.IsCompletionRequest(args) {
		os.Exit(cli.Complete(args))
	}

	// Windows cannot delete a running executable, so an upgrade leaves the
	// previous binary alongside the new one. This is the next run.
	update.CleanupOld()

	env := &cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		TTY:    cli.DetectTTY(),
	}

	os.Exit(run(env, args))
}

func run(env *cli.Env, args []string) int {
	checker := update.NewChecker(buildinfo.Version)

	// The check runs alongside the command and is never waited for. A build or
	// a test run gives it more than enough time; a command that exits first
	// simply leaves the cache alone and tries again next time. Either way the
	// user's command is not held up by it.
	notify := !suppressUpdates(args)
	if notify {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go checker.Refresh(ctx)
	}

	code := cli.Execute(env, args)

	// The notice comes from what a previous run already learned, so a cold
	// cache costs nothing and the message simply appears next time.
	if notify {
		if notice := checker.Notice(); notice != "" {
			fmt.Fprintf(os.Stderr, "\n%s\n", notice)
		}
	}

	return code
}

func suppressUpdates(args []string) bool {
	if cli.SkipsUpdateNotice(args) {
		return true
	}

	// stderr rather than stdout: the notice is never part of a command's
	// output, so piping stdout somewhere must not silence it, and piping
	// stderr into a file must.
	suppressed, _ := update.Suppressed(buildinfo.Version, term.IsTerminal(os.Stderr.Fd()))
	return suppressed
}
