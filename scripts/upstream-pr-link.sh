#!/bin/bash

# upstream-pr-link.sh — print a prefilled GitHub compare URL for opening a PR
# that merges this fork's branch into an upstream fork's branch.
#
# Why: mendixlabs/mxcli (the upstream) is not in tooling scope, so we can't open
# the PR via API. Instead we generate a cross-fork *compare* URL with the title
# and body prefilled, which the user opens in a browser and clicks "Create".
#
# The mechanical part — URL-encoding a multi-line title + Markdown body into the
# `?title=…&body=…` query — is what this script automates.
#
# Usage:
#   scripts/upstream-pr-link.sh \
#       [--upstream mendixlabs/mxcli] [--fork ako/mxcli] \
#       [--base main] [--head main] \
#       [--title "…"] [--body-file path] \
#       [--commits <git-range>]
#
# Defaults reproduce the common case: merge ako/mxcli:main → mendixlabs/mxcli:main.
#
#   --title       PR title. Default: "Sync <fork>: <head> → <upstream>:<base>".
#   --body-file   File whose contents become the PR body (Markdown). Use "-" for stdin.
#   --commits     A git revision range (e.g. origin/upstream-main..HEAD). When given
#                 and no --body-file is set, the body is auto-built from the
#                 one-line commit log over that range.
#
# Examples:
#   # simplest — just the link with default title and a one-line body:
#   scripts/upstream-pr-link.sh
#
#   # supply a hand-written body:
#   scripts/upstream-pr-link.sh --body-file /tmp/pr-body.md
#
#   # auto-build the body from commits not yet upstream:
#   git fetch https://github.com/mendixlabs/mxcli main
#   scripts/upstream-pr-link.sh --commits FETCH_HEAD..HEAD

set -euo pipefail

UPSTREAM="mendixlabs/mxcli"
FORK="ako/mxcli"
BASE="main"
HEAD="main"
TITLE=""
BODY_FILE=""
COMMITS=""

usage() {
	sed -n '3,40p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-2}"
}

while [[ $# -gt 0 ]]; do
	case "$1" in
		--upstream)  UPSTREAM="${2:?--upstream requires owner/repo}"; shift 2 ;;
		--fork)      FORK="${2:?--fork requires owner/repo}"; shift 2 ;;
		--base)      BASE="${2:?--base requires a branch}"; shift 2 ;;
		--head)      HEAD="${2:?--head requires a branch}"; shift 2 ;;
		--title)     TITLE="${2:?--title requires text}"; shift 2 ;;
		--body-file) BODY_FILE="${2:?--body-file requires a path}"; shift 2 ;;
		--commits)   COMMITS="${2:?--commits requires a git range}"; shift 2 ;;
		-h|--help)   usage 0 ;;
		*) echo "unknown argument: $1" >&2; usage 2 ;;
	esac
done

# owner:repo (fork owner) form for the cross-fork head ref in a compare URL.
FORK_OWNER="${FORK%%/*}"
FORK_REPO="${FORK##*/}"

if [[ -z "$TITLE" ]]; then
	TITLE="Sync ${FORK}: ${HEAD} → ${UPSTREAM}:${BASE}"
fi

# Resolve the body text.
BODY=""
if [[ -n "$BODY_FILE" ]]; then
	if [[ "$BODY_FILE" == "-" ]]; then
		BODY="$(cat)"
	else
		BODY="$(cat "$BODY_FILE")"
	fi
elif [[ -n "$COMMITS" ]]; then
	BODY="Merges \`${FORK}:${HEAD}\` into \`${UPSTREAM}:${BASE}\`."$'\n\n'"### Commits"$'\n'
	BODY+="$(git log --no-merges --pretty='- %s' "$COMMITS")"
else
	BODY="Merges \`${FORK}:${HEAD}\` into \`${UPSTREAM}:${BASE}\`."
fi

# URL-encode title/body and assemble the compare URL. Python3 handles RFC-3986
# percent-encoding of newlines, backticks, and Markdown reliably.
TITLE="$TITLE" BODY="$BODY" \
UPSTREAM="$UPSTREAM" BASE="$BASE" FORK_OWNER="$FORK_OWNER" FORK_REPO="$FORK_REPO" HEAD="$HEAD" \
python3 - <<'PY'
import os, urllib.parse

upstream = os.environ["UPSTREAM"]
base     = os.environ["BASE"]
owner    = os.environ["FORK_OWNER"]
repo     = os.environ["FORK_REPO"]
head     = os.environ["HEAD"]

q = urllib.parse.urlencode({
    "expand": "1",
    "title": os.environ["TITLE"],
    "body":  os.environ["BODY"],
})

print(f"https://github.com/{upstream}/compare/{base}...{owner}:{repo}:{head}?{q}")
PY
