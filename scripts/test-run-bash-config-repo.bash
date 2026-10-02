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
for fn in info success warning error hl_abort fatal gh_primary config_repo_read config_repo_exists push_config_to_repo hl_pull_config_source; do
    extract "$RUN_BASH" "$fn"
done

RED='' ; GREEN='' ; YELLOW='' ; CYAN='' ; BOLD='' ; NC='' ; CROSS='x' ; CHECK='v' ; ARROW='>' ; INFO='i' ; WARN='!'
export RED GREEN YELLOW CYAN BOLD NC CROSS CHECK ARROW INFO WARN

# ── stubs ─────────────────────────────────────────────────────────────────────
# gh: `auth token --user X` hands out "tok-X" unless $STUB_DIR/no-token exists. `api repos/O/…`
# answers only when GH_TOKEN is "tok-O" (the repo is private to its owner); any other caller
# gets gh's real 404 wording. $STUB_DIR/api-error makes every api call fail with its content
# instead, as an expired token or a network fault does. $STUB_DIR/missing names one path that
# answers 404. Every api call is logged with the token it carried, and a PUT's --input body is
# kept in $STUB_DIR/put-body. A read answers by its --jq filter.
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
        if [ -f "$STUB_DIR/missing" ] && [ "$2" = "$(cat "$STUB_DIR/missing")" ] && [ "$3" != "--method" ]; then
            echo "gh: Not Found (HTTP 404)" >&2
            exit 1
        fi
        prev=""
        for arg; do
            if [ "$prev" = "--input" ]; then cp "$arg" "$STUB_DIR/put-body"; fi
            if [ "$prev" = "--jq" ]; then filter="$arg"; fi
            prev="$arg"
        done
        case "${filter:-}" in
            .name) echo "fedora-desktop-config" ;;
            .private) echo "true" ;;
            .sha) echo "sha-1" ;;
            .content) printf 'x: 1\n' | base64 ;;
            ".[].name") printf 'h.yml\nother.yml\n' ;;
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
    rm -f "$STUB_DIR/api.log" "$STUB_DIR/api-error" "$STUB_DIR/no-token" "$STUB_DIR/missing" "$STUB_DIR/put-body"
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
contains "  saying whose token is missing" "could not read alice's gh token" "$OUT"
check "  and never calls the API with someone else's token" "no" \
    "$([ -s "$STUB_DIR/api.log" ] && echo yes || echo no)"

echo "== config_repo_read"
# read_path <path> <jq> — config_repo_read as alice, in the main shell as run.bash calls it.
# Prints "read:<value>" or "absent", and nothing of its own when it stopped the run.
read_path() {
    GH_TOKEN=tok-someone-else primary_gh_username=alice bash -c 'source "$1"; if config_repo_read alice/fedora-desktop-config "$2" "$3"; then echo "read:${config_repo_value}"; else echo absent; fi' _ "$work/fn.bash" "$1" "$2" 2>&1
}
reset_stub
check "a file's field is read into config_repo_value" "read:sha-1" "$(read_path hosts/h.yml .sha)"
contains "  from its contents endpoint" "repos/alice/fedora-desktop-config/contents/hosts/h.yml" "$(cat "$STUB_DIR/api.log")"
reset_stub
echo "repos/alice/fedora-desktop-config/contents/hosts/h.yml" >"$STUB_DIR/missing"
check "a 404 leaves it empty and reads as absent" "absent" "$(read_path hosts/h.yml .sha)"
reset_stub
echo "gh: connection reset" >"$STUB_DIR/api-error"
OUT="$(read_path hosts/h.yml .content)"
check "a network fault stops the run" "no" "$(echo "$OUT" | grep -qE '^(read:|absent)' && echo yes || echo no)"
contains "  with gh's words" "connection reset" "$OUT"

echo "== push_config_to_repo"
# push — push_config_to_repo as alice; its last line is "pushed" when it returned.
push() {
    GH_TOKEN=tok-someone-else primary_gh_username=alice bash -c 'source "$1"; push_config_to_repo "$2" alice/fedora-desktop-config hosts/h.yml h && echo pushed' _ "$work/fn.bash" "$work/local.yml" 2>&1
}
body_field() {
    python3 -c 'import json, sys; print(json.load(open(sys.argv[1])).get(sys.argv[2], "-"))' "$STUB_DIR/put-body" "$1" 2>&1
}
echo "x: 1" >"$work/local.yml"
local_b64="$(base64 -w0 "$work/local.yml")"
reset_stub
OUT="$(push)"
check "the push succeeds" "pushed" "$(last_line "$OUT")"
check "  every call carries the primary's token" "0" "$(awk '$1 != "tok-alice"' "$STUB_DIR/api.log" | wc -l | tr -d ' ')"
check "  the config is not on gh's command line" "no" "$(grep -qF "$local_b64" "$STUB_DIR/api.log" && echo yes || echo no)"
check "  it travels in the request body" "$local_b64" "$(body_field content)"
check "  updating the existing file by its sha" "sha-1" "$(body_field sha)"
check "  with the commit message" "Update config from h" "$(body_field message)"
reset_stub
echo "repos/alice/fedora-desktop-config/contents/hosts/h.yml" >"$STUB_DIR/missing"
OUT="$(push)"
check "a first save creates the file" "pushed" "$(last_line "$OUT")"
check "  with no sha" "-" "$(body_field sha)"
reset_stub
echo "gh: Bad credentials (HTTP 401)" >"$STUB_DIR/api-error"
OUT="$(push)"
check "a failed sha lookup stops the save" "no" "$(echo "$OUT" | grep -qx pushed && echo yes || echo no)"
check "  before anything is uploaded" "no" "$([ -e "$STUB_DIR/put-body" ] && echo yes || echo no)"

echo "== hl_pull_config_source (headless)"
reset_stub
pull_out="$(GH_TOKEN=tok-someone-else primary_gh_username=alice HEADLESS=true bash -c 'source "$1"; hl_pull_config_source "$2" hosts/h.yml' _ "$work/fn.bash" "$work/pulled.yml" 2>&1)"
check "the headless pull writes the saved config" "x: 1" "$(cat "$work/pulled.yml" 2>&1)"
check "  reading as the primary" "0" "$(awk '$1 != "tok-alice"' "$STUB_DIR/api.log" | wc -l | tr -d ' ')"
contains "  and says so" "pulled config hosts/h.yml" "$pull_out"

echo "== run.bash: the wiring"
# Turned round, so a new spelling cannot slip past: EVERY `api … repos/` line, whatever runs it,
# whatever flags come first and however the repo is quoted, must be gh_primary, or the
# account-choice probe that hands each account its own token.
check "every repos/ API call carries a named account's token" "" "$(grep -nE '(^|[^[:alnum:]_])api([[:space:]]|$).*repos/' "$RUN_BASH" | grep -vE 'gh_primary api |GH_TOKEN="[$]token" gh api ')"
check "no gh repo subcommand runs outside a printed hint" "" "$(awk '{ line = $0; gsub(/"[^"]*"/, "", line); if (line ~ /(^|[^[:alnum:]_-])(gh|GH_REPO[}]?|gh-[a-z]+)[[:space:]]+repo[[:space:]]/) print NR ": " $0 }' "$RUN_BASH")"
check "no file content is passed to gh as an argument" "" "$(grep -nE -- '--(field|raw-field|f|F)[[:space:]]+"?content=' "$RUN_BASH")"
check "the config step asks config_repo_exists" "yes" "$(grep -qE '^if config_repo_exists "[$]config_repo"; then' "$RUN_BASH" && echo yes || echo no)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
