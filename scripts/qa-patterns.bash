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
# shellcheck source=qa-discovery.bash
source "$REPO_ROOT/scripts/qa-discovery.bash"

qa_discover_shell_files "$REPO_ROOT"
if [[ ${#QA_SHELL_FILES[@]} -eq 0 ]]; then
    echo "ERROR: shell-script discovery found 0 files under $REPO_ROOT" >&2
    echo "  This repo always has bash files, so the discovery is broken —" >&2
    echo "  reporting a pass here would vouch for code nothing had read." >&2
    exit 2
fi

# Appending `.bash` can make two DIFFERENT repo files claim one mirror path:
# `dir/foo` and `dir/foo.bash` both mirror to `dir/foo.bash`. The second `cp`
# would overwrite the first, and the map — built by merging one object per file —
# would keep only the last value for that key. So one real script would be
# neither scanned NOR missed by the assertion below, which reads the map's
# values: it would simply cease to exist, and the gate would report a pass over
# it. That is this plan's own defect, reproduced inside the fix for it, so it is
# a hard failure rather than a clever disambiguation.
declare -A MIRROR_CLAIMED=()
: > "$TMP_MAP"
for src_file in "${QA_SHELL_FILES[@]}"; do
    repo_rel="${src_file#"$REPO_ROOT"/}"
    mirror_rel="$repo_rel"
    if [[ "$repo_rel" != *.sh && "$repo_rel" != *.bash ]]; then
        mirror_rel="$repo_rel.bash"
    fi
    if [[ -n "${MIRROR_CLAIMED[$mirror_rel]:-}" ]]; then
        echo "ERROR: two files claim the same scan-mirror path '$mirror_rel':" >&2
        echo "    ${MIRROR_CLAIMED[$mirror_rel]}" >&2
        echo "    $repo_rel" >&2
        echo "  One would silently replace the other and vanish from this gate" >&2
        echo "  entirely — scanned by nothing, and missed by no assertion." >&2
        echo "  Rename one of them, or teach the mirror a collision-free suffix." >&2
        exit 2
    fi
    MIRROR_CLAIMED["$mirror_rel"]="$repo_rel"
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
# one exclusion list for both bash gates, in scripts/qa-discovery.bash.
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
# Two error classes, and they cost completely different amounts. Both figures
# below were MEASURED with a probe rule whose regex matches on every line, so the
# parse is always attempted (Plan 00076 Task 4.3):
#
#   "Syntax error"   costs EVERYTHING. A rule whose regex has matches gets none
#                    of them back: `rclone-tail` had three real `|| echo` sites
#                    and reported zero. GATING.
#   "PartialParsing" costs NOTHING for this ruleset. `ftp-camera` reports a
#                    585-2486 skipped range, yet a probe for `echo` returned 293
#                    findings against 293 raw occurrences — 262 of them on
#                    distinct lines INSIDE that range. Reported, not gating.
#
# The asymmetry is because every rule in .semgrep/bash-conventions.yml is
# `pattern-regex`, and the regex engine reads raw text — the parse tree is never
# consulted for a match. A PartialParsing range therefore costs nothing TODAY and
# would start costing something the day an AST `pattern:` rule is added. It is
# reported for that reason, not because coverage is currently lost.
#
# This also kills the earlier "parseability is a property of (file × rule)"
# reading. Semgrep only parses a file when a rule's regex actually MATCHED
# something in it: across ten measured (file, rule) cells the correlation was
# exact — matches > 0 ⇒ parse attempted ⇒ error surfaces; matches == 0 ⇒ no parse,
# no error. So the three rules that appeared to "parse these files fine" had
# simply never been asked. An absence of an error was being read as coverage,
# which is this plan's own defect class one level up.
#
# Whether semgrep's grammar can parse a given piece of VALID bash is a property
# of semgrep, not of our code (all of these pass `bash -n`), so "rewrite it until
# the parser copes" is not automatically the right answer — but when the cost is
# a whole-file blackout it is. Both former entries were one construct:
# `if ! cmd <<'PY' … PY` with `then` on the following line, valid bash that
# tree-sitter-bash cannot parse. Feeding the heredoc to a command substitution
# instead fixed both. The list is empty and self-expiring in both directions: an
# unlisted unparseable file fails the gate, and so does a listed one that parses.
SEMGREP_CANNOT_PARSE=()

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
    for allowed in ${SEMGREP_CANNOT_PARSE[@]+"${SEMGREP_CANNOT_PARSE[@]}"}; do
        [[ "$rel" == "$allowed" ]] && known=1
    done
    [[ $known -eq 1 ]] || UNEXPECTED_UNPARSED+=("$rel")
done

# The list must shrink, never rot: a file that has started parsing is coverage
# regained, and leaving it listed would hide the next regression behind it.
STALE_EXCEPTIONS=()
for allowed in ${SEMGREP_CANNOT_PARSE[@]+"${SEMGREP_CANNOT_PARSE[@]}"}; do
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
PARTIAL_COUNT=$(jq -r '[.errors[]? | select(.type != "Syntax error")
                        | (.path // "(no path)")] | unique | length' "$TMP_SEMGREP")
if [[ ${#UNPARSED[@]} -gt 0 ]]; then
    # This branch is only reachable for a file in SEMGREP_CANNOT_PARSE (an
    # unlisted one already exited 2 above), and that list is empty today.
    #
    # "NOT ANALYSED" is the right words here, and they are strong on purpose. An
    # earlier revision softened them to "some rules could not parse" on the
    # theory that parseability was a property of (file × rule). It is not: a rule
    # is only asked to parse a file when its regex has already matched something
    # there, so the rules that appeared to cope had simply never been asked.
    # Measured on the two former entries — every match a rule DID have was lost.
    echo "⚠ patterns: ${#UNPARSED[@]} file(s) semgrep could not parse — every rule with a match in them lost it:"
    printf '    %s\n' "${UNPARSED[@]}"
    echo "    (known exceptions; they pass bash -n. See SEMGREP_CANNOT_PARSE in this script.)"
fi
if [[ "$PARTIAL_COUNT" -gt 0 ]]; then
    # Report HOW MUCH was skipped, and what that currently costs — which is
    # nothing.
    #
    # The magnitude is here because a bare file list makes a 3-line gap and a
    # 1,900-line gap look identical. The "costs nothing" is here because the
    # previous wording — "N of M lines not analysed" — was itself false, in the
    # alarming direction rather than the reassuring one, but false either way.
    # Measured: `ftp-camera` reports a 585-2486 skipped range, and a probe rule
    # for `echo` returned 293 findings against 293 raw occurrences, 262 of them
    # on distinct lines inside that range. Every rule in this repo's ruleset is
    # `pattern-regex`, and the regex engine reads raw text — the parse tree is
    # never consulted for a match.
    #
    # So the skipped ranges are a standing note about a FUTURE cost: the day an
    # AST `pattern:` rule is added, these lines stop being covered. Keeping the
    # numbers visible is what makes that day noticeable.
    #
    # PartialParsing carries its ranges in `.type[1][]`; the union is taken so
    # overlapping ranges are not double-counted.
    echo "⚠ patterns: $PARTIAL_COUNT file(s) semgrep parsed only in part (regex rules unaffected — see below):"
    while IFS=$'\t' read -r rel skipped first_line last_line; do
        total="?"
        if [[ -f "$REPO_ROOT/$rel" ]]; then
            total=$(wc -l < "$REPO_ROOT/$rel")
        fi
        printf '    %s — %s of %s lines outside the parse tree (first gap from %s, last to %s)\n' \
            "$rel" "$skipped" "$total" "$first_line" "$last_line"
    done < <(jq -r --slurpfile mapfile "$TMP_MAP" \
        '($mapfile | add) as $map
         | def union_len:
             sort_by(.s)
             | reduce .[] as $r ({acc: 0, cs: null, ce: null};
                 if .cs == null then {acc: .acc, cs: $r.s, ce: $r.e}
                 elif $r.s <= (.ce + 1) then
                     {acc: .acc, cs: .cs, ce: (if $r.e > .ce then $r.e else .ce end)}
                 else {acc: (.acc + .ce - .cs + 1), cs: $r.s, ce: $r.e}
                 end)
             | .acc + (if .cs == null then 0 else (.ce - .cs + 1) end);
           .errors[]? | select(.type != "Syntax error")
         | ((.path // "") | sub("^\\./"; "")) as $mirrored
         | ([.type[1]? // [] | .[] | {s: .start.line, e: .end.line}]) as $ranges
         | [($map[$mirrored] // .path // "(no path)"),
            ($ranges | union_len | tostring),
            ($ranges | map(.s) | min // 0 | tostring),
            ($ranges | map(.e) | max // 0 | tostring)]
         | @tsv' "$TMP_SEMGREP" | sort -u)
    echo "    Every rule here is pattern-regex, which reads raw text — measured as"
    echo "    full coverage of those ranges. This becomes a real gap only if an AST"
    echo "    'pattern:' rule is ever added to .semgrep/bash-conventions.yml."
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
