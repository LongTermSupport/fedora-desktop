#!/usr/bin/env bash
# Unit-test `ccy-sessions restore-status` and the reboot audit
# (files/home/.local/bin/ccy-sessions).
#
# WHY THIS EXISTS. `restore-status` is where this feature's central rule is either kept or
# broken: **"could not tell" and "nothing to do" must be different answers.** Every other part
# of the design appeals to it, and it is the one command an operator runs to find out what their
# machine will do after a reboot. Three fixes landed in this file after a review found the rule
# violated three separate ways — a listing failure printed as an empty section, an unreachable
# systemd reported as "not enabled", and a record count of zero for a directory that could not
# be read — and all three were verified by reading the diff, which is exactly how the first
# violations got in.
#
# So the states are driven here against the REAL file: the installation states through a stub
# `systemctl`/`loginctl`, and the registry contents through the real library.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
SESSIONS_CMD="$REPO_ROOT/files/home/.local/bin/ccy-sessions"
LIB_DIR="$CCY_DIR/lib"

for required in "$SESSIONS_CMD" "$LIB_DIR/session-registry.bash"; do
    if [ ! -e "$required" ]; then
        echo "FAIL: required file not found: $required" >&2
        exit 1
    fi
done

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/common-pure.bash
source "$LIB_DIR/common-pure.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../files/var/local/claude-yolo/lib/session-registry.bash
source "$LIB_DIR/session-registry.bash"

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %s\n        got:  %s\n' "$label" "$want" "$got" >&2
    fi
}

WORK="$(mktemp -d)"
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT

STUB_BIN="$WORK/bin"
mkdir -p "$STUB_BIN"
FAKE_HOME="$WORK/home"
UNIT_DIR="$FAKE_HOME/.config/systemd/user"
mkdir -p "$UNIT_DIR"

# The two probes restore_installation_state makes. Each answers from a file this test writes, so
# every state — including the ones that exist only when something is BROKEN, which is the whole
# point — can be driven without a systemd to break.
cat >"$STUB_BIN/systemctl" <<STUB
#!/bin/sh
if [ -f "$WORK/is-enabled-rc" ]; then rc=\$(cat "$WORK/is-enabled-rc"); else rc=0; fi
if [ -f "$WORK/is-enabled-out" ]; then cat "$WORK/is-enabled-out"; fi
exit "\$rc"
STUB
cat >"$STUB_BIN/loginctl" <<STUB
#!/bin/sh
if [ -f "$WORK/linger-rc" ]; then rc=\$(cat "$WORK/linger-rc"); else rc=0; fi
if [ -f "$WORK/linger-out" ]; then cat "$WORK/linger-out"; fi
exit "\$rc"
STUB
# tmux is only reached by the reboot audit; absent output means "no server yet".
printf '#!/bin/sh\nexit 0\n' >"$STUB_BIN/tmux"
chmod +x "$STUB_BIN/systemctl" "$STUB_BIN/loginctl" "$STUB_BIN/tmux"

STATE="$WORK/state"
SESSIONS="$STATE/ccy/sessions"

# set_systemd <is-enabled-output> <is-enabled-rc> <linger-output> <linger-rc>
set_systemd() {
    printf '%s' "$1" >"$WORK/is-enabled-out"
    printf '%s' "$2" >"$WORK/is-enabled-rc"
    printf '%s' "$3" >"$WORK/linger-out"
    printf '%s' "$4" >"$WORK/linger-rc"
}

OUT=""
RC=0
# RUN_TMPDIR, when set to a path that does not exist, makes the registry's `mktemp` fail — which
# is how the "the registry could not be read" branch is driven. A permission-based failure
# (chmod 000) is useless here: this suite runs as root in CI and in the dev container, and root
# is not stopped by a mode, so the case would have passed by reading the directory it was
# supposed to be denied.
RUN_TMPDIR=""
run_sessions() {
    OUT="$(
        PATH="$STUB_BIN:$PATH" \
            XDG_STATE_HOME="$STATE" \
            CCY_LIB="$LIB_DIR" \
            CCY_LAUNCHER="$WORK/fake-launcher" \
            HOME="$FAKE_HOME" \
            TMPDIR="${RUN_TMPDIR:-${TMPDIR:-/tmp}}" \
            bash "$SESSIONS_CMD" "$@" 2>&1
    )"
    RC=$?
}

# said <text> — did the report say it?
said() {
    printf '%s' "$OUT" | grep -qF "$1" && echo yes || echo no
}

reset_all() {
    rm -rf "$STATE"
    mkdir -p "$SESSIONS"
    rm -f "$UNIT_DIR/ccy-sessions-restore.service"
    set_systemd enabled 0 yes 0
}

install_unit() { printf '[Unit]\n' >"$UNIT_DIR/ccy-sessions-restore.service"; }

write_record() {
    ccy_registry_write "$SESSIONS" "$1" \
        "project_dir=$2" "project_name=$(basename "$2")" \
        "boot_id=a-boot" "boot_time=1" "restore=${3:-yes}"
}

# ── the installation states, each of which needs a DIFFERENT fix ─────────────────────

reset_all
run_sessions restore-status
check "no unit file reports not-installed" "yes" "$(said 'not-installed')"
check "and says how to turn it on" "yes" "$(said 'ccy_restore_sessions=true')"
check "a clean report exits 0" "0" "$RC"

reset_all
install_unit
set_systemd disabled 1 yes 0
run_sessions restore-status
check "a disabled unit reports installed-not-enabled" "yes" "$(said 'installed-not-enabled')"

# THE ONE THAT MATTERS. systemctl failing for any other reason — a masked unit, an unreachable
# user bus, the dangling .wants symlink a delete-without-disable leaves — was reported as
# "installed-not-enabled", whose advice is "re-run the play". None of those is fixed by the
# play, and all of them look identical from that advice.
reset_all
install_unit
set_systemd "Failed to connect to bus" 1 yes 0
run_sessions restore-status
check "an unreachable bus is NOT reported as merely disabled" "no" "$(said 'installed-not-enabled')"
check "it reports installed-state-unknown instead" "yes" "$(said 'installed-state-unknown')"
check "and says the answer is unknown, not negative" "yes" "$(said 'UNKNOWN')"
check "and quotes what systemctl actually said" "yes" "$(said 'Failed to connect to bus')"

# Enabled but not lingering: everything looks right and the unit simply never runs at boot.
reset_all
install_unit
set_systemd enabled 0 no 0
run_sessions restore-status
check "no linger reports enabled-no-linger" "yes" "$(said 'enabled-no-linger')"
check "and says it will not run at boot" "yes" "$(said 'will not run at boot')"

reset_all
install_unit
set_systemd enabled 0 "" 1
run_sessions restore-status
check "an unanswerable linger probe reports enabled-linger-unknown" "yes" \
    "$(said 'enabled-linger-unknown')"

reset_all
install_unit
run_sessions restore-status
check "enabled and lingering reports enabled" "yes" "$(said 'Session restore on this machine: enabled')"

# ── the registry axis, reported INDEPENDENTLY of the state above ─────────────────────
#
# This is the sentence the whole design argues for: a machine can have sessions recorded AND no
# restore installed, and those are two facts, not one shrug.

reset_all
write_record ccy-alpha /projects/alpha
write_record ccy-beta /projects/beta no
run_sessions restore-status
check "records are counted while restore is not installed" "yes" "$(said 'Recorded sessions: 2')"
check "and it says plainly that none will be restored" "yes" \
    "$(said 'None of these will be restored')"
check "each record's restore flag is shown" "yes" "$(said 'ccy-beta')"

reset_all
run_sessions restore-status
check "an empty registry reports zero, not silence" "yes" "$(said 'Recorded sessions: 0')"

# ── "could not tell" must not print as "nothing" ─────────────────────────────────────
#
# An unreadable registry directory. The report must say so, must NOT say "0", and must exit
# non-zero — and it must still print everything else, because a status command that says LESS
# the more wrong the machine is, is the opposite of useful.
reset_all
install_unit
write_record ccy-gamma /projects/gamma
RUN_TMPDIR="$WORK/no-such-tmpdir"
run_sessions restore-status
RUN_TMPDIR=""
check "an unreadable registry does not report a count of 0" "no" "$(said 'Recorded sessions: 0')"
check "it says it could not be read" "yes" "$(said 'COULD NOT BE READ')"
check "it says that is not the same as none" "yes" "$(said 'not the same as none')"
check "and the command exits non-zero" "1" "$RC"
# The rest of the report must still have been produced. Returning early here meant the last-run
# note and the retired/quarantined sections vanished exactly when they were most wanted.
check "the installation state is still reported" "yes" \
    "$(said 'Session restore on this machine:')"
check "the last-run line is still reported" "yes" "$(said 'Last restore run')"

# ── retirement evidence is shown with its reason ─────────────────────────────────────
reset_all
install_unit
RETIRED="$STATE/ccy/restore/retired"
write_record ccy-delta /projects/delta
ccy_registry_retire "$SESSIONS/ccy-delta.record" "$RETIRED" directory-gone
run_sessions restore-status
check "a retired record is listed" "yes" "$(said 'ccy-delta')"
check "with the reason it was retired for" "yes" "$(said 'directory-gone')"

# "it ran and did nothing" and "it never ran" are different answers about a machine.
reset_all
install_unit
run_sessions restore-status
check "no last-run note reports 'never'" "yes" "$(said 'Last restore run: never')"

# ── the blocked reboot seam must ALWAYS refuse ───────────────────────────────────────
#
# With no sessions running, the refusal used to sit inside a per-session loop and never
# executed: the command exited 0 having neither warned anyone nor rebooted. A blocked command
# that sometimes succeeds is worse than one that always refuses.
reset_all
run_sessions reboot --in 5
check "reboot --in refuses even with no sessions running" "1" "$RC"
check "and names the upstream issue" "yes" "$(said 'claude-code-hooks-daemon#39')"
check "and says nothing was rebooted" "yes" "$(said 'NOTHING WAS REBOOTED')"

reset_all
run_sessions notify reboot-warning --minutes 5
check "notify refuses too" "1" "$RC"
check "and names the same issue" "yes" "$(said 'claude-code-hooks-daemon#39')"

# The dry run is the half that works, and it must NOT refuse.
reset_all
run_sessions reboot --dry-run
check "reboot --dry-run succeeds" "0" "$RC"
check "and says it signalled and rebooted nothing" "yes" "$(said 'nothing was rebooted')"

# ── argument validation, which must not abort silently ───────────────────────────────
#
# `--in` with nothing after it consumed the value that was not there, then shifted past the end;
# under `set -e` that exited 1 with no message at all, twelve lines above the friendly one.
reset_all
run_sessions reboot --in
check "a trailing --in is refused with the usage code" "64" "$RC"
check "and explains what it wanted" "yes" "$(said 'number of minutes')"

reset_all
run_sessions reboot --in abc
check "a non-numeric --in is refused" "64" "$RC"

reset_all
run_sessions notify --minutes
check "notify with no kind is refused" "64" "$RC"

reset_all
run_sessions notify not-a-kind
check "an unknown signal kind is refused" "64" "$RC"
check "and lists the kinds it knows" "yes" "$(said 'reboot-warning')"

reset_all
run_sessions restore-status extra-argument
check "restore-status takes no arguments" "64" "$RC"

reset_all
run_sessions --help
check "--help works without a terminal" "0" "$RC"
check "and documents restore-status" "yes" "$(said 'restore-status')"
check "and marks the blocked subcommands as blocked" "yes" "$(said 'BLOCKED')"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
