package runner

// DefaultShell is cmd on Windows.
//
// PowerShell would be the friendlier interpreter, but it costs a few hundred
// milliseconds of startup per step and noodge runs each step as its own
// process. A config that wants it can say so with shell: pwsh -Command.
func DefaultShell() Shell {
	return Shell{Argv: []string{"cmd", "/c"}, Style: StyleCmd}
}
