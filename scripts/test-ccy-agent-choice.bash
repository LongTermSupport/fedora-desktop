#!/usr/bin/env bash
# Unit-test ccy --opencode (-oc), lib/agent-choice.bash (Plan 00215 decisions O1, O2), and the
# session's container run under its network policy, lib/network-policy.bash (Plan 00232).
#
# Sources the library from THIS repo, and checks the launcher calls it where the flag is meant to
# act, so the text under test is the text that ships.
#
# WHY THIS TEST EXISTS. The flag changes only which agent the container runs, so each case below
# is a way it could change more (a claude launch whose command or banner moves, a provider key
# exported to every program the launcher starts) or quietly do less (a launch with no key that
# starts anyway, an option opencode cannot honour that is dropped instead of refused).
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB="$CCY_DIR/lib/agent-choice.bash"
POLICY_LIB="$CCY_DIR/lib/network-policy.bash"
LAUNCHER="$CCY_DIR/claude-yolo"

if [ ! -f "$LIB" ]; then
    echo "FAIL: library not found at $LIB" >&2
    exit 1
fi
print_error() { printf 'ERROR: %s\n' "$*" >&2; }

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

# fresh — the library loaded anew, with the launcher's parse state at its defaults.
fresh() {
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/agent-choice.bash
    source "$LIB"
    # shellcheck source-path=SCRIPTDIR source=../files/var/local/claude-yolo/lib/network-policy.bash
    source "$POLICY_LIB"
    CLAUDE_ARGS=() CLAUDE_CMD_ARGS=() PROMPT_TEXT="" HEADLESS_MODE=false SUPERVISE_MODE=false
    NO_SUPERVISE_MODE=false NO_BUS_MODE=false TEAMS_GIVEN=false OPT_MAX_AGE="" OPT_RUN_FOR=""
    OPT_UNTIL="" CLAUDE_ARGS_BEFORE_SEPARATOR=""
    unset CCY_MAX_AGE
    CCY_ROOT="$work/ccy-$RANDOM$RANDOM"
    mkdir -p "$CCY_ROOT/provider-keys"
}

# key <name> [content] [mode] — a provider key file.
key() {
    printf '%s\n' "${2:-sk-test-value}" >"$CCY_ROOT/provider-keys/$1"
    chmod "${3:-600}" "$CCY_ROOT/provider-keys/$1"
}

# options — ccy_agent_options run as the launcher runs it: its status, then its stderr.
options() {
    local rc=0
    ccy_agent_options 2>"$work/err" || rc=$?
    printf '%s' "$rc"
}

echo "=== the flag ==="
fresh
for a in --model haiku -oc "fix it"; do ccy_agent_arg "$a"; done
check "-oc chooses opencode" "opencode" "$CCY_AGENT"
check "-oc is not passed on; the other arguments are, in order" "--model|haiku|fix it" "$(IFS='|'; echo "${CLAUDE_ARGS[*]}")"
fresh
ccy_agent_arg --opencode
check "--opencode chooses opencode" "opencode" "$CCY_AGENT"
fresh
for a in --resume -o -c --opencodes; do ccy_agent_arg "$a"; done
check "with neither spelling the agent is claude" "claude" "$CCY_AGENT"
check "near spellings are claude's arguments" "--resume -o -c --opencodes" "${CLAUDE_ARGS[*]}"

echo "=== claude: nothing changes ==="
fresh
check "options leave a claude launch alone" "0" "$(options)"
ccy_agent_options 2>/dev/null
check "no bus, supervisor or flag-check change for claude" "false false " "$NO_BUS_MODE $NO_SUPERVISE_MODE $CLAUDE_ARGS_BEFORE_SEPARATOR"
check "no container arguments for claude" "0" "${#CCY_AGENT_RUN_ARGS[@]}"
CLAUDE_CMD_ARGS=(--append-system-prompt "hello there" --resume)
rule="════════════════════════════════════════════════════════════════════════════════"
want=$(printf '%s\n' "$rule" "Executing Claude Code with arguments:" "  claude --dangerously-skip-permissions --append-system-prompt hello there --resume" "$rule" "")
ccy_agent_command >"$work/out"
check "the claude banner is the launcher's, byte for byte" "$want" "$(cat "$work/out")"
check "the claude command is claude's, every argument kept" "claude|--dangerously-skip-permissions|--append-system-prompt|hello there|--resume" "$(IFS='|'; echo "${CCY_AGENT_CMD[*]}")"
CLAUDE_CMD_ARGS=()
ccy_agent_command >"$work/out"
check "an empty claude argument list keeps the banner's trailing space" "  claude --dangerously-skip-permissions " "$(awk 'NR == 3' "$work/out")"

echo "=== opencode: what it cannot be given is refused ==="
for clash in HEADLESS_MODE:--headless SUPERVISE_MODE:--supervise TEAMS_GIVEN:--teams; do
    fresh
    key OPENROUTER_API_KEY
    CCY_AGENT=opencode
    printf -v "${clash%%:*}" true
    check "${clash#*:} is refused (64)" "64" "$(options)"
    check "${clash#*:} is named" "1" "$(grep -c -- "--opencode cannot be used with ${clash#*:}" "$work/err")"
done
for var in OPT_MAX_AGE OPT_RUN_FOR OPT_UNTIL CCY_MAX_AGE; do
    fresh
    key OPENROUTER_API_KEY
    CCY_AGENT=opencode
    printf -v "$var" 2h
    check "a session limit ($var) is refused (64)" "64" "$(options)"
done

fresh
key OPENROUTER_API_KEY
CCY_AGENT=opencode
CLAUDE_ARGS=("fix it" --model x)
check "a bare first message is refused (64)" "64" "$(options)"
check "it is told to use --prompt" "1" "$(grep -c -- "give it with --prompt" "$work/err")"
fresh
key OPENROUTER_API_KEY
CCY_AGENT=opencode
CLAUDE_ARGS=(--model "openrouter/x")
check "a flag's value is not a first message" "0" "$(options)"

echo "=== opencode: the provider key ==="
fresh
CCY_AGENT=opencode
check "no key fails (1)" "1" "$(options)"
check "the message names the directory" "1" "$(grep -c -F "$CCY_ROOT/provider-keys holds none" "$work/err")"
check "the message says how to add one" "1" "$(grep -c -F '_API_KEY' "$work/err")"
fresh
CCY_AGENT=opencode
rmdir "$CCY_ROOT/provider-keys"
check "no key directory fails (1)" "1" "$(options)"
for bad in "lower_api_key" "PATH" "GH_TOKEN" "OPENROUTER_API_KEY.bak"; do
    fresh
    CCY_AGENT=opencode
    key "$bad"
    check "a file named $bad is refused, not skipped" "1" "$(options)"
done
fresh
CCY_AGENT=opencode
key OPENROUTER_API_KEY sk-x 644
check "a key others can read is refused" "1" "$(options)"
check "the mode it needs is named" "1" "$(grep -c 'mode 0600' "$work/err")"
fresh
CCY_AGENT=opencode
key OPENROUTER_API_KEY "$(printf 'one\ntwo')"
check "a key of two lines is refused" "1" "$(options)"
fresh
CCY_AGENT=opencode
: >"$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
chmod 600 "$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
check "an empty key is refused" "1" "$(options)"
fresh
CCY_AGENT=opencode
key OPENROUTER_API_KEY "   "
check "a blank key is refused" "1" "$(options)"
fresh
CCY_AGENT=opencode
printf 'sk-x\r\n' >"$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
chmod 600 "$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
check "a key with a Windows line ending is refused" "1" "$(options)"
fresh
CCY_AGENT=opencode
key ANTHROPIC_API_KEY
ln -s "$CCY_ROOT/provider-keys/ANTHROPIC_API_KEY" "$CCY_ROOT/provider-keys/OPENAI_API_KEY"
check "a key that is a link is refused" "1" "$(options)"

echo "=== opencode: a launch with keys ==="
fresh
CCY_AGENT=opencode
key ANTHROPIC_API_KEY sk-ant-test
key OPENROUTER_API_KEY sk-or-test
check "two keys pass" "0" "$(options)"
ccy_agent_options 2>/dev/null
check "each key goes to the container by name only" "-e ANTHROPIC_API_KEY -e OPENROUTER_API_KEY" "${CCY_AGENT_RUN_ARGS[*]}"
check "no bus kit, no supervisor, no claude flag check" "true true 0" "$NO_BUS_MODE $NO_SUPERVISE_MODE $CLAUDE_ARGS_BEFORE_SEPARATOR"
CLAUDE_ARGS=(--model openrouter/x) CLAUDE_CMD_ARGS=(--append-system-prompt startup)
PROMPT_TEXT="fix the bug"
ccy_agent_command >"$work/out"
check "opencode runs with --auto, the prompt and the arguments, not claude's" "opencode|--auto|--prompt|fix the bug|--model|openrouter/x" "$(IFS='|'; echo "${CCY_AGENT_CMD[*]}")"
check "the banner names opencode" "Executing opencode with arguments:" "$(awk 'NR == 2' "$work/out")"
CONTAINER_ENGINE=podman CONTAINER_NAME=proj_yolo IMAGE_NAME=img
ccy-network-policy() { env >"$work/child-env"; printf '%s\n' "$@" >"$work/child-args"; }
check "the run is the engine's own status" "0" "$(ccy_agent_container run --rm img; echo $?)"
check "the run goes through ccy-network-policy, its arguments as given" "launch --name proj_yolo --image img -- podman run --rm img" "$(tr '\n' ' ' <"$work/child-args" | awk '{$1=$1; print}')"
check "the engine gets each key's value" "ANTHROPIC_API_KEY=sk-ant-test OPENROUTER_API_KEY=sk-or-test" "$(grep -E '^(ANTHROPIC|OPENROUTER)_API_KEY=' "$work/child-env" | sort | tr '\n' ' ' | awk '{$1=$1; print}')"
ccy_agent_container run >/dev/null
check "after the run no key is set in the launcher" "unset unset" "${ANTHROPIC_API_KEY-unset} ${OPENROUTER_API_KEY-unset}"
check "nothing the launcher starts later inherits a key" "0" "$(env | grep -c '_API_KEY=')"
ccy-network-policy() { env >"$work/child-env"; }
(set -x; ccy_agent_container run) 2>"$work/trace"
check "under --debug's set -x no key reaches the trace" "0" "$(grep -c -e sk-ant-test -e sk-or-test "$work/trace")"
check "  the trace still shows the engine call" "1" "$(grep -c -E '^\++ ccy_network_policy_launch run$' "$work/trace")"
check "  and the engine still gets the key" "1" "$(grep -c '^OPENROUTER_API_KEY=sk-or-test$' "$work/child-env")"
check "  and set -x is on again after the call" "on" "$(set -x; ccy_agent_container run 2>/dev/null; case $- in *x*) echo on ;; *) echo off ;; esac)"
ccy-network-policy() { return 3; }
check "a failed run's status comes back" "3" "$(ccy_agent_container run; echo $?)"
ccy-network-policy() { echo started >"$work/started"; }
rm -f "$work/started"
: >"$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
check "a key gone blank since the check stops the run (1)" "1" "$(ccy_agent_container run 2>/dev/null; echo $?)"
check "the engine is not started without it" "no" "$([ -e "$work/started" ] && echo yes || echo no)"
rm -f "$CCY_ROOT/provider-keys/OPENROUTER_API_KEY"
check "a key gone since the check stops the run (1)" "1" "$(ccy_agent_container run 2>/dev/null; echo $?)"
key OPENROUTER_API_KEY sk-or-test
unset -f ccy-network-policy
check "no ccy-network-policy installed stops the run (1)" "1" "$(PATH="$work/empty-bin" ccy_agent_container run 2>/dev/null; echo $?)"
check "  and says to run the play" "1" "$(PATH="$work/empty-bin" ccy_agent_container run 2>&1 | grep -c 'ccy-network-policy is not installed.*play-claude-yolo.yml')"

echo "=== --connect and --disconnect refresh the live sessions' policies ==="
connect_to_network() { echo "connect $*" >>"$work/net-calls"; return "${net_rc:-0}"; }
disconnect_from_network() { echo "disconnect $*" >>"$work/net-calls"; return "${net_rc:-0}"; }
ccy-network-policy() { echo "policy $*" >>"$work/net-calls"; return "${policy_rc:-0}"; }
net_calls() { tr '\n' '|' <"$work/net-calls"; }
: >"$work/net-calls"
check "both at once are refused (1)" "1" "$(ccy_network_change true true a b 2>/dev/null; echo $?)"
check "  and nothing is changed" "" "$(net_calls)"
check "--connect, then the refresh" "0 connect a _yolo ccy|policy refresh|" "$(ccy_network_change true false a b; echo "$? $(net_calls)")"
: >"$work/net-calls"
check "--disconnect, then the refresh" "0 disconnect b _yolo ccy|policy refresh|" "$(ccy_network_change false true a b; echo "$? $(net_calls)")"
: >"$work/net-calls"
check "a failed connect's status comes back, and nothing is refreshed" "4 connect a _yolo ccy|" "$(net_rc=4 ccy_network_change true false a b; echo "$? $(net_calls)")"
: >"$work/net-calls"
check "a failed refresh's status comes back" "5" "$(policy_rc=5 ccy_network_change false true a b; echo $?)"

echo "=== the launcher calls it ==="
check "the parse's last branch is the flag" "1" "$(grep -c -x -F "        ccy_agent_arg \"\$arg\"" "$LAUNCHER")"
check "the options follow the parse, before the bus and flag checks" "yes" "$(awk '/^ccy_agent_options [|][|] exit [$][?]$/ {o=NR} /^ccy_no_bus_check / {b=NR} /^suspect_flags=[(][)]$/ {s=NR} END {print (o && o < b && b < s) ? "yes" : "no"}' "$LAUNCHER")"
check "the container runs the chosen agent" "1" "$(grep -c -x -F "    \"\${CCY_AGENT_CMD[@]}\" || container_rc=\$?" "$LAUNCHER")"
check "the run goes through the key hand-over" "1" "$(grep -c -F "GH_TOKEN=\"\$GH_TOKEN\" ccy_agent_container run " "$LAUNCHER")"
check "--connect and --disconnect go through the policy refresh" "1" "$(grep -c -x -F '    ccy_network_change "$CONNECT_MODE" "$DISCONNECT_MODE" "$CONNECT_NETWORK" "$DISCONNECT_NETWORK"' "$LAUNCHER")"
check "  and nothing else connects or disconnects" "0" "$(grep -c -E '^ *(dis)?connect_(to|from)_network ' "$LAUNCHER")"
check "the keys' -e arguments are in the run" "1" "$(grep -c -F "\"\${CCY_AGENT_RUN_ARGS[@]}\"" "$LAUNCHER")"
check "--opencode is offered as a ccy flag in the did-you-mean list" "1" "$(grep -c -- '--prevent --opencode --help' "$LAUNCHER")"
# The Quick Launch block itself, with no bus kit, is run by test-ccy-teams.bash; the save is
# outside that block.
check "a saved team a no-bus Quick Launch kept is written back for the next launch" "1" "$(grep -c -x -F "LAST_TEAMS=\"\${CCY_SEATS:-\${quick_kept_team:-}}\"" "$LAUNCHER")"
check "--help names both spellings" "1" "$(grep -c -- '^  --opencode, -oc ' "$LAUNCHER")"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
