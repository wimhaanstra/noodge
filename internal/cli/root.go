// Package cli builds noodge's command tree.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
)

// Exit codes. A step's own exit code passes through verbatim, so noodge's
// failures use a code it can only mean itself. Every one of these happens
// before any child process starts, so there is no ambiguity in practice.
const (
	// ExitOK is success.
	ExitOK = 0
	// ExitConfig is a bad config, an unknown command, a missing required
	// parameter, or any other refusal to start.
	ExitConfig = 2
)

// Env is everything the command tree needs from the outside world, gathered
// here so tests can drive the CLI without touching the real terminal.
type Env struct {
	Stdout io.Writer
	Stderr io.Writer
	// Dir is where config discovery starts. Empty means the working directory.
	Dir string
}

// options holds the values of the persistent flags.
type options struct {
	// directory overrides where discovery starts.
	directory string
}

// startDir returns the directory config discovery should begin in.
func (o *options) startDir(env *Env) (string, error) {
	if o.directory != "" {
		return o.directory, nil
	}
	if env.Dir != "" {
		return env.Dir, nil
	}
	return os.Getwd()
}

// load discovers and loads the config, turning the two "no usable config"
// cases into errors that already read well.
func (o *options) load(env *Env) (*config.File, error) {
	dir, err := o.startDir(env)
	if err != nil {
		return nil, err
	}

	path := os.Getenv("NOODGE_CONFIG")
	if path != "" {
		return config.Load(path)
	}
	return config.LoadFrom(dir)
}

// NewRoot builds the full command tree.
func NewRoot(env *Env) *cobra.Command {
	opts := &options{}

	root := &cobra.Command{
		Use:   "noodge",
		Short: "Run and discover a project's documented commands",
		Long: "noodge runs the commands described in the noodge.yaml for the project you are in.\n\n" +
			"Every command carries its own documentation: what it does, what parameters it\n" +
			"takes, and what it produces. noodge collects no telemetry and makes no network\n" +
			"requests other than an update check you can turn off.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Bare noodge lists the commands. The full-screen browser arrives in a
		// later milestone; listing is also the required behaviour whenever
		// output is not a terminal, so it is never wasted.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(env, opts, false)
		},
	}

	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)

	root.PersistentFlags().StringVarP(&opts.directory, "directory", "C", "",
		"start looking for noodge.yaml in this directory instead of the current one")

	root.AddCommand(
		newListCmd(env, opts),
		newValidateCmd(env, opts),
		newSchemaCmd(env),
		newVersionCmd(env),
	)

	return root
}

// Execute runs the command tree and returns the process exit code.
func Execute(env *Env, args []string) int {
	root := NewRoot(env)
	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		report(env, err)
		return ExitConfig
	}
	return ExitOK
}

// report prints an error the way its type deserves. A missing config is the
// most common first experience with the tool, so it gets a next step rather
// than just a complaint.
func report(env *Env, err error) {
	// A command that has already printed its own diagnostics returns this, so
	// the summary it just wrote is not followed by a redundant second line.
	if errors.Is(err, errSilent) {
		return
	}

	var notFound *config.NotFoundError
	if errors.As(err, &notFound) {
		fmt.Fprintf(env.Stderr, "noodge: %v\n", err)
		fmt.Fprintln(env.Stderr, "  hint: run 'noodge init' to create one")
		return
	}

	fmt.Fprintf(env.Stderr, "noodge: %v\n", err)
}
