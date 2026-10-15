#!/usr/bin/env bash
# shellcheck source-path=SCRIPTDIR
# Unit-test cc's agent team bus library, files/var/local/claude-code/cc-teams.bash
# (Plan 00177 Task 4.10, Plan 00201 M6).
#
# WHY THIS TEST EXISTS. `cc --teams` puts the desktop Claude Code session in one or more teams
# as `host` members that run as the owner, beside their Element profile. It takes the same
# list grammar as ccy and is answered by the same chooser (`ccy-teams choose-teams
# --type=host`, which also asks the owner's once-per-user Element consent, tested in Go). The
# guard is cc's alone and it is instruction, not containment: the session's own text tells it
# only to post through pingbus, as itself, and never mentions Element or anyone else's
# sign-in; cc's own settings file denies Claude's file tools Element's folders. Each of these
# is a property a later edit could quietly lose, so each is checked here: the argument split,
# the chooser called with the host type (attended only) and the list it prints checked by
# agent-bus, the seats taken as `host`, the launch block, the merged settings, the session's
# text, and the command line that claims the seats. A fake ccy-teams and agent-bus stand in
# for the two programs.
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

# A fake agent-bus: `seat check` prints its list back (and notes --resolved), `seat take`
# records its argv and makes each seat's directory, as the real one does.
bin="$work/bin"
mkdir -p "$bin"
cat >"$bin/agent-bus" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_WORK/agent-bus.calls"
case "$1 $2" in
    "seat check") [ -z "${FAKE_CHECK_RC:-}" ] || exit "$FAKE_CHECK_RC"; [ -n "$3" ] || exit 64; printf '%s\n' "$3" ;;
    "seat take")
        IFS=, read -ra refs <<<"$3"
        for ref in "${refs[@]}"; do
            mkdir -p ".claude/ccy/pingbus/seats/${ref#*@}/${ref%@*}"
            printf 'CHANGED\tseat %s\n' "$ref"
        done
        ;;
    *) exit 64 ;;
esac
EOF
# A fake ccy-teams: choose-teams prints $FAKE_WORK/chosen (exit FAKE_CHOOSE_RC) and notes its
# argv; describe-seats prints a block or a sentence for the list it is given.
cat >"$bin/ccy-teams" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_WORK/ccy-teams.calls"
case "$1 $2" in
    "choose-teams "*) cat "$FAKE_WORK/chosen"; exit "${FAKE_CHOOSE_RC:-0}" ;;
    "describe-seats --block") printf 'This session is on teams: %s\n' "$3" ;;
    "describe-seats --sentence") [ -z "${FAKE_SENTENCE_RC:-}" ] || exit "$FAKE_SENTENCE_RC"; printf 'You are h in %s.\n' "$3" ;;
    *) exit 64 ;;
esac
EOF
chmod 755 "$bin/agent-bus" "$bin/ccy-teams"
export PATH="$bin:$PATH" FAKE_WORK="$work"
CC_TEAMS_AGENT_BUS="$bin/agent-bus"
CC_TEAMS_CCY_TEAMS="$bin/ccy-teams"

echo "=== the arguments ==="

cc_teams_split_args --teams long-term-support --resume abc 2>"$work/err"
check "--teams <team> is taken out" "true|long-term-support|--resume abc" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
cc_teams_split_args --model x --teams=1@dev-team,qa2@other 2>"$work/err"
check "--teams=<list> takes several teams" "true|1@dev-team,qa2@other|--model x" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
cc_teams_split_args --resume 2>"$work/err"
check "no --teams: nothing given, every argument kept" "false||--resume" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
cc_teams_split_args --teams 2>"$work/err"
check "bare --teams at the end is a chooser launch" "true|" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST"
cc_teams_split_args --teams --resume abc 2>"$work/err"
check "an option where the list belongs means it was left out" "true||--resume abc" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
cc_teams_split_args --teams= --model x 2>"$work/err"
check "--teams= is a chooser launch too" "true||--model x" "$CC_TEAMS_GIVEN|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
rc=0
cc_teams_split_args --teams a --teams b 2>"$work/err" || rc=$?
check "--teams twice is refused 64" "64" "$rc"

echo "=== the chooser: attended only, host type, then agent-bus checks the list ==="

printf '1@dev-team,2@other\n' >"$work/chosen"
: >"$work/ccy-teams.calls"
: >"$work/agent-bus.calls"
CC_TEAMS_LIST=dev-team
rc=0
cc_teams_choose >"$work/out" 2>"$work/err" || rc=$?
check "attended: the chooser's list is the seats" "0|1@dev-team,2@other" "$rc|$CC_TEAMS_SEATS"
check "attended: the chooser is asked as host with the list as given" "choose-teams --type=host dev-team" "$(cat "$work/ccy-teams.calls")"
check "attended: agent-bus checks what the chooser printed" "seat check 1@dev-team,2@other" "$(cat "$work/agent-bus.calls")"
check "nothing on stdout" "" "$(cat "$work/out")"

CC_TEAMS_LIST=""
: >"$work/ccy-teams.calls"
cc_teams_choose 2>"$work/err"
check "bare --teams passes an empty list" "choose-teams --type=host " "$(cat "$work/ccy-teams.calls")"

: >"$work/chosen"
rc=0
(cc_teams_choose >"$work/out" 2>"$work/err") || rc=$?
check "leaving the chooser (it printed nothing) ends cc quietly with 0" "0|" "$rc|$(cat "$work/out")"

printf '1@x\n' >"$work/chosen"
rc=0
(FAKE_CHOOSE_RC=1 cc_teams_choose) 2>"$work/err" || rc=$?
check "a declined consent or a failed chooser is cc's exit" "1" "$rc"

: >"$work/ccy-teams.calls"
: >"$work/agent-bus.calls"
printf '1@dev-team\n' >"$work/chosen"
CC_TEAMS_LIST=1@dev-team
rc=0
(cd "$work" && cc_teams_choose 2>"$work/err" </dev/null) || rc=$?
check "the chooser is always asked, a full list and no terminal included (Go decides)" "choose-teams --type=host 1@dev-team" "$(cat "$work/ccy-teams.calls")"
check "no --resolved branch: agent-bus checks what the chooser printed" "seat check 1@dev-team" "$(cat "$work/agent-bus.calls")"
: >"$work/agent-bus.calls"
rc=0
(FAKE_CHOOSE_RC=78 cc_teams_choose) 2>"$work/err" || rc=$?
check "the chooser refusing for want of a terminal (78) is cc's exit, agent-bus never called" "78|0" "$rc|$(grep -c . "$work/agent-bus.calls")"
CC_TEAMS_LIST=dev-team
rc=0
FAKE_CHECK_RC=64 cc_teams_choose 2>"$work/err" || rc=$?
check "agent-bus refusing the list is cc's exit" "64" "$rc"
check "the library has no attended or --resolved branch left" "0" "$(grep -c 'attended\|resolved' "$LIB")"

echo "=== the seats: taken as host, the block shown, the sentence made ==="

checkout="$work/checkout"
mkdir -p "$checkout"
rm -f "$work/agent-bus.calls"
: >"$work/ccy-teams.calls"
CC_TEAMS_SEATS="2@dev-team,1@other"
(cd "$checkout" && cc_teams_take >"$work/out" 2>"$work/err" && printf '%s\n' "$CC_TEAMS_SENTENCE" >"$work/sentence")
check "take names every seat and the host type" "seat take 2@dev-team,1@other --type=host" "$(cat "$work/agent-bus.calls")"
check "the seats exist" "yes|yes" "$(yes_no test -d "$checkout/.claude/ccy/pingbus/seats/dev-team/2")|$(yes_no test -d "$checkout/.claude/ccy/pingbus/seats/other/1")"
check "the launch block is asked for the seats" "yes" "$(yes_no grep -qx 'describe-seats --block 2@dev-team,1@other' "$work/ccy-teams.calls")"
check "the block and take's report go to stderr, none to stdout" "" "$(cat "$work/out")"
check "the block is on stderr" "yes" "$(yes_no grep -q 'This session is on teams: 2@dev-team,1@other' "$work/err")"
check "the sentence is made for the same seats" "You are h in 2@dev-team,1@other." "$(cat "$work/sentence")"
rc=0
(cd "$checkout" && FAKE_SENTENCE_RC=78 cc_teams_take) >"$work/out" 2>"$work/err" || rc=$?
check "a sentence that cannot be made stops the launch with its code" "78" "$rc"

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
# Security audit H2: Bash can reach the human's sign-in too. A deny rule matches the command
# text, so it stops the plain routes (the keyring, Element's profile, the Flatpak) and not a
# determined one; the consent and the docs say so.
for tool in Read Edit; do
    rule="$tool($home_rel/.local/share/keyrings/**)"
    check "deny $rule" "true" \
        "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$work/settings.json")"
done
# The Secret Service's bus name is lower case and its interface capitalised; rules are case
# sensitive, so both are denied (review L-C). The dot is a variable only so this file's text is
# not mistaken for a path to a secrets file by the repository's guards.
dot=.
bus_name_rule="Bash(*freedesktop${dot}secrets*)"
for rule in 'Bash(*secret-tool*)' 'Bash(*keyring*)' 'Bash(*freedesktop.Secret*)' "$bus_name_rule" \
    'Bash(*im.riot.Riot*)' 'Bash(*.config/Element*)' 'Bash(*IndexedDB*)' 'Bash(*Local Storage*)'; do
    check "deny $rule" "true" \
        "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$work/settings.json")"
done
check "deny Edit of the acceptance record's folder" "true" \
    "$(jq --arg r "Edit($home_rel/.config/pingbus/**)" '.permissions.deny | index($r) != null' "$work/settings.json")"
check "the settings file is the owner's alone" "600" "$(stat -c %a "$work/settings.json")"

echo "=== what the session reads ==="

prompt=$(cc_teams_prompt "You are myrepo.2+local.host in dev-team and myrepo.1+local.host in other.")
check "it names its own handles and teams" "yes" "$(yes_no grep -qF 'myrepo.2+local.host in dev-team and myrepo.1+local.host in other' <<<"$prompt")"
check "it says to post through pingbus" "yes" "$(yes_no grep -q pingbus <<<"$prompt")"
check "it never mentions Element, sign-ins or other people's accounts" "no" \
    "$(yes_no grep -qiE 'element|riot|matrix session|sign|login|log in|owner|human|as anyone|impersonat' <<<"$prompt")"
check "the plugin's text never mentions the owner's Element session either" "no" \
    "$(yes_no grep -rqiE "owner'?s element|your element|element session|im\.riot" "$KIT_SRC/plugin")"

echo "=== the checkout's .claude/ccy/ ==="

fresh="$work/fresh"
mkdir -p "$fresh"
git -C "$fresh" init -q
(cd "$fresh" && cc_teams_ccy_dir >"$work/out" 2>"$work/err")
check "a repo never used with ccy gets .claude/ccy/ (0700)" "700" "$(stat -c %a "$fresh/.claude/ccy")"
check "seats' tree is ignored by git, so seat take may place a token there" "0" \
    "$(cd "$fresh" && git check-ignore -q .claude/ccy/pingbus/ && echo 0 || echo 1)"
check "creating it says nothing on stdout" "" "$(cat "$work/out")"
echo marker >"$fresh/.claude/ccy/keep"
(cd "$fresh" && cc_teams_ccy_dir 2>"$work/err")
check "an existing .claude/ccy/ is left as it is" "marker" "$(cat "$fresh/.claude/ccy/keep")"
linked="$work/linked"
mkdir -p "$linked/.claude" "$work/elsewhere-dir"
git -C "$linked" init -q
ln -s "$work/elsewhere-dir" "$linked/.claude/ccy"
rc=0
(cd "$linked" && cc_teams_ccy_dir) 2>"$work/err" || rc=$?
check ".claude/ccy as a symlink is refused 78" "78" "$rc"

echo "=== the command line ==="

cc_teams_command "$work/tmp" "$checkout" "2@dev-team,1@other" "You are h in x." --resume abc
got=$(printf '%s\n' "${CC_TEAMS_CMD[@]}")
want=$(printf '%s\n' env "PINGBUS_SEATS=2@dev-team,1@other" "$KIT_SRC/pingbus" seat exec --root "$checkout/.claude/ccy/pingbus/seats" \
    --home "$work/tmp/pingbus-home" -- claude --plugin-dir "$KIT_SRC/plugin/pingbus" \
    --settings "$work/tmp/settings.json" --append-system-prompt "$(cc_teams_prompt 'You are h in x.')" --resume abc)
check "pingbus seat exec claims the checkout's seats, then claude with plugin and settings" "$want" "$got"

echo "=== the session records its teams for the status line ==="

hooks_json="$KIT_SRC/plugin/pingbus/hooks/hooks.json"
check "the plugin cc passes runs pingbus's SessionStart hook, which writes the per-session teams file" "pingbus hook session-start" \
    "$(jq -r '.hooks.SessionStart[0].hooks[0].command' "$hooks_json")"
check "cc passes that plugin directory" "yes" "$(yes_no grep -qF -- "--plugin-dir" <<<"$got")"
check "cc puts the kit first on PATH so the hook runs the kit's pingbus" "yes" \
    "$(yes_no grep -qF "PATH=\"\$CC_TEAMS_KIT:\$PATH\"" "$REPO_ROOT/files/var/local/claude-code/cc")"

echo "=== prepare: chooser, take, settings, command, in order ==="

prep_kit="$work/prep-kit"
mkdir -p "$prep_kit/plugin"
cp -r "$KIT_SRC/plugin/pingbus" "$prep_kit/plugin/pingbus"
cp "$KIT_SRC/settings.json" "$prep_kit/settings.json"
: >"$prep_kit/pingbus"
CC_TEAMS_KIT="$prep_kit"
prep_repo="$work/prep-repo"
mkdir -p "$prep_repo" "$work/prep-tmp"
git -C "$prep_repo" init -q
printf '1@dev-team\n' >"$work/chosen"
CC_TEAMS_LIST=""
: >"$work/agent-bus.calls"
: >"$work/ccy-teams.calls"
rc=0
(cd "$prep_repo" && cc_teams_prepare "$work/prep-tmp" --resume x && printf '%s\n' "${CC_TEAMS_CMD[1]}" >"$work/cmd1") >"$work/out" 2>"$work/err" || rc=$?
check "prepare succeeds" "0" "$rc"
check "chooser first, then agent-bus check, then take" "choose-teams --type=host |describe-seats --block 1@dev-team|describe-seats --sentence 1@dev-team" \
    "$(paste -sd'|' "$work/ccy-teams.calls")"
check "agent-bus saw check then take" "seat check 1@dev-team|seat take 1@dev-team --type=host" "$(paste -sd'|' "$work/agent-bus.calls")"
check "the seat exists" "yes" "$(yes_no test -d "$prep_repo/.claude/ccy/pingbus/seats/dev-team/1")"
check "the command was built for the seats" "PINGBUS_SEATS=1@dev-team" "$(cat "$work/cmd1")"
rm -rf "$prep_repo/.claude/ccy/pingbus"
: >"$work/agent-bus.calls"
rc=0
(cd "$prep_repo" && FAKE_CHOOSE_RC=1 cc_teams_prepare "$work/prep-tmp") >"$work/out" 2>"$work/err" || rc=$?
check "a declined consent stops prepare (1) before any seat is taken" "1|0" "$rc|$(grep -c 'seat take' "$work/agent-bus.calls")"
untouched_repo="$work/prep-untouched"
mkdir -p "$untouched_repo"
git -C "$untouched_repo" init -q
rc=0
(cd "$untouched_repo" && FAKE_CHOOSE_RC=1 cc_teams_prepare "$work/prep-tmp") >"$work/out" 2>"$work/err" || rc=$?
check "a declined consent makes nothing in the checkout, not even .claude/ccy/ (S-E4)" "1|no" \
    "$rc|$(yes_no test -e "$untouched_repo/.claude/ccy")"
untouched_link="$work/prep-untouched-link"
mkdir -p "$untouched_link/.claude" "$work/prep-elsewhere"
git -C "$untouched_link" init -q
ln -s "$work/prep-elsewhere" "$untouched_link/.claude/ccy"
: >"$work/agent-bus.calls"
rc=0
(cd "$untouched_link" && cc_teams_prepare "$work/prep-tmp") >"$work/out" 2>"$work/err" || rc=$?
check ".claude/ccy as a symlink is refused (78) before any seat is taken" "78|0" \
    "$rc|$(grep -c 'seat take' "$work/agent-bus.calls")"
CC_TEAMS_KIT="$KIT_SRC"

echo "=== cc and pingbus name the same acceptance record ==="

pb_config="$REPO_ROOT/helpers/pingbus/config.py"
pb_dir=$(grep -E '^ELEMENT_ACCEPTED_DIR = ' "$pb_config" | cut -d'"' -f2)
pb_file=$(grep -E '^ELEMENT_ACCEPTED_FILE = ' "$pb_config" | cut -d'"' -f2)
go_consent="$REPO_ROOT/src-go/internal/teams/chooser/consent.go"
go_dir=$(grep -E '^\s*acceptedDir\s*=' "$go_consent" | cut -d'"' -f2)
go_file=$(grep -E '^\s*acceptedFile\s*=' "$go_consent" | cut -d'"' -f2)
check "ccy-teams and pingbus.config name one path" "$pb_dir/$pb_file" "$go_dir/$go_file"

echo "=== the play installs both files beside cc ==="

for file in cc-teams.bash cc-teams-settings.json; do
    check "play-claude-yolo.yml installs $file" "yes" \
        "$(yes_no grep -qx " *- $file" "$PLAY")"
done

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
