#!/usr/bin/env bash
#
# Plan 00130 — establish which live plan scripts still write their run log with a
# process substitution, and which plan-local logs/ directories still exist.
#
# READ-ONLY. Reads tracked repo text and nothing else.
#
# Neither plan_require_host nor plan_require_container is called, and R2 asks that a
# script accepting either location be re-examined rather than left unguarded. Re-examined:
# every fact this script establishes comes from the repo checkout's own text, which is
# bind-mounted identically into the container and is byte-for-byte the same file on the
# host. There is no host state, no deployed tree, no container engine in any answer it
# gives, so neither guard has anything to protect. That is the rare case R2 names.
#
# Usage: triage.bash [--help]

set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat << 'EOF'
Plan 00130 — triage the legacy run-log pattern in live plan scripts

Usage: triage.bash [--help]

Establishes, without changing anything:
  * every script under CLAUDE/Plan/NNNNN-*/ that redirects its output through a
    process substitution, with the line number
  * for each, whether it already sources _planlib.inc.bash — the conversion is a
    one-line swap where it does and a bootstrap where it does not
  * for each, whether it prompts for its own consent, which is what stops the
    batch runner answering for it
  * every plan-local logs/ directory in the live tree, and whether it is empty

Counts what it EXAMINED as well as what it found, because "no occurrences" and
"the glob matched nothing" are different facts that otherwise print identically.

Renders no verdict and converts nothing. Completed/ and Cancelled/ are reported
separately as out-of-scope context — those scripts will not run again.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: triage.bash --help" >&2
            exit 1
            ;;
    esac
done

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"
plan_mode gather
plan_start_log auto

PLAN_TREE="$PLAN_REPO_ROOT/CLAUDE/Plan"

# The first character is bracketed so this script does not match its own text. Every live
# script is scanned, including this one, and a pattern written out plainly here would be
# found in the very file doing the finding — one phantom hit in every run, and a later
# conversion sweep chasing an occurrence that does not exist. Same reason a process probe
# brackets its pattern; the target here is a file rather than an argv.
LEGACY_REDIRECT='exec > >\([t]ee'

# ── who is in scope ───────────────────────────────────────────────────────────────────────

# Live plans only: NNNNN-* directly under CLAUDE/Plan/. Completed/ and Cancelled/ are
# subdirectories, so -maxdepth keeps them out by structure rather than by a name filter that
# a future subdirectory would slip past.
live_scripts() {
    find "$PLAN_TREE" -mindepth 2 -maxdepth 2 -name '*.bash' -type f -print | sort
}

# Each archive directory is tested before it is walked. `find` on a missing path writes to
# stderr and exits non-zero, and the tempting fix is to discard both — which would also
# discard a permissions error, an I/O error, and any other reason the walk came back short.
# An absent Cancelled/ tree is a fact worth stating once, not a stream to silence.
archived_scripts() {
    local dir
    for dir in "$PLAN_TREE/Completed" "$PLAN_TREE/Cancelled"; do
        if [ ! -d "$dir" ]; then
            printf 'archived tree absent, nothing to contrast: %s\n' "${dir#"$PLAN_REPO_ROOT"/}" >&2
            continue
        fi
        find "$dir" -mindepth 2 -maxdepth 2 -name '*.bash' -type f -print
    done | sort
}

# Non-comment occurrences only. The pattern appears in prose in at least one already-converted
# script, explaining what was removed and why — counting that as an occurrence would report a
# script as unconverted precisely because it documented its conversion.
#
# grep exits 1 on zero matches, which is a legitimate empty result rather than an error, and
# 2 or more for a real failure. Collapsing the two would report an unreadable file as a clean
# one, which is the defect class this whole plan tree keeps tripping over. The comment filter
# is awk rather than a second grep for the same reason: `grep -v` exits 1 when it rejects
# every line, so an all-comments file and a broken filter would look identical.
legacy_hits() {
    local script="$1" out rc
    if out="$(grep -nE "$LEGACY_REDIRECT" "$script")"; then
        rc=0
    else
        rc=$?
    fi
    case "$rc" in
        0) ;;
        1) return 0 ;;
        *)
            printf '[FATAL] grep failed (rc=%d) on %s — this is not "no matches"\n' \
                "$rc" "$script" >&2
            return 1
            ;;
    esac
    printf '%s\n' "$out" | awk '!/^[0-9]+:[ \t]*#/'
}

echo "=============================================================="
echo "Plan 00130 triage — the legacy run-log pattern in live plans"
echo "=============================================================="
echo

# ── 1. what was examined ──────────────────────────────────────────────────────────────────
#
# Stated before any finding. A report that says "0 scripts still carry the pattern" is the
# same text whether the sweep read every script or the glob matched nothing at all, and this
# plan exists because that ambiguity destroyed evidence once already.
EXAMINED=0
while IFS= read -r _script; do
    EXAMINED=$((EXAMINED + 1))
done < <(live_scripts)

printf 'EXAMINED: %d script(s) under %s/NNNNN-*/\n\n' "$EXAMINED" "${PLAN_TREE#"$PLAN_REPO_ROOT"/}"

# A sweep that examined nothing is a failed gather, not a clean tree. Recorded as a leg so it
# drives a non-zero exit and is named in plan_finish's summary. The leg is `test`, an external
# command: a shell function passed here is an indirection shellcheck cannot follow.
plan_gather_leg "the live-plan glob matched at least one script" test "$EXAMINED" -gt 0

# ── 2. the occurrences, with conversion shape ─────────────────────────────────────────────

report_occurrences() {
    local script rel hits planlib consent n
    local total_scripts=0 total_hits=0

    printf '%-58s %-5s %-9s %-8s\n' SCRIPT HITS PLANLIB CONSENT
    while IFS= read -r script; do
        hits="$(legacy_hits "$script")"
        if [ -z "$hits" ]; then
            continue
        fi
        rel="${script#"$PLAN_REPO_ROOT"/}"
        # grep -c counts LINES; this counts OCCURRENCES, which is the figure a conversion
        # has to remove one at a time.
        n="$(printf '%s\n' "$hits" | grep -oE "$LEGACY_REDIRECT" | wc -l)"

        # Task 1.2: a script that already sources the library needs a one-line swap. One that
        # does not needs the R1 bootstrap first, which is a different piece of work.
        if grep -q '_planlib.inc.bash' "$script"; then
            planlib="sources"
        else
            planlib="NO"
        fi

        # Task 2.4: a script that prompts for itself is what stops meta-deploy.bash being one
        # consent — the batch's single answer does not reach a prompt the script asks itself.
        if grep -qE '^[[:space:]]*read[[:space:]]+-' "$script"; then
            consent="OWN"
        else
            consent="-"
        fi

        printf '%-58s %-5s %-9s %-8s\n' "$rel" "$n" "$planlib" "$consent"
        printf '%s\n' "$hits" | while IFS= read -r line; do
            printf '    %s\n' "$line"
        done

        total_scripts=$((total_scripts + 1))
        total_hits=$((total_hits + n))
    done < <(live_scripts)

    printf '\nTOTAL: %d occurrence(s) across %d live script(s), of %d examined\n' \
        "$total_hits" "$total_scripts" "$EXAMINED"
}
# Called DIRECTLY, with the result then recorded as a leg whose command is `test`.
#
# Handing a shell function to plan_gather_leg is an indirection shellcheck cannot follow, and
# in a script ending in plan_finish — which terminates, so control never falls off the end —
# every such body is reported unreachable. Measured on this file: 60 SC2317s for three
# functions, and suppressions are banned (R11). A direct call site is visible to the linter
# and loses nothing, because `test "$rc" -eq 0` records the same failure by the same name.
if report_occurrences; then rc_occurrences=0; else rc_occurrences=$?; fi
plan_gather_leg "occurrence enumeration" test "$rc_occurrences" -eq 0
echo

# ── 3. the archived tree, for contrast only ───────────────────────────────────────────────

report_archived() {
    local script n=0
    while IFS= read -r script; do
        if [ -n "$(legacy_hits "$script")" ]; then
            n=$((n + 1))
        fi
    done < <(archived_scripts)
    printf 'Completed/ and Cancelled/: %d script(s) carry the pattern — DELIBERATELY OUT OF\n' "$n"
    printf 'SCOPE. They will not run again, and editing an archived plan makes its recorded\n'
    printf 'history disagree with what it actually ran.\n'
}
if report_archived; then rc_archived=0; else rc_archived=$?; fi
plan_gather_leg "archived-tree contrast" test "$rc_archived" -eq 0
echo

# ── 4. the plan-local logs/ directories ───────────────────────────────────────────────────

report_log_dirs() {
    local dir rel scope contents n=0 live=0
    printf '%-58s %-10s %s\n' DIRECTORY SCOPE STATE
    while IFS= read -r dir; do
        rel="${dir#"$PLAN_REPO_ROOT"/}"
        n=$((n + 1))
        scope="archived"
        case "$rel" in
            CLAUDE/Plan/Completed/* | CLAUDE/Plan/Cancelled/*) ;;
            *)
                scope="LIVE"
                live=$((live + 1))
                ;;
        esac
        # An empty one is the orphan shape: gitignored, so `git mv` into Completed/ left it
        # behind at the old path with nothing to indicate it was ever there.
        contents="$(find "$dir" -type f | wc -l)"
        if [ "$contents" -eq 0 ]; then
            printf '%-58s %-10s %s\n' "$rel" "$scope" "EMPTY"
        else
            printf '%-58s %-10s %s file(s)\n' "$rel" "$scope" "$contents"
        fi
    done < <(find "$PLAN_TREE" -type d -name logs | sort)
    printf '\nTOTAL: %d logs/ director(ies), %d of them in the live tree\n' "$n" "$live"
}
if report_log_dirs; then rc_log_dirs=0; else rc_log_dirs=$?; fi
plan_gather_leg "plan-local logs/ enumeration" test "$rc_log_dirs" -eq 0
echo

echo "=============================================================="
echo "READ THIS FIRST:"
echo "  1. EXAMINED at the top. If it is 0 the sweep found nothing because it"
echo "     looked nowhere, and every count below is vacuous."
echo "  2. PLANLIB=sources means the conversion is a one-line swap. PLANLIB=NO"
echo "     means the R1 bootstrap has to go in first."
echo "  3. CONSENT=OWN names a script that prompts for itself. Those are what"
echo "     stop meta-deploy.bash being a single consent for the whole batch."
echo "  4. A LIVE logs/ directory is in scope; an archived one is not, and"
echo "     removing it would rewrite a finished plan's record."
echo "=============================================================="

plan_finish
