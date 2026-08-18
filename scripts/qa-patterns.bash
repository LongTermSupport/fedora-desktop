#!/usr/bin/bash
# Pattern-based QA using Semgrep - LLM-friendly
# Runs .semgrep/bash-conventions.yml rules against all bash files in the repo.
# stdout:  terse — violations + summary only
# JSON:    ${QA_JSON_OUT:-/tmp/qa-patterns-results.json}
#
# jq usage:
#   jq '.status'           # "pass" or "fail"
#   jq '.summary'          # {total, passed, failed}
#   jq '.failures[]'       # all files with violations
#
# Exit codes:
#   0  pass
#   1  fail (violations found)
#   2  missing required tool (semgrep not installed)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
JSON_OUT="${QA_JSON_OUT:-/tmp/qa-patterns-results.json}"
TMP_SEMGREP=$(mktemp)
TMP_SEMGREP_ERR=$(mktemp)
TMP_MAP=$(mktemp)
MIRROR=$(mktemp -d)
trap 'rm -rf "$TMP_SEMGREP" "$TMP_SEMGREP_ERR" "$TMP_MAP" "$MIRROR"' EXIT

# Require semgrep
if ! command -v semgrep >/dev/null 2>/dev/null; then
    echo "ERROR: semgrep not found. Install with: pipx install semgrep" >&2
    exit 2
fi

# Self-check: validate the rules against their annotated fixture before scanning.
# A rule that silently stops matching (or over-matches) is a worse failure than a
# missing tool, so a self-test regression is a hard error (exit 2), never "0 issues".
TMP_TEST_ERR=$(mktemp)
if ! semgrep --test \
        --config "$REPO_ROOT/.semgrep/bash-conventions.yml" \
        "$REPO_ROOT/.semgrep/bash-conventions.bash" \
        --metrics=off >/dev/null 2>"$TMP_TEST_ERR"; then
    echo "ERROR: semgrep rule self-tests failed (see .semgrep/bash-conventions.bash)" >&2
    cat "$TMP_TEST_ERR" >&2
    rm -f "$TMP_TEST_ERR"
    exit 2
fi
rm -f "$TMP_TEST_ERR"

# Build a scan mirror (Plan 00076)
# --------------------------------
# Semgrep decides a file's language from its extension, and for an extensionless
# file it falls back to reading the shebang — but ONLY if the file has the owner
# execute bit (`target_manager.py`, `S_IRUSR | S_IXUSR`). No `+x`, no shebang
# read, no language, no scan. That silently excluded 27 of this repo's scripts,
# including the 140 KB ccy launcher, while this gate printed "125 files OK".
#
# `chmod +x` is NOT the fix. Two of those files — /var/local/colours and
# /var/local/ps1-prompt — are SOURCED libraries their play deploys 0644, so an
# execute bit would be a lie told to a linter. Coverage must not depend on a
# permission bit at all.
#
# So: copy every discovered script into a temp mirror at its own repo-relative
# path, appending `.bash` where it has no shell extension, and scan that. The
# relative path is preserved so each rule's `paths.include` still applies
# exactly as it does against the real tree.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=qa-shell-discovery.bash
source "$REPO_ROOT/scripts/qa-shell-discovery.bash"

qa_discover_shell_files "$REPO_ROOT"
if [[ ${#QA_SHELL_FILES[@]} -eq 0 ]]; then
    echo "ERROR: shell-script discovery found 0 files under $REPO_ROOT" >&2
    echo "  This repo always has bash files, so the discovery is broken —" >&2
    echo "  reporting a pass here would vouch for code nothing had read." >&2
    exit 2
fi

: > "$TMP_MAP"
for src_file in "${QA_SHELL_FILES[@]}"; do
    repo_rel="${src_file#"$REPO_ROOT"/}"
    mirror_rel="$repo_rel"
    if [[ "$repo_rel" != *.sh && "$repo_rel" != *.bash ]]; then
        mirror_rel="$repo_rel.bash"
    fi
    mkdir -p "$MIRROR/$(dirname "$mirror_rel")"
    cp "$src_file" "$MIRROR/$mirror_rel"
    jq -nc --arg m "$mirror_rel" --arg r "$repo_rel" '{($m): $r}' >> "$TMP_MAP"
done

# Run semgrep over the mirror
# --json-output: write findings JSON to file (separate from progress output)
# --quiet:       suppress progress/banner output
# --metrics=off: no telemetry
# The mirror contains only files that passed discovery, so the previous
# --exclude list (vendored/upstream trees) is now enforced by discovery itself —
# one exclusion list for both bash gates, in scripts/qa-shell-discovery.bash.
rc=0
(
    cd "$MIRROR" || exit 2
    semgrep \
        --config "$REPO_ROOT/.semgrep/bash-conventions.yml" \
        --json-output "$TMP_SEMGREP" \
        --metrics=off \
        --quiet \
        . 2>"$TMP_SEMGREP_ERR"
) || rc=$?

# rc >= 2 means semgrep itself failed (not just "found something")
if [[ $rc -ge 2 ]]; then
    echo "ERROR: semgrep failed (exit $rc)" >&2
    cat "$TMP_SEMGREP_ERR" >&2
    exit 2
fi

# Validate JSON output was produced
if [[ ! -s "$TMP_SEMGREP" ]]; then
    echo "ERROR: semgrep produced no JSON output" >&2
    cat "$TMP_SEMGREP_ERR" >&2
    exit 2
fi

# Build QA JSON from semgrep output
# Semgrep JSON fields used:
#   .paths.scanned[]          - files that were scanned
#   .results[].path           - file with a finding
#   .results[].check_id       - rule that matched
#   .results[].start.line     - line number
#   .results[].extra.message  - human-readable message
# Mirror paths are translated back to real repo paths, so every path this gate
# reports is one a human can open.
jq \
    --slurpfile mapfile "$TMP_MAP" \
    '($mapfile | add) as $map
     | def unmirror: sub("^\\./"; "") | ($map[.] // .);
     {
        "type": "patterns",
        "status": (if (.results | length) > 0 then "fail" else "pass" end),
        "scanned": [.paths.scanned[] | unmirror],
        "summary": {
            "total":  (.paths.scanned | length),
            "passed": ((.paths.scanned | length) - (.results | map(.path) | unique | length)),
            "failed": (.results | map(.path) | unique | length)
        },
        "failures": [
            .results | group_by(.path)[] | {
                "file":   (.[0].path | unmirror),
                "type":   "patterns",
                "status": "fail",
                "error":  (map(.check_id + ":" + (.start.line | tostring) + " " + .extra.message) | join("; "))
            }
        ]
    }' "$TMP_SEMGREP" > "$JSON_OUT"

# Coverage assertion, part 1 of 2 (Plan 00076): FILES THE ANALYSER NEVER READ
#
# `.paths.scanned` is NOT proof a file was analysed. Semgrep lists a file it
# could not PARSE as scanned, returns zero findings for it, exits 0, and records
# the reason only in `.errors[]`. `ftp-camera` — 2,475 lines — was in exactly
# that state, and so were two others that the OLD gate scanned too. Reading the
# scanned list alone reproduced this plan's own defect inside the gate meant to
# catch it.
#
# Two error classes, deliberately treated differently:
#
#   "Syntax error"   the whole file failed to parse. NO rule ran on ANY line.
#                    Indistinguishable from not scanning it at all → GATING.
#   "PartialParsing" the file parsed except for named ranges. Every rule ran on
#                    everything else, and the ranges are printed, so nothing is
#                    claimed that was not checked. Reported, not gating.
#
# Whether semgrep's grammar can parse a given piece of VALID bash is a property
# of semgrep, not of our code (all of these pass `bash -n`), so "rewrite it until
# the parser copes" is not automatically the right answer. The exceptions below
# are therefore explicit, named, and self-expiring: a file that starts parsing
# must be removed from the list, and the gate fails until it is.
# Cause not yet established for these two (Plan 00076 Task 4.3). What IS
# established: a one-line `case … esac` nested inside a `while` inside a command
# substitution breaks the grammar, and one occurrence of it made the whole of
# `ftp-camera` — 2,475 lines — unanalysable. Splitting that `case` across lines
# restored it, and the first thing it then reported was a real defect.
SEMGREP_CANNOT_PARSE=(
    "files/home/.local/bin/rclone-cache-status"
    "files/home/.local/bin/rclone-tail"
)

UNPARSED=()
while IFS= read -r rel; do
    UNPARSED+=("$rel")
done < <(jq -r --slurpfile mapfile "$TMP_MAP" \
    '($mapfile | add) as $map
     | [.errors[]? | select(.type == "Syntax error")
        | $map[(.path // "" | sub("^\\./"; ""))] // .path // "(no path)"]
     | unique[]' "$TMP_SEMGREP")

UNEXPECTED_UNPARSED=()
for rel in ${UNPARSED[@]+"${UNPARSED[@]}"}; do
    known=0
    for allowed in "${SEMGREP_CANNOT_PARSE[@]}"; do
        [[ "$rel" == "$allowed" ]] && known=1
    done
    [[ $known -eq 1 ]] || UNEXPECTED_UNPARSED+=("$rel")
done

# The list must shrink, never rot: a file that has started parsing is coverage
# regained, and leaving it listed would hide the next regression behind it.
STALE_EXCEPTIONS=()
for allowed in "${SEMGREP_CANNOT_PARSE[@]}"; do
    still_broken=0
    for rel in ${UNPARSED[@]+"${UNPARSED[@]}"}; do
        [[ "$rel" == "$allowed" ]] && still_broken=1
    done
    [[ $still_broken -eq 1 ]] || STALE_EXCEPTIONS+=("$allowed")
done

if [[ ${#UNEXPECTED_UNPARSED[@]} -gt 0 ]]; then
    echo "ERROR: semgrep could not parse ${#UNEXPECTED_UNPARSED[@]} file(s), so NO rule ran on them:" >&2
    printf '    %s\n' "${UNEXPECTED_UNPARSED[@]}" >&2
    echo "  A file the analyser never read cannot be reported as passing." >&2
    exit 2
fi
if [[ ${#STALE_EXCEPTIONS[@]} -gt 0 ]]; then
    echo "ERROR: ${#STALE_EXCEPTIONS[@]} file(s) in SEMGREP_CANNOT_PARSE now parse fine:" >&2
    printf '    %s\n' "${STALE_EXCEPTIONS[@]}" >&2
    echo "  Remove them from the list in scripts/qa-patterns.bash — a stale" >&2
    echo "  exception hides the next file that stops being analysed." >&2
    exit 2
fi

# Coverage assertion, part 2 of 2: FILES NEVER REACHED
# Every file handed to semgrep must come back in .paths.scanned. A file semgrep
# quietly declined — wrong language, over --max-target-bytes — would otherwise be
# reported as passing.
MISSED=()
while IFS= read -r rel; do
    MISSED+=("$rel")
done < <(jq -r --slurpfile mapfile "$TMP_MAP" \
    '($mapfile | add) as $map
     | ([.scanned[]] | map({(.): true}) | add // {}) as $seen
     | [$map[]] | map(select($seen[.] | not))[]' "$JSON_OUT")

if [[ ${#MISSED[@]} -gt 0 ]]; then
    echo "ERROR: semgrep did not scan ${#MISSED[@]} discovered shell script(s):" >&2
    printf '    %s\n' "${MISSED[@]}" >&2
    echo "  A pass reported over files the analyser never opened is not a pass." >&2
    cat "$TMP_SEMGREP_ERR" >&2
    exit 2
fi

# Coverage report — printed on EVERY run, pass or fail.
#
# A summary that says only "153 files OK" is the shape of statement this plan
# exists to distrust. These two lines say what was actually analysed, so a
# shrinking number is visible rather than something you have to go looking for.
PARTIAL_COUNT=$(jq -r '[.errors[]? | select(.type != "Syntax error")] | length' "$TMP_SEMGREP")
if [[ ${#UNPARSED[@]} -gt 0 ]]; then
    echo "⚠ patterns: ${#UNPARSED[@]} file(s) NOT ANALYSED — semgrep cannot parse them:"
    printf '    %s\n' "${UNPARSED[@]}"
    echo "    (known exceptions; they pass bash -n. See SEMGREP_CANNOT_PARSE in this script.)"
fi
if [[ "$PARTIAL_COUNT" -gt 0 ]]; then
    echo "⚠ patterns: $PARTIAL_COUNT file(s) analysed EXCEPT for some ranges:"
    jq -r --slurpfile mapfile "$TMP_MAP" \
        '($mapfile | add) as $map
         | .errors[]? | select(.type != "Syntax error")
         | ((.path // "") | sub("^\\./"; "")) as $mirrored
         | "    \($map[$mirrored] // .path // "(no path)")"' "$TMP_SEMGREP" | sort -u
fi

# Terse summary
TOTAL=$(jq '.summary.total' "$JSON_OUT")
ERRORS=$(jq '.summary.failed' "$JSON_OUT")

if [[ $ERRORS -eq 0 ]]; then
    echo "✓ patterns: $TOTAL files OK"
    exit 0
else
    echo "✗ patterns: $ERRORS/$TOTAL files failed → $JSON_OUT"
    jq -r '.failures[] | "  ✗ \(.file)\n    \(.error)"' "$JSON_OUT"
    exit 1
fi
