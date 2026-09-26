#!/bin/sh
# Re-records assets/setup.gif: the e2e build of jira pointed at a tiny fake
# Jira (scripts/record/fakejira.py), a throwaway HOME whose git config keeps a
# personal and a work identity, and a made-up API token copied to the
# clipboard 2.5s after setup "opens" Atlassian's token page (the e2e build
# logs the URL instead of opening it). macOS only (pbcopy); needs vhs.
#
#   make e2e && scripts/record-setup.sh
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
[ -x "$root/bin/jira-e2e" ] || { echo "build it first: make e2e" >&2; exit 1; }
command -v vhs >/dev/null 2>&1 || { echo "needs vhs: brew install vhs" >&2; exit 1; }

token="ATATT3xFfGF0$(printf 'aB3_x-Q9%.0s' 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20)=1A2B3C4D"
port=18431
work="$(mktemp -d)"
mkdir -p "$work/home" "$work/bin"
printf '[user]\n\temail = ana.personal@example.org\n[includeIf "gitdir:~/work/"]\n\tpath = ~/.gitconfig-work\n' > "$work/home/.gitconfig"
printf '[user]\n\temail = ana@acme.com\n' > "$work/home/.gitconfig-work"
ln -s "$root/bin/jira-e2e" "$work/bin/jira"
: > "$work/open.log"
sed -e "s|@R@|$work|g" "$root/scripts/record/setup.tape.in" > "$work/setup.tape"

python3 "$root/scripts/record/fakejira.py" "$port" "$token" &
fake=$!
clip="$(pbpaste 2>/dev/null || true)"
trap 'kill "$fake" 2>/dev/null; wait "$fake" 2>/dev/null; printf "%s" "$clip" | pbcopy; rm -rf "$work"' EXIT
printf 'nothing here' | pbcopy
(
	i=0
	while [ "$i" -lt 600 ]; do
		if grep -q "id.atlassian.com" "$work/open.log"; then
			sleep 2.5
			printf '%s' "$token" | pbcopy
			exit 0
		fi
		sleep 0.1
		i=$((i + 1))
	done
) &
sleep 1
(cd "$work" && vhs setup.tape)
cp "$work/setup.gif" "$root/assets/setup.gif"
echo "assets/setup.gif updated: look at it before committing"
