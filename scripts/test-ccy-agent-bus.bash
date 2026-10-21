#!/usr/bin/env bash
# Unit-test the entrypoint's agent team bus step (Plan 00161 DESIGN.md section 5.3).
#
# WHY THIS TEST EXISTS. A ccy session's teams come only from the host (Plan 00203
# SPEC-hot-control.md H2): the launcher passes PINGBUS_MEMBERSHIP, either /run/ccy/membership
# (the read-only directory holding the session's desired membership; the session carries the
# agent team bus kit) or off (`ccy --no-bus`, or a launch outside ccy's tmux with no --teams).
# Nothing the session can write may choose a team or seat, so the entrypoint refuses to start
# when the project's ccy.env (tracked) or ccy.env.local sets, changes or unsets any PINGBUS_*
# variable, and refuses the marker unset or unknown, or a PINGBUS_SEATS, PINGBUS_TEAMS or
# PINGBUS_HOME from the launcher, which only a launcher older than the image would pass. With
# the kit it links pingbus, gives claude the plugin and the session's own settings (written by
# the claim into the session home) inside a supervisor wrapper's `--`, and puts
# `pingbus seat exec --` in front of both final exec lines, which claims the desired seats and
# builds the session home; with off, nothing is linked or added. The test cuts the real PROJECT-ENV block and the real tail (from the
# AGENT-BUS marker to the end) out of entrypoint.sh, points their paths at a throwaway tree,
# and replaces exec with a printer. The last cases then replay the printed command line
# through the real zipapp (built by helpers/pingbus/bundle.py as the play builds it), so the
# entrypoint's `pingbus seat exec -- ...` is parsed, claimed and exec'd by the real CLI.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
ENTRYPOINT="$CCY_DIR/entrypoint.sh"
KIT_SRC="$REPO_ROOT/files/opt/claude-yolo/optional/agent-bus"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"

# The caller's own bus variables must not leak into the cases.
unset "${!PINGBUS_@}"

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
        if [ -s "$work/step.err" ]; then
            echo "        stderr: $(cat "$work/step.err")"
        fi
    fi
}
yes_no() { if "$@"; then echo yes; else echo no; fi; }

echo "=== the image tree: the real plugin, settings and zipapp ==="

# The step's exec is replaced, so the zipapp does not run there; the last section replays
# the command line the step built through this real zipapp, built as the play builds it.
image="$work/image/agent-bus"
mkdir -p "$image"
cp -r "$KIT_SRC/plugin" "$KIT_SRC/settings.json" "$image/"
marker=$(cd "$REPO_ROOT" && python3 -s -m helpers.pingbus.bundle --source "$REPO_ROOT" \
    --out "$image/pingbus" pingbus 2>"$work/step.err")
check "the zipapp builds" "BUNDLE-CHANGED $image/pingbus" "$marker"
chmod 755 "$image/pingbus"
# The step only requires it to be there; the Go golden test judges what it prints.
printf '#!/bin/sh\nexit 0\n' >"$image/pingbus-status-line"
chmod 755 "$image/pingbus-status-line"
check "the kit has the plugin manifest the step needs" "yes" \
    "$(yes_no test -f "$image/plugin/pingbus/.claude-plugin/plugin.json")"

echo "=== the steps under test ==="

ccy="$work/ccy"
bin="$work/bin"
# The read-only membership directory the launcher mounts; here a throwaway with a desired file.
desired="$work/run-ccy-membership"
mkdir -p "$ccy" "$bin" "$desired"
# write_desired <seat@team>...: the desired membership file, version 1, with those seats.
write_desired() {
    local seats="" item
    for item in "$@"; do seats+="${seats:+,}{\"team\":\"${item#*@}\",\"seat\":\"${item%@*}\"}"; done
    printf '{"v":1,"session":"ccy-proj-1","kind":"ccy","checkout":"/home/u/proj","generation":1,"written_ms":1,"seats":[%s],"farewell":[]}\n' \
        "$seats" >"$desired/membership.json"
}
write_desired dev1@team-a qa2@team-b
step="$work/step.bash"
{
    awk '/^# >>> PROJECT-ENV/ { on = 1; next } /^# <<< PROJECT-ENV/ { on = 0 } on' "$ENTRYPOINT"
    awk '/^# >>> AGENT-BUS/ { on = 1 } on' "$ENTRYPOINT"
} | awk -v ccy="$ccy" -v image="$image" -v link="$bin/pingbus" -v desired="$desired" -v home="$work/session-home" '{
    gsub("/opt/claude-yolo/optional/agent-bus", image)
    gsub("/usr/local/bin/pingbus", link)
    gsub("/workspace/.claude/ccy", ccy)
    gsub("/run/ccy/membership", desired)
    gsub("/tmp/pingbus-home", home)
    print
}' >"$step"
check "the PROJECT-ENV block is there" "yes" "$(yes_no grep -q "for _ccy_env_file in" "$step")"
check "the AGENT-BUS block is there" "yes" "$(yes_no grep -q '^# <<< AGENT-BUS' "$step")"
check "every image path was redirected" "0" "$(grep -c '/opt/claude-yolo/optional\|/usr/local/bin/pingbus\|/run/ccy/membership\|/tmp/pingbus-home' "$step")"
# Both final exec lines carry the claim; a third exec would be a path that skips it.
check "both final exec lines start with the seat claim" "2" \
    "$(grep -cF -- "exec \"\${_ccy_seat_exec[@]}\" " "$step")"
check "no other exec in the tail" "2" "$(grep -c '^ *exec ' "$step")"

# A function named exec takes precedence over the builtin, so the tail's exec prints what the
# session would get instead of replacing the shell. The variables are read in a child, so
# only exported values count, as they would for the session.
cat >"$work/exec-stub.bash" <<STUB
exec() {
    local IFS='|'
    printf '%s\0' "\$@" >"$work/argv.bin"
    printf 'argv=%s\n' "\$*"
    printf 'membership=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_MEMBERSHIP-unset}"')"
    printf 'home=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_HOME-unset}"')"
    printf 'teams=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_TEAMS-unset}"')"
    if [ -L "$bin/pingbus" ]; then printf 'link=%s\n' "\$(readlink "$bin/pingbus")"; else printf 'link=none\n'; fi
    builtin exit 0
}
STUB

# run_step <wrapper|-|+> <args...>: the stub's lines joined with ';', or "rc=<n>" when the
# step refused. "-" is `ccy --no-supervise` (no wrapper from the host, CCY_NO_SUPERVISOR=1);
# "+" forwards neither, so the project's ccy.env or the default decides. Inherits the
# caller's environment, which is where the launcher's -e variables would be. PROJECT-ENV
# announces each file it sources on stdout; anything else there lands in the result and
# fails the exact comparisons.
run_step() {
    local wrapper="$1" out rc
    shift
    rm -f "$bin/pingbus"
    out=$(
        exec 2>"$work/step.err"
        if [ "$wrapper" = "-" ]; then
            unset CCY_CLAUDE_WRAPPER
            export CCY_NO_SUPERVISOR=1
        elif [ "$wrapper" = "+" ]; then
            unset CCY_CLAUDE_WRAPPER CCY_NO_SUPERVISOR
        else
            export CCY_CLAUDE_WRAPPER="$wrapper"
        fi
        HOME="$work/home" bash -e -c '. "$1"; shift; set -- "$@"; . "$0"' "$step" "$work/exec-stub.bash" "$@"
    )
    rc=$?
    if [ "$rc" -ne 0 ]; then
        printf 'rc=%s' "$rc"
    else
        printf '%s\n' "$out" | grep -v '^Sourcing project ccy env: ' | paste -sd';'
    fi
}
err_has() { yes_no grep -qF -- "$1" "$work/step.err"; }

# env_files <ccy.env text> <ccy.env.local text>: write the project's env files; an empty
# text means no file.
env_files() {
    rm -f "$ccy/ccy.env" "$ccy/ccy.env.local"
    if [ -n "$1" ]; then printf '%s\n' "$1" >"$ccy/ccy.env"; fi
    if [ -n "$2" ]; then printf '%s\n' "$2" >"$ccy/ccy.env.local"; fi
}

# step_with <ccy.env text> <ccy.env.local text> [VAR=value from the launcher...]: run the
# step with no supervisor and the command `claude`.
step_with() {
    env_files "$1" "$2"
    shift 2
    (
        for assignment in "$@"; do
            export "${assignment?}"
        done
        run_step - claude
    )
}

mkdir -p "$work/home"
SUP="python3 $ccy/claude-supervise.py --"
CLAIM="$image/pingbus|seat|exec|--"
PLUGIN_ARGS="--plugin-dir|$image/plugin/pingbus|--settings|$work/session-home/settings.json"
PLAIN_ENV="membership=off;home=unset;teams=unset;link=none"

echo "=== PINGBUS_MEMBERSHIP=off (--no-bus, or no ccy tmux and no --teams): inert ==="

export PINGBUS_MEMBERSHIP=off

env_files "" ""
check "no env files: argv, environment and PATH untouched" \
    "argv=python3|$ccy/claude-supervise.py|--|claude|--dangerously-skip-permissions;$PLAIN_ENV" \
    "$(run_step "$SUP" claude --dangerously-skip-permissions)"
check "no supervisor: the plain exec is untouched too" \
    "argv=claude|--x;$PLAIN_ENV" "$(run_step - claude --x)"
env_files "" 'export HOOKS_DAEMON_HOSTNAME=role-a'
check "a ccy.env.local that sets only the role: untouched" \
    "argv=claude|--x;$PLAIN_ENV" "$(run_step - claude --x)"
check "a command other than claude is left alone" \
    "argv=bash|-l;$PLAIN_ENV" "$(run_step - bash -l)"

echo "=== --no-supervise beats a wrapper the project's ccy.env arms ==="

# This repository's own ccy.env arms the supervisor with ${CCY_CLAUDE_WRAPPER:-...}, the
# idiom that lets a host-forwarded wrapper win. --no-supervise forwards none, so without the
# rule the project's wrapper came back: a headless session driven through a pipe (Plan 00161
# U20) then ran under the PTY supervisor it asked to be rid of.
PROJECT_WRAPPER="export CCY_CLAUDE_WRAPPER=\"\${CCY_CLAUDE_WRAPPER:-python3 $ccy/claude-supervise.py --arm --}\""
env_files "$PROJECT_WRAPPER" ""
check "without --no-supervise the project's wrapper runs claude" \
    "argv=python3|$ccy/claude-supervise.py|--arm|--|claude|--x;$PLAIN_ENV" "$(run_step + claude --x)"
check "with --no-supervise claude runs unwrapped" \
    "argv=claude|--x;$PLAIN_ENV" "$(run_step - claude --x)"
check "and says the project's wrapper was set aside" "yes" "$(err_has "--no-supervise")"
env_files "" "$PROJECT_WRAPPER"
check "a wrapper armed by ccy.env.local is set aside too" \
    "argv=claude|--x;$PLAIN_ENV" "$(run_step - claude --x)"
env_files "" ""

echo "=== PINGBUS_MEMBERSHIP=/run/ccy/membership: the kit, its settings and the claim ==="

SEATED_ENV="membership=$desired;home=unset;teams=unset;link=$image/pingbus"
export PINGBUS_MEMBERSHIP="$desired"
env_files "" ""
check "the claim goes first, the plugin and settings right after claude inside the wrapper's --" \
    "argv=$CLAIM|python3|$ccy/claude-supervise.py|--|claude|$PLUGIN_ARGS|--dangerously-skip-permissions|hi;$SEATED_ENV" \
    "$(run_step "$SUP" claude --dangerously-skip-permissions hi)"
check "an armed wrapper keeps its own flags before its --" \
    "argv=$CLAIM|python3|$ccy/claude-supervise.py|--arm|--|claude|$PLUGIN_ARGS;$SEATED_ENV" \
    "$(run_step "python3 $ccy/claude-supervise.py --arm --" claude)"
check "no supervisor: the plain exec gets the claim and the plugin too" \
    "argv=$CLAIM|claude|$PLUGIN_ARGS|--x;$SEATED_ENV" "$(run_step - claude --x)"
env_files 'export CCY_X=1' 'export HOOKS_DAEMON_HOSTNAME=role-a'
check "env files that set no bus variable: accepted" \
    "argv=$CLAIM|claude|$PLUGIN_ARGS;$SEATED_ENV" "$(run_step - claude)"
env_files "" ""

check "with a command other than claude: refused" "rc=1" "$(run_step - bash -l)"
check "that refusal names the command" "yes" "$(err_has "'bash'")"
check "a refused launch links nothing" "no" "$(yes_no test -L "$bin/pingbus")"

mv "$image/plugin/pingbus/hooks/hooks.json" "$work/hooks.json.away"
check "an image without the kit: refused" "rc=1" "$(run_step - claude)"
check "that refusal says to rebuild" "yes" "$(err_has "ccy --rebuild")"
mv "$work/hooks.json.away" "$image/plugin/pingbus/hooks/hooks.json"
mv "$image/pingbus-status-line" "$work/pingbus-status-line.away"
check "an image without the Go status line (Plan 00213): refused" "rc=1" "$(run_step - claude)"
check "that refusal names it" "yes" "$(err_has "$image/pingbus-status-line is missing")"
mv "$work/pingbus-status-line.away" "$image/pingbus-status-line"
mv "$desired/membership.json" "$work/membership.json.away"
check "no membership file in the mount: refused" "rc=1" "$(run_step - claude)"
check "that refusal names the file" "yes" "$(err_has "$desired/membership.json is missing")"
check "a refused launch links nothing" "no" "$(yes_no test -L "$bin/pingbus")"
mv "$work/membership.json.away" "$desired/membership.json"
export PINGBUS_MEMBERSHIP=off

echo "=== a launcher older than the image: refused, never started teamless ==="

check "PINGBUS_MEMBERSHIP unset" "rc=1" "$(unset PINGBUS_MEMBERSHIP; run_step - claude)"
check "that refusal says the launcher is older" "yes" "$(err_has "older than this image")"
check "PINGBUS_MEMBERSHIP a value ccy never passes" "rc=1" "$(step_with '' '' PINGBUS_MEMBERSHIP=/elsewhere)"
check "  named in the refusal" "yes" "$(err_has "PINGBUS_MEMBERSHIP is /elsewhere")"
check "a leftover PINGBUS_SEATS beside the marker (a mixed deploy)" "rc=1" \
    "$(step_with '' '' PINGBUS_SEATS=dev1@team-a PINGBUS_MEMBERSHIP="$desired")"
check "  named in the refusal" "yes" "$(err_has "PINGBUS_SEATS came from the launcher")"
check "a leftover PINGBUS_SEATS beside off" "rc=1" "$(step_with '' '' PINGBUS_SEATS=dev1@team-a)"

echo "=== a bus variable set, changed or unset by ccy.env or ccy.env.local: refused ==="

check "PINGBUS_SEATS set by the tracked ccy.env" "rc=1" \
    "$(step_with 'export PINGBUS_SEATS=dev1@team-a' '')"
check "that refusal names the host as the only way" "yes" "$(err_has "only from the host")"
check "that refusal names the variable" "yes" "$(err_has "PINGBUS_SEATS")"
check "PINGBUS_MEMBERSHIP changed by ccy.env.local" "rc=1" \
    "$(step_with '' "export PINGBUS_MEMBERSHIP=$desired")"
check "PINGBUS_MEMBERSHIP unset by ccy.env" "rc=1" "$(step_with 'unset PINGBUS_MEMBERSHIP' '')"
check "PINGBUS_SEATS set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_SEATS=dev1@team-a')"
check "PINGBUS_SEATS set empty by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_SEATS=')"
check "PINGBUS_TEAMS set by ccy.env.local (the old opt-in)" "rc=1" \
    "$(step_with '' 'export PINGBUS_TEAMS=team-a')"
check "PINGBUS_TEAMS set by the tracked ccy.env" "rc=1" \
    "$(step_with 'export PINGBUS_TEAMS=team-a' '')"
check "PINGBUS_HOME set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_HOME=/workspace/elsewhere' PINGBUS_MEMBERSHIP="$desired")"
check "a refused launch links nothing" "no" "$(yes_no test -L "$bin/pingbus")"
# Every PINGBUS_* variable, not only the three the entrypoint reads (D62): the names below
# are arbitrary PINGBUS_ names (pingbus reads no forge credential variable, D66), and the
# checkout carries nothing about the bus.
check "PINGBUS_FORGE_TOKEN set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_FORGE_TOKEN=x' PINGBUS_MEMBERSHIP="$desired")"
check "that refusal names the variable" "yes" "$(err_has "PINGBUS_FORGE_TOKEN")"
check "PINGBUS_FORGE_TOKEN_FILE set by the tracked ccy.env" "rc=1" \
    "$(step_with 'export PINGBUS_FORGE_TOKEN_FILE=/workspace/t' '')"
check "a PINGBUS_ name pingbus does not read, set by ccy.env.local" "rc=1" \
    "$(step_with '' 'PINGBUS_ANYTHING=1')"
check "PINGBUS_ENV unset by ccy.env" "rc=1" "$(step_with 'unset PINGBUS_ENV' '' PINGBUS_ENV=/x)"
check "a bus variable from the launcher that neither file touches: accepted" \
    "argv=$CLAIM|claude|$PLUGIN_ARGS;$SEATED_ENV" \
    "$(step_with 'export CCY_X=1' '' PINGBUS_MEMBERSHIP="$desired" PINGBUS_FORGE_TOKEN_FILE=/run/t)"
check "a name that only contains PINGBUS_: accepted" \
    "argv=claude;$PLAIN_ENV" "$(step_with '' 'export MY_PINGBUS_X=1')"

echo "=== PINGBUS_TEAMS or PINGBUS_HOME from the launcher: refused ==="

check "PINGBUS_TEAMS passed in" "rc=1" "$(step_with '' '' PINGBUS_TEAMS=team-a)"
check "that refusal says the launcher is older than the image" "yes" "$(err_has "older")"
check "PINGBUS_HOME passed in" "rc=1" \
    "$(step_with '' '' PINGBUS_HOME=/workspace/x PINGBUS_MEMBERSHIP="$desired")"
check "PINGBUS_TEAMS passed in beside the marker" "rc=1" \
    "$(step_with '' '' PINGBUS_TEAMS=team-a PINGBUS_MEMBERSHIP="$desired")"
env_files "" ""

echo "=== the real pingbus seat exec runs the command line the entrypoint builds ==="

# The step's exec printed its argv; here that exact argv, from "seat" on, goes to the real
# zipapp's own cli.main, so its parser, the claim and the final execvpe all run. Only the
# two paths fixed for a container (/workspace's seats, /tmp/pingbus-home) are pointed into
# this tree. Stand-ins for the wrapper and claude print what they were given.
seats="$work/seats"
write_seat() { # <team> <seat>: a valid bundle, reserved example values (CLAUDE/ExampleValues.md)
    local dir="$seats/$1/$2" sn="$1.agent-bus.internal"
    mkdir -p "$dir"
    chmod 700 "$seats" "$seats/$1" "$dir"
    printf '{"protocol":1,"team":"%s","user_id":"@myrepo.%s+local.podman:%s","server_name":"%s","base_url":"http://192.0.2.10:8448","plain_http_hosts":["192.0.2.10"],"token_file":"token","admin":"@admin:%s","room":"!%s"}\n' \
        "$1" "$2" "$sn" "$sn" "$sn" "$(printf 'A%.0s' $(seq 43))" >"$dir/member.json"
    printf 'syt_ZXhhbXBsZQ_notarealtoken_%s' "$2" >"$dir/token"
    chmod 600 "$dir/token"
}
write_seat team-a dev1
write_seat team-b qa2
for stand_in in wrap claude; do
    cat >"$bin/$stand_in" <<'STAND_IN'
#!/bin/bash
IFS='|'
printf '%s=%s;home=%s;teams=%s\n' "${0##*/}" "$*" "$PINGBUS_HOME" "$PINGBUS_TEAMS"
STAND_IN
    chmod 755 "$bin/$stand_in"
done
# seat exec asks gh for its github.com login (D66); this stand-in answers as a logged-in gh
# with a token that is not one, so the run never reads the real login of whoever runs it.
cat >"$bin/gh" <<'STAND_IN'
#!/bin/bash
[ "$*" = "auth token --hostname github.com" ] || { echo "stand-in gh: unexpected: $*" >&2; exit 2; }
echo gho_notarealtoken_standin
STAND_IN
chmod 755 "$bin/gh"
# replay: run the argv the step last printed through the real zipapp; its output, or rc=<n>.
replay() {
    rm -rf "$work/session-home"
    (cd "$work/proj" && env -u CLAUDE_CONFIG_DIR HOME="$work/home" PATH="$bin:$PATH" \
        python3 -I - "$work/argv.bin" "$seats" "$work/session-home" 2>"$work/step.err" <<'PY'
import pathlib, sys
argv = [arg.decode() for arg in pathlib.Path(sys.argv[1]).read_bytes().split(b"\0")[:-1]]
sys.path.insert(0, argv[0])  # the zipapp the step put in front of the command
from helpers.pingbus import cli
runtime = cli.Runtime(seats_root=pathlib.Path(sys.argv[2]), session_home=pathlib.Path(sys.argv[3]))
sys.exit(cli.main(argv[1:], runtime=runtime))
PY
    )
    local rc=$?
    if [ "$rc" -ne 0 ]; then printf 'rc=%s' "$rc"; fi
}
REPLAYED="home=$work/session-home;teams=team-a,team-b"
mkdir -p "$work/proj/.claude"
printf '{"statusLine":{"type":"command","command":"project-line","refreshInterval":5}}\n' >"$work/proj/.claude/settings.json"
export PINGBUS_MEMBERSHIP="$desired"
write_desired dev1@team-a qa2@team-b
env_files "" ""
run_step "$bin/wrap --arm --" claude --dangerously-skip-permissions hi >/dev/null
check "the step put the image's zipapp in front of the command" "$image/pingbus" \
    "$(tr '\0' '\n' <"$work/argv.bin" | awk 'NR == 1')"
check "with a wrapper: the wrapper gets its own --, then claude with the plugin" \
    "wrap=--arm|--|claude|$PLUGIN_ARGS|--dangerously-skip-permissions|hi;$REPLAYED" "$(replay)"
check "  and pingbus said which seats it claimed" "yes" "$(err_has "agent team bus: dev1@team-a")"
run_step - claude --x >/dev/null
check "no wrapper: claude itself, with the plugin" "claude=$PLUGIN_ARGS|--x;$REPLAYED" "$(replay)"
check "  the settings claude is given exist, accepting inbound" "accept" \
    "$(jq -r .crossSessionInbound "$work/session-home/settings.json")"
check "  a seat is held, so the active flag is up" "yes" "$(yes_no test -f "$work/session-home/active")"
check "  and the project's own status line is kept for when no seat is held" "project-line" \
    "$(cat "$work/session-home/statusline-inner")"
write_desired
run_step - claude --x >/dev/null
check "a plain session (no seat desired): claude with the plugin, on no team" \
    "claude=$PLUGIN_ARGS|--x;home=$work/session-home;teams=" "$(replay)"
check "  no active flag, so every hook and the status line stay in the shell" "no" \
    "$(yes_no test -e "$work/session-home/active")"
check "  the plain session refuses inbound on the inbox socket Claude Code always binds (Decision 1)" "refuse" \
    "$(jq -r .crossSessionInbound "$work/session-home/settings.json")"
check "  the plain session keeps the project's status line interval" "5" \
    "$(jq -r .statusLine.refreshInterval "$work/session-home/settings.json")"
write_desired dev1@team-a qa2@team-b
rm -rf "$seats/team-b"
run_step - claude --x >/dev/null
check "a seat with no directory: the real claim refuses (78)" "rc=78" "$(replay)"
check "  naming the seat" "yes" "$(err_has "qa2@team-b")"
export PINGBUS_MEMBERSHIP=off

echo "=== the image and the play ship what the step needs ==="

: >"$work/step.err"

check "the Dockerfile makes the zipapp executable" "yes" \
    "$(yes_no grep -q 'chmod 755 /opt/claude-yolo/optional/agent-bus/pingbus' "$CCY_DIR/Dockerfile")"
check "the play builds the zipapp with the shared builder into the build context" "yes" \
    "$(yes_no grep -q 'helpers.pingbus.bundle' "$PLAY")"
check "the play builds it where the image COPYs it from" "yes" \
    "$(yes_no grep -q -- '- /opt/claude-yolo/optional/agent-bus/pingbus' "$PLAY")"
check "the play builds the Go status line static, where the image COPYs it from" "yes" \
    "$(yes_no awk '/go_command_name: pingbus-status-line/ { n = NR } n && NR <= n + 4 && /go_command_static: true/ { s = 1 }
        n && NR <= n + 1 && /go_command_dest: \/opt\/claude-yolo\/optional\/agent-bus\/pingbus-status-line$/ { d = 1 }
        END { exit !(s && d) }' "$PLAY")"
check "the Dockerfile links it onto PATH, where the seated guard runs it by name" "yes" \
    "$(yes_no grep -qF 'ln -s /opt/claude-yolo/optional/agent-bus/pingbus-status-line /usr/local/bin/pingbus-status-line' "$CCY_DIR/Dockerfile")"
check "  by the name the seated guard runs" "yes" \
    "$(cd "$REPO_ROOT" && python3 -I -c 'import sys; sys.path.insert(0, "."); from helpers.pingbus import seat; print("yes" if seat.STATUS_LINE_GUARD.endswith("exec pingbus-status-line") else "no")')"
check "the step's settings path is the claim's session home" "yes" \
    "$(yes_no grep -qF -- '--settings /tmp/pingbus-home/settings.json' "$ENTRYPOINT")"
check "  which is pingbus's SESSION_HOME" "/tmp/pingbus-home" \
    "$(cd "$REPO_ROOT" && python3 -I -c 'import sys; sys.path.insert(0, "."); from helpers.pingbus import seat; print(seat.SESSION_HOME)')"
for kit_file in settings.json plugin/pingbus/.claude-plugin/plugin.json plugin/pingbus/hooks/hooks.json \
    plugin/pingbus/skills/pingbus/SKILL.md; do
    check "the play stages $kit_file" "yes" \
        "$(yes_no grep -qF "files/opt/claude-yolo/optional/agent-bus/$kit_file" "$PLAY")"
done

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
