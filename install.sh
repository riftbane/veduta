#!/bin/sh
# Veduta installer: curl -fsSL https://raw.githubusercontent.com/riftbane/veduta/main/install.sh | sh
#
# Installs the veduta tool from GitHub Releases (checksum verified), installs Go when it
# is missing or too old (optional), checks git, and runs `veduta doctor`.
#
# Flags:
#   --prefix DIR   install directory (default: $VEDUTA_HOME/bin or ~/.local/bin)
#   --version V    release to install (default: $VEDUTA_VERSION, or the newest in the channel)
#   --channel C    release channel: stable (default) or beta, which includes candidates
#   --with-go      install Go into ~/.local/go when it is missing or older than 1.25
#   --no-go        never install Go
#   --yes          do not ask questions
#
# Idempotent: running it again reinstalls the requested version in place.
set -eu

REPO="riftbane/veduta"
GO_MIN_MINOR=25

prefix=""
version="${VEDUTA_VERSION:-}"
channel="${VEDUTA_CHANNEL:-stable}"
with_go=""
assume_yes=""

say() { printf '%s\n' "$*"; }
die() { printf 'install.sh: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
	--prefix) [ $# -ge 2 ] || die "--prefix needs a directory"; prefix="$2"; shift 2 ;;
	--prefix=*) prefix="${1#*=}"; shift ;;
	--version) [ $# -ge 2 ] || die "--version needs a value"; version="$2"; shift 2 ;;
	--version=*) version="${1#*=}"; shift ;;
	--channel) [ $# -ge 2 ] || die "--channel needs a value"; channel="$2"; shift 2 ;;
	--channel=*) channel="${1#*=}"; shift ;;
	--with-go) with_go="yes"; shift ;;
	--no-go) with_go="no"; shift ;;
	--yes|-y) assume_yes="yes"; shift ;;
	-h|--help) sed -n '2,15p' "$0" 2>/dev/null || true; exit 0 ;;
	*) die "unknown flag $1 (try --help)" ;;
	esac
done

case "$channel" in
stable | beta) ;;
*) die "unknown channel $channel (want stable or beta)" ;;
esac

# 1. Platform.
os="$(uname -s)"
arch="$(uname -m)"
case "$os" in
Linux) os=linux ;;
Darwin) die "no prebuilt veduta tool for macOS (releases carry linux/amd64, linux/arm64 and windows/amd64); build it from source with Go 1.25 or newer: go install -ldflags \"-X main.version=${version:-vX.Y.Z}\" github.com/riftbane/veduta/cmd/veduta@${version:-vX.Y.Z}" ;;
*) die "unsupported OS $os (Veduta ships Linux and Windows binaries; on Windows download the zip from https://github.com/$REPO/releases)" ;;
esac
case "$arch" in
x86_64|amd64) arch=amd64 ;;
aarch64|arm64) arch=arm64 ;;
*) die "unsupported architecture $arch (supported: amd64, arm64)" ;;
esac

# Downloads with curl or wget.
# A GitHub token, when the environment already holds one, only raises the API rate limit:
# the releases themselves are public. Shared addresses — CI runners above all — are refused
# without it. It is sent to the GitHub API and nowhere else, never to go.dev.
gh_token="${GITHUB_TOKEN:-${GH_TOKEN:-}}"
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL --retry 3 -o "$2" "$1"; }
	fetch_stdout() { curl -fsSL --retry 3 "$1"; }
	api_get() {
		if [ -n "$gh_token" ]; then
			curl -fsSL --retry 3 -H "Authorization: Bearer $gh_token" "$1"
		else
			curl -fsSL --retry 3 "$1"
		fi
	}
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
	fetch_stdout() { wget -q -O - "$1"; }
	api_get() {
		if [ -n "$gh_token" ]; then
			wget -q -O - --header="Authorization: Bearer $gh_token" "$1"
		else
			wget -q -O - "$1"
		fi
	}
else
	die "curl or wget is required"
fi
if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
	die "sha256sum or shasum is required to verify downloads"
fi

interactive() { [ -z "$assume_yes" ] && [ -t 0 ] && [ -r /dev/tty ]; }
ask() { # ask QUESTION → 0 for yes
	if ! interactive; then return 0; fi
	printf '%s [Y/n] ' "$1" >/dev/tty
	read -r answer </dev/tty || answer=""
	case "$answer" in n*|N*) return 1 ;; *) return 0 ;; esac
}

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t veduta)"
trap 'rm -rf "$tmp"' EXIT INT TERM

# 2. Version.
if [ -z "$version" ]; then
	# The beta channel reads the whole list, which includes pre-releases; GitHub returns
	# it newest first. The stable endpoint never answers with a pre-release.
	if [ "$channel" = beta ]; then
		url="https://api.github.com/repos/$REPO/releases?per_page=30"
	else
		url="https://api.github.com/repos/$REPO/releases/latest"
	fi
	# One JSON field per line before matching: the answer may arrive as a single line,
	# where a greedy match would take the last tag_name instead of the first, and where
	# the first field would still carry the opening [ and {. The anchor then keeps an
	# escaped \"tag_name\" inside release notes from matching.
	tags="$(api_get "$url" | tr ',{[' '\n\n\n' | sed -n 's/^[[:space:]]*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
	if [ "$channel" = beta ]; then
		# The list is not in version order, so take the highest version rather than the
		# first: major, minor and patch as numbers, and a release ahead of its own
		# candidates (~ sorts after every letter and digit). Among candidates of one
		# version the comparison is textual, which the tool itself then corrects.
		version="$(printf '%s\n' "$tags" | awk '
			{
				tag = $0
				t = tag
				sub(/^v/, "", t)
				pre = "~"
				i = index(t, "-")
				if (i) { pre = substr(t, i + 1); t = substr(t, 1, i - 1) }
				if (t !~ /^[0-9]+\.[0-9]+\.[0-9]+$/) next
				if (pre != "~") {
					# Pad the numeric identifiers of the suffix too, or rc.9 would sort
					# after rc.10.
					n = split(pre, q, ".")
					pre = ""
					for (j = 1; j <= n; j++) {
						if (q[j] ~ /^[0-9]+$/) q[j] = sprintf("%010d", q[j])
						pre = pre (j > 1 ? "." : "") q[j]
					}
				}
				split(t, p, ".")
				printf "%010d%010d%010d%s %s\n", p[1], p[2], p[3], pre, tag
			}' | LC_ALL=C sort -r | head -n 1 | cut -d" " -f2)"
	else
		version="$(printf '%s\n' "$tags" | head -n 1)"
	fi
	[ -n "$version" ] || die "could not find the newest $channel release (GitHub API unreachable?); pass --version vX.Y.Z"
fi
case "$version" in v[0-9]*) ;; *) version="v$version" ;; esac

# 3. Download and verify.
archive="veduta_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$version"
say "Downloading veduta $version for $os/$arch"
fetch "$base/$archive" "$tmp/$archive" || die "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "download failed: $base/checksums.txt"
want="$(grep "  $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)"
[ -n "$want" ] || die "checksums.txt has no entry for $archive"
got="$(sha256 "$tmp/$archive")"
[ "$got" = "$want" ] || die "checksum mismatch for $archive (got $got, want $want); nothing was installed"
say "Checksum OK ($got)"
tar -xzf "$tmp/$archive" -C "$tmp" veduta || die "cannot unpack $archive"

# 4. Install.
if [ -z "$prefix" ]; then
	prefix="${VEDUTA_HOME:+$VEDUTA_HOME/bin}"
	prefix="${prefix:-$HOME/.local/bin}"
	mkdir -p "$prefix" || die "cannot create $prefix"
fi
sudo=""
if [ -d "$prefix" ] && [ ! -w "$prefix" ]; then
	command -v sudo >/dev/null 2>&1 || die "$prefix is not writable and sudo is not available"
	sudo="sudo"
	say "$prefix is not writable: using sudo"
elif [ ! -d "$prefix" ]; then
	mkdir -p "$prefix" 2>/dev/null || { command -v sudo >/dev/null 2>&1 && sudo="sudo" && $sudo mkdir -p "$prefix"; } || die "cannot create $prefix"
fi
$sudo cp "$tmp/veduta" "$prefix/veduta.new"
$sudo chmod 0755 "$prefix/veduta.new"
$sudo mv -f "$prefix/veduta.new" "$prefix/veduta"
say "Installed $prefix/veduta"
case ":$PATH:" in
*":$prefix:"*) ;;
*) say "Note: $prefix is not on your PATH. Add this to your shell profile:"; say "  export PATH=\"$prefix:\$PATH\"" ;;
esac

# 5. Go.
go_ok() {
	command -v go >/dev/null 2>&1 || return 1
	v="$(go env GOVERSION 2>/dev/null || true)"
	minor="$(printf '%s' "$v" | sed -n 's/^go1\.\([0-9]*\).*/\1/p')"
	[ -n "$minor" ] && [ "$minor" -ge "$GO_MIN_MINOR" ]
}
if go_ok; then
	say "Go: $(go env GOVERSION)"
elif [ "$with_go" = "no" ]; then
	say "Go 1.$GO_MIN_MINOR or newer is required: install it from https://go.dev/dl/"
elif [ "$with_go" = "yes" ] || ask "Go 1.$GO_MIN_MINOR+ was not found. Install the latest Go into ~/.local/go?"; then
	gov="$(fetch_stdout "https://go.dev/VERSION?m=text" | head -n 1)"
	case "$gov" in go1.*) ;; *) die "cannot determine the latest Go version" ;; esac
	gotar="$gov.$os-$arch.tar.gz"
	if [ -x "$HOME/.local/go/bin/go" ] && [ "$("$HOME/.local/go/bin/go" env GOVERSION)" = "$gov" ]; then
		say "Go $gov already in ~/.local/go"
	else
		say "Downloading $gotar"
		fetch "https://dl.google.com/go/$gotar" "$tmp/$gotar" || die "Go download failed"
		fetch "https://dl.google.com/go/$gotar.sha256" "$tmp/$gotar.sha256" || die "Go checksum download failed"
		[ "$(sha256 "$tmp/$gotar")" = "$(tr -d ' \n' <"$tmp/$gotar.sha256")" ] || die "Go checksum mismatch"
		mkdir -p "$HOME/.local"
		rm -rf "$HOME/.local/go"
		tar -xzf "$tmp/$gotar" -C "$HOME/.local"
		say "Installed Go $gov in ~/.local/go"
	fi
	PATH="$HOME/.local/go/bin:$PATH"
	export PATH
	say "Add Go to your PATH:"
	say "  export PATH=\"\$HOME/.local/go/bin:\$PATH\""
fi

# 6. git.
if command -v git >/dev/null 2>&1; then
	say "git: $(git --version)"
else
	say "git is required (projects and releases use it): sudo apt install git"
fi

# 7. Doctor and next steps.
"$prefix/veduta" doctor || true
say ""
if [ "$channel" != stable ]; then
	# Installing from a channel does not subscribe to it: the tool reads its own
	# configuration, which only it writes.
	say "This is a $channel build. To keep receiving $channel releases:"
	say "  veduta update --channel $channel"
	say ""
fi
say "Next: veduta init mygame && cd mygame && claude"
