package cli

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// shells are the four Cobra can generate for. There are no others: the Cobra
// source tree contains exactly bash, zsh, fish and PowerShell.
var shells = []string{"bash", "zsh", "fish", "powershell"}

func newCompletionCmd(env *Env, opts *options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <shell>",
		Short: "Print the shell completion script",
		Long: `Prints the completion script for a shell.

Completion is per directory: the generated script calls back into noodge on
every TAB, running wherever you are, so the commands it offers are the ones
this project declares. A folder with no noodge.yaml offers nothing, and a
noodge.yaml being edited still completes what it can.

Rather than pasting the output into your profile by hand, 'noodge completion
install <shell>' will write it to a file and add one line to your profile,
showing you the change first.`,
		ValidArgs: shells,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(c *cobra.Command, args []string) error {
			script, err := completionScript(c.Root(), args[0])
			if err != nil {
				return err
			}
			_, err = env.Stdout.Write([]byte(script))
			return err
		},
	}

	cmd.AddCommand(newCompletionInstallCmd(env, opts))
	return cmd
}

// completionScript generates the script for one shell.
func completionScript(root *cobra.Command, shell string) (string, error) {
	var buf bytes.Buffer

	switch shell {
	case "bash":
		if err := root.GenBashCompletionV2(&buf, true); err != nil {
			return "", err
		}
	case "zsh":
		if err := root.GenZshCompletion(&buf); err != nil {
			return "", err
		}
	case "fish":
		if err := root.GenFishCompletion(&buf, true); err != nil {
			return "", err
		}
	case "powershell":
		if err := root.GenPowerShellCompletionWithDesc(&buf); err != nil {
			return "", err
		}
		return withExeAlias(buf.String(), root.Name()), nil
	default:
		return "", fmt.Errorf("unknown shell %q, expected one of %s", shell, strings.Join(shells, ", "))
	}

	return buf.String(), nil
}

// withExeAlias also registers the completer for "noodge.exe".
//
// Cobra registers only the bare root name, so typing "noodge.exe <TAB>" in
// PowerShell silently completes nothing — which looks like completion is
// broken rather than like it was never registered. This is Cobra #1853.
//
// The existing registration line is copied rather than rebuilt, so this keeps
// working if Cobra changes how it names the completer variable.
func withExeAlias(script, name string) string {
	marker := fmt.Sprintf("Register-ArgumentCompleter -CommandName '%s'", name)

	idx := strings.LastIndex(script, marker)
	if idx < 0 {
		return script
	}

	end := strings.IndexByte(script[idx:], '\n')
	if end < 0 {
		end = len(script) - idx
	}
	line := script[idx : idx+end]

	alias := strings.Replace(line, "'"+name+"'", "'"+name+".exe'", 1)
	return strings.TrimRight(script, "\n") + "\n" + alias + "\n"
}
