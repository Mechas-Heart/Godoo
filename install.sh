#!/bin/sh
# Copyright 2026 Mechas Heart
# SPDX-License-Identifier: Apache-2.0
#
# Installs a prebuilt godoo from the GitHub releases:
#
#   curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh | sh
#
# Options, as environment variables (put them before `sh`):
#   GODOO_VERSION=v0.3.0         a specific release (default: the latest)
#   GODOO_INSTALL_DIR=/usr/local/bin   where to put it (default: ~/.local/bin)
#
# Steps: work out the OS and CPU, download the matching archive and
# checksums.txt, check the SHA-256, copy the binary into the install dir.
#
# To remove godoo again (your projects and their databases are kept):
#
#   curl -fsSL https://raw.githubusercontent.com/Mechas-Heart/Godoo/main/install.sh | sh -s -- --uninstall
set -eu

REPO="Mechas-Heart/Godoo"
INSTALL_DIR="${GODOO_INSTALL_DIR:-$HOME/.local/bin}"
STATE_DIR="${XDG_STATE_HOME:-$HOME/.local/state}/godoo"

say() { printf '%s\n' "godoo-install: $*"; }
fail() { say "error: $*" >&2; exit 1; }

# uninstall stops every project's services, removes the binary, and removes
# godoo's project list unless projects still exist. Projects are not touched.
uninstall() {
	bin="$INSTALL_DIR/godoo"
	[ -f "$bin" ] || bin=$(command -v godoo 2>/dev/null || true)
	[ -n "$bin" ] || fail "godoo is not installed (not in $INSTALL_DIR, not on PATH)"
	case "$(readlink -f "$bin" 2>/dev/null || echo "$bin")" in
		/nix/store/*) fail "$bin was installed with Nix: run \`nix profile remove Godoo\` instead" ;;
	esac

	# Nothing should keep running once godoo is gone.
	if "$bin" down --help 2>&1 | grep -q -- '-all'; then
		"$bin" down --all ||
			fail "could not stop all projects; run \`godoo down\` in the running ones, then uninstall again"
	elif "$bin" list 2>/dev/null | grep -q ' running '; then
		# godoo v0.3.0 and older have no `down --all`
		"$bin" list
		fail "projects are running; run \`godoo down\` in each, then uninstall again"
	fi

	rm -f "$bin"
	say "removed $bin"

	# Project folders from the list that still exist.
	left=""
	if [ -f "$STATE_DIR/instances.json" ]; then
		paths=$(sed -n 's/^ *"path": *"\(.*\)",\{0,1\}$/\1/p' "$STATE_DIR/instances.json")
		while IFS= read -r dir; do
			if [ -n "$dir" ] && [ -d "$dir" ]; then
				left="$left  $dir
"
			fi
		done <<EOF
$paths
EOF
	fi
	if [ -n "$left" ]; then
		say "kept $STATE_DIR (the project list, for a reinstall). These projects still exist:"
		printf '%s' "$left"
		say "each keeps its code, databases and Nix environment in <project>/.godoo."
		say "to delete those: rm -rf <project>/.godoo (or \`godoo destroy\` before uninstalling),"
		say "then free the Nix store with: nix store gc"
	else
		rm -rf "$STATE_DIR"
		say "removed $STATE_DIR"
		say "to free the Nix store space godoo used: nix store gc"
	fi
	exit 0
}

case "${1:-}" in
	"") ;;
	--uninstall) uninstall ;;
	*) fail "unknown option $1 (the only option is --uninstall)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"

# --- 1. OS and CPU (the release has linux/darwin x amd64/arm64) ---
case "$(uname -s)" in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) fail "unsupported OS $(uname -s): godoo needs Nix, which runs on Linux and macOS" ;;
esac
case "$(uname -m)" in
	x86_64 | amd64) arch=amd64 ;;
	aarch64 | arm64) arch=arm64 ;;
	*) fail "unsupported CPU $(uname -m): releases exist for amd64 and arm64" ;;
esac

# --- 2. Version: the latest release unless GODOO_VERSION is set ---
version="${GODOO_VERSION:-}"
if [ -z "$version" ]; then
	# github.com/.../releases/latest redirects to .../releases/tag/vX.Y.Z
	# (no GitHub API, so no rate limit).
	latest=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		fail "could not reach GitHub to find the latest release"
	version="${latest##*/}"
	case "$version" in v*) ;; *) fail "could not find the latest release (got $latest)" ;; esac
fi
case "$version" in v*) ;; *) version="v$version" ;; esac

name="godoo_${version}_${os}_${arch}"
base="https://github.com/$REPO/releases/download/$version"

# --- 3. Download and verify ---
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# An existing godoo in the install dir is replaced, like an update.
current=""
if [ -x "$INSTALL_DIR/godoo" ]; then
	current=$("$INSTALL_DIR/godoo" version 2>/dev/null | awk '{ print $2 }') || true
fi
case "$current" in
	"") say "downloading godoo $version for $os/$arch" ;;
	"$version") say "godoo $version is already installed; installing it again" ;;
	*) say "replacing godoo ${current} with $version ($os/$arch)" ;;
esac
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" ||
	fail "no download at $base/$name.tar.gz (is $version a release?)"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" ||
	fail "no checksums.txt in release $version"

expected=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$expected" ] || fail "$name.tar.gz is not listed in checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "$tmp/$name.tar.gz" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	actual=$(shasum -a 256 "$tmp/$name.tar.gz" | awk '{ print $1 }')
else
	fail "need sha256sum or shasum to verify the download"
fi
[ "$expected" = "$actual" ] || fail "checksum mismatch for $name.tar.gz (expected $expected, got $actual)"
say "checksum OK"

# --- 4. Install ---
tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
mkdir -p "$INSTALL_DIR" || fail "cannot create $INSTALL_DIR (try GODOO_INSTALL_DIR=... or sudo)"
# install to a temp name, then rename, so a running godoo is never half-written
cp "$tmp/$name/godoo" "$INSTALL_DIR/.godoo.new" || fail "cannot write to $INSTALL_DIR"
chmod 755 "$INSTALL_DIR/.godoo.new"
mv -f "$INSTALL_DIR/.godoo.new" "$INSTALL_DIR/godoo"
say "installed $INSTALL_DIR/godoo"
"$INSTALL_DIR/godoo" version

# --- 5. Hints ---
# Another godoo earlier on PATH (go install, an old copy) would still win.
found=$(command -v godoo 2>/dev/null || true)
if [ -n "$found" ] && [ "$found" != "$INSTALL_DIR/godoo" ]; then
	say "warning: \`godoo\` runs $found ($("$found" version 2>/dev/null | awk '{ print $2 }')), not the one just installed."
	say "  remove it (rm $found) or put $INSTALL_DIR first in PATH"
fi
case ":$PATH:" in
	*":$INSTALL_DIR:"*) ;;
	*) say "note: $INSTALL_DIR is not on your PATH; add this to your shell profile:"
	   say "  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
esac
command -v nix >/dev/null 2>&1 ||
	say "note: Nix is not installed; godoo needs it: https://nixos.org/download"
