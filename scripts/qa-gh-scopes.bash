#!/usr/bin/env bash
# qa-gh-scopes.bash — the gh-scope-outside-ssot rule (.semgrep/gh-scopes.yml).
#
# vars/github-required-scopes.yml is the one statement of which scopes a gh token needs,
# and helpers/github_scopes/ is the one implementation of how scopes imply one another.
# This gate finds every other place a GitHub OAuth scope is named. The rule's reasoning,
# and how to fix a finding, are in CLAUDE/QA.md under "gh-scope-outside-ssot".
#
# The rule reads every tracked regular file, in every language, because a scope restated
# in a doc or a message drifts just as one restated in code does. Files are handed to
# semgrep by name. A directory scan would apply semgrep's default ignore list, which skips
# tests/, and would hide a copy there. Symlinks are left out: semgrep refuses them, and
# each one points at a file that is scanned in its own right or is vendored.
#
# Exit: 0 clean, 1 findings or a rule self-test failure, 2 semgrep missing or broken, or
# a handed file that was not scanned.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RULES="$REPO_ROOT/.semgrep/gh-scopes.yml"
FIXTURE="$REPO_ROOT/.semgrep/gh-scopes.fixture"

if ! command -v semgrep >/dev/null; then
    echo "ERROR: semgrep not found. Install with: pipx install semgrep" >&2
    exit 2
fi

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The rule is proven against its fixture on every run, so a rule edited into matching
# nothing fails here rather than reporting a clean repo.
if ! semgrep --test --config "$RULES" "$FIXTURE" >"$work/selftest.txt" 2>&1; then
    echo "ERROR: gh-scope-outside-ssot failed its fixture (.semgrep/gh-scopes.fixture):" >&2
    cat "$work/selftest.txt" >&2
    exit 1
fi

cd "$REPO_ROOT"
targets=()
while IFS= read -r -d '' f; do
    if [[ -f "$f" && ! -L "$f" ]]; then
        targets+=("$f")
    fi
done < <(git ls-files -z)
if [[ "${#targets[@]}" -eq 0 ]]; then
    echo "ERROR: git ls-files named no files; nothing was checked" >&2
    exit 2
fi

rc=0
semgrep --config "$RULES" --json --quiet --metrics=off "${targets[@]}" \
    >"$work/scan.json" 2>"$work/scan.err" || rc=$?
if [[ "$rc" -ge 2 ]] || [[ ! -s "$work/scan.json" ]]; then
    echo "ERROR: semgrep failed (exit $rc):" >&2
    cat "$work/scan.err" >&2
    exit 2
fi

# The rule's own path excludes drop files without listing them as skipped, so the
# expected set is read from the rule itself. Anything else handed in must come back as
# scanned, or a "clean" verdict covers files nobody read.
printf '%s\n' "${targets[@]}" | python3 -c '
import fnmatch, sys, yaml
with open(sys.argv[1], encoding="utf-8") as handle:
    rules = yaml.safe_load(handle)["rules"]
globs = [g for rule in rules for g in rule.get("paths", {}).get("exclude", [])]
if not globs:
    sys.exit("the rule declares no path excludes; this gate expects its own at least")
# semgrep lets "**/" match no directory at all; fnmatch needs one, so try both.
globs += [g.replace("**/", "") for g in globs if "**/" in g]
for line in sys.stdin:
    path = line.rstrip("\n")
    if not any(fnmatch.fnmatch(path, g) for g in globs):
        print(path)
' "$RULES" | sort >"$work/handed.txt"
jq -r '.paths.scanned[] | ltrimstr("./")' "$work/scan.json" | sort >"$work/scanned.txt"
jq -r '.paths.skipped[]? | .path | ltrimstr("./")' "$work/scan.json" | sort >"$work/skipped.txt"
comm -23 "$work/handed.txt" "$work/scanned.txt" | comm -23 - "$work/skipped.txt" >"$work/lost.txt"
if [[ -s "$work/lost.txt" ]]; then
    echo "ERROR: semgrep did not scan files it was handed and did not say why it skipped them:" >&2
    awk '{print "    " $0}' "$work/lost.txt" >&2
    exit 2
fi

jq -r '.results[] | "\(.path | ltrimstr("./")):\(.start.line)"' "$work/scan.json" | sort -u >"$work/findings.txt"
scanned="$(wc -l <"$work/scanned.txt")"
if [[ -s "$work/findings.txt" ]]; then
    awk '{print $0 "  gh-scope-outside-ssot"}' "$work/findings.txt"
    files="$(awk -F: '{print $1}' "$work/findings.txt" | sort -u | wc -l)"
    lines="$(wc -l <"$work/findings.txt")"
    echo "gh-scope-outside-ssot: ${lines} line(s) in ${files} file(s) name a GitHub scope outside vars/github-required-scopes.yml. See CLAUDE/QA.md \"gh-scope-outside-ssot\"."
    echo "passed: $((scanned - files)) failed: ${files}"
    exit 1
fi
echo "passed: ${scanned} failed: 0"
