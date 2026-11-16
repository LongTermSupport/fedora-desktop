#!/usr/bin/env bash
# Judge every merge commit of RANGE by the pre-commit hook's ccy version gates, as if it were being
# committed now (issue #311). A local hook can be skipped (--no-verify, a client with none); this
# CI step cannot. Each merge is rebuilt in a shared clone: HEAD at its first parent, its own tree
# checked out and staged, its second parent as MERGE_HEAD. This checkout's hook then runs there as
# `pre-commit --ccy-version-only --protected-tip TIP` (the gates alone), TIP being the protected
# line's tip before the merges: after a push the line holds both parents.
# Usage: qa-ccy-merge-commits.bash TIP RANGE   (run from the repository root)
# Exit codes: 0 every merge passes, 1 a merge is refused, 2 cannot judge, 64 usage.
set -euo pipefail

# in_clone <git args> — git in the scratch clone; a failure there cannot judge (exit 2), it is
# never a refusal.
in_clone() {
    if ! git -C "$scratch/repo" "$@"; then
        echo "ERROR: qa-ccy-merge-commits: git $1 failed in the scratch clone" >&2
        exit 2
    fi
}

if [ "$#" -ne 2 ] || [ -z "$1" ] || [ -z "$2" ]; then
    echo "usage: qa-ccy-merge-commits.bash TIP RANGE" >&2
    exit 64
fi
tip=$1 range=$2
hook="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/git-hooks/pre-commit"
if ! tip=$(git rev-parse -q --verify "$tip^{commit}"); then
    echo "ERROR: qa-ccy-merge-commits: the tip $1 is not a commit" >&2
    exit 2
fi
if ! merges=$(git rev-list --merges --reverse "$range"); then
    echo "ERROR: qa-ccy-merge-commits: git cannot list $range" >&2
    exit 2
fi
if [ -z "$merges" ]; then
    echo "qa-ccy-merge-commits: no merge commits in $range"
    exit 0
fi

scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
git clone -q --shared --no-checkout . "$scratch/repo"
# Git refuses update-ref on MERGE_HEAD, a pseudo-ref: it is the file a merge in progress writes.
merge_head=$(git -C "$scratch/repo" rev-parse --path-format=absolute --git-path MERGE_HEAD)
refused=0
for merge in $merges; do
    parents=$(git rev-list --no-walk --parents "$merge")
    read -r self first second extra <<<"$parents"
    if [ -n "${extra:-}" ]; then
        echo "✗ $merge: an octopus merge, which the version gates cannot judge" >&2
        refused=1
        continue
    fi
    # The tree replaces whatever the last merge left staged, then HEAD moves to the first parent.
    in_clone read-tree --reset -u "$merge"
    in_clone update-ref --no-deref HEAD "$first"
    echo "$second" >"$merge_head"
    echo "🔍 merge $self ($first + $second)"
    rc=0
    (cd "$scratch/repo" && bash "$hook" --ccy-version-only --protected-tip "$tip") || rc=$?
    case "$rc" in
    0) ;;
    1) echo "✗ merge $merge is refused by the ccy version gates (above)" >&2 && refused=1 ;;
    *) echo "ERROR: qa-ccy-merge-commits: the hook could not judge merge $merge (exit $rc)" >&2 && exit 2 ;;
    esac
    rm "$merge_head"
done
exit "$refused"
