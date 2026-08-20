#!/usr/bin/env bash
#
# Plan 00081 — acceptance gate for the pre-commit secret-scanner fixes.
#
# Renders a PASS/FAIL verdict (triage gathers facts and renders none — see
# CLAUDE/PlanTriage.md).
#
# WHAT IT EXERCISES:
#
# Every check drives the REAL hook (scripts/git-hooks/pre-commit) inside a
# THROWAWAY git repository under $TMPDIR, never this repo. Nothing here stages,
# commits or mutates anything in the working tree. The hook is copied in rather
# than symlinked so a failing run cannot leave the source repo's hooksPath
# pointing at a scratch directory.
#
# The two defects under test both PASSED SILENTLY before the fix, so each check
# is written to fail loudly if the fix is ever reverted:
#
#   H1  a `git mv` + edit was classified R by rename detection and excluded by
#       --diff-filter=ACM, so the file was never scanned at all
#   H2  the email whitelist filtered whole LINES, so a real address sharing a
#       line with git@github.com was deleted along with it
#
# NOTE ON TEST DATA: the "leak" strings below must look real enough to trip the
# scanner but must never be a real address or host. They use the RFC 2606
# reserved .invalid / .test TLDs, which no one can own — and which the scanner
# itself whitelists, so each leak token is built at RUNTIME from fragments to
# defeat that whitelist without ever writing a plausible real address into this
# tracked file.
set -euo pipefail

for arg in "$@"; do
    case "$arg" in
        -h | --help)
            cat <<'EOF'
Usage: acceptance.bash [--help]

Plan 00081 acceptance gate — proves the pre-commit secret scanner sees:

   1  a staged rename (git mv + edit), which --diff-filter=ACM excluded
   2  a real address sharing a line with a whitelisted git@ token
   3  ... and still does NOT flag a line whose only match IS whitelisted
      (the fix must not trade a false negative for a false positive)
   4  a plain modified file (the pre-existing path still works)

Runs entirely in a throwaway git repo under $TMPDIR. Touches nothing in this
repository. Safe to run anywhere, including inside a CCY container.

Writes its log to this plan's logs/acceptance.log.
EOF
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument: $arg" >&2
            echo "  Try: acceptance.bash --help" >&2
            exit 1
            ;;
    esac
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
HOOK="$REPO_ROOT/scripts/git-hooks/pre-commit"

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/acceptance.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

if [ ! -x "$HOOK" ]; then
    echo "ERROR: $HOOK is missing or not executable." >&2
    exit 1
fi

PASS=0
FAIL=0
ok() {
    echo "  OK — $*"
    PASS=$((PASS + 1))
}
bad() {
    echo "  FAIL — $*"
    FAIL=$((FAIL + 1))
}

SANDBOX="$(mktemp -d)"
cleanup() { rm -rf "$SANDBOX"; }
trap cleanup EXIT

# Built at runtime so this tracked file never contains a plausible address.
# `.invalid` is RFC 2606 reserved, so it can never name a real host — but the
# scanner whitelists that TLD, so the local part is joined to a domain whose
# TLD is not on the reserved list to make it look real to the pattern.
LEAK_USER="alice.smith"
LEAK_DOMAIN="notarealcorp-$$"
LEAK_TLD="co.uk"
LEAK="${LEAK_USER}@${LEAK_DOMAIN}.${LEAK_TLD}"

echo "=============================================================="
echo "Plan 00081 — acceptance: pre-commit secret scanner coverage"
echo "=============================================================="
echo

new_sandbox_repo() {
    local d="$SANDBOX/$1"
    mkdir -p "$d"
    git -C "$d" init -q .
    git -C "$d" config user.email "test@example.com"
    git -C "$d" config user.name "test"
    # A file big enough for rename detection to fire. Below the similarity
    # threshold git records D+A instead, the add IS scanned, and the check would
    # pass for the wrong reason — which is exactly why this hole went unnoticed.
    local i
    for i in $(seq 1 40); do
        echo "line $i of a document long enough for rename detection" >> "$d/doc.md"
    done
    git -C "$d" add -A
    git -C "$d" commit -qm init
    printf '%s' "$d"
}

# The hook resolves its own repo root, so running it with cwd inside the
# sandbox makes it scan the sandbox's index.
run_hook() {
    local d="$1" out rc
    if out="$(cd "$d" && "$HOOK" 2>&1)"; then rc=0; else rc=$?; fi
    printf '%s\n---rc=%s\n' "$out" "$rc"
}

echo "### 1. a staged rename (git mv + edit) is scanned"
D="$(new_sandbox_repo rename)"
git -C "$D" mv doc.md moved.md
echo "contact $LEAK" >> "$D/moved.md"
git -C "$D" add -A
echo "  (git sees: $(git -C "$D" diff --cached --name-status | tr '\n' ' '))"
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "the hook PASSED a renamed file carrying an address — ACM excluded it" ;;
    *) ok "the rename was scanned and rejected" ;;
esac

echo "### 2. a real address sharing a line with git@ is still caught"
D="$(new_sandbox_repo shared)"
echo "git clone git@github.com:owner/repo.git  # ask $LEAK" >> "$D/doc.md"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "the git@ whitelist deleted the whole line, address included" ;;
    *) ok "the whitelisted token no longer shields the rest of its line" ;;
esac

echo "### 3. a line whose ONLY match is whitelisted is NOT flagged"
# The fix must not trade a false negative for a false positive: per-token
# filtering has to keep accepting the legitimate shapes this repo is full of.
D="$(new_sandbox_repo clean)"
{
    echo "git clone git@github.com:owner/repo.git"
    echo "systemctl status ftp-camera@cam1.service"
    echo "contact admin@host.example.com"
    echo "owner: <someone@somewhere>"
    echo "email: {{ user_email }}"
} >> "$D/doc.md"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) ok "legitimate git@ / systemd / example.com / placeholder lines pass" ;;
    *) bad "false positive: the fix rejected a file with no real address
       $RES" ;;
esac

echo "### 4. a plain modified file is still scanned (no regression)"
D="$(new_sandbox_repo modified)"
echo "contact $LEAK" >> "$D/doc.md"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "a plain modification carrying an address was passed" ;;
    *) ok "the pre-existing modified-file path still rejects" ;;
esac

echo
echo "=============================================================="
echo "COVERAGE: 4 check(s) over 2 fixed defects, each driving the real hook"
echo "  in a throwaway repo. Checks 1 and 2 FAIL against the pre-fix hook;"
echo "  check 3 is what stops the fix over-correcting."
echo "--------------------------------------------------------------"
if [ "$FAIL" -eq 0 ]; then
    echo "VERDICT: PASS — $PASS check(s) passed."
else
    echo "VERDICT: FAIL — $FAIL of $((PASS + FAIL)) check(s) failed."
fi
echo "=============================================================="

[ "$FAIL" -eq 0 ]
