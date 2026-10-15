#!/usr/bin/bash
# The docs gate's exit codes, asserted against the real wrapper and the built Go tool (Plan 00125,
# Task 2.2; the tool is src-go/qa/docs, Plan 00192).
#
# `qa-docs.bash` documents three exit codes. Two of them need a tree this repository is not: a
# checkout git cannot answer for, a tree with no in-scope documents, and a playbook list that is
# not YAML. This suite builds those trees and runs the real script over each, so the codes are
# produced by the script and not by reasoning.
#
# The exit-2 paths are what stop a gate that could not look from reading as clean: an unread file,
# an unparsed file and an empty scope are all refusals, never a pass.
#
# The fixtures are outside this repository on purpose. A fixture under `untracked/` is still inside
# THIS git checkout, so `git -C <fixture> ls-files` would answer about this repository and the
# tree git cannot answer for would not exist. They borrow this repository's `scripts/` directory
# for the excluded-trees table only.
#
# Exit codes: 0 = every case passed; 1 = at least one case failed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$REPO_ROOT/scripts/qa-docs.bash"

passed=0
failed=0

FIXTURE_ROOT=$(mktemp -d)
trap 'rm -rf "$FIXTURE_ROOT"' EXIT

# run_gate <fixture-dir> — leaves the exit code in $GATE_RC and the output in $GATE_OUT.
#
# Both are globals rather than a printed return value on purpose: `$(run_gate …)` runs the
# function in a SUBSHELL, so the captured output would be assigned inside it and lost, and
# every case would compare against an empty string.
GATE_OUT=""
GATE_RC=0
run_gate() {
    local root="$1"
    GATE_RC=0
    GATE_OUT="$(QA_JSON_OUT="$FIXTURE_ROOT/out.json" bash "$GATE" --root "$root" 2>&1)" || GATE_RC=$?
}

check_exit() {
    local label="$1" expected="$2" actual="$3" needle="$4"
    if [ "$actual" != "$expected" ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        expected exit %s, got %s\n        output: %s\n' \
            "$label" "$expected" "$actual" "$GATE_OUT" >&2
        return
    fi
    # The code alone is not enough. Several different faults share exit 2, and a gate that
    # returned the right number for the wrong reason would pass a check that only read it.
    if [ -n "$needle" ] && [[ "$GATE_OUT" != *"$needle"* ]]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        exit %s was right but the message did not say why\n' \
            "$label" "$actual" >&2
        printf '        wanted to see: %s\n        output: %s\n' "$needle" "$GATE_OUT" >&2
        return
    fi
    passed=$((passed + 1))
    printf '  PASS  %s -> exit %s\n' "$label" "$actual"
}

# fixture <dir> — the files every check READS, in a tree where nothing is wrong: one gate, its row
# in the QA document, one imported playbook named in both catalogue documents, one topic file in
# the index. The tree is a git repository with everything staged.
fixture() {
    local dir="$1"
    mkdir -p "$dir/docs" "$dir/CLAUDE" "$dir/playbooks" "$dir/scripts"
    printf 'CLAUDE/QA.md\n' > "$dir/CLAUDE.md"
    printf '# QA\n\n| Script | What |\n| --- | --- |\n| %s | x |\n' "\`x.bash\`" > "$dir/CLAUDE/QA.md"
    printf 'gate\tx\tscripts/x.bash\tx failed\tcount\n' > "$dir/scripts/qa-gates.tsv"
    printf -- '- import_playbook: imports/play-example.yml\n' > "$dir/playbooks/playbook-main.yml"
    printf '# Playbooks\n\nplay-example.yml\n' > "$dir/docs/playbooks.md"
    printf '# Architecture\n\nplay-example.yml\n' > "$dir/docs/architecture.md"
    printf '# Doc\n' > "$dir/docs/README.md"
    git init -q "$dir"
    git -C "$dir" add -A
}

echo "=== qa-docs.bash exit codes, driven against fixture trees ==="

# --- exit 0: a tree where nothing is wrong ---------------------------------------------------
#
# The control: without it every refusal below could be the fixture being broken in some other way.
clean="$FIXTURE_ROOT/clean"
fixture "$clean"
run_gate "$clean"
check_exit "a tree with nothing wrong" 0 "$GATE_RC" "✓ docs:"

# --- exit 2: a tree git cannot answer for -----------------------------------------------------
#
# The tool refuses rather than treating "git lists nothing" as "this repository tracks nothing",
# which would turn every link into a finding: a confident verdict from a check that did not run.
no_git="$FIXTURE_ROOT/no-git"
mkdir -p "$no_git/docs"
printf '# Doc\n\n[x](./missing.md)\n' > "$no_git/docs/README.md"
run_gate "$no_git"
# The needle must be unique to the branch under test, not merely present in it.
check_exit "a tree git cannot answer for" 2 "$GATE_RC" "discovery of markdown failed"

# --- exit 2: zero in-scope documents -----------------------------------------------------------
#
# A gate that scanned NOTHING must never report a pass. This is also what keeps the root argument
# safe: a mistyped root finds no documents and is refused, rather than quietly passing.
empty="$FIXTURE_ROOT/empty"
mkdir -p "$empty"
git init -q "$empty"
run_gate "$empty"
check_exit "a tree with no in-scope documents" 2 "$GATE_RC" "0 in-scope"

# --- exit 2: a playbook list that is not YAML ----------------------------------------------------
#
# The file is parsed, not matched line by line, so a file that does not parse is a refusal: a
# gate that could not read what it checks says so.
broken_yaml="$FIXTURE_ROOT/broken-yaml"
fixture "$broken_yaml"
printf -- '- import_playbook: [unclosed\n' > "$broken_yaml/playbooks/playbook-main.yml"
run_gate "$broken_yaml"
check_exit "a playbook list that does not parse" 2 "$GATE_RC" "playbooks/playbook-main.yml does not parse"

# --- exit 1: a real finding ----------------------------------------------------------------
#
# The ordinary failing path, asserted here so the three codes are distinguished by the same
# mechanism. A gate that returned 2 for everything would pass the cases above.
findings="$FIXTURE_ROOT/findings"
fixture "$findings"
printf '# Doc\n\n[x](./NoSuchFile.md)\n' > "$findings/docs/README.md"
git -C "$findings" add -A
run_gate "$findings"
check_exit "a broken link in a tracked document" 1 "$GATE_RC" "does not exist"

# --- exit 1 is a FINDING, not a crash ------------------------------------------------------
#
# A failing run must still write a valid payload. If a crash and a finding were told apart only by
# the exit code, a report that was never written would read as a pass for the stage above it.
#
# jq's own complaint is captured and shown rather than discarded.
payload_check=""
if payload_check="$(jq -e 'has("results") and has("vendored") and .status == "fail"' "$FIXTURE_ROOT/out.json" 2>&1)"; then
    passed=$((passed + 1))
    printf '  PASS  a finding still writes a valid JSON payload\n'
else
    failed=$((failed + 1))
    printf '  FAIL  %s\n        jq said: %s\n' \
        "exit 1 produced no usable JSON — a finding is indistinguishable from a crash" \
        "$payload_check" >&2
fi

echo
echo "passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
