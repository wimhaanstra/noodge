package runner

import (
	"os/exec"
	"strings"
	"syscall"
)

// applyShellQuirks fixes how a command line reaches cmd.exe.
//
// Go builds a Windows command line from argv using the C runtime convention,
// where an embedded quote is escaped as \". cmd.exe does not understand that
// convention at all — it uses "" — so a step containing a quoted path arrives
// mangled and fails with an unhelpful exit code 1.
//
// The fix is to build the command line ourselves and hand it over verbatim.
// /S makes cmd's rule unambiguous: strip the first and last quote of what
// follows /C and treat everything between them as the command, with no further
// interpretation. That is exactly the semantics a step needs.
func applyShellQuirks(cmd *exec.Cmd, sh Shell, line string) {
	if sh.Style != StyleCmd || len(sh.Argv) == 0 {
		return
	}

	var b strings.Builder
	b.WriteString(sh.Argv[0])
	b.WriteString(" /s")
	for _, a := range sh.Argv[1:] {
		b.WriteString(" ")
		b.WriteString(a)
	}
	b.WriteString(` "`)
	b.WriteString(line)
	b.WriteString(`"`)

	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: b.String()}
}
