#!/usr/bin/env bash
# Unit-test that the gh-<alias> wrappers put gh's active account back (play-github-cli-multi.yml).
#
# gh-<alias> and clone-<alias> switch gh to their account for one command. They used to
# switch afterwards to the SAVED default (~/.config/gh/default-account), not to the account
# that was active, so anything that had chosen an account was left on another one. run.bash
# made the primary active, called gh-lts, and then read the primary's private config repo as
# the wrong account. The wrappers now share one jinja-free function, _gh_as_account, which
# restores whatever was active before.
#
# The functions are extracted from the play's gh-aliases block with awk and sourced; the
# per-alias wrappers are rendered by replacing their two jinja names, since jinja2 is not
# available everywhere QA runs. `gh` is a stub on PATH that keeps its active account in a file.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLAY="${PLAY_UNDER_TEST:-$REPO_ROOT/playbooks/imports/play-github-cli-multi.yml}"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# extract <function name as written in the play> — the function, de-indented, rendered for
# alias "lts" / user "lts-user".
extract() {
    local fn="$1"
    awk -v fn="$fn" '
        index($0, "function " fn "() {") { p = 1; match($0, /^ */); indent = RLENGTH }
        p { print substr($0, indent + 1) }
        p && substr($0, indent + 1) == "}" { exit }
    ' "$PLAY" | awk '{ gsub(/\{\{ alias \}\}/, "lts"); gsub(/\{\{ username \}\}/, "lts-user"); print }' >>"$work/fn.bash"
    if ! grep -qF "function ${fn//\{\{ alias \}\}/lts}() {" "$work/fn.bash"; then
        echo "FAIL: could not extract ${fn} from ${PLAY}" >&2
        exit 1
    fi
}

: >"$work/fn.bash"
for fn in _gh_active_account _gh_as_account "gh-{{ alias }}"; do
    extract "$fn"
done

# ── stubs ─────────────────────────────────────────────────────────────────────
# gh: `auth status --json hosts --jq …` prints the active login from $STUB_DIR/active;
# `auth switch --user X` makes X active (unless X is in $STUB_DIR/unknown) and logs it;
# anything else is logged with the account active when it ran, and fails when
# $STUB_DIR/command-fails exists.
mkdir -p "$work/bin"
cat >"$work/bin/gh" <<'STUB'
#!/usr/bin/env bash
case "$1 $2" in
    "auth status")
        cat "$STUB_DIR/active"
        ;;
    "auth switch")
        for arg; do last="$arg"; done
        if [ -f "$STUB_DIR/unknown" ] && grep -qx "$last" "$STUB_DIR/unknown"; then
            echo "no account $last" >&2
            exit 1
        fi
        echo "$last" >"$STUB_DIR/active"
        echo "switch $last" >>"$STUB_DIR/calls.log"
        ;;
    *)
        echo "run as $(cat "$STUB_DIR/active"): $*" >>"$STUB_DIR/calls.log"
        if [ -f "$STUB_DIR/command-fails" ]; then exit 3; fi
        echo "output of $*"
        ;;
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

# start <active account> — a fresh stub state; the saved default is deliberately a THIRD
# account, so a wrapper that restores the default instead of the active one is caught.
start() {
    rm -f "$STUB_DIR/unknown" "$STUB_DIR/command-fails"
    : >"$STUB_DIR/calls.log"
    echo "$1" >"$STUB_DIR/active"
    mkdir -p "$work/home/.config/gh"
    echo "saved-default" >"$work/home/.config/gh/default-account"
}

# wrap <args…> — gh-lts in a fresh shell; prints its stdout, then "rc=<code>".
wrap() {
    local out code=0
    out="$(HOME="$work/home" bash -c 'source "$1"; shift; gh-lts "$@"' _ "$work/fn.bash" "$@" 2>"$work/stderr")" || code=$?
    printf '%s\nrc=%s' "$out" "$code"
}

echo "== gh-lts from another account"
start primary
OUT="$(wrap api user)"
check "the command runs as the alias's account" "run as lts-user: api user" "$(grep '^run as' "$STUB_DIR/calls.log")"
check "gh is back on the account that was active" "primary" "$(cat "$STUB_DIR/active")"
check "  not on the saved default" "switch lts-user
switch primary" "$(grep '^switch' "$STUB_DIR/calls.log")"
check "its stdout is the command's alone" "output of api user
rc=0" "$OUT"

echo "== gh-lts when the alias's account is already active"
start lts-user
OUT="$(wrap api user)"
check "nothing is switched" "" "$(grep '^switch' "$STUB_DIR/calls.log")"
check "gh stays where it was" "lts-user" "$(cat "$STUB_DIR/active")"

echo "== a failing command"
start primary
touch "$STUB_DIR/command-fails"
OUT="$(wrap api user)"
check "its exit code is passed through" "rc=3" "$(printf '%s\n' "$OUT" | awk 'END {print}')"
check "  and gh is still put back" "primary" "$(cat "$STUB_DIR/active")"

echo "== an account gh does not know"
start primary
echo "lts-user" >"$STUB_DIR/unknown"
OUT="$(wrap api user)"
check "the wrapper fails" "rc=1" "$(printf '%s\n' "$OUT" | awk 'END {print}')"
check "  without running the command" "" "$(grep '^run as' "$STUB_DIR/calls.log")"
check "  saying so on stderr" "yes" "$(grep -qF 'Account lts-user not authenticated' "$work/stderr" && echo yes || echo no)"
check "  and gh is left where it was" "primary" "$(cat "$STUB_DIR/active")"

echo "== a switch back that fails"
start primary
echo "primary" >"$STUB_DIR/unknown"
OUT="$(wrap api user)"
check "the wrapper fails" "rc=1" "$(printf '%s\n' "$OUT" | awk 'END {print}')"
check "  naming both accounts" "yes" "$(grep -qF 'could not switch gh back to primary; it is still on lts-user' "$work/stderr" && echo yes || echo no)"

echo "== the play's wiring"
check "no wrapper restores the saved default" "" "$(grep -nE 'auth switch .*default_account' "$PLAY")"
check "clone-<alias> goes through _gh_as_account" "yes" "$(awk '/function clone-\{\{ alias \}\}\(\) \{/ {p = 1} p && /_gh_as_account "\{\{ username \}\}" gh repo clone/ {f = 1} p && /^ *\}$/ {p = 0} END {print f ? "yes" : "no"}' "$PLAY")"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
