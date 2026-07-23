#!/usr/bin/env bash
#
# Plan 00063 — headless run.bash acceptance tests.
#
# Exercises the HEADLESS PREFLIGHT fail-fast gates (Plan 00063 slice 2) as a
# NON-root user, asserting each missing/unsafe input aborts with exit 1 and the
# expected message — proving the "never hang, fail fast, name the fix" contract
# (CLAUDE/InteractiveScripts.md rule 11) without needing a host.
#
# Safe by design: every case aborts in preflight BEFORE any provisioning action
# (no dnf, no clone, no ansible). Runs the interpreter as user `nobody` so the
# preflight non-root check passes and the config/secret gates are reachable.
#
# The full end-to-end unattended provision (gh token auth, ssh clone, ansible run)
# is HOST-only (Phase 3) and is NOT covered here.
#
# Usage:  ./CLAUDE/Plan/00063-headless-run-bash-server-cloud-provisioning/acceptance.bash
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

echo "== Plan 00063 headless preflight acceptance (as $DROP_USER) =="

# Pre-req: we can drop to the unprivileged user (capture reason, no error-hiding).
if ! _probe="$(drop_run true 2>&1)"; then
  echo -e "${Y}SKIP${N} cannot drop to $DROP_USER here (${_probe:-unknown}) — host-run these tests."
  exit 0
fi

# Readable + unreadable secret files for the file-based gate tests.
readable_secret="$(mktemp)"; printf 'sekret\n' >"$readable_secret"; chmod 0644 "$readable_secret"
unreadable_secret="$(mktemp)"; printf 'sekret\n' >"$unreadable_secret"; chmod 0600 "$unreadable_secret"
trap 'rm -f "$readable_secret" "$unreadable_secret"' EXIT

# 1. Missing email.
expect_fail "missing USER_EMAIL" "RUN_BASH_USER_EMAIL is required"

# 2. Bad email.
expect_fail "bad USER_EMAIL" "is not a valid email" \
  RUN_BASH_USER_EMAIL=notanemail

# 3. Missing GitHub accounts.
expect_fail "missing GITHUB_ACCOUNTS" "RUN_BASH_GITHUB_ACCOUNTS is required" \
  RUN_BASH_USER_EMAIL=name@example.com

# 4. GitHub-empty ('none') path is deferred in v1 — must fail fast, not provision.
expect_fail "github=none deferred in v1" "not supported in headless v1" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=none

# 5. Multiple GitHub accounts (v1 single-account).
expect_fail "multiple GITHUB_ACCOUNTS" "Multiple GitHub accounts" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice,bob

# 6. GitHub account set but no token file (v1 requires GitHub configured).
expect_fail "missing GITHUB_TOKEN_FILE" "RUN_BASH_GITHUB_TOKEN_FILE is required" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice

# 7. Both literal + file forms for the vault secret (resolved before the token gate).
expect_fail "both vault forms set" "Both RUN_BASH_VAULT_PASSWORD_FILE" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice \
  RUN_BASH_VAULT_PASSWORD=lit RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

# 8. Vault *_FILE set but unreadable — must NOT fall back.
expect_fail "unreadable vault file" "is not a readable file" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice \
  RUN_BASH_VAULT_PASSWORD_FILE="$unreadable_secret"

# 9. Account + token but no SSH passphrase — the login key must stay passphrase-
#    protected (Decision 6), so the passphrase file is required in v1.
expect_fail "missing SSH passphrase" "RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE is required" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice \
  RUN_BASH_GITHUB_TOKEN_FILE="$readable_secret"

# 10. All config valid (single account, token + vault + ssh passphrase via readable
#     files) reaches the LAST gate, the NOPASSWD-sudo probe, which fails for the
#     unprivileged user — proving the happy config path resolves and the sudo
#     precondition is enforced.
expect_fail "valid config hits NOPASSWD gate" "NOPASSWD" \
  RUN_BASH_USER_EMAIL=name@example.com RUN_BASH_GITHUB_ACCOUNTS=alice \
  RUN_BASH_GITHUB_TOKEN_FILE="$readable_secret" \
  RUN_BASH_GITHUB_SSH_PASSPHRASE_FILE="$readable_secret" \
  RUN_BASH_VAULT_PASSWORD_FILE="$readable_secret"

echo
echo "== $pass passed, $fail failed =="
[ "$fail" -eq 0 ]
