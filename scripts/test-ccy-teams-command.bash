#!/usr/bin/env bash
# Unit-test the ccy-teams command (files/home/.local/bin/ccy-teams.j2), the manager of the
# agent teams on a machine: its table (--list), every picker key, and its create, apply,
# remove, backup and restore wrappers. (scripts/test-ccy-teams.bash is ccy's own --teams flag.)
#
# WHY THIS EXISTS. ccy-teams holds no team logic: every row is something agent-bus,
# systemd, tmux or the session registry said, and every key hands over to one of them. What
# must hold is that the rows say what those sources said (a seat with the session holding
# it and its restore mark, the members that are not seats, the humans, a homeserver that
# is down), that each key reaches the right command with the right row's values, that a
# yes/no answered no changes nothing, and that the template renders to a script with no
# Jinja left in it. None of the real commands can run here, so fakes for tmux, fzf,
# systemctl, sudo, agent-bus, agent-bus-install, gio, the ccy launcher and play-agent-bus.yml
# keep their state in files and log every call; the declare helper, git and the registry
# library are real.
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
remove) rm -f "$TEST_STATE/applied/$3.json" ;;
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
echo "electron chatter on stdout"
echo "electron chatter on stderr" >&2
[[ -z "${TEST_GIO_FAIL:-}" ]]
EOF
cat >"$BIN/systemd-run" <<'EOF'
#!/usr/bin/env bash
args=()
while [[ $# -gt 0 && "$1" != "--" ]]; do args+=("$1"); shift; done
printf 'systemd-run %s\n' "${args[*]}" >>"$TEST_LOG"
shift
exec "$@"
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

# gh: logged in unless TEST_GH_NOAUTH; example-org is the one owner GitHub has.
cat >"$BIN/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'gh %s\n' "$*" >>"$TEST_LOG"
case "$1 ${2:-}" in
"auth status")
    if [ -n "${TEST_GH_NOAUTH:-}" ]; then echo "You are not logged into any GitHub hosts." >&2; exit 1; fi ;;
"api users/example-org") ;;
# A repository's permissions.push for the login: example-org/readonly is read-only.
"api repos/example-org/proj" | "api repos/example-org/alpha" | "api repos/other/tool")
    [ "$3 $4" = "--jq .permissions.push" ] || { echo "fake gh: unexpected call: $*" >&2; exit 97; }
    echo true ;;
"api repos/example-org/readonly") echo false ;;
"api "*) echo "gh: Not Found (HTTP 404)" >&2; exit 1 ;;
*) echo "fake gh: unexpected call: $*" >&2; exit 97 ;;
esac
EOF
# play-agent-bus.yml, run by ccy-teams through its shebang: logs its arguments; unless
# TEST_PLAY_FAIL, each team in agent_bus_only_teams is applied as declared (its stamp
# written by the real functions the installer uses), or, declared absent, taken down.
cat >"$BIN/play" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'play %s\n' "$*" >>"$TEST_LOG"
[ "$1" = -e ] || { echo "fake play: expected -e JSON" >&2; exit 97; }
if [ -n "${TEST_PLAY_FAIL:-}" ]; then echo "fatal: [localhost]: FAILED!" >&2; exit 2; fi
cd "$TEST_REPO_ROOT" && python3 -m tests.helpers.agent_bus.ccy_teams_play "$2" "$CCY_TEAMS_HOST_VARS" "$TEST_STATE/applied"
EOF
# play-agent-bus-element.yml: logs its run; unless TEST_ELEMENT_FAIL, writes a launcher for
# every agent_bus_element_teams entry, as the real play's helper does.
cat >"$BIN/element-play" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'element-profiles %s\n' "$*" >>"$TEST_LOG"
if [ -n "${TEST_ELEMENT_FAIL:-}" ]; then echo "fatal: [localhost]: FAILED!" >&2; exit 2; fi
lines="$(cd "$TEST_REPO_ROOT" && python3 -m helpers.agent_bus.declare element-teams --file "$CCY_TEAMS_HOST_VARS")"
mkdir -p "$HOME/.local/share/applications"
while IFS=$'\t' read -r _ team _; do
    [ -z "$team" ] || : >"$HOME/.local/share/applications/agent-bus-$team-element.desktop"
done <<<"$lines"
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
        if [ "$sub" = choose ]; then
            printf '%s\n' "$selected"
        elif [ "$sub" != filter ]; then
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
    org: example-org
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
OUTSIDE="$SCRATCH/not-a-project"
mkdir -p "$OUTSIDE"

export TEST_LOG="$LOG" TEST_STATE="$STATE" TEST_ANSWERS="$SCRATCH/answers" TEST_FZF_ROWS="$SCRATCH/fzf-rows"
export TEST_FZF_RAW="$SCRATCH/fzf-raw"
unset NO_COLOR WAYLAND_DISPLAY XDG_STATE_HOME
export TEST_GUM_ANSWERS="$SCRATCH/gum-answers"
# The ccy image installs a real gum, so every directory holding one is left out.
PATH_WITHOUT_GUM="$BIN"
IFS=: read -ra path_dirs <<<"$PATH"
for dir in "${path_dirs[@]}"; do
    [ -x "$dir/gum" ] || PATH_WITHOUT_GUM+=":$dir"
done
export PATH="$BIN:$GUMBIN:$PATH" HOME="$HOME_DIR" CCY_STATE_DIR="$SCRATCH/ccy-state" CCY_LIB="$LIB_DIR"
export CCY_TEAMS_HOST_VARS="$HOST_VARS" CCY_TEAMS_LAUNCHER="$BIN/launcher"
export CCY_TEAMS_PLAY="$BIN/play" CCY_TEAMS_APPLIED_DIR="$STATE/applied" TEST_REPO_ROOT="$REPO_ROOT"
export CCY_TEAMS_ELEMENT_PLAY="$BIN/element-play"
mkdir -p "$STATE/applied"
# alpha as built from this host_vars: the stamp the fake play writes, kept for every reset.
(cd "$REPO_ROOT" && python3 -m tests.helpers.agent_bus.ccy_teams_play '{"agent_bus_only_teams": ["alpha"]}' \
    "$HOST_VARS" "$STATE/applied")
cp "$STATE/applied/alpha.json" "$SCRATCH/alpha-stamp.json"

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
    rm -f "$STATE/applied/"*
    cp "$SCRATCH/alpha-stamp.json" "$STATE/applied/alpha.json"
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
check_has "  APPLIED among them" "APPLIED" "$heading"
alpha="$(grep -E '^alpha +- +team' <<<"$out")"
check_has "alpha's homeserver is up" " up " "$alpha"
check_has "  on its declared port" "port 6167" "$alpha"
check_has "  applied as declared (D65)" "up to date" "$alpha"
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
check_has "  and the row says what installs it" "ccy-teams apply alpha" "$alpha"
check_lacks "  never meta-deploy" "meta-deploy" "$out"
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
rm -f "$STATE/applied/"*.json # a built team is listed even when undeclared
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
printf '%s\n' NO "=Leave it running" >"$TEST_GUM_ANSWERS"
printf '0\n' >"$STATE/gum-calls"
out="$(run_picker "$OUTSIDE" "ctrl-x|port 6167" "|ABORT")"
check_has "Ctrl-X on a team starts its removal, saying what is lost" "history" "$out"
check "  and leaving it running changes nothing" "no" "$(logged 'play ')"

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

gum_queue() {
    printf '%s\n' "$@" >"$TEST_GUM_ANSWERS"
    printf '0\n' >"$STATE/gum-calls"
}
reset
cp "$HOST_VARS" "$SCRATCH/element.yml"
gum_queue NO
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/element.yml" run_picker "$OUTSIDE" "ctrl-e|owner" "|ABORT")"
check_has "Ctrl-E with no Element profile says so" "has no Element profile on this desktop yet" "$out"
check_lacks "  never telling you to edit the host_vars" "agent_bus_element_teams" "$out"
check "  offering to set it up; no opens nothing" "no" "$(logged 'gio')"
check "  and changes nothing" "$(cat "$HOST_VARS")" "$(cat "$SCRATCH/element.yml")"
reset
printf 'DEVICE\tABC\t1\n' >"$STATE/devices/owner"
gum_queue YES
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/element.yml" run_picker "$OUTSIDE" "ctrl-e|owner")"
check_has "  yes declares the team's Element profile" 'base_url: "http://192.0.2.10:6167"' \
    "$(cat "$SCRATCH/element.yml")"
check "  runs play-agent-bus-element.yml" "yes" "$(logged 'element-profiles')"
check "  then opens it" "yes" "$(logged "gio launch $LAUNCHER_FILE")"
reset
cp "$HOST_VARS" "$SCRATCH/element.yml"
gum_queue YES
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/element.yml" TEST_ELEMENT_FAIL=1 run_picker "$OUTSIDE" "ctrl-e|owner" "|ABORT")"
check_has "  a failed Element play is said" "play-agent-bus-element.yml failed" "$out"
check "  and nothing is opened" "no" "$(logged 'gio')"
reset
element_profile
printf 'DEVICE\tABC\t1\n' >"$STATE/devices/owner"
out="$(run_picker "$OUTSIDE" "ctrl-e|owner")"
check "Ctrl-E opens the team's Element launcher" "yes" "$(logged "gio launch $LAUNCHER_FILE")"
check "  asking no password of a human who has a session" "no" "$(logged 'human password')"
check "  and exits 0" "rc=0" "$(rc_of "$out")"
check_has "  in its own user scope, so closing the terminal tab does not end it" \
    "systemd-run --user --scope --quiet --collect --unit ccy-teams-element-alpha-" "$(cat "$LOG")"
check_lacks "  Element's own output never reaches the terminal" "electron chatter" "$out"
check_has "  it goes to a log file, which is named" "ccy-teams/element-alpha.log" "$out"
check_has "  and holds both its streams" $'electron chatter on stdout\nelectron chatter on stderr' \
    "$(cat "$HOME_DIR/.local/state/ccy-teams/element-alpha.log")"
reset
element_profile
printf 'DEVICE\tABC\t1\n' >"$STATE/devices/owner"
out="$(TEST_GIO_FAIL=1 run_picker "$OUTSIDE" "ctrl-e|owner")"
check_has "a failed Element launch is said, with the log's tail" "Element did not start" "$out"
check_has "  showing what it said" "electron chatter on stderr" "$out"
check "  and is a failure" "rc=1" "$(rc_of "$out")"
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
copy_call="$(awk '/Copy the password\?/ { on = 1 } on { print } on && /--bind=q:abort/ { exit }' "$TEST_FZF_RAW")"
check_has "  the copy question is asked" "Yes, copy the password" "$(cat "$TEST_FZF_RAW")"
check_lacks "  with Yes, not Exit, as the default" "start:last" "$copy_call"
check_has "  while the password reset itself still defaults to Exit" "start:last" \
    "$(awk '/other Element sessions are signed out/ { on = 1 } on { print } on && /--bind=q:abort/ { exit }' "$TEST_FZF_RAW")"
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
check_has "? shows every key, explained" "Ctrl-U" "$out"
check_has "  and the APPLIED words" "decide: absent" "$out"
check_has "  and the status words" "need sudo" "$out"
reset
out="$(run_picker "$OUTSIDE" "|port 6167" "|ABORT")"
check_has "Enter on a team shows the same help" "Colours:" "$out"

# ── applied or not (D65), apply and remove ──────────────────────────────────────────────
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
    --org example-org --yes </dev/null 2>"$SCRATCH/err")"
check "create with every value adds the entry" "0" "$?"
check_has "  saying so on stdout, on a port neither declared nor listening" $'CHANGED\tteam beta, port 6169' "$out"
check "  and installs it: the play for beta alone" "yes" "$(logged 'play -e {"agent_bus_only_teams": ["beta"]}')"
check_has "  then the next steps" "ccy --teams beta" "$(cat "$SCRATCH/err")"
check_lacks "  never meta-deploy" "meta-deploy" "$(cat "$SCRATCH/err")"
check_lacks "  nor an edit of localhost.yml" "localhost.yml" "$(grep -v 'kept at' "$SCRATCH/err")"
check_has "  its Element profile declared beside it" $'agent_bus_element_teams:\n  - team: beta' "$(cat "$SCRATCH/create.yml")"
check "  and written by play-agent-bus-element.yml, after the team's play" "play,element-profiles" \
    "$(awk '/^play / { printf "play," } /^element-profiles/ { printf "element-profiles" }' "$LOG")"
check "  --yes offers no sign-in" "no" "$(logged 'human password')"
reset
cp "$HOST_VARS" "$SCRATCH/create.yml"
out="$(CCY_TEAMS_HOST_VARS="$SCRATCH/create.yml" "$TOOL" create --team beta --human owner \
    --repo example-org/proj --repo other/tool --yes </dev/null 2>"$SCRATCH/err")"
check "create --repo: a team of repositories of any owners" "0" "$?"
check_has "  each on any branch, no org" 'repos: [{"repo": "example-org/proj"}, {"repo": "other/tool"}]' \
    "$(cat "$SCRATCH/create.yml")"
check "  no org line for it" "1" "$(grep -c '^    org:' "$SCRATCH/create.yml")"
check "  each checked on GitHub for push access, as the questions do" "yes yes" \
    "$(logged 'gh api repos/example-org/proj --jq .permissions.push') $(logged 'gh api repos/other/tool --jq .permissions.push')"
reset
cp "$HOST_VARS" "$SCRATCH/create.yml"
cp "$SCRATCH/create.yml" "$SCRATCH/create.before"
CCY_TEAMS_HOST_VARS="$SCRATCH/create.yml" "$TOOL" create --team beta --human owner \
    --repo example-org/proj --repo example-org/readonly --repo other/missing --repo bad --yes \
    </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "create --repo --yes: a read-only, a missing and a malformed repository fail it (1)" "1" "$?"
check_has "  the read-only one named with its reason" "example-org/readonly: read-only" "$(cat "$SCRATCH/err")"
check_has "  the missing one named with its reason" "other/missing: not found" "$(cat "$SCRATCH/err")"
check_has "  the malformed one named with its reason" "bad: not OWNER/REPO" "$(cat "$SCRATCH/err")"
check_lacks "  the good one is not blamed" "example-org/proj:" "$(cat "$SCRATCH/err")"
check "  nothing is written" "" "$(diff "$SCRATCH/create.before" "$SCRATCH/create.yml")"
check "  nothing is installed" "no" "$(logged 'play ')"
reset
cp "$HOST_VARS" "$SCRATCH/create.yml"
CCY_TEAMS_HOST_VARS="$SCRATCH/create.yml" TEST_ELEMENT_FAIL=1 "$TOOL" create --team beta --human owner \
    --org example-org --yes </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "a failed Element play fails create" "1" "$?"
check_has "  saying the Element profile is not applied, and how to retry" "ccy-teams apply beta" "$(cat "$SCRATCH/err")"
check_has "  the org written, no repository list" 'org: "example-org"' "$(cat "$SCRATCH/create.yml")"
check "  in the host_vars" "1" "$(grep -c 'port: 6169' "$SCRATCH/create.yml")"
check "  listening on the bus address" "2" "$(grep -c 'listen: \["192.0.2.10"\]' "$SCRATCH/create.yml")"
reset
cp "$HOST_VARS" "$SCRATCH/create.yml"
CCY_TEAMS_HOST_VARS="$SCRATCH/create.yml" TEST_PLAY_FAIL=1 "$TOOL" create --team beta --human owner \
    --org example-org --yes </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "a failed install is a failure" "1" "$?"
check_has "  saying the team is declared but not installed, and how to retry" "ccy-teams apply beta" "$(cat "$SCRATCH/err")"

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
    rm -f "$STATE/applied/"*
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
# Answers: create now; the scope (this repository), the repository, the name, the people and the
# subnets all by Enter; the summary; no sign-in; no Element; yes to a session here.
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES ENTER ENTER ENTER ENTER ENTER YES NO NO YES)")"
check "yes runs create there and then; every default accepted with Enter" "rc=0" "$(rc_of "$out")"
written="$(cat "$NO_TEAMS")"
check_has "  the team is named after the repository, lower-cased" "- team: proj" "$written"
check_has "  you are its human: your login name" 'humans: ["owner"]' "$written"
check_has "  this repository, on any branch, is the whole scope" 'repos: [{"repo": "example-org/proj"}]' "$written"
check_lacks "    no org" "org:" "$written"
check_has "  and no other machine" 'allow_from: []' "$written"
check_has "  its Element profile is declared with it" $'agent_bus_element_teams:\n  - team: proj' "$written"
check "  the team is installed at once" "yes" "$(logged 'play -e {"agent_bus_only_teams": ["proj"]}')"
check "  and its Element profile written" "yes" "$(logged 'element-profiles')"
inputs="$(grep -F 'gum input' "$LOG")"
check_has "  the scope is asked first, listed repositories selected, this one first" \
    "gum choose --header Trust --selected Repositories you list (one or more), starting with example-org/proj" \
    "$(grep -F 'gum choose' "$LOG")"
check_has "  then the repository, defaulting to this checkout's remote, lower-cased" "--value example-org/proj" \
    "$(awk 'NR == 1' <<<"$inputs")"
check "    checked on GitHub for push access" "yes" "$(logged 'gh api repos/example-org/proj --jq .permissions.push')"
check_has "  then the name, defaulting to the repository's" "--value proj" "$(grep -F -- '--header Team name' <<<"$inputs")"
check_has "  the login name is offered as the people's default" "--value owner" "$(grep -F -- '--header People' <<<"$inputs")"
check_has "  each explanation is printed, wrapped, before its question" "installer creates the accounts" \
    "$(grep -F 'gum style --width' "$LOG")"
check "  and every header is one short line" "0" \
    "$(grep -E 'gum (input|filter)' "$LOG" | grep -cE -- '--header [^-]{70,}')"
check_has "  the one question before writing covers installing it" "gum confirm --default=true Create team proj?" "$(cat "$LOG")"
check_has "  the summary states the scope in plain words" "only example-org/proj, on any branch" "$out"
check_has "  and says Element is set up" "a profile for the team, set up on this desktop" "$out"
check_has "  the next steps are numbered, ending with how to bring in agents" "ccy --teams proj" "$out"
check_lacks "  never meta-deploy" "meta-deploy" "$out"
check_lacks "  nor an edit of localhost.yml" "localhost.yml" "$(grep -v 'saved to\|kept at' <<<"$out")"
check_has "  sign-in is offered straight after" "gum confirm --default=true Sign owner in to Element now?" "$(cat "$LOG")"
check_has "  then Element opened" "gum confirm --default=true Open proj in Element now?" "$(cat "$LOG")"
check "  declined, no password is set" "no" "$(logged 'human password')"
check "  in a checkout of the team, a session is offered, and started in it with no seat" "yes" \
    "$(logged 'launcher --teams proj (in proj)')"
check_has "  gum's greys are replaced by the terminal's own text colour" "GUM_INPUT_HEADER_FOREGROUND="$'\n' \
    "$(cat "$STATE/gum-env")"$'\n'
check_has "  with one accent from the terminal's theme" "GUM_CONFIRM_SELECTED_BACKGROUND=3" "$(cat "$STATE/gum-env")"
check "  and no 256-colour value anywhere (the theme decides every shade)" "0" \
    "$(grep -cE '=[0-9]{2,}$' "$STATE/gum-env")"
check_has "  gum's dim key-help line is hidden" "GUM_CONFIRM_SHOW_HELP=false" "$(cat "$STATE/gum-env")"
check_has "  each question is a numbered step" "1/5  What the team works on" "$out"
check_has "    the last of five" "5/5  Other machines" "$out"
check_has "  each answer stays on screen" "example-org/proj" "$(grep -F '✓' <<<"$out")"
check_lacks "    never the file's internals" "forge_api" "$out"
check_lacks "    nor its path limits" "path_prefixes" "$out"
check "  the team is explained once, not twice" "1" "$(grep -c 'private room' <<<"$out")"

# The sign-in is taken up; the session offer is declined.
fresh_no_teams
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES ENTER ENTER ENTER ENTER ENTER YES YES NO NO)")"
check "a yes to the sign-in sets the password straight after create" "yes" "$(logged 'sudo -n /usr/local/bin/agent-bus human password proj owner')"
check_has "  and shows the login" "Username" "$out"
check "  a no to the session starts nothing" "no" "$(logged 'launcher')"

# The whole owner: the second scope.
fresh_no_teams
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES NONE ENTER ENTER ENTER ENTER YES NO NO NO)")"
check "the whole GitHub owner as the scope" "rc=0" "$(rc_of "$out")"
written="$(cat "$NO_TEAMS")"
check_has "  is written as the org, no repository list" 'org: "example-org"' "$written"
check_lacks "    no repos" "repos:" "$written"
check "  looked up on GitHub" "yes" "$(logged 'gh api users/example-org')"
check_has "  the team is named after the owner" "- team: example-org" "$written"
check_has "  the summary says so in words" "everything example-org owns: any repository, any branch" "$out"

# No bus address declared yet: asked for (a bad one refused, bounded), then declared with the team.
fresh_no_teams
printf 'user_login: someone\n' >"$NO_TEAMS"
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES NONE ENTER ENTER ENTER ENTER \
    '=not-an-ip' '=192.0.2.10' YES NO NO NO)")"
check "no bus address: asked for, and the team is created" "rc=0" "$(rc_of "$out")"
check_has "  a bad one is refused with its reason" "not an IP address" "$out"
written="$(cat "$NO_TEAMS")"
check_has "  the address is declared by the tool" "agent_bus_address: 192.0.2.10" "$written"
check_has "  and the team listens on it" 'listen: ["192.0.2.10"]' "$written"
check_lacks "  the owner is never told to edit a file" "localhost.yml" "$(grep -v 'saved to\|kept at' <<<"$out")"
fresh_no_teams
printf 'user_login: someone\n' >"$NO_TEAMS"
CCY_TEAMS_HOST_VARS="$NO_TEAMS" "$TOOL" create --team beta --human owner --org example-org --yes \
    </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "no bus address with --yes: refused" "78" "$?"
check_has "  saying to run create on a terminal" "ccy-teams create" "$(cat "$SCRATCH/err")"
check_lacks "  and never to edit a file" "localhost.yml" "$(cat "$SCRATCH/err")"
check "  nothing written" "user_login: someone" "$(cat "$NO_TEAMS")"

# Several repositories, of any owners: failing entries named with why, the list kept as typed.
fresh_no_teams
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' YES ENTER \
    '=Example-Org/Alpha, example-org/readonly, other/missing, bad' '=example-org/alpha,other/tool' ENTER ENTER ENTER YES NO NO NO)")"
check "several repositories: a bounded re-prompt, then accepted" "rc=0" "$(rc_of "$out")"
check_has "  a read-only repository is named with its reason" "example-org/readonly: read-only" "$out"
check_has "  a missing one is named with its reason" "other/missing: not found" "$out"
check_has "  a malformed one is named" "bad: not OWNER/REPO" "$out"
check_has "  the list comes back as typed, to edit" "--value Example-Org/Alpha, example-org/readonly, other/missing, bad" \
    "$(grep -F 'gum input' "$LOG" | awk 'NR == 2')"
written="$(cat "$NO_TEAMS")"
check_has "  the repositories of any owners, on any branch" 'repos: [{"repo": "example-org/alpha"}, {"repo": "other/tool"}]' "$written"
check_has "  the team is named after the first" "- team: alpha" "$written"
check_has "  the summary names them" "only these repositories, on any branch: example-org/alpha, other/tool" "$out"

# The old default path limit: offered once, removed through declare.py, and applied.
fresh_no_teams
cat >"$NO_TEAMS" <<'EOF'
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: legacy
    port: 6190
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: example-org
    forge_api: https://api.github.com
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
EOF
printf '|ABORT\n' >"$TEST_ANSWERS"
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$OUTSIDE" "$(printf '%s\n' YES)")"
check_has "a path limit is offered for removal" "gum confirm --default=true Remove the limit, so any path in its repositories can be named?" "$(cat "$LOG")"
check_has "  saying what it does" "may only point each other at files under" "$out"
check_has "  naming what the team covers" "Team legacy (for any repository of example-org)" "$out"
check_has "  and that it is about a team already here, not this checkout" "a team already on this machine" "$out"
check_lacks "  and never claiming it was the default" "was the default" "$out"
check_lacks "  and removed from the entry" "path_prefixes" "$(cat "$NO_TEAMS")"
check "  then applied" "yes" "$(logged 'play -e {"agent_bus_only_teams": ["legacy"]}')"
fresh_no_teams
cat >"$NO_TEAMS" <<'EOF'
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: legacy
    port: 6190
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: example-org
    forge_api: https://api.github.com
    path_prefixes: ["CLAUDE/Plan/", "docs/"]
EOF
printf '|ABORT\n' >"$TEST_ANSWERS"
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$OUTSIDE" "$(printf '%s\n' NO NO)")"
check_has "  declined, the limit stays" "path_prefixes" "$(cat "$NO_TEAMS")"
printf '|ABORT\n' >"$TEST_ANSWERS"
: >"$LOG" # not reset: that clears the declined marker too
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$OUTSIDE" "$(printf '%s\n' NO)")"
check "  and not offered again" "no" "$(logged 'Remove the limit, so any path')"

echo "== a checkout no team covers"
fresh_no_teams
cat >"$NO_TEAMS" <<'EOF'
agent_bus_address: 192.0.2.10
agent_bus_teams:
  - team: elsewhere
    port: 6191
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: other-org
    forge_api: https://api.github.com
EOF
printf '|ABORT\n' >"$TEST_ANSWERS"
# Answers: no to updating the team (not built here), no to creating one for the checkout.
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$CHECKOUT" "$(printf '%s\n' NO NO)")"
check_has "a checkout no team covers is named as such" "No team on this machine works on example-org/proj (this checkout)" "$out"
check_has "  so the other teams are not mistaken for its own" "The teams listed next are for other repositories" "$out"
check "  and creating one is offered, default yes" "yes" "$(logged 'gum confirm --default=true Create a team for example-org/proj now?')"
check_has "  declined, it says how to create one later" "ccy-teams create starts one whenever you like" "$out"
check "  and the picker still opens" "rc=0" "$(rc_of "$out")"
printf '|ABORT\n' >"$TEST_ANSWERS"
out="$(CCY_TEAMS_HOST_VARS="$NO_TEAMS" run_in "$OUTSIDE" "")"
check_lacks "outside a checkout, nothing is said about one" "this checkout" "$out"

echo "== applied, apply, remove"
STATES="$SCRATCH/states.yml"
fresh_states() {
    reset
    cp "$HOST_VARS" "$STATES"
    cat >>"$STATES" <<'EOF'
  - team: beta
    port: 6180
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: example-org
    forge_api: https://api.github.com
  - team: delta
    port: 6181
    listen: ["192.0.2.10"]
    allow_from: []
    humans: ["owner"]
    org: example-org
    forge_api: https://api.github.com
EOF
    # beta built from an older declaration; gone declared absent but still built; zeta
    # built and not declared at all. delta was never built.
    printf '{"v": 1, "team": "beta", "config": "%s", "software": "%s"}\n' "$(printf '0%.0s' {1..64})" \
        "$(python3 -c 'import json, sys; print(json.load(open(sys.argv[1]))["software"])' "$STATE/applied/alpha.json")" \
        >"$STATE/applied/beta.json"
    for team in gone zeta; do
        printf '{"v": 1, "team": "%s", "config": "%s", "software": "%s"}\n' "$team" \
            "$(printf '1%.0s' {1..64})" "$(printf '2%.0s' {1..64})" >"$STATE/applied/$team.json"
    done
}
fresh_states
out="$(cd "$OUTSIDE" && CCY_TEAMS_HOST_VARS="$STATES" "$TOOL" --list 2>"$SCRATCH/err")"
check_has "a team as declared is up to date" "up to date" "$(grep -E '^alpha +- +team' <<<"$out")"
check_has "a team built from an older declaration is out of date, saying what differs" "out of date: config" \
    "$(grep -E '^beta +- +team' <<<"$out")"
check_has "a team declared and never built is not built" "not built" "$(grep -E '^delta +- +team' <<<"$out")"
check_has "a team declared absent but still built needs a decision" "decide: absent" "$(grep -E '^gone +- +team' <<<"$out")"
check_has "a team built but not declared needs a decision" "decide: undeclared" "$(grep -E '^zeta +- +team' <<<"$out")"
check "  read with no sudo" "no" "$(logged 'sudo -v')"

fresh_states
CCY_TEAMS_HOST_VARS="$STATES" "$TOOL" apply --all >"$SCRATCH/out" 2>"$SCRATCH/err"
check "apply --all succeeds once each team matches its declaration" "0" "$?"
check "  running the play once" "1" "$(grep -c '^play ' "$LOG")"
check_has "  for the teams out of date or not built, and only those" '"agent_bus_only_teams": ["beta", "delta"]' "$(grep '^play ' "$LOG")"
check_has "  saying which teams it leaves for your decision" "gone (decide: absent)" "$(cat "$SCRATCH/err")"
check_has "    both of them" "zeta (decide: undeclared)" "$(cat "$SCRATCH/err")"
check_has "  saying so" "Applied: beta delta." "$(cat "$SCRATCH/err")"
check "  never taking a team down" "no" "$(logged 'agent-bus-install remove')"
out="$(cd "$OUTSIDE" && CCY_TEAMS_HOST_VARS="$STATES" "$TOOL" --list 2>"$SCRATCH/err")"
check_has "  beta is up to date afterwards" "up to date" "$(grep -E '^beta +- +team' <<<"$out")"

fresh_states
CCY_TEAMS_HOST_VARS="$STATES" "$TOOL" apply delta >"$SCRATCH/out" 2>"$SCRATCH/err"
check "apply <team> runs the play for that team" "yes" "$(logged 'play -e {"agent_bus_only_teams": ["delta"]}')"
fresh_states
CCY_TEAMS_HOST_VARS="$STATES" TEST_PLAY_FAIL=1 "$TOOL" apply delta >"$SCRATCH/out" 2>"$SCRATCH/err"
check "a failed play is a failure" "1" "$?"
check_has "  saying the entry stands but is not applied, and how to retry" "ccy-teams apply" "$(cat "$SCRATCH/err")"
CCY_TEAMS_HOST_VARS="$STATES" "$TOOL" apply gone >"$SCRATCH/out" 2>"$SCRATCH/err"
check "apply refuses a team declared absent" "64" "$?"
check_has "  pointing at remove" "ccy-teams remove gone" "$(cat "$SCRATCH/err")"
"$TOOL" apply >"$SCRATCH/out" 2>"$SCRATCH/err"
check "apply needs a team or --all" "64" "$?"
"$TOOL" remove alpha </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "remove needs a terminal: a human decides" "64" "$?"
"$TOOL" remove alpha --yes </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "  and takes no --yes" "64" "$?"

fresh_states
out="$(CCY_TEAMS_HOST_VARS="$STATES" run_in "$OUTSIDE" "$(printf '%s\n' ENTER '=Take it down, keep its data')" remove beta)"
check "remove: a backup first by default, then down with the data kept" "rc=0" "$(rc_of "$out")"
check "  the backup is taken first" "yes" "$(logged 'sudo /usr/local/sbin/agent-bus-install backup-now --team beta')"
check_has "  the backup is offered, default yes" "gum confirm --default=true Back beta up first?" "$(cat "$LOG")"
check_has "  beta is declared absent, its data kept" "purge: false" "$(cat "$STATES")"
check "  the play runs for beta alone" "yes" "$(logged 'play -e {"agent_bus_only_teams": ["beta"]}')"
check_has "  and says the data is kept" "its data is kept" "$out"

fresh_states
out="$(CCY_TEAMS_HOST_VARS="$STATES" run_in "$OUTSIDE" "$(printf '%s\n' NO '=Take it down and delete its data' '=betta')" remove beta)"
check "remove and delete: a wrong name deletes nothing" "rc=1" "$(rc_of "$out")"
check_lacks "  the entry is not changed" "purge" "$(cat "$STATES")"
check "  and no play runs" "no" "$(logged 'play ')"
check "  no backup when declined" "no" "$(logged 'backup-now')"
fresh_states
out="$(CCY_TEAMS_HOST_VARS="$STATES" run_in "$OUTSIDE" "$(printf '%s\n' NO '=Take it down and delete its data' '=beta')" remove beta)"
check "  typing the team's name deletes it" "rc=0" "$(rc_of "$out")"
check_has "  declared absent with purge: true" "purge: true" "$(cat "$STATES")"
fresh_states
out="$(CCY_TEAMS_HOST_VARS="$STATES" run_in "$OUTSIDE" "$(printf '%s\n' NO '=Take it down, keep its data')" remove zeta)"
check "a team built but not declared is taken down by the installer, after the same questions" "yes" \
    "$(logged 'sudo /usr/local/sbin/agent-bus-install remove --team zeta')"
check_lacks "  never with --purge unless chosen" "--purge" "$(grep 'remove --team zeta' "$LOG")"

fresh_states
printf '|ABORT\n' >"$TEST_ANSWERS"
out="$(CCY_TEAMS_HOST_VARS="$STATES" run_in "$OUTSIDE" "$(printf '%s\n' NO)")"
check_has "the picker offers to update the teams out of date when it starts" "Not as declared" "$out"
check "  and no leaves them" "no" "$(logged 'play ')"

# ── backup, restore, create ──────────────────────────────────────────────────────────────
echo "== create, on a terminal"
# Each case starts from a copy of the host_vars, alpha declared.
WIZ="$SCRATCH/wiz.yml"
fresh_wiz() {
    reset
    cp "$HOST_VARS" "$WIZ"
}
before="$(cat "$HOST_VARS")"
fresh_wiz
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' NONE '=nope' '=Example-Org' '=Bad_Name' '=beta' ENTER \
    '=198.51.100.0/24' YES NO NO NO)" create)"
check "a bad name, then a good one; an unknown owner, then a known one" "rc=0" "$(rc_of "$out")"
check_has "  the bad name is explained, and asked again" "Try again" "$out"
check_has "  the unknown owner is explained" "no user or organisation called 'nope'" "$out"
written="$(cat "$WIZ")"
check_has "  the org is written, lower-cased" 'org: "example-org"' "$written"
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
out="$(CCY_TEAMS_HOST_VARS="$WIZ" run_in "$CHECKOUT" "$(printf '%s\n' ENTER ENTER '=gamma' ENTER ENTER NO)" create)"
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
