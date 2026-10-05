#!/usr/bin/env bash
# qa-ansible-pause.bash — the rule pause-without-register.
#
# A play that stops on `ansible.builtin.pause` to show text and wait for Enter hangs an
# unattended batch (`run.bash --changed --yes`, CLAUDE/Plan/meta-deploy.bash) on a
# question nobody knows is owed. A pause task must either `register:` the answer the play
# goes on to use, or carry a `# PAUSE-OK: <reason>` comment saying why it has to wait.
# Status and instructions go in `ansible.builtin.debug`. The reasoning, and how to fix a
# finding, is in CLAUDE/QA.md under "pause-without-register".
#
# The scan is line-based, not a YAML load, because the annotation is a comment and a YAML
# loader drops comments. A task is the list item that holds the pause key: from its `- `
# line to the next line at or left of that dash. Comment lines directly above the dash
# belong to the task too, so a PAUSE-OK may sit on its own line before `- name:`.
#
# The rule is proven against its fixtures on every run: every line marked `EXPECT-FINDING`
# in the flagged fixture must be reported and nothing else, and the clean fixture must
# report nothing. A rule edited into matching nothing fails here.
#
# Usage: scripts/qa-ansible-pause.bash [PATH...]   (paths: scan those files only)
# Exit: 0 clean, 1 findings or a fixture self-test failure, 2 nothing found to scan.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURE_DIR="$REPO_ROOT/tests/fixtures/ansible-pause"
FLAGGED_FIXTURE="$FIXTURE_DIR/flagged.yml"
CLEAN_FIXTURE="$FIXTURE_DIR/clean.yml"

# scan_pauses FILE... — print `path:line: pause-without-register` for each pause task with
# neither a register nor a PAUSE-OK comment.
scan_pauses() {
    awk '
        function indent_of(s) { match(s, /^[ ]*/); return RLENGTH }
        function is_blank(s) { return s ~ /^[ \t]*$/ }
        function is_comment(s) { return s ~ /^[ \t]*#/ }
        function report(fname, n, total,    i, key, ki, d, start, stop, ok, reg, t, j) {
            for (i = 1; i <= n; i++) {
                if (L[i] !~ /^[ ]*(- )?(ansible\.(builtin|legacy)\.)?pause:([ \t]|$)/) continue
                key = L[i]
                ki = indent_of(key)
                if (substr(key, ki + 1, 2) == "- ") { start = i; d = ki; ki = ki + 2 }
                else {
                    start = 0
                    for (j = i - 1; j >= 1; j--) {
                        if (is_blank(L[j]) || is_comment(L[j])) continue
                        if (indent_of(L[j]) < ki) { start = j; break }
                    }
                    if (start == 0) continue
                    d = indent_of(L[start])
                }
                stop = n + 1
                for (j = start + 1; j <= n; j++) {
                    if (is_blank(L[j])) continue
                    if (indent_of(L[j]) <= d) { stop = j; break }
                }
                ok = 0; reg = 0
                for (j = start; j < stop; j++) {
                    if (L[j] ~ /#[ \t]*PAUSE-OK:[ \t]*[^ \t]/) ok = 1
                    t = L[j]
                    if (j == start) sub(/^[ ]*- /, "", t); else sub(/^[ ]*/, "", t)
                    if (t ~ /^register:/ && (j == start || indent_of(L[j]) == ki)) reg = 1
                }
                for (j = start - 1; j >= 1 && is_comment(L[j]); j--) {
                    if (L[j] ~ /#[ \t]*PAUSE-OK:[ \t]*[^ \t]/) ok = 1
                }
                if (!ok && !reg) printf "%s:%d: pause-without-register\n", fname, i
            }
        }
        FNR == 1 && NR > 1 { report(prev, n); n = 0 }
        { L[++n] = $0; prev = FILENAME }
        END { if (n > 0) report(prev, n) }
    ' "$@"
}

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
cd "$REPO_ROOT"

for f in "$FLAGGED_FIXTURE" "$CLEAN_FIXTURE"; do
    if [[ ! -f "$f" ]]; then
        echo "ERROR: fixture $f is missing; the rule cannot be proven" >&2
        exit 1
    fi
done
grep -nE '#[[:space:]]*EXPECT-FINDING[[:space:]]*$' "$FLAGGED_FIXTURE" | awk -F: -v f="$FLAGGED_FIXTURE" \
    '{print f ":" $1 ": pause-without-register"}' | sort >"$work/expected.txt"
if [[ ! -s "$work/expected.txt" ]]; then
    echo "ERROR: $FLAGGED_FIXTURE marks no EXPECT-FINDING line; the red case proves nothing" >&2
    exit 1
fi
scan_pauses "$FLAGGED_FIXTURE" | sort >"$work/flagged.txt"
if ! diff -u "$work/expected.txt" "$work/flagged.txt" >"$work/selftest.txt"; then
    echo "ERROR: pause-without-register failed its flagged fixture (expected vs reported):" >&2
    cat "$work/selftest.txt" >&2
    exit 1
fi
scan_pauses "$CLEAN_FIXTURE" >"$work/clean.txt"
if [[ -s "$work/clean.txt" ]]; then
    echo "ERROR: pause-without-register reported findings in its clean fixture:" >&2
    cat "$work/clean.txt" >&2
    exit 1
fi

targets=()
if [[ "$#" -gt 0 ]]; then
    targets=("$@")
else
    while IFS= read -r -d '' f; do
        targets+=("$f")
    done < <(git ls-files -z -- 'playbooks/*.yml' 'playbooks/*.yaml' 'tasks/*.yml' 'tasks/*.yaml')
fi
if [[ "${#targets[@]}" -eq 0 ]]; then
    echo "ERROR: no YAML found under playbooks/ or tasks/; nothing was checked" >&2
    exit 2
fi

scan_pauses "${targets[@]}" >"$work/findings.txt"
if [[ -s "$work/findings.txt" ]]; then
    cat "$work/findings.txt"
    files="$(awk -F: '{print $1}' "$work/findings.txt" | sort -u | wc -l)"
    echo "pause-without-register: $(wc -l <"$work/findings.txt") pause task(s) in ${files} file(s) wait with no register and no '# PAUSE-OK: <reason>'. Use ansible.builtin.debug for status. See CLAUDE/QA.md \"pause-without-register\"."
    echo "passed: $((${#targets[@]} - files)) failed: ${files}"
    exit 1
fi
echo "passed: ${#targets[@]} failed: 0"
