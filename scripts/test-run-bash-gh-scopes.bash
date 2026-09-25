#!/usr/bin/env bash
# Unit-test that GitHub scopes are asked for in ONE pass (run.bash, gh-account-setup.bash).
#
# vars/github-required-scopes.yml is the one list of scopes and helpers/github_scopes the one
# judge of what a token covers. What this gate proves is the "one pass" promise the owner
# asked for: a token short of several scopes gets ONE `gh auth refresh` carrying all of them,
# the first login asks for every scope, and a headless run fails once naming everything,
# rather than stopping at the first gap and leaving the next to fail on the re-run.
#
# The functions under test are extracted with awk and sourced, as the other run.bash gates
# do: run.bash provisions on load and must never be sourced whole. `gh` and `git` are stubs
# on PATH. The helper is the real one from this checkout.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"
SETUP="${GH_SETUP_UNDER_TEST:-$REPO_ROOT/scripts/gh-account-setup.bash}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

extract() {
    local file="$1" fn="$2"
    awk -v fn="$fn" '$0 ~ "^" fn "\\(\\) ?\\{" {p=1} p {print} p && /^\}/ {exit}' "$file" >>"$work/fn.bash"
    if ! grep -qE "^${fn}\(\) ?\{" "$work/fn.bash"; then
        echo "FAIL: could not extract ${fn} from ${file}" >&2
        exit 1
    fi
}

: >"$work/fn.bash"
for fn in info success warning error hl_abort fatal gh_scopes_repo_complete gh_scopes_repo gh_request_missing_scopes; do
    extract "$RUN_BASH" "$fn"
done
cp "$work/fn.bash" "$work/run-fn.bash"
: >"$work/fn.bash"
for fn in scopes_cli audit_all_accounts_headless; do
    extract "$SETUP" "$fn"
done
mv "$work/fn.bash" "$work/setup-fn.bash"

RED='' ; GREEN='' ; YELLOW='' ; CYAN='' ; BOLD='' ; NC='' ; CROSS='x' ; CHECK='v' ; ARROW='>' ; INFO='i' ; WARN='!'
RUN_BASH_VERSION='test'
fedora_desktop_https_url="$(awk -F'"' '/^fedora_desktop_https_url=/ { print $2; exit }' "$RUN_BASH")"
export RED GREEN YELLOW CYAN BOLD NC CROSS CHECK ARROW INFO WARN RUN_BASH_VERSION fedora_desktop_https_url

# ── stubs ─────────────────────────────────────────────────────────────────────
# gh: `api -i user` answers with the scopes in $STUB_DIR/granted (or, with GH_TOKEN set, in
# $STUB_DIR/granted-<token>); `auth refresh` records its arguments and grants what
# $STUB_DIR/after-refresh holds; `auth token --user X` hands out token "tok-X".
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
    "api -i")
        if [ -n "${GH_TOKEN:-}" ]; then f="$STUB_DIR/granted-${GH_TOKEN}"; else f="$STUB_DIR/granted"; fi
        printf 'HTTP/2.0 200 OK\r\nX-Oauth-Scopes: %s\r\n\r\n{}\n' "$(cat "$f")"
        ;;
    "auth refresh")
        printf '%s\n' "$*" >>"$STUB_DIR/refresh.log"
        cp "$STUB_DIR/after-refresh" "$STUB_DIR/granted"
        ;;
    "auth token")
        for arg; do last="$arg"; done
        if [ -f "$STUB_DIR/granted-tok-$last" ]; then echo "tok-$last"; else exit 1; fi
        ;;
    *) echo "unexpected gh $*" >&2; exit 99 ;;
esac
STUB
cat >"$work/bin/git" <<'STUB'
#!/usr/bin/env bash
# `git clone [--depth N] <url> <dir>`: a stand-in checkout holding the real scopes file
# and helper.
[ "$1" = clone ] || { echo "unexpected git $*" >&2; exit 99; }
args=("$@")
url="${args[-2]}" dir="${args[-1]}"
mkdir -p "$dir/vars" && cp -r "$REAL_REPO/vars/github-required-scopes.yml" "$dir/vars/" && cp -r "$REAL_REPO/helpers" "$dir/"
printf 'cloned %s into %s\n' "$url" "$dir" >>"$STUB_DIR/clone.log"
STUB
chmod +x "$work/bin/gh" "$work/bin/git"
STUB_DIR="$work/stub"
mkdir -p "$STUB_DIR"
REAL_REPO="$REPO_ROOT"
export STUB_DIR REAL_REPO
PATH="$work/bin:$PATH"
export PATH

# The cases run against a stand-in checkout with a scopes file of their own, so they test
# the one-pass behaviour and not whatever vars/github-required-scopes.yml lists today.
FIXTURE_REPO="$work/fixture-repo"
mkdir -p "$FIXTURE_REPO/vars"
cp -r "$REPO_ROOT/helpers" "$FIXTURE_REPO/"
printf 'github_required_scopes:\n  - gist\n  - project\n  - repo\n  - workflow\n' \
    >"$FIXTURE_REPO/vars/github-required-scopes.yml"
ALL="gist, project, repo, workflow"
SHORT="gist, project"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}
contains() {
    local label="$1" needle="$2" hay="$3"
    if [[ "$hay" == *"$needle"* ]]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        wanted to find: %s\n        in: %s\n' "$label" "$needle" "$hay" >&2
    fi
}

# run_request <headless> — gh_request_missing_scopes in a subshell, as run.bash calls it.
run_request() {
    rm -f "$STUB_DIR/refresh.log"
    OUT="$(HEADLESS="$1" bash -c 'source "$1"; gh_request_missing_scopes "$2" gh' _ "$work/run-fn.bash" "$FIXTURE_REPO" 2>&1)"
    RC=$?
    REFRESHES=""
    if [ -f "$STUB_DIR/refresh.log" ]; then
        REFRESHES="$(cat "$STUB_DIR/refresh.log")"
    fi
}

# scopes_repo_for <home> — gh_scopes_repo's answer, or FAILED and its stderr.
scopes_repo_for() {
    local answer
    if ! answer="$(HOME="$1" bash -c 'source "$1"; gh_scopes_repo' _ "$work/run-fn.bash" 2>"$work/scopes-repo.err")"; then
        answer="FAILED: $(cat "$work/scopes-repo.err")"
    fi
    printf '%s' "$answer"
}

echo "== run.bash: a token that already carries every scope"
printf '%s' "$ALL" >"$STUB_DIR/granted"
run_request false
check "accepted" 0 "$RC"
check "no refresh is asked for" "" "$REFRESHES"

echo "== run.bash: a token short of two scopes"
printf '%s' "$SHORT" >"$STUB_DIR/granted"
printf '%s' "$ALL" >"$STUB_DIR/after-refresh"
run_request false
check "accepted after the refresh" 0 "$RC"
check "exactly one refresh" 1 "$(printf '%s\n' "$REFRESHES" | grep -c 'auth refresh')"
contains "the one refresh asks for both missing scopes" "--scopes repo,workflow" "$REFRESHES"

echo "== run.bash: a refresh that still leaves a scope missing"
printf '%s' "$SHORT" >"$STUB_DIR/granted"
printf '%s' "gist, project, repo" >"$STUB_DIR/after-refresh"
run_request false
check "refused" 1 "$RC"
contains "names what is still missing" "workflow" "$OUT"

echo "== run.bash: headless, short of two scopes"
printf '%s' "$SHORT" >"$STUB_DIR/granted"
run_request true
check "refused" 1 "$RC"
check "no browser flow is started" "" "$REFRESHES"
contains "names every missing scope at once" "repo,workflow" "$OUT"

echo "== run.bash: the first login asks for every scope"
login_line="$(grep -nE '^[[:space:]]*if ! gh auth login' "$RUN_BASH" | grep -v -- '--with-token')"
contains "the interactive gh auth login carries --scopes" "--scopes" "$login_line"
check "no private scope table remains (ghCheckTokenPermission)" "" "$(grep -n 'ghCheckTokenPermission' "$RUN_BASH")"

echo "== run.bash: where the scope list comes from"
fake_home="$work/home-with-checkout"
mkdir -p "$fake_home/Projects/fedora-desktop/vars" "$fake_home/Projects/fedora-desktop/helpers"
cp "$REPO_ROOT/vars/github-required-scopes.yml" "$fake_home/Projects/fedora-desktop/vars/"
cp -r "$REPO_ROOT/helpers/github_scopes" "$fake_home/Projects/fedora-desktop/helpers/"
rm -f "$STUB_DIR/clone.log"
check "an existing checkout with the helper is used" "$fake_home/Projects/fedora-desktop" "$(scopes_repo_for "$fake_home")"
check "and nothing is cloned" "" "$(cat "$STUB_DIR/clone.log" 2>/dev/null)"
old_home="$work/home-with-old-checkout"
mkdir -p "$old_home/Projects/fedora-desktop/vars"
cp "$REPO_ROOT/vars/github-required-scopes.yml" "$old_home/Projects/fedora-desktop/vars/"
check "a checkout from before the helper is not used; a fresh clone in the cache is" \
    "$old_home/.cache/fedora-desktop-scopes" "$(XDG_CACHE_HOME="" scopes_repo_for "$old_home")"
contains "that clone is the public HTTPS URL, into the cache" \
    "cloned https://github.com/LongTermSupport/fedora-desktop.git into $old_home/.cache/fedora-desktop-scopes" \
    "$(cat "$STUB_DIR/clone.log" 2>/dev/null)"
check "and the old checkout is left for the repository step to pull" "absent" \
    "$([ -e "$old_home/Projects/fedora-desktop/helpers" ] && echo present || echo absent)"
empty_home="$work/home-empty"
mkdir -p "$empty_home"
rm -f "$STUB_DIR/clone.log"
check "with no checkout, it is cloned and used" "$empty_home/Projects/fedora-desktop" "$(scopes_repo_for "$empty_home")"
contains "the clone is the public HTTPS URL" "cloned https://github.com/LongTermSupport/fedora-desktop.git" "$(cat "$STUB_DIR/clone.log" 2>/dev/null)"

echo "== gh-account-setup.bash: headless audits every account before failing"
printf '%s' "$ALL" >"$STUB_DIR/granted-tok-alice"
printf '%s' "$SHORT" >"$STUB_DIR/granted-tok-bob"
printf '%s' "gist" >"$STUB_DIR/granted-tok-carol"
SCOPES_FILE="$FIXTURE_REPO/vars/github-required-scopes.yml"
export REPO_ROOT SCOPES_FILE
OUT="$(bash -c 'source "$1"; REQUIRED_SCOPES_CSV=all; audit_all_accounts_headless a:alice b:bob c:carol' _ "$work/setup-fn.bash" 2>&1)"
RC=$?
check "refused" 1 "$RC"
contains "names the first short account and its scopes" "MISSING bob repo,workflow" "$OUT"
contains "and the next one, in the same failure" "MISSING carol" "$OUT"
check "a healthy account is not listed" "" "$(printf '%s\n' "$OUT" | grep 'alice')"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
