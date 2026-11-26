#!/usr/bin/env bash
# Unit-test that the gh-<alias> wrappers put gh's active account back (play-github-cli-multi.yml).
#
# gh-<alias> and clone-<alias> switch gh to their account for one command. They used to
# switch afterwards to the SAVED default (~/.config/gh/default-account), not to the account
# that was active, so anything that had chosen an account was left on another one. run.bash
# made the primary active, called gh-lts, and then read the primary's private config repo as
# the wrong account. The wrappers now share one jinja-free function, __gh_as_account, which
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
for fn in __gh_active_account __gh_as_account "gh-{{ alias }}" "clone-{{ alias }}" "gh-token-{{ alias }}"; do
    extract "$fn"
done
# clone-<alias> builds an ssh command with this play helper; its shape is not under test.
cat >>"$work/fn.bash" <<'FN'
__gh443_sshcmd() { echo "ssh -i $1"; }
FN

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
        # gh's own --json hosts shape, replayed through real jq with the caller's query, so a
        # broken query fails here as it would on the host. With $STUB_DIR/token-env (GH_TOKEN
        # set) gh reports one active row with an empty login and exits 0.
        if [ "$*" != "auth status --hostname github.com --json hosts --jq ${8:-}" ] || [ "$#" -ne 8 ]; then
            echo "unexpected gh $*" >&2
            exit 99
        fi
        active="$(cat "$STUB_DIR/active")"
        if [ -f "$STUB_DIR/token-env" ]; then active=""; fi
        printf '{"hosts":{"github.com":[{"login":"other","active":false,"state":"success"},{"login":"%s","active":true,"state":"success"}]}}' "$active" | jq -r "$8"
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
    rm -f "$STUB_DIR/unknown" "$STUB_DIR/command-fails" "$STUB_DIR/token-env"
    : >"$STUB_DIR/calls.log"
    echo "$1" >"$STUB_DIR/active"
    mkdir -p "$work/home/.config/gh"
    echo "saved-default" >"$work/home/.config/gh/default-account"
}

# wrap <function> <args…> — one wrapper in a fresh shell; prints its stdout, then "rc=<code>".
run_fn() {
    local fn="$1" out code=0
    shift
    out="$(cd "$work" && HOME="$work/home" bash -c 'source "$1"; shift; "$@"' _ "$work/fn.bash" "$fn" "$@" 2>"$work/stderr")" || code=$?
    printf '%s\nrc=%s' "$out" "$code"
}
wrap() { run_fn gh-lts "$@"; }

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

echo "== no account to put back (GH_TOKEN set, so gh reports an empty login)"
start primary
touch "$STUB_DIR/token-env"
OUT="$(wrap api user)"
check "the wrapper refuses" "rc=1" "$(printf '%s\n' "$OUT" | awk 'END {print}')"
check "  before switching anything" "" "$(cat "$STUB_DIR/calls.log")"

echo "== clone-lts"
start primary
OUT="$(run_fn clone-lts owner/repo)"
check "the clone runs as the alias's account" "run as lts-user: repo clone owner/repo -- --config core.sshCommand=ssh -i $work/home/.ssh/github_lts" "$(grep '^run as' "$STUB_DIR/calls.log")"
check "  and gh is put back" "primary" "$(cat "$STUB_DIR/active")"
start primary
touch "$STUB_DIR/command-fails"
OUT="$(run_fn clone-lts owner/repo)"
check "a failed clone passes its exit code through" "rc=3" "$(printf '%s\n' "$OUT" | awk 'END {print}')"
check "  and gh is still put back" "primary" "$(cat "$STUB_DIR/active")"

echo "== gh-token-lts"
start primary
OUT="$(run_fn gh-token-lts)"
check "it reads the alias's token by name" "run as primary: auth token --hostname github.com --user lts-user" "$(grep '^run as' "$STUB_DIR/calls.log")"
check "  without switching gh" "" "$(grep '^switch' "$STUB_DIR/calls.log")"

echo "== after Claude Code's shell snapshot (Plan 00265 R72)"
# Claude Code's Bash tool does not read ~/.bashrc: it replays a snapshot of the login shell,
# which keeps every function except those whose name starts with a single underscore (its
# filter is [[ "$func" == _[!_]* ]]) and of the variables only those exported. The whole
# gh-aliases block is rendered for one alias, sourced, put through that filter, and replayed
# into a clean shell, where each command must still work.
awk '
    index($0, "marker: \"# {mark} ANSIBLE MANAGED: GitHub CLI Multi-Account Aliases\"") { m = 1; next }
    m && !b && /^ +block: \|$/ { b = 1; next }
    b && /^ +$|^$/ { print ""; next }
    b {
        match($0, /^ +/)
        if (indent == 0) indent = RLENGTH
        if (RLENGTH < indent) exit
        print substr($0, indent + 1)
    }
' "$PLAY" | awk -v root="$REPO_ROOT" '{
    gsub(/\{%[^%]+%\}/, ""); gsub(/\{\{ alias \}\}/, "lts"); gsub(/\{\{ username \}\}/, "lts-user")
    gsub(/\{\{ root_dir \}\}/, root); print
}' >"$work/block.bash"
check "the gh-aliases block renders with no Jinja left" "yes" "$(grep -q 'function git()' "$work/block.bash" && ! grep -qE '\{\{|\{%' "$work/block.bash" && echo yes || echo no)"
if ! bash --norc --noprofile -c 'source "$1" || exit 1
    declare -F | while read -r _ _ func; do
        [[ "$func" == _[!_]* ]] || declare -f "$func"
    done' _ "$work/block.bash" >"$work/snapshot.bash"; then
    echo "FAIL: the rendered gh-aliases block could not be sourced" >&2
    exit 1
fi

# git-account-helper names the alias in $STUB_DIR/alias, as it does for a github.com-<alias>
# remote; git logs each call and answers the few questions the wrappers ask.
cat >"$work/bin/git-account-helper" <<'STUB'
#!/usr/bin/env bash
[ "$1" = detect ] && [ -f "$STUB_DIR/alias" ] || exit 1
cat "$STUB_DIR/alias"
STUB
cat >"$work/bin/git" <<'STUB'
#!/usr/bin/env bash
words="$(printf ' %s' "$@")"
echo "git${words}" >>"$STUB_DIR/calls.log"
while [ "${1-}" = -c ]; do shift 2; done
words="$(printf ' %s' "$@")"
case "${words# }" in
    "symbolic-ref --quiet refs/remotes/origin/HEAD") ;;
    "symbolic-ref --short refs/remotes/origin/HEAD") echo origin/main ;;
    "remote get-url origin") echo "git@github.com:owner/repo.git" ;;
esac
STUB
chmod +x "$work/bin/git-account-helper" "$work/bin/git"
mkdir -p "$work/repo"
# replay.bash <snapshot> <command…> — what the Bash tool does: source the snapshot, then run.
cat >"$work/replay.bash" <<'REPLAY'
source "$1"
shift
"$@"
REPLAY

# snap <command…> — the command in a clean shell holding only the snapshot and the
# environment; prints its stdout, then "rc=<code>".
snap() {
    local out code=0
    out="$(cd "$work/repo" && env -i HOME="$work/home" PATH="$PATH" STUB_DIR="$STUB_DIR" \
        bash "$work/replay.bash" "$work/snapshot.bash" "$@" 2>"$work/stderr")" || code=$?
    printf '%s\nrc=%s' "$out" "$code"
}
# snap_check <label> <want> <command…> — snap, and the output and stderr judged.
snap_check() {
    local label="$1" want="$2"
    shift 2
    check "$label" "$want" "$(snap "$@")"
    check "  with nothing missing" "" "$(grep -E 'command not found|unknown GitHub account' "$work/stderr")"
}
account_git="git -c core.sshCommand=ssh -F /dev/null -o HostName=github.com -p 22 -i $work/home/.ssh/github_lts -o IdentitiesOnly=yes -o AddKeysToAgent=yes -c credential.https://github.com.helper="

start primary
echo lts >"$STUB_DIR/alias"
snap_check "git in a repository with an account's remote" "
rc=0" git status
check "  runs as that account over SSH and HTTPS" "yes" "$(grep -F "$account_git" "$STUB_DIR/calls.log" | grep -F -- '--user lts-user)' | grep -q ' status$' && echo yes || echo no)"
start primary
snap_check "git-lts" "
rc=0" git-lts fetch
check "  runs as that account" "yes" "$(grep -F "$account_git" "$STUB_DIR/calls.log" | grep -q ' fetch$' && echo yes || echo no)"
start primary
rm -f "$STUB_DIR/alias"
snap_check "git in a repository with no account's remote" "
rc=0" git status
check "  is plain git" "git status" "$(cat "$STUB_DIR/calls.log")"
start primary
echo lts >"$STUB_DIR/alias"
snap_check "git-branch-default" "main
rc=0" git-branch-default
snap_check "git-lts-branch-default" "main
rc=0" git-lts-branch-default
start primary
snap_check "gh-lts" "output of api user
rc=0" gh-lts api user
check "  runs as the alias's account and puts gh back" "primary" "$(cat "$STUB_DIR/active")"
start primary
snap_check "clone-lts" "output of repo clone owner/repo -- --config core.sshCommand=ssh -F /dev/null -o HostName=github.com -p 22 -i $work/home/.ssh/github_lts -o IdentitiesOnly=yes -o AddKeysToAgent=yes
rc=0" clone-lts owner/repo
start primary
snap_check "remote-lts -y" "
rc=0" remote-lts -y
check "  points origin at the alias's host" "yes" "$(grep -qxF 'git remote set-url origin git@github.com-lts:owner/repo.git' "$STUB_DIR/calls.log" && echo yes || echo no)"

echo "== the play's wiring"
# Every account switch in the gh-aliases block sits in a function whose job is to switch
# (gh-switch, gh-set-default) or in __gh_as_account, which puts the account back.
switchers="$(awk '
    /^ *function [^ ]+\(\) \{/ { fn = $2; sub(/\(\).*/, "", fn) }
    /gh auth switch/ && fn != "" { print fn }
' "$PLAY" | sort -u | tr '\n' ' ')"
check "only gh-switch, gh-set-default and __gh_as_account switch accounts" "__gh_as_account gh-set-default gh-switch " "$switchers"
check "gh-<alias>-token-phpstorm goes through __gh_as_account" "yes" "$(grep -qE '^ *__gh_as_account "\{\{ username \}\}" __gh_phpstorm_token_report ' "$PLAY" && echo yes || echo no)"

echo ""
echo "RESULT: passed: $passed failed: $failed"
[ "$failed" -eq 0 ]
