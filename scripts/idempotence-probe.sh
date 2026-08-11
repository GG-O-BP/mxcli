#!/bin/bash
# idempotence-probe.sh — how many documents would a "skip unchanged writes"
# change actually skip on this project?
#
# Runs an MDL script twice against a copy of a project and compares per-unit
# CANONICAL digests: the document with element IDs normalised away. Two runs that
# are canonically identical are runs where the write changed nothing but the
# UUIDs it happened to mint — exactly the writes ADR-0008 proposes to skip.
#
# Read-only with respect to mxcli's behaviour: it measures the current binary,
# it does not change how anything is written.
#
# Each unit lands in one of three buckets:
#
#   identical      would be skipped today
#   volatile-only  differs ONLY in a field mxcli regenerates by policy
#                  (StableId) — skippable once that policy changes
#   real           differs for some other reason — investigate, this is the
#                  bucket that decides whether the approach works at all
#
# Usage:
#   scripts/idempotence-probe.sh -p /path/to/App.mpr -s script.mdl [-s more.mdl ...]
#
# The project is copied first; the original is never written to.
set -euo pipefail

MXCLI="${MXCLI:-./bin/mxcli}"
PROJECT=""
SCRIPTS=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    -p) PROJECT="$2"; shift 2 ;;
    -s) SCRIPTS+=("$2"); shift 2 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

if [[ -z "$PROJECT" || ${#SCRIPTS[@]} -eq 0 ]]; then
  echo "usage: $0 -p <project.mpr> -s <script.mdl> [-s <script.mdl> ...]" >&2
  exit 2
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

SRC_DIR="$(cd "$(dirname "$PROJECT")" && pwd)"
MPR_NAME="$(basename "$PROJECT")"
cp -r "$SRC_DIR" "$WORK/proj"
rm -rf "$WORK/proj/.mxcli"
COPY="$WORK/proj/$MPR_NAME"

apply() {
  for s in "${SCRIPTS[@]}"; do
    "$MXCLI" exec "$s" -p "$COPY" >/dev/null 2>&1 || {
      echo "error: mxcli exec failed on $s — the probe needs a script that applies cleanly" >&2
      exit 1
    }
  done
}

snap() { go run ./scripts/mprsnapshot -p "$COPY" -canon 2>/dev/null | grep '^C' || true; }

# Run once to reach the fixed point the scripts describe, then measure the two
# runs after it. Measuring from a pristine project would compare "created" with
# "re-created" and report differences that are real.
echo "applying scripts (baseline)..." >&2
apply
echo "run 1..." >&2; apply; snap > "$WORK/a.txt"
echo "run 2..." >&2; apply; snap > "$WORK/b.txt"

python3 - "$WORK/a.txt" "$WORK/b.txt" <<'PY'
import sys
def load(p):
    d = {}
    for line in open(p):
        f = line.rstrip('\n').split('\t')
        if f[0] == 'C':
            d[f[1]] = (f[2], f[3], f[4], f[5])   # id -> path, type, canon, masked
    return d

a, b = load(sys.argv[1]), load(sys.argv[2])
only_a, only_b = set(a) - set(b), set(b) - set(a)

same = vol = real = 0
details = []
for k in sorted(set(a) & set(b)):
    path, typ, ca, ma = a[k]
    _, _, cb, mb = b[k]
    if ca == cb:
        same += 1
    elif ma == mb:
        vol += 1
        details.append(("volatile-only", path, typ))
    else:
        real += 1
        details.append(("REAL", path, typ))

total = len(set(a) | set(b))
print(f"units                {total}")
print(f"  identical          {same}   <- would be skipped today")
print(f"  volatile-only      {vol}   <- skippable once StableId is frozen")
print(f"  real differences   {real}   <- investigate")
if only_a or only_b:
    print(f"  units added/removed between runs: {len(only_a)}/{len(only_b)}  <- not idempotent at unit level")

if details:
    print()
    for kind, path, typ in sorted(details):
        print(f"  {kind:<14} {path}  [{typ}]")

# A probe that classified nothing proves nothing.
if total == 0:
    print("\nNO UNITS COMPARED — the probe measured nothing; check -p and the snapshot filter.")
    sys.exit(1)
PY
