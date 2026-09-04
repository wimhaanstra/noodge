#!/bin/sh
# Installs noodge on macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/wimhaanstra/noodge/main/scripts/install.sh | sh
#
# A script piped into sh cannot take arguments, so it is configured with
# environment variables:
#
#   NOODGE_VERSION=v1.2.3      # default: the latest release
#   NOODGE_INSTALL_DIR=~/.bin  # default: ~/.local/bin
#
# POSIX sh rather than bash: some minimal containers and Alpine images have no
# bash, and there is nothing here that needs it.

set -eu

REPO="wimhaanstra/noodge"
INSTALL_DIR="${NOODGE_INSTALL_DIR:-$HOME/.local/bin}"

step() { printf '==> %s\n' "$1"; }
note() { printf '    %s\n' "$1"; }
die() { printf 'install: %s\n' "$1" >&2; exit 1; }

need() {
    command -v "$1" >/dev/null 2>&1 || die "$1 is required but not installed"
}

# Every function prefixes its variables, because POSIX sh has no local scope.
# An unprefixed name inside a function is the same variable as the one in
# main, and assigning to it silently changes what the caller sees afterwards.

detect_platform() {
    dp_os=$(uname -s)
    case "$dp_os" in
        Linux)  dp_os=linux ;;
        Darwin) dp_os=darwin ;;
        *)      die "noodge has no build for $dp_os" ;;
    esac

    dp_arch=$(uname -m)
    case "$dp_arch" in
        x86_64|amd64)  dp_arch=amd64 ;;
        arm64|aarch64) dp_arch=arm64 ;;
        *)             die "noodge has no build for $dp_arch" ;;
    esac

    printf '%s_%s' "$dp_os" "$dp_arch"
}

latest_version() {
    # Plain text rather than a JSON parser: jq is not installed everywhere, and
    # requiring it to install a single binary is a poor trade.
    curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
        sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' |
        head -n 1
}

verify_checksum() {
    vc_file="$1"
    vc_sums="$2"
    vc_name="$3"

    vc_expected=$(grep " $vc_name\$" "$vc_sums" | awk '{print $1}' | head -n 1)
    [ -n "$vc_expected" ] || die "checksums.txt has no entry for $vc_name"

    # Linux has sha256sum, macOS has shasum. Neither has both.
    if command -v sha256sum >/dev/null 2>&1; then
        vc_actual=$(sha256sum "$vc_file" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        vc_actual=$(shasum -a 256 "$vc_file" | awk '{print $1}')
    else
        die "need sha256sum or shasum to verify the download"
    fi

    [ "$vc_actual" = "$vc_expected" ] || die "checksum mismatch for $vc_name
  expected $vc_expected
  got      $vc_actual"
}

main() {
    need curl
    need tar

    platform=$(detect_platform)

    version="${NOODGE_VERSION:-$(latest_version)}"
    [ -n "$version" ] || die "could not work out the latest version"
    case "$version" in v*) ;; *) version="v$version" ;; esac
    bare="${version#v}"

    archive="noodge_${bare}_${platform}.tar.gz"
    base="https://github.com/$REPO/releases/download/$version"

    step "Installing noodge $version ($platform)"

    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT INT TERM

    step "Downloading $archive"
    curl -fsSL -o "$work/$archive" "$base/$archive"

    # Unsigned downloads over a pipe-to-shell installer have no other
    # integrity check, so this one is not optional.
    step 'Verifying checksum'
    curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt"
    verify_checksum "$work/$archive" "$work/checksums.txt" "$archive"

    step "Installing to $INSTALL_DIR"
    tar -xzf "$work/$archive" -C "$work"
    [ -f "$work/noodge" ] || die "the archive did not contain a noodge binary"

    mkdir -p "$INSTALL_DIR"
    install -m 0755 "$work/noodge" "$INSTALL_DIR/noodge" 2>/dev/null ||
        { cp "$work/noodge" "$INSTALL_DIR/noodge" && chmod 0755 "$INSTALL_DIR/noodge"; }

    printf '\n'
    step 'Done'
    "$INSTALL_DIR/noodge" version

    # Editing someone's shell profile without being asked is rude, so this
    # says what to do rather than doing it.
    case ":$PATH:" in
        *":$INSTALL_DIR:"*) ;;
        *)
            printf '\n'
            note "$INSTALL_DIR is not on your PATH. Add it with:"
            note "  echo 'export PATH=\"\$PATH:$INSTALL_DIR\"' >> ~/.profile"
            ;;
    esac

    found=$(command -v noodge 2>/dev/null || true)
    if [ -n "$found" ] && [ "$found" != "$INSTALL_DIR/noodge" ]; then
        printf '\n'
        note "warning: 'noodge' currently resolves to $found, not the copy just installed"
    fi

    printf '\n'
    note 'Next: run  noodge init  in a project, then  noodge'
    note 'Tab completion:  noodge completion install bash'
}

main "$@"
