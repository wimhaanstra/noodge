package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// Installing completion means editing a file the user curates, so it is a
// deliberate subcommand rather than something install.ps1 does behind their
// back. It shows the change, takes a backup, and is safe to run twice.
//
// The script is written to its own file and the profile only gains one line
// that loads it. Appending the whole script to a profile works once and then
// cannot be upgraded without hand-editing.

const (
	markerStart = "# >>> noodge completion >>>"
	markerEnd   = "# <<< noodge completion <<<"
)

// installTargets are what install accepts. It is the four shells plus the two
// PowerShell editions named individually, because on Windows they have
// separate profiles and "powershell" alone is often ambiguous.
var installTargets = append(append([]string{}, shells...), "pwsh", "windows-powershell")

func newCompletionInstallCmd(env *Env, opts *options) *cobra.Command {
	var printOnly, assumeYes bool

	cmd := &cobra.Command{
		Use:   "install <shell>",
		Short: "Write the completion script and load it from your shell profile",
		Long: `Writes the completion script to its own file and adds one line to your shell
profile that loads it.

The change is shown before anything is written, your profile is backed up
first, and running this twice makes no second change.`,
		ValidArgs: installTargets,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(c *cobra.Command, args []string) error {
			return runCompletionInstall(c.Root(), env, args[0], printOnly, assumeYes)
		},
	}

	cmd.Flags().BoolVar(&printOnly, "print-only", false,
		"show what would change and write nothing")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false,
		"do not ask for confirmation")

	return cmd
}

// installPlan is what installation would do, worked out before anything is
// written so it can be shown and confirmed.
type installPlan struct {
	// ScriptPath is where the completion script goes.
	ScriptPath string
	// ProfilePath is the file that gains a line. Empty when the shell loads
	// completions from a directory and needs no profile edit.
	ProfilePath string
	// Line is what gets added to the profile.
	Line string
}

func runCompletionInstall(root *cobra.Command, env *Env, shell string, printOnly, assumeYes bool) error {
	script, err := completionScript(root, shell)
	if err != nil {
		return err
	}

	plan, err := planInstall(shell)
	if err != nil {
		return err
	}

	fmt.Fprintf(env.Stdout, "completion script:  %s\n", plan.ScriptPath)
	if plan.ProfilePath == "" {
		fmt.Fprintf(env.Stdout, "profile:            not needed, %s loads this directory automatically\n", shell)
	} else {
		already, err := profileHasMarker(plan.ProfilePath)
		if err != nil {
			return err
		}
		if already {
			fmt.Fprintf(env.Stdout, "profile:            %s (already set up, will be left alone)\n", plan.ProfilePath)
		} else {
			fmt.Fprintf(env.Stdout, "profile:            %s\n", plan.ProfilePath)
			fmt.Fprintf(env.Stdout, "\nwould add:\n\n  %s\n  %s\n  %s\n", markerStart, plan.Line, markerEnd)
		}
	}

	if printOnly {
		return nil
	}
	if !assumeYes && !confirm(env) {
		fmt.Fprintln(env.Stdout, "nothing written")
		return nil
	}

	if err := writeScript(plan.ScriptPath, script); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "wrote %s\n", plan.ScriptPath)

	if plan.ProfilePath != "" {
		changed, err := appendToProfile(plan)
		if err != nil {
			return err
		}
		if changed {
			fmt.Fprintf(env.Stdout, "updated %s\n", plan.ProfilePath)
		}
	}

	fmt.Fprintln(env.Stdout, "\nopen a new shell for completion to take effect")
	if shell == "powershell" {
		// The default Windows Tab binding cycles matches inline one at a time
		// and cannot show descriptions, which for this tool is most of what
		// completion is worth.
		fmt.Fprintln(env.Stdout, "\ntip: descriptions only show with a menu-style Tab:")
		fmt.Fprintln(env.Stdout, "  Set-PSReadLineKeyHandler -Key Tab -Function MenuComplete")
	}

	return nil
}

// planInstall works out the paths for a shell without writing anything.
func planInstall(shell string) (installPlan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return installPlan{}, err
	}

	switch shell {
	case "fish":
		// fish loads every file in this directory, so no profile edit at all.
		return installPlan{
			ScriptPath: filepath.Join(home, ".config", "fish", "completions", "noodge.fish"),
		}, nil

	case "bash":
		script := filepath.Join(home, ".noodge", "completion.bash")
		return installPlan{
			ScriptPath:  script,
			ProfilePath: filepath.Join(home, ".bashrc"),
			Line:        fmt.Sprintf("[ -f %s ] && . %s", shQuote(script), shQuote(script)),
		}, nil

	case "zsh":
		script := filepath.Join(home, ".noodge", "completion.zsh")
		return installPlan{
			ScriptPath:  script,
			ProfilePath: filepath.Join(home, ".zshrc"),
			Line:        fmt.Sprintf("[ -f %s ] && . %s", shQuote(script), shQuote(script)),
		}, nil

	case "pwsh", "windows-powershell", "powershell":
		return planPowerShell(shell)
	}

	return installPlan{}, fmt.Errorf("unknown shell %q", shell)
}

// psQuote renders a path as a PowerShell literal string.
//
// Not Go's %q: PowerShell has no backslash escapes, so a Go-quoted Windows path
// arrives as C:\\Users\\... with the doubled separators taken literally. Single
// quotes are literal in PowerShell, and an embedded quote is doubled.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// shQuote renders a path as a POSIX shell literal string.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// powerShellEditions maps an explicit install target to the interpreter whose
// profile it means.
var powerShellEditions = map[string]string{
	"pwsh":               "pwsh",
	"windows-powershell": "powershell",
}

// planPowerShell works out which PowerShell to set up.
//
// Windows commonly has both editions installed and they do not share a
// profile: PowerShell 7 uses Documents\PowerShell, Windows PowerShell 5.1 uses
// Documents\WindowsPowerShell. Picking one silently means whoever runs the
// other gets no completion and no explanation of why, which is a far worse
// failure than being asked to say which one they meant.
func planPowerShell(target string) (installPlan, error) {
	if exe, ok := powerShellEditions[target]; ok {
		profile, err := powerShellProfile(exe)
		if err != nil {
			return installPlan{}, err
		}
		return powerShellPlan(profile), nil
	}

	var found []string
	for _, name := range []string{"pwsh", "powershell"} {
		if _, err := exec.LookPath(name); err == nil {
			found = append(found, name)
		}
	}

	switch len(found) {
	case 0:
		return installPlan{}, fmt.Errorf("could not find PowerShell to ask where its profile lives")

	case 1:
		profile, err := powerShellProfile(found[0])
		if err != nil {
			return installPlan{}, err
		}
		return powerShellPlan(profile), nil

	default:
		return installPlan{}, fmt.Errorf(
			"both PowerShell editions are installed, and they do not share a profile.\n" +
				"Say which one you use:\n" +
				"  PowerShell 7:           noodge completion install pwsh\n" +
				"  Windows PowerShell 5.1: noodge completion install windows-powershell")
	}
}

// isPowerShell reports whether a target is one of the PowerShell editions.
func isPowerShell(target string) bool {
	_, named := powerShellEditions[target]
	return named || target == "powershell"
}

func powerShellPlan(profile string) installPlan {
	script := filepath.Join(filepath.Dir(profile), "noodge.completion.ps1")
	return installPlan{
		ScriptPath:  script,
		ProfilePath: profile,
		Line:        fmt.Sprintf(". %s", psQuote(script)),
	}
}

// powerShellProfile asks a PowerShell where its profile is.
//
// The path differs between editions, and OneDrive redirects Documents on many
// machines — often under a localised name, so "Documents" is not even the
// right word to look for. Asking the shell itself is the only reliable answer.
func powerShellProfile(exe string) (string, error) {
	path, err := exec.LookPath(exe)
	if err != nil {
		return "", fmt.Errorf("%s is not installed", exe)
	}

	out, err := exec.Command(path, "-NoProfile", "-NonInteractive", "-Command", "$PROFILE").Output()
	if err != nil {
		return "", fmt.Errorf("asking %s where its profile lives: %w", exe, err)
	}

	profile := strings.TrimSpace(string(out))
	if profile == "" {
		return "", fmt.Errorf("%s reported no profile path", exe)
	}
	return profile, nil
}

func profileHasMarker(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return strings.Contains(string(b), markerStart), nil
}

func writeScript(path, script string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(script), 0o644)
}

// appendToProfile adds the load line, backing the profile up first. It reports
// whether it changed anything.
func appendToProfile(plan installPlan) (bool, error) {
	already, err := profileHasMarker(plan.ProfilePath)
	if err != nil {
		return false, err
	}
	if already {
		return false, nil
	}

	existing, err := os.ReadFile(plan.ProfilePath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}

	if len(existing) > 0 {
		// A profile is a file the user has curated, possibly for years.
		if err := os.WriteFile(plan.ProfilePath+".noodge-backup", existing, 0o644); err != nil {
			return false, err
		}
	}

	if err := os.MkdirAll(filepath.Dir(plan.ProfilePath), 0o755); err != nil {
		return false, err
	}

	var b strings.Builder
	b.Write(existing)
	if len(existing) > 0 && !strings.HasSuffix(string(existing), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n" + markerStart + "\n")
	b.WriteString(plan.Line + "\n")
	b.WriteString(markerEnd + "\n")

	return true, os.WriteFile(plan.ProfilePath, []byte(b.String()), 0o644)
}

func confirm(env *Env) bool {
	if env.Stdin == nil {
		return false
	}

	fmt.Fprint(env.Stdout, "\nproceed? [y/N] ")

	line, err := bufio.NewReader(env.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
