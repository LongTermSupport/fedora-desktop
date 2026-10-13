#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Unit-test cc's agent team bus library, files/var/local/claude-code/cc-teams.bash
# (Plan 00177 Task 4.10).
#
# WHY THIS TEST EXISTS. `cc --teams <team>` puts the desktop Claude Code session in a team as
# a `host` member that runs as the owner, beside their Element profile. The guard is cc's
# alone and it is instruction, not containment: the session's own text tells it only to post
# through pingbus, as itself, and never mentions Element or anyone else's sign-in; cc's own
# settings file denies Claude's file tools Element's folders. The owner accepts the
# arrangement once per seat at a confirm the session never sees, and only that record lets
# pingbus run beside Element. Each of these is a property a later edit could quietly lose, so
# each is checked here: the argument split, the seat chosen and taken as `host` through
# agent-bus, the confirm (declined, accepted, asked once), the merged settings, the session's
# text, and the command line that claims the seat.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
LIB="$REPO_ROOT/files/var/local/claude-code/cc-teams.bash"
DENY="$REPO_ROOT/files/var/local/claude-code/cc-teams-settings.json"
KIT_SRC="$REPO_ROOT/files/opt/claude-yolo/optional/agent-bus"
PLAY="$REPO_ROOT/playbooks/imports/play-claude-yolo.yml"

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
        if [ -s "$work/err" ]; then
            echo "        stderr: $(cat "$work/err")"
        fi
    fi
}
yes_no() { if "$@"; then echo yes; else echo no; fi; }

# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$REPO_ROOT/files/var/local/claude-yolo/lib/common-pure.bash"
# shellcheck source=../files/var/local/claude-code/cc-teams.bash
source "$LIB"

# A fake agent-bus: `seat choose` prints what $work/choose holds; `seat take` records its
# argv and makes the seat's directory with a bundle, as the real one does.
bin="$work/bin"
mkdir -p "$bin"
cat >"$bin/agent-bus" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_WORK/agent-bus.calls"
case "$1 $2" in
    "seat choose") cat "$FAKE_WORK/choose" ;;
    "seat take")
        ref=$3; seat=${ref%@*}; team=${ref#*@}
        mkdir -p ".claude/ccy/pingbus/seats/$team/$seat"
        printf 'CHANGED\tseat %s\n' "$ref"
        ;;
    *) exit 64 ;;
esac
EOF
# A fake gum: `confirm` answers with $FAKE_GUM_ANSWER (0 yes, 1 no) and counts the asks.
cat >"$bin/gum" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_WORK/gum.calls"
exit "${FAKE_GUM_ANSWER:-1}"
EOF
chmod 755 "$bin/agent-bus" "$bin/gum"
export PATH="$bin:$PATH" FAKE_WORK="$work"
CC_TEAMS_AGENT_BUS="$bin/agent-bus"
CC_TEAMS_TTY=/dev/null

echo "=== the arguments ==="

cc_teams_split_args --teams long-term-support --resume abc 2>"$work/err"
check "--teams <team> is taken out" "long-term-support|--resume abc" "$CC_TEAM|${CC_TEAMS_REST[*]}"
cc_teams_split_args --model x --teams=dev-team 2>"$work/err"
check "--teams=<team> is taken out" "dev-team|--model x" "$CC_TEAM|${CC_TEAMS_REST[*]}"
cc_teams_split_args --resume 2>"$work/err"
check "no --teams: no team, every argument kept" "|--resume" "$CC_TEAM|${CC_TEAMS_REST[*]}"
for bad in "dev1@dev-team" "a,b" ""; do
    rc=0
    cc_teams_split_args --teams "$bad" 2>"$work/err" || rc=$?
    check "--teams '$bad' is refused 64" "64" "$rc"
done
rc=0
cc_teams_split_args --teams 2>"$work/err" || rc=$?
check "--teams with no value is refused 64" "64" "$rc"
rc=0
cc_teams_split_args --teams a --teams b 2>"$work/err" || rc=$?
check "--teams twice is refused 64" "64" "$rc"

echo "=== the seat: chosen and taken as host ==="

checkout="$work/checkout"
mkdir -p "$checkout"
{
    printf 'CHOICE\t%s\t%s\tmyrepo.%s+local.host\tdesktop (host)\n' 1 held 1 2 free 2 3 new 3
    printf 'DEFAULT\t2\n'
} >"$work/choose"
(cd "$checkout" && cc_teams_seat dev-team >"$work/out" 2>"$work/err" && printf '%s|%s\n' "$CC_SEAT" "$CC_HANDLE" >"$work/seat")
check "the default seat and its handle" "2|myrepo.2+local.host" "$(cat "$work/seat")"
check "choose asked for a host seat" "seat choose dev-team --type=host" "$(cat "$work/agent-bus.calls")"
check "nothing on stdout" "" "$(cat "$work/out")"
printf 'CHOICE\t1\theld\tmyrepo.1+local.host\n' >"$work/choose"
rc=0
(cd "$checkout" && cc_teams_seat dev-team) 2>"$work/err" || rc=$?
check "no DEFAULT line is refused 78" "78" "$rc"
rm -f "$work/agent-bus.calls"
(cd "$checkout" && cc_teams_take 2 dev-team >"$work/out" 2>"$work/err")
check "take names the seat and the host type" "seat take 2@dev-team --type=host" "$(cat "$work/agent-bus.calls")"
check "take's report goes to stderr" "" "$(cat "$work/out")"
check "the seat exists" "yes" "$(yes_no test -d "$checkout/.claude/ccy/pingbus/seats/dev-team/2")"

echo "=== the owner's confirm, once per seat ==="

seat_dir="$checkout/.claude/ccy/pingbus/seats/dev-team/2"
record="$seat_dir/element-accepted"
rc=0
FAKE_GUM_ANSWER=1 cc_teams_accept "$seat_dir" myrepo.2+local.host 2>"$work/err" || rc=$?
check "declined: not started (1)" "1" "$rc"
check "declined: no record" "no" "$(yes_no test -e "$record")"
rc=0
FAKE_GUM_ANSWER=0 cc_teams_accept "$seat_dir" myrepo.2+local.host 2>"$work/err" || rc=$?
check "accepted" "0" "$rc"
check "accepted: the record is a file only its owner reads" "600" "$(stat -c %a "$record")"
asks=$(wc -l <"$work/gum.calls")
rc=0
FAKE_GUM_ANSWER=1 cc_teams_accept "$seat_dir" myrepo.2+local.host 2>"$work/err" || rc=$?
check "asked once: the record answers" "0|$asks" "$rc|$(wc -l <"$work/gum.calls")"
check "the confirm defaults to No" "yes" "$(yes_no grep -q -- '--default=false' "$work/gum.calls")"
rm "$record"
ln -s "$work/elsewhere" "$record"
rc=0
FAKE_GUM_ANSWER=0 cc_teams_accept "$seat_dir" myrepo.2+local.host 2>"$work/err" || rc=$?
check "a record that is a symlink is refused 78" "78" "$rc"
check "nothing was written through it" "no" "$(yes_no test -e "$work/elsewhere")"
rm "$record"

echo "=== the settings: the kit's, plus cc's deny rules ==="

CC_TEAMS_KIT="$KIT_SRC"
CC_TEAMS_DENY_SETTINGS="$DENY"
cc_teams_settings "$work/settings.json" 2>"$work/err"
check "the bus can wake an idle session" "accept" "$(jq -r .crossSessionInbound "$work/settings.json")"
# Claude Code's home-relative rule form, a literal tilde.
home_rel=\~
for path in .var/app/im.riot.Riot/** .config/Element/** .config/Element-*/**; do
    for tool in Read Edit; do
        rule="$tool($home_rel/$path)"
        check "deny $rule" "true" \
            "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$work/settings.json")"
    done
done
check "the settings file is the owner's alone" "600" "$(stat -c %a "$work/settings.json")"

echo "=== what the session reads ==="

prompt=$(cc_teams_prompt myrepo.2+local.host dev-team)
check "it names its own handle" "yes" "$(yes_no grep -qF myrepo.2+local.host <<<"$prompt")"
check "it says to post through pingbus" "yes" "$(yes_no grep -q pingbus <<<"$prompt")"
check "it never mentions Element, sign-ins or other people's accounts" "no" \
    "$(yes_no grep -qiE 'element|riot|matrix session|sign|login|log in|owner|human|as anyone|impersonat' <<<"$prompt")"
check "the plugin's text never mentions the owner's Element session either" "no" \
    "$(yes_no grep -rqiE "owner'?s element|your element|element session|im\.riot" "$KIT_SRC/plugin")"

echo "=== the command line ==="

cc_teams_command "$work/tmp" "$checkout" 2@dev-team myrepo.2+local.host dev-team --resume abc
got=$(printf '%s\n' "${CC_TEAMS_CMD[@]}")
want=$(printf '%s\n' env PINGBUS_SEATS=2@dev-team "$KIT_SRC/pingbus" seat exec --root "$checkout/.claude/ccy/pingbus/seats" \
    --home "$work/tmp/pingbus-home" -- claude --plugin-dir "$KIT_SRC/plugin/pingbus" \
    --settings "$work/tmp/settings.json" --append-system-prompt "$prompt" --resume abc)
check "pingbus seat exec claims the checkout's seat, then claude with plugin and settings" "$want" "$got"

echo "=== the play installs both files beside cc ==="

for file in cc-teams.bash cc-teams-settings.json; do
    check "play-claude-yolo.yml installs $file" "yes" \
        "$(yes_no grep -qx " *- $file" "$PLAY")"
done

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
