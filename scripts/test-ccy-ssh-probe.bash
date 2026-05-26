#!/usr/bin/bash
# Diagnose ccy gh-token SSH-key probe end-to-end on the host.
#
# Sources ssh-handling.bash directly from this repo (NOT the deployed
# /var/local/claude-yolo/lib copy) so you can verify a fix before running
# the playbook. Run from anywhere; pass a git repo path as $1 (defaults to
# this repo's root).
#
# Each check is sequential and prints PASS/FAIL with diagnostic info on
# the first failure. Exits non-zero on the first FAIL.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/var/local/claude-yolo/lib/ssh-handling.bash"

REPO_PATH="${1:-$REPO_ROOT}"

TMP_ERR=$(mktemp)
trap 'rm -f "$TMP_ERR"' EXIT

pass() { echo "  PASS: $*"; }
fail() { echo "  FAIL: $*"; exit 1; }
hdr()  { echo ""; echo "=== $* ==="; }

hdr "0. Library sourceable from repo"
if [ ! -f "$LIB" ]; then
    fail "lib not found at $LIB"
fi
# shellcheck source=/dev/null
source "$LIB"
pass "$LIB"

hdr "1. Detect remote URL for $REPO_PATH"
url=$(get_project_remote_url "$REPO_PATH")
if [ -z "$url" ]; then
    fail "no remote URL — is $REPO_PATH a git repo with a remote?"
fi
pass "$url"

hdr "2. Parse owner/repo from URL"
# parse_github_owner_repo returns 1 on no match (no -e set, so the var
# assignment proceeds and we check the value).
owner_repo=$(parse_github_owner_repo "$url")
if [ -z "$owner_repo" ]; then
    fail "could not parse $url (not a recognised github URL form?)"
fi
pass "$owner_repo"

hdr "3. github_* SSH keys present in ~/.ssh/"
mapfile -t KEYS < <(find "$HOME/.ssh" -type f -name 'github_*' ! -name '*.pub' 2>"$TMP_ERR" | sort)
if [ ${#KEYS[@]} -eq 0 ]; then
    if [ -s "$TMP_ERR" ]; then
        echo "    find stderr:"
        while IFS= read -r line; do echo "      $line"; done < "$TMP_ERR"
    fi
    fail "no ~/.ssh/github_<alias> keys found — run play-github-cli-multi.yml"
fi
for k in "${KEYS[@]}"; do echo "    $k"; done
pass "${#KEYS[@]} key(s)"

hdr "4. gh-aliases.inc.bash sourceable"
GHALIAS="$HOME/.bashrc-includes/gh-aliases.inc.bash"
if [ ! -f "$GHALIAS" ]; then
    fail "$GHALIAS not found — run play-github-cli-multi.yml"
fi
# shellcheck source=/dev/null
source "$GHALIAS"
pass "$GHALIAS"

hdr "5. gh-token-<alias> functions defined"
missing=()
aliases=()
for k in "${KEYS[@]}"; do
    bn=$(basename "$k")
    if [[ "$bn" =~ ^github_(.+)$ ]]; then
        alias_name="${BASH_REMATCH[1]}"
        aliases+=("$alias_name")
        if ! type -t "gh-token-${alias_name}" >/dev/null; then
            missing+=("gh-token-${alias_name}")
        fi
    fi
done
if [ ${#missing[@]} -gt 0 ]; then
    fail "missing functions: ${missing[*]} — re-run play-github-cli-multi.yml"
fi
pass "all ${#aliases[@]} gh-token-<alias> functions defined"

hdr "6. Each gh-token-<alias> returns a non-empty token"
empty=()
for a in "${aliases[@]}"; do
    : > "$TMP_ERR"
    tok=$("gh-token-${a}" 2>"$TMP_ERR")
    if [ -z "$tok" ]; then
        empty+=("$a")
        echo "    EMPTY: $a"
        if [ -s "$TMP_ERR" ]; then
            while IFS= read -r line; do echo "      stderr: $line"; done < "$TMP_ERR"
        fi
    else
        echo "    ok:    $a (${#tok} bytes)"
    fi
done
if [ ${#empty[@]} -gt 0 ]; then
    fail "empty token from: ${empty[*]} — likely 'gh auth switch' / not logged in"
fi
pass "${#aliases[@]} token(s) returned"

hdr "7. Full probe: probe_gh_keys_for_remote (checks .permissions.push)"
working=$(probe_gh_keys_for_remote "$url")
echo "  Probe log dir: ${PROBE_LOG_DIR:-<unset>}"
if [ -z "$working" ]; then
    echo "  FAIL: zero keys with push access to $owner_repo"
    if [ -n "${PROBE_LOG_DIR:-}" ] && [ -d "$PROBE_LOG_DIR" ]; then
        echo ""
        echo "  --- diagnostic logs ---"
        for f in "$PROBE_LOG_DIR"/*; do
            [ -e "$f" ] || continue
            echo ""
            echo "  ### $(basename "$f")"
            while IFS= read -r line; do echo "    $line"; done < "$f"
        done
    fi
    exit 1
fi
echo "$working" | while IFS= read -r line; do echo "    match: $line"; done
match_count=$(echo "$working" | grep -c .)
pass "$match_count key(s) have push access to $owner_repo"

echo ""
echo "ALL CHECKS PASSED — probe is functional on this host."
