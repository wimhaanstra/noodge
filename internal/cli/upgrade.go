package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/buildinfo"
	"github.com/wimhaanstra/noodge/internal/update"
)

// ExitUpdateAvailable is what 'noodge upgrade --check' returns when a newer
// version exists, so a script can act on it without parsing output.
const ExitUpdateAvailable = 10

func newUpgradeCmd(env *Env) *cobra.Command {
	var (
		checkOnly bool
		force     bool
	)

	cmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Install the latest version of noodge",
		Long: `Downloads the latest release, checks it against the published checksum, and
replaces this binary with it.

Upgrading is never automatic. noodge will tell you when a newer version exists,
at most once a day and only when it is talking to a terminal, but it will not
change itself unless you ask.

If noodge was installed by a package manager, this refuses and tells you the
command to use instead. Replacing it in place would leave that package manager
believing the old version is still installed, and its next update would put it
back.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(c *cobra.Command, _ []string) error {
			return runUpgrade(c.Context(), env, checkOnly, force)
		},
	}

	cmd.Flags().BoolVar(&checkOnly, "check", false,
		"report whether an upgrade is available and change nothing (exit 10 if there is one)")
	cmd.Flags().BoolVar(&force, "force", false,
		"upgrade even when a package manager installed this copy")

	return cmd
}

func runUpgrade(ctx context.Context, env *Env, checkOnly, force bool) error {
	if ctx == nil {
		ctx = context.Background()
	}

	checker := update.NewChecker(buildinfo.Version)

	if checkOnly {
		return runUpgradeCheck(ctx, env, checker)
	}

	up := &update.Upgrader{
		Version: buildinfo.Version,
		Checker: checker,
		Force:   force,
		Out:     env.Stdout,
	}

	err := up.Run(ctx)
	switch {
	case errors.Is(err, update.ErrUpToDate):
		fmt.Fprintf(env.Stdout, "noodge %s is already the latest version\n", buildinfo.Version)
		return nil
	default:
		return err
	}
}

// runUpgradeCheck reports what is available without changing anything.
func runUpgradeCheck(ctx context.Context, env *Env, checker *update.Checker) error {
	latest, err := checker.Fetch(ctx)
	if err != nil {
		return err
	}

	if !update.Newer(buildinfo.Version, latest.Version) {
		fmt.Fprintf(env.Stdout, "noodge %s is the latest version\n", buildinfo.Version)
		return nil
	}

	fmt.Fprintf(env.Stdout, "noodge %s is available (you have %s)\n", latest.Version, buildinfo.Version)
	if latest.NotesURL != "" {
		fmt.Fprintf(env.Stdout, "%s\n", latest.NotesURL)
	}

	// An exit code rather than only text, so a script can branch on it.
	return &exitCodeError{code: ExitUpdateAvailable}
}

// exitCodeError carries a specific exit code out of a command without
// printing anything further.
type exitCodeError struct {
	code int
}

func (e *exitCodeError) Error() string { return "" }

// SkipsUpdateNotice reports whether an invocation should not be followed by
// the "a newer version is available" notice.
//
// Printing it after an upgrade would be actively confusing: the running binary
// still reports the old version, so the notice would advertise the version the
// user had just installed.
func SkipsUpdateNotice(args []string) bool {
	for _, a := range positionalWords(args) {
		return a == "upgrade"
	}
	return false
}
