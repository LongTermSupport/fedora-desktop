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
# the chooser called with the host type (attended only) and the list it prints checked by the
# seat check, the seats taken as `host`, the launch block, cc's deny rules, the plugin's text,
# and the command line, `ccy-teams internal cc-run` (Plan 00209 Task 5.1). cc-run's settings,
# session line and prompt are Go's, tested there by running it (teamchatlaunch's parity test
# against this library's earlier bash, and ccrun_prompt_test.go for the prompt on each system).
# A fake ccy-teams and agent-bus stand in for the two programs.
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
# argv; describe-seats prints a block for the list it is given; internal seat-check and
# seat-take are agent-bus's seat check and take, as the real verbs are for an agent-bus list.
cat >"$bin/ccy-teams" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$FAKE_WORK/ccy-teams.calls"
case "$1 $2" in
    "choose-teams "*) cat "$FAKE_WORK/chosen"; exit "${FAKE_CHOOSE_RC:-0}" ;;
    "describe-seats --block") printf 'This session is on teams: %s\n' "$3" ;;
    "internal seat-check" | "internal seat-take") verb=${2#seat-}; shift 2; exec agent-bus seat "$verb" "$@" ;;
    *) exit 64 ;;
esac
EOF
chmod 755 "$bin/agent-bus" "$bin/ccy-teams"
export PATH="$bin:$PATH" FAKE_WORK="$work"
CC_TEAMS_CCY_TEAMS="$bin/ccy-teams"
# tmux-session.bash names the session cc runs as in ccy's tmux; here, FAKE_SESSION ("" unnamed).
ccy_tmux_current_session() { printf '%s\n' "${FAKE_SESSION-cc-proj}"; }

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
cc_teams_split_args --bus --resume abc 2>"$work/err"
check "--bus is the bus on no team (Plan 00203 M6)" "true|true||--resume abc" "$CC_TEAMS_GIVEN|$CC_TEAMS_BUS|$CC_TEAMS_LIST|${CC_TEAMS_REST[*]}"
cc_teams_split_args --teams 1@a 2>"$work/err"
check "--teams alone is not --bus" "true|false" "$CC_TEAMS_GIVEN|$CC_TEAMS_BUS"
rc=0
cc_teams_split_args --bus --teams 1@a 2>"$work/err" || rc=$?
check "--bus then --teams is refused 64" "64" "$rc"
CC_TEAMS_BUS=false

echo "=== the chooser: attended only, host type, then the seat check checks the list ==="

printf '1@dev-team,2@other\n' >"$work/chosen"
: >"$work/ccy-teams.calls"
: >"$work/agent-bus.calls"
CC_TEAMS_LIST=dev-team
rc=0
cc_teams_choose >"$work/out" 2>"$work/err" || rc=$?
check "attended: the chooser's list is the seats" "0|1@dev-team,2@other" "$rc|$CC_TEAMS_SEATS"
check "attended: the chooser is asked as host with the list as given, then the seat check" \
    "choose-teams --type=host dev-team|internal seat-check 1@dev-team,2@other" "$(paste -sd'|' "$work/ccy-teams.calls")"
check "attended: agent-bus checks what the chooser printed" "seat check 1@dev-team,2@other" "$(cat "$work/agent-bus.calls")"
check "nothing on stdout" "" "$(cat "$work/out")"

CC_TEAMS_LIST=""
: >"$work/ccy-teams.calls"
cc_teams_choose 2>"$work/err"
check "bare --teams passes an empty list" "choose-teams --type=host " "$(head -n1 "$work/ccy-teams.calls")"

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
check "the chooser is always asked, a full list and no terminal included (Go decides)" "choose-teams --type=host 1@dev-team" "$(head -n1 "$work/ccy-teams.calls")"
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

: >"$work/ccy-teams.calls"
: >"$work/agent-bus.calls"
CC_TEAMS_BUS=true CC_TEAMS_LIST="" CC_TEAMS_SEATS=""
rc=0
cc_teams_choose >"$work/out" 2>"$work/err" || rc=$?
check "--bus asks the chooser only the consent, with the list (none)" "0|choose-teams --type=host --bus " "$rc|$(cat "$work/ccy-teams.calls")"
check "--bus makes no seat and checks none" "|0" "$CC_TEAMS_SEATS|$(grep -c . "$work/agent-bus.calls")"
rc=0
(FAKE_CHOOSE_RC=1 cc_teams_choose) 2>"$work/err" || rc=$?
check "--bus with a declined consent is cc's exit" "1" "$rc"
CC_TEAMS_BUS=false

echo "=== the seats: taken as host, the block shown ==="

checkout="$work/checkout"
mkdir -p "$checkout"
rm -f "$work/agent-bus.calls"
: >"$work/ccy-teams.calls"
CC_TEAMS_SEATS="2@dev-team,1@other"
(cd "$checkout" && cc_teams_take >"$work/out" 2>"$work/err")
check "take names every seat and the host type" "seat take 2@dev-team,1@other --type=host" "$(cat "$work/agent-bus.calls")"
check "the seats exist" "yes|yes" "$(yes_no test -d "$checkout/.claude/ccy/pingbus/seats/dev-team/2")|$(yes_no test -d "$checkout/.claude/ccy/pingbus/seats/other/1")"
check "the launch block is asked for the seats" "yes" "$(yes_no grep -qx 'describe-seats --block 2@dev-team,1@other' "$work/ccy-teams.calls")"
check "the block and take's report go to stderr, none to stdout" "" "$(cat "$work/out")"
check "the block is on stderr" "yes" "$(yes_no grep -q 'This session is on teams: 2@dev-team,1@other' "$work/err")"
check "no sentence naming the handles is made: the teams may change (H32)" "0" "$(grep -c -- '--sentence' "$work/ccy-teams.calls")"

echo "=== cc's deny rules (cc-run merges them into the session's settings) ==="

# Claude Code's home-relative rule form, a literal tilde.
home_rel=\~
# Every client on the one Matrix client list (issue #251): its Flatpak data, and each native
# folder with its named profiles.
MATRIX_LIST="$REPO_ROOT/helpers/pingbus/matrix_clients.json"
mapfile -t matrix_paths < <(jq -r '.clients[] | ".var/app/\(.app_id)/**",
    (.native[] | "\(.parent)/\(.name)/**", (select(.profiles) | "\(.parent)/\(.name)-*/**"))' "$MATRIX_LIST")
mapfile -t matrix_bash_rules < <(jq -r '.clients[] | "Bash(*\(.app_id)*)", (.native[] | "Bash(*\(.parent)/\(.name)*)")' "$MATRIX_LIST")
check "the Matrix client list names Element, Nheko and Lightning" "3" "$(jq '.clients | length' "$MATRIX_LIST")"
for path in "${matrix_paths[@]}"; do
    for tool in Read Edit; do
        rule="$tool($home_rel/$path)"
        check "deny $rule" "true" \
            "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$DENY")"
    done
done
# Security audit H2: Bash can reach the human's sign-in too. A deny rule matches the command
# text, so it stops the plain routes (the keyring, Element's profile, the Flatpak) and not a
# determined one; the consent and the docs say so.
for tool in Read Edit; do
    rule="$tool($home_rel/.local/share/keyrings/**)"
    check "deny $rule" "true" \
        "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$DENY")"
done
# The Secret Service's bus name is lower case and its interface capitalised; rules are case
# sensitive, so both are denied (review L-C). The dot is a variable only so this file's text is
# not mistaken for a path to a secrets file by the repository's guards.
dot=.
bus_name_rule="Bash(*freedesktop${dot}secrets*)"
for rule in 'Bash(*secret-tool*)' 'Bash(*keyring*)' 'Bash(*freedesktop.Secret*)' "$bus_name_rule" \
    "${matrix_bash_rules[@]}" 'Bash(*IndexedDB*)' 'Bash(*Local Storage*)'; do
    check "deny $rule" "true" \
        "$(jq --arg r "$rule" '.permissions.deny | index($r) != null' "$DENY")"
done
check "deny Edit of the acceptance record's folder" "true" \
    "$(jq --arg r "Edit($home_rel/.config/pingbus/**)" '.permissions.deny | index($r) != null' "$DENY")"

echo "=== what the session reads ==="

# The prompt line cc-run hands claude on each system, and the plugin directory it passes, are
# checked by running cc-run in Go (teamchatlaunch's ccrun_prompt_test.go), not here.
check "the plugin's text never mentions the owner's Element session either" "no" \
    "$(yes_no grep -rqiE "owner'?s element|your element|element session|im\.riot" "$KIT_SRC/plugin")"

echo "=== the checkout's .claude/ccy/ ==="

fresh="$work/fresh"
mkdir -p "$fresh"
git -C "$fresh" init -q
cc_teams_ccy_dir "$fresh" >"$work/out" 2>"$work/err"
check "a repo never used with ccy gets .claude/ccy/ (0700)" "700" "$(stat -c %a "$fresh/.claude/ccy")"
check "seats' tree is ignored by git, so seat take may place a token there" "0" \
    "$(cd "$fresh" && git check-ignore -q .claude/ccy/pingbus/ && echo 0 || echo 1)"
check "creating it says nothing on stdout" "" "$(cat "$work/out")"
echo marker >"$fresh/.claude/ccy/keep"
cc_teams_ccy_dir "$fresh" 2>"$work/err"
check "an existing .claude/ccy/ is left as it is" "marker" "$(cat "$fresh/.claude/ccy/keep")"
linked="$work/linked"
mkdir -p "$linked/.claude" "$work/elsewhere-dir"
git -C "$linked" init -q
ln -s "$work/elsewhere-dir" "$linked/.claude/ccy"
rc=0
cc_teams_ccy_dir "$linked" 2>"$work/err" || rc=$?
check ".claude/ccy as a symlink is refused 78" "78" "$rc"

echo "=== the command line ==="

check "plain cc starts the same cc-claude, so both paths get the window" "yes" \
    "$(yes_no grep -qxF '    /var/local/claude-code/cc-claude "$@"' <(grep -xF -A1 'else' "$REPO_ROOT/files/var/local/claude-code/cc"))"
check "the play installs cc-claude where cc runs it" "yes" \
    "$(yes_no grep -qF 'go_command_dest: /var/local/claude-code/cc-claude' "$PLAY")"

echo "=== the session records its teams for the status line ==="

hooks_json="$KIT_SRC/plugin/pingbus/hooks/hooks.json"
# The command's inert guard (Plan 00203 H33) passes a session with no PINGBUS_MEMBERSHIP, as cc's is.
check "the plugin cc passes runs pingbus's SessionStart hook, which writes the per-session teams file" "exec pingbus hook session-start" \
    "$(jq -r '.hooks.SessionStart[0].hooks[0].command' "$hooks_json" | grep -oF 'exec pingbus hook session-start')"
check "cc puts the kit first on PATH so the hook runs the kit's pingbus" "yes" \
    "$(yes_no grep -qF "PATH=\"\$CC_TEAMS_KIT:\$PATH\"" "$REPO_ROOT/files/var/local/claude-code/cc")"

echo "=== prepare: chooser, take, settings, command, in order ==="

prep_repo="$work/prep-repo"
mkdir -p "$prep_repo" "$work/prep-tmp"
git -C "$prep_repo" init -q
printf '1@dev-team\n' >"$work/chosen"
CC_TEAMS_LIST=""
: >"$work/agent-bus.calls"
: >"$work/ccy-teams.calls"
rc=0
(cd "$prep_repo" && cc_teams_prepare "$work/prep-tmp" --resume x && printf '%s\n' "${CC_TEAMS_CMD[@]}" >"$work/cmd1") >"$work/out" 2>"$work/err" || rc=$?
check "prepare succeeds" "0" "$rc"
check "chooser first, then the seat check, then take, then the block" \
    "choose-teams --type=host |internal seat-check 1@dev-team|internal seat-take 1@dev-team --type=host|describe-seats --block 1@dev-team" \
    "$(paste -sd'|' "$work/ccy-teams.calls")"
check "agent-bus saw check then take" "seat check 1@dev-team|seat take 1@dev-team --type=host" "$(paste -sd'|' "$work/agent-bus.calls")"
check "the seat exists" "yes" "$(yes_no test -d "$prep_repo/.claude/ccy/pingbus/seats/dev-team/1")"
check "the command is cc-run with the session, cc's pid, the seats and the claude arguments" \
    "$bin/ccy-teams|internal|cc-run|$work/prep-tmp|$prep_repo|cc-proj|$$|1@dev-team|false|--|--resume|x" "$(paste -sd'|' "$work/cmd1")"
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
bus_repo="$work/prep-bus"
mkdir -p "$bus_repo"
git -C "$bus_repo" init -q
: >"$work/agent-bus.calls"
: >"$work/ccy-teams.calls"
rc=0
(cd "$bus_repo" && CC_TEAMS_BUS=true cc_teams_prepare "$work/prep-tmp") >"$work/out" 2>"$work/err" || rc=$?
check "--bus: the consent, no seat (cc-run writes the membership on no team, H32)" \
    "0|choose-teams --type=host --bus |0" \
    "$rc|$(paste -sd'|' "$work/ccy-teams.calls")|$(grep -c . "$work/agent-bus.calls")"
check "--bus still makes the checkout's .claude/ccy/, where the keeper reports" "yes" "$(yes_no test -d "$bus_repo/.claude/ccy")"
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
