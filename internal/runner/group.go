package runner

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// A parallel group exists to run several long-lived things at once — a set of
// services, typically — and the interesting parts are not the starting but the
// stopping and the reading.
//
// Stopping: everything in the group goes into one process tree, so a failure
// takes the whole stack down rather than leaving orphans holding ports.
//
// Reading: three servers writing to one terminal interleave into something
// unreadable, so output is labelled by default. Labelling means capturing it
// through a pipe, and a program handed a pipe concludes it is not talking to a
// terminal and turns its colours off — which is why prefix: false exists.

// prefixColors are ANSI 256 foreground codes, chosen to stay legible on a
// light and a dark background alike, and to be distinguishable from each other.
var prefixColors = []int{39, 214, 78, 170, 203, 45, 220, 141}

// entryResult is how one entry of a group finished.
type entryResult struct {
	step PlannedStep
	err  error
}

// runGroup starts every entry at once and waits for them together.
func runGroup(req *Request, plan *Plan, group PlannedStep, stepNum int) error {
	tree, err := newProcessTree()
	if err != nil {
		return fmt.Errorf("step %d: preparing the process group: %w", stepNum, err)
	}
	defer tree.close()

	width := labelWidth(group.Parallel)

	// Every entry's output funnels into one writer from its own goroutine, so
	// the writes have to be serialised. Without this it is a data race, and on
	// a real terminal it is two log lines spliced together.
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results = make(chan entryResult, len(group.Parallel))
	)

	stdout := lockedWriter{mu: &mu, w: req.Stdout}
	stderr := lockedWriter{mu: &mu, w: req.Stderr}

	for _, entry := range group.Parallel {
		cmd := newProcess(plan, entry)
		tree.prepare(cmd)

		// No entry gets stdin. Several processes reading one terminal is a
		// race with no useful outcome, and a service that needs input is not
		// something to start alongside two others.
		cmd.Stdin = nil

		var closers []io.Closer
		if group.Prefix {
			outPipe, errPipe, err := prefixedPipes(
				stdout, stderr, req.Color,
				entry.Label, colorFor(entry.Label, group.Parallel), width, cmd)
			if err != nil {
				tree.terminate()
				return fmt.Errorf("step %d: %w", stepNum, err)
			}
			closers = append(closers, outPipe, errPipe)

			// Many tools keep their colour when told to explicitly, even
			// though they can see they are writing to a pipe.
			if req.Color {
				cmd.Env = append(cmd.Env, "FORCE_COLOR=1", "CLICOLOR_FORCE=1")
			}
		} else {
			// Raw passthrough: the entries write straight to the terminal, so
			// they keep their colours and their progress bars, at the cost of
			// interleaving with each other.
			cmd.Stdout = req.Stdout
			cmd.Stderr = req.Stderr
		}

		if err := cmd.Start(); err != nil {
			tree.terminate()
			return fmt.Errorf("step %d: starting %s: %w", stepNum, entry.Display, err)
		}

		if err := tree.adopt(cmd); err != nil {
			tree.terminate()
			return fmt.Errorf("step %d: tracking %s: %w", stepNum, entry.Label, err)
		}

		wg.Add(1)
		go func(entry PlannedStep) {
			defer wg.Done()
			err := cmd.Wait()
			for _, c := range closers {
				_ = c.Close()
			}
			results <- entryResult{step: entry, err: err}
		}(entry)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	return collect(stderr, results, tree, stepNum)
}

// collect waits for the group, tearing it down at the first failure.
//
// A clean exit is left alone: a watcher or a one-shot task finishing should not
// take the servers beside it down. A non-zero exit stops everything, because a
// crashed API with two healthy logs still scrolling is not a useful state.
func collect(stderr io.Writer, results <-chan entryResult, tree *processTree, stepNum int) error {
	var failure error

	for res := range results {
		if res.err == nil {
			fmt.Fprintf(stderr, "noodge: %s finished\n", res.step.Label)
			continue
		}

		if failure == nil {
			failure = &ExitError{
				Step:    stepNum,
				Display: res.step.Display,
				Code:    exitCodeOf(res.err),
			}
			fmt.Fprintf(stderr, "noodge: %s failed, stopping the rest of the group\n", res.step.Label)
			tree.terminate()
		}
	}

	return failure
}

// lockedWriter serialises writes coming from the goroutine that reads each
// entry's output.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l lockedWriter) Write(p []byte) (int, error) {
	if l.w == nil {
		return len(p), nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// prefixedPipes wires a process's output through a labeller.
func prefixedPipes(stdout, stderr io.Writer, useColor bool, label string, color, width int, cmd *exec.Cmd) (io.Closer, io.Closer, error) {
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, nil, err
	}

	cmd.Stdout = outW
	cmd.Stderr = errW

	prefix := renderPrefix(label, color, width, useColor)
	go pump(outR, stdout, prefix)
	go pump(errR, stderr, prefix)

	// The write ends belong to the child once it has started; closing ours
	// after Wait is what lets the readers see EOF.
	return outW, errW, nil
}

// pump copies one stream line by line, tagging each line.
//
// Line by line rather than byte by byte, because the whole point is that two
// services never end up sharing a line.
func pump(r io.ReadCloser, w io.Writer, prefix string) {
	defer r.Close()

	scanner := bufio.NewScanner(r)
	// Log lines can be long: a stack trace or a JSON payload should not be
	// silently truncated at bufio's default 64KB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		fmt.Fprintf(w, "%s%s\n", prefix, scanner.Text())
	}
}

func renderPrefix(label string, color, width int, useColor bool) string {
	padded := fmt.Sprintf("%-*s", width, label)
	if !useColor {
		return padded + " | "
	}
	return fmt.Sprintf("\x1b[38;5;%dm%s |\x1b[0m ", color, padded)
}

func labelWidth(entries []PlannedStep) int {
	width := 0
	for _, e := range entries {
		if n := len(e.Label); n > width {
			width = n
		}
	}
	return width
}

// colorFor picks a stable colour for a label: the same entry keeps the same
// colour every run, so the eye learns it.
func colorFor(label string, entries []PlannedStep) int {
	for i, e := range entries {
		if e.Label == label {
			return prefixColors[i%len(prefixColors)]
		}
	}
	return prefixColors[0]
}

// exitCodeOf extracts a child's exit code, falling back to 1 for a failure
// that was not the process exiting at all.
func exitCodeOf(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

// summariseGroup renders what a group would run, for --dry-run.
func summariseGroup(group PlannedStep) []string {
	out := make([]string, 0, len(group.Parallel))
	for _, e := range group.Parallel {
		out = append(out, fmt.Sprintf("%s: %s", e.Label, e.Display))
	}
	return out
}

// Summary describes a group for display.
func (s PlannedStep) Summary() string {
	if !s.IsGroup() {
		return s.Display
	}
	return "parallel: " + strings.Join(summariseGroup(s), " | ")
}
