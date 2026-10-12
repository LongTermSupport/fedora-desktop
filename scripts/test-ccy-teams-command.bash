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
cat >"$BIN/fzf" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
rows="$(cat -)"
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

# sudo: -v succeeds; otherwise runs the named command's fake by its basename. TEST_SUDO_FAIL
# makes every sudo fail as one with no cached credential does.
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'sudo %s\n' "$*" >>"$TEST_LOG"
if [ -n "${TEST_SUDO_FAIL:-}" ]; then
    echo "sudo: a password is required" >&2
    exit 1
fi
[ "$1" = "-v" ] && exit 0
[ "$1" = "-n" ] && shift
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
"seat list") cat "$TEST_STATE/seats/$(basename "$PWD")" ;;
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
cat >"$BIN/launcher" <<'EOF'
#!/usr/bin/env bash
printf 'launcher %s\n' "$*" >>"$TEST_LOG"
EOF
chmod 755 "$BIN"/*

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
OUTSIDE="$SCRATCH/not-a-project"
mkdir -p "$OUTSIDE"

export TEST_LOG="$LOG" TEST_STATE="$STATE" TEST_ANSWERS="$SCRATCH/answers" TEST_FZF_ROWS="$SCRATCH/fzf-rows"
export PATH="$BIN:$PATH" HOME="$HOME_DIR" CCY_STATE_DIR="$SCRATCH/ccy-state" CCY_LIB="$LIB_DIR"
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
    printf '0\n' >"$STATE/fzf-calls"
    printf 'agent-bus-hs@alpha.service loaded active running x\nagent-bus-hs@extra.service loaded inactive dead x\n' \
        >"$STATE/units"
    rm -f "$STATE/active/"* "$STATE/devices/"*
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
check_has "the human" "join" "$(grep -E '^alpha +owner +human' <<<"$out")"
extra="$(grep -E '^extra +- +team' <<<"$out")"
check_has "a loaded homeserver not declared is listed, down" "down:inactive" "$extra"
check_has "  with no port and no members" "port unknown (not declared), members unknown" "$extra"
check "a team declared absent is not listed" "0" "$(grep -c '^gone ' <<<"$out")"
check "the members are read with sudo's cached credential only" "yes" \
    "$(logged 'sudo -n /usr/local/bin/agent-bus list alpha')"
check "  and never for a homeserver that is down" "no" "$(logged 'agent-bus list extra')"

reset
out="$(cd "$OUTSIDE" && TEST_SUDO_FAIL=1 "$TOOL" --list 2>"$SCRATCH/err")"
check "--list with no cached sudo still exits 0" "0" "$?"
check_has "  says why the members are left out" "members of alpha are left out" "$(cat "$SCRATCH/err")"
check_has "  and the seat's state is unknown" "unknown" "$(grep -E '^alpha +1 +ccy' <<<"$out")"

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

# ── the picker ───────────────────────────────────────────────────────────────────────────
echo "== the picker"
reset
"$TOOL" </dev/null >"$SCRATCH/out" 2>"$SCRATCH/err"
check "it needs a terminal" "1" "$?"
check_has "  and points at --list" "--list" "$(cat "$SCRATCH/err")"

reset
out="$(run_picker "$OUTSIDE" "|ABORT")"
check "Esc leaves, exit 0" "rc=0" "$(rc_of "$out")"
check "it asks for sudo first" "yes" "$(logged 'sudo -v')"

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

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
