#!/bin/sh
# Installs jira (https://github.com/ngavilan-dogfy/jira-cli) step by step:
# finds the right build for your computer, checks it, puts it on your PATH
# and offers to connect it to your Jira.
#
#   curl -fsSL https://raw.githubusercontent.com/ngavilan-dogfy/jira-cli/main/install.sh | sh
#
# Options (environment variables):
#   JIRA_INSTALL_DIR=~/bin   where to put jira (default: ~/.local/bin)
#   JIRA_VERSION=v0.2.0      a specific release (default: the latest)
#   JIRA_NO_SETUP=1          don't offer to run 'jira setup' at the end
#   JIRA_NO_MODIFY_PATH=1    never touch your shell's config file
#
# Nothing here needs sudo. Run it again any time to update.

set -eu

REPO="ngavilan-dogfy/jira-cli"
RELEASES="${JIRA_RELEASES_URL:-https://github.com/$REPO/releases}"
INSTALL_DIR="${JIRA_INSTALL_DIR:-$HOME/.local/bin}"
TMP=""

# ─── output ──────────────────────────────────────────────────────

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	B=$(printf '\033[1m') DIM=$(printf '\033[90m') OK=$(printf '\033[32m')
	WARN=$(printf '\033[33m') ERR=$(printf '\033[31m') ACC=$(printf '\033[35m') R=$(printf '\033[0m')
else
	B="" DIM="" OK="" WARN="" ERR="" ACC="" R=""
fi

say()  { printf '  %s\n' "$*"; }
ok()   { printf '  %s●%s %s\n' "$OK" "$R" "$*"; }
info() { printf '  %s·%s %s\n' "$DIM" "$R" "$*"; }
warn() { printf '  %s!%s %s\n' "$WARN" "$R" "$*"; }
hint() { printf '    %s%s%s\n' "$DIM" "$*" "$R"; }
fail() {
	printf '  %s×%s %s\n' "$ERR" "$R" "$1" >&2
	shift
	for line in "$@"; do printf '    %s\n' "$line" >&2; done
	printf '\n  %sStuck? Open an issue: https://github.com/%s/issues%s\n\n' "$DIM" "$REPO" "$R" >&2
	exit 1
}

cleanup() { if [ -n "$TMP" ]; then rm -rf "$TMP"; fi; }
trap cleanup EXIT
trap 'cleanup; printf "\n"; exit 130' INT TERM

# tildify shows paths under your home as ~/…
tildify() {
	# shellcheck disable=SC2088 # a literal ~ for display
	case "$1" in
	"$HOME"/*) printf '~/%s' "${1#"$HOME"/}" ;;
	*) printf '%s' "$1" ;;
	esac
}

# can_ask: is there a person at a terminal to answer questions? With
# 'curl | sh' stdin is the script itself, so questions go to /dev/tty.
can_ask() { [ -t 1 ] && (: </dev/tty) 2>/dev/null; }

# ask QUESTION → yes (0) / no (1); Enter means yes.
ask() {
	printf '  %s?%s %s %s[Y/n]%s ' "$ACC" "$R" "$1" "$DIM" "$R"
	read -r answer </dev/tty || answer=n
	case "$answer" in [nN]*) return 1 ;; *) return 0 ;; esac
}

# ─── what computer is this? ──────────────────────────────────────

detect_platform() {
	case "$(uname -s)" in
	Darwin) OS=darwin OS_NAME=macOS ;;
	Linux) OS=linux OS_NAME=Linux ;;
	MINGW* | MSYS* | CYGWIN*)
		fail "This installer is for macOS and Linux." \
			"On Windows, download jira-windows-amd64.exe from $RELEASES/latest," \
			"rename it to jira.exe, put it in a folder on your PATH, then run: jira setup"
		;;
	*) fail "Unsupported system: $(uname -s)" "Build it from source: go install github.com/$REPO/cmd/jira@latest" ;;
	esac
	case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	arm64 | aarch64) ARCH=arm64 ;;
	*) fail "Unsupported processor: $(uname -m)" "Build it from source: go install github.com/$REPO/cmd/jira@latest" ;;
	esac
	# A shell running under Rosetta reports x86_64 on Apple Silicon.
	if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
		ARCH=arm64
	fi
	case "$OS/$ARCH" in
	darwin/arm64) CPU="Apple Silicon" ;;
	darwin/amd64) CPU="Intel" ;;
	*) CPU="$ARCH" ;;
	esac
}

# ─── downloads ───────────────────────────────────────────────────

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL --retry 2 -o "$2" "$1"; }
	final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
	final_url() { wget -q -S --spider "$1" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1 | tr -d '\r'; }
else
	fail "Neither curl nor wget is installed." "Install one of them (e.g. sudo apt install curl) and run this again."
fi

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d' ' -f1
	else
		openssl dgst -sha256 "$1" | sed 's/.*= //'
	fi
}

# latest_tag resolves "latest" through the releases page redirect
# (…/releases/latest → …/releases/tag/v1.2.3), which needs no API token.
latest_tag() {
	url=$(final_url "$RELEASES/latest" 2>/dev/null || true)
	case "$url" in
	*/tag/*) printf '%s' "${url##*/tag/}" ;;
	*) printf '' ;;
	esac
}

# ─── fallback: build from source ─────────────────────────────────

build_from_source() {
	info "$1"
	if ! command -v go >/dev/null 2>&1; then
		fail "No ready-made build to download, and Go isn't installed to build one." \
			"Install Go from https://go.dev/dl/ and run this installer again," \
			"or ask whoever shared jira with you for a build for $OS_NAME ($CPU)."
	fi
	say "Building from source with $(go version | cut -d' ' -f3) (takes a minute)…"
	mkdir -p "$INSTALL_DIR"
	GOBIN="$INSTALL_DIR" go install "github.com/$REPO/cmd/jira@${JIRA_VERSION:-latest}" ||
		fail "The build failed." "Check the error above; your Go may be too old (jira needs Go 1.25+)."
	ok "Built and installed $(tildify "$INSTALL_DIR/jira")"
}

# ─── PATH ────────────────────────────────────────────────────────

on_path() {
	case ":$PATH:" in *":$INSTALL_DIR:"*) return 0 ;; *) return 1 ;; esac
}

# shell_rc: the file your shell reads at startup, and the line to add.
shell_rc() {
	dir="$INSTALL_DIR"
	case "$dir" in "$HOME"/*) dir="\$HOME/${dir#"$HOME"/}" ;; esac
	LINE="export PATH=\"$dir:\$PATH\""
	case "$(basename "${SHELL:-sh}")" in
	zsh) RC="${ZDOTDIR:-$HOME}/.zshrc" ;;
	bash) if [ "$OS" = darwin ]; then RC="$HOME/.bash_profile"; else RC="$HOME/.bashrc"; fi ;;
	fish)
		RC="$HOME/.config/fish/config.fish"
		LINE="fish_add_path \"$dir\""
		;;
	*) RC="$HOME/.profile" ;;
	esac
}

fix_path() {
	shell_rc
	warn "$(tildify "$INSTALL_DIR") isn't in your PATH yet, so typing 'jira' won't find it."
	if [ -z "${JIRA_NO_MODIFY_PATH:-}" ] && can_ask && ask "Add it for you? (one line at the end of $(tildify "$RC"))"; then
		mkdir -p "$(dirname "$RC")"
		printf '\n# Added by the jira installer\n%s\n' "$LINE" >>"$RC"
		ok "Added to $(tildify "$RC") — new terminals will find jira"
		PATH_CHANGED=1
	else
		hint "Add this line to $(tildify "$RC") and open a new terminal:"
		hint "  $LINE"
	fi
}

# configured: jira already knows your Jira (a saved profile or env vars).
configured() {
	[ -n "${JIRA_TOKEN:-}" ] && return 0
	for f in "$HOME/.config/jira-cli/profiles/"*.yaml; do
		[ -f "$f" ] && return 0
	done
	return 1
}

# ─── main ────────────────────────────────────────────────────────

main() {
	printf '\n  %sjira%s installer\n\n' "$ACC$B" "$R"

	detect_platform
	ok "Your computer: $OS_NAME · $CPU"

	previous=""
	if [ -x "$INSTALL_DIR/jira" ]; then
		previous=$("$INSTALL_DIR/jira" --version 2>/dev/null | sed 's/^jira version //' || true)
	fi

	TMP=$(mktemp -d 2>/dev/null || mktemp -d -t jira-install)
	version="${JIRA_VERSION:-}"
	if [ -z "$version" ]; then
		version=$(latest_tag)
	fi

	if [ -z "$version" ]; then
		build_from_source "No release published yet."
	else
		if [ -n "${JIRA_VERSION:-}" ]; then ok "Release: $version"; else ok "Latest release: $version"; fi
		asset="jira-${OS}-${ARCH}"
		if ! fetch "$RELEASES/download/$version/checksums.txt" "$TMP/checksums.txt" 2>/dev/null; then
			[ -z "${JIRA_VERSION:-}" ] || fail "There's no release $version." "See which ones exist: $RELEASES"
			build_from_source "Release $version can't be downloaded right now."
		elif ! fetch "$RELEASES/download/$version/$asset" "$TMP/$asset" 2>/dev/null; then
			build_from_source "Release $version has no build for $OS_NAME ($CPU)."
		else
			size=$(wc -c <"$TMP/$asset" | tr -d ' ')
			ok "Downloaded $asset ($((size / 1024 / 1024)).$((size / 1024 % 1024 * 10 / 1024)) MB)"

			want=$(awk -v f="$asset" '$2 == f || $2 == "*" f { print $1 }' "$TMP/checksums.txt")
			got=$(sha256_of "$TMP/$asset")
			[ -n "$want" ] || fail "$asset isn't listed in checksums.txt." "Not installing an unverified file."
			[ "$want" = "$got" ] || fail "Checksum mismatch: the download is corrupt or was tampered with." \
				"Nothing was installed. Try again; if it keeps happening, open an issue."
			ok "Checksum verified"

			mv "$TMP/$asset" "$TMP/jira"
			chmod 755 "$TMP/jira"
			"$TMP/jira" --version >/dev/null 2>&1 ||
				fail "The downloaded jira doesn't run on this computer." "Nothing was installed."

			mkdir -p "$INSTALL_DIR" 2>/dev/null ||
				fail "Can't create $(tildify "$INSTALL_DIR")." "Choose another folder: JIRA_INSTALL_DIR=~/bin sh install.sh"
			[ -w "$INSTALL_DIR" ] ||
				fail "Can't write to $(tildify "$INSTALL_DIR")." "Choose another folder: JIRA_INSTALL_DIR=~/bin sh install.sh"
			# Move into place in one step: a running jira is never half-written.
			cp "$TMP/jira" "$INSTALL_DIR/.jira.new" && mv -f "$INSTALL_DIR/.jira.new" "$INSTALL_DIR/jira"
			# macOS: files from the internet get quarantined; this one was verified.
			if [ "$OS" = darwin ]; then xattr -d com.apple.quarantine "$INSTALL_DIR/jira" 2>/dev/null || true; fi
			ok "Installed $(tildify "$INSTALL_DIR/jira")"
		fi
	fi

	now=$("$INSTALL_DIR/jira" --version 2>/dev/null | sed 's/^jira version //' || true)
	[ -n "$now" ] || fail "jira was installed but doesn't start." "Run $(tildify "$INSTALL_DIR/jira") --version to see why."
	if [ -n "$previous" ] && [ "$previous" != "$now" ]; then
		ok "Updated from $previous to $now"
	fi

	# The Claude Code skill ships inside the binary: refresh it if it's there.
	if [ -f "$HOME/.claude/skills/jira/SKILL.md" ] && "$INSTALL_DIR/jira" skill install --quiet >/dev/null 2>&1; then
		ok "Claude Code skill refreshed"
	fi

	PATH_CHANGED=""
	if ! on_path; then
		fix_path
	fi

	# Another jira earlier in PATH would shadow this one.
	other=$(command -v jira 2>/dev/null || true)
	if on_path && [ -n "$other" ] && [ "$other" != "$INSTALL_DIR/jira" ]; then
		warn "Typing 'jira' runs a different program: $(tildify "$other")"
		hint "Maybe another Jira CLI (e.g. from Homebrew). Remove it, or put $(tildify "$INSTALL_DIR") first in your PATH."
	fi

	printf '\n'
	if [ -d "$HOME/.claude" ] && [ ! -f "$HOME/.claude/skills/jira/SKILL.md" ]; then
		info "Using Claude Code? ${B}jira skill install${R} teaches it this CLI (/jira)."
	fi
	if [ -n "$previous" ] && configured; then
		say "${OK}Done.${R} Your settings are untouched. ${DIM}What's new: $RELEASES${R}"
		say "${DIM}Later updates: jira update${R}"
		printf '\n'
		return
	fi
	say "${B}Next: connect jira to your Jira${R} — about two minutes."
	if [ -z "${JIRA_NO_SETUP:-}" ] && can_ask && ask "Run 'jira setup' now?"; then
		printf '\n'
		"$INSTALL_DIR/jira" setup </dev/tty || true
		if ! on_path; then
			printf '\n'
			info "Open a new terminal to use 'jira' (this one doesn't know the new PATH yet)."
		fi
	elif on_path; then
		hint "Run: jira setup"
	elif [ -n "$PATH_CHANGED" ]; then
		hint "Open a new terminal and run: jira setup"
	else
		hint "Once PATH is set, run: jira setup   (or right now: $(tildify "$INSTALL_DIR/jira") setup)"
	fi
	printf '\n'
}

main "$@"
