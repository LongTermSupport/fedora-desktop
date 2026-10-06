#!/usr/bin/env bash
# Unit-test the entrypoint's agent team bus step (Plan 00161 DESIGN.md section 5.3).
#
# WHY THIS TEST EXISTS. A ccy checkout joins the bus with PINGBUS_TEAMS in its untracked
# ccy.env.local, which the launcher binds read-only. The entrypoint must then refuse to start
# on a bundle pingbus refuses, put pingbus on PATH, and give claude the plugin and settings,
# after the supervisor wrapper's `--`; unset, it must change nothing. The test cuts the real
# PROJECT-ENV block and the real tail (from the AGENT-BUS marker to the end, its final exec
# replaced by a printer) out of entrypoint.sh, points their paths at a throwaway tree, and
# runs them against the real pingbus zipapp, built by helpers/pingbus/bundle.py as the play
# builds it, and the real plugin and settings.
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

echo "=== the image tree: the real zipapp, plugin and settings ==="

image="$work/image/agent-bus"
mkdir -p "$image"
cp -r "$KIT_SRC/plugin" "$KIT_SRC/settings.json" "$image/"
marker=$(cd "$REPO_ROOT" && python3 -s -m helpers.pingbus.bundle --source "$REPO_ROOT" \
    --out "$image/pingbus" pingbus 2>"$work/step.err")
check "the zipapp builds" "BUNDLE-CHANGED $image/pingbus" "$marker"

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
check "the tail ends in the exec" "yes" "$(yes_no grep -q '^exec "\$@"$' "$step")"
check "every image path was redirected" "0" "$(grep -c '/opt/claude-yolo/optional\|/usr/local/bin/pingbus' "$step")"

# A function named exec takes precedence over the builtin, so the tail's exec prints what the
# session would get instead of replacing the shell.
cat >"$work/exec-stub.bash" <<STUB
exec() {
    local IFS='|'
    printf 'argv=%s\n' "\$*"
    printf 'home=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_HOME-unset}"')"
    printf 'teams=%s\n' "\$(bash -c 'printf %s "\${PINGBUS_TEAMS-unset}"')"
    if [ -L "$bin/pingbus" ]; then printf 'link=%s\n' "\$(readlink "$bin/pingbus")"; else printf 'link=none\n'; fi
    builtin exit 0
}
STUB

# run_step <wrapper|-> <args...>: the stub's lines joined with ';', or "rc=<n>" when the
# step refused. "-" runs with no supervisor at all. Inherits the caller's environment.
# PROJECT-ENV announces each file it sources on stdout; anything else there, the bus step's
# own output included, lands in the result and fails the exact comparisons.
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

mkdir -p "$work/home"
SUP="python3 $ccy/claude-supervise.py --"

# A valid bundle for team-a: reserved example values only (CLAUDE/ExampleValues.md).
write_bundle() {
    local home="$1" team="$2" sn="$2.agent-bus.internal"
    mkdir -p "$home/$team"
    chmod 700 "$home" "$home/$team"
    printf '{"protocol":1,"team":"%s","user_id":"@myrepo.1+workstation.podman:%s","server_name":"%s","base_url":"http://192.0.2.10:8448","plain_http_hosts":["192.0.2.10"],"token_file":"token","admin":"@admin:%s","room":"!%s"}\n' \
        "$team" "$sn" "$sn" "$sn" "$(printf 'A%.0s' $(seq 43))" >"$home/$team/member.json"
    printf 'syt_ZXhhbXBsZQ_notarealtoken_0123' >"$home/$team/token"
    chmod 600 "$home/$team/token"
}

echo "=== not opted in: inert ==="

check "no env files: argv, environment and PATH untouched" \
    "argv=python3|$ccy/claude-supervise.py|--|claude|--dangerously-skip-permissions;home=unset;teams=unset;link=none" \
    "$(run_step "$SUP" claude --dangerously-skip-permissions)"
printf 'export HOOKS_DAEMON_HOSTNAME=role-a\n' >"$ccy/ccy.env.local"
check "a ccy.env.local without PINGBUS_TEAMS: untouched" \
    "argv=claude|--x;home=unset;teams=unset;link=none" "$(run_step - claude --x)"
printf 'export PINGBUS_TEAMS=\n' >"$ccy/ccy.env.local"
check "an empty PINGBUS_TEAMS in ccy.env.local: untouched" \
    "argv=claude|--x;home=unset;teams=;link=none" "$(run_step - claude --x)"
check "not opted in: a command other than claude is left alone" \
    "argv=bash|-l;home=unset;teams=;link=none" "$(run_step - bash -l)"

echo "=== opted in from ccy.env.local ==="

write_bundle "$ccy/pingbus" team-a
printf '# based on ccy.env.local.dist version 2\nexport PINGBUS_TEAMS=team-a\n' >"$ccy/ccy.env.local"
PLUGIN_ARGS="--plugin-dir|$image/plugin/pingbus|--settings|$image/settings.json"
check "the plugin and settings come after the wrapper's -- and right after claude" \
    "argv=python3|$ccy/claude-supervise.py|--|claude|$PLUGIN_ARGS|--dangerously-skip-permissions|hi;home=$ccy/pingbus;teams=team-a;link=$image/pingbus" \
    "$(run_step "$SUP" claude --dangerously-skip-permissions hi)"
check "the launch names the teams on stderr" "yes" "$(err_has "team-a")"
check "an armed wrapper keeps its own flags before its --" \
    "argv=python3|$ccy/claude-supervise.py|--arm|--|claude|$PLUGIN_ARGS;home=$ccy/pingbus;teams=team-a;link=$image/pingbus" \
    "$(run_step "python3 $ccy/claude-supervise.py --arm --" claude)"
check "no supervisor: the plain exec gets them too" \
    "argv=claude|$PLUGIN_ARGS|--x;home=$ccy/pingbus;teams=team-a;link=$image/pingbus" \
    "$(run_step - claude --x)"

write_bundle "$work/elsewhere" team-a
printf 'export PINGBUS_TEAMS=team-a\nexport PINGBUS_HOME=%s\n' "$work/elsewhere" >"$ccy/ccy.env.local"
check "a PINGBUS_HOME set in ccy.env.local is kept" \
    "argv=claude|$PLUGIN_ARGS;home=$work/elsewhere;teams=team-a;link=$image/pingbus" \
    "$(run_step - claude)"

echo "=== opted in, but broken: refused, nothing installed ==="

printf 'export PINGBUS_TEAMS=team-a,team-b\n' >"$ccy/ccy.env.local"
check "a listed team with no bundle: refused" "rc=1" "$(run_step "$SUP" claude)"
check "the refusal carries pingbus's reason" "yes" "$(err_has "team-b")"
check "the refusal says it was pingbus config check" "yes" "$(err_has "pingbus config check")"
check "a refused launch links nothing" "no" "$(yes_no test -L "$bin/pingbus")"

printf 'export PINGBUS_TEAMS=team-a\n' >"$ccy/ccy.env.local"
printf '{}\n' >"$ccy/pingbus/team-a/member.json"
check "a malformed member.json: refused" "rc=1" "$(run_step - claude)"
write_bundle "$ccy/pingbus" team-a
chmod 644 "$ccy/pingbus/team-a/token"
check "a token readable by others: refused" "rc=1" "$(run_step - claude)"
chmod 600 "$ccy/pingbus/team-a/token"
check "the repaired bundle: accepted again" \
    "argv=claude|$PLUGIN_ARGS;home=$ccy/pingbus;teams=team-a;link=$image/pingbus" "$(run_step - claude)"

printf 'export PINGBUS_TEAMS=Team_A\n' >"$ccy/ccy.env.local"
check "a malformed team list: refused" "rc=1" "$(run_step - claude)"
printf 'export PINGBUS_TEAMS=team-a\n' >"$ccy/ccy.env.local"

check "opted in with a command other than claude: refused" "rc=1" "$(run_step - bash -l)"
check "that refusal names the command" "yes" "$(err_has "'bash'")"

mv "$image/settings.json" "$work/settings.json.away"
check "an image without the kit: refused" "rc=1" "$(run_step - claude)"
check "that refusal says to rebuild" "yes" "$(err_has "ccy --rebuild")"
mv "$work/settings.json.away" "$image/settings.json"

echo "=== opt-in comes from ccy.env.local only ==="

rm "$ccy/ccy.env.local"
printf 'export PINGBUS_TEAMS=team-a\n' >"$ccy/ccy.env"
check "PINGBUS_TEAMS in the tracked ccy.env: refused" "rc=1" "$(run_step - claude)"
check "that refusal names ccy.env.local" "yes" "$(err_has "ccy.env.local")"
printf 'export PINGBUS_TEAMS=team-a\n' >"$ccy/ccy.env.local"
check "in ccy.env as well as ccy.env.local: refused" "rc=1" "$(run_step - claude)"
rm "$ccy/ccy.env"
check "from the container environment: refused" "rc=1" "$(PINGBUS_TEAMS=team-a run_step - claude)"
rm "$ccy/ccy.env.local"
check "from the container environment with no ccy.env.local: refused" "rc=1" \
    "$(PINGBUS_TEAMS=team-a run_step - claude)"

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
