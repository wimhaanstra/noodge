package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/runner"
)

// buildCommands turns every command in a config into a Cobra command.
//
// The tree is built at run time from the file in the current project, which is
// what lets tab completion offer a different set of commands in every
// directory: Cobra's completion script calls back into the binary, and the
// binary reads whatever noodge.yaml is next to the caller.
func buildCommands(env *Env, opts *options, file *config.File) []*cobra.Command {
	out := make([]*cobra.Command, 0, len(file.Config.Commands))

	for i := range file.Config.Commands {
		out = append(out, buildCommand(env, opts, file, &file.Config.Commands[i]))
	}
	return out
}

func buildCommand(env *Env, opts *options, file *config.File, nc *config.NamedCommand) *cobra.Command {
	cmd := &cobra.Command{
		Use:     usageLine(nc),
		Short:   firstLine(nc.Description),
		Long:    longHelp(nc),
		Aliases: nc.Aliases,
		Hidden:  nc.Hidden,
		// Positional arguments are how pass-through works, so they are
		// accepted here and checked below, where a useful message is possible.
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	registerFlags(cmd, nc)

	cmd.RunE = func(c *cobra.Command, args []string) error {
		passthrough, err := splitPassthrough(c, args, nc)
		if err != nil {
			return err
		}

		vals, err := resolveValues(c, nc)
		if err != nil {
			return err
		}

		req := &runner.Request{
			File:    file,
			Command: nc,
			Values:  vals,
			Args:    passthrough,
			Stdout:  env.Stdout,
			Stderr:  env.Stderr,
			Stdin:   env.Stdin,
			Color:   env.TTY,
		}

		plan, err := runner.PlanCommand(req)
		if err != nil {
			return err
		}

		if opts.dryRun {
			printPlan(env, nc, plan)
			return nil
		}
		return runner.Run(req, plan)
	}

	return cmd
}

// splitPassthrough separates arguments meant for the wrapped tool from
// arguments typed by mistake.
//
// Everything after a bare "--" is pass-through. Anything before it is a
// mistake, because a noodge command declares its parameters and has no
// positional arguments of its own.
func splitPassthrough(cmd *cobra.Command, args []string, nc *config.NamedCommand) ([]string, error) {
	dash := cmd.ArgsLenAtDash()

	if dash < 0 {
		if len(args) > 0 {
			return nil, fmt.Errorf("unexpected argument %q\n"+
				"  hint: to pass it to the wrapped tool, put it after --, as in 'noodge %s -- %s'",
				args[0], nc.Name, strings.Join(args, " "))
		}
		return nil, nil
	}

	if dash > 0 {
		return nil, fmt.Errorf("unexpected argument %q before --", args[0])
	}
	return args, nil
}

// usageLine shows how pass-through arguments are typed, but only for commands
// that can make use of them.
func usageLine(nc *config.NamedCommand) string {
	return nc.Name + " [flags] [-- args]"
}

// longHelp renders the full documentation for one command: what it does, its
// parameters, what it produces, and where pass-through arguments land.
func longHelp(nc *config.NamedCommand) string {
	var b strings.Builder

	desc := strings.TrimSpace(nc.Description)
	if desc == "" {
		desc = "(no description)"
	}
	b.WriteString(desc)

	if out := strings.TrimSpace(nc.Output); out != "" {
		b.WriteString("\n\nOutput:\n  ")
		b.WriteString(strings.ReplaceAll(out, "\n", "\n  "))
	}

	b.WriteString("\n\nSteps:\n")
	for _, s := range nc.Steps {
		b.WriteString("  " + s.String() + "\n")
	}

	// Where "--" arguments end up is not guessable from the config, so say it
	// per command rather than leaving it to the README.
	b.WriteString("\nArguments after -- are ")
	if runner.UsesArgs(nc.Steps) {
		b.WriteString("substituted where {{args}} appears.")
	} else {
		b.WriteString("appended to the last step.")
	}

	return b.String()
}

// printPlan writes what would run, without running it.
func printPlan(env *Env, nc *config.NamedCommand, plan *runner.Plan) {
	fmt.Fprintf(env.Stdout, "%s would run in %s:\n", nc.Name, plan.Dir)

	for i, s := range plan.Steps {
		if !s.IsGroup() {
			fmt.Fprintf(env.Stdout, "  %d. %s\n", i+1, s.Display)
			continue
		}

		// A group starts one process per entry, so list them rather than
		// collapsing the whole group into a single opaque line.
		fmt.Fprintf(env.Stdout, "  %d. parallel (prefix: %t)\n", i+1, s.Prefix)
		for _, e := range s.Parallel {
			fmt.Fprintf(env.Stdout, "       %s: %s\n", e.Label, e.Display)
		}
	}

	if len(plan.Env) > 0 {
		fmt.Fprintln(env.Stdout, "\nwith environment:")
		for _, kv := range sortedEnv(plan.Env) {
			fmt.Fprintf(env.Stdout, "  %s\n", kv)
		}
	}
}
