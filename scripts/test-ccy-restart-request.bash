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

# The forwarded-agent sentinel, read from ssh-handling.bash rather than retyped, so the key
# functions are tested against the value the launcher really uses.
SSH_AGENT_SENTINEL=$(awk -F'"' '/^readonly SSH_AGENT_SENTINEL=/ {print $2}' "$LIB_DIR/ssh-handling.bash")
if [ -z "$SSH_AGENT_SENTINEL" ]; then
    echo "FAIL: SSH_AGENT_SENTINEL not found in $LIB_DIR/ssh-handling.bash" >&2
    exit 1
fi
export SSH_AGENT_SENTINEL

for fn in ccy_restart_request_read ccy_restart_request_discard ccy_restart_budget_take ccy_restart_relaunch_args \
    ccy_restart_choice_args ccy_restart_keys_unattended ccy_restart_marker_take ccy_restart_refuse; do
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
check "ccy options that are not launch choices are kept" "--supervise|--engine|podman|--max-age|3d|--resume|$SID|" \
    "$(relaunch "$SID" --supervise --engine podman --max-age 3d -c)"
check "launch choices are taken out (the caller supplies the ones the session ran with)" \
    "--supervise|--resume|$SID|" \
    "$(relaunch "$SID" --token work --ssh-key /k --ssh-agent --no-ssh --network n --no-network --github-443 --supervise)"
check "…a launch choice after -- is claude's and stays" "--|--token|x|--resume|$SID|" \
    "$(relaunch "$SID" -- --token x)"
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
bad_rc=0
bad_out=$(ccy_restart_relaunch_args "not-a-uuid" 2>/dev/null) || bad_rc=$?
check "a malformed session id is refused (status)" 1 "$bad_rc"
check "a malformed session id is refused (no output)" "" "$bad_out"

echo "=== ccy_restart_choice_args ==="

# choices [args...] → the output, NUL separated, rendered with | after each; "rc=N" if it failed.
choices() {
    local out=() a joined="" rc=0
    mapfile -d '' -t out < <(ccy_restart_choice_args "$@" 2>/dev/null)
    wait "$!" || rc=$?
    if [ "$rc" -ne 0 ]; then
        printf 'rc=%s' "$rc"
        return
    fi
    for a in "${out[@]}"; do joined+="$a|"; done
    printf '%s' "$joined"
}

check "a token, a key and a network become the launcher's options" \
    "--token|/t/work.2027-01-01.token|--ssh-key|/k/id|--network|proj-net|" \
    "$(choices /t/work.2027-01-01.token 0 false proj-net /k/id)"
check "no keys is --no-ssh, so the key picker is not offered" "--token|/t/w|--no-ssh|" \
    "$(choices /t/w 0 false "")"
check "the agent sentinel is --ssh-agent, beside a key file" \
    "--token|/t/w|--ssh-agent|--ssh-key|/k/id|" "$(choices /t/w 0 false "" "$SSH_AGENT_SENTINEL" /k/id)"
check "443 mode is carried" "--token|/t/w|--no-ssh|--github-443|" "$(choices /t/w 1 false "")"
check "--no-network is carried, and outranks a network name" "--token|/t/w|--no-ssh|--no-network|" \
    "$(choices /t/w 0 true proj-net)"
check "a value with a space or a newline survives" $'--token|/t/a b|--ssh-key|/k/x\ny|' \
    "$(choices "/t/a b" 0 false "" $'/k/x\ny')"
check "no token is refused, not relaunched into the token picker" "rc=1" "$(choices "" 0 false "")"
check "a 443 state other than 0/1 is refused" "rc=1" "$(choices /t/w yes false "")"
check "a no-network state other than true/false is refused" "rc=1" "$(choices /t/w 0 1 "")"
check "an empty key entry is refused" "rc=1" "$(choices /t/w 0 false "" "")"

echo "=== ccy_restart_keys_unattended ==="

ssh-keygen -q -t ed25519 -N '' -C '' -f "$work/key-open"
ssh-keygen -q -t ed25519 -N 'fixture-passphrase-not-a-secret' -C '' -f "$work/key-locked"
ccy_restart_keys_unattended "" "$work/key-open" 2>/dev/null
check "a key without a passphrase opens unattended" 0 "$?"
ccy_restart_keys_unattended "" "$SSH_AGENT_SENTINEL" 2>/dev/null
check "a forwarded agent needs nothing" 0 "$?"
ccy_restart_keys_unattended "" 2>/dev/null
check "no keys need nothing" 0 "$?"
ccy_restart_keys_unattended "" "$work/key-open" "$work/key-locked" </dev/null 2>"$work/locked.err"
check "a key with a passphrase does not (and nothing waits for one)" 1 "$?"
check "…and the refusal names the key" "yes" \
    "$(grep -q -F "$work/key-locked" "$work/locked.err" && echo yes || echo no)"
ccy_restart_keys_unattended "$work/pp-file" "$work/key-locked" 2>/dev/null
check "a named passphrase file (a server restore) lets it through" 0 "$?"
ccy_restart_keys_unattended "" "$work/no-such-key" 2>/dev/null
check "a key that cannot be read is refused" 1 "$?"

echo "=== ccy_restart_marker_take ==="

# marker <value|-> → "rc=<status> session=<id> env=<set|unset>". Called inside $( ), so
# what it sets stays in that subshell, as it would in a launcher at start-up.
marker() {
    if [ "$1" = "-" ]; then
        unset CCY_RESTART_RELAUNCH
    else
        export CCY_RESTART_RELAUNCH="$1"
    fi
    local rc=0
    ccy_restart_marker_take 2>/dev/null || rc=$?
    printf 'rc=%s session=%s env=%s' "$rc" "$CCY_RESTART_RELAUNCH_SESSION" "${CCY_RESTART_RELAUNCH+set}"
}
check "an ordinary launch has no restart session" "rc=0 session= env=" "$(marker -)"
check "a restart's session id is taken, and removed from the environment" \
    "rc=0 session=$SID env=" "$(marker "$SID")"
check "a mark that is not a session id is refused" "rc=1 session= env=" "$(marker "yes")"
marker_err=$(CCY_RESTART_RELAUNCH="$SID" ccy_restart_marker_take 2>&1)
check "…and a restart says how to resume by hand" "yes" \
    "$(grep -q -F "ccy --resume $SID" <<<"$marker_err" && echo yes || echo no)"

ccy_restart_refuse "$SID" "a test reason" 2>"$work/refuse.err"
check "ccy_restart_refuse gives the reason and the manual resume" "yes" \
    "$(grep -q 'a test reason' "$work/refuse.err" && grep -q -F "ccy --resume $SID" "$work/refuse.err" && echo yes || echo no)"

echo "=== ccy_handle_restart_exit (the launcher's own function, with stubs) ==="

# Lift the function out of the launcher so the real text is what runs, then drive it in a
# subshell against a throwaway project. The stand-in "launcher" it execs records its argv.
awk '/^ccy_handle_restart_exit\(\) \{$/ {p=1} p {print} p && /^}$/ {exit}' "$LAUNCHER" >"$work/handler.bash"
check "the function was found in the launcher" "yes" "$([ -s "$work/handler.bash" ] && echo yes || echo no)"

mkdir -p "$work/stub" "$work/tokens"
TOK="$work/tokens/work.2027-01-01.token"
cat >"$work/stub/claude-yolo" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$STUB_RECORD"
printf 'marker=%s passphrase-file=%s stdin-is-null=%s\n' "${CCY_RESTART_RELAUNCH:-unset}" \
    "${CCY_RESTORE_SSH_PASSPHRASE_FILE:-unset}" "$([ -t 0 ] && echo no || echo yes)" >"$STUB_RECORD.env"
STUB
chmod +x "$work/stub/claude-yolo"
cat >"$work/update-stub.bash" <<'STUB'
update_claude_inplace() { echo "$1" >>"$STUB_UPDATES"; return "$STUB_UPDATE_RC"; }
cleanup() { echo cleaned >>"$STUB_CLEANUPS"; }
STUB
# The driver runs the lifted function the way the launcher does: under `set -e`, in the
# project directory, holding the launch state a launcher that got this far would hold. Every
# input arrives in HC_* variables.
cat >"$work/driver.bash" <<'DRIVER'
set -e
print_error() { printf 'ERROR: %s\n' "$*" >&2; }
source "$HC_LIB_DIR/session-registry.bash"
source "$HC_LIB_DIR/restart-request.bash"
source "$HC_WORK/handler.bash"
source "$HC_WORK/update-stub.bash"
cd "$HC_PROJ"
SELECTED_TOKEN="$HC_TOKEN"
GITHUB_SSH_443="$HC_443"
NO_NETWORK_MODE="$HC_NO_NETWORK"
AUTO_CONNECT_NETWORK="$HC_NETWORK"
RESTORE_SSH_PASSPHRASE_FILE="$HC_PP_FILE"
SSH_KEYS=()
if [ -n "$HC_KEYS" ]; then
    IFS=: read -r -a SSH_KEYS <<<"$HC_KEYS"
fi
CCY_ORIG_ARGS=("$@")
ccy_handle_restart_exit
echo "returned"
DRIVER

# handler_case <name> <request-body|-> <update-rc> <history-lines> <orig-args...>
# Prints "rc=<status> update=<n> exec=<yes|no> request=<present|gone>" and leaves stderr in
# $work/<name>.err, the exec argv in $work/<name>.argv and its environment in .argv.env.
# The launch state defaults to a token, no keys and no network; a caller overrides it by
# prefixing HC_TOKEN, HC_KEYS (colon separated), HC_NETWORK, HC_443, HC_NO_NETWORK,
# HC_PP_FILE or HC_HISTORY_DIR. stdin is closed and the run is bounded, so a handler that
# waited for anything would show here as rc=124, not hang the suite.
handler_case() {
    local name="$1" body="$2" update_rc="$3" history="$4"
    shift 4
    local proj="$work/proj-$name" hist_dir="${HC_HISTORY_DIR:-$work/history-$name}"
    mkdir -p "$proj/.claude/ccy/state"
    if [ "$history" -gt 0 ]; then
        mkdir -p "$hist_dir"
        local i hist_file
        hist_file="$hist_dir/$(printf '%s' "$proj" | md5sum | cut -c1-16)"
        for ((i = 0; i < history; i++)); do
            date +%s >>"$hist_file"
        done
    fi
    [ "$body" = "-" ] || printf '%s' "$body" >"$proj/.claude/ccy/state/restart-request.json"
    local rc=0
    SCRIPT_DIR="$work/stub" VERSION_CHECK_CACHE="$work/cache-$name" IMAGE_NAME="img:tag" \
        CCY_RESTART_HISTORY_DIR="$hist_dir" \
        HC_LIB_DIR="$LIB_DIR" HC_WORK="$work" HC_PROJ="$proj" \
        HC_TOKEN="${HC_TOKEN-$TOK}" HC_KEYS="${HC_KEYS:-}" HC_NETWORK="${HC_NETWORK:-}" \
        HC_443="${HC_443:-0}" HC_NO_NETWORK="${HC_NO_NETWORK:-false}" HC_PP_FILE="${HC_PP_FILE:-}" \
        STUB_RECORD="$work/$name.argv" STUB_UPDATES="$work/$name.updates" \
        STUB_UPDATE_RC="$update_rc" STUB_CLEANUPS="$work/$name.cleanups" \
        timeout 60 bash "$work/driver.bash" "$@" </dev/null >"$work/$name.out" 2>"$work/$name.err" || rc=$?
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
check "…the relaunch carries the token file the session used, not the name it was given" \
    "--token|$TOK|--no-ssh|--resume|$SID" "$(paste -sd'|' "$work/ok.argv")"
check "…the update was of the launcher's image" "img:tag" "$(cat "$work/ok.updates")"
check "…the relaunch is marked unattended, with its stdin closed here" \
    "marker=$SID passphrase-file=unset stdin-is-null=yes" "$(cat "$work/ok.argv.env")"
check "…this launch's staged files are cleaned up before the exec (exec skips the EXIT trap)" \
    "cleaned" "$(cat "$work/ok.cleanups")"
check "…the budget history is created in its own directory" "1" \
    "$(cat "$work/history-ok/"* | wc -l)"
check "…and nothing of it is in the disposable update-check cache" "no" \
    "$([ -e "$work/cache-ok" ] && find "$work/cache-ok" -name '*history*' | grep -q . && echo yes || echo no)"

# A launch where every choice was made at a prompt: none of them is in the original argv.
check "an interactive launch: the relaunch still runs with nobody there" \
    "rc=0 update=1 exec=yes request=gone" \
    "$(HC_KEYS="$work/key-open" HC_NETWORK=proj-net HC_443=1 handler_case asked "$GOOD" 0 0 --max-age 3d)"
check "…and carries the token, key, 443 mode and network chosen at the prompts" \
    "--token|$TOK|--ssh-key|$work/key-open|--github-443|--network|proj-net|--max-age|3d|--resume|$SID" \
    "$(paste -sd'|' "$work/asked.argv")"
mapfile -t asked_argv <"$work/asked.argv"
check "a restart of a restart relaunches with the same arguments, not a growing list" \
    "rc=0 update=1 exec=yes request=gone" \
    "$(HC_KEYS="$work/key-open" HC_NETWORK=proj-net HC_443=1 handler_case again "$GOOD" 0 0 "${asked_argv[@]}")"
check "…argv identical" "$(paste -sd'|' "$work/asked.argv")" "$(paste -sd'|' "$work/again.argv")"
check "a forwarded agent and --no-network are carried" \
    "--token|$TOK|--ssh-agent|--no-network|--resume|$SID" \
    "$(HC_KEYS="$SSH_AGENT_SENTINEL" HC_NO_NETWORK=true handler_case agent "$GOOD" 0 0 >/dev/null
        paste -sd'|' "$work/agent.argv")"

check "a key that needs a passphrase stops the restart before the budget or an update" \
    "rc=1 update=0 exec=no request=gone" \
    "$(HC_KEYS="$work/key-locked" handler_case locked "$GOOD" 0 0)"
check "…names the key and the manual resume" "yes" \
    "$(grep -q -F "$work/key-locked" "$work/locked.err" && grep -q -F "ccy --resume $SID" "$work/locked.err" && echo yes || echo no)"
check "…and spends no budget" "no" "$([ -d "$work/history-locked" ] && echo yes || echo no)"
check "the same key with a server restore's passphrase file relaunches" \
    "rc=0 update=1 exec=yes request=gone" \
    "$(HC_KEYS="$work/key-locked" HC_PP_FILE="$work/pp-file" handler_case ppfile "$GOOD" 0 0)"
check "…and hands the file's path on, never its content" \
    "marker=$SID passphrase-file=$work/pp-file stdin-is-null=yes" "$(cat "$work/ppfile.argv.env")"

check "a session with no token on record stops before the budget or an update" \
    "rc=1 update=0 exec=no request=gone" "$(HC_TOKEN="" handler_case notoken "$GOOD" 0 0)"
check "…and names the manual resume" "yes" \
    "$(grep -q -F "ccy --resume $SID" "$work/notoken.err" && echo yes || echo no)"

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

check "a history directory that cannot be created stops the restart" \
    "rc=1 update=0 exec=no request=gone" \
    "$(HC_HISTORY_DIR="$work/key-open/history" handler_case nohist "$GOOD" 0 0)"

check "a failed image update stops the restart, no relaunch" \
    "rc=1 update=1 exec=no request=gone" "$(handler_case updfail "$GOOD" 1 0)"

echo "=== a relaunch's arguments, read by the launcher's own parser and prompt gates ==="

# The parser and its defaults, lifted out of the launcher, fed the argv the handler built
# above for a launch whose every choice was made at a prompt. Then each prompt's own `if`,
# lifted out the same way, says whether the relaunch would enter it.
awk '/^FORCE_REBUILD=false$/ {p=1} p {print} p && /^done$/ {exit}' "$LAUNCHER" >"$work/parser.bash"
check "the launcher's argument parser was found" "yes" \
    "$(grep -q '^for arg in "\$@"; do$' "$work/parser.bash" && echo yes || echo no)"

# The first line of each gate's `if`, exactly as the launcher has it, after the gate's name.
#   quick-launch  enters = the Quick Launch question is asked
#   key-picker    enters = the SSH key picker runs
#   token-flag    enters = a given token is used; skipping it leads to the token picker
#   network-flag  enters = a given network is used; skipping it leads to network detection
cat >"$work/gate-starts" <<'STARTS'
quick-launch if [[ "$NO_SSH_MODE" = false ]] && [[ ${#SSH_KEYS[@]} -eq 0 ]] && \
key-picker if [ "$NO_SSH_MODE" = false ] && [ ${#SSH_KEYS[@]} -eq 0 ]; then
token-flag if [ -n "$SPECIFIED_TOKEN" ]; then
network-flag if [[ -n "$SPECIFIED_NETWORK" ]]; then
STARTS
{
    printf 'print_error() { printf "ERROR: %%s\\n" "$*" >&2; }\n'
    cat "$work/parser.bash"
} >"$work/gates.bash"
gates_found=0
while read -r gate start; do
    # ENVIRON, not -v: awk -v would read the backslash ending the quick-launch line as an escape.
    found=$(start="$start" awk '$0 == ENVIRON["start"] {p=1} p {print} p && / then$/ {exit}' "$LAUNCHER")
    [ -z "$found" ] || gates_found=$((gates_found + 1))
    printf '%s\n    echo %s=enters\nelse\n    echo %s=skips\nfi\n' "$found" "$gate" "$gate" >>"$work/gates.bash"
done <"$work/gate-starts"
check "all four prompt gates were found in the launcher" "4" "$gates_found"

# gates <args...> → each gate's verdict for a launch with these arguments, space separated.
gates() {
    bash "$work/gates.bash" "$@" </dev/null 2>"$work/gates.err" | paste -sd' '
}
check "an ordinary launch with no flags meets every prompt (the gates are live)" \
    "quick-launch=enters key-picker=enters token-flag=skips network-flag=skips" "$(gates)"
check "the relaunch of an all-prompts launch meets none" \
    "quick-launch=skips key-picker=skips token-flag=enters network-flag=enters" \
    "$(gates "${asked_argv[@]}")"
check "a relaunch with no keys and no project network meets none of the pickers" \
    "quick-launch=skips key-picker=skips token-flag=enters network-flag=skips" \
    "$(gates --token "$TOK" --no-ssh --resume "$SID")"

echo "=== launcher wiring ==="

check "launcher sources the library" "1" \
    "$(grep -c -F "source \"\$SCRIPT_DIR/lib/restart-request.bash\"" "$LAUNCHER")"
check "launcher lists the library in CCY_LIBS" "1" \
    "$(grep -c -E '^CCY_LIBS=\(.* restart-request( |\))' "$LAUNCHER")"
check "launcher captures the container status instead of dying on it" "1" \
    "$(grep -c -F 'container_rc=$?' "$LAUNCHER")"
check "launcher discards a stale request before the run" "1" \
    "$(grep -c -E '^ccy_restart_request_discard "[^"]*CCY_RESTART_REQUEST_REL" [|][|] exit 1$' "$LAUNCHER")"

check "the restart history has its own directory under ~/.cache" "1" \
    "$(grep -c -x -F "CCY_RESTART_HISTORY_DIR=\"\$HOME/.cache/claude-yolo-restart-history\"" "$LAUNCHER")"
check "…which no rebuild deletes" "0" "$(grep -c -F "rm -rf \"\$CCY_RESTART_HISTORY_DIR\"" "$LAUNCHER")"
check "…and the handler keeps no history in the update-check cache" "0" \
    "$(grep -c -F 'VERSION_CHECK_CACHE/restart-history' "$work/handler.bash")"

# Every prompt on the launch path must fall where a restart's stdin is /dev/null: after the
# restart mark is read and before the terminal is handed back, which is before the container
# runs. The one prompt after the container (stopping compose services) is outside the window.
marker_line=$(grep -n -x 'ccy_restart_marker_take || exit 1' "$LAUNCHER" | cut -d: -f1)
restore_line=$(grep -n -F "exec <&\"\$CCY_RESTART_TTY_FD\" {CCY_RESTART_TTY_FD}<&-" "$LAUNCHER" | cut -d: -f1)
run_line=$(grep -n -E "^container_cmd run \\\$DOCKER_FLAGS --rm" "$LAUNCHER" | cut -d: -f1)
check "the launcher reads the restart mark, closes stdin and hands it back once each" "1 1 1" \
    "$(grep -c -x 'ccy_restart_marker_take || exit 1' "$LAUNCHER") $(grep -c -F 'exec {CCY_RESTART_TTY_FD}<&0 </dev/null' "$LAUNCHER") $(grep -c -F "exec <&\"\$CCY_RESTART_TTY_FD\"" "$LAUNCHER")"
mapfile -t prompt_lines < <(grep -n -E 'read -r?p ' "$LAUNCHER" | cut -d: -f1)
launch_prompts=0 outside=0
for line in "${prompt_lines[@]}"; do
    [ "$line" -lt "$run_line" ] || continue
    launch_prompts=$((launch_prompts + 1))
    if [ "$line" -le "$marker_line" ] || [ "$line" -ge "$restore_line" ]; then
        outside=$((outside + 1))
        echo "    prompt outside the closed-stdin window at launcher line $line"
    fi
done
check "all $launch_prompts launch-path prompts in the launcher fall inside the closed-stdin window" \
    "0 yes" "$outside $([ "$launch_prompts" -gt 0 ] && [ "$restore_line" -lt "$run_line" ] && echo yes || echo no)"

check "the network chain has a restart branch between --no-network and detection" "yes" \
    "$(awk '/^elif \[\[ "\$NO_NETWORK_MODE" = true \]\]; then$/ {a=NR}
            /^elif \[\[ -n "\$CCY_RESTART_RELAUNCH_SESSION" \]\]; then$/ {b=NR}
            /# No network flags - check for persisted network preference first/ {c=NR}
            END {print (a && b && c && a < b && b < c) ? "yes" : "no"}' "$LAUNCHER")"
check "a restart rejoins its network without the compose-start offer" "1" \
    "$(grep -c -F "|| check_and_start_compose_services \"\$SPECIFIED_NETWORK\" \"\$PROJECT_NAME\"; then" "$LAUNCHER")"
check "zombie and sibling containers take the unattended answer on a restart too" "1 1" \
    "$(grep -c -F "check_zombie_containers_startup \"yolo\" \"\$CCY_UNATTENDED_LAUNCH\"" "$LAUNCHER") $(grep -c -F "check_project_containers_startup \"\$PROJECT_NAME\" \"yolo\" \"\$CCY_UNATTENDED_LAUNCH\"" "$LAUNCHER")"
check "a restart may use a server restore's passphrase file" "1" \
    "$(grep -c -x -F "ccy_restore_passphrase_take \"\$CCY_UNATTENDED_LAUNCH\" || exit 1" "$LAUNCHER")"
check "the old-sessions migration is not asked on a restart" "1" \
    "$(grep -c -F "if [ -d \".claude/ccy/sessions\" ] && [ -z \"\$CCY_RESTART_RELAUNCH_SESSION\" ]; then" "$LAUNCHER")"
check "keys, an expired token and a failed token refuse a restart rather than ask" "3" \
    "$(grep -c -F "ccy_restart_refuse \"\$CCY_RESTART_RELAUNCH_SESSION\"" "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
