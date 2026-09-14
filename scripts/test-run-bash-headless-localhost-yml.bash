#!/usr/bin/env bash
# Unit-test hl_write_localhost_yml (run.bash, Plan 00119, RUN_BASH_VERSION 1.20.0).
#
# Extracts the ONE function under test out of run.bash with awk into a temp file and
# sources that — run.bash itself refuses to be sourced (it provisions on load), and this
# test must never start provisioning. The function is bounded by its `hl_write_localhost_yml() {`
# line and the first `^}` after it; a refactor that moves the function keeps working, one
# that renames it fails this test loudly at extraction.
#
# WHY THIS TEST EXISTS. Headless provisioning had no way to declare the always-on
# ssh.github.com:443 route; on a box whose egress blocks port 22 the key upload (HTTPS)
# succeeded and every later SSH use of the key hung. RUN_BASH_GITHUB_SSH_443=1 writes
# `github_ssh_over_443: true` into the fresh localhost.yml. Three things are asserted:
# the flag on writes the line, the flag off does not, and an already-configured file is
# kept untouched either way (the idempotency the function has always promised).
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the
# full picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="$REPO_ROOT/run.bash"

if [ ! -f "$RUN_BASH" ]; then
    echo "FAIL: run.bash not found at $RUN_BASH" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The function under test and the two renderers it composes, each bounded by its own
# `name() {` … `}` at column 0.
: > "$work/fn.bash"
for fn in hl_render_github_block hl_strip_github_block hl_write_localhost_yml; do
    awk -v fn="$fn" '$0 == fn "() {" {p=1} p {print} p && /^\}/ {exit}' "$RUN_BASH" >> "$work/fn.bash"
    if ! grep -q "^${fn}() {" "$work/fn.bash"; then
        echo "FAIL: could not extract ${fn} from run.bash" >&2
        exit 1
    fi
done

# The function's collaborators, stubbed: messages are noise here, and the config-repo
# import path is not under test.
info() { :; }
success() { :; }
error() { echo "ERROR: $*" >&2; }
hl_pull_config_source() { echo "FAIL: hl_pull_config_source must not be called" >&2; return 1; }
# The extract is generated at run time, so there is no tracked path for shellcheck to follow.
# shellcheck source=/dev/null
source "$work/fn.bash"

if ! declare -F hl_write_localhost_yml >/dev/null; then
    echo "FAIL: hl_write_localhost_yml is not defined after sourcing the extract" >&2
    exit 1
fi

# Read by the extracted function, so exported for shellcheck's benefit — nothing else sees them.
export HL_USER_LOGIN="tester"
export HL_USER_NAME="Test User"
export HL_USER_EMAIL="tester@example.com"
export RUN_BASH_CONFIG_SOURCE="none"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

echo "=== hl_write_localhost_yml ==="

yml="$work/on.yml"
HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="1" hl_write_localhost_yml "$yml"
check "flag 1: github_ssh_over_443 declared true" "1" "$(grep -c '^github_ssh_over_443: true$' "$yml")"
check "flag 1: the account alias is written" "1" "$(grep -c '^  bot: "example-bot"$' "$yml")"

yml="$work/off.yml"
HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="0" hl_write_localhost_yml "$yml"
check "flag 0: no github_ssh_over_443 line" "0" "$(grep -c 'github_ssh_over_443' "$yml")"

yml="$work/unset.yml"
HL_GITHUB_ACCOUNTS="example-bot" hl_write_localhost_yml "$yml"
check "flag unset: no github_ssh_over_443 line" "0" "$(grep -c 'github_ssh_over_443' "$yml")"
check "flag unset: bare login gets the personal alias" "1" "$(grep -c '^  personal: "example-bot"$' "$yml")"

yml="$work/none.yml"
HL_GITHUB_ACCOUNTS="none" HL_GITHUB_SSH_443="0" hl_write_localhost_yml "$yml"
check "accounts none: empty map, no 443 line" "1" "$(grep -c '^github_accounts: {}$' "$yml")"
check "accounts none: no github_ssh_over_443 line" "0" "$(grep -c 'github_ssh_over_443' "$yml")"

echo "=== reconcile of an existing file (RUN_BASH_CONFIG_SOURCE=none) ==="

# A box first provisioned with no account, then declared an account: the GitHub half is
# rewritten to the inputs; identity and a vaulted value survive untouched.
yml="$work/reconcile.yml"
{
    printf 'user_login: "kept"\n'
    printf '# No GitHub identity configured (RUN_BASH_GITHUB_ACCOUNTS=none). To add one later:\n'
    printf '# scripts/gh-account-setup.bash --add=alias:username\n'
    printf 'github_accounts: {}\n\n'
    printf 'github_ssh_passphrase: !vault |\n'
    printf '          %sANSIBLE_VAULT;1.1;AES256\n' '$'
    printf '          6162636465\n'
} > "$yml"
HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="1" hl_write_localhost_yml "$yml"
check "reconcile: the declared account replaces the empty map" "1" "$(grep -c '^  bot: "example-bot"$' "$yml")"
check "reconcile: the empty map is gone" "0" "$(grep -c '^github_accounts: {}$' "$yml")"
check "reconcile: the none-path comment is gone" "0" "$(grep -c 'RUN_BASH_GITHUB_ACCOUNTS=none' "$yml")"
check "reconcile: the 443 flag is declared" "1" "$(grep -c '^github_ssh_over_443: true$' "$yml")"
check "reconcile: identity preserved" "1" "$(grep -c '^user_login: "kept"$' "$yml")"
check "reconcile: the vaulted value preserved (both lines)" "2" "$(grep -c -E '^(github_ssh_passphrase: !vault \|| +6162636465)$' "$yml")"

# Second run with the same inputs: byte-identical (the reconcile is idempotent).
before=$(sha256sum "$yml" | cut -d' ' -f1)
HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="1" hl_write_localhost_yml "$yml"
after=$(sha256sum "$yml" | cut -d' ' -f1)
check "reconcile: a second run with the same inputs is byte-identical" "$before" "$after"

# Flag flipped off on a later run: the 443 line is removed, the account stays.
HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="0" hl_write_localhost_yml "$yml"
check "reconcile: flag 0 removes the 443 line" "0" "$(grep -c 'github_ssh_over_443' "$yml")"
check "reconcile: flag 0 keeps the account" "1" "$(grep -c '^  bot: "example-bot"$' "$yml")"

# Account changed on a later run: the old alias is replaced, not accumulated.
HL_GITHUB_ACCOUNTS="other:other-user" HL_GITHUB_SSH_443="0" hl_write_localhost_yml "$yml"
check "reconcile: a changed account replaces the old alias" "0" "$(grep -c 'example-bot' "$yml")"
check "reconcile: a changed account is declared" "1" "$(grep -c '^  other: "other-user"$' "$yml")"
check "reconcile: exactly one github_accounts key" "1" "$(grep -c '^github_accounts:' "$yml")"

# With a config source declared, an already-configured file is kept byte-identical.
yml="$work/kept.yml"
printf 'user_login: "kept"\ngithub_accounts:\n  old: "old-user"\n' > "$yml"
before=$(sha256sum "$yml" | cut -d' ' -f1)
RUN_BASH_CONFIG_SOURCE="my-host" HL_GITHUB_ACCOUNTS="bot:example-bot" HL_GITHUB_SSH_443="1" hl_write_localhost_yml "$yml"
after=$(sha256sum "$yml" | cut -d' ' -f1)
check "config source set: an already-configured file is kept byte-identical" "$before" "$after"

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
