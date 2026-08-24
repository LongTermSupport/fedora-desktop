#!/usr/bin/env bash
#
# Plan 00082 triage — measure the backlog the three newly-enabled hooks-daemon
# handlers will police, plus the plan-index rows that are already over length.
#
# READ-ONLY. Gathers facts, renders no verdict (that is acceptance.bash's job).
# Safe to re-run at any time, on a live system, as many times as needed.
#
# Usage:  CLAUDE/Plan/00082-.../triage.bash [--help]

set -euo pipefail

# --- argument parsing FIRST: --help must work before any environment resolution
usage() {
    cat >&2 <<'USAGE'
Plan 00082 triage — backlog measurement (read-only)

  triage.bash            gather every probe and write the report
  triage.bash --help     this text

Probes:
  P1  plan-index rows over the 500-char index-row-length limit
  P2  comment_changelog BLOCKING signals across repo-owned source
  P3  comment_size backlog — over-long comment lines and comment blocks
  P4  sensitive_content readiness — is either source populated?

The report is written to this plan's own logs/ directory (gitignored) so an
agent can read it at the same repo-relative path with no copy-paste.
USAGE
}

case "${1:-}" in
    -h | --help)
        usage
        exit 0
        ;;
    "") ;;
    *)
        echo "ERROR: unknown option: $1 (see --help)" >&2
        exit 1
        ;;
esac

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
REPORTS_DIR="$PLAN_DIR/logs"
mkdir -p "$REPORTS_DIR"
LOG="$REPORTS_DIR/backlog-triage.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

# A non-zero exit status is DATA, not a failure. Capture it and carry on.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

echo "=========================================================="
echo " Plan 00082 — backlog triage"
echo " repo: $REPO_ROOT"
echo "=========================================================="
echo

# --- P1: plan-index rows over the limit ----------------------------------
# The index-row-length check fires at 500 characters. State the count as a
# NUMBER, never imply it from a list — a set you never enumerate cannot look
# short.
index_row_lengths() {
    local readme="$REPO_ROOT/CLAUDE/Plan/README.md"
    if [ ! -f "$readme" ]; then
        echo "ERROR: $readme not found — cannot measure the index." >&2
        return 1
    fi
    local total over
    total="$(awk 'END {print NR}' "$readme")"
    over="$(awk 'length($0) > 500 {n++} END {print n + 0}' "$readme")"
    echo "COVERAGE: $total line(s) in the index; $over exceed 500 characters"
    echo
    echo "line | chars | plan"
    awk 'length($0) > 500 {
             plan = $0
             sub(/^[^[]*\[/, "", plan)
             sub(/\].*$/, "", plan)
             printf "%4d | %5d | %s\n", NR, length($0), plan
         }' "$readme"
    echo
    echo "longest row: $(awk '{ if (length($0) > m) m = length($0) } END {print m}' "$readme") characters"
}
echo "### P1  plan-index rows over the 500-char limit"
echo "###   READ THIS FOR: how much of CLAUDE/Plan/README.md needs rewriting."
echo "###   Every row over 500 chars is one the reader must scroll to finish."
index_row_lengths
echo

# --- P2: comment_changelog blocking signals ------------------------------
# Two signals BLOCK: "Prior <version>:" / "Previously <version>:" phrasing, and
# a date-prefixed entry. Everything else in that handler is advisory. Grep is a
# proxy for the handler (it does not restrict itself to comment spans), so these
# counts are an UPPER bound on the blocking backlog, not the backlog itself.
scan_signal() {
    local label="$1" pattern="$2" hits
    echo "-- $label"
    # grep exits 1 on no-match, which is the answer this probe wants. Capture the
    # status in the condition rather than letting a fallback swallow it.
    if hits="$(grep -rInE --exclude-dir=.git --exclude-dir=untracked --exclude-dir=node_modules \
        --exclude-dir=.claude --exclude-dir=roles \
        --include='*.bash' --include='*.py' --include='*.sh' --include='*.js' --include='*.yml' \
        "$pattern" "$REPO_ROOT")"; then
        printf '%s\n' "$hits"
    else
        echo "(none)"
    fi
}

changelog_signals() {
    local pattern_prior='(Prior|Previously)[[:space:]]+v?[0-9]+\.[0-9]+'
    local pattern_dated='^[[:space:]]*(#|//|\*)[[:space:]]*(19|20)[0-9]{2}-[0-9]{2}-[0-9]{2}[[:space:]]*[-:—]'
    scan_signal "signal A: 'Prior <version>:' / 'Previously <version>:'" "$pattern_prior"
    echo
    scan_signal "signal B: date-prefixed comment entry" "$pattern_dated"
}
echo "### P2  comment_changelog — BLOCKING signals only"
echo "###   READ THIS FOR: whether enabling the handler traps existing files."
echo "###   UPPER BOUND: grep sees whole files, the handler sees comment spans"
echo "###   only, and on an Edit only the ADDED text. .md files are exempt."
probe "comment_changelog blocking signals" changelog_signals

# --- P3: comment_size backlog --------------------------------------------
# Two independent limits: a comment LINE over 400 chars, or a contiguous comment
# BLOCK over 40 lines. Only an edit that GROWS an already-over-limit comment is
# denied, so a backlog is survivable — but it is worth knowing its size.
comment_size_backlog() {
    local f count_lines=0 count_blocks=0 files_lines=0 files_blocks=0
    local scanned=0
    while IFS= read -r f; do
        scanned=$((scanned + 1))
        local n_long n_block
        n_long="$(awk 'length($0) > 400 && $0 ~ /^[[:space:]]*(#|\/\/)/ {n++} END {print n + 0}' "$f")"
        n_block="$(awk '
            /^[[:space:]]*(#|\/\/)/ { run++; if (run > max) max = run; next }
            { run = 0 }
            END { print (max > 40) ? 1 : 0 }' "$f")"
        if [ "$n_long" -gt 0 ]; then
            files_lines=$((files_lines + 1))
            count_lines=$((count_lines + n_long))
            echo "  LINE  $n_long over 400 chars  ${f#"$REPO_ROOT"/}"
        fi
        if [ "$n_block" -gt 0 ]; then
            files_blocks=$((files_blocks + 1))
            count_blocks=$((count_blocks + 1))
            echo "  BLOCK >40 contiguous comment lines  ${f#"$REPO_ROOT"/}"
        fi
    done < <(git -C "$REPO_ROOT" ls-files -z \
        -- '*.bash' '*.py' '*.sh' '*.js' 'files/home/.local/bin/*' 'files/var/local/*' \
        | tr '\0' '\n' | while IFS= read -r rel; do
            [ -f "$REPO_ROOT/$rel" ] && printf '%s\n' "$REPO_ROOT/$rel"
        done)
    echo
    echo "COVERAGE: scanned $scanned tracked file(s)"
    echo "  over-long comment LINES : $count_lines across $files_lines file(s)"
    echo "  over-long comment BLOCKS: $count_blocks across $files_blocks file(s)"
}
echo "### P3  comment_size — existing over-limit comments"
echo "###   READ THIS FOR: how many files become edit-restricted."
echo "###   SHRINKING an over-limit comment is always allowed; a same-size edit"
echo "###   only advises. Only GROWING one is denied — so this is a backlog to"
echo "###   work down, never a trap."
probe "comment_size backlog" comment_size_backlog

# --- P4: sensitive_content readiness -------------------------------------
# The handler is inert until at least one of its two sources is populated.
sensitive_content_sources() {
    local cfg="$REPO_ROOT/.claude/hooks-daemon.yaml"
    local wordlist="$REPO_ROOT/.claude/block-words.secret"
    echo "-- options.public_patterns in $cfg"
    if grep -n -A20 'sensitive_content' "$cfg"; then :; else echo "(handler not present in config)"; fi
    echo
    echo "-- secret word list"
    if [ -f "$wordlist" ]; then
        # NEVER print the contents. A count and the path is the whole payload.
        echo "present: ${wordlist#"$REPO_ROOT"/} — $(grep -cve '^[[:space:]]*$' "$wordlist") non-empty line(s)"
        echo "gitignored: $(git -C "$REPO_ROOT" check-ignore -q "$wordlist" && echo yes || echo 'NO — THIS IS A LEAK RISK')"
    else
        echo "absent: ${wordlist#"$REPO_ROOT"/} (a missing list is a silent no-op)"
    fi
}
echo "### P4  sensitive_content — are either of its two sources populated?"
echo "###   READ THIS FOR: whether enabling it actually does anything here."
echo "###   The word list's CONTENTS are never printed by this script."
probe "sensitive_content sources" sensitive_content_sources

# --- P5: candidate public_patterns, measured before they are made blocking
# A pattern is only safe to make blocking once it is known to match NOTHING that
# is already tracked — otherwise enabling it traps existing files, and the first
# person to touch one is stuck. Each candidate is counted here first.
#
# The candidates are assembled from character classes, never written as literal
# example values, so this script does not itself trip the repo's secret scanner.

# grep exits 1 on "no match", which is the DESIRED answer here. Capture the
# status where it is produced rather than reading it after the fact.
count_nonempty() {
    local n
    if n="$(grep -c .)"; then
        printf '%s' "$n"
    else
        printf '0'
    fi
}

report_pattern() {
    local label="$1" pattern="$2"
    shift 2
    local hits n
    if hits="$(git -C "$REPO_ROOT" grep -InE "$pattern" -- "$@")"; then :; else hits=""; fi
    n="$(printf '%s\n' "$hits" | count_nonempty)"
    echo "-- $label: $n match(es)"
    if [ "$n" -gt 0 ]; then
        printf '%s\n' "$hits" | awk -F: '{ print "     " $1 ":" $2 }' | sort -u | awk 'NR <= 25'
        if [ "$n" -gt 25 ]; then
            echo "     ... ($n total; first 25 file:line shown)"
        fi
    fi
    echo
}

candidate_patterns() {
    local ip_10='10(\.[0-9]{1,3}){3}'
    local ip_192='192\.168(\.[0-9]{1,3}){2}'
    local ip_172='172\.(1[6-9]|2[0-9]|3[01])(\.[0-9]{1,3}){2}'
    local private_ipv4="(^|[^0-9.])($ip_10|$ip_192|$ip_172)([^0-9.]|$)"
    local real_home='/home/[a-z][a-z0-9_-]*'
    local any_email='[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}'

    local tracked_count
    tracked_count="$(git -C "$REPO_ROOT" ls-files | count_nonempty)"
    echo "COVERAGE: $tracked_count tracked file(s) considered"
    echo

    report_pattern "private-ipv4 (literal RFC1918 address)" "$private_ipv4" \
        ':!CLAUDE/Plan' ':!docs/ccy-changelog.md'
    report_pattern "real-home-path (/home/<literal name>)" "$real_home" \
        ':!CLAUDE/Plan' ':!docs/ccy-changelog.md'

    echo "-- non-example email addresses"
    local emails filtered n_em
    if emails="$(git -C "$REPO_ROOT" grep -IhoE "$any_email" -- ':!CLAUDE/Plan')"; then :; else emails=""; fi
    if filtered="$(printf '%s\n' "$emails" | grep -vE '@example\.(com|org|net)')"; then :; else filtered=""; fi
    n_em="$(printf '%s\n' "$filtered" | count_nonempty)"
    echo "   $n_em non-example address(es) outside CLAUDE/Plan"
    if [ "$n_em" -gt 0 ]; then
        printf '%s\n' "$filtered" | sort -u | awk 'NR <= 15 { print "     " $0 }'
    fi
    echo
}
echo "### P5  candidate sensitive_content public_patterns — match counts"
echo "###   READ THIS FOR: which candidates are safe to make BLOCKING."
echo "###   A candidate with 0 matches can be enabled today. A candidate with"
echo "###   matches must be narrowed, or its hits fixed first — enabling it"
echo "###   over an existing backlog traps whoever next touches those files."
probe "candidate public_patterns" candidate_patterns

# --- P6: does each over-length row's detail already exist in its own plan?
# Trimming a row is only safe if what it says survives somewhere. For every
# over-length row this reports the row's distinctive words (>= 7 characters)
# that appear NOWHERE in the plan folder the row links to. A row with an empty
# missing-list is duplicating its plan and can be trimmed freely; a row with
# entries is carrying something unique, which must be relocated into that plan
# BEFORE the row loses it.
#
# The signal is deliberately word-level rather than sentence-level: a summary is
# almost never a verbatim copy, so matching phrases would report every row as
# unique and the check would say nothing.
row_detail_coverage() {
    local readme="$REPO_ROOT/CLAUDE/Plan/README.md"
    local plan_root="$REPO_ROOT/CLAUDE/Plan"
    local rows=0 clean=0 flagged=0

    while IFS=$'\t' read -r lineno row; do
        rows=$((rows + 1))
        local target
        target="$(printf '%s' "$row" | grep -oE '\]\([^)]+\)' | awk 'NR == 1 { print substr($0, 3, length($0) - 3) }')"
        if [ -z "$target" ] || [ ! -d "$plan_root/$target" ]; then
            echo "  line $lineno: LINK TARGET MISSING (${target:-none}) — resolve before trimming"
            flagged=$((flagged + 1))
            continue
        fi

        # Words the row uses that the plan folder never mentions.
        local missing
        missing="$(
            printf '%s' "$row" |
                tr -cs 'A-Za-z0-9_-' '\n' |
                awk 'length($0) >= 7' |
                tr '[:upper:]' '[:lower:]' |
                sort -u |
                while IFS= read -r word; do
                    if grep -rqiF -- "$word" "$plan_root/$target"; then :; else printf '%s ' "$word"; fi
                done
        )"
        if [ -z "$missing" ]; then
            clean=$((clean + 1))
        else
            flagged=$((flagged + 1))
            echo "  line $lineno  $target"
            echo "      words not found anywhere in that plan folder: $missing"
        fi
    done < <(awk 'length($0) > 500 { printf "%d\t%s\n", NR, $0 }' "$readme")

    echo
    echo "COVERAGE: examined $rows over-length row(s); $clean fully duplicated by their plan, $flagged carrying at least one word their plan never uses"
}
echo "### P6  is each over-length row's detail already in its own plan?"
echo "###   READ THIS FOR: which rows can be trimmed freely, and which carry"
echo "###   something that must be relocated into the plan FIRST."
echo "###   A flagged row is not automatically unique content — a single"
echo "###   unmatched word is usually just different phrasing. Read the words."
probe "row detail coverage" row_detail_coverage

echo "=========================================================="
echo " READ FIRST: P1 (index rows) — it is the only probe whose"
echo " findings are already blocking-severity today."
echo " Then P6 — it says which of those rows are safe to trim."
echo "=========================================================="
