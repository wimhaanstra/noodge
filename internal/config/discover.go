package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileNames are the accepted config file names, in preference order.
var FileNames = []string{"noodge.yaml", "noodge.yml"}

// maxWalkDepth bounds the upward search. Nothing legitimate is 64 directories
// above the working directory, and the bound means a pathological filesystem
// cannot hang the completion path.
const maxWalkDepth = 64

// NotFoundError reports that no config was found anywhere above a directory.
type NotFoundError struct {
	// StartDir is where the search began.
	StartDir string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("no noodge.yaml found in %s or any parent directory", e.StartDir)
}

// AmbiguousError reports that a single directory holds more than one config
// file. Picking one silently would mean edits to the other quietly did
// nothing, so this is an error rather than a preference order.
type AmbiguousError struct {
	Dir   string
	Files []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("both %s and %s exist in %s; keep only one",
		e.Files[0], e.Files[1], e.Dir)
}

// Discover walks up from startDir looking for a config file, returning the
// path to the first one found.
//
// The walk stops at the first hit, at a directory containing a .git entry, at
// the user's home directory, or at the filesystem root. Stopping at .git and
// at home means a stray noodge.yaml in the home directory is never picked up
// by an unrelated project several levels below it.
func Discover(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", err
	}

	home, _ := os.UserHomeDir()

	for depth := 0; depth < maxWalkDepth; depth++ {
		path, err := findIn(dir)
		if err != nil {
			return "", err
		}
		if path != "" {
			return path, nil
		}

		// A repository root is a boundary: a project's config belongs to the
		// project, not to whatever happens to sit above the checkout.
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			break
		}
		if home != "" && dir == home {
			break
		}

		parent := filepath.Dir(dir)
		// filepath.Dir is its own fixed point at a root, including for UNC
		// paths such as \server\share, where naive walking loops forever.
		if parent == dir {
			break
		}
		dir = parent
	}

	abs, _ := filepath.Abs(startDir)
	return "", &NotFoundError{StartDir: abs}
}

// findIn returns the config file in dir, or "" when there is none.
func findIn(dir string) (string, error) {
	var found []string
	for _, name := range FileNames {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			found = append(found, name)
		}
	}

	switch len(found) {
	case 0:
		return "", nil
	case 1:
		return filepath.Join(dir, found[0]), nil
	default:
		return "", &AmbiguousError{Dir: dir, Files: found}
	}
}
