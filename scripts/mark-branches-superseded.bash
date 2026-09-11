#!/usr/bin/env bash
#
# mark-branches-superseded.bash — stamp a "NO LONGER ACTIVE BRANCH" banner onto
# the README of every retired F<VERSION> branch. Run --help for the options.
#
# This repo uses one branch per Fedora release (F42, F43, F44…), and the newest
# one is the GitHub default. Anyone landing on an old branch — from a search
# result, a stale bookmark, or an old link — sees a README that looks current
# and gives no hint it is abandoned. This script puts an unmissable banner at
# the top of those READMEs pointing at the active branch.
#
# The banner NAMES the current branch, which buys a much louder warning than a
# bare "see the default branch" would, at the cost of going stale: when F45
# lands, the banners on F42/F43/F44 all still say F44. So this re-stamps EVERY
# retired branch on every run, not just the newly retired one, and --check
# exists so a missed run is detectable rather than silent. Wire --check into CI
# if you want the invariant enforced rather than merely documented.
#
# Re-stamping is idempotent: a branch whose banner is already correct produces
# no commit. Run it as part of retiring a branch — see docs/development.md,
# "Creating New Version Branch".
#
# Each branch is edited in its own throwaway git worktree, so the checkout you
# run this from is never switched and an interrupted run cannot strand you on
# someone else's branch. Pushing to the PR-protected F* branches requires
# push-bypass on the repository; without it the push is rejected and this exits
# non-zero.
#
# Exit status: 0 = all retired branches correctly stamped (or --dry-run
# previewed); 1 = --check found at least one missing or stale banner; 2 = a hard
# error (missing tool, unresolvable active branch, a worktree/commit/push that
# failed, or a README this refuses to rewrite).
#
# Requires: git, and gh (authenticated) unless --current is given.

set -euo pipefail

readonly BANNER_START='<!-- SUPERSEDED-BANNER:START -->'
readonly BANNER_END='<!-- SUPERSEDED-BANNER:END -->'
readonly REPO_URL='https://github.com/LongTermSupport/fedora-desktop'

# stamp | dry-run | check
MODE=stamp
ACTIVE_BRANCH=''
WORKTREE_DIR=''
WORKTREE_REGISTERED=false
STALE_COUNT=0

usage() {
    cat <<'EOF'
mark-branches-superseded.bash — stamp a "no longer active" banner onto retired
F<VERSION> branch READMEs.

Usage:
  scripts/mark-branches-superseded.bash [options]

Options:
  --current BRANCH   Treat BRANCH as the active branch instead of asking GitHub
                     for the repository default. Useful before the default has
                     been switched over. Must exist on origin.
  --check            Report branches whose banner is missing or stale and exit 1
                     if there are any. Writes nothing. Suitable as a CI gate.
  --dry-run          Print the banner each branch would get and change nothing.
                     Always exits 0 if the preview itself succeeded.
  -h, --help         Show this help.

Examples:
  scripts/mark-branches-superseded.bash                 # stamp and push
  scripts/mark-branches-superseded.bash --check         # CI gate, no writes
  scripts/mark-branches-superseded.bash --dry-run       # preview the banner
  scripts/mark-branches-superseded.bash --current F45   # before the default moves

Stdout is the machine-readable result, one "<branch> <status>" line per retired
branch, where status is one of: stamped, current, would-stamp.

Branches are read from, and written straight back to, origin — the published
state is what needs the banner. Your local refs/heads/* are never touched; only
refs/remotes/origin/* moves, via this script's own fetch and pushes.
EOF
}

log() { printf '%s\n' "$*" >&2; }

die() {
    printf 'ERROR: %s\n' "$*" >&2
    exit 2
}

# Removes the temp dir for the branch in flight. Two callers with different
# contracts, so they are separate functions: this one runs from the EXIT trap
# (including the failure paths), where aborting would mask the error already on
# its way out, so it warns instead.
cleanup_on_exit() {
    [[ -n "$WORKTREE_DIR" ]] || return 0
    local dir="$WORKTREE_DIR" registered="$WORKTREE_REGISTERED"
    WORKTREE_DIR=''
    WORKTREE_REGISTERED=false
    if [[ "$registered" == true ]]; then
        if ! git worktree remove --force "$dir"; then
            log "WARNING: could not remove worktree $dir"
            log "         remove it by hand: git worktree remove --force $dir"
        fi
        return 0
    fi
    # Never registered as a worktree, so `git worktree remove` would fail with
    # "not a working tree" and the advice above would be wrong. It is our own
    # mktemp -d, so remove it directly.
    if ! rm -rf "$dir"; then
        log "WARNING: could not remove temp dir $dir"
    fi
}
trap cleanup_on_exit EXIT

# The in-loop caller, on the success path. A removal failure here is a plain
# failure and must stop the run — warning and carrying on would leak a worktree
# registration and still exit 0.
discard_worktree() {
    [[ -n "$WORKTREE_DIR" ]] || return 0
    local dir="$WORKTREE_DIR" registered="$WORKTREE_REGISTERED"
    WORKTREE_DIR=''
    WORKTREE_REGISTERED=false
    if [[ "$registered" == true ]]; then
        git worktree remove --force "$dir" || die "could not remove worktree $dir"
        return 0
    fi
    rm -rf "$dir" || die "could not remove temp dir $dir"
}

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

count_marker() {
    local readme="$1" marker="$2"
    awk -v m="$marker" '$0 == m { n++ } END { print n + 0 }' "$readme"
}

# Rewrite README.md so it starts with the banner, dropping any previous banner
# block. Marker-delimited so re-running replaces rather than accumulates.
stamp_readme() {
    local readme="$1" banner="$2" body starts ends
    starts="$(count_marker "$readme" "$BANNER_START")"
    ends="$(count_marker "$readme" "$BANNER_END")"

    # An unterminated START would make the strip below swallow the whole file,
    # and more than one pair means the markers appear somewhere that is not the
    # banner. Either way this cannot safely guess, so refuse rather than publish
    # a mangled README.
    if [[ "$starts" != "$ends" ]]; then
        die "$readme has $starts start marker(s) and $ends end marker(s) — unbalanced banner, refusing to rewrite"
    fi
    if [[ "$starts" -gt 1 ]]; then
        die "$readme contains $starts banner marker pairs — refusing to rewrite, expected at most 1"
    fi

    body="$(awk -v s="$BANNER_START" -v e="$BANNER_END" '
        $0 == s { skipping = 1; next }
        $0 == e { skipping = 0; next }
        !skipping { print }
    ' "$readme")"

    # Verify the rewrite took: a README that is nothing but a banner is not a
    # thing we ever want to commit.
    [[ -n "$body" ]] || die "stripping the banner from $readme left no content — refusing to publish an empty README"

    # Drop leading blank lines left behind by a removed banner so the spacing
    # below the new one does not grow on every re-stamp.
    body="${body#"${body%%[![:space:]]*}"}"
    printf '%s\n\n%s\n' "$banner" "$body" >"$readme"
}

process_branch() {
    local branch="$1" active="$2" banner readme
    banner="$(banner_for "$branch" "$active")"

    WORKTREE_DIR="$(mktemp -d -t superseded-XXXXXX)"
    if ! git worktree add --quiet --detach "$WORKTREE_DIR" "origin/$branch"; then
        discard_worktree
        die "could not create a worktree for $branch"
    fi
    WORKTREE_REGISTERED=true

    readme="$WORKTREE_DIR/README.md"
    [[ -f "$readme" ]] || die "$branch has no README.md at its root"

    stamp_readme "$readme" "$banner"

    if git -C "$WORKTREE_DIR" diff --quiet -- README.md; then
        log "  $branch: banner already correct"
        printf '%s current\n' "$branch"
        discard_worktree
        return
    fi

    STALE_COUNT=$((STALE_COUNT + 1))

    if [[ "$MODE" == check ]]; then
        log "  $branch: banner MISSING or STALE (should name $active)"
        printf '%s would-stamp\n' "$branch"
        discard_worktree
        return
    fi

    if [[ "$MODE" == dry-run ]]; then
        log "  $branch: would stamp this banner (naming $active):"
        log "$banner"
        printf '%s would-stamp\n' "$branch"
        discard_worktree
        return
    fi

    git -C "$WORKTREE_DIR" add README.md
    # [skip ci] because a retired branch's workflows are not maintained and are
    # not expected to still pass; without it this turns an untouched branch red
    # on a failure that has nothing to do with the banner.
    git -C "$WORKTREE_DIR" -c "advice.detachedHead=false" commit --quiet \
        -m "README: mark $branch superseded, current branch is $active [skip ci]" \
        || die "commit failed on $branch"

    # Committed detached, so push the new commit explicitly at the branch ref.
    # Never forced: a non-fast-forward means origin moved and must be re-read.
    git -C "$WORKTREE_DIR" push --quiet origin "HEAD:refs/heads/$branch" \
        || die "push failed for $branch"
    log "  $branch: stamped and pushed (naming $active)"
    printf '%s stamped\n' "$branch"
    discard_worktree
}

main() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --current)
                [[ $# -ge 2 ]] || die "--current needs a branch name"
                ACTIVE_BRANCH="$2"
                shift 2
                ;;
            --check)
                MODE=check
                shift
                ;;
            --dry-run)
                MODE=dry-run
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

    log "Fetching origin..."
    # --prune so a branch deleted on GitHub does not survive as a stale remote
    # ref and get re-created by the push below.
    git fetch --prune --quiet origin || die "git fetch failed"

    local active
    active="$(resolve_active_branch)"
    # A typo here would mark the real default branch retired and push a banner
    # telling readers to check out a branch that does not exist.
    git rev-parse --verify --quiet "refs/remotes/origin/$active" >/dev/null \
        || die "origin has no branch '$active' — check the --current value"
    log "Active branch: $active"

    # Captured rather than piped into mapfile: process substitution discards the
    # pipeline status, so a failing selector would look like "no retired
    # branches" and this would exit 0 having done nothing.
    local raw
    raw="$(git for-each-ref --format='%(refname:strip=3)' 'refs/remotes/origin/F[0-9]*' | sort -V)" \
        || die "could not list the origin F<VERSION> branches"

    local -a candidates=() targets=()
    if [[ -n "$raw" ]]; then
        mapfile -t candidates <<<"$raw"
    fi

    local branch
    for branch in "${candidates[@]}"; do
        [[ "$branch" =~ ^F[0-9]+$ ]] || continue
        [[ "$branch" != "$active" ]] || continue
        targets+=("$branch")
    done

    log "Found ${#candidates[@]} F<VERSION> branch(es) on origin; ${#targets[@]} retired."

    if [[ ${#targets[@]} -eq 0 ]]; then
        log "Nothing to stamp."
        return 0
    fi

    log "Retired branches: ${targets[*]}"
    for branch in "${targets[@]}"; do
        process_branch "$branch" "$active"
    done

    if [[ "$MODE" == check ]]; then
        if [[ "$STALE_COUNT" -gt 0 ]]; then
            log "FAIL: $STALE_COUNT retired branch(es) need stamping — run this script without --check."
            return 1
        fi
        log "OK: every retired branch carries a current banner."
        return 0
    fi

    log "Done."
}

main "$@"
