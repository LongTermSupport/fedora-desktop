#!/usr/bin/env bash
# qa-speech-to-text-rules.bash — the rules in .semgrep/speech-to-text.yml (Plan 00156).
#
#   model-present-without-weights        a model judged present from its cache snapshot
#   dropdown-label-carries-explanation   a dropdown option that explains itself, truncated
#
# Both came from the same failure: an `auto` model that was never fully downloaded, listed
# as installed under labels too long to read. Each rule's reasoning, and how to fix a
# finding, is in CLAUDE/QA.md under its identifier.
#
# Scope: every tracked JavaScript and Python file, the extensionless Python scripts under
# files/ included (found by shebang), minus the rules' own fixtures and the vendored
# hooks-daemon tree. Files are handed to semgrep by name, so its default ignore list
# (which skips tests/) cannot drop one, and every file handed in must come back scanned.
#
# Usage: scripts/qa-speech-to-text-rules.bash [PATH...]   (paths: run on those files only)
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or
# a handed file that was not scanned.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RULES="$REPO_ROOT/.semgrep/speech-to-text.yml"
FIXTURES=("$REPO_ROOT/.semgrep/speech-to-text.js" "$REPO_ROOT/.semgrep/speech-to-text.py")

if ! command -v semgrep >/dev/null; then
    echo "ERROR: semgrep not found. Install with: pipx install semgrep" >&2
    exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The rules are proven against their fixtures on every run, so a rule edited into matching
# nothing fails here rather than reporting a clean repo.
for fixture in "${FIXTURES[@]}"; do
    if ! semgrep --test --metrics=off --disable-version-check --config "$RULES" "$fixture" \
        >"$work/selftest.txt" 2>&1; then
        echo "ERROR: .semgrep/speech-to-text.yml failed its fixture ${fixture#"$REPO_ROOT"/}:" >&2
        cat "$work/selftest.txt" >&2
        exit 1
    fi
done

cd "$REPO_ROOT"
targets=()
if [[ "$#" -gt 0 ]]; then
    targets=("$@")
else
    while IFS= read -r -d '' f; do
        [[ -f "$f" && ! -L "$f" ]] || continue
        case "$f" in
            .semgrep/* | .claude/hooks-daemon/* | .claude/skills/* | */node_modules/*) continue ;;
        esac
        # The extension is read from the file name alone: the scripts live under
        # files/home/.local/bin/, whose dot would make every one of them look extended.
        case "${f##*/}" in
            *.js | *.mjs | *.py) targets+=("$f") ;;
            *.*) ;;
            *) if awk 'NR == 1 { found = /^#!.*python/; exit } END { exit !found }' "$f"; then
                targets+=("$f")
            fi ;;
        esac
    done < <(git ls-files -z)
fi
if [[ "${#targets[@]}" -eq 0 ]]; then
    echo "ERROR: no JavaScript or Python files were found; nothing was checked" >&2
    exit 2
fi

# semgrep applies a rule to an extensionless file only under --scan-unknown-extensions, and
# then it applies every rule, whatever its language. So each language is a run of its own,
# with only that language's rules, over the files that are that language.
for lang in javascript python; do
    python3 -c '
import sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    rules = yaml.safe_load(handle)["rules"]
mine = [r for r in rules if r["languages"] == [sys.argv[2]]]
if not mine:
    sys.exit(f"{sys.argv[1]} has no {sys.argv[2]} rules")
yaml.safe_dump({"rules": mine}, sys.stdout)
' "$RULES" "$lang" >"$work/rules-$lang.yml"
done
js=() py=()
for f in "${targets[@]}"; do
    case "$f" in
        *.js | *.mjs) js+=("$f") ;;
        *) py+=("$f") ;;
    esac
done

: >"$work/findings.txt"
: >"$work/handed.txt"
: >"$work/scanned.txt"
scan() {
    local lang="$1"
    shift
    [[ "$#" -gt 0 ]] || return 0
    local rc=0
    semgrep --metrics=off --disable-version-check --config "$work/rules-$lang.yml" \
        --no-rewrite-rule-ids --scan-unknown-extensions --json --quiet "$@" \
        >"$work/scan-$lang.json" 2>"$work/scan-$lang.err" || rc=$?
    if [[ "$rc" -ge 2 ]] || [[ ! -s "$work/scan-$lang.json" ]]; then
        echo "ERROR: semgrep failed on the $lang files (exit $rc):" >&2
        cat "$work/scan-$lang.err" >&2
        exit 2
    fi
    if jq -e '.errors | length > 0' "$work/scan-$lang.json" >/dev/null; then
        echo "ERROR: semgrep could not parse some $lang files, so no rule ran on them:" >&2
        jq -r '.errors[] | "    \(.path // "?"): \(.message)"' "$work/scan-$lang.json" >&2
        exit 2
    fi
    printf '%s\n' "$@" >>"$work/handed.txt"
    jq -r '.paths.scanned[] | ltrimstr("./")' "$work/scan-$lang.json" >>"$work/scanned.txt"
    jq -r '.results[] | "\(.path | ltrimstr("./")):\(.start.line): \(.check_id)"' \
        "$work/scan-$lang.json" >>"$work/findings.txt"
}
scan javascript "${js[@]}"
scan python "${py[@]}"

sort -u -o "$work/handed.txt" "$work/handed.txt"
sort -u -o "$work/scanned.txt" "$work/scanned.txt"
comm -23 "$work/handed.txt" "$work/scanned.txt" >"$work/lost.txt"
if [[ -s "$work/lost.txt" ]]; then
    echo "ERROR: semgrep did not scan files it was handed:" >&2
    awk '{print "    " $0}' "$work/lost.txt" >&2
    exit 2
fi

scanned="$(wc -l <"$work/scanned.txt")"
sort -u -o "$work/findings.txt" "$work/findings.txt"
if [[ -s "$work/findings.txt" ]]; then
    cat "$work/findings.txt"
    files="$(awk -F: '{print $1}' "$work/findings.txt" | sort -u | wc -l)"
    echo "speech-to-text rules: $(wc -l <"$work/findings.txt") finding(s) in ${files} file(s). See CLAUDE/QA.md under each identifier."
    echo "passed: $((scanned - files)) failed: ${files}"
    exit 1
fi
echo "passed: ${scanned} failed: 0"
