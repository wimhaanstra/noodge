package update

import "strings"

// Manager is a package manager that owns an installed copy of noodge.
type Manager struct {
	// Name is what to call it when explaining the refusal.
	Name string
	// Command is what the user should run instead.
	Command string
}

// managers maps a path fragment to the package manager that owns that
// location. Matching is case-insensitive and uses forward slashes, so the
// same table works on every platform.
var managers = []struct {
	fragment string
	manager  Manager
}{
	{"/scoop/apps/", Manager{Name: "Scoop", Command: "scoop update noodge"}},
	{"/microsoft/winget/packages/", Manager{Name: "winget", Command: "winget upgrade wimhaanstra.noodge"}},
	{"/microsoft/winget/links/", Manager{Name: "winget", Command: "winget upgrade wimhaanstra.noodge"}},
	// Homebrew lives in three places: /opt/homebrew on Apple silicon,
	// /usr/local on Intel Macs, and /home/linuxbrew/.linuxbrew on Linux.
	{"/homebrew/cellar/", Manager{Name: "Homebrew", Command: "brew upgrade noodge"}},
	{"/.linuxbrew/cellar/", Manager{Name: "Homebrew", Command: "brew upgrade noodge"}},
	{"/usr/local/cellar/", Manager{Name: "Homebrew", Command: "brew upgrade noodge"}},
}

// DetectManager reports whether a package manager installed the binary at
// path.
//
// This is not politeness, it is correctness. Replacing a Scoop-installed
// binary in place leaves Scoop's own record saying the old version is still
// installed, so the next 'scoop update' silently puts the user back where they
// started — a bug that is very hard to diagnose from the outside.
func DetectManager(path string) (Manager, bool) {
	// Not filepath.ToSlash: that converts the separator of whatever platform
	// this is compiled for, so on Linux it leaves a Windows path's backslashes
	// exactly as they are and nothing ever matches. Replacing explicitly makes
	// the detection depend on the path rather than on the host.
	needle := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))

	for _, m := range managers {
		if strings.Contains(needle, m.fragment) {
			return m.manager, true
		}
	}
	return Manager{}, false
}
