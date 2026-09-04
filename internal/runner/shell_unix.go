//go:build !windows

package runner

// DefaultShell is sh everywhere other than Windows.
func DefaultShell() Shell {
	return Shell{Argv: []string{"sh", "-c"}, Style: StylePosix}
}
