package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

// binaryName is what the archive contains.
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "noodge.exe"
	}
	return "noodge"
}

// Upgrader replaces the running binary with a newer one.
type Upgrader struct {
	// Version is the running binary's version.
	Version string
	// Checker supplies the feed.
	Checker *Checker
	// Client downloads the archive. Nil means a client with a generous
	// timeout, since this one is user-initiated and they are waiting.
	Client *http.Client
	// Force ignores the refusal to fight a package manager.
	Force bool

	// Out receives progress.
	Out io.Writer
}

// ErrUpToDate reports that there is nothing to do.
var ErrUpToDate = errors.New("already the latest version")

// ManagedError reports that a package manager owns this installation.
type ManagedError struct {
	Manager Manager
	Path    string
}

func (e *ManagedError) Error() string {
	return fmt.Sprintf("this noodge was installed by %s, which keeps its own record of the installed version.\n"+
		"Upgrading in place would leave %s believing the old version is still installed, and its next update would put it back.\n\n"+
		"  %s\n\n"+
		"Use --force to replace it anyway.",
		e.Manager.Name, e.Manager.Name, e.Manager.Command)
}

// ExecutablePath returns the real path of the running binary, following any
// symlink so that what gets replaced is the binary rather than a link to it.
func ExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// A path that cannot be resolved is still worth trying to replace.
		return exe, nil
	}
	return resolved, nil
}

func (u *Upgrader) printf(format string, args ...any) {
	if u.Out != nil {
		fmt.Fprintf(u.Out, format, args...)
	}
}

// Run performs the upgrade.
func (u *Upgrader) Run(ctx context.Context) error {
	exe, err := ExecutablePath()
	if err != nil {
		return fmt.Errorf("finding the running binary: %w", err)
	}

	if m, managed := DetectManager(exe); managed && !u.Force {
		return &ManagedError{Manager: m, Path: exe}
	}

	latest, err := u.Checker.Fetch(ctx)
	if err != nil {
		return err
	}

	if !Newer(u.Version, latest.Version) {
		return ErrUpToDate
	}

	asset, ok := latest.AssetFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return fmt.Errorf("release %s has no build for %s/%s", latest.Version, runtime.GOOS, runtime.GOARCH)
	}

	u.printf("downloading noodge %s\n", latest.Version)
	archive, err := u.download(ctx, asset)
	if err != nil {
		return err
	}

	u.printf("verifying checksum\n")
	if err := verifySHA256(archive, asset.SHA256); err != nil {
		return err
	}

	binary, err := extractBinary(archive, asset.URL)
	if err != nil {
		return err
	}

	u.printf("replacing %s\n", exe)
	if err := applyWithRetry(binary, exe); err != nil {
		return err
	}

	u.printf("\nnoodge %s installed\n", latest.Version)
	if latest.NotesURL != "" {
		u.printf("%s\n", latest.NotesURL)
	}
	return nil
}

func (u *Upgrader) download(ctx context.Context, asset Asset) ([]byte, error) {
	client := u.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "noodge/"+u.Version)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", asset.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: %s", asset.URL, resp.Status)
	}

	// Bounded so a wrong URL cannot stream something enormous into memory.
	return io.ReadAll(io.LimitReader(resp.Body, 128<<20))
}

// verifySHA256 checks the archive against the published checksum.
//
// The published checksums are of the archives, not of the binaries inside
// them, so this is done here rather than handed to selfupdate's own checksum
// option, which would be comparing against the wrong thing.
func verifySHA256(data []byte, expected string) error {
	sum := sha256.Sum256(data)
	got := hex.EncodeToString(sum[:])

	if !strings.EqualFold(got, expected) {
		return fmt.Errorf("checksum mismatch, refusing to install\n  expected %s\n  got      %s", expected, got)
	}
	return nil
}

// extractBinary pulls the noodge binary out of a release archive.
func extractBinary(archive []byte, url string) ([]byte, error) {
	if strings.HasSuffix(path.Base(url), ".zip") {
		return extractFromZip(archive)
	}
	return extractFromTarGz(archive)
}

func extractFromZip(archive []byte) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, fmt.Errorf("reading the archive: %w", err)
	}

	want := binaryName()
	for _, f := range r.File {
		if path.Base(f.Name) != want {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()

		return io.ReadAll(io.LimitReader(rc, 256<<20))
	}

	return nil, fmt.Errorf("the archive does not contain %s", want)
}

func extractFromTarGz(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("reading the archive: %w", err)
	}
	defer gz.Close()

	want := binaryName()
	tr := tar.NewReader(gz)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading the archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg || path.Base(hdr.Name) != want {
			continue
		}
		return io.ReadAll(io.LimitReader(tr, 256<<20))
	}

	return nil, fmt.Errorf("the archive does not contain %s", want)
}

// applyWithRetry swaps the binary, retrying briefly on the transient failures
// Windows produces.
//
// A freshly written executable is routinely held open for a moment by
// antivirus, which surfaces as a sharing violation or an access denial. Left
// unretried it looks like a random, unreproducible upgrade failure.
func applyWithRetry(binary []byte, target string) error {
	delays := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 900 * time.Millisecond}

	var err error
	for attempt := 0; ; attempt++ {
		err = selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{TargetPath: target})
		if err == nil {
			return nil
		}

		// A rollback error means the filesystem is in a state the user has to
		// know about, and retrying cannot help.
		if rollbackErr := selfupdate.RollbackError(err); rollbackErr != nil {
			return fmt.Errorf("the upgrade failed and could not be undone: %w\n"+
				"the previous binary is alongside %s with a .old suffix", rollbackErr, target)
		}

		if attempt >= len(delays) || !isTransient(err) {
			return fmt.Errorf("replacing %s: %w", target, err)
		}
		time.Sleep(delays[attempt])
	}
}

// isTransient reports whether an error is the sort that goes away on its own,
// which on Windows means something else is briefly holding the file.
func isTransient(err error) bool {
	if errors.Is(err, os.ErrPermission) {
		return true
	}

	// ERROR_SHARING_VIOLATION is not represented by a portable error value, so
	// the message is the only thing available here.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "being used by another process") ||
		strings.Contains(msg, "sharing violation") ||
		strings.Contains(msg, "access is denied")
}

// oldBinaryPath returns where the previous binary is left after an upgrade.
//
// The name is not "<exe>.old" but ".<exe>.old", with a leading dot, which is
// what selfupdate writes. Getting this wrong leaves the previous binary behind
// after every single upgrade, and each one is several megabytes.
func oldBinaryPath(exe string) string {
	return filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".old")
}

// CleanupOld removes the previous binary left beside the current one.
//
// Windows cannot delete a running executable, so an upgrade renames the old
// one aside and hides it. It can only be removed once that process has exited,
// which means the next run. Failure is ignored: this is tidying, not work the
// user asked for.
func CleanupOld() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(oldBinaryPath(exe))
}
