#!/usr/bin/env bash
# Unit-test the entrypoint's agent team bus step (Plan 00161 DESIGN.md section 5.3).
#
# WHY THIS TEST EXISTS. A ccy session's teams come only from the host (Plan 00203
# SPEC-hot-control.md H2): the launcher passes PINGBUS_MEMBERSHIP (v1) and TEAMCHAT_MEMBERSHIP
# (v2), and nothing the session can write may choose a team or seat, so the entrypoint refuses to
# start when the project's ccy.env (tracked) or ccy.env.local sets, changes or unsets any
# PINGBUS_* or TEAMCHAT_* variable (D62), and otherwise execs ccy-start-claude, which builds the
# session's command (src-go/internal/ccystart; its parity test holds every v1 command line,
# refusal and message to the bash it replaced). The test cuts the real PROJECT-ENV block and the
# real tail (from the AGENT-BUS marker to the end) out of entrypoint.sh, points their paths at a
# throwaway tree, and replaces exec with a printer. The last cases replay the v1 command line
# ccy-start-claude builds through the real zipapp (built by helpers/pingbus/bundle.py as the
# image's builder stage builds it), so `pingbus seat exec -- ...` is parsed, claimed and exec'd by the real CLI.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
ENTRYPOINT="$CCY_DIR/entrypoint.sh"
KIT_SRC="$REPO_ROOT/files/opt/claude-yolo/optional/agent-bus"
# The image's build-context staging and build are in tasks/claude-yolo-image.yml, which
# play-claude-yolo.yml and play-claude-code-consumer.yml import (Plan 00233).
PLAY="$REPO_ROOT/tasks/claude-yolo-image.yml"

# The caller's own bus variables must not leak into the cases.
unset "${!PINGBUS_@}" "${!TEAMCHAT_@}"

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
# The tail's one exec hands the session's command to ccy-start-claude; a second would be a path
# that skips it.
check "the tail ends in the exec of ccy-start-claude" 'exec /usr/local/bin/ccy-start-claude "$@"' \
    "$(grep '^ *exec ' "$step")"

# A function named exec takes precedence over the builtin, so the tail's exec prints what the
# session would get instead of replacing the shell. The variables are read in a child, so
# only exported values count, as they would for the session.
cat >"$work/exec-stub.bash" <<STUB
exec() {
    local IFS='|'
    printf '%s\0' "\$@" >"$work/argv.bin"
    printf 'argv=%s\n' "\$*"
    printf 'membership=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_MEMBERSHIP-unset}"')"
    printf 'teamchat=%s\n' "\$(bash -c 'printf %s "\${TEAMCHAT_MEMBERSHIP-unset}"')"
    bash -c 'printf %s "\${CCY_CLAUDE_WRAPPER-unset}"' >"$work/wrapper.seen"
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
PLUGIN_ARGS="--plugin-dir|$image/plugin/pingbus|--settings|$work/session-home/settings.json"
START="/usr/local/bin/ccy-start-claude|claude"
PLAIN_ENV="membership=off;teamchat=unset;home=unset;teams=unset;link=none"
SEATED_ENV="membership=$desired;teamchat=unset;home=unset;teams=unset;link=none"
export PINGBUS_MEMBERSHIP=off
env_files "" ""
check "no env files: the command goes to ccy-start-claude untouched" \
    "argv=$START|--dangerously-skip-permissions;$PLAIN_ENV" "$(run_step + claude --dangerously-skip-permissions)"
check "the launcher's variables reach it" "argv=$START;membership=off;teamchat=/run/teamchat/membership;home=unset;teams=unset;link=none" \
    "$(step_with '' '' TEAMCHAT_MEMBERSHIP=/run/teamchat/membership)"
env_files 'CCY_CLAUDE_WRAPPER="python3 /x.py --"' ''
check "a wrapper ccy.env sets without export reaches it" "python3 /x.py --" \
    "$(run_step + claude >/dev/null; cat "$work/wrapper.seen")"
env_files "" ""

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
    "argv=$START;$SEATED_ENV" \
    "$(step_with 'export CCY_X=1' '' PINGBUS_MEMBERSHIP="$desired" PINGBUS_FORGE_TOKEN_FILE=/run/t)"
check "a name that only contains PINGBUS_: accepted" \
    "argv=$START;$PLAIN_ENV" "$(step_with '' 'export MY_PINGBUS_X=1')"
check "TEAMCHAT_MEMBERSHIP set by ccy.env.local" "rc=1" \
    "$(step_with '' 'export TEAMCHAT_MEMBERSHIP=/run/teamchat/membership')"
check "  named in the refusal" "yes" "$(err_has "TEAMCHAT_MEMBERSHIP")"
check "TEAMCHAT_MEMBERSHIP unset by the tracked ccy.env" "rc=1" \
    "$(step_with 'unset TEAMCHAT_MEMBERSHIP' '' TEAMCHAT_MEMBERSHIP=/run/teamchat/membership)"
check "a TEAMCHAT_ name changed by ccy.env.local" "rc=1" \
    "$(step_with '' 'export TEAMCHAT_X=2' TEAMCHAT_X=1)"

echo "=== the real pingbus seat exec runs the command line the entrypoint builds ==="

# ccy-start-claude's v1 command line (its Go tests pin this shape: the claim, the wrapper with its
# own --, then claude with the plugin and settings); here that argv, from "seat" on, goes to the real
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
PLUGIN_ARGV=(--plugin-dir "$image/plugin/pingbus" --settings "$work/session-home/settings.json")
# v1_argv <args...>: the command line ccy-start-claude builds for a seated session.
v1_argv() { printf '%s\0' "$image/pingbus" seat exec -- "$@" >"$work/argv.bin"; }
v1_argv "$bin/wrap" --arm -- claude "${PLUGIN_ARGV[@]}" --dangerously-skip-permissions hi
check "with a wrapper: the wrapper gets its own --, then claude with the plugin" \
    "wrap=--arm|--|claude|$PLUGIN_ARGS|--dangerously-skip-permissions|hi;$REPLAYED" "$(replay)"
check "  and pingbus said which seats it claimed" "yes" "$(err_has "agent team bus: dev1@team-a")"
v1_argv claude "${PLUGIN_ARGV[@]}" --x
check "no wrapper: claude itself, with the plugin" "claude=$PLUGIN_ARGS|--x;$REPLAYED" "$(replay)"
check "  the settings claude is given exist, accepting inbound" "accept" \
    "$(jq -r .crossSessionInbound "$work/session-home/settings.json")"
check "  a seat is held, so the active flag is up" "yes" "$(yes_no test -f "$work/session-home/active")"
check "  and the project's own status line is kept for when no seat is held" "project-line" \
    "$(cat "$work/session-home/statusline-inner")"
write_desired
v1_argv claude "${PLUGIN_ARGV[@]}" --x
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
v1_argv claude "${PLUGIN_ARGV[@]}" --x
check "a seat with no directory: the real claim refuses (78)" "rc=78" "$(replay)"
check "  naming the seat" "yes" "$(err_has "qa2@team-b")"
export PINGBUS_MEMBERSHIP=off

echo "=== the image and the play ship what the step needs ==="

: >"$work/step.err"

check "the Dockerfile makes the zipapp executable" "yes" \
    "$(yes_no grep -q 'chmod 755 /opt/claude-yolo/optional/agent-bus/pingbus' "$CCY_DIR/Dockerfile")"
# The zipapp and the Go status line are built by the Dockerfile's builder stage (issue 235), into
# /out/optional/, which the runtime stage copies over the context's optional/.
check "the builder stage builds the zipapp with the shared builder" "yes" \
    "$(yes_no grep -qF -- '-m helpers.pingbus.bundle --source /src --out /out/optional/agent-bus/pingbus pingbus' "$CCY_DIR/Dockerfile")"
check "the image copies the builder's optional/ over the context's" "yes" \
    "$(yes_no grep -qxF 'COPY --from=ccy-builder /out/optional/ /opt/claude-yolo/optional/' "$CCY_DIR/Dockerfile")"
check "the builder stage builds the Go status line static" "yes" \
    "$(yes_no awk '/^ENV / && / CGO_ENABLED=0/ { s = 1 }
        s && /-o \/out\/optional\/agent-bus\/pingbus-status-line \.\/cmd\/pingbus-status-line/ { d = 1 }
        END { exit !(s && d) }' "$CCY_DIR/Dockerfile")"
check "the play no longer builds either into the build context" "no" \
    "$(yes_no grep -qE 'helpers.pingbus.bundle|go_command_name: pingbus-status-line' "$PLAY")"
check "the Dockerfile links it onto PATH, where the seated guard runs it by name" "yes" \
    "$(yes_no grep -qF 'ln -s /opt/claude-yolo/optional/agent-bus/pingbus-status-line /usr/local/bin/pingbus-status-line' "$CCY_DIR/Dockerfile")"
check "  by the name the seated guard runs" "yes" \
    "$(cd "$REPO_ROOT" && python3 -I -c 'import sys; sys.path.insert(0, "."); from helpers.pingbus import seat; print("yes" if seat.STATUS_LINE_GUARD.endswith("exec pingbus-status-line") else "no")')"
check "the step's settings path is the claim's session home" "yes" \
    "$(yes_no grep -qF -- 'SessionHome:       "/tmp/pingbus-home"' "$REPO_ROOT/src-go/internal/ccystart/ccystart.go")"
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
