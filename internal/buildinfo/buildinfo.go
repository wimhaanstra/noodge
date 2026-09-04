// Package buildinfo carries the version stamped into the binary at link time.
package buildinfo

import "fmt"

// These are set with -ldflags at release time. The defaults are what a
// locally built binary reports, and "dev" is also the signal that suppresses
// the update check.
var (
	// Version is the release version, without a leading v.
	Version = "dev"
	// Commit is the short commit hash the binary was built from.
	Commit = "none"
	// Date is the build timestamp, in RFC 3339.
	Date = "unknown"
)

// IsDev reports whether this is a locally built binary rather than a release.
func IsDev() bool { return Version == "dev" }

// String renders the full version line.
func String() string {
	return fmt.Sprintf("noodge %s (commit %s, built %s)", Version, Commit, Date)
}
