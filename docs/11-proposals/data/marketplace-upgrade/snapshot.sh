#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Take one labelled snapshot of a project for the marketplace-upgrade experiment.
#
#   ./snapshot.sh <label> <path/to/app.mpr>
#   ./snapshot.sh 01-before /workspaces/mxcli/mx-test-projects/test1-app/Test1App.mpr
#
# Every snapshot MUST be taken with this script rather than by hand: the
# before/after comparison is only meaningful if both sides used identical flags,
# and a --refs mismatch between two runs produces a diff of thousands of lines
# that has nothing to do with the upgrade.
set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "usage: $0 <label> <path/to/app.mpr>" >&2
    exit 2
fi

label=$1
project=$2
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
repo=$(cd "$here/../../../.." && pwd)
out="$here/$label"

if [[ ! -f $project ]]; then
    echo "error: no such project: $project" >&2
    exit 1
fi

mkdir -p "$out"

snap() { (cd "$repo" && go run ./scripts/mprsnapshot -p "$project" "$@"); }

# Whole project without --refs: coverage of every unit, at a size that stays
# readable. The reference lines are only needed for the modules under test.
snap >"$out/full.txt"

# The modules the experiment compares, with --refs so that a pointer rewrite is
# visible directly rather than only as a changed content hash.
#   Administration  — the module being upgraded (has entities + security)
#   DataWidgets     — widget-only module, different upgrade shape
#   MyFirstModule   — the consumer: generalization + retrieve of Administration.Account
for module in Administration DataWidgets MyFirstModule; do
    snap --module "$module" --refs >"$out/$module.txt"
done

echo "wrote $out"
wc -l "$out"/*.txt
