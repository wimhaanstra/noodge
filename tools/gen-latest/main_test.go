package main

import "testing"

// The parser is coupled to the archives name_template in .goreleaser.yaml.
// This fixture is a real checksums.txt from the v0.1.2 release, so a change to
// that template fails here rather than silently producing an empty feed.
const realChecksums = `48314512623f6ff7eaad5cdb9c97e2eaf9efa571f77574f758b5b43296ee4b14  noodge_0.1.2_darwin_amd64.tar.gz
28169cfbbfd0e11271c6b88772b286b88ea1b5ad08bf8ddb1a0a87b1b2f88e13  noodge_0.1.2_darwin_arm64.tar.gz
06daaacecb310d5b7aa45863635c3fdb566569bc6cba11b122907bf2878aaf21  noodge_0.1.2_linux_amd64.tar.gz
ec2775d66528793a3b00e706e8f46ff71db5bd5876e2ca8acef835437fb2524f  noodge_0.1.2_linux_arm64.tar.gz
6c1946feb9b56b46610e3f60791993bc278b9663d10a6bd6d476e6c5fe839975  noodge_0.1.2_windows_amd64.zip
9ceec3c64a54f5f78e9aa5560cbef582d29cd857339e02162cf069117337517b  noodge_0.1.2_windows_arm64.zip
`

func TestParseChecksums(t *testing.T) {
	assets, err := parseChecksums(realChecksums, "0.1.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 6 {
		t.Fatalf("got %d assets, want 6", len(assets))
	}

	// Sorted, so the published file only changes when its contents do.
	want := []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64"}
	for i, a := range assets {
		if got := a.OS + "/" + a.Arch; got != want[i] {
			t.Errorf("asset %d: got %s, want %s", i, got, want[i])
		}
		if len(a.SHA256) != 64 {
			t.Errorf("asset %s has a %d character checksum", a.OS, len(a.SHA256))
		}
	}

	if got, want := assets[4].URL,
		"https://github.com/wimhaanstra/noodge/releases/download/v0.1.2/noodge_0.1.2_windows_amd64.zip"; got != want {
		t.Errorf("url:\n got %s\nwant %s", got, want)
	}
}

// A feed describing a different version than the release it came from would
// send everyone to the wrong download.
func TestParseChecksumsRejectsAVersionMismatch(t *testing.T) {
	if _, err := parseChecksums(realChecksums, "0.9.9"); err == nil {
		t.Error("expected an error when the checksums describe another version")
	}
}

func TestParseChecksumsNeedsArchives(t *testing.T) {
	if _, err := parseChecksums("deadbeef  some-other-file.txt\n", "0.1.2"); err == nil {
		t.Error("expected an error when nothing matches the archive naming")
	}
}
