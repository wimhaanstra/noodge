package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Interval is how often the feed is fetched. A task runner is invoked dozens
// of times a day; checking on each one would be rude to both the user and the
// server.
const Interval = 24 * time.Hour

// state is what one machine remembers between runs.
type state struct {
	// LastCheck is when the feed was last fetched, successfully or not, so a
	// server that is down is not retried on every invocation.
	LastCheck time.Time `json:"last_check"`
	// Version is the newest version seen.
	Version string `json:"version,omitempty"`
	// NotesURL points at that version's release notes.
	NotesURL string `json:"notes_url,omitempty"`
}

// stateFile returns where the cache lives, following each platform's
// convention for state that is not configuration and not worth backing up.
func stateFile() (string, error) {
	if runtime.GOOS == "windows" {
		dir := os.Getenv("LOCALAPPDATA")
		if dir == "" {
			var err error
			if dir, err = os.UserCacheDir(); err != nil {
				return "", err
			}
		}
		return filepath.Join(dir, "noodge", "state.json"), nil
	}

	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "noodge", "state.json"), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "noodge", "state.json"), nil
}

// loadState reads the cache. A missing or unreadable file is not an error:
// there is nothing here that matters enough to complain about.
func loadState() state {
	path, err := stateFile()
	if err != nil {
		return state{}
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return state{}
	}

	var s state
	if err := json.Unmarshal(b, &s); err != nil {
		return state{}
	}
	return s
}

// saveState writes the cache, replacing it atomically.
//
// The write happens on a background goroutine that the process does not wait
// for, so it can be interrupted by the process exiting. Writing to a temporary
// file and renaming means an interrupted write leaves the previous cache
// intact rather than a half-written file that fails to parse.
func saveState(s state) {
	path, err := stateFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}

	b, err := json.Marshal(s)
	if err != nil {
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "state-*.json")
	if err != nil {
		return
	}
	name := tmp.Name()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return
	}

	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
	}
}
