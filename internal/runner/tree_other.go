//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// processTree puts every started process into one process group, so a single
// signal reaches the whole tree rather than just the process noodge started.
type processTree struct {
	pgids []int
}

func newProcessTree() (*processTree, error) { return &processTree{}, nil }

// prepare asks for a new process group, which must be requested before the
// process starts.
func (t *processTree) prepare(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func (t *processTree) adopt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// With Setpgid the group id is the child's own pid.
	t.pgids = append(t.pgids, cmd.Process.Pid)
	return nil
}

// terminate signals each group, asking politely before insisting.
func (t *processTree) terminate() {
	for _, pgid := range t.pgids {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	}
	t.pgids = nil
}

func (t *processTree) close() { t.terminate() }
