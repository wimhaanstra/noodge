package update

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Checker looks for newer releases and reports what it already knows.
type Checker struct {
	// Version is the running binary's version.
	Version string
	// FeedURL is where to fetch the release description from.
	FeedURL string
	// Client is the HTTP client to use. Nil means a client with a short
	// timeout.
	Client *http.Client
	// Now is the clock, replaceable in tests.
	Now func() time.Time
}

// FeedURLEnv overrides where the feed is fetched from. It exists so the
// upgrade path can be exercised against a local server, and so anyone running
// their own build can point noodge at their own releases.
const FeedURLEnv = "NOODGE_UPDATE_FEED"

// NewChecker returns a Checker for the running binary.
func NewChecker(version string) *Checker {
	feed := FeedURL
	if override := os.Getenv(FeedURLEnv); override != "" {
		feed = override
	}

	return &Checker{
		Version: version,
		FeedURL: feed,
		// Two seconds. This runs unattended alongside real work; a longer
		// timeout would only make a broken network hold a goroutine open for
		// no benefit, since nothing waits for the result anyway.
		Client: &http.Client{Timeout: 2 * time.Second},
		Now:    time.Now,
	}
}

func (c *Checker) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Refresh fetches the feed if the cache is stale, and stores what it finds.
//
// It is started in the background and never waited for. That is the whole
// design: a command's runtime is not spent on an update check, and the notice
// a user sees comes from what a previous run already learned. A quick command
// that exits before the fetch finishes simply leaves the cache as it was and
// tries again next time; a build or a test run gives it more than enough time.
func (c *Checker) Refresh(ctx context.Context) {
	// A panic on a goroutine nobody is waiting for would take the whole
	// process down, and an update check is never worth that.
	defer func() { _ = recover() }()

	s := loadState()
	if !s.LastCheck.IsZero() && c.now().Sub(s.LastCheck) < Interval {
		return
	}

	// Recorded before the fetch, so a server that is failing is not retried on
	// every single invocation.
	s.LastCheck = c.now()

	if latest, err := c.fetch(ctx); err == nil {
		s.Version = latest.Version
		s.NotesURL = latest.NotesURL
	}

	saveState(s)
}

// Fetch reads the feed now, without touching the cache. This is what an
// explicit check uses, where the user is waiting and wants a real answer.
func (c *Checker) Fetch(ctx context.Context) (Latest, error) {
	return c.fetch(ctx)
}

func (c *Checker) fetch(ctx context.Context) (Latest, error) {
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.FeedURL, nil)
	if err != nil {
		return Latest{}, err
	}
	req.Header.Set("User-Agent", "noodge/"+c.Version)

	resp, err := client.Do(req)
	if err != nil {
		return Latest{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Latest{}, fmt.Errorf("update feed returned %s", resp.Status)
	}

	return ParseLatest(resp.Body)
}

// Notice returns the message to show, or an empty string when there is
// nothing to say. It reads only the cache and never touches the network.
func (c *Checker) Notice() string {
	s := loadState()
	if s.Version == "" || !Newer(c.Version, s.Version) {
		return ""
	}

	msg := fmt.Sprintf("noodge %s is available (you have %s). Run 'noodge upgrade'.",
		s.Version, c.Version)
	if s.NotesURL != "" {
		msg += "\n  " + s.NotesURL
	}
	return msg
}

// Suppressed reports whether update checking should be skipped entirely, and
// why, so callers can log the reason when debugging.
//
// isTerminal says whether stderr is a terminal, passed in rather than probed
// here so tests never depend on how they were invoked.
func Suppressed(version string, isTerminal bool) (bool, string) {
	switch {
	case os.Getenv("NOODGE_NO_UPDATE_CHECK") != "":
		return true, "NOODGE_NO_UPDATE_CHECK is set"

	case os.Getenv("CI") != "":
		// A build machine cannot act on the notice and does not want it in the
		// log of every job.
		return true, "running in CI"

	case version == "dev":
		// A locally built binary has no release to compare against, and would
		// otherwise be told to upgrade to whatever is published.
		return true, "this is a development build"

	case !isTerminal:
		// The output is going somewhere that is being read by a program, or
		// captured into a file. Neither wants an advert in it.
		return true, "stderr is not a terminal"

	default:
		return false, ""
	}
}
