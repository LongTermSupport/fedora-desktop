#!/usr/bin/env bash
# Unit-test the host and container halves of --max-age / --run-for / --until.
#
# Sources the libraries from THIS repo (not the deployed /var/local copy). The launcher's and
# the entrypoint's own functions are lifted out of their files, so the text under test is the
# text that ships.
#
# WHY THIS TEST EXISTS. The options decide when a session is restarted or told to stop, so a
# value that is read wrongly (a typo taken as "off", a deadline that restarts at every relaunch,
# a wrapper line altered when no option was given) costs a session. Every refusal below is a way
# the options could be silently wrong. The plugin itself is tested in
# tests/helpers/ccy_lifecycle/test_plugin.py.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
LIB_DIR="$CCY_DIR/lib"
LAUNCHER="$CCY_DIR/claude-yolo"
ENTRYPOINT="$CCY_DIR/entrypoint.sh"
DOCKERFILE="$CCY_DIR/Dockerfile"

for lib in session-registry restart-request session-lifecycle; do
    if [ ! -f "$LIB_DIR/$lib.bash" ]; then
        echo "FAIL: library not found at $LIB_DIR/$lib.bash" >&2
        exit 1
    fi
done
print_error() { printf 'ERROR: %s\n' "$*" >&2; }
# shellcheck source=../files/var/local/claude-yolo/lib/session-registry.bash
source "$LIB_DIR/session-registry.bash"
# shellcheck source=../files/var/local/claude-yolo/lib/restart-request.bash
source "$LIB_DIR/restart-request.bash"
# shellcheck source=../files/var/local/claude-yolo/lib/session-lifecycle.bash
source "$LIB_DIR/session-lifecycle.bash"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# The launcher validates from the project directory, where it looks for a supervisor that takes
# --plugin. Every case runs in a stand-in project whose supervisor declares the plugin API.
mkdir -p "$work/project/.claude/ccy"
printf '_PLUGIN_API_MAJOR = 1\n' >"$work/project/.claude/ccy/claude-supervise.py"
cd "$work/project" || exit 1

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

# yes_if_grep <pattern> <file> — "yes" when the file holds the fixed string.
has() { if grep -q -F -- "$1" "$2"; then echo yes; else echo no; fi; }

# A clean slate for every validate call: nothing inherited from the shell running the test.
reset_env() {
    unset CCY_MAX_AGE CCY_RESTART_WARN_MINUTES CCY_RELAUNCH_DEADLINE_EPOCH
    unset CCY_LIFECYCLE_MAX_AGE_SECONDS CCY_LIFECYCLE_DEADLINE_EPOCH CCY_LIFECYCLE_WARN_MINUTES
    unset CCY_LIFECYCLE_RUN_FOR_SECONDS CCY_LIFECYCLE_UNTIL_TEXT CCY_LIFECYCLE_LAUNCH_ID
    unset CCY_CLAUDE_WRAPPER
}

# validate <max-age> <run-for> <until> [no-supervise] — prints the outcome as one line.
validate() {
    reset_env
    local rc
    ccy_lifecycle_validate_options "$1" "$2" "$3" "${4:-false}" 2>"$work/validate.err"
    rc=$?
    printf 'rc=%s age=%s run=%s until=%s dl=%s warn=%s' "$rc" "${CCY_LIFECYCLE_MAX_AGE_SECONDS:-}" \
        "${CCY_LIFECYCLE_RUN_FOR_SECONDS:-}" "${CCY_LIFECYCLE_UNTIL_TEXT:-}" \
        "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}" "${CCY_LIFECYCLE_WARN_MINUTES:-}"
}

# nul_to_bar — a NUL separated argv as one line, words joined with '|'.
nul_to_bar() { tr '\0' '|' | awk '{sub(/\|$/, ""); print}'; }

echo "=== ccy_lifecycle_parse_duration ==="

dur() { ccy_lifecycle_parse_duration "$1" test 2>/dev/null; }
check "90m" "5400" "$(dur 90m)"
check "12h" "43200" "$(dur 12h)"
check "3d" "259200" "$(dur 3d)"
check "1d12h" "129600" "$(dur 1d12h)"
check "1h30m" "5400" "$(dur 1h30m)"
check "2d3h4m" "183840" "$(dur 2d3h4m)"
check "leading zeros are decimal, not octal" "28800" "$(dur 08h)"
for bad in "" abc "3 days" 1h1d 5 -5m 1.5h 12H "3d!" 99999d 1h30 " 3d" "3d "; do
    got=$(ccy_lifecycle_parse_duration "$bad" --max-age 2>"$work/dur.err")
    rc=$?
    check "'$bad' is refused" "1" "$rc"
    check "'$bad' prints nothing on stdout" "" "$got"
    check "'$bad' says what is accepted" "yes" "$(has '90m, 12h, 3d or 1d12h' "$work/dur.err")"
done

echo "=== ccy_lifecycle_describe ==="
check "5400s" "1h30m" "$(ccy_lifecycle_describe 5400)"
check "129600s" "1d12h" "$(ccy_lifecycle_describe 129600)"
check "60s" "1m" "$(ccy_lifecycle_describe 60)"
check "2592000s" "30d" "$(ccy_lifecycle_describe 2592000)"

echo "=== ccy_lifecycle_parse_until (TZ=UTC, now = 12:00) ==="

NOW=$(date -u -d '2026-10-03 12:00:00' +%s)
until_at() { TZ=UTC ccy_lifecycle_parse_until "$1" "$NOW" 2>/dev/null; }
check "17:30 is later today" "$(date -u -d '2026-10-03 17:30:00' +%s)" "$(until_at 17:30)"
check "09:05 has passed, so it is tomorrow" "$(date -u -d '2026-10-04 09:05:00' +%s)" "$(until_at 09:05)"
check "12:00 is not in the future, so it is tomorrow" "$(date -u -d '2026-10-04 12:00:00' +%s)" "$(until_at 12:00)"
check "12:01 is today" "$(date -u -d '2026-10-03 12:01:00' +%s)" "$(until_at 12:01)"
check "00:00 is tomorrow" "$(date -u -d '2026-10-04 00:00:00' +%s)" "$(until_at 00:00)"
check "23:59 is today" "$(date -u -d '2026-10-03 23:59:00' +%s)" "$(until_at 23:59)"
for bad in "" 25:00 24:00 9:05 17:60 1730 17:5 "17:30:00" 17.30 noon " 17:30"; do
    got=$(TZ=UTC ccy_lifecycle_parse_until "$bad" "$NOW" 2>"$work/until.err")
    rc=$?
    check "'$bad' is refused" "1" "$rc"
    check "'$bad' prints nothing on stdout" "" "$got"
    check "'$bad' says what is accepted" "yes" "$(has 'HH:MM' "$work/until.err")"
done

echo "=== ccy_lifecycle_validate_options ==="

check "nothing given: every feature is off" "rc=0 age= run= until= dl= warn=" "$(validate '' '' '')"
check "--max-age 3d" "rc=0 age=259200 run= until= dl= warn=" "$(validate 3d '' '')"
check "--max-age at the 30m floor" "rc=0 age=1800 run= until= dl= warn=" "$(validate 30m '' '')"
check "--max-age at the 30d ceiling" "rc=0 age=2592000 run= until= dl= warn=" "$(validate 30d '' '')"
check "--max-age below the floor is refused" "rc=1 age= run= until= dl= warn=" "$(validate 29m '' '')"
check "…and says the range" "yes" "$(has 'between 30m and 30d' "$work/validate.err")"
check "--max-age above the ceiling is refused" "rc=1 age= run= until= dl= warn=" "$(validate 31d '' '')"
check "--max-age 0m is refused" "rc=1 age= run= until= dl= warn=" "$(validate 0m '' '')"
check "--max-age garbage is refused" "rc=1 age= run= until= dl= warn=" "$(validate soon '' '')"
check "--run-for 2h" "rc=0 age= run=7200 until= dl= warn=" "$(validate '' 2h '')"
check "--run-for 1m is the floor" "rc=0 age= run=60 until= dl= warn=" "$(validate '' 1m '')"
check "--run-for 0m is refused" "rc=1 age= run= until= dl= warn=" "$(validate '' 0m '')"
check "--run-for garbage is refused" "rc=1 age= run= until= dl= warn=" "$(validate '' later '')"
check "--until 17:30" "rc=0 age= run= until=17:30 dl= warn=" "$(validate '' '' 17:30)"
check "--until garbage is refused" "rc=1 age= run= until= dl= warn=" "$(validate '' '' 5pm)"
check "--run-for with --until is refused" "rc=1 age= run= until= dl= warn=" "$(validate '' 2h 17:30)"
check "…and says to use one" "yes" "$(has 'use one' "$work/validate.err")"
check "--max-age with --run-for is allowed" "rc=0 age=259200 run=7200 until= dl= warn=" "$(validate 3d 2h '')"
check "a feature with --no-supervise is refused" "rc=1 age=259200 run= until= dl= warn=" "$(validate 3d '' '' true)"
check "…and says why" "yes" "$(has 'no-supervise' "$work/validate.err")"
check "no feature with --no-supervise is fine" "rc=0 age= run= until= dl= warn=" "$(validate '' '' '' true)"

echo "=== the project's supervisor must take --plugin (checked before any prompt) ==="

# status <max-age> <run-for> <until> — the validator's exit status alone; stderr to validate.err.
status() {
    reset_env
    ccy_lifecycle_validate_options "$1" "$2" "$3" false 2>"$work/validate.err"
    echo $?
}

supervisor="$work/project/.claude/ccy/claude-supervise.py"
mv "$supervisor" "$work/supervisor.keep"
check "no supervisor: a feature is refused" "1" "$(status 3d '' '')"
check "…and says the project has no supervisor" "yes" "$(has 'no hooks-daemon supervisor' "$work/validate.err")"
check "no supervisor: no feature is still fine" "0" "$(status '' '' '')"
printf 'import argparse\n' >"$supervisor"
check "a supervisor without the plugin API: --until is refused" "1" "$(status '' '' 17:30)"
check "…and says to upgrade the hooks daemon" "yes" "$(has 'upgrade the hooks daemon' "$work/validate.err")"
printf '_PLUGIN_API_MAJOR = 2\n' >"$supervisor"
check "a supervisor on another plugin API major: refused" "1" "$(status 3d '' '')"
check "…and names both majors" "yes" "$(has 'API 2' "$work/validate.err")"
check "a host CCY_CLAUDE_WRAPPER is left to the entrypoint to judge" "0" "$(
    reset_env
    CCY_CLAUDE_WRAPPER='python3 /elsewhere/claude-supervise.py --' \
        ccy_lifecycle_validate_options 3d '' '' false 2>/dev/null
    echo $?
)"
mv "$work/supervisor.keep" "$supervisor"

echo "=== host defaults ==="

reset_env
export CCY_MAX_AGE=2d
got=$(
    ccy_lifecycle_validate_options "" "" "" false 2>/dev/null
    printf 'age=%s' "$CCY_LIFECYCLE_MAX_AGE_SECONDS"
)
check "CCY_MAX_AGE is the default for --max-age" "age=172800" "$got"
got=$(
    ccy_lifecycle_validate_options "6h" "" "" false 2>/dev/null
    printf 'age=%s' "$CCY_LIFECYCLE_MAX_AGE_SECONDS"
)
check "--max-age beats CCY_MAX_AGE" "age=21600" "$got"
export CCY_MAX_AGE=forever
ccy_lifecycle_validate_options "" "" "" false 2>"$work/default.err"
check "a bad CCY_MAX_AGE is refused" "1" "$?"
check "…naming the variable, not the option" "yes" "$(has 'CCY_MAX_AGE' "$work/default.err")"
reset_env

export CCY_RESTART_WARN_MINUTES=25
got=$(
    ccy_lifecycle_validate_options 3d "" "" false 2>/dev/null
    printf 'warn=%s' "$CCY_LIFECYCLE_WARN_MINUTES"
)
check "CCY_RESTART_WARN_MINUTES reaches the plugin" "warn=25" "$got"
got=$(
    ccy_lifecycle_validate_options "" "" "" false 2>/dev/null
    printf 'rc=%s warn=%s' "$?" "${CCY_LIFECYCLE_WARN_MINUTES:-}"
)
check "…and is ignored when there is no maximum age" "rc=0 warn=" "$got"
for bad in 0 241 abc -3 1000; do
    export CCY_RESTART_WARN_MINUTES="$bad"
    ccy_lifecycle_validate_options 3d "" "" false 2>/dev/null
    check "CCY_RESTART_WARN_MINUTES='$bad' is refused" "1" "$?"
done
export CCY_RESTART_WARN_MINUTES=""
ccy_lifecycle_validate_options 3d "" "" false 2>/dev/null
check "an empty CCY_RESTART_WARN_MINUTES means the default" "0" "$?"
export CCY_RESTART_WARN_MINUTES=30
ccy_lifecycle_validate_options 30m "" "" false 2>"$work/warn.err"
check "a warning as long as the maximum age is refused" "1" "$?"
check "…and says so" "yes" "$(has 'shorter than the maximum age' "$work/warn.err")"
reset_env

echo "=== a relaunch keeps the deadline ==="

reset_env
export CCY_RELAUNCH_DEADLINE_EPOCH=1790007200
got=$(
    ccy_lifecycle_validate_options 3d "" "" false 2>/dev/null
    printf 'dl=%s left=%s' "$CCY_LIFECYCLE_DEADLINE_EPOCH" "${CCY_RELAUNCH_DEADLINE_EPOCH:-gone}"
)
check "the carried deadline is taken, and consumed" "dl=1790007200 left=gone" "$got"
export CCY_RELAUNCH_DEADLINE_EPOCH=1790007200
got=$(
    ccy_lifecycle_validate_options "" 3h "" false 2>/dev/null
    printf 'dl=%s run=%s' "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}" "$CCY_LIFECYCLE_RUN_FOR_SECONDS"
)
check "an explicit --run-for beats the carried deadline" "dl= run=10800" "$got"
export CCY_RELAUNCH_DEADLINE_EPOCH=tomorrow
ccy_lifecycle_validate_options 3d "" "" false 2>/dev/null
check "a carried deadline that is not an epoch is refused" "1" "$?"
reset_env

echo "=== ccy_lifecycle_finalize ==="

reset_env
ccy_lifecycle_validate_options 3d 2h "" false 2>/dev/null
ccy_lifecycle_finalize 1790000000 2>"$work/final.err"
check "--run-for is added to the launch time" "1790007200" "$CCY_LIFECYCLE_DEADLINE_EPOCH"
check "it says what is in force" "yes" "$(has 'Session limits: restart after 3d; deadline at ' "$work/final.err")"
first_id="$CCY_LIFECYCLE_LAUNCH_ID"
ccy_lifecycle_finalize 1790000001 2>/dev/null
check "each launch gets its own id" "different" "$([ "$first_id" != "$CCY_LIFECYCLE_LAUNCH_ID" ] && echo different || echo same)"
check "the id is a shape the plugin accepts" "yes" \
    "$([[ "$CCY_LIFECYCLE_LAUNCH_ID" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{7,63}$ ]] && echo yes || echo no)"

reset_env
ccy_lifecycle_validate_options "" "" 09:05 false 2>/dev/null
TZ=UTC ccy_lifecycle_finalize "$NOW" 2>/dev/null
check "--until is resolved at launch" "$(date -u -d '2026-10-04 09:05:00' +%s)" "$CCY_LIFECYCLE_DEADLINE_EPOCH"

reset_env
ccy_lifecycle_validate_options "" "" "" false 2>/dev/null
ccy_lifecycle_finalize 1790000000 2>"$work/quiet.err"
check "no option: nothing is announced" "" "$(cat "$work/quiet.err")"
check "…and no deadline is set" "" "${CCY_LIFECYCLE_DEADLINE_EPOCH:-}"
reset_env

echo "=== the registry classifies the new flags ==="

SID="aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
check "--max-age is kept with its value" "keep-value" "$(ccy_registry_flag_class --max-age)"
check "--run-for is dropped with its value" "drop-value" "$(ccy_registry_flag_class --run-for)"
check "--until is dropped with its value" "drop-value" "$(ccy_registry_flag_class --until)"
# --token is a launch choice: the handler passes the token the session actually used, so the
# walk removes the typed one (scripts/test-ccy-restart-request.bash covers that half).
check "a relaunch keeps --max-age and drops --run-for" \
    "--max-age|3d|--resume|$SID" \
    "$(ccy_restart_relaunch_args "$SID" --token work --max-age 3d --run-for 2h -c | nul_to_bar)"
check "…--until too" \
    "--max-age|3d|--resume|$SID" \
    "$(ccy_restart_relaunch_args "$SID" --until 17:30 --max-age 3d | nul_to_bar)"
check "the value of --run-for is not mistaken for an opening message" \
    "--resume|$SID" "$(ccy_restart_relaunch_args "$SID" --run-for 2h | nul_to_bar)"

echo "=== the launcher's restart hand-off (its own function, with stubs) ==="

awk '/^ccy_handle_restart_exit\(\) \{$/ {p=1} p {print} p && /^}$/ {exit}' "$LAUNCHER" >"$work/handler.bash"
check "the handler was found in the launcher" "yes" "$([ -s "$work/handler.bash" ] && echo yes || echo no)"
mkdir -p "$work/stub"
cat >"$work/stub/claude-yolo" <<'STUB'
#!/usr/bin/env bash
{
    printf 'argv=%s\n' "$*"
    printf 'deadline=%s\n' "${CCY_RELAUNCH_DEADLINE_EPOCH:-unset}"
} >"$STUB_RECORD"
STUB
chmod +x "$work/stub/claude-yolo"
# Besides the update stub, the launch state the handler turns into the relaunch's choices: a
# token file, no keys, no network, and a restart history inside the case's own cache.
cat >"$work/update-stub.bash" <<STUB
update_claude_inplace() { return 0; }
cleanup() { :; }
SSH_AGENT_SENTINEL="$(awk -F'"' '/^readonly SSH_AGENT_SENTINEL=/ {print $2}' "$LIB_DIR/ssh-handling.bash")"
SELECTED_TOKEN="$work/token"
GITHUB_SSH_443=0
SSH_KEYS=()
NO_NETWORK_MODE=false
AUTO_CONNECT_NETWORK=""
RESTORE_SSH_PASSPHRASE_FILE=""
CCY_RESTART_HISTORY_DIR="\$VERSION_CHECK_CACHE/restart-history"
STUB

# relaunch_case <name> <deadline-epoch|-> <orig-args...> — runs the handler against a valid
# request, with restarted.json left beside it, and prints what became of the two files.
relaunch_case() {
    local name="$1" deadline="$2"
    shift 2
    local proj="$work/proj-$name"
    mkdir -p "$proj/.claude/ccy/state" "$work/cache-$name"
    printf '{"session_id": "%s", "reason": "max age", "requested_at": %s}' "$SID" "$(date +%s)" \
        >"$proj/.claude/ccy/state/restart-request.json"
    printf '{"session_id": "%s", "requested_at": %s}' "$SID" "$(date +%s)" \
        >"$proj/.claude/ccy/state/restarted.json"
    (
        cd "$proj" || exit 99
        # shellcheck source=/dev/null
        source "$work/handler.bash"
        export SCRIPT_DIR="$work/stub" VERSION_CHECK_CACHE="$work/cache-$name" IMAGE_NAME="img:tag"
        export STUB_RECORD="$work/$name.record"
        CCY_ORIG_ARGS=("$@")
        export CCY_ORIG_ARGS
        # shellcheck source=/dev/null
        source "$work/update-stub.bash"
        # The handler treats an empty deadline as none.
        if [ "$deadline" = "-" ]; then deadline=""; fi
        CCY_LIFECYCLE_DEADLINE_EPOCH="$deadline" ccy_handle_restart_exit
    ) >"$work/$name.out" 2>"$work/$name.err"
    local rc=$?
    local request_state=gone marker_state=gone
    [ -e "$proj/.claude/ccy/state/restart-request.json" ] && request_state=present
    [ -e "$proj/.claude/ccy/state/restarted.json" ] && marker_state=kept
    printf 'rc=%s request=%s restarted.json=%s' "$rc" "$request_state" "$marker_state"
}

check "a restart consumes the request and leaves restarted.json for the next supervisor" \
    "rc=0 request=gone restarted.json=kept" "$(relaunch_case a 1790007200 --max-age 3d --run-for 2h)"
check "…the relaunch carries the deadline and drops --run-for" \
    "argv=--token $work/token --no-ssh --max-age 3d --resume $SID|deadline=1790007200" "$(paste -sd'|' "$work/a.record")"
relaunch_case b - --max-age 3d >/dev/null
check "without a deadline nothing is carried" \
    "argv=--token $work/token --no-ssh --max-age 3d --resume $SID|deadline=unset" "$(paste -sd'|' "$work/b.record")"
check "outside comments the launcher never touches restarted.json" "0" \
    "$(grep -v '^[[:space:]]*#' "$LAUNCHER" | grep -c -F 'restarted.json')"
check "the library never touches it either" "0" \
    "$(grep -v '^[[:space:]]*#' "$LIB_DIR/restart-request.bash" | grep -c -F 'restarted.json')"

echo "=== the entrypoint's wrapper line (its own functions, exec replaced by a printer) ==="

awk '/^ccy_lifecycle_wanted\(\) \{$/ {p=1} p {print}' "$ENTRYPOINT" >"$work/tail.bash"
check "the entrypoint's tail was found" "yes" "$([ -s "$work/tail.bash" ] && echo yes || echo no)"
mkdir -p "$work/plugins"
printf '# plugin\nPLUGIN_API = 1\n' >"$work/plugins/ccy_lifecycle.py"
awk -v dir="$work/plugins" '{ gsub("/opt/claude-yolo/supervisor-plugins", dir) } 1' "$work/tail.bash" >"$work/tail-test.bash"
check "the plugin path was redirected for the test" "yes" "$(has "$work/plugins/ccy_lifecycle.py" "$work/tail-test.bash")"

# A function named exec takes precedence over the builtin, so the tail's final exec prints
# its argv instead of replacing the shell.
cat >"$work/exec-stub.bash" <<'STUB'
exec() {
    printf '%s\n' "$@"
    builtin exit 0
}
STUB

# wrapper_case <wrapper|-> <max-age> <deadline> <args...> — the exec argv joined with '|', or
# "rc=<n>" when the entrypoint's tail refused.
wrapper_case() {
    local wrapper="$1" max_age="$2" deadline="$3"
    shift 3
    local out rc
    out=$(
        exec 2>"$work/wrapper.err"
        # shellcheck source=/dev/null
        source "$work/exec-stub.bash"
        if [ "$wrapper" = "-" ]; then unset CCY_CLAUDE_WRAPPER; else export CCY_CLAUDE_WRAPPER="$wrapper"; fi
        set -- "$@"
        # shellcheck source=/dev/null
        CCY_LIFECYCLE_MAX_AGE_SECONDS="$max_age" CCY_LIFECYCLE_DEADLINE_EPOCH="$deadline" \
            source "$work/tail-test.bash"
    )
    rc=$?
    if [ "$rc" -ne 0 ]; then
        printf 'rc=%s' "$rc"
    else
        printf '%s' "$out" | paste -sd'|'
    fi
}

DEFAULT_WRAPPER="python3 /workspace/.claude/ccy/claude-supervise.py --"
PLUG="ccy-lifecycle=$work/plugins/ccy_lifecycle.py"
check "no option: the default wrapper line is byte-identical" \
    "python3|/workspace/.claude/ccy/claude-supervise.py|--|claude|--dangerously-skip-permissions" \
    "$(wrapper_case "$DEFAULT_WRAPPER" '' '' claude --dangerously-skip-permissions)"
check "no option: an armed wrapper line is byte-identical" \
    "python3|/workspace/.claude/ccy/claude-supervise.py|--arm|--|claude" \
    "$(wrapper_case "python3 /workspace/.claude/ccy/claude-supervise.py --arm --" '' '' claude)"
check "no option and no wrapper: plain exec" "claude|--x" "$(wrapper_case - '' '' claude --x)"
# With an option on, the entrypoint reads the supervisor the wrapper names, so these cases name
# the stand-in project's supervisor, which declares the plugin API.
SUP="$work/project/.claude/ccy/claude-supervise.py"
mkdir -p "$work/old-supervise"
check "--max-age: the plugin is named before the final --" \
    "python3|$SUP|--plugin|$PLUG|--|claude" \
    "$(wrapper_case "python3 $SUP --" 7200 '' claude)"
check "a deadline alone adds the plugin too" \
    "python3|$SUP|--plugin|$PLUG|--|claude" \
    "$(wrapper_case "python3 $SUP --" '' 1790007200 claude)"
check "an armed wrapper keeps --arm and gains the plugin" \
    "python3|$SUP|--arm|--plugin|$PLUG|--|claude" \
    "$(wrapper_case "python3 $SUP --arm --" 7200 '' claude)"
# The hooks daemon arms every project through its own launcher, not the .py: its deployed ccy.env
# sets CCY_CLAUDE_WRAPPER to "<.claude/ccy>/claude-supervise --arm --", and that launcher execs its
# sibling claude-supervise.py with every argument unchanged.
DAEMON_LAUNCHER="$work/project/.claude/ccy/claude-supervise"
check "no option: the daemon's launcher line is byte-identical" \
    "$DAEMON_LAUNCHER|--arm|--|claude" \
    "$(wrapper_case "$DAEMON_LAUNCHER --arm --" '' '' claude)"
check "the daemon's launcher keeps --arm and gains the plugin" \
    "$DAEMON_LAUNCHER|--arm|--plugin|$PLUG|--|claude" \
    "$(wrapper_case "$DAEMON_LAUNCHER --arm --" 7200 '' claude)"
printf 'import argparse\n' >"$work/old-supervise/claude-supervise.py"
check "the daemon's launcher is judged by its sibling claude-supervise.py" "rc=1" \
    "$(wrapper_case "$work/old-supervise/claude-supervise --arm --" 7200 '' claude)"
check "…and says to upgrade the hooks daemon" "yes" "$(has 'upgrade the hooks daemon' "$work/wrapper.err")"
check "a supervisor without the plugin API is refused, not handed --plugin" "rc=1" \
    "$(wrapper_case "python3 $work/old-supervise/claude-supervise.py --" 7200 '' claude)"
check "…and says to upgrade the hooks daemon" "yes" "$(has 'upgrade the hooks daemon' "$work/wrapper.err")"
printf '_PLUGIN_API_MAJOR = 2\n' >"$work/old-supervise/claude-supervise.py"
check "a supervisor on another plugin API major is refused" "rc=1" \
    "$(wrapper_case "python3 $work/old-supervise/claude-supervise.py --" 7200 '' claude)"
check "…and names both majors" "yes" "$(has 'API 2' "$work/wrapper.err")"
check "a wrapper naming a supervisor that is not there is refused" "rc=1" \
    "$(wrapper_case "python3 $work/nowhere/claude-supervise.py --" 7200 '' claude)"
check "a wrapper that is not the supervisor is refused" "rc=1" \
    "$(wrapper_case "env FOO=1 --" 7200 '' claude)"
check "…and says what the wrapper was" "yes" "$(has 'env FOO=1 --' "$work/wrapper.err")"
check "a supervisor line with no final -- is refused" "rc=1" \
    "$(wrapper_case "python3 /workspace/.claude/ccy/claude-supervise.py --arm" 7200 '' claude)"
check "a feature with no supervisor at all is refused" "rc=1" "$(wrapper_case - 7200 '' claude)"
check "…and says so" "yes" "$(has 'runs without one' "$work/wrapper.err")"
rm -f "$work/plugins/ccy_lifecycle.py"
check "a missing plugin file is refused" "rc=1" "$(wrapper_case "python3 $SUP --" 7200 '' claude)"
check "…and says to rebuild" "yes" "$(has 'ccy --rebuild' "$work/wrapper.err")"

echo "=== launcher and image wiring ==="

check "launcher sources the library" "1" \
    "$(grep -c -F "source \"\$SCRIPT_DIR/lib/session-lifecycle.bash\"" "$LAUNCHER")"
check "launcher lists the library in CCY_LIBS" "1" \
    "$(grep -c -E '^CCY_LIBS=\(.* session-lifecycle( |\))' "$LAUNCHER")"
for flag in --max-age --run-for --until; do
    check "launcher parses $flag" "1" "$(grep -c -F "elif [ \"\$arg\" = \"$flag\" ]; then" "$LAUNCHER")"
    check "launcher help documents $flag" "1" "$(grep -c -E "^  $flag " "$LAUNCHER")"
done
check "the unknown-flag suggestion list knows the new flags" "1" \
    "$(grep -c -E '^[[:space:]]+--max-age --run-for --until$' "$LAUNCHER")"
for var in CCY_LIFECYCLE_LAUNCH_ID CCY_LIFECYCLE_MAX_AGE_SECONDS CCY_LIFECYCLE_DEADLINE_EPOCH CCY_LIFECYCLE_WARN_MINUTES; do
    check "launcher passes $var into the container" "1" "$(grep -c -F -- "-e \"$var=" "$LAUNCHER")"
done
validate_call="ccy_lifecycle_validate_options \"\$OPT_MAX_AGE\" \"\$OPT_RUN_FOR\" \"\$OPT_UNTIL\" \"\$NO_SUPERVISE_MODE\" || exit 1"
check "launcher validates the options after parsing" "1" "$(grep -c -F -- "$validate_call" "$LAUNCHER")"
check "launcher resolves the deadline just before the container starts" "1" \
    "$(grep -c -F 'ccy_lifecycle_finalize || exit 1' "$LAUNCHER")"

container_version=$(grep -oE '^LABEL claude-yolo-version="[0-9.]+"' "$DOCKERFILE" | grep -oE '[0-9]+\.[0-9]+')
required_version=$(grep -oE '^REQUIRED_CONTAINER_VERSION="[0-9.]+"' "$LAUNCHER" | grep -oE '[0-9]+\.[0-9]+')
check "the Dockerfile label and REQUIRED_CONTAINER_VERSION agree" "$required_version" "$container_version"
check "the Dockerfile ships the plugin directory" "1" \
    "$(grep -c -F 'COPY supervisor-plugins/ /opt/claude-yolo/supervisor-plugins/' "$DOCKERFILE")"
check "…root owned, directory 755, file 644" "3" \
    "$(grep -c -E 'chown -R root:root /opt/claude-yolo/supervisor-plugins|chmod 755 /opt/claude-yolo/supervisor-plugins|chmod 644 /opt/claude-yolo/supervisor-plugins/ccy_lifecycle.py' "$DOCKERFILE")"
check "the playbook copies the plugin into the build context" "1" \
    "$(grep -c -F 'Copy Supervisor Plugins into Docker Build Context' "$REPO_ROOT/playbooks/imports/play-claude-yolo.yml")"
check "the plugin source exists" "yes" "$([ -f "$CCY_DIR/supervisor-plugins/ccy_lifecycle.py" ] && echo yes || echo no)"
check "the host probes for the plugin API major the plugin declares" \
    "PLUGIN_API = $CCY_LIFECYCLE_PLUGIN_API_MAJOR" \
    "$(grep -m1 -E '^PLUGIN_API = [0-9]+$' "$CCY_DIR/supervisor-plugins/ccy_lifecycle.py")"

# The launcher sources its libraries from /var/local/claude-yolo/lib/, but these tests source them
# from the repo tree, so a library the play does not deploy passes here and breaks every launch on
# the host. Every lib/*.bash is checked, not the launcher's CCY_LIBS: that list omits
# common-pure.bash, which common.bash sources.
lib_count=0
for lib_path in "$LIB_DIR"/*.bash; do
    lib_count=$((lib_count + 1))
    lib=$(basename "$lib_path")
    check "the playbook deploys lib/$lib" "1" \
        "$(grep -c -F -- "files/var/local/claude-yolo/lib/$lib\"" "$REPO_ROOT/playbooks/imports/play-claude-yolo.yml")"
done
check "the library check found the libraries" "yes" "$([ "$lib_count" -ge 11 ] && echo yes || echo no)"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
