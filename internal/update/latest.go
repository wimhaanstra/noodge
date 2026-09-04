// Package update tells the user when a newer noodge exists, and replaces the
// running binary when they ask for it.
//
// Nothing here ever blocks a command. A task runner is invoked constantly, and
// a tool that pauses to phone home before doing what it was asked is a tool
// people stop using.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// FeedURL is where the published description of the latest release lives.
//
// A static file rather than the GitHub API: that API's unauthenticated limit
// is per IP address, so a whole company behind one NAT egress can exhaust it
// between them and everyone starts seeing spurious failures.
const FeedURL = "https://wimhaanstra.github.io/noodge/latest.json"

// Latest describes the newest release. It is written by tools/gen-latest and
// read by the running binary, so the two share this definition rather than
// agreeing on a shape by hand.
type Latest struct {
	// Version is the release version without a leading v, for example "1.4.0".
	Version string `json:"version"`
	// Published is when the release was made, in RFC 3339.
	Published string `json:"published"`
	// NotesURL points at the release notes.
	NotesURL string `json:"notes_url"`
	// Assets are the downloadable builds, one per platform.
	Assets []Asset `json:"assets"`
}

// Asset is one platform's build.
type Asset struct {
	// OS is a Go GOOS value: windows, darwin or linux.
	OS string `json:"os"`
	// Arch is a Go GOARCH value: amd64 or arm64.
	Arch string `json:"arch"`
	// URL is where to download it.
	URL string `json:"url"`
	// SHA256 is the archive's checksum, lower-case hex.
	SHA256 string `json:"sha256"`
}

// AssetFor returns the build for a platform.
func (l Latest) AssetFor(goos, goarch string) (Asset, bool) {
	for _, a := range l.Assets {
		if a.OS == goos && a.Arch == goarch {
			return a, true
		}
	}
	return Asset{}, false
}

// Validate reports whether the document is usable, so a truncated or
// unexpected response is rejected rather than acted on.
func (l Latest) Validate() error {
	switch {
	case strings.TrimSpace(l.Version) == "":
		return fmt.Errorf("no version")
	case len(l.Assets) == 0:
		return fmt.Errorf("no assets")
	}

	for _, a := range l.Assets {
		if a.URL == "" || a.SHA256 == "" {
			return fmt.Errorf("asset %s/%s is missing a url or checksum", a.OS, a.Arch)
		}
	}
	return nil
}

// ParseLatest reads a feed document.
func ParseLatest(r io.Reader) (Latest, error) {
	// Bounded, because this is parsed from the network and a wrong URL could
	// otherwise stream something enormous into memory.
	var l Latest
	if err := json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(&l); err != nil {
		return Latest{}, fmt.Errorf("reading the update feed: %w", err)
	}
	if err := l.Validate(); err != nil {
		return Latest{}, fmt.Errorf("the update feed is not usable: %w", err)
	}
	return l, nil
}

// Newer reports whether available is a later version than current.
//
// Both are compared as dotted numbers, so 0.10.0 is correctly newer than
// 0.9.0, which a string comparison gets wrong. Anything unparseable compares
// as not newer, because telling someone to upgrade to a version that does not
// exist is worse than saying nothing.
func Newer(current, available string) bool {
	cur, ok := parseVersion(current)
	if !ok {
		return false
	}
	next, ok := parseVersion(available)
	if !ok {
		return false
	}

	for i := range next {
		switch {
		case next[i] > cur[i]:
			return true
		case next[i] < cur[i]:
			return false
		}
	}
	return false
}

// parseVersion splits a semantic version into its three numbers, ignoring any
// prerelease or build suffix.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int

	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}

	for i, p := range parts {
		n := 0
		if p == "" {
			return out, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}
