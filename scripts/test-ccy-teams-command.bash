#!/usr/bin/env bash
# Unit-test the ccy-teams command (files/home/.local/bin/ccy-teams.j2), the manager of the
# agent teams on a machine: its table (--list), every picker key, and its create, backup and
# restore wrappers. (scripts/test-ccy-teams.bash is ccy's own --teams flag.)
#
# WHY THIS EXISTS. ccy-teams holds no team logic: every row is something agent-bus,
# systemd, tmux or the session registry said, and every key hands over to one of them. What
# must hold is that the rows say what those sources said (a seat with the session holding
# it and its restore mark, the members that are not seats, the humans, a homeserver that
# is down), that each key reaches the right command with the right row's values, that a
# yes/no answered no changes nothing, and that the template renders to a script with no
# Jinja left in it. None of the real commands can run here, so fakes for tmux, fzf,
# systemctl, sudo, agent-bus, agent-bus-install, gio and the ccy launcher keep their state
# in files and log every call; the declare helper, git and the registry library are real.
# The picker runs under `script`, because it demands a terminal.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEMPLATE="$REPO_ROOT/files/home/.local/bin/ccy-teams.j2"
LIB_DIR="$REPO_ROOT/files/var/local/claude-yolo/lib"

for tool in script shellcheck git python3 timeout; do
    if [ -z "$(command -v "$tool")" ]; then
        echo "FAIL: $tool is needed by this test" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}
check_has() {
    if [[ "$3" == *"$2"* ]]; then
        check "$1" yes yes
    else
        check "$1" "contains: $2" "$3"
    fi
}
check_lacks() {
    if [[ "$3" != *"$2"* ]]; then
        check "$1" yes yes
    else
        check "$1" "lacks: $2" "$3"
    fi
}

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/ccy-teams-command-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# ── the template renders to a script ─────────────────────────────────────────────────────
echo "== the template"
TOOL="$SCRATCH/ccy-teams"
python3 - "$TEMPLATE" "$REPO_ROOT" "$TOOL" <<'PYEOF'
import sys

template, root, dest = sys.argv[1:4]
with open(template, encoding="utf-8") as handle:
    text = handle.read()
with open(dest, "w", encoding="utf-8") as handle:
    handle.write(text.replace("{{ root_dir }}", root))
PYEOF
chmod 0755 "$TOOL"
# Ansible renders the whole file with Jinja: a bash ${#array[@]} opens a Jinja comment and
# breaks the deploy, so no Jinja syntax may appear but the one placeholder.
check "the template holds no Jinja syntax but {{ root_dir }}" "0" \
    "$(grep -v -F '{{ root_dir }}' "$TEMPLATE" | grep -cE '\{\{|\{%|\{#|\}\}|%\}|#\}')"
check "the rendered script passes shellcheck" "" \
    "$(shellcheck -S warning -s bash "$TOOL" 2>&1)"

# ── the fakes ────────────────────────────────────────────────────────────────────────────
BIN="$SCRATCH/bin"
STATE="$SCRATCH/state"
LOG="$SCRATCH/calls.log"
mkdir -p "$BIN" "$STATE/active" "$STATE/members" "$STATE/devices" "$STATE/seats"

# tmux: sessions are "<name> <attached> <dir>" lines in $STATE/sessions.
cat >"$BIN/tmux" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
[ "$1" = "-L" ] && shift 2
printf 'tmux %s\n' "$*" >>"$TEST_LOG"
case "$1" in
list-sessions) cat "$TEST_STATE/sessions" ;;
list-clients | set-hook | attach-session) ;;
*) echo "fake tmux: unexpected call: $*" >&2; exit 97 ;;
esac
EOF

# fzf: answers from $TEST_ANSWERS, one line per call, "<key>|<choice>": <choice> is text the
# chosen row contains (the first such row), YES, EXIT or ABORT. Every call's rows are kept.
# With --ansi, as the real fzf does, the rows' colour is shown (kept in $TEST_FZF_RAW) and the
# chosen row handed back without it.
cat >"$BIN/fzf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
rows="$(cat -)"
printf '%s\n%s\n' "$*" "$rows" >>"$TEST_FZF_RAW"
if [[ " $* " == *" --ansi "* ]]; then
    rows="$(awk '{ gsub(/\033\[[0-9;]*m/, ""); print }' <<<"$rows")"
fi
printf '%s\n' "$rows" >>"$TEST_FZF_ROWS"
n=$(($(cat "$TEST_STATE/fzf-calls") + 1))
printf '%s\n' "$n" >"$TEST_STATE/fzf-calls"
answer="$(awk -v n="$n" 'NR == n' "$TEST_ANSWERS")"
key="${answer%%|*}"
choice="${answer#*|}"
case "$choice" in
ABORT) exit 130 ;;
EXIT) row="Exit" ;;
YES) row="$(awk '/^Yes, / { print; exit }' <<<"$rows")" ;;
"") echo "fake fzf: no answer queued for call $n" >&2; exit 2 ;;
*) row="$(grep -m1 -F -- "$choice" <<<"$rows")" ;;
esac
if [[ "$*" == *--expect* ]]; then
    printf '%s\n' "$key"
fi
printf '%s\n' "$row"
EOF

# systemctl: the loaded homeserver units are lines in $STATE/units; one is active when
# $STATE/active/<unit> exists.
cat >"$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'systemctl %s\n' "$*" >>"$TEST_LOG"
case "$1" in
list-units) cat "$TEST_STATE/units" ;;
is-active)
    if [ -e "$TEST_STATE/active/$2" ]; then echo active; else echo inactive; exit 3; fi ;;
*) echo "fake systemctl: unexpected call: $*" >&2; exit 97 ;;
esac
EOF

# sudo: a credential is cached while $STATE/sudo-cached exists (reset makes it). Without one,
# `sudo -n` refuses as the real one does, except for the two read-only commands when
# TEST_SUDO_RULE stands for the sudoers rule; `sudo -v` takes the password and caches it,
# unless TEST_SUDO_REFUSE stands for three wrong passwords or a Ctrl-C. Then it runs the
# named command's fake by its basename.
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'sudo %s\n' "$*" >>"$TEST_LOG"
refuse() {
    echo "sudo: $1" >&2
    exit 1
}
cached=no
[ ! -e "$TEST_STATE/sudo-cached" ] || cached=yes
if [ "$1" = "-v" ]; then
    [ -z "${TEST_SUDO_REFUSE:-}" ] || refuse "3 incorrect password attempts"
    touch "$TEST_STATE/sudo-cached"
    exit 0
fi
if [ "$1" = "-n" ]; then
    shift
    if [ "$1" = "-v" ]; then
        [ "$cached" = yes ] || refuse "a password is required"
        exit 0
    fi
    if [ "$cached" = no ]; then
        # The rule: the rendered sudoers template's own command paths and anchored regexes.
        allowed=no
        if [ -n "${TEST_SUDO_RULE:-}" ]; then
            while IFS=$'\t' read -r path regex; do
                if [ "$1" = "$path" ] && [[ "${*:2}" =~ $regex ]]; then allowed=yes; fi
            done <"$TEST_STATE/sudo-rules"
        fi
        [ "$allowed" = yes ] || refuse "a password is required"
    fi
elif [ "$cached" = no ] && [ -n "${TEST_SUDO_REFUSE:-}" ]; then
    refuse "3 incorrect password attempts"
fi
cmd="$(basename "$1")"
shift
exec "$cmd" "$@"
EOF

# agent-bus: list and human devices from files; seat list from $STATE/seats/<checkout name>;
# the rest logged.
cat >"$BIN/agent-bus" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'agent-bus %s (in %s)\n' "$*" "$(basename "$PWD")" >>"$TEST_LOG"
case "$1 ${2:-}" in
"seat list")
    # A seat whose bundle cannot be read: the others print, and the command exits 78.
    cat "$TEST_STATE/seats/$(basename "$PWD")"
    if [ -e "$TEST_STATE/seats/$(basename "$PWD").unreadable" ]; then exit 78; fi ;;
"seat remove") ;;
"list "*) cat "$TEST_STATE/members/$2" ;;
"park-member "*) ;;
"human devices")
    if [ -f "$TEST_STATE/devices/$4" ]; then cat "$TEST_STATE/devices/$4"; fi ;;
"human password") echo "a-new-password" ;;
*) echo "fake agent-bus: unexpected call: $*" >&2; exit 97 ;;
esac
EOF

cat >"$BIN/agent-bus-install" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'agent-bus-install %s\n' "$*" >>"$TEST_LOG"
case "$1" in
backups) cat "$TEST_STATE/backups" ;;
backup-now | restore) ;;
*) echo "fake agent-bus-install: unexpected call: $*" >&2; exit 97 ;;
esac
EOF

# ss: one listener, on the first port create would otherwise choose after alpha's.
cat >"$BIN/ss" <<'EOF'
#!/usr/bin/env bash
printf 'LISTEN 0 128 127.0.0.1:6168 0.0.0.0:*\n'
EOF

# gio and the ccy launcher: each logs its arguments.
cat >"$BIN/gio" <<'EOF'
#!/usr/bin/env bash
printf 'gio %s\n' "$*" >>"$TEST_LOG"
EOF
cat >"$BIN/wl-copy" <<'EOF'
#!/usr/bin/env bash
printf 'wl-copy %s\n' "$*" >>"$TEST_LOG"
if [ -n "${TEST_WLCOPY_FAIL:-}" ]; then
    echo "wl-copy: failed to connect to a Wayland server" >&2
    exit 1
fi
cat >"$TEST_STATE/clipboard"
EOF
cat >"$BIN/launcher" <<'EOF'
#!/usr/bin/env bash
printf 'launcher %s (in %s)\n' "$*" "$(basename "$PWD")" >>"$TEST_LOG"
EOF
# id: the desktop login is "owner".
cat >"$BIN/id" <<'EOF'
#!/usr/bin/env bash
[ "$*" = "-un" ] && { echo owner; exit 0; }
exec /usr/bin/id "$@"
EOF

# gh: logged in unless TEST_GH_NOAUTH; example-org is the one owner GitHub has; its
# repositories are $STATE/gh-repos.json.
cat >"$BIN/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >>"$TEST_LOG"
case "$1 ${2:-}" in
"auth status")
    if [ -n "${TEST_GH_NOAUTH:-}" ]; then echo "You are not logged into any GitHub hosts." >&2; exit 1; fi ;;
"api users/example-org") ;;
"api "*) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
"repo list") cat "$TEST_STATE/gh-repos.json" ;;
*) echo "fake gh: unexpected call: $*" >&2; exit 97 ;;
esac
EOF
chmod 755 "$BIN"/*

# gum, in a directory of its own so a case can take it off PATH. Answers come from
# $TEST_GUM_ANSWERS, one line per input, confirm or filter: ENTER (input: its --value;
# filter: its --selected), =TEXT (input: TEXT), PICK A,B (filter), ALL or NONE (choose: the
# first or second option), YES, NO, ESC (exit 1),
# CTRLC (exit 130). style prints its text; spin runs its command.
GUMBIN="$SCRATCH/gumbin"
mkdir -p "$GUMBIN"
cat >"$GUMBIN/gum" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
sub="$1"
shift
printf 'gum %s %s\n' "$sub" "$*" >>"$TEST_LOG"
env | awk '/^GUM_/' | sort >"$TEST_STATE/gum-env"
next() {
    local n
    n=$(($(cat "$TEST_STATE/gum-calls") + 1))
    printf '%s\n' "$n" >"$TEST_STATE/gum-calls"
    awk -v n="$n" 'NR == n' "$TEST_GUM_ANSWERS"
}
value="" selected=""
texts=()
while [ $# -gt 0 ]; do
    case "$1" in
    --) shift; break ;;
    --value) value="$2"; shift ;;
    --selected) selected="$2"; shift ;;
    --header | --placeholder | --title | --border | --padding | --width) shift ;;
    --*) ;;
    *) texts+=("$1") ;;
    esac
    shift
done
case "$sub" in
style) printf '%s\n' "${texts[@]}" ;;
spin) exec "$@" ;;
filter) cat >"$TEST_STATE/filter-items" ;;
esac
case "$sub" in
input | confirm | filter | choose)
    answer="$(next)"
    case "$answer" in
    ENTER)
        if [ "$sub" != filter ]; then
            printf '%s\n' "$value"
        elif [ "$selected" = "*" ]; then
            cat "$TEST_STATE/filter-items"
        else
            printf '%s\n' "$selected"
        fi ;;
    =*) printf '%s\n' "${answer#=}" ;;
    "PICK "*) tr ',' '\n' <<<"${answer#PICK }" ;;
    ALL) printf '%s\n' "${texts[0]}" ;;
    NONE) printf '%s\n' "${texts[1]}" ;;
    YES) ;;
    NO | ESC) exit 1 ;;
    CTRLC) exit 130 ;;
    *) echo "fake gum: no answer queued for $sub" >&2; exit 2 ;;
    esac ;;
esac
EOF
chmod 755 "$GUMBIN/gum"

# The sudoers rule play-agent-bus.yml installs, as "<command path><TAB><regex>" lines: the
# fake sudo grants exactly these when TEST_SUDO_RULE is set, so the rule and the calls
# ccy-teams makes cannot drift apart unnoticed.
SUDOERS_TEMPLATE="$REPO_ROOT/files/etc/sudoers.d/agent-bus-read-only.j2"
python3 - "$SUDOERS_TEMPLATE" "$STATE/sudo-rules" <<'PYEOF'
import re
import sys

source, dest = sys.argv[1:3]
text = open(source, encoding="utf-8").read().replace("\\\n", " ")
rules = re.findall(r"(/\S+) (\^[^$]*\$)", text.split("NOPASSWD:", 1)[1])
with open(dest, "w", encoding="utf-8") as handle:
    handle.writelines(f"{path}\t{regex}\n" for path, regex in rules)
PYEOF
check "the sudoers template grants exactly two read-only commands" \
    $'/usr/local/bin/agent-bus\t^list [a-z][a-z0-9-]*$\n/usr/local/bin/agent-bus\t^human devices [a-z][a-z0-9-]* [a-z][a-z0-9_-]*$' \
    "$(cat "$STATE/sudo-rules")"

# ── the machine: a host_vars file, one checkout with seats and a session ─────────────────
HOME_DIR="$SCRATCH/home"
mkdir -p "$HOME_DIR"
HOST_VARS="$SCRATCH/localhost.yml"
cat >"$HOST_VARS" <<'EOF'
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: alpha
    port: 6167
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    repos:
      - repo: example-org/proj
        branches: ["main"]
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
    forge_api: https://api.github.com
  - team: gone
    state: absent
EOF
# The scratch directory is inside this repository: the ceiling keeps OUTSIDE out of it.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 GIT_CEILING_DIRECTORIES="$SCRATCH"
CHECKOUT="$SCRATCH/proj"
mkdir -p "$CHECKOUT/.claude/ccy/pingbus/seats"
git -C "$CHECKOUT" init -q
git -C "$CHECKOUT" remote add origin git@github.com:Example-Org/proj.git
cat >"$STATE/gh-repos.json" <<'EOF'
[{"name": "proj", "defaultBranchRef": {"name": "main"}, "isArchived": false},
 {"name": "Other", "defaultBranchRef": {"name": "trunk"}, "isArchived": false},
 {"name": "old", "defaultBranchRef": {"name": "main"}, "isArchived": true},
 {"name": "fresh", "defaultBranchRef": null, "isArchived": false}]
EOF
OUTSIDE="$SCRATCH/not-a-project"
mkdir -p "$OUTSIDE"

export TEST_LOG="$LOG" TEST_STATE="$STATE" TEST_ANSWERS="$SCRATCH/answers" TEST_FZF_ROWS="$SCRATCH/fzf-rows"
export TEST_FZF_RAW="$SCRATCH/fzf-raw"
unset NO_COLOR WAYLAND_DISPLAY
export TEST_GUM_ANSWERS="$SCRATCH/gum-answers"
PATH_WITHOUT_GUM="$BIN:$PATH"
export PATH="$BIN:$GUMBIN:$PATH" HOME="$HOME_DIR" CCY_STATE_DIR="$SCRATCH/ccy-state" CCY_LIB="$LIB_DIR"
export CCY_TEAMS_HOST_VARS="$HOST_VARS" CCY_TEAMS_LAUNCHER="$BIN/launcher"

# shellcheck source=/dev/null
source "$LIB_DIR/common-pure.bash"
# shellcheck source=/dev/null
source "$LIB_DIR/session-registry.bash"

# reset — the machine as each case starts: alpha up, with seat 1 held by ccy-proj and seat
# dev free in the checkout, a parked ccy member whose checkout is not known here, an LXC
# member and a human; "extra", a homeserver loaded but not declared, down.
reset() {
    : >"$LOG"
    : >"$TEST_FZF_ROWS"
    : >"$TEST_FZF_RAW"
    printf '0\n' >"$STATE/fzf-calls"
    touch "$STATE/sudo-cached"
    rm -f "$STATE/clipboard"
    printf 'agent-bus-hs@alpha.service loaded active running x\nagent-bus-hs@extra.service loaded inactive dead x\n' \
        >"$STATE/units"
    rm -f "$STATE/active/"* "$STATE/devices/"* "$STATE/seats/"*.unreadable
    touch "$STATE/active/agent-bus-hs@alpha.service"
    printf 'HUMAN\towner\tjoin\nMEMBER\tproj.1+local.podman\tworker\tjoin\tactive\nMEMBER\tproj.2+local.podman\tworker\tjoin\tparked\nMEMBER\tproj.box+role.lxc\tworker\tjoin\tactive\n' \
        >"$STATE/members/alpha"
    printf 'SEAT\talpha\t1\theld\t-\tproj.1+local.podman\nSEAT\talpha\tdev\tfree\t-\tproj.dev+local.podman\n' \
        >"$STATE/seats/proj"
    printf 'ccy-proj 0 %s\n' "$CHECKOUT" >"$STATE/sessions"
    printf 'BACKUP\t1\t2026-10-10 03:00 UTC\nBACKUP\t2\t2026-10-11 03:00 UTC\n' >"$STATE/backups"
    rm -rf "$CCY_STATE_DIR" "$HOME_DIR/.local"
    ccy_registry_write ccy-proj "$CHECKOUT" /var/local/claude-yolo/claude-yolo ccy yes --teams 1@alpha
}
rc_of() { awk '/^rc=/ { print }' <<<"$1"; }
logged() { if grep -qF -- "$1" "$LOG"; then echo yes; else echo no; fi; }
# run_picker <dir> <answers...> — the real ccy-teams picker in <dir>, under a terminal.
run_picker() {
    local dir="$1"
    shift
    printf '%s\n' "$@" >"$TEST_ANSWERS"
    printf '0\n' >"$STATE/fzf-calls"
    (cd "$dir" && timeout 30 script -qec "$(printf '%q' "$TOOL")" /dev/null </dev/null 2>&1; echo "rc=$?") |
        tr -d '\r'
}
# run_on_terminal <words...> — ccy-teams with those arguments, under a terminal.
run_on_terminal() {
    printf '0\n' >"$STATE/fzf-calls"
    (cd "$OUTSIDE" && timeout 30 script -qec "$(printf '%q ' "$TOOL" "$@")" /dev/null </dev/null 2>&1) |
        tr -d '\r'
}
LAUNCHER_FILE="$HOME_DIR/.local/share/applications/agent-bus-alpha-element.desktop"
element_profile() {
    mkdir -p "$HOME_DIR/.local/share/applications"
    : >"$LAUNCHER_FILE"
}

# ── usage ────────────────────────────────────────────────────────────────────────────────
echo "== usage"
out="$("$TOOL" --help)"
check "--help exits 0" "0" "$?"
check_has "--help prints the usage" "ccy-teams -l|--list" "$out"
check_lacks "  and stops at the code" "set -euo" "$out"
"$TOOL" --frob 2>"$SCRATCH/err"
check "an unknown option is a usage error" "64" "$?"
"$TOOL" --list extra 2>"$SCRATCH/err"
check "--list takes nothing more" "64" "$?"

# ── the table ────────────────────────────────────────────────────────────────────────────
echo "== --list"
reset
out="$(cd "$OUTSIDE" && "$TOOL" --list 2>"$SCRATCH/err")"
check "it exits 0" "0" "$?"
heading="$(awk 'NR == 1' <<<"$out")"
check_has "a heading first" "TEAM" "$heading"
check_has "  naming every column" "RESTORE" "$heading"
alpha="$(grep -E '^alpha +- +team' <<<"$out")"
check_has "alpha's homeserver is up" " up " "$alpha"
check_has "  on its declared port" "port 6167" "$alpha"
seat="$(grep -E '^alpha +1 +ccy' <<<"$out")"
check_has "seat 1 is an active ccy seat" "active" "$seat"
check_has "  held by ccy-proj" "ccy-proj" "$seat"
check_has "  which is restored after a reboot" " yes " "$seat"
check_has "  in its checkout" "$CHECKOUT" "$seat"
check_has "a free seat says free" "free" "$(grep -E '^alpha +dev +ccy' <<<"$out")"
check_has "a ccy member whose checkout is not known is listed by handle, parked" "parked" \
    "$(grep -E '^alpha +proj\.2\+local\.podman +ccy' <<<"$out")"
check "  and seat 1's handle is not listed twice" "0" "$(grep -c 'proj\.1+local\.podman' <<<"$out")"
check_has "the LXC member" "active" "$(grep -E '^alpha +proj\.box\+role\.lxc +lxc' <<<"$out")"
human="$(grep -E '^alpha +owner +human' <<<"$out")"
check_has "the person, in the room in words" "in room" "$human"
check_has "  never signed in to Element (no device)" "never signed in" "$human"
check_has "  and the key that gives them a password" "Enter: new password" "$human"
extra="$(grep -E '^extra +- +team' <<<"$out")"
check_has "a loaded homeserver not declared is listed, down" "down (inactive)" "$extra"
check_has "  with no port, and why there are no members" "port unknown (not declared), members: homeserver down" "$extra"
check "a team declared absent is not listed" "0" "$(grep -c '^gone ' <<<"$out")"
check "the members are read with sudo -n only" "yes" \
    "$(logged 'sudo -n /usr/local/bin/agent-bus list alpha')"
check "  and never for a homeserver that is down" "no" "$(logged 'agent-bus list extra')"
check "  --list never asks for a password" "no" "$(logged 'sudo -v')"
check "piped, --list is plain text" "0" "$(grep -c $'\e' <<<"$out")"

reset
printf 'DEVICE\tABC\t1\nDEVICE\tDEF\t2\n' >"$STATE/devices/owner"
out="$(cd "$OUTSIDE" && "$TOOL" --list 2>"$SCRATCH/err")"
check_has "a person signed in on two devices says so" "2 signed in" "$(grep -E '^alpha +owner +human' <<<"$out")"

reset
printf 'agent-bus-hs@extra.service loaded inactive dead x\n' >"$STATE/units"
out="$(cd "$OUTSIDE" && "$TOOL" --list 2>"$SCRATCH/err")"
alpha="$(grep -E '^alpha +- +team' <<<"$out")"
check_has "a team declared but with no homeserver unit is not installed" "not installed" "$alpha"
check_has "  and the row says what installs it" "play-agent-bus.yml" "$alpha"
check "  no members are asked for" "no" "$(logged 'agent-bus list alpha')"

reset
out="$(cd "$OUTSIDE" && script -qec "$(printf '%q ' "$TOOL" --list)" /dev/null </dev/null 2>&1 | tr -d '\r')"
check_has "on a terminal, --list is in colour" $'\e[32m' "$out"
out="$(cd "$OUTSIDE" && NO_COLOR=1 script -qec "$(printf '%q ' "$TOOL" --list)" /dev/null </dev/null 2>&1 | tr -d '\r')"
check "  but not with NO_COLOR" "0" "$(grep -c $'\e' <<<"$out")"

reset
rm -f "$STATE/sudo-cached"
out="$(cd "$OUTSIDE" && "$TOOL" --list 2>"$SCRATCH/err")"
check "--list with no sudo credential prints the table, but exits 1: part of it is unread" "1" "$?"
check_has "  says why the members are not shown" "members of alpha are not shown" "$(cat "$SCRATCH/err")"
check_has "  and the two ways to show them" "play-agent-bus.yml" "$(cat "$SCRATCH/err")"
check_has "  the seat's state is need sudo" "need sudo" "$(grep -E '^alpha +1 +ccy' <<<"$out")"
check_has "  and so is the team row's" "members: need sudo" "$(grep -E '^alpha +- +team' <<<"$out")"

reset
rm -f "$STATE/sudo-cached"
out="$(cd "$OUTSIDE" && TEST_SUDO_RULE=1 "$TOOL" --list 2>"$SCRATCH/err")"
check_has "with the sudoers rule and no credential, the members are read" "active" \
    "$(grep -E '^alpha +1 +ccy' <<<"$out")"
check_lacks "  with nothing said about sudo" "sudo" "$(cat "$SCRATCH/err")"

reset
printf 'SEAT\talpha\t3\tfree\t-\tproj.3+local.podman\n' >"$STATE/seats/proj"
: >"$STATE/sessions"
rm -rf "$CCY_STATE_DIR"
out="$(cd "$CHECKOUT" && "$TOOL" --list 2>"$SCRATCH/err")"
check_has "the current checkout's seats are listed with no session anywhere" "free" \
    "$(grep -E '^alpha +3 +ccy' <<<"$out")"

reset
: >"$STATE/units"
printf 'agent_bus_address: 192.0.2.10\n' >"$SCRATCH/empty.yml"
out="$(cd "$OUTSIDE" && CCY_TEAMS_HOST_VARS="$SCRATCH/empty.yml" "$TOOL" --list 2>"$SCRATCH/err")"
check "no teams: exit 0" "0" "$?"
check_has "  and says how to declare one" "ccy-teams create" "$(cat "$SCRATCH/err")"

printf 'agent_bus_teams: []\n' >"$SCRATCH/bad.yml"
(cd "$OUTSIDE" && CCY_TEAMS_HOST_VARS="$SCRATCH/bad.yml" "$TOOL" --list >"$SCRATCH/out" 2>"$SCRATCH/err")
check "a host_vars shape the helper refuses is a failure" "1" "$?"

reset
touch "$STATE/seats/proj.unreadable"
out="$(cd "$OUTSIDE" && "$TOOL" --list 2>"$SCRATCH/err")"
check "a seat that cannot be read: the checkout's other seats are still listed" "1" \
    "$(grep -cE '^alpha +1 +ccy' <<<"$out")"
check_has "  and every team row says something is unreadable" "a seat is unreadable" \
    "$(grep -E '^alpha +- +team' <<<"$out")"
out="$(run_picker "$OUTSIDE" "ctrl-x|proj.2+local.podman" "|ABORT")"
check "  so Ctrl-X parks no ccy member whose checkout is unknown" "no" "$(logged 'park-member')"
check_has "  saying why" "could not be read" "$out"
out="$(run_picker "$CHECKOUT" "ctrl-n|port 6167" "|ABORT")"
check "  and Ctrl-N in that checkout offers no seat" "no" "$(logged 'launcher')"
check_has "  saying why" "Not every seat" "$out"

# ── the picker ───────────────────────────────────────────────────────────────────────────
echo "== the picker"
reset
"$TOOL" </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "it needs a terminal" "1" "$?"
check_has "  and points at --list" "--list" "$(cat "$SCRATCH/err")"

reset
out="$(run_picker "$OUTSIDE" "|ABORT")"
check "Esc leaves, exit 0" "rc=0" "$(rc_of "$out")"
check "with a cached credential it asks no password" "no" "$(logged 'sudo -v')"
check_has "the rows carry colour, through fzf --ansi" $'\e[32m' "$(cat "$TEST_FZF_RAW")"
reset
out="$(NO_COLOR=1 run_picker "$OUTSIDE" "|ABORT")"
check "  but not with NO_COLOR" "0" "$(grep -c $'\e' "$TEST_FZF_RAW")"

echo "== sudo"
reset
rm -f "$STATE/sudo-cached"
out="$(run_picker "$OUTSIDE" "|ABORT")"
check "no credential: sudo asks, once the reason is given" "yes" "$(logged 'sudo -v')"
check_has "  the reason comes first" "done as root" "$out"
check_has "  with what installs the rule that removes the need" "play-agent-bus.yml" "$out"
check "  and with the password, the picker shows the members" "rc=0" "$(rc_of "$out")"
check_has "    seat 1 active" "active" "$(grep -E '^alpha +1 +ccy' "$TEST_FZF_ROWS")"

reset
rm -f "$STATE/sudo-cached"
out="$(TEST_SUDO_REFUSE=1 run_picker "$OUTSIDE" "|ABORT")"
check "sudo refused (three wrong passwords, or Ctrl-C): exit non-zero" "rc=1" "$(rc_of "$out")"
check_has "  saying sudo was not given the password" "sudo was not given your password" "$out"
check_has "  and what that costs" "members cannot be shown" "$out"
check_lacks "  never 'no teams'" "No agent teams" "$out"
check "  and no picker is shown" "0" "$(cat "$STATE/fzf-calls")"

reset
rm -f "$STATE/sudo-cached"
out="$(TEST_SUDO_RULE=1 run_picker "$OUTSIDE" "|ABORT")"
check "the sudoers rule installed: no password is asked" "no" "$(logged 'sudo -v')"
check_has "  and the members are shown" "active" "$(grep -E '^alpha +1 +ccy' "$TEST_FZF_ROWS")"
check_has "  and whether the person is signed in, through the rule's second command" "never signed in" \
    "$(grep -E '^alpha +owner +human' "$TEST_FZF_ROWS")"
check "  which ccy-teams called as the rule names it" "yes" \
    "$(logged 'sudo -n /usr/local/bin/agent-bus human devices alpha owner')"

reset
out="$(run_picker "$OUTSIDE" "|ccy-proj")"
check "Enter on a seat attaches its session" "yes" "$(logged 'tmux attach-session -t =ccy-proj')"
check "  and exits 0" "rc=0" "$(rc_of "$out")"

reset
printf 'ccy-proj 1 %s\n' "$CHECKOUT" >"$STATE/sessions"
out="$(run_picker "$OUTSIDE" "|ccy-proj" "|ABORT")"
check "a session open elsewhere is not attached" "no" "$(logged 'attach-session')"
check_has "  ccy-sessions is named for taking it over" "Ctrl-T" "$out"

reset
out="$(run_picker "$OUTSIDE" "ctrl-r|ccy-proj" "|ABORT")"
ccy_registry_read "$CCY_STATE_DIR/sessions/ccy-proj"
check "Ctrl-R turns the seat's session's restore off" "no" "$REC_RESTORE"
check "  keeping its seats" "--teams 1@alpha" "${REC_ARGS[*]}"
check_has "  and the refreshed list shows it" " no " \
    "$(grep -E '^alpha +1 +ccy' "$TEST_FZF_ROWS" | awk 'NR == 2')"
out="$(run_picker "$OUTSIDE" "ctrl-r|ccy-proj" "|ABORT")"
ccy_registry_read "$CCY_STATE_DIR/sessions/ccy-proj"
check "  and on again" "yes" "$REC_RESTORE"

reset
out="$(run_picker "$OUTSIDE" "ctrl-r|proj.box+role.lxc" "|ABORT")"
check_has "Ctrl-R on a row with no session says so" "has no session" "$out"

reset
out="$(run_picker "$OUTSIDE" "ctrl-x|ccy-proj" "|YES" "|ABORT")"
check "Ctrl-X then yes removes the seat, in its checkout" "yes" "$(logged 'agent-bus seat remove 1@alpha (in proj)')"
reset
out="$(run_picker "$OUTSIDE" "ctrl-x|ccy-proj" "|EXIT" "|ABORT")"
check "Ctrl-X then no removes nothing" "no" "$(logged 'seat remove')"
reset
out="$(run_picker "$OUTSIDE" "ctrl-x|proj.box+role.lxc" "|YES" "|ABORT")"
check "Ctrl-X then yes on an LXC member parks it" "yes" \
    "$(logged 'sudo -n /usr/local/bin/agent-bus park-member alpha proj.box+role.lxc')"
reset
out="$(run_picker "$OUTSIDE" "ctrl-x|proj.2+local.podman" "|YES" "|ABORT")"
check "  as it does a ccy member whose checkout is not known" "yes" \
    "$(logged 'agent-bus park-member alpha proj.2+local.podman')"
reset
out="$(run_picker "$OUTSIDE" "ctrl-x|port 6167" "|ABORT")"
check_has "Ctrl-X on a team says how a team is removed" "state: absent" "$out"
check "  and asks nothing" "2" "$(cat "$STATE/fzf-calls")"

reset
out="$(run_picker "$OUTSIDE" "ctrl-n|port 6167" "|ABORT")"
check_has "Ctrl-N outside a project says so" "not a project folder" "$out"
check "  and starts nothing" "no" "$(logged 'launcher')"
reset
out="$(run_picker "$CHECKOUT" "ctrl-n|port 6167" "|new")"
check "Ctrl-N offers the free seat, and a new one numbered after every numbered seat" "yes" \
    "$(grep -q 'Seat dev@alpha   free' "$TEST_FZF_ROWS" && grep -q 'Seat 3@alpha   new' "$TEST_FZF_ROWS" && echo yes || echo no)"
check "  and starts ccy --teams in the new one" "yes" "$(logged 'launcher --teams 3@alpha')"
reset
out="$(run_picker "$CHECKOUT" "ctrl-n|owner" "|free")"
check "  or in the free one, from any row of the team" "yes" "$(logged 'launcher --teams dev@alpha')"
reset
out="$(run_picker "$CHECKOUT" "ctrl-n|extra" "|ABORT")"
check_has "Ctrl-N on a team whose homeserver is down says so" "homeserver is down" "$out"
reset
mkdir -p "$CHECKOUT/sub/dir"
out="$(run_picker "$CHECKOUT/sub/dir" "ctrl-n|port 6167" "|new")"
check "Ctrl-N from a subdirectory starts ccy at the checkout's top, where its seats are" "yes" \
    "$(logged 'launcher --teams 3@alpha (in proj)')"

reset
out="$(run_picker "$OUTSIDE" "ctrl-e|owner" "|ABORT")"
check_has "Ctrl-E with no Element profile names the play" "play-agent-bus-element.yml" "$out"
check "  and opens nothing" "no" "$(logged 'gio')"
reset
element_profile
printf 'DEVICE\tABC\t1\n' >"$STATE/devices/owner"
out="$(run_picker "$OUTSIDE" "ctrl-e|owner")"
check "Ctrl-E opens the team's Element launcher" "yes" "$(logged "gio launch $LAUNCHER_FILE")"
check "  asking no password of a human who has a session" "no" "$(logged 'human password')"
check "  and exits 0" "rc=0" "$(rc_of "$out")"
reset
element_profile
out="$(run_picker "$OUTSIDE" "ctrl-e|ccy-proj" "|EXIT")"
check "a human with no session is offered a password; no sets none" "no" "$(logged 'human password')"
check "  and Element still opens" "yes" "$(logged 'gio launch')"
reset
element_profile
out="$(run_picker "$OUTSIDE" "ctrl-e|ccy-proj" "|YES")"
check "  yes sets one, through sudo agent-bus" "yes" "$(logged 'sudo -n /usr/local/bin/agent-bus human password alpha owner')"
check_has "  printed once, here" "a-new-password" "$out"

echo "== a person's password"
reset
out="$(run_picker "$OUTSIDE" "|owner" "|YES" "|ABORT")"
check "Enter on a person sets a new Element password, after a yes" "yes" \
    "$(logged 'agent-bus human password alpha owner')"
check_has "  the yes says their other sessions are signed out" "other Element sessions are signed out" \
    "$(cat "$TEST_FZF_RAW")"
check_has "  the login shows the homeserver" "http://192.0.2.10:6167" "$out"
check_has "  the username" "Username" "$out"
check_has "  the Matrix ID" "@owner:alpha.agent-bus.internal" "$out"
check_has "  and the password" "a-new-password" "$out"
check "  then back to the list" "rc=0" "$(rc_of "$out")"
reset
out="$(run_picker "$OUTSIDE" "|owner" "|EXIT" "|ABORT")"
check "  no sets none" "no" "$(logged 'human password')"
reset
out="$(WAYLAND_DISPLAY=wayland-test run_picker "$OUTSIDE" "ctrl-p|owner" "|YES" "|YES" "|ABORT")"
check "Ctrl-P does the same, and offers the clipboard where wl-copy can reach it" "a-new-password" \
    "$(cat "$STATE/clipboard" 2>&1)"
check_lacks "  the password never reaches wl-copy's arguments" "a-new-password" "$(grep -F 'wl-copy' "$LOG")"
reset
out="$(TEST_WLCOPY_FAIL=1 WAYLAND_DISPLAY=wayland-test run_picker "$OUTSIDE" "ctrl-p|owner" "|YES" "|YES" "|ABORT")"
check_has "a failed wl-copy says the password is not on the clipboard" "not on the clipboard" "$out"
check_lacks "  and never that it is" "The password is on the clipboard." "$out"
reset
rm -f "$STATE/sudo-cached"
out="$(TEST_SUDO_RULE=1 TEST_SUDO_REFUSE=1 run_picker "$OUTSIDE" "|owner" "|YES" "|ABORT")"
check "a password needs sudo; refused, none is set" "no" "$(logged 'agent-bus human password')"
check_has "  saying so" "No password was set for owner." "$out"
reset
out="$(run_picker "$OUTSIDE" "ctrl-p|ccy-proj" "|ABORT")"
check_has "Ctrl-P on another row says where the people are" "KIND human" "$out"

echo "== help and repositories"
reset
out="$(run_picker "$OUTSIDE" "?|port 6167" "|ABORT")"
check_has "? shows every key, explained" "Ctrl-A" "$out"
check_has "  and the status words" "need sudo" "$out"
reset
out="$(run_picker "$OUTSIDE" "|port 6167" "|ABORT")"
check_has "Enter on a team shows the same help" "Colours:" "$out"

reset
cp "$HOST_VARS" "$SCRATCH/repo.yml"
printf '%s\n' ENTER ALL 'PICK example-org/other' YES >"$TEST_GUM_ANSWERS"
printf '0\n' >"$STATE/gum-calls"
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/repo.yml" run_picker "$OUTSIDE" "ctrl-a|port 6167" "|ABORT")"
check_has "Ctrl-A adds a repository to the row's team" 'repo: "example-org/other"' "$(cat "$SCRATCH/repo.yml")"
check_has "  with its default branch" 'branches: ["trunk"]' "$(cat "$SCRATCH/repo.yml")"
check_lacks "  not offering one listed already" "example-org/proj" "$(cat "$STATE/filter-items")"
check_has "  and says what installs it" "meta-deploy" "$out"
reset
cp "$HOST_VARS" "$SCRATCH/repo.yml"
CCY_TEAMS_HOST_VARS="$SCRATCH/repo.yml" "$TOOL" add-repo alpha --repo example-org/two=main --yes \
    >"$SCRATCH/out" 2>"$SCRATCH/err"
check "add-repo with --repo and --yes asks nothing" "0" "$?"
check_has "  and adds it" 'repo: "example-org/two"' "$(cat "$SCRATCH/repo.yml")"
"$TOOL" add-repo </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "add-repo needs a team" "64" "$?"

# ── backup, restore, create ──────────────────────────────────────────────────────────────
echo "== backup and restore"
reset
"$TOOL" backup alpha 2>"$SCRATCH/err"
check "backup runs backup-now through sudo" "yes" "$(logged 'sudo /usr/local/sbin/agent-bus-install backup-now --team alpha')"
"$TOOL" backup 2>"$SCRATCH/err"
check "  and needs a team" "64" "$?"
"$TOOL" restore alpha 2 --yes 2>"$SCRATCH/err"
check "restore <team> <n> --yes restores that backup" "yes" \
    "$(logged 'sudo /usr/local/sbin/agent-bus-install restore --team alpha --backup 2')"
reset
"$TOOL" restore alpha 2 </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "without a terminal a restore needs --yes" "64" "$?"
check "  and restores nothing" "no" "$(logged 'restore --team')"
out="$("$TOOL" restore alpha </dev/null 2>"$SCRATCH/err")"
check "without a number and a terminal, the backups are listed" "64" "$?"
check_has "  on stdout" $'BACKUP\t2' "$out"
"$TOOL" restore alpha latest 2>"$SCRATCH/err"
check "a backup is a number" "64" "$?"
reset
printf '%s\n' "|Backup 1" "|YES" >"$TEST_ANSWERS"
out="$(run_on_terminal restore alpha)"
check "restore <team> on a terminal picks a backup, then asks" "yes" \
    "$(logged 'agent-bus-install restore --team alpha --backup 1')"
reset
printf '%s\n' "|Backup 2" "|EXIT" >"$TEST_ANSWERS"
out="$(run_on_terminal restore alpha)"
check "  and no restores nothing" "no" "$(logged 'restore --team')"

echo "== create"
reset
cp "$HOST_VARS" "$SCRATCH/create.yml"
"$TOOL" create </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "create with no terminal and no --yes is a usage error" "64" "$?"
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/create.yml" "$TOOL" create --team beta --human owner \
    --repo example-org/proj=main --yes </dev/null 2>"$SCRATCH/err")"
check "create with every value adds the entry" "0" "$?"
check_has "  saying so on stdout, on a port neither declared nor listening" $'CHANGED\tteam beta, port 6169' "$out"
check_has "  and that play-agent-bus.yml must run" "play-agent-bus.yml" "$(cat "$SCRATCH/err")"
check "  in the host_vars" "1" "$(grep -c '^  - team: beta$' "$SCRATCH/create.yml")"
check "  listening on the bus address" "2" "$(grep -c 'listen: \["192.0.2.10"\]' "$SCRATCH/create.yml")"

# run_in <dir> <gum answers (one string, lines)> <words...> — ccy-teams with those words in
# <dir>, under a terminal, its gum answered from the queue; the output ends "rc=<status>".
run_in() {
    local dir="$1"
    printf '%s\n' "$2" >"$TEST_GUM_ANSWERS"
    shift 2
    printf '0\n' >"$STATE/gum-calls"
    printf '0\n' >"$STATE/fzf-calls"
    (cd "$dir" && timeout 30 script -qec "$(printf '%q ' "$TOOL" "$@")" /dev/null </dev/null 2>&1
        echo "rc=$?") | tr -d '\r'
}
NO_TEAMS="$SCRATCH/no-teams.yml"
fresh_no_teams() {
    reset
    : >"$STATE/units"
    printf 'agent_bus_address: 192.0.2.10\n' >"$NO_TEAMS"
}

echo "== no teams"
fresh_no_teams
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$OUTSIDE" "NO")"
check "no teams: the picker offers to create one, and no leaves with exit 0" "rc=0" "$(rc_of "$out")"
check_has "  saying there are none" "No agent teams on this machine yet" "$out"
check_has "  and how to create one later" "ccy-teams create" "$out"
check "  asking for no sudo, as there is nothing to read" "no" "$(logged 'sudo')"
check "  asking with gum, default no" "yes" "$(logged 'gum confirm --default=false Create one now?')"

fresh_no_teams
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES ENTER ENTER ENTER ALL ENTER ENTER YES)")"
check "yes runs create there and then; every default accepted with Enter" "rc=0" "$(rc_of "$out")"
written="$(cat "$NO_TEAMS")"
check_has "  the team is named after the GitHub owner, never the repository" "- team: example-org" "$written"
check_has "  you are its human: your login name" 'humans: ["owner"]' "$written"
check_has "  every repository of the owner, all ticked to start with" 'repo: "example-org/proj"' "$written"
check_has "    the other one too" 'repo: "example-org/other"' "$written"
check_has "  each with its default branch, from gh, never asked" 'branches: ["trunk"]' "$written"
check_has "  and no other machine" 'allow_from: []' "$written"
check "  owner, name, people, how to start the list, repositories, subnets, and two yeses" "8" "$(cat "$STATE/gum-calls")"
inputs="$(grep -F 'gum input' "$LOG")"
check_has "  the owner is asked first" "--header GitHub owner" "$(awk 'NR == 1' <<<"$inputs")"
check_has "    defaulting to this checkout's remote, lower-cased" "--value example-org" "$(awk 'NR == 1' <<<"$inputs")"
check_has "  then the name, defaulting to the owner's" "--value example-org" "$(grep -F -- '--header Team name' <<<"$inputs")"
check_has "  the login name is offered as the people's default" "--value owner" "$(grep -F -- '--header People' <<<"$inputs")"
check_has "  each explanation is printed, wrapped, before its question" "installer creates the accounts" \
    "$(grep -F 'gum style --foreground 252 --width' "$LOG")"
check "  and every header is one short line" "0" \
    "$(grep -E 'gum (input|filter)' "$LOG" | grep -cE -- '--header [^-]{70,}')"
check "  which gh looked up" "yes" "$(logged 'gh api users/example-org')"
check_has "  the repositories explanation says what the list is for" "send each other to" \
    "$(grep -F 'gum style' "$LOG")"
check_has "  the person chooses how the list starts" "All repos ticked (untick the ones you don't want)" \
    "$(grep -F 'gum choose' "$LOG")"
check_has "    or" "No repos ticked (tick the ones you want)" "$(grep -F 'gum choose' "$LOG")"
check_has "  all ticked starts the list all ticked" "--selected *" "$(grep -F 'gum filter' "$LOG")"
check_has "  its header names the keys" "Tab unticks" "$(grep -F 'gum filter' "$LOG")"
items="$(cat "$STATE/filter-items")"
check_has "  the owner's repositories are offered, lower-cased" "example-org/other" "$items"
check_lacks "  but not an archived one" "example-org/old" "$items"
check_lacks "  nor one with no commits" "example-org/fresh" "$items"
check_has "  which is named as left out" "example-org/fresh" "$out"
check_has "  the summary says other branches travel by pull request" "pull requests" "$out"
check_has "  the next steps are numbered, ending with how to bring in agents" "ccy --teams 1@example-org" "$out"
check_has "  and name the play that installs it" "play-agent-bus.yml" "$out"
check_has "  gum's greys are replaced, through its environment" "GUM_INPUT_HEADER_FOREGROUND=255" \
    "$(cat "$STATE/gum-env")"
check_lacks "  no 240-range grey for anything read" "=24" "$(grep -v 'UNSELECTED_BACKGROUND' "$STATE/gum-env")"

echo "== create, on a terminal"
# Each case starts from a copy of the host_vars, alpha declared.
WIZ="$SCRATCH/wiz.yml"
fresh_wiz() {
    reset
    cp "$HOST_VARS" "$WIZ"
}
before="$(cat "$HOST_VARS")"
fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' '=nope' '=Example-Org' '=Bad_Name' '=beta' ENTER \
    NONE 'PICK example-org/proj,example-org/other' '=198.51.100.0/24' YES)" create)"
check "a bad name, then a good one; an unknown owner, then a known one" "rc=0" "$(rc_of "$out")"
check_has "  the bad name is explained, and asked again" "Try again" "$out"
check_has "  the unknown owner is explained" "no user or organisation called 'nope'" "$out"
written="$(cat "$WIZ")"
check_lacks "  none ticked starts the list with nothing ticked" "--selected" "$(grep -F 'gum filter' "$LOG")"
check_has "    and its header says Tab ticks" "Tab ticks" "$(grep -F 'gum filter' "$LOG")"
check_has "  both repositories chosen are written, each with its own default branch" \
    'repo: "example-org/other"' "$written"
check_has "    trunk for the one whose default is trunk" 'branches: ["trunk"]' "$written"
check_has "  the subnet is written" 'allow_from: ["198.51.100.0/24"]' "$written"

fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' ENTER ESC)" create)"
check "Esc at a question leaves with 130" "rc=130" "$(rc_of "$out")"
check_has "  in one line" "Cancelled; nothing was changed." "$out"
check "  and writes nothing" "$before" "$(cat "$WIZ")"
fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' ENTER CTRLC)" create)"
check "Ctrl-C at a question, the same" "rc=130" "$(rc_of "$out")"
check_lacks "  with no traceback" "Traceback" "$out"
check "  and writes nothing" "$before" "$(cat "$WIZ")"
fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' ENTER '=gamma' ENTER ALL ENTER ENTER NO)" create)"
check "no at the summary adds nothing" "rc=1" "$(rc_of "$out")"
check_has "  saying so" "Nothing was added." "$out"
check "  the file is as it was" "$before" "$(cat "$WIZ")"

fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" TEST_GH_NOAUTH=1 run_in "$CHECKOUT" "" create)"
check "gh not logged in: create stops before asking" "rc=1" "$(rc_of "$out")"
check_has "  and says to run gh auth login" "gh auth login" "$out"
fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" PATH="$PATH_WITHOUT_GUM" run_in "$CHECKOUT" "" create)"
check "gum missing: create stops" "rc=1" "$(rc_of "$out")"
check_has "  naming the play that installs it" "play-claude-yolo.yml" "$out"

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
