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

HOOKS_DIR=""
while [ "$#" -gt 0 ]; do
    arg="$1"
    case "$arg" in
        --hooks-dir)
            # Point the checks at a DIFFERENT copy of the hooks. This exists so
            # "each check fails against the unfixed code" is a claim that can be
            # re-run rather than one taken on trust:
            #   mkdir -p /tmp/old && git show HEAD~1:scripts/git-hooks/pre-commit > ...
            #   acceptance.bash --hooks-dir /tmp/old   # expect FAILs
            shift
            if [ "$#" -eq 0 ]; then
                echo "ERROR: --hooks-dir needs a directory" >&2
                exit 1
            fi
            HOOKS_DIR="$1"
            shift
            continue
            ;;
        -h | --help)
            cat <<'EOF'
Usage: acceptance.bash [--hooks-dir DIR] [--help]

Plan 00081 acceptance gate — proves the git-hook secret scanners see:

  pre-commit
   1  a staged rename (git mv + edit), which --diff-filter=ACM excluded
   2  a real address sharing a line with a whitelisted git@ token
   3  ... and still does NOT flag a line whose only match IS whitelisted
      (the fix must not trade a false negative for a false positive)
   4  a plain modified file (the pre-existing path still works)
   5  a real /home/ path sharing a line with a whitelisted one

  commit-msg (which had NO localhost.yml denylist at all)
   6  a private identifier from localhost.yml
   7  ... including one under the PLURAL *_accounts convention
   8  ... while an ordinary commit message still passes
   9  a real address sharing a line with git@github.com

--hooks-dir DIR   run the checks against another copy of the hooks, so
                  "these checks fail against the unfixed code" stays a
                  re-runnable claim:
                    mkdir -p /tmp/old
                    git show <pre-fix-rev>:scripts/git-hooks/pre-commit \
                      > /tmp/old/pre-commit    # + commit-msg, then chmod +x
                    acceptance.bash --hooks-dir /tmp/old     # expect FAILs

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
    shift
done

PLAN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(git -C "$PLAN_DIR" rev-parse --show-toplevel)"
if [ -z "$HOOKS_DIR" ]; then
    HOOKS_DIR="$REPO_ROOT/scripts/git-hooks"
fi
HOOK="$HOOKS_DIR/pre-commit"
MSG_HOOK="$HOOKS_DIR/commit-msg"

mkdir -p "$PLAN_DIR/logs"
LOG="$PLAN_DIR/logs/acceptance.log"
exec > >(tee "$LOG") 2>&1
echo "Logging this run to: $LOG" >&2

for _h in "$HOOK" "$MSG_HOOK"; do
    if [ ! -x "$_h" ]; then
        echo "ERROR: $_h is missing or not executable." >&2
        exit 1
    fi
done

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

echo "### 5. per-token whitelisting also applies to /home/ paths"
# Was a whole-line grep -v: one placeholder home path on the line deleted the
# line, real home path included.
D="$(new_sandbox_repo homepaths)"
echo "cp /home/ansible/x /home/${LEAK_USER//./}/y" >> "$D/doc.md"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "the /home/ansible whitelist deleted the whole line, real path included" ;;
    *) ok "a real home path beside a whitelisted one is still caught" ;;
esac

# --------------------------------------------------------------------------
# commit-msg. Until Plan 00081 this hook ran ONLY the static patterns and then
# printed "✓ Commit message looks clean" — no localhost.yml denylist at all.
# A commit message is the one surface a follow-up commit cannot fix, so the
# weaker of the two checks was guarding the less recoverable surface.
# --------------------------------------------------------------------------

# Denylist identifiers are invented at RUNTIME and written to a throwaway
# localhost.yml inside the sandbox. Nothing real is read, and this tracked file
# never contains a plausible identifier.
DENY_PERSONA="zzqa${$}persona"
DENY_PLURAL="zzqa${$}plural"

seed_localhost_yml() {
    local d="$1"
    mkdir -p "$d/environment/localhost/host_vars"
    cat > "$d/environment/localhost/host_vars/localhost.yml" <<YEOF
user_login: sandbox
project_personas:
  ${DENY_PERSONA}:
    name: "Sandbox Persona"
lastpass_accounts:
  ${DENY_PLURAL}:
    name: "Sandbox Account"
YEOF
}

run_msg_hook() {
    local d="$1" msg="$2" out rc
    printf '%s\n' "$msg" > "$d/.msg"
    if out="$(cd "$d" && "$MSG_HOOK" "$d/.msg" 2>&1)"; then rc=0; else rc=$?; fi
    printf '%s\n---rc=%s\n' "$out" "$rc"
}

echo "### 6. commit-msg rejects a private identifier from localhost.yml"
D="$(new_sandbox_repo msgdeny)"
seed_localhost_yml "$D"
RES="$(run_msg_hook "$D" "Plan 00081: rework the ${DENY_PERSONA} flow")"
case "$RES" in
    *"---rc=0"*) bad "commit-msg accepted a denylisted identifier — it has no denylist" ;;
    *) ok "commit-msg now runs the same denylist as pre-commit" ;;
esac

echo "### 7. the denylist harvests the PLURAL *_accounts convention"
# The harvester matched _account/_username (singular) only, so this repo's own
# convention — github_accounts, project_personas — was reached solely by two
# hardcoded names, and lastpass_accounts by nothing at all.
RES="$(run_msg_hook "$D" "Plan 00081: migrate ${DENY_PLURAL} credentials")"
case "$RES" in
    *"---rc=0"*) bad "a *_accounts field was never harvested into the denylist" ;;
    *) ok "plural identity fields are harvested" ;;
esac

echo "### 8. commit-msg passes an ordinary message (no false positive)"
RES="$(run_msg_hook "$D" "Plan 00081: share the scanner between both git hooks

Refs: CLAUDE/Plan/00081-secret-scanner-and-qa-gate-coverage-holes")"
case "$RES" in
    *"---rc=0"*) ok "a normal commit message still passes" ;;
    *) bad "false positive: an ordinary commit message was rejected
       $RES" ;;
esac

echo "### 9. commit-msg filters per TOKEN, not per line"
RES="$(run_msg_hook "$D" "cloned git@github.com:owner/repo.git for $LEAK")"
case "$RES" in
    *"---rc=0"*) bad "commit-msg dropped the whole line on the git@ whitelist" ;;
    *) ok "a real address beside git@github.com is caught in the message too" ;;
esac

echo "### 10. a broken whitelist filter FAILS rather than returning empty"
# The error branch has to be live. It was not: the first draft read \$? after
# `fi`, which is the status of the if STATEMENT — zero whenever no branch ran —
# so every grep error would have been reported as "nothing matched". Caught by
# this repo's own bash-status-after-block rule, inside the fix for the sibling
# defect class. This check exists so it cannot go dead again.
if [ ! -f "$HOOKS_DIR/lib/secret-scan.bash" ]; then
    bad "no shared lib/secret-scan.bash — the two hooks carry duplicate filters"
elif (
    set -euo pipefail
    # shellcheck source=/dev/null
    source "$HOOKS_DIR/lib/secret-scan.bash"
    # $LEAK is assembled at runtime — see the note at the top of this file.
    printf '1:%s\n' "$LEAK" \
        | hook_keep_unwhitelisted '[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}' '[unclosed'
) 2> /dev/null; then
    bad "a broken whitelist regex returned success — the error branch is dead code"
else
    ok "a broken whitelist regex is a hard failure, not an empty result"
fi

# --------------------------------------------------------------------------
# The CCY version-bump gate. "The CCY script" is seven files: the launcher and
# the six libraries it sources, which together are the larger half. The gate
# and the runtime hash both keyed on the launcher alone, so 22 commits shipped
# lib-only behaviour changes with no bump required and none made.
# --------------------------------------------------------------------------

CCY_PATH="files/var/local/claude-yolo/claude-yolo"
CCY_LIB_PATH="files/var/local/claude-yolo/lib/token-management.bash"

new_ccy_repo() {
    local d="$SANDBOX/$1"
    mkdir -p "$d/files/var/local/claude-yolo/lib"
    git -C "$d" init -q .
    git -C "$d" config user.email "test@example.com"
    git -C "$d" config user.name "test"
    {
        echo '#!/bin/bash'
        echo 'CCY_VERSION="1.0.0"  # baseline'
        echo 'echo hello'
    } > "$d/$CCY_PATH"
    {
        echo '#!/bin/bash'
        echo 'lib_function() { echo original; }'
    } > "$d/$CCY_LIB_PATH"
    git -C "$d" add -A
    git -C "$d" commit -qm init
    printf '%s' "$d"
}

echo "### 11. a lib-only change with no version bump is REJECTED"
D="$(new_ccy_repo ccylib)"
echo 'lib_function() { echo CHANGED; }' >> "$D/$CCY_LIB_PATH"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "a lib/ behaviour change shipped with no version bump" ;;
    *) ok "lib/ is inside the version-bump gate" ;;
esac

echo "### 12. a lib change WITH a launcher bump is accepted"
D="$(new_ccy_repo ccybump)"
echo 'lib_function() { echo CHANGED; }' >> "$D/$CCY_LIB_PATH"
if ! python3 - "$D/$CCY_PATH" <<'PYEOF'
import sys
p = sys.argv[1]
with open(p) as fh:
    text = fh.read()
with open(p, "w") as fh:
    fh.write(text.replace('CCY_VERSION="1.0.0"', 'CCY_VERSION="1.1.0"'))
PYEOF
then
    echo "ERROR: could not rewrite the sandbox version line" >&2
    exit 1
fi
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) ok "a bumped lib change passes — the gate is not a blanket block" ;;
    *) bad "false positive: a properly bumped lib change was rejected
       $RES" ;;
esac

echo "### 13. a launcher-only change with no bump is still rejected"
D="$(new_ccy_repo ccylauncher)"
echo 'echo goodbye' >> "$D/$CCY_PATH"
git -C "$D" add -A
RES="$(run_hook "$D")"
case "$RES" in
    *"---rc=0"*) bad "the pre-existing launcher check regressed" ;;
    *) ok "the pre-existing launcher check still holds" ;;
esac

echo
echo "=============================================================="
echo "COVERAGE: 13 check(s) over 7 fixed defects, 12 driving a REAL hook"
echo "  in a throwaway repo — 8 against pre-commit, 4 against commit-msg, 1"
echo "  against the shared library. Measured, not asserted (--hooks-dir"
echo "  against the pre-plan hooks at 0369468b~1): 8 of these 13 FAIL there"
echo "  — 1, 2, 5, 6, 7, 9, 10, 11. Checks 3, 8, 12 and 13 pass in both:"
echo "  they exist to stop the fixes over-correcting into false positives"
echo "  or regressing what already worked, and that is worth saying."
echo "--------------------------------------------------------------"
if [ "$FAIL" -eq 0 ]; then
    echo "VERDICT: PASS — $PASS check(s) passed."
else
    echo "VERDICT: FAIL — $FAIL of $((PASS + FAIL)) check(s) failed."
fi
echo "=============================================================="

[ "$FAIL" -eq 0 ]
