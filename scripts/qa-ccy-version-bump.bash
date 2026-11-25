#!/usr/bin/env bash
# Judge the change from BASE to REV by the pre-commit hook's ccy version gates, as one commit
# (issue #428). A local hook can be absent (a checkout with no hooks installed, --no-verify); this
# check cannot. Without REV the change is the work tree as `git add -A` would commit it, built in a
# private index, so the real index is untouched. A shared clone is set up with HEAD at BASE and the
# change staged, and this checkout's hook runs there as `pre-commit --ccy-version-only`: the same
# gates and the same libraries (lib/ccy-staged.bash, lib/ccy-image-staged.bash and the image's
# image-source-paths), so a change to a ccy image source must move CCY_VERSION, the Dockerfile
# LABEL and REQUIRED_CONTAINER_VERSION together, whatever commits the range is made of. Merge
# commits are judged one by one by scripts/qa-ccy-merge-commits.bash.
# Usage: qa-ccy-version-bump.bash BASE [REV]   (run from the repository root)
# Exit codes: 0 passes, 1 refused, 2 cannot judge, 64 usage.
set -euo pipefail

# in_git <git args> — a git that fails cannot judge (exit 2); it is never a refusal.
in_git() {
    if ! git "$@"; then
        echo "ERROR: qa-ccy-version-bump: git $1 failed" >&2
        exit 2
    fi
}

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ] || [ -z "$1" ] || [ -z "${2-x}" ]; then
    echo "usage: qa-ccy-version-bump.bash BASE [REV]" >&2
    exit 64
fi
hook="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/git-hooks/pre-commit"
base=$(in_git rev-parse -q --verify "$1^{commit}")
scratch=$(mktemp -d)
trap 'rm -rf "$scratch"' EXIT
if [ "$#" -eq 2 ]; then
    tree=$(in_git rev-parse -q --verify "$2^{tree}")
else
    export GIT_INDEX_FILE="$scratch/index"
    in_git read-tree HEAD
    in_git add -A
    tree=$(in_git write-tree)
    unset GIT_INDEX_FILE
fi

in_git clone -q --shared --no-checkout . "$scratch/repo"
in_git -C "$scratch/repo" read-tree --reset -u "$tree"
in_git -C "$scratch/repo" update-ref --no-deref HEAD "$base"
echo "🔍 the change $base..${2:-work tree} by the ccy version gates"
rc=0
(cd "$scratch/repo" && bash "$hook" --ccy-version-only) || rc=$?
if [ "$rc" -eq 0 ]; then
    echo "qa-ccy-version-bump: the ccy and container versions move with every ccy change"
elif [ "$rc" -eq 1 ]; then
    echo "✗ qa-ccy-version-bump: $base..${2:-work tree} changes ccy without the version bump (above)" >&2
else
    echo "ERROR: qa-ccy-version-bump: the hook could not judge the change (exit $rc)" >&2
    exit 2
fi
exit "$rc"
