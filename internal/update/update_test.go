package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// isolateState points the cache at a temporary directory, so tests never read
// or write the real one.
func isolateState(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	return dir
}

func TestNewer(t *testing.T) {
	tests := []struct {
		current, available string
		want               bool
	}{
		{"1.0.0", "1.0.1", true},
		{"1.0.0", "1.1.0", true},
		{"1.0.0", "2.0.0", true},
		{"1.0.1", "1.0.0", false},
		{"1.0.0", "1.0.0", false},

		// The one a string comparison gets wrong.
		{"0.9.0", "0.10.0", true},
		{"0.10.0", "0.9.0", false},

		// Leading v on either side.
		{"v1.0.0", "1.0.1", true},
		{"1.0.0", "v1.0.1", true},

		// A prerelease suffix is ignored rather than misparsed.
		{"1.0.0", "1.0.1-rc.1", true},

		// A development build has no version to compare, and must never be
		// told to upgrade.
		{"dev", "1.0.0", false},
		{"", "1.0.0", false},
		{"1.0.0", "not-a-version", false},
	}

	for _, tt := range tests {
		if got := Newer(tt.current, tt.available); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.current, tt.available, got, tt.want)
		}
	}
}

func TestParseLatestRejectsUnusableDocuments(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", "<html>404</html>"},
		{"no version", `{"assets":[{"os":"linux","arch":"amd64","url":"u","sha256":"s"}]}`},
		{"no assets", `{"version":"1.0.0","assets":[]}`},
		{"asset without a checksum", `{"version":"1.0.0","assets":[{"os":"linux","arch":"amd64","url":"u"}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseLatest(strings.NewReader(tt.body)); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

func TestAssetFor(t *testing.T) {
	l := Latest{Assets: []Asset{
		{OS: "linux", Arch: "amd64", URL: "l-amd64"},
		{OS: "windows", Arch: "arm64", URL: "w-arm64"},
	}}

	if a, ok := l.AssetFor("windows", "arm64"); !ok || a.URL != "w-arm64" {
		t.Errorf("got %+v, %v", a, ok)
	}
	if _, ok := l.AssetFor("darwin", "amd64"); ok {
		t.Error("should not have found a darwin build")
	}
}

// Replacing a package-managed binary in place leaves that manager's record
// pointing at the old version, and its next update silently reverts the user.
func TestDetectManager(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{`C:\Users\wim\scoop\apps\noodge\current\noodge.exe`, "Scoop"},
		{`C:\Users\wim\AppData\Local\Microsoft\WinGet\Packages\x\noodge.exe`, "winget"},
		{`C:\Users\wim\AppData\Local\Microsoft\WinGet\Links\noodge.exe`, "winget"},
		{"/opt/homebrew/Cellar/noodge/1.0.0/bin/noodge", "Homebrew"},
		{"/home/linuxbrew/.linuxbrew/Cellar/noodge/1.0.0/bin/noodge", "Homebrew"},

		// Not managed: the installer's own location, and a plain build.
		{`C:\Users\wim\AppData\Local\Programs\noodge\bin\noodge.exe`, ""},
		{"/home/wim/.local/bin/noodge", ""},
		{"/usr/local/bin/noodge", ""},
	}

	for _, tt := range tests {
		m, ok := DetectManager(tt.path)
		switch {
		case tt.want == "" && ok:
			t.Errorf("%s: unexpectedly detected %s", tt.path, m.Name)
		case tt.want != "" && !ok:
			t.Errorf("%s: expected %s, detected nothing", tt.path, tt.want)
		case tt.want != "" && m.Name != tt.want:
			t.Errorf("%s: got %s, want %s", tt.path, m.Name, tt.want)
		}
	}
}

func TestSuppressed(t *testing.T) {
	tests := []struct {
		name       string
		env        map[string]string
		version    string
		isTerminal bool
		want       bool
	}{
		{"normal terminal use", nil, "1.0.0", true, false},
		{"opted out", map[string]string{"NOODGE_NO_UPDATE_CHECK": "1"}, "1.0.0", true, true},
		{"in CI", map[string]string{"CI": "true"}, "1.0.0", true, true},
		{"development build", nil, "dev", true, true},
		{"stderr is redirected", nil, "1.0.0", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NOODGE_NO_UPDATE_CHECK", "")
			t.Setenv("CI", "")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, reason := Suppressed(tt.version, tt.isTerminal)
			if got != tt.want {
				t.Errorf("got %v (%s), want %v", got, reason, tt.want)
			}
			if got && reason == "" {
				t.Error("a suppression should say why")
			}
		})
	}
}

// The notice must come from the cache alone. A command that reaches the
// network before doing what it was asked is the thing this design exists to
// avoid.
func TestNoticeReadsOnlyTheCache(t *testing.T) {
	isolateState(t)

	c := &Checker{Version: "1.0.0", FeedURL: "http://127.0.0.1:1/never-reachable"}

	if notice := c.Notice(); notice != "" {
		t.Errorf("an empty cache should say nothing, got %q", notice)
	}

	saveState(state{
		LastCheck: time.Now(),
		Version:   "1.2.0",
		NotesURL:  "https://example.invalid/notes",
	})

	notice := c.Notice()
	if !strings.Contains(notice, "1.2.0") || !strings.Contains(notice, "noodge upgrade") {
		t.Errorf("got %q", notice)
	}
	if !strings.Contains(notice, "https://example.invalid/notes") {
		t.Errorf("the notes link should be offered: %q", notice)
	}
}

func TestNoticeSaysNothingWhenCurrent(t *testing.T) {
	isolateState(t)
	saveState(state{LastCheck: time.Now(), Version: "1.0.0"})

	c := &Checker{Version: "1.0.0"}
	if notice := c.Notice(); notice != "" {
		t.Errorf("got %q, want nothing", notice)
	}
}

func TestRefreshStoresWhatItFinds(t *testing.T) {
	isolateState(t)

	feed := Latest{
		Version:  "2.0.0",
		NotesURL: "https://example.invalid/2.0.0",
		Assets:   []Asset{{OS: "linux", Arch: "amd64", URL: "u", SHA256: "s"}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(feed)
	}))
	defer srv.Close()

	c := &Checker{Version: "1.0.0", FeedURL: srv.URL, Client: srv.Client()}
	c.Refresh(context.Background())

	if got := loadState().Version; got != "2.0.0" {
		t.Errorf("cached version: got %q, want 2.0.0", got)
	}
	if notice := c.Notice(); !strings.Contains(notice, "2.0.0") {
		t.Errorf("got %q", notice)
	}
}

// Checking on every invocation would be rude to the user and to the server.
func TestRefreshHonoursTheInterval(t *testing.T) {
	isolateState(t)

	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(Latest{
			Version: "2.0.0",
			Assets:  []Asset{{OS: "linux", Arch: "amd64", URL: "u", SHA256: "s"}},
		})
	}))
	defer srv.Close()

	c := &Checker{Version: "1.0.0", FeedURL: srv.URL, Client: srv.Client()}

	c.Refresh(context.Background())
	c.Refresh(context.Background())
	c.Refresh(context.Background())

	if hits != 1 {
		t.Errorf("fetched %d times, want 1", hits)
	}

	// Pretend a day has passed.
	c.Now = func() time.Time { return time.Now().Add(Interval + time.Minute) }
	c.Refresh(context.Background())

	if hits != 2 {
		t.Errorf("after the interval, fetched %d times, want 2", hits)
	}
}

// A server that is down must not be retried on every single invocation.
func TestRefreshRecordsTheAttemptEvenWhenItFails(t *testing.T) {
	isolateState(t)

	c := &Checker{
		Version: "1.0.0",
		FeedURL: "http://127.0.0.1:1/never-reachable",
		Client:  &http.Client{Timeout: 100 * time.Millisecond},
	}
	c.Refresh(context.Background())

	if loadState().LastCheck.IsZero() {
		t.Error("a failed check should still be recorded, or it retries every run")
	}
}

func TestVerifySHA256(t *testing.T) {
	data := []byte("some archive bytes")
	sum := sha256.Sum256(data)
	good := hex.EncodeToString(sum[:])

	if err := verifySHA256(data, good); err != nil {
		t.Errorf("matching checksum rejected: %v", err)
	}
	if err := verifySHA256(data, strings.ToUpper(good)); err != nil {
		t.Errorf("case should not matter: %v", err)
	}
	if err := verifySHA256(data, "0000"); err == nil {
		t.Error("a mismatched checksum must be refused")
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// A real archive carries the README and LICENSE alongside the binary.
	for _, name := range []string{"README.md", binaryName(), "LICENSE"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("contents of " + name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromZip(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contents of "+binaryName() {
		t.Errorf("got %q", got)
	}
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, name := range []string{"README.md", binaryName(), "LICENSE"} {
		body := "contents of " + name
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := extractFromTarGz(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "contents of "+binaryName() {
		t.Errorf("got %q", got)
	}
}

func TestExtractRejectsAnArchiveWithoutTheBinary(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("README.md")
	_, _ = w.Write([]byte("nothing useful"))
	_ = zw.Close()

	if _, err := extractFromZip(buf.Bytes()); err == nil {
		t.Error("expected an error when the binary is missing")
	}
}

func TestExecutablePathResolves(t *testing.T) {
	path, err := ExecutablePath()
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected an absolute path, got %q", path)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(path), ".exe") {
		t.Errorf("expected an .exe on Windows, got %q", path)
	}
}
