package runner

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// Killing a whole process tree on Windows is not the same problem as on Unix.
//
// There are no process groups to signal, and terminating a process leaves its
// children running: kill npm and node carries on holding the port, invisible
// to whoever is wondering why the next run fails to bind. The supported answer
// is a Job Object with kill-on-close, which the kernel tears down along with
// every process assigned to it, however deeply nested.

// processTree owns a job object that every started process is assigned to.
type processTree struct {
	job windows.Handle
}

func newProcessTree() (*processTree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}

	// KILL_ON_JOB_CLOSE is what makes closing the handle sufficient, including
	// when noodge itself is killed rather than exiting cleanly.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafePointer(&info)),
		uint32(unsafeSizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, err
	}

	return &processTree{job: job}, nil
}

// prepare has nothing to do here: assignment happens once the process exists.
func (t *processTree) prepare(*exec.Cmd) {}

// adopt puts a started process, and everything it goes on to spawn, into the
// job.
//
// There is a small window between the process starting and being assigned, in
// which a grandchild could escape. Closing it properly needs the process
// created suspended and then resumed, and Go's os/exec does not expose the
// thread handle that requires. For starting dev servers the race is
// theoretical, and the alternative is reimplementing process creation.
func (t *processTree) adopt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}

	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)

	return windows.AssignProcessToJobObject(t.job, h)
}

// terminate kills every process in the job.
func (t *processTree) terminate() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}

// close releases the job, which also kills anything still running in it.
func (t *processTree) close() { t.terminate() }
