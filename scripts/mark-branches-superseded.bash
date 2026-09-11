#!/usr/bin/env bash
#
# mark-branches-superseded.bash — stamp a "NO LONGER ACTIVE BRANCH" banner onto
# the README of every retired F<VERSION> branch.
#
# This repo uses one branch per Fedora release (F42, F43, F44…), and the newest
# one is the GitHub default. Anyone landing on an old branch — from a search
# result, a stale bookmark, or an old link — sees a README that looks current
# and gives no hint it is abandoned. This script puts an unmissable banner at
# the top of those READMEs pointing at the active branch.
#
# It re-stamps EVERY retired branch on every run, not just the newly retired
# one. The banner names the current branch by version, so when F45 lands the
# banners on F42/F43/F44 must all be refreshed or they will advertise a branch
# that is itself no longer current. Re-stamping is idempotent: a branch whose
# banner is already correct is left alone and produces no commit.
#
# Run it as part of retiring a branch — see docs/development.md, "Creating New
# Version Branch".
#
# Each branch is edited in its own throwaway git worktree, so the checkout you
# run this from is never switched and an interrupted run cannot strand you on
# someone else's branch.
#
# Exit status: 0 = every retired branch is correctly stamped (whether or not
# this run changed anything); 2 = a hard error (missing tool, no active branch
# resolvable, a worktree or push that failed).
#
# Usage:
#   scripts/mark-branches-superseded.bash                 # stamp and push
#   scripts/mark-branches-superseded.bash --dry-run       # preview, change nothing
#   scripts/mark-branches-superseded.bash --current F45   # override active branch
#   scripts/mark-branches-superseded.bash --help
#
# Requires: git, and gh (authenticated) unless --current is given.

set -euo pipefail

readonly BANNER_START='<!-- SUPERSEDED-BANNER:START -->'
readonly BANNER_END='<!-- SUPERSEDED-BANNER:END -->'
readonly REPO_URL='https://github.com/LongTermSupport/fedora-desktop'

DRY_RUN=false
ACTIVE_BRANCH=''
WORKTREE_DIR=''

usage() {
    cat <<'EOF'
mark-branches-superseded.bash — stamp a "no longer active" banner onto retired
F<VERSION> branch READMEs.

Usage:
  scripts/mark-branches-superseded.bash [options]

Options:
  --current BRANCH   Treat BRANCH as the active branch instead of asking GitHub
                     for the repository default. Useful before the default has
                     been switched over.
  --dry-run          Print the banner each branch would get and change nothing.
  -h, --help         Show this help.

Stdout is the machine-readable result, one "<branch> <status>" line per retired
branch, where status is one of: stamped, current, would-stamp.

Branches are read from, and written straight back to, origin — the published
state is what needs the banner. Your local branch refs are left untouched;
run `git fetch` afterwards to see the new commits.
EOF
}

log() { printf '%s\n' "$*" >&2; }

die() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 2
}

# Removes the current worktree. Runs on EXIT, including the failure paths, so it
# reports a removal failure rather than aborting — the original error is already
# on its way out and must not be masked by this one.
cleanup() {
    [[ -n "$WORKTREE_DIR" && -d "$WORKTREE_DIR" ]] || return 0
    local dir="$WORKTREE_DIR"
    WORKTREE_DIR=''
    if ! git worktree remove --force "$dir"; then
        log "WARNING: could not remove worktree $dir"
        log "         remove it by hand: git worktree remove --force $dir"
    fi
}
trap cleanup EXIT

# Discards the resolved path on stdout, which is the payload here; command -v
# writes nothing to stderr, so no diagnostic is being swallowed.
require_tool() {
    command -v "$1" >/dev/null || die "required tool not found: $1"
}

# The active branch is whatever GitHub currently serves as the repository
# default, which is the single source of truth for "current Fedora version".
resolve_active_branch() {
    if [[ -n "$ACTIVE_BRANCH" ]]; then
        printf '%s' "$ACTIVE_BRANCH"
        return
    fi
    require_tool gh
    local resolved
    resolved="$(gh repo view --json defaultBranchRef -q .defaultBranchRef.name)" \
        || die "could not ask GitHub for the default branch; pass --current BRANCH"
    [[ -n "$resolved" ]] || die "GitHub returned an empty default branch; pass --current BRANCH"
    printf '%s' "$resolved"
}

# Every remote F<digits> branch except the active one, oldest first.
retired_branches() {
    local active="$1"
    git for-each-ref --format='%(refname:strip=3)' 'refs/remotes/origin/F[0-9]*' \
        | grep -E '^F[0-9]+$' \
        | grep -vxF "$active" \
        | sort -V
}

banner_for() {
    local branch="$1" active="$2"
    cat <<EOF
$BANNER_START
> [!CAUTION]
> # NO LONGER ACTIVE BRANCH — CURRENT BRANCH IS $active
>
> This branch targets **Fedora ${branch#F}** and is **no longer maintained**.
> It is kept only so machines still on that release have a matching checkout.
>
> ### [Go to $active, the current branch]($REPO_URL)
>
> \`\`\`bash
> git fetch origin && git checkout $active
> \`\`\`
>
> Documentation, playbooks and versions below this banner describe
> **Fedora ${branch#F}** and may be wrong for any later release.
$BANNER_END
EOF
}

# Rewrite README.md so it starts with the banner, dropping any previous banner
# block. Marker-delimited so re-running replaces rather than accumulates.
stamp_readme() {
    local readme="$1" banner="$2" body
    body="$(awk -v s="$BANNER_START" -v e="$BANNER_END" '
        $0 == s { skipping = 1; next }
        $0 == e { skipping = 0; next }
        !skipping { print }
    ' "$readme")"
    # Drop leading blank lines left behind by a removed banner so the spacing
    # below the new one does not grow on every re-stamp.
    body="${body#"${body%%[![:space:]]*}"}"
    printf '%s\n\n%s\n' "$banner" "$body" >"$readme"
}

process_branch() {
    local branch="$1" active="$2" banner readme
    banner="$(banner_for "$branch" "$active")"

    WORKTREE_DIR="$(mktemp -d -t superseded-XXXXXX)"
    git worktree add --quiet --detach "$WORKTREE_DIR" "origin/$branch" \
        || die "could not create a worktree for $branch"

    readme="$WORKTREE_DIR/README.md"
    [[ -f "$readme" ]] || die "$branch has no README.md at its root"

    stamp_readme "$readme" "$banner"

    if git -C "$WORKTREE_DIR" diff --quiet -- README.md; then
        log "  $branch: banner already correct"
        printf '%s current\n' "$branch"
        cleanup
        return
    fi

    if [[ "$DRY_RUN" == true ]]; then
        log "  $branch: would stamp this banner (naming $active):"
        log "$banner"
        printf '%s would-stamp\n' "$branch"
        cleanup
        return
    fi

    git -C "$WORKTREE_DIR" add README.md
    git -C "$WORKTREE_DIR" -c "advice.detachedHead=false" commit --quiet \
        -m "README: mark $branch superseded, current branch is $active" \
        || die "commit failed on $branch"

    # Committed detached, so push the new commit explicitly at the branch ref.
    git -C "$WORKTREE_DIR" push --quiet origin "HEAD:refs/heads/$branch" \
        || die "push failed for $branch"
    log "  $branch: stamped and pushed (naming $active)"
    printf '%s stamped\n' "$branch"
    cleanup
}

main() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --current)
                [[ $# -ge 2 ]] || die "--current needs a branch name"
                ACTIVE_BRANCH="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            -h | --help)
                usage
                exit 0
                ;;
            *) die "unknown argument: $1 (try --help)" ;;
        esac
    done

    require_tool git
    # Discards the git dir path (the payload); git's own stderr still surfaces
    # if this is not a repository, alongside the message below.
    git rev-parse --git-dir >/dev/null || die "not inside a git repository"
    cd "$(git rev-parse --show-toplevel)"

    local active
    active="$(resolve_active_branch)"
    log "Active branch: $active"

    log "Fetching origin..."
    git fetch --quiet origin || die "git fetch failed"

    local -a targets=()
    mapfile -t targets < <(retired_branches "$active")

    if [[ ${#targets[@]} -eq 0 ]]; then
        log "No retired F<VERSION> branches found — nothing to stamp."
        return 0
    fi

    log "Retired branches: ${targets[*]}"
    local branch
    for branch in "${targets[@]}"; do
        process_branch "$branch" "$active"
    done

    log "Done."
}

main "$@"
