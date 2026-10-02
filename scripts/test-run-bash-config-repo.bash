#!/usr/bin/env bash
# Unit-test that run.bash reaches the config repo as the PRIMARY account (run.bash).
#
# <primary>/fedora-desktop-config is private, so only the primary account can see it. gh's
# active account is not trusted to still be the primary by the time the config step runs:
# the gh-<alias> wrappers switch account and then switch to the SAVED default, not back to
# whatever was active. A config-repo call that rode on the active account got a 404 and was
# reported as "No config repo found" for a repo that exists. So every config-repo call
# carries the primary's own token, and a failed existence check that is not a 404 stops the
# run with gh's own words instead of being read as "not there".
#
# The functions under test are extracted with awk and sourced, as the other run.bash gates
# do: run.bash provisions on load and must never be sourced whole. `gh` is a stub on PATH.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
RUN_BASH="${RUN_BASH_UNDER_TEST:-$REPO_ROOT/run.bash}"

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
for fn in info success warning error hl_abort fatal gh_primary config_repo_exists push_config_to_repo; do
    extract "$RUN_BASH" "$fn"
done

RED='' ; GREEN='' ; YELLOW='' ; CYAN='' ; BOLD='' ; NC='' ; CROSS='x' ; CHECK='v' ; ARROW='>' ; INFO='i' ; WARN='!'
export RED GREEN YELLOW CYAN BOLD NC CROSS CHECK ARROW INFO WARN

# ── stubs ─────────────────────────────────────────────────────────────────────
# gh: `auth token --user X` hands out "tok-X" unless $STUB_DIR/no-token exists. `api repos/O/…`
# answers only when GH_TOKEN is "tok-O" (the repo is private to its owner); any other caller
# gets gh's real 404 wording. $STUB_DIR/api-error makes every api call fail with its content
# instead, as an expired token or a network fault does. Every api call is logged with the
# token it carried.
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
    "auth token")
        for arg; do last="$arg"; done
        if [ -f "$STUB_DIR/no-token" ]; then echo "no oauth token found for $last" >&2; exit 1; fi
        echo "tok-$last"
        ;;
    "api repos/"*)
        printf '%s %s\n' "${GH_TOKEN:-<none>}" "$*" >>"$STUB_DIR/api.log"
        if [ -f "$STUB_DIR/api-error" ]; then cat "$STUB_DIR/api-error" >&2; exit 1; fi
        owner="${2#repos/}"
        owner="${owner%%/*}"
        if [ "${GH_TOKEN:-}" != "tok-$owner" ]; then
            echo "gh: Not Found (HTTP 404)" >&2
            exit 1
        fi
        case "$*" in
            *"--method PUT"*) ;;
            *"/contents/"*) echo "sha-1" ;;
            *) echo "fedora-desktop-config" ;;
        esac
        ;;
    *) echo "unexpected gh $*" >&2; exit 99 ;;
esac
STUB
chmod +x "$work/bin/gh"
STUB_DIR="$work/stub"
mkdir -p "$STUB_DIR"
export STUB_DIR
PATH="$work/bin:$PATH"
export PATH

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
    if printf '%s' "$hay" | grep -qF -- "$needle"; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        wanted to find: %s\n        in: %s\n' "$label" "$needle" "$hay" >&2
    fi
}

reset_stub() {
    rm -f "$STUB_DIR/api.log" "$STUB_DIR/api-error" "$STUB_DIR/no-token"
}

# exists <primary> <repo> — config_repo_exists in a subshell; prints "ok" or "failed", then its
# output. GH_TOKEN starts as another account's, as a leftover would.
exists() {
    local out verdict=ok
    out="$(GH_TOKEN=tok-someone-else primary_gh_username="$1" bash -c 'source "$1"; if config_repo_exists "$2"; then echo found; else echo absent; fi' _ "$work/fn.bash" "$2" 2>&1)" || verdict=failed
    printf '%s\n%s' "$verdict" "$out"
}
first_line() { printf '%s\n' "$1" | awk 'NR==1'; }
last_line() { printf '%s\n' "$1" | awk 'END {print}'; }

echo "== config_repo_exists"
reset_stub
OUT="$(exists alice alice/fedora-desktop-config)"
check "the primary's private repo is found" "found" "$(last_line "$OUT")"
check "  asked with the primary's own token" "tok-alice api repos/alice/fedora-desktop-config --jq .name" \
    "$(cat "$STUB_DIR/api.log")"

reset_stub
OUT="$(exists alice bob/fedora-desktop-config)"
check "a repo GitHub answers 404 for is reported as absent, silently" "ok
absent" "$OUT"

reset_stub
printf 'gh: Bad credentials (HTTP 401)\n' >"$STUB_DIR/api-error"
OUT="$(exists alice alice/fedora-desktop-config)"
check "any other failure stops the run" "failed" "$(first_line "$OUT")"
check "  rather than reading as absent" "no" "$(printf '%s\n' "$OUT" | grep -qxE 'found|absent' && echo yes || echo no)"
contains "  with gh's own words" "Bad credentials (HTTP 401)" "$OUT"
contains "  naming the repo" "github.com/alice/fedora-desktop-config" "$OUT"

reset_stub
touch "$STUB_DIR/no-token"
OUT="$(exists alice alice/fedora-desktop-config)"
check "a primary with no token stops the run" "failed" "$(first_line "$OUT")"
check "  rather than reading as absent" "no" "$(printf '%s\n' "$OUT" | grep -qxE 'found|absent' && echo yes || echo no)"
contains "  saying whose token is missing" "no oauth token found for alice" "$OUT"
check "  and never calls the API with someone else's token" "no" \
    "$([ -s "$STUB_DIR/api.log" ] && echo yes || echo no)"

echo "== push_config_to_repo"
reset_stub
printf 'x: 1\n' >"$work/local.yml"
push_verdict=pushed
GH_TOKEN=tok-someone-else primary_gh_username=alice bash -c 'source "$1"; push_config_to_repo "$2" alice/fedora-desktop-config hosts/h.yml h' _ "$work/fn.bash" "$work/local.yml" >"$work/push.out" 2>&1 || push_verdict=failed
check "the push succeeds" "pushed" "$push_verdict"
check "  every call carries the primary's token" "0" "$(awk '$1 != "tok-alice"' "$STUB_DIR/api.log" | wc -l | tr -d ' ')"
contains "  and it updates the existing file by its sha" "sha=sha-1" "$(cat "$STUB_DIR/api.log")"

echo "== run.bash: the wiring"
check "no config-repo call rides on gh's active account" "" "$(grep -nE 'gh api "repos/[$][{](config_)?repo[}]' "$RUN_BASH")"
check "the config step asks config_repo_exists" "yes" "$(grep -qE '^if config_repo_exists "[$]config_repo"; then' "$RUN_BASH" && echo yes || echo no)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
