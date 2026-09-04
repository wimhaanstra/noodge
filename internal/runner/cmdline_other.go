//go:build !windows

package runner

import "os/exec"

// applyShellQuirks has nothing to fix away from Windows: sh and its relatives
// take the command as a single argument and Go passes argv through untouched.
func applyShellQuirks(*exec.Cmd, Shell, string) {}
