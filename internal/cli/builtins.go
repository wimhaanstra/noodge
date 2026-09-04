package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/wimhaanstra/noodge/internal/buildinfo"
	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/schema"
)

func newVersionCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version of noodge",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			fmt.Fprintln(env.Stdout, buildinfo.String())
			return nil
		},
	}
}

func newSchemaCmd(env *Env) *cobra.Command {
	return &cobra.Command{
		Use:   "schema",
		Short: "Print the JSON Schema for noodge.yaml",
		Long: "Prints the JSON Schema describing noodge.yaml.\n\n" +
			"Editors use it for completion and hover text. Rather than piping this to a\n" +
			"file, most editors will fetch it if you put this line at the top of your\n" +
			"noodge.yaml:\n\n" +
			"  # yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			_, err := env.Stdout.Write(schema.JSON())
			return err
		},
	}
}

func newValidateCmd(env *Env, opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Check noodge.yaml for problems",
		Long: "Loads the noodge.yaml for this project and reports anything wrong with it.\n\n" +
			"Errors mean the file cannot be used. Warnings are advisory and never stop a\n" +
			"command running: a missing description is worth knowing about, but it is not\n" +
			"a reason to refuse to work.",
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runValidate(env, opts)
		},
	}
}

func runValidate(env *Env, opts *options) error {
	file, err := opts.load(env)
	if err != nil {
		return err
	}

	diags := config.Validate(file)
	for _, d := range diags {
		fmt.Fprintln(env.Stderr, d.String())
	}

	errCount := len(diags.Errors())
	warnCount := len(diags.Warnings())

	switch {
	case errCount > 0:
		fmt.Fprintf(env.Stderr, "\n%s: %s, %s\n", file.Path,
			plural(errCount, "error"), plural(warnCount, "warning"))
		// The diagnostics are already printed; returning a bare error would
		// print a second, less useful summary line.
		return errSilent

	case warnCount > 0:
		fmt.Fprintf(env.Stdout, "%s is valid (%s)\n", file.Path, plural(warnCount, "warning"))

	default:
		fmt.Fprintf(env.Stdout, "%s is valid\n", file.Path)
	}

	return nil
}

// errSilent reports failure without adding another line of output.
var errSilent = errors.New("")

func newListCmd(env *Env, opts *options) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the commands this project declares",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runList(env, opts, asJSON)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print as JSON, for scripts and editors")
	return cmd
}

// listEntry is the shape of one command in --json output.
type listEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Aliases     []string `json:"aliases,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	Params      []string `json:"params,omitempty"`
}

func runList(env *Env, opts *options, asJSON bool) error {
	file, err := opts.load(env)
	if err != nil {
		// Being in a directory with no noodge.yaml is not a failure, it is
		// just the answer to the question. Say what to do next and stop.
		var notFound *config.NotFoundError
		if errors.As(err, &notFound) && !asJSON {
			fmt.Fprintf(env.Stderr, "%v\n", err)
			fmt.Fprintln(env.Stderr, "  hint: run 'noodge init' to create one")
			return nil
		}
		return err
	}

	if asJSON {
		return listJSON(env, file)
	}
	return listText(env, file)
}

func listJSON(env *Env, file *config.File) error {
	entries := make([]listEntry, 0, len(file.Config.Commands))

	for _, nc := range file.Config.Commands {
		params := make([]string, 0, len(nc.Params))
		for _, p := range nc.Params {
			params = append(params, p.Name)
		}
		entries = append(entries, listEntry{
			Name:        nc.Name,
			Description: nc.Description,
			Aliases:     nc.Aliases,
			Hidden:      nc.Hidden,
			Params:      params,
		})
	}

	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func listText(env *Env, file *config.File) error {
	if name := file.Config.Name; name != "" {
		fmt.Fprintf(env.Stdout, "%s\n", name)
	}
	fmt.Fprintf(env.Stdout, "%s\n\n", file.Path)

	w := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	shown := 0

	for _, nc := range file.Config.Commands {
		if nc.Hidden {
			continue
		}
		shown++

		summary := firstLine(nc.Description)
		if summary == "" {
			summary = "(no description)"
		}
		fmt.Fprintf(w, "  %s\t%s\n", nc.Name, summary)
	}

	if err := w.Flush(); err != nil {
		return err
	}

	if shown == 0 {
		fmt.Fprintln(env.Stdout, "  (no visible commands)")
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func newRunCmd(env *Env, opts *options, file *config.File) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <command> [flags] [-- args]",
		Short: "Run a command from noodge.yaml by name",
		Long: "Runs a command declared in noodge.yaml.\n\n" +
			"Every command is also available directly, as 'noodge <command>'. This form\n" +
			"exists for the case where a command shares a name with one of noodge's own:\n" +
			"'noodge list' runs the built-in, and 'noodge run list' runs yours.\n\n" +
			"Hidden commands are reachable here too.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Cobra commands hold a pointer to their parent, so the tree is built a
	// second time rather than shared with the root.
	cmd.AddCommand(buildCommands(env, opts, file)...)
	return cmd
}

// starterConfig is what noodge init writes. It is deliberately a working
// example rather than an empty skeleton: the fastest way to learn the format
// is to edit something that already runs.
const starterConfig = `# yaml-language-server: $schema=https://wimhaanstra.github.io/noodge/schema/v1/noodge.schema.json
version: 1
name: %s

commands:
  hello:
    description: |
      A placeholder so you can check noodge works.

      Replace this with something your project actually needs, and write the
      description for whoever joins next month.
    steps:
      - echo Hello from noodge
    output: One line on stdout.

  greet:
    description: Shows how parameters work.
    params:
      - name: name
        flag: --name
        type: string
        default: world
        description: Who to greet.
      - name: loud
        flag: --loud
        type: bool
        description: Shout it.
    steps:
      # {{flag loud}} disappears entirely when --loud is not passed, so no
      # empty flag is left behind.
      - echo Hello {{name}} {{flag loud}}
    output: One line on stdout.
`

func newInitCmd(env *Env, opts *options) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a starter noodge.yaml",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return runInit(env, opts)
		},
	}
}

func runInit(env *Env, opts *options) error {
	dir, err := opts.startDir(env)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, "noodge.yaml")
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}

	name := filepath.Base(dir)
	content := fmt.Sprintf(starterConfig, name)

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(env.Stdout, "created %s\n", path)
	fmt.Fprintln(env.Stdout, "run 'noodge' to see what it declares")
	return nil
}
