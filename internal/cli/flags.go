package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/config"
)

// registerFlags adds a command's declared parameters as flags.
//
// Values are registered as strings whatever their declared type, and coerced
// afterwards. Registering an int flag with pflag would mean the flag's own
// default is indistinguishable from a value the user typed, and telling those
// apart is what makes {{flag x}} disappear when a parameter is unset.
func registerFlags(cmd *cobra.Command, nc *config.NamedCommand) {
	freeUpDashH(cmd)

	for _, p := range nc.Params {
		name := strings.TrimPrefix(strings.TrimPrefix(p.Flag, "--"), "-")
		short := strings.TrimPrefix(p.Short, "-")
		usage := flagUsage(p)

		if p.ResolvedType() == config.TypeBool {
			cmd.Flags().BoolP(name, short, false, usage)
			continue
		}

		cmd.Flags().StringP(name, short, "", usage)
		registerValueCompletion(cmd, name, p)
	}
}

// registerValueCompletion decides what TAB offers for a flag's value.
//
// An enum completes its declared values, which is the config author's list
// arriving in the shell for free. Anything that is not a path completes
// nothing rather than falling back to the working directory's files, since
// offering to complete --host with a list of filenames is only ever noise.
func registerValueCompletion(cmd *cobra.Command, name string, p config.Param) {
	switch p.ResolvedType() {
	case config.TypeEnum:
		_ = cmd.RegisterFlagCompletionFunc(name,
			func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return p.Values, cobra.ShellCompDirectiveNoFileComp
			})

	case config.TypePath:
		// Leave the default, which is the shell's own file completion.

	default:
		_ = cmd.RegisterFlagCompletionFunc(name,
			func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveNoFileComp
			})
	}
}

// freeUpDashH stops Cobra claiming -h.
//
// Cobra adds a help flag with the shorthand h unless one named help already
// exists, and pflag panics outright with "unable to redefine 'h' shorthand" if
// a config then declares -h for itself. Registering help without a shorthand
// first means Cobra leaves it alone, -h stays available to config authors, and
// --help still works because Cobra reads the flag by name.
func freeUpDashH(cmd *cobra.Command) {
	if cmd.Flags().Lookup("help") == nil {
		cmd.Flags().Bool("help", false, "help for this command")
	}
}

// flagUsage builds the one-line help text for a parameter.
func flagUsage(p config.Param) string {
	var b strings.Builder
	b.WriteString(firstLine(p.Description))

	var notes []string
	if p.ResolvedType() == config.TypeEnum && len(p.Values) > 0 {
		notes = append(notes, "one of: "+strings.Join(p.Values, ", "))
	}
	if p.Required {
		notes = append(notes, "required")
	}
	if p.Default != nil {
		notes = append(notes, "default: "+stringify(p.Default))
	}

	if len(notes) > 0 {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("(" + strings.Join(notes, "; ") + ")")
	}

	return b.String()
}
