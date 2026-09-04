package runner

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wimhaanstra/noodge/internal/config"
	"github.com/wimhaanstra/noodge/internal/template"
)

// Request is everything needed to run one command.
type Request struct {
	// File is the loaded config the command came from.
	File *config.File
	// Command is the command to run.
	Command *config.NamedCommand
	// Values are the resolved parameters.
	Values Values
	// Args are the pass-through arguments typed after a bare "--".
	Args []string

	// Stdout, Stderr and Stdin are handed to each step unchanged, so a step
	// keeps its colours, its progress bars and its ability to prompt.
	Stdout io.Writer
	Stderr io.Writer
	Stdin  io.Reader
}

// Plan is a command resolved down to the exact processes it will start.
type Plan struct {
	// Dir is the working directory every step runs in.
	Dir string
	// Env is the extra environment applied to every step.
	Env map[string]string
	// Shell is the interpreter string steps are handed to.
	Shell Shell
	// Steps are the resolved steps, in order.
	Steps []PlannedStep
}

// PlannedStep is one step with every placeholder already expanded.
type PlannedStep struct {
	// Argv is the exact process and arguments that will be started.
	Argv []string
	// Shell reports whether Argv runs the step through an interpreter.
	Shell bool
	// Line is the expanded command line handed to the interpreter. Empty for
	// steps written in list form.
	Line string
	// Display is the step as it should be shown to a human.
	Display string
}

// ExitError reports that a step exited non-zero. The code is the child's own,
// passed through unchanged.
type ExitError struct {
	// Step is the 1-based index of the step that failed.
	Step int
	// Display is the step that failed, for the message.
	Display string
	// Code is the child's exit code.
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("step %d exited with code %d: %s", e.Step, e.Code, e.Display)
}

// PlanCommand resolves a command into the processes it would start, without
// starting any of them. It is what --dry-run prints and what Run executes, so
// what you inspect is exactly what runs.
func PlanCommand(req *Request) (*Plan, error) {
	cmd := req.Command

	shell, err := resolveShell(req.File.Config, cmd)
	if err != nil {
		return nil, err
	}

	dir := req.File.Dir
	if cmd.Cwd != "" {
		dir = filepath.Join(dir, cmd.Cwd)
	}

	plan := &Plan{
		Dir:   dir,
		Env:   mergedEnv(req),
		Shell: shell,
		Steps: make([]PlannedStep, 0, len(cmd.Steps)),
	}

	// Pass-through arguments land in {{args}} if any step asks for them, and
	// are otherwise appended to the last step.
	explicitArgs := UsesArgs(cmd.Steps)

	for i, s := range cmd.Steps {
		last := i == len(cmd.Steps)-1
		extra := req.Args
		if explicitArgs || !last {
			extra = nil
		}

		planned, err := planStep(s, req, shell, extra, explicitArgs)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		plan.Steps = append(plan.Steps, planned)
	}

	return plan, nil
}

func planStep(s config.Step, req *Request, shell Shell, appended []string, explicitArgs bool) (PlannedStep, error) {
	args := req.Args
	if !explicitArgs {
		// {{args}} is absent, so no expansion should consume them.
		args = nil
	}

	if s.IsArgv() {
		argv, err := ExpandArgv(s.Argv, req.Values, args)
		if err != nil {
			return PlannedStep{}, err
		}
		argv = append(argv, appended...)

		if len(argv) == 0 {
			return PlannedStep{}, errors.New("expanded to nothing")
		}
		return PlannedStep{Argv: argv, Display: strings.Join(argv, " ")}, nil
	}

	line, err := ExpandLine(s.Line, req.Values, args, shell.Style)
	if err != nil {
		return PlannedStep{}, err
	}
	if len(appended) > 0 {
		line = strings.TrimSpace(line + " " + strings.Join(appended, " "))
	}
	if line == "" {
		return PlannedStep{}, errors.New("expanded to nothing")
	}

	argv := append(append([]string{}, shell.Argv...), line)
	return PlannedStep{Argv: argv, Shell: true, Line: line, Display: line}, nil
}

// Run executes a plan, stopping at the first step that fails.
func Run(req *Request, plan *Plan) error {
	// While a child is running, Ctrl+C belongs to it. On Windows the child is
	// in the same console group and receives it directly; if noodge also took
	// the default action it would die first and report the wrong exit code.
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)

	for i, step := range plan.Steps {
		cmd := exec.Command(step.Argv[0], step.Argv[1:]...)
		if step.Shell {
			applyShellQuirks(cmd, plan.Shell, step.Line)
		}
		cmd.Dir = plan.Dir
		cmd.Env = environ(plan.Env)
		cmd.Stdin = req.Stdin
		cmd.Stdout = req.Stdout
		cmd.Stderr = req.Stderr

		if err := cmd.Run(); err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				return &ExitError{Step: i + 1, Display: step.Display, Code: exitErr.ExitCode()}
			}
			return fmt.Errorf("step %d (%s): %w", i+1, step.Display, err)
		}
	}

	return nil
}

// resolveShell picks the interpreter for string steps: the command's own
// override, then the file's, then the platform default.
func resolveShell(cfg *config.Config, cmd *config.NamedCommand) (Shell, error) {
	if cmd.Shell != "" {
		return ParseShell(cmd.Shell)
	}
	if cfg.Shell != "" {
		return ParseShell(cfg.Shell)
	}
	return DefaultShell(), nil
}

// mergedEnv layers the command's environment over the file's, and adds the
// values noodge contributes itself.
func mergedEnv(req *Request) map[string]string {
	out := map[string]string{}

	for k, v := range req.File.Config.Env {
		out[k] = v
	}
	for k, v := range req.Command.Env {
		out[k] = v
	}

	out["NOODGE_COMMAND"] = req.Command.Name

	// Every parameter is also exported, so a script can read a value it was
	// not handed on its command line.
	for name, v := range req.Values {
		if !v.Set && v.Param.ResolvedType() != config.TypeBool {
			continue
		}
		out["NOODGE_PARAM_"+envName(name)] = v.Str
	}

	return out
}

// envName converts a parameter name to the shape an environment variable
// takes: upper case, with dashes as underscores.
func envName(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "-", "_"))
}

// environ combines the process environment with the extra variables. Sorting
// keeps the result stable, which matters for tests and for --dry-run.
func environ(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}

	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	out := os.Environ()
	for _, k := range keys {
		out = append(out, k+"="+extra[k])
	}
	return out
}

// UsesArgs reports whether any step asks for the pass-through arguments by
// name, which decides whether they are placed or appended.
func UsesArgs(steps []config.Step) bool {
	for _, s := range steps {
		texts := s.Argv
		if !s.IsArgv() {
			texts = []string{s.Line}
		}
		for _, t := range texts {
			// Parsed rather than string-matched: the parser tolerates
			// whitespace inside the braces, so a text search would miss
			// "{{ args }}" and silently append instead of substituting.
			refs, err := template.Parse(t)
			if err != nil {
				continue
			}
			for _, ref := range refs {
				if ref.Kind == template.KindArgs {
					return true
				}
			}
		}
	}
	return false
}
