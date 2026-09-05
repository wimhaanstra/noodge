// Package cli builds noodge's command tree.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/runner"
)

// Exit codes. A step's exit code passes through verbatim, so noodge's own
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
	Stdin  io.Reader
	// Dir is where config discovery starts. Empty means the working directory.
	Dir string
	// TTY reports whether the browser can be shown. False in a pipe, in CI,
	// and in every test.
	TTY bool
}

// options holds the values of the persistent flags.
type options struct {
	// directory overrides where discovery starts.
	directory string
	// dryRun prints what would run instead of running it.
	dryRun bool
	// assumeYes answers any confirmation prompt with yes, for CI and scripts.
	assumeYes bool
}

func (o *options) startDir(env *Env) (string, error) {
	if o.directory != "" {
		return o.directory, nil
	}
	if env.Dir != "" {
		return env.Dir, nil
	}
	return os.Getwd()
}

// load discovers and loads the config.
func (o *options) load(env *Env) (*config.File, error) {
	if path := os.Getenv("NOODGE_CONFIG"); path != "" {
		return config.Load(path)
	}

	dir, err := o.startDir(env)
	if err != nil {
		return nil, err
	}
	return config.LoadFrom(dir)
}

// configOnlyBuiltins are the commands that must keep working when the config
// is missing or broken, because they are how you fix it.
var configOnlyBuiltins = []string{"version", "schema", "init", "help", "completion", "upgrade"}

// NewRoot builds the command tree, including the commands declared by the
// config that opts points at.
func NewRoot(env *Env, opts *options) (*cobra.Command, error) {
	root := &cobra.Command{
		Use:   "noodge",
		Short: "Run and discover a project's documented commands",
		Long: "noodge runs the commands described in the noodge.yaml for the project you are in.\n\n" +
			"Every command carries its own documentation: what it does, what parameters it\n" +
			"takes, and what it produces. noodge collects no telemetry and makes no network\n" +
			"requests other than an update check you can turn off.",
		SilenceUsage:  true,
		SilenceErrors: true,
		// Cobra's own completion command cannot carry an install subcommand,
		// and its PowerShell output misses the noodge.exe alias.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		// Bare noodge opens the browser when there is a terminal to draw on,
		// and lists otherwise. Listing is not a fallback so much as the
		// correct answer in a pipe, in CI, or under a shell that cannot give
		// a native program a console.
		RunE: func(*cobra.Command, []string) error {
			if env.TTY && !opts.dryRun {
				return runBrowse(env, opts)
			}
			noteIfMintty(env)
			return runList(env, opts, false)
		},
	}

	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)

	// The default is the value already in opts, not "". StringVarP writes the
	// default into the target as it registers, so passing "" here would undo
	// the pre-scan that found -C in the first place, and the flag would be
	// silently ignored.
	root.PersistentFlags().StringVarP(&opts.directory, "directory", "C", opts.directory,
		"start looking for noodge.yaml in this directory instead of the current one")
	root.PersistentFlags().BoolVar(&opts.dryRun, "dry-run", false,
		"print the exact command lines that would run, and run nothing")
	root.PersistentFlags().BoolVar(&opts.assumeYes, "yes", false,
		"answer any confirmation prompt with yes, for CI and scripts")

	root.AddCommand(
		newListCmd(env, opts),
		newValidateCmd(env, opts),
		newSchemaCmd(env),
		newVersionCmd(env),
		newInitCmd(env, opts),
		newCompletionCmd(env, opts),
		newUpgradeCmd(env),
	)

	file, loadErr := opts.load(env)
	if loadErr != nil {
		return root, loadErr
	}

	commands := buildCommands(env, opts, file)
	root.AddCommand(commands...)

	// run addresses a config command unambiguously, which is the escape hatch
	// when one shares a name with a built-in.
	root.AddCommand(newRunCmd(env, opts, file))

	return root, nil
}

// Execute runs the command tree and returns the process exit code.
func Execute(env *Env, args []string) int {
	opts := &options{
		// The config has to be read before the tree can be built, but the flag
		// that says where to read it from is only parsed once the tree exists.
		// Reading it out of the raw arguments first breaks that circle.
		directory: preScanDirectory(args),
	}

	root, loadErr := NewRoot(env, opts)

	if loadErr != nil && !allowedWithoutConfig(args) {
		report(env, loadErr)
		return ExitConfig
	}

	root.SetArgs(args)

	if err := root.Execute(); err != nil {
		// A command asking for a particular exit code has already printed
		// whatever it wanted to say.
		var coded *exitCodeError
		if errors.As(err, &coded) {
			return coded.code
		}

		// A step that failed owns the exit code; noodge does not editorialise.
		var exitErr *runner.ExitError
		if errors.As(err, &exitErr) {
			fmt.Fprintf(env.Stderr, "noodge: %v\n", err)
			return exitErr.Code
		}

		report(env, err)
		return ExitConfig
	}

	return ExitOK
}

// allowedWithoutConfig reports whether the invocation is one that must still
// work when there is no usable config.
func allowedWithoutConfig(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return slices.Contains(configOnlyBuiltins, a)
	}

	// Bare noodge, or only flags: listing handles a missing config itself, and
	// a broken one should be reported.
	return true
}

// preScanDirectory finds -C or --directory in raw arguments.
func preScanDirectory(args []string) string {
	for i, a := range args {
		switch {
		case a == "-C" || a == "--directory":
			if i+1 < len(args) {
				return args[i+1]
			}
		case strings.HasPrefix(a, "--directory="):
			return strings.TrimPrefix(a, "--directory=")
		case strings.HasPrefix(a, "-C="):
			return strings.TrimPrefix(a, "-C=")
		}
	}
	return ""
}

// report prints an error the way its type deserves.
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

// sortedEnv renders an environment map as stable KEY=value lines.
func sortedEnv(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}
