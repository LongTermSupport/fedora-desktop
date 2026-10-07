#!/usr/bin/env bash
# Unit-test the entrypoint's agent team bus step (Plan 00161 DESIGN.md section 5.3).
#
# WHY THIS TEST EXISTS. A ccy session is in a team only when its launch says so:
# `ccy --teams <seat>@<team>[,...]` passes one variable, PINGBUS_SEATS, into the container.
# Nothing the session can write may choose a team or seat, so the entrypoint refuses to start
# when the project's ccy.env (tracked) or ccy.env.local sets, changes or unsets a bus variable,
# and refuses a PINGBUS_TEAMS or PINGBUS_HOME from the launcher, which only a launcher older
# than the image would pass. With PINGBUS_SEATS it links pingbus, gives claude the plugin and
# settings inside a supervisor wrapper's `--`, and puts `pingbus seat exec --` in front of both
# final exec lines, which claims the seats and builds the session home; without it, nothing is
# linked or added. The test cuts the real PROJECT-ENV block and the real tail (from the
# AGENT-BUS marker to the end) out of entrypoint.sh, points their paths at a throwaway tree,
# and replaces exec with a printer, so `seat exec` itself never runs (helpers' test_seat.py
# covers the claim).
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
unset PINGBUS_SEATS PINGBUS_TEAMS PINGBUS_HOME

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

echo "=== the image tree: the real plugin and settings, a stand-in zipapp ==="

# The zipapp is never run here (exec is replaced), so a stand-in marks where it would be.
image="$work/image/agent-bus"
mkdir -p "$image"
cp -r "$KIT_SRC/plugin" "$KIT_SRC/settings.json" "$image/"
printf '#!/bin/sh\nexit 99\n' >"$image/pingbus"
chmod 755 "$image/pingbus"
check "the kit has the plugin manifest the step needs" "yes" \
    "$(yes_no test -f "$image/plugin/pingbus/.claude-plugin/plugin.json")"

echo "=== the steps under test ==="

ccy="$work/ccy"
bin="$work/bin"
mkdir -p "$ccy" "$bin"
step="$work/step.bash"
{
    awk '/^# >>> PROJECT-ENV/ { on = 1; next } /^# <<< PROJECT-ENV/ { on = 0 } on' "$ENTRYPOINT"
    awk '/^# >>> AGENT-BUS/ { on = 1 } on' "$ENTRYPOINT"
} | awk -v ccy="$ccy" -v image="$image" -v link="$bin/pingbus" '{
    gsub("/opt/claude-yolo/optional/agent-bus", image)
    gsub("/usr/local/bin/pingbus", link)
    gsub("/workspace/.claude/ccy", ccy)
    print
}' >"$step"
check "the PROJECT-ENV block is there" "yes" "$(yes_no grep -q "for _ccy_env_file in" "$step")"
check "the AGENT-BUS block is there" "yes" "$(yes_no grep -q '^# <<< AGENT-BUS' "$step")"
check "every image path was redirected" "0" "$(grep -c '/opt/claude-yolo/optional\|/usr/local/bin/pingbus' "$step")"
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
    printf 'argv=%s\n' "\$*"
    printf 'seats=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_SEATS-unset}"')"
    printf 'home=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_HOME-unset}"')"
    printf 'teams=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_TEAMS-unset}"')"
    if [ -L "$bin/pingbus" ]; then printf 'link=%s\n' "\$(readlink "$bin/pingbus")"; else printf 'link=none\n'; fi
    builtin exit 0
}
STUB

# run_step <wrapper|-> <args...>: the stub's lines joined with ';', or "rc=<n>" when the
# step refused. "-" runs with no supervisor at all. Inherits the caller's environment, which
# is where the launcher's -e variables would be. PROJECT-ENV announces each file it sources
# on stdout; anything else there lands in the result and fails the exact comparisons.
run_step() {
    local wrapper="$1" out rc
    shift
    rm -f "$bin/pingbus"
    out=$(
        exec 2>"$work/step.err"
        if [ "$wrapper" = "-" ]; then
            unset CCY_CLAUDE_WRAPPER
            export CCY_NO_SUPERVISOR=1
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
PLUGIN_ARGS="--plugin-dir|$image/plugin/pingbus|--settings|$image/settings.json"
PLAIN_ENV="seats=unset;home=unset;teams=unset;link=none"

echo "=== a plain launch (no PINGBUS_SEATS): inert ==="

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

echo "=== a launch with --teams: PINGBUS_SEATS from the launcher ==="

SEATED_ENV="seats=dev1@team-a,qa2@team-b;home=unset;teams=unset;link=$image/pingbus"
export PINGBUS_SEATS=dev1@team-a,qa2@team-b
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

mv "$image/settings.json" "$work/settings.json.away"
check "an image without the kit: refused" "rc=1" "$(run_step - claude)"
check "that refusal says to rebuild" "yes" "$(err_has "ccy --rebuild")"
mv "$work/settings.json.away" "$image/settings.json"
unset PINGBUS_SEATS

echo "=== a bus variable set, changed or unset by ccy.env or ccy.env.local: refused ==="

check "PINGBUS_SEATS set by the tracked ccy.env" "rc=1" \
    "$(step_with 'export PINGBUS_SEATS=dev1@team-a' '')"
check "that refusal names ccy --teams as the only way" "yes" "$(err_has "ccy --teams")"
check "that refusal names the variable" "yes" "$(err_has "PINGBUS_SEATS")"
check "PINGBUS_SEATS set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_SEATS=dev1@team-a')"
check "PINGBUS_SEATS changed by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_SEATS=pm@team-a' PINGBUS_SEATS=dev1@team-a)"
check "PINGBUS_SEATS unset by ccy.env" "rc=1" \
    "$(step_with 'unset PINGBUS_SEATS' '' PINGBUS_SEATS=dev1@team-a)"
check "PINGBUS_SEATS set empty by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_SEATS=')"
check "PINGBUS_TEAMS set by ccy.env.local (the old opt-in)" "rc=1" \
    "$(step_with '' 'export PINGBUS_TEAMS=team-a')"
check "PINGBUS_TEAMS set by the tracked ccy.env" "rc=1" \
    "$(step_with 'export PINGBUS_TEAMS=team-a' '')"
check "PINGBUS_HOME set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export PINGBUS_HOME=/workspace/elsewhere' PINGBUS_SEATS=dev1@team-a)"
check "a refused launch links nothing" "no" "$(yes_no test -L "$bin/pingbus")"

echo "=== PINGBUS_TEAMS or PINGBUS_HOME from the launcher: refused ==="

check "PINGBUS_TEAMS passed in" "rc=1" "$(step_with '' '' PINGBUS_TEAMS=team-a)"
check "that refusal says the launcher is older than the image" "yes" "$(err_has "older")"
check "PINGBUS_HOME passed in" "rc=1" \
    "$(step_with '' '' PINGBUS_HOME=/workspace/x PINGBUS_SEATS=dev1@team-a)"
check "PINGBUS_TEAMS passed in beside PINGBUS_SEATS" "rc=1" \
    "$(step_with '' '' PINGBUS_TEAMS=team-a PINGBUS_SEATS=dev1@team-a)"
env_files "" ""

echo "=== the image and the play ship what the step needs ==="

: >"$work/step.err"

check "the Dockerfile makes the zipapp executable" "yes" \
    "$(yes_no grep -q 'chmod 755 /opt/claude-yolo/optional/agent-bus/pingbus' "$CCY_DIR/Dockerfile")"
check "the play builds the zipapp with the shared builder into the build context" "yes" \
    "$(yes_no grep -q 'helpers.pingbus.bundle' "$PLAY")"
check "the play builds it where the image COPYs it from" "yes" \
    "$(yes_no grep -q -- '- /opt/claude-yolo/optional/agent-bus/pingbus' "$PLAY")"
for kit_file in settings.json plugin/pingbus/.claude-plugin/plugin.json plugin/pingbus/hooks/hooks.json \
    plugin/pingbus/skills/pingbus/SKILL.md; do
    check "the play stages $kit_file" "yes" \
        "$(yes_no grep -qF "files/opt/claude-yolo/optional/agent-bus/$kit_file" "$PLAY")"
done

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
