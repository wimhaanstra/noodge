// Command gen-latest writes the update feed that noodge checks.
//
// It runs in the Pages workflow, from the checksums file the release already
// published, so the feed cannot describe artifacts that do not exist.
//
// Usage:
//
//	go run ./tools/gen-latest \
//	  -version 1.4.0 -published 2026-09-04T10:00:00Z \
//	  -notes https://github.com/wimhaanstra/noodge/releases/tag/v1.4.0 \
//	  -checksums checksums.txt -out site/latest.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/wimhaanstra/noodge/internal/update"
)

const repo = "wimhaanstra/noodge"

func main() {
	var (
		version   = flag.String("version", "", "release version, without a leading v")
		published = flag.String("published", "", "release timestamp, RFC 3339")
		notes     = flag.String("notes", "", "URL of the release notes")
		checksums = flag.String("checksums", "", "path to the release's checksums.txt")
		out       = flag.String("out", "site/latest.json", "where to write the feed")
	)
	flag.Parse()

	if err := run(*version, *published, *notes, *checksums, *out); err != nil {
		fmt.Fprintln(os.Stderr, "gen-latest:", err)
		os.Exit(1)
	}
}

func run(version, published, notes, checksums, out string) error {
	if version == "" || checksums == "" {
		return fmt.Errorf("-version and -checksums are both required")
	}

	raw, err := os.ReadFile(checksums)
	if err != nil {
		return err
	}

	assets, err := parseChecksums(string(raw), strings.TrimPrefix(version, "v"))
	if err != nil {
		return err
	}

	feed := update.Latest{
		Version:   strings.TrimPrefix(version, "v"),
		Published: published,
		NotesURL:  notes,
		Assets:    assets,
	}

	// The generator validates with the same code the client uses, so a feed
	// that would be rejected at runtime fails the build instead.
	if err := feed.Validate(); err != nil {
		return err
	}

	body, err := json.MarshalIndent(feed, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')

	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, body, 0o644)
}

// assetRE matches an archive name produced by the release configuration:
// noodge_1.4.0_windows_amd64.zip, noodge_1.4.0_linux_arm64.tar.gz.
//
// This is coupled to the archives name_template in .goreleaser.yaml. Changing
// one without the other is caught here rather than at a user's machine,
// because no asset matching means no feed.
var assetRE = regexp.MustCompile(`^noodge_([^_]+)_([a-z0-9]+)_([a-z0-9]+)\.(zip|tar\.gz)$`)

// parseChecksums turns a checksums.txt into the feed's asset list.
func parseChecksums(contents, version string) ([]update.Asset, error) {
	var assets []update.Asset

	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) != 2 {
			continue
		}
		sum, name := fields[0], fields[1]

		m := assetRE.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if m[1] != version {
			return nil, fmt.Errorf("checksums.txt describes version %s but %s was expected", m[1], version)
		}

		assets = append(assets, update.Asset{
			OS:     m[2],
			Arch:   m[3],
			SHA256: strings.ToLower(sum),
			URL: fmt.Sprintf("https://github.com/%s/releases/download/v%s/%s",
				repo, version, name),
		})
	}

	if len(assets) == 0 {
		return nil, fmt.Errorf("no release archives found in the checksums file")
	}

	// Stable order so the published file only changes when its contents do.
	sort.Slice(assets, func(i, j int) bool {
		if assets[i].OS != assets[j].OS {
			return assets[i].OS < assets[j].OS
		}
		return assets[i].Arch < assets[j].Arch
	})

	return assets, nil
}
