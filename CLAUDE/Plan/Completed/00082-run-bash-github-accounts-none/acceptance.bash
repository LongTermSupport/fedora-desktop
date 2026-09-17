#!/usr/bin/env bash
#
# Plan 00082 — RUN_BASH_GITHUB_ACCOUNTS=none acceptance tests.
#
# Exercises the HEADLESS PREFLIGHT gates this plan added/changed, as a
# NON-root user, asserting each case aborts (or proceeds past preflight) with
# the expected message — proving the empty-GitHub path is actually reachable
# and that it rejects the two contradictory configs (RUN_BASH_CONFIG_SOURCE,
# RUN_BASH_RESTORE_PROJECTS=1) without needing a host.
#
# Safe by design: every case aborts in preflight BEFORE any provisioning action
# (no dnf, no clone, no ansible) — the "reaches NOPASSWD gate" cases fail at
# the LAST preflight check (no real sudo for the unprivileged user), not at
# any later execution step.
#
# The full end-to-end empty-path provision (HTTPS clone, ansible run) is
# HOST-only and is NOT covered here — see PLAN.md Phase 5.
#
# Usage:  ./CLAUDE/Plan/00082-run-bash-github-accounts-none/acceptance.bash
set -euo pipefail

REPO_ROOT="$(git -C "$(dirname "${BASH_SOURCE[0]}")" rev-parse --show-toplevel)"
RUN_BASH="$REPO_ROOT/run.bash"
DROP_USER="nobody"

pass=0
fail=0

# Colours only on a tty.
if [ -t 1 ]; then G='\033[0;32m'; R='\033[0;31m'; Y='\033[1;33m'; N='\033[0m'; else G=''; R=''; Y=''; N=''; fi

# Privilege-drop: prefer runuser (works as root without sudo), else sudo.
if [ -n "$(command -v runuser)" ]; then
  drop_run() { runuser -u "$DROP_USER" -- "$@"; }
elif [ -n "$(command -v sudo)" ]; then
  drop_run() { sudo -u "$DROP_USER" -- "$@"; }
else
  drop_run() { return 127; }
fi

# run_headless <VAR=VAL ...> — run run.bash --headless as $DROP_USER with ONLY the
# given RUN_BASH_* env, combined output on stdout; caller checks $?.
run_headless() {
  drop_run env -i PATH="/usr/bin:/bin" HOME=/tmp "$@" \
    bash "$RUN_BASH" --headless 2>&1
}

# expect_fail <name> <expected-substr> <VAR=VAL ...>
expect_fail() {
  local name="$1" want="$2"; shift 2
  local out rc=0
  out="$(run_headless "$@")" || rc=$?
  if [ "$rc" -eq 0 ]; then
    echo -e "${R}FAIL${N} $name — expected non-zero exit, got 0"
    fail=$((fail + 1)); return
  fi
  if grep -qF "$want" <<<"$out"; then
    echo -e "${G}PASS${N} $name (exit $rc)"
    pass=$((pass + 1))
  else
    echo -e "${R}FAIL${N} $name — exit $rc but missing message: '$want'"
    echo "    got: $(tr '\n' '|' <<<"$out")"
    fail=$((fail + 1))
  fi
}

# expect_not <name> <forbidden-substr> <VAR=VAL ...> — the case must fail (as
# always in preflight-only testing, exit non-zero), but must NOT fail for the
# forbidden reason.
expect_not() {
  local name="$1" forbidden="$2"; shift 2
  local out rc=0
  out="$(run_headless "$@")" || rc=$?
  if [ "$rc" -eq 0 ]; then
    echo -e "${R}FAIL${N} $name — expected non-zero exit, got 0"
    fail=$((fail + 1)); return
  fi
  if grep -qF "$forbidden" <<<"$out"; then
    echo -e "${R}FAIL${N} $name — hit the forbidden message: '$forbidden'"
    echo "    got: $(tr '\n' '|' <<<"$out")"
    fail=$((fail + 1))
  else
    echo -e "${G}PASS${N} $name (exit $rc, did not hit '$forbidden')"
    pass=$((pass + 1))
  fi
}

echo "== Plan 00082 GITHUB_ACCOUNTS=none preflight acceptance (as $DROP_USER) =="

# Pre-req: we can drop to the unprivileged user (capture reason, no error-hiding).
if ! _probe="$(drop_run true 2>&1)"; then
  echo -e "${Y}SKIP${N} cannot drop to $DROP_USER here (${_probe:-unknown}) — host-run these tests."
  exit 0
fi

readable_secret="$(mktemp)"; printf 'sekret\n' >"$readable_secret"; chmod 0644 "$readable_secret"
trap 'rm -f "$readable_secret"' EXIT

# 1. 'none' alone (with only the vault password provided) reaches the LAST
#    preflight gate — proving neither GITHUB_TOKEN_FILE nor
#    GITHUB_SSH_PASSPHRASE_FILE is required for the empty path.
expect_fail "none: reaches NOPASSWD gate with no token/passphrase files" "NOPASSWD" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

# 2. 'none' must NOT hit the old "not supported" rejection this plan removed.
expect_not "none: does not hit the old 'not supported' rejection" "not supported in headless v1" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

# 3. 'none' + a real RUN_BASH_CONFIG_SOURCE is contradictory (a config import
#    needs a GitHub identity to pull from) — must fail fast, naming the conflict.
expect_fail "none + CONFIG_SOURCE fails fast" "RUN_BASH_CONFIG_SOURCE" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_CONFIG_SOURCE=hosts/example.yml

# 4. RUN_BASH_CONFIG_SOURCE=none explicitly is NOT contradictory (it is the
#    'no import' default spelled out) — must reach the NOPASSWD gate, not the
#    CONFIG_SOURCE conflict message.
expect_fail "none + CONFIG_SOURCE=none is not a conflict" "NOPASSWD" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_CONFIG_SOURCE=none \
  RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

# 5. 'none' + RUN_BASH_RESTORE_PROJECTS=1 is contradictory (restoring projects
#    needs a GitHub identity to clone them from) — must fail fast.
expect_fail "none + RESTORE_PROJECTS=1 fails fast" "RUN_BASH_RESTORE_PROJECTS" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_RESTORE_PROJECTS=1

# 6. 'none' + RUN_BASH_RESTORE_PROJECTS=0 (the default, spelled out explicitly)
#    is NOT a conflict — must reach the NOPASSWD gate.
expect_fail "none + RESTORE_PROJECTS=0 is not a conflict" "NOPASSWD" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none \
  RUN_BASH_RESTORE_PROJECTS=0 \
  RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

# 7. Regression: a REAL account still requires the token file (unchanged
#    behaviour for the GitHub-configured path).
expect_fail "real account still requires GITHUB_TOKEN_FILE" "RUN_BASH_GITHUB_TOKEN_FILE is required" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice

# 8. Regression: a REAL account still requires the SSH passphrase file
#    (unchanged behaviour for the GitHub-configured path).
expect_fail "real account still requires GITHUB_SSH_PASSPHRASE_FILE" "RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE is required" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice \
  RUN_BASH_GITHUB_TOKEN_FILE="$readable_secret"

echo
echo "== $pass passed, $fail failed =="
[ "$fail" -eq 0 ]
