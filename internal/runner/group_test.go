package runner

import (
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// The entries of a group really do overlap in time. Run one after the other
// these two would take at least 800ms; run together they take a little over
// 400ms, so the bound leaves a wide margin and still fails a sequential
// implementation.
func TestGroupEntriesRunAtTheSameTime(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  both:
    description: Two slow entries.
    steps:
      - parallel:
          first: [`+yaml(exe)+`, '-noodge-helper', 'sleep', '400', 'first done']
          second: [`+yaml(exe)+`, '-noodge-helper', 'sleep', '400', 'second done']
`)

	start := time.Now()
	out, err := execute(t, file, "both", nil, nil)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if elapsed > 750*time.Millisecond {
		t.Errorf("took %v, which suggests the entries ran one after the other", elapsed)
	}
	for _, want := range []string{"first done", "second done"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Output is labelled with the name the author gave the entry, which is the
// whole reason entries are named rather than listed.
func TestGroupPrefixesOutputWithTheDeclaredName(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  dev:
    description: Named entries.
    steps:
      - parallel:
          api: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'listening']
          worker: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'polling']
`)

	out, err := execute(t, file, "dev", nil, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}

	for _, want := range []string{"api", "listening", "worker", "polling"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// Each line carries exactly one label, so two services can never share one.
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.Contains(line, "listening") && !strings.Contains(line, "api") {
			t.Errorf("output line is not attributed: %q", line)
		}
	}
}

func TestGroupWithoutPrefixDoesNotLabel(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  dev:
    description: Raw passthrough.
    steps:
      - parallel:
          api: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'listening']
        prefix: false
`)

	out, err := execute(t, file, "dev", nil, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "listening") {
		t.Fatalf("output missing:\n%s", out)
	}
	if strings.Contains(out, "api |") {
		t.Errorf("prefix: false should not label the output:\n%s", out)
	}
}

// A crash takes the group down; leaving two healthy logs scrolling beside a
// dead API is not a useful state.
func TestGroupFailureStopsTheOthers(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  boom:
    description: One entry fails.
    steps:
      - parallel:
          server: [`+yaml(exe)+`, '-noodge-helper', 'sleep', '5000', 'server should never get here']
          crasher: [`+yaml(exe)+`, '-noodge-helper', 'exit', '4']
`)

	start := time.Now()
	out, err := execute(t, file, "boom", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected a failure, got:\n%s", out)
	}

	exitErr, ok := err.(*ExitError)
	if !ok {
		t.Fatalf("got %T (%v), want *ExitError", err, err)
	}
	if exitErr.Code != 4 {
		t.Errorf("exit code: got %d, want 4", exitErr.Code)
	}

	// The five second sleeper must have been killed, not waited for.
	if elapsed > 3*time.Second {
		t.Errorf("took %v, so the rest of the group was not stopped", elapsed)
	}
	if strings.Contains(out, "server should never get here") {
		t.Error("the sleeping entry was allowed to finish")
	}
}

// A clean exit is not a failure. A watcher or a one-shot task finishing should
// not tear down the servers beside it.
func TestGroupCleanExitDoesNotStopTheOthers(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  mixed:
    description: One entry finishes early, one carries on.
    steps:
      - parallel:
          setup: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'setup done']
          server: [`+yaml(exe)+`, '-noodge-helper', 'sleep', '400', 'server finished normally']
`)

	out, err := execute(t, file, "mixed", nil, nil)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(out, "server finished normally") {
		t.Errorf("the early clean exit should not have stopped the other entry:\n%s", out)
	}
}

func TestGroupPlanKeepsDeclaredOrder(t *testing.T) {
	exe := helperPath(t)

	file := project(t, `version: 1
commands:
  dev:
    description: Order matters for colours and for dry-run.
    steps:
      - parallel:
          zebra: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'z']
          apple: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'a']
          mango: [`+yaml(exe)+`, '-noodge-helper', 'echo', 'm']
`)

	nc, ok := file.Config.Commands.Get("dev")
	if !ok {
		t.Fatal("dev not found")
	}

	plan, err := PlanCommand(&Request{File: file, Command: nc, Values: Values{}})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || !plan.Steps[0].IsGroup() {
		t.Fatal("expected one group step")
	}

	var got []string
	for _, e := range plan.Steps[0].Parallel {
		got = append(got, e.Label)
	}

	// Alphabetising here would mean the file said one thing and the tool did
	// another, which is exactly what an earlier decoding round-trip did.
	want := []string{"zebra", "apple", "mango"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A program can write its last line and exit immediately, leaving that line in
// the pipe after the process is already gone. Returning without waiting for
// the readers loses it, and races with whoever reads the output next — which
// is how CI's race detector found this.
func TestGroupCapturesEveryLine(t *testing.T) {
	exe := helperPath(t)

	const lines = 400

	file := project(t, `version: 1
commands:
  noisy:
    description: Two chatty entries that exit at once.
    steps:
      - parallel:
          one: [`+yaml(exe)+`, '-noodge-helper', 'spam', '400']
          two: [`+yaml(exe)+`, '-noodge-helper', 'spam', '400']
`)

	nc, ok := file.Config.Commands.Get("noisy")
	if !ok {
		t.Fatal("noisy not found")
	}

	// A slow writer is what makes this deterministic rather than a race the
	// test usually loses. The processes finish long before their output has
	// been written out, so returning early is guaranteed to truncate.
	out := &slowWriter{delay: 200 * time.Microsecond}

	req := &Request{
		File:    file,
		Command: nc,
		Values:  Values{},
		Stdout:  out,
		Stderr:  io.Discard,
	}

	plan, err := PlanCommand(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := Run(req, plan); err != nil {
		t.Fatalf("run: %v", err)
	}

	text := out.String()
	for _, label := range []string{"one", "two"} {
		count := 0
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, label) && strings.Contains(l, "line ") {
				count++
			}
		}
		if count != lines {
			t.Errorf("entry %q produced %d lines, want %d", label, count, lines)
		}
	}
}

// slowWriter takes a moment over every write, so output still in flight when a
// process exits is unmistakably still in flight.
type slowWriter struct {
	delay time.Duration
	mu    sync.Mutex
	buf   strings.Builder
}

func (w *slowWriter) Write(p []byte) (int, error) {
	time.Sleep(w.delay)
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *slowWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
