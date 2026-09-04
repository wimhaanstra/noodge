// Command noodge runs the documented commands a project declares in its
// noodge.yaml.
package main

import (
	"os"

	"github.com/wimhaanstra/noodge/internal/cli"
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

	env := &cli.Env{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		TTY:    cli.DetectTTY(),
	}
	os.Exit(cli.Execute(env, args))
}
