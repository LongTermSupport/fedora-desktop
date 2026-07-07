#!/usr/bin/env bash
#
# mkplan.bash — scaffold the next numbered plan folder, in this script's own
# directory. Drop it into any hooks-daemon / plan-workflow project's plan
# folder (conventionally CLAUDE/Plan/) and run it from anywhere.
#
# Usage:
#   <plan-dir>/mkplan.bash "descriptive-kebab-name"
#
# Deployment contract (READ THIS):
#   The script scaffolds plans in ITS OWN directory, resolved from BASH_SOURCE
#   (intermediate symlinks are followed) — never the current working directory.
#   So the REAL script file must live in the plan dir. A convenience symlink
#   pointing AT it from elsewhere (e.g. bin/mkplan) works correctly: the plan
#   still lands in the plan dir.
#
#   The one layout that does NOT auto-work is the reverse — a symlink placed
#   INSIDE the plan dir that points at a real file OUTSIDE it (e.g. a shared
#   tools/mkplan.bash). BASH_SOURCE resolves to the real file, so plans would
#   land next to the real file. For that packaging, set MKPLAN_PLAN_DIR to the
#   plan dir explicitly and it overrides all self-location.
#
#   Wherever the plan dir resolves IS the project's configured plan dir — the
#   SSoT stays with the daemon's .claude/hooks-daemon config, which is why the
#   script is placed there.
#
# What it does (fail-fast at every step):
#   1. Requires exactly one non-empty argument: the plan name.
#   2. Normalises + validates the name into a safe kebab slug that starts
#      with a letter (matches the daemon's NNNNN-[a-zA-Z] plan pattern).
#   3. Takes an exclusive, portable lock on the plan dir so concurrent runs
#      (two agents, or an agent + a human) cannot assign the same number.
#   4. Resolves the next plan number from the git-anchored counter
#      (`hooksdaemon.latestPlanNumber`), bootstrapping from a filesystem
#      scan when the counter is unset — exactly like the hooks daemon does.
#      A brand-new project (no counter, no plans) starts at 00001.
#   5. Sanity-checks the number against the folders on disk and refuses to
#      proceed on drift or collision.
#   6. Creates <plan-dir>/NNNNN-name/ and scaffolds PLAN.md.
#   7. Advances the git counter so the next plan reads counter + 1.
#
# The script is the SOLE writer of the counter when it creates a plan: the
# folder + PLAN.md are written with bash (mkdir / cat), NOT the Claude Code
# Write tool, so the daemon's plan-numbering handler never sees the write and
# never double-increments. The lock (step 3) closes the multi-process race.

set -euo pipefail

readonly COUNTER_KEY="hooksdaemon.latestPlanNumber"
readonly NUMBER_WIDTH=5
readonly MAX_NAME_LENGTH=80
readonly LOCK_BASENAME=".mkplan.lock"
readonly LOCK_MAX_ATTEMPTS=100
readonly LOCK_RETRY_SECONDS=0.1

# Populated once the lock is held, so the EXIT trap only ever removes a lock
# this process actually owns (never another runner's lock on a timeout-die).
lock_held=""

# --- helpers ---------------------------------------------------------------

die() {
    printf 'mkplan: error: %s\n' "$1" >&2
    exit 1
}

usage() {
    cat >&2 <<'USAGE'
Usage: mkplan.bash "descriptive-kebab-name"

Creates the next sequentially-numbered plan folder (in this script's own
directory, or $MKPLAN_PLAN_DIR if set) and scaffolds its PLAN.md.

Arguments:
  name   Required. A single non-empty string describing the plan. It is
         normalised to a kebab-case slug and MUST start with a letter.
         Do NOT include a number prefix — the script assigns it.

Environment:
  MKPLAN_PLAN_DIR   Optional. Absolute path to the plan dir. Overrides
                    self-location (use when the script is symlinked into the
                    plan dir from a shared location elsewhere).

Examples:
  mkplan.bash "wsdl-patch-pipeline-hardening"
  mkplan.bash "Order despatch retries"   # -> 000NN-Order-despatch-retries
USAGE
}

# Release the plan-dir lock. Only removes the lock if THIS process took it
# (lock_held set) and the dir is still present — never another runner's lock.
release_lock() {
    if [[ -n "$lock_held" && -d "$lock_held" ]]; then
        rmdir "$lock_held"
    fi
}

# Acquire an exclusive lock on the plan dir via atomic `mkdir`. Portable
# (Linux + macOS/BSD, no `flock` dependency) and silent: mkdir's stderr is
# captured (2>&1 into a var, NOT discarded) and only surfaced if we time out.
acquire_lock() {
    local lock_dir="$1"
    local attempts=0 err
    while ! err="$(mkdir "$lock_dir" 2>&1)"; do
        attempts=$((attempts + 1))
        if (( attempts >= LOCK_MAX_ATTEMPTS )); then
            die "timed out acquiring plan lock '$lock_dir' (concurrent run, or a stale lock — remove the directory if no other mkplan is running). Last error: $err"
        fi
        sleep "$LOCK_RETRY_SECONDS"
    done
    lock_held="$lock_dir"
    # Release on any exit path once we hold it.
    trap release_lock EXIT INT TERM
}

# Highest existing plan number on disk. Mirrors the daemon's scan:
#   - direct numbered children of CLAUDE/Plan/      (NNNNN-name/)
#   - one level inside non-numbered subdirs         (Completed/NNNNN-name/, archive/...)
# Pattern requires a letter after the hyphen so date dirs (2026-06-19) are ignored.
# Dotfiles (e.g. the .mkplan.lock dir) are not matched by the unquoted-* globs.
filesystem_highest() {
    local plan_dir="$1"
    local highest=0 dir base num
    shopt -s nullglob
    for dir in "$plan_dir"/*/ "$plan_dir"/*/*/; do
        base="$(basename "$dir")"
        if [[ "$base" =~ ^([0-9]{1,5})-[a-zA-Z] ]]; then
            num=$((10#${BASH_REMATCH[1]}))
            if (( num > highest )); then
                highest=$num
            fi
        fi
    done
    shopt -u nullglob
    printf '%d' "$highest"
}

# --- argument handling -----------------------------------------------------

if [[ $# -eq 1 ]] && { [[ "$1" == "-h" ]] || [[ "$1" == "--help" ]]; }; then
    usage
    exit 0
fi

if [[ $# -ne 1 ]]; then
    usage
    die "expected exactly one argument (the plan name), got $#"
fi

raw_name="$1"

# Trim surrounding whitespace.
raw_name="${raw_name#"${raw_name%%[![:space:]]*}"}"
raw_name="${raw_name%"${raw_name##*[![:space:]]}"}"

if [[ -z "$raw_name" ]]; then
    die "plan name must be a non-empty string"
fi

# Normalise: whitespace -> hyphen, collapse repeats, trim stray hyphens.
name="${raw_name//[[:space:]]/-}"
while [[ "$name" == *--* ]]; do
    name="${name//--/-}"
done
name="${name#-}"
name="${name%-}"

if [[ -z "$name" ]]; then
    die "plan name normalised to empty — provide letters/digits"
fi

if (( ${#name} > MAX_NAME_LENGTH )); then
    die "plan name too long (${#name} chars, max ${MAX_NAME_LENGTH})"
fi

# Must start with a letter and contain only [A-Za-z0-9-]. This both keeps the
# folder filesystem-safe and satisfies the daemon's NNNNN-[a-zA-Z] convention.
if [[ ! "$name" =~ ^[A-Za-z][A-Za-z0-9-]*$ ]]; then
    die "invalid plan name '$name' — must start with a letter and contain only letters, digits and hyphens (no number prefix, no path separators)"
fi

# --- locate the plan dir (override, else this script's own dir) + the repo --

if [[ -n "${MKPLAN_PLAN_DIR:-}" ]]; then
    # Explicit override for shared/symlinked deployments.
    [[ -d "$MKPLAN_PLAN_DIR" ]] || die "MKPLAN_PLAN_DIR is set but not a directory: $MKPLAN_PLAN_DIR"
    plan_dir="$(cd -P "$MKPLAN_PLAN_DIR" && pwd)"
else
    # Resolve the real directory containing this script, following symlinks, so
    # the plan dir is wherever the script physically lives — independent of CWD.
    source_path="${BASH_SOURCE[0]}"
    while [[ -L "$source_path" ]]; do
        link_dir="$(cd -P "$(dirname "$source_path")" && pwd)"
        source_path="$(readlink "$source_path")"
        [[ "$source_path" == /* ]] || source_path="$link_dir/$source_path"
    done
    plan_dir="$(cd -P "$(dirname "$source_path")" && pwd)"
fi

# The counter lives in the enclosing repo's git config — resolve it from the
# plan dir (not CWD) so the script works from anywhere, including nested repos.
if ! repo_root="$(git -C "$plan_dir" rev-parse --show-toplevel)"; then
    die "$plan_dir is not inside a git repository — cannot resolve the plan counter"
fi

plan_rel="${plan_dir#"$repo_root"/}"

# --- take the lock, then resolve + sanity-check the next number ------------

# Everything from here to the counter write is the critical section: a single
# atomic-mkdir lock serialises concurrent runners so no two assign the same N.
acquire_lock "$plan_dir/$LOCK_BASENAME"

fs_highest="$(filesystem_highest "$plan_dir")"

if counter_raw="$(git -C "$repo_root" config --local --get "$COUNTER_KEY")"; then
    if [[ ! "$counter_raw" =~ ^[0-9]+$ ]]; then
        printf 'mkplan: counter %s is non-numeric (%q); bootstrapping from filesystem (%s)\n' \
            "$COUNTER_KEY" "$counter_raw" "$fs_highest" >&2
        counter="$fs_highest"
    else
        counter=$((10#$counter_raw))
    fi
else
    printf 'mkplan: counter %s unset; bootstrapping from filesystem high-water mark (%s)\n' \
        "$COUNTER_KEY" "$fs_highest" >&2
    counter="$fs_highest"
fi

# Drift guard: the git counter is authoritative, but if the filesystem already
# holds a HIGHER number the counter is stale and counter+1 would collide or
# mis-order. Refuse and tell the user how to reconcile.
if (( fs_highest > counter )); then
    die "counter ($counter) is behind the highest plan on disk ($fs_highest). The counter is stale; reconcile with: git -C '$repo_root' config --local $COUNTER_KEY $fs_highest"
fi

next=$((counter + 1))
printf -v padded '%0*d' "$NUMBER_WIDTH" "$next"

# Collision guard: nothing on disk may already carry this number (active or
# archived). filesystem_highest already covers the common case, but check the
# concrete number explicitly for a precise error.
shopt -s nullglob
existing=( "$plan_dir/$padded-"*/ "$plan_dir"/*/"$padded-"*/ )
shopt -u nullglob
if (( ${#existing[@]} > 0 )); then
    die "plan number $padded already exists on disk: ${existing[0]}"
fi

target="$plan_dir/$padded-$name"
if [[ -e "$target" ]]; then
    die "target folder already exists: $target"
fi

# --- create the folder + scaffold PLAN.md ----------------------------------

if ! mkdir "$target"; then
    die "could not create plan folder '$target' (permissions, or a concurrent run won the race)"
fi

title="${name//-/ }"
created="$(date +%F)"
plan_file="$target/PLAN.md"

# Owner from git identity, with a portable fallback.
if ! owner="$(git -C "$repo_root" config user.name)" || [[ -z "$owner" ]]; then
    owner="Unknown"
fi

# Project-managed template (Plan 00144): when the plan dir carries a tracked
# _TEMPLATE_.md, render it with pure-bash placeholder substitution — projects
# own their template while still getting the daemon default as a starting
# point (the deploy seeds it when missing and never overwrites it). Without
# one, fall back to the built-in skeleton below so the script stays drop-in
# self-contained.
readonly TEMPLATE_BASENAME="_TEMPLATE_.md"
template_file="$plan_dir/$TEMPLATE_BASENAME"
if [[ -f "$template_file" ]]; then
    if ! body="$(cat "$template_file")"; then
        die "could not read plan template '$template_file'"
    fi
    body="${body//\{\{PLAN_NUMBER\}\}/$padded}"
    body="${body//\{\{PLAN_TITLE\}\}/$title}"
    body="${body//\{\{CREATED_DATE\}\}/$created}"
    body="${body//\{\{OWNER\}\}/$owner}"
    printf '%s\n' "$body" > "$plan_file"
else
cat > "$plan_file" <<PLAN
# Plan $padded: $title

**Status**: Not Started
**Created**: $created
**Owner**: $owner
**Priority**: Medium

## Overview

<!-- 2-3 paragraphs: what this plan achieves and why. -->

## Goals

- <!-- clear, measurable goal -->

## Non-Goals

- <!-- what this plan will NOT do -->

## Tasks

### Phase 1: <!-- phase name -->

- [ ] ⬜ **Task 1.1**: <!-- description -->

## Success Criteria

- [ ] <!-- criterion that must be met -->

## Notes & Updates

### $created

- Plan scaffolded.
PLAN
fi

# --- advance the counter (only after a successful write) -------------------

# High-water-mark write (mirrors the daemon's max() semantics, Plan 00112): the
# lock guarantees `next` is monotonic, and max() is belt-and-braces against ever
# lowering the counter.
new_counter="$next"
if (( counter > new_counter )); then
    new_counter="$counter"
fi
git -C "$repo_root" config --local "$COUNTER_KEY" "$new_counter"

# Lock is released by the EXIT trap (release_lock).

# --- report ----------------------------------------------------------------

rel_target="${target#"$repo_root"/}"
cat >&2 <<DONE
mkplan: created plan $padded
  folder: $rel_target/
  plan:   $rel_target/PLAN.md
  counter $COUNTER_KEY -> $new_counter

Next steps (not done automatically):
  - Fill in PLAN.md (overview, goals, tasks).
  - Add a row to $plan_rel/README.md under "Active Plans" (if the project keeps one).
DONE

printf '%s\n' "$target"
