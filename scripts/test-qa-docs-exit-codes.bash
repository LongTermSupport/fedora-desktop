#!/usr/bin/bash
# The docs gate's exit codes, asserted against the real script (Plan 00125, Task 2.2).
#
# `qa-docs.bash` documents three exit codes and until now only one of them had ever been
# produced by running it. The other two need a tree this repository is not — a checkout git
# cannot answer for, and a tree with no in-scope documents — so they were reachable by
# reasoning and by hand-assembled payloads, never by the script. A reviewer measured the
# chain twice, by hand, and the measurement was thrown away with the session both times.
#
# The exit-2 path is the one that matters. It is what stops a CRASHED checker from reading as
# a clean run: the interpreter exits 1, which this gate treats as its ordinary findings
# status, and only the payload validation turns that into a refusal. Two hops, and a change
# to either would make a traceback look like "no findings" — the defect class this whole plan
# exists to remove, in the gate the plan repaired.
#
# The fixtures use a SYMLINK to the real `helpers/`, not a copy. A copy cannot notice the
# thing it is copying changing, which is the argument the coupling gate in
# test-qa-helper-summary.bash is built on.
#
# Exit codes: 0 = every case passed; 1 = at least one case failed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$REPO_ROOT/scripts/qa-docs.bash"

passed=0
failed=0

# Outside the repository on purpose. A fixture under `untracked/` is still inside THIS git
# checkout, so `git -C <fixture> ls-files` would answer about this repository and the tree
# git cannot answer for would not exist.
FIXTURE_ROOT=$(mktemp -d)
trap 'rm -rf "$FIXTURE_ROOT"' EXIT

# run_gate <fixture-dir> — leaves the exit code in $GATE_RC and the output in $GATE_OUT.
#
# Both are globals rather than a printed return value on purpose: `$(run_gate …)` runs the
# function in a SUBSHELL, so the captured output would be assigned inside it and lost, and
# every case would compare against an empty string. It read as working because the exit codes
# were still right.
GATE_OUT=""
GATE_RC=0
run_gate() {
    local root="$1"
    GATE_RC=0
    GATE_OUT="$(QA_JSON_OUT="$FIXTURE_ROOT/out.json" bash "$GATE" "$root" 2>&1)" || GATE_RC=$?
}

check_exit() {
    local label="$1" expected="$2" actual="$3" needle="$4"
    if [ "$actual" != "$expected" ]; then
        failed=$((failed + 1))
        printf '  FAIL  %s\n        expected exit %s, got %s\n        output: %s\n' \
            "$label" "$expected" "$actual" "$GATE_OUT" >&2
        return
    fi
    # The code alone is not enough. Two different faults share exit 2, and a gate that
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

echo "=== qa-docs.bash exit codes, driven against fixture trees ==="

# --- exit 2: a tree git cannot answer for -------------------------------------------------
#
# The checker raises rather than returning an empty tracked set, because "this repository
# tracks nothing" would turn every link into a finding — a confident verdict from a check
# that did not run. The raise leaves a traceback where JSON belongs, and the payload
# validation refuses it.
no_git="$FIXTURE_ROOT/no-git"
mkdir -p "$no_git/docs"
ln -s "$REPO_ROOT/helpers" "$no_git/helpers"
printf '# Doc\n\n[x](./missing.md)\n' > "$no_git/docs/README.md"
run_gate "$no_git"
check_exit "a tree git cannot answer for" 2 "$GATE_RC" "link_check"

# --- exit 2: zero in-scope documents -------------------------------------------------------
#
# A gate that scanned NOTHING must never report a pass. This is also what keeps the root
# argument safe: a mistyped root finds no documents and is refused, rather than quietly
# passing on an empty tree.
empty="$FIXTURE_ROOT/empty"
mkdir -p "$empty"
ln -s "$REPO_ROOT/helpers" "$empty/helpers"
git init -q "$empty"
run_gate "$empty"
check_exit "a tree with no in-scope documents" 2 "$GATE_RC" "0 in-scope"

# --- exit 1: a real finding ----------------------------------------------------------------
#
# The ordinary failing path, asserted here so the three codes are distinguished by the same
# mechanism. A gate that returned 2 for everything would pass both cases above.
#
# The fixture carries every file the checker READS, because a missing one raises and the run
# becomes an exit 2 — which is how this case first failed, and is itself a demonstration that
# the crash path works. The fixture is not arranged to produce exactly one finding: the
# catalogue, topic-index and gate-inventory checks all have something to say about a
# four-file tree. What is asserted is the exit code and that the broken link is named, not a
# finding count that would pin this test to the checker's other rules.
findings="$FIXTURE_ROOT/findings"
mkdir -p "$findings/docs" "$findings/CLAUDE" "$findings/playbooks" "$findings/scripts"
ln -s "$REPO_ROOT/helpers" "$findings/helpers"
printf 'CLAUDE.md\n' > "$findings/CLAUDE.md"
printf '# QA\n' > "$findings/CLAUDE/QA.md"
printf '# no gates here\n' > "$findings/scripts/qa-all.bash"
printf -- '- import_playbook: imports/play-example.yml\n' > "$findings/playbooks/playbook-main.yml"
printf '# Playbooks\n\nplay-example.yml\n' > "$findings/docs/playbooks.md"
printf '# Architecture\n\nplay-example.yml\n' > "$findings/docs/architecture.md"
printf '# Doc\n\n[x](./NoSuchFile.md)\n' > "$findings/docs/README.md"
git init -q "$findings"
git -C "$findings" add -A
run_gate "$findings"
check_exit "a broken link in a tracked document" 1 "$GATE_RC" "does not exist"

# --- exit 1 is a FINDING, not a crash ------------------------------------------------------
#
# The case that pins the distinction the two hops exist for: a failing run must still emit a
# valid payload. If a crash and a finding both produced exit 1 with unreadable output,
# nothing downstream could tell them apart — and the gate above would be asserting a number
# that means two different things.
#
# jq's own complaint is captured and shown rather than discarded, the same convention
# qa-docs.bash's own validation uses.
payload_check=""
if payload_check="$(jq -e 'has("results") and has("vendored")' "$FIXTURE_ROOT/out.json" 2>&1)"; then
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
