#!/usr/bin/bash
# qa-deployed-drift — repo-owned user scripts must match their deployed copies.
#
# WHY THIS EXISTS (Plan 00072)
# ----------------------------
# Plan 00067 fixed files/home/.local/bin/ftp-camera in the repo and never ran
# the play that deploys it, because its deploy.bash ran only play-rclone.yml
# while that script belongs to play-ftp-camera.yml. The repo said "fixed", the
# host ran the broken build, and the gap surfaced weeks later as a camera
# session that would not copy. Nothing in QA could see it: every mechanical
# check reads the REPO, and the repo was correct.
#
# This gate closes that hole by comparing the two. It is the one QA check whose
# subject is the host rather than the source tree.
#
# SCOPE: files/home/.local/bin/ — the user-facing tools. A file is checked only
# when a deployed copy already EXISTS, so a machine that never installed a
# feature is never nagged about it.
#
# stdout: terse — findings and a one-line summary.
# Exit 0 = in sync (or nothing deployed to compare against); 1 = drift found.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SRC_DIR="$REPO_ROOT/files/home/.local/bin"
DEPLOYED_DIR="$HOME/.local/bin"

# The CCY container mounts the repo at /workspace and deliberately has no
# deployed system state — comparing there would compare the repo to nothing.
# Same for a clean CI checkout. Neither is drift; both are "not the host".
if [ "$REPO_ROOT" = "/workspace" ]; then
    echo "✓ deployed-drift: skipped (CCY container — no deployed copies to compare)"
    exit 0
fi

# A LINKED WORKTREE is not the deployed branch. Reviewing a feature branch in
# .claude/worktrees/ would otherwise compare that branch's scripts against
# whatever the MAIN checkout deployed, producing an unclearable hard failure —
# you cannot deploy a review branch just to run QA on it, and you should not.
# Detected structurally: in a linked worktree, .git is a file and the common
# git dir differs from this tree's git dir.
git_dir=""
git_common=""
if git_dir=$(git -C "$REPO_ROOT" rev-parse --absolute-git-dir 2> /dev/null) \
    && git_common=$(git -C "$REPO_ROOT" rev-parse --path-format=absolute --git-common-dir 2> /dev/null); then
    if [ "$git_dir" != "$git_common" ]; then
        echo "✓ deployed-drift: skipped (linked git worktree — not the deployed checkout)"
        exit 0
    fi
fi

if [ ! -d "$DEPLOYED_DIR" ]; then
    echo "✓ deployed-drift: skipped ($DEPLOYED_DIR does not exist)"
    exit 0
fi

if [ ! -d "$SRC_DIR" ]; then
    echo "ERROR: source directory not found: $SRC_DIR" >&2
    exit 2
fi

# Which play deploys this file? Derived by searching the playbooks rather than
# from a hand-maintained table, so a script that moves between plays cannot make
# this gate lie.
#
# Two tiers, because not every script is named in a literal src: path. Several
# are deployed by a loop over a templated src —
#   src: "{{ root_dir }}/files/home/.local/bin/{{ item }}"
#   loop: [lan-scan, qnap-finder]
# — where the basename never appears next to the path. Searching only for the
# literal path reports those as orphaned, which is worse than unhelpful: it
# tells you to go looking for a play that does exist.
owning_play() {
    local basename="$1" hit escaped
    # Regex-escape the basename: several helpers contain a dot (rclone-rc-auth.bash)
    # and an unescaped one would match any character.
    escaped=${basename//./\\.}
    # Tier 1: an actual `src:` DECLARATION naming this file.
    #
    # Anchored to `src:` rather than matching the bare path anywhere in the file:
    # a play that merely MENTIONS the path in a comment — e.g. play-ftp-camera.yml
    # documenting its cross-play dependency on this library — was otherwise
    # reported as deploying it, sending the reader to a play that does no such
    # thing. The trailing boundary stops `ftp-camera` matching `ftp-camera-foo`.
    if hit=$(grep -rlE --include='*.yml' -- \
        "src:.*files/home/\.local/bin/${escaped}([\"' ]|\$)" \
        "$REPO_ROOT/playbooks"); then
        # A file can legitimately be deployed by more than one play; list all.
        printf '%s\n' "$hit" | while IFS= read -r p; do
            printf '    ansible-playbook %s\n' "${p#"$REPO_ROOT"/}"
        done
        return 0
    fi
    # Tier 2: a play that deploys into this directory via a templated src AND
    # mentions the basename somewhere (its loop list).
    if hit=$(grep -rl --include='*.yml' -- 'files/home/.local/bin/{{' \
        "$REPO_ROOT/playbooks"); then
        local found=0
        while IFS= read -r p; do
            if grep -q -- "$basename" "$p"; then
                printf '    ansible-playbook %s\n' "${p#"$REPO_ROOT"/}"
                found=1
            fi
        done <<< "$hit"
        if [ "$found" -eq 1 ]; then
            return 0
        fi
    fi
    printf '    (no playbook references this file — it may be orphaned)\n'
}

DRIFTED=0
CHECKED=0

for src in "$SRC_DIR"/*; do
    if [ ! -f "$src" ]; then
        continue
    fi
    name="$(basename "$src")"
    dep="$DEPLOYED_DIR/$name"
    if [ ! -f "$dep" ]; then
        continue
    fi
    CHECKED=$((CHECKED + 1))
    if cmp -s "$src" "$dep"; then
        continue
    fi
    DRIFTED=$((DRIFTED + 1))
    if [ "$DRIFTED" -eq 1 ]; then
        echo "✗ deployed-drift: repo changes that were never deployed" >&2
        echo >&2
    fi
    echo "  $name" >&2
    echo "    repo:     $src" >&2
    echo "    deployed: $dep" >&2
    echo "    deploy it with:" >&2
    owning_play "$name" >&2
    echo >&2
done

if [ "$DRIFTED" -gt 0 ]; then
    echo "  The repo and the host disagree. Deploy, test, THEN commit —" >&2
    echo "  see CLAUDE/InfrastructureAsCode.md (edit -> playbook -> deploy -> test)." >&2
    echo >&2
    echo "✗ deployed-drift: $DRIFTED of $CHECKED deployed script(s) differ from the repo" >&2
    exit 1
fi

echo "✓ deployed-drift: $CHECKED deployed script(s) match the repo"
exit 0
