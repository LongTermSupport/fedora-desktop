#!/usr/bin/env bash
# Unit-test lib/restart-request.bash (the supervisor's "restart this session" contract).
#
# Sources the libraries from THIS repo (not the deployed /var/local copy).
#
# WHY THIS TEST EXISTS. A supervisor plugin inside the container can ask for a restart: the
# supervisor writes a JSON request file, then exits with status 75. The launcher acts on that
# on the host, so the file is container-controlled input that ends up in a relaunch argv.
# Every refusal here is a way a bad or stale file could otherwise start a session, and the
# budget is what stops a crash loop from relaunching for ever.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"
LAUNCHER="$REPO_ROOT/files/var/local/claude-yolo/claude-yolo"

for lib in session-registry restart-request; do
    if [ ! -f "$LIB_DIR/$lib.bash" ]; then
        echo "FAIL: library not found at $LIB_DIR/$lib.bash" >&2
        exit 1
    fi
done
# session-registry's writers call print_error; the functions under test do not, but
# define it so a regression that starts calling it fails on content, not on a missing name.
print_error() { printf 'ERROR: %s\n' "$*" >&2; }
# shellcheck source=../files/var/local/claude-yolo/lib/session-registry.bash
source "$LIB_DIR/session-registry.bash"
# shellcheck source=../files/var/local/claude-yolo/lib/restart-request.bash
source "$LIB_DIR/restart-request.bash"

for fn in ccy_restart_request_read ccy_restart_request_discard ccy_restart_budget_take ccy_restart_relaunch_args; do
    if ! declare -F "$fn" >/dev/null; then
        echo "FAIL: $fn is not defined after sourcing the library" >&2
        exit 1
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

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

SID="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
OLD="bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
NOW=1790000100

# read_case <label> <want-rc> <want-out> <file> — run the reader, compare stdout and status.
read_case() {
    local label="$1" want_rc="$2" want_out="$3" file="$4" out rc
    out=$(ccy_restart_request_read "$file" "$NOW" 2>"$work/err")
    rc=$?
    check "$label (status)" "$want_rc" "$rc"
    check "$label (stdout)" "$want_out" "$out"
    if [ "$want_rc" -eq 1 ]; then
        check "$label (says why on stderr)" "yes" "$([ -s "$work/err" ] && echo yes || echo no)"
    fi
}

write_req() { printf '%s' "$2" >"$1"; }

echo "=== ccy_restart_request_read ==="

write_req "$work/good.json" "{\"session_id\": \"$SID\", \"reason\": \"max age\", \"plugin\": \"restart\", \"requested_at\": 1790000000.5}"
read_case "a well-formed fresh request yields the session id" 0 "$SID" "$work/good.json"

read_case "no file is status 2, not an error" 2 "" "$work/absent.json"

write_req "$work/upper.json" "{\"session_id\": \"${SID^^}\", \"requested_at\": 1790000000}"
read_case "an upper-case uuid is accepted" 0 "${SID^^}" "$work/upper.json"

write_req "$work/notjson.json" "not json at all"
read_case "malformed JSON is refused" 1 "" "$work/notjson.json"

write_req "$work/array.json" "[\"$SID\"]"
read_case "a non-object is refused" 1 "" "$work/array.json"

write_req "$work/empty.json" ""
read_case "an empty file is refused" 1 "" "$work/empty.json"

write_req "$work/nosid.json" '{"requested_at": 1790000000}'
read_case "a missing session id is refused" 1 "" "$work/nosid.json"

write_req "$work/badsid.json" '{"session_id": "--dangerously-skip-permissions", "requested_at": 1790000000}'
read_case "a session id shaped like an option is refused" 1 "" "$work/badsid.json"

write_req "$work/badsid2.json" "{\"session_id\": \"$SID; id\", \"requested_at\": 1790000000}"
read_case "a session id with trailing text is refused" 1 "" "$work/badsid2.json"

write_req "$work/badsid3.json" "{\"session_id\": \"$SID\\n\", \"requested_at\": 1790000000}"
read_case "a session id with a trailing newline is refused" 1 "" "$work/badsid3.json"

write_req "$work/numsid.json" '{"session_id": 12345, "requested_at": 1790000000}'
read_case "a non-string session id is refused" 1 "" "$work/numsid.json"

write_req "$work/stale.json" "{\"session_id\": \"$SID\", \"requested_at\": 1789990000}"
read_case "a stale request is refused" 1 "" "$work/stale.json"

write_req "$work/future.json" "{\"session_id\": \"$SID\", \"requested_at\": 1790009999}"
read_case "a request from the future is refused" 1 "" "$work/future.json"

write_req "$work/notime.json" "{\"session_id\": \"$SID\"}"
read_case "a missing timestamp is refused" 1 "" "$work/notime.json"

write_req "$work/strtime.json" "{\"session_id\": \"$SID\", \"requested_at\": \"1790000000\"}"
read_case "a string timestamp is refused" 1 "" "$work/strtime.json"

{
    printf '{"session_id": "%s", "requested_at": 1790000000, "reason": "' "$SID"
    head -c 9000 /dev/zero | tr '\0' 'x'
    printf '"}'
} >"$work/big.json"
read_case "an oversize file is refused" 1 "" "$work/big.json"

ln -s "$work/good.json" "$work/link.json"
read_case "a symlink is refused" 1 "" "$work/link.json"

mkdir -p "$work/dir.json"
read_case "a directory is refused" 1 "" "$work/dir.json"

# A reason full of control characters must not reach the terminal raw.
write_req "$work/esc.json" "{\"session_id\": \"$SID\", \"requested_at\": 1790000000, \"reason\": \"a\\u001b[31mred\"}"
ccy_restart_request_read "$work/esc.json" "$NOW" >"$work/esc.out" 2>"$work/esc.err"
check "control characters in the reason are not echoed" "0" \
    "$(grep -c -P '\x1b' "$work/esc.err")"

echo "=== ccy_restart_request_discard ==="

write_req "$work/d.json" "x"
ccy_restart_request_discard "$work/d.json"; check "discard removes the file (status)" 0 "$?"
check "…and it is gone" "no" "$([ -e "$work/d.json" ] && echo yes || echo no)"
ccy_restart_request_discard "$work/d.json"; check "discard of an absent file is fine" 0 "$?"
ln -s "$work/good.json" "$work/dl.json"
ccy_restart_request_discard "$work/dl.json"
check "discard of a symlink removes the link, not its target" "yes" \
    "$([ -e "$work/good.json" ] && [ ! -L "$work/dl.json" ] && echo yes || echo no)"
mkdir -p "$work/dd.json"
ccy_restart_request_discard "$work/dd.json" 2>/dev/null; check "discard of a directory is an error" 1 "$?"

echo "=== ccy_restart_budget_take ==="

hist="$work/hist"
ccy_restart_budget_take "$hist" 1000 3 600 2>/dev/null; check "1st restart in a window is allowed" 0 "$?"
ccy_restart_budget_take "$hist" 1100 3 600 2>/dev/null; check "2nd restart is allowed" 0 "$?"
ccy_restart_budget_take "$hist" 1200 3 600 2>/dev/null; check "3rd restart is allowed" 0 "$?"
ccy_restart_budget_take "$hist" 1300 3 600 2>"$work/budget.err"; check "4th restart in the window is refused" 1 "$?"
check "…and the refusal names the limit" "yes" \
    "$(grep -q -E '3 .*600' "$work/budget.err" && echo yes || echo no)"
check "…and a refusal records nothing" "3" "$(wc -l <"$hist")"
ccy_restart_budget_take "$hist" 1701 3 600 2>/dev/null; check "once the oldest ages out, one more is allowed" 0 "$?"

printf 'garbage\n' >"$work/hist2"
ccy_restart_budget_take "$work/hist2" 1000 3 600 2>/dev/null; check "a corrupt history is an error, not a free pass" 1 "$?"

ccy_restart_budget_take "$work/hist3" 1000 0 600 2>/dev/null; check "a limit of 0 refuses every restart" 1 "$?"
ccy_restart_budget_take "$work/hist4" 1000 abc 600 2>/dev/null; check "a non-numeric limit is an error" 1 "$?"
ccy_restart_budget_take "$work/nodir/hist5" 1000 3 600 2>/dev/null; check "an unwritable history is an error" 1 "$?"

echo "=== ccy_restart_relaunch_args ==="

# relaunch <session-id> [args...] → the output, NUL separated, rendered with | after each.
relaunch() {
    local out=() a joined=""
    mapfile -d '' -t out < <(ccy_restart_relaunch_args "$@")
    for a in "${out[@]}"; do joined+="$a|"; done
    printf '%s' "$joined"
}

check "no args: just --resume <id>" "--resume|$SID|" "$(relaunch "$SID")"
check "--continue is replaced" "--model|opus|--resume|$SID|" \
    "$(relaunch "$SID" --continue --model opus)"
check "-c is replaced" "--resume|$SID|" "$(relaunch "$SID" -c)"
check "an old --resume <id> is replaced, id and all" "--resume|$SID|" \
    "$(relaunch "$SID" --resume "$OLD")"
check "-r <id> is replaced" "--resume|$SID|" "$(relaunch "$SID" -r "$OLD")"
check "--resume=<id> is replaced" "--resume|$SID|" "$(relaunch "$SID" --resume="$OLD")"
check "a bare --resume does not eat the next option" "--model|opus|--resume|$SID|" \
    "$(relaunch "$SID" --resume --model opus)"
check "ccy options are kept" "--token|work|--ssh-key|/k|--network|n|--supervise|--resume|$SID|" \
    "$(relaunch "$SID" --token work --ssh-key /k --network n --supervise -c)"
check "--ssh-agent is kept (the agent outlives the container)" "--ssh-agent|--resume|$SID|" \
    "$(relaunch "$SID" --ssh-agent)"
check "one-shot options are dropped" "--resume|$SID|" \
    "$(relaunch "$SID" --rebuild=claude --debug)"
check "--prompt and its text are dropped" "--resume|$SID|" \
    "$(relaunch "$SID" --prompt "do the thing")"
check "a bare opening message is dropped" "--resume|$SID|" \
    "$(relaunch "$SID" "fix the bug")"
check "an unknown flag's value is kept" "--model|opus|--resume|$SID|" \
    "$(relaunch "$SID" --model opus)"
check "after -- everything is claude's, but resume flags are still replaced" \
    "--|--model|opus|--resume|$SID|" \
    "$(relaunch "$SID" -- --continue --model opus)"
check "after -- a literal word is kept" "--|keep me|--resume|$SID|" \
    "$(relaunch "$SID" -- "keep me")"
check "--session-id is dropped (it conflicts with --resume)" "--resume|$SID|" \
    "$(relaunch "$SID" --session-id "$OLD")"
check "an argument with a newline survives" $'--model|a\nb|--resume|'"$SID|" \
    "$(relaunch "$SID" --model $'a\nb')"
bad_out=$(ccy_restart_relaunch_args "not-a-uuid" 2>/dev/null)
bad_rc=$?
check "a malformed session id is refused (status)" 1 "$bad_rc"
check "a malformed session id is refused (no output)" "" "$bad_out"

echo "=== ccy_handle_restart_exit (the launcher's own function, with stubs) ==="

# Lift the function out of the launcher so the real text is what runs, then drive it in a
# subshell against a throwaway project. The stand-in "launcher" it execs records its argv.
awk '/^ccy_handle_restart_exit\(\) \{$/ {p=1} p {print} p && /^}$/ {exit}' "$LAUNCHER" >"$work/handler.bash"
check "the function was found in the launcher" "yes" "$([ -s "$work/handler.bash" ] && echo yes || echo no)"

mkdir -p "$work/stub"
cat >"$work/stub/claude-yolo" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$STUB_RECORD"
STUB
chmod +x "$work/stub/claude-yolo"
cat >"$work/update-stub.bash" <<'STUB'
update_claude_inplace() { echo "$1" >>"$STUB_UPDATES"; return "$STUB_UPDATE_RC"; }
STUB

# handler_case <name> <request-body|-> <update-rc> <history-lines> <orig-args...>
# Prints "rc=<status> update=<n> exec=<yes|no> request=<present|gone>" and leaves stderr in
# $work/<name>.err and the exec argv in $work/<name>.argv.
handler_case() {
    local name="$1" body="$2" update_rc="$3" history="$4"
    shift 4
    local proj="$work/proj-$name"
    mkdir -p "$proj/.claude/ccy/state" "$work/cache-$name"
    : >"$work/cache-$name/restart-history-$(printf '%s' "$proj" | md5sum | cut -c1-16)"
    local i
    for ((i = 0; i < history; i++)); do
        printf '%s\n' "$(date +%s)" >>"$work/cache-$name/restart-history-$(printf '%s' "$proj" | md5sum | cut -c1-16)"
    done
    [ "$body" = "-" ] || printf '%s' "$body" >"$proj/.claude/ccy/state/restart-request.json"
    (
        cd "$proj" || exit 99
        # shellcheck source=/dev/null
        source "$work/handler.bash"
        export SCRIPT_DIR="$work/stub"
        export VERSION_CHECK_CACHE="$work/cache-$name"
        export IMAGE_NAME="img:tag"
        export STUB_RECORD="$work/$name.argv"
        export STUB_UPDATES="$work/$name.updates" STUB_UPDATE_RC="$update_rc"
        CCY_ORIG_ARGS=("$@")
        export CCY_ORIG_ARGS
        # shellcheck source=/dev/null
        source "$work/update-stub.bash"
        ccy_handle_restart_exit
        echo "returned"
    ) >"$work/$name.out" 2>"$work/$name.err"
    local rc=$?
    local upd=0 ex=no req=gone
    [ -f "$work/$name.updates" ] && upd=$(wc -l <"$work/$name.updates")
    [ -f "$work/$name.argv" ] && ex=yes
    [ -e "$proj/.claude/ccy/state/restart-request.json" ] && req=present
    printf 'rc=%s update=%s exec=%s request=%s' "$rc" "$upd" "$ex" "$req"
}

NOWSEC=$(date +%s)
GOOD="{\"session_id\": \"$SID\", \"reason\": \"max age\", \"requested_at\": $NOWSEC}"

check "a valid request: updates, execs the launcher, consumes the file" \
    "rc=0 update=1 exec=yes request=gone" \
    "$(handler_case ok "$GOOD" 0 0 --token work -c)"
check "…and the relaunch argv is the original with --resume <id>" \
    "--token|work|--resume|$SID" "$(paste -sd'|' "$work/ok.argv")"
check "…and the update was of the launcher's image" "img:tag" "$(cat "$work/ok.updates")"

check "status 75 with no file: not a restart, nothing updated" \
    "rc=0 update=0 exec=no request=gone" "$(handler_case nofile - 0 0)"
check "…and it says so" "yes" \
    "$(grep -q 'not a restart' "$work/nofile.err" && echo yes || echo no)"

check "status 75 with a bad file: not a restart, the file is discarded" \
    "rc=0 update=0 exec=no request=gone" "$(handler_case bad 'not json' 0 0)"

check "an exhausted budget stops the restart before any update" \
    "rc=1 update=0 exec=no request=gone" "$(handler_case budget "$GOOD" 0 3)"
check "…and names the manual resume" "yes" \
    "$(grep -q -- "ccy --resume $SID" "$work/budget.err" && echo yes || echo no)"

check "a failed image update stops the restart, no relaunch" \
    "rc=1 update=1 exec=no request=gone" "$(handler_case updfail "$GOOD" 1 0)"

echo "=== launcher wiring ==="

check "launcher sources the library" "1" \
    "$(grep -c -F "source \"\$SCRIPT_DIR/lib/restart-request.bash\"" "$LAUNCHER")"
check "launcher lists the library in CCY_LIBS" "1" \
    "$(grep -c -E '^CCY_LIBS=\(.* restart-request( |\))' "$LAUNCHER")"
check "launcher captures the container status instead of dying on it" "1" \
    "$(grep -c -F 'container_rc=$?' "$LAUNCHER")"
check "launcher discards a stale request before the run" "1" \
    "$(grep -c -E '^ccy_restart_request_discard "[^"]*CCY_RESTART_REQUEST_REL" [|][|] exit 1$' "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
