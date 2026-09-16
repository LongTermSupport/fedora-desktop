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
# `systemctl`/`loginctl`, the registry contents through the real library, and the reboot audit
# through a stub `tmux` that can report sessions, report none, or fail — the last being the case
# that must not read as "nothing is running", since an operator would reboot on it.
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
# tmux is reached by the reboot audit. It answers from files this test writes, so the audit can
# actually be driven: with no sessions, with sessions whose projects are ready, with a project
# that has no daemon CLI, and with a listing that FAILS. A stub that always exited 0 left every
# reboot case running the zero-session path, so the audit's table, its refusal and its
# listing-failure branch were untested while the suite's own header claimed otherwise.
cat >"$STUB_BIN/tmux" <<STUB
#!/bin/sh
if [ -f "$WORK/tmux-rc" ]; then rc=\$(cat "$WORK/tmux-rc"); else rc=0; fi
if [ "\$rc" -ne 0 ]; then
    # Deliberately NOT "no server running": ccy_tmux_list treats that phrase as the benign
    # first-run state and returns success with no sessions, which is correct. This stands for a
    # REAL failure — the case that must not be reported as an idle machine.
    echo "error connecting to the ccy socket (Permission denied)" >&2
    exit "\$rc"
fi
if [ -f "$WORK/tmux-sessions" ]; then cat "$WORK/tmux-sessions"; fi
exit 0
STUB
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
    : >"$WORK/tmux-sessions"
    printf '0' >"$WORK/tmux-rc"
}

# tmux_session <name> <dir> — one line in the format ccy_tmux_list parses.
tmux_session() { printf '%s 0 %s\n' "$1" "$2" >>"$WORK/tmux-sessions"; }

# A project directory with, or without, a hooks-daemon CLI — which is what the audit reports on.
#
# The stub RECORDS ITS ARGV to `cli-args.log`, one line per call. It used to be
# `#!/bin/sh\nexit 0\n`, which was enough while the signal seam only ever refused — but a stub
# that exits 0 whatever it is handed cannot tell "invoked with the right kind and number" from
# "invoked with neither", so every assertion about what `notify` passes would have been green
# against a command that passed nothing at all. Recording the argv is what makes those
# assertions able to fail.
#
# `failing-cli` exits non-zero. That is not a hypothetical branch: the daemon exits 1 when a
# project has no live session of its own, so a real machine reaches it.
make_project() {
    mkdir -p "$1"
    case "${2:-with-cli}" in
    with-cli)
        mkdir -p "$1/bin"
        printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s/cli-args.log"\nexit 0\n' "$1" >"$1/bin/hooks-daemon"
        chmod +x "$1/bin/hooks-daemon"
        ;;
    failing-cli)
        mkdir -p "$1/bin"
        printf '#!/bin/sh\nprintf "%%s\\n" "$*" >>"%s/cli-args.log"\necho "stub refusal" >&2\nexit 1\n' "$1" >"$1/bin/hooks-daemon"
        chmod +x "$1/bin/hooks-daemon"
        ;;
    without-cli) ;;
    *)
        printf 'make_project: unknown mode %s\n' "$2" >&2
        exit 1
        ;;
    esac
}

# What the project's CLI was actually invoked with. An ABSENT log means no call was made,
# which is a different answer from a call that did not carry the text — so the missing-file
# case returns `no` rather than letting grep fail the script under `set -e`.
said_in_log() {
    local log="$1/cli-args.log" needle="$2"
    if [ ! -f "$log" ]; then
        echo no
        return 0
    fi
    if grep -qF -- "$needle" "$log"; then
        echo yes
    else
        echo no
    fi
}

# How many times the project's CLI was invoked. Zero when it never was.
cli_call_count() {
    local log="$1/cli-args.log"
    if [ ! -f "$log" ]; then
        echo 0
        return 0
    fi
    awk 'END { print NR }' "$log"
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
# The whole LINE, not a substring: `enabled` is a prefix of `enabled-no-linger` and
# `enabled-linger-unknown`, so a substring match here would have passed for two states that mean
# the opposite of this one.
check "enabled and lingering reports exactly 'enabled'" "yes" \
    "$(printf '%s' "$OUT" | grep -qx 'Session restore on this machine: enabled' && echo yes || echo no)"

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
# The evidence directories must EXIST, or `list_with_reasons` short-circuits on "absent
# directory, nothing to report" and never reaches the read that this case breaks — so the half
# of the fix that covers those three sections would be asserted by nobody, inside the suite that
# exists because reading was not enough.
mkdir -p "$STATE/ccy/restore/attempted" "$STATE/ccy/restore/retired" "$STATE/ccy/restore/malformed"
RUN_TMPDIR="$WORK/no-such-tmpdir"
run_sessions restore-status
RUN_TMPDIR=""
check "an unreadable registry does not report a count of 0" "no" "$(said 'Recorded sessions: 0')"
# Four sections: the live registry plus attempted/, retired/ and malformed/. Every one of them
# has to say so — the bug this replaced aborted the report at the first, so the other three
# vanished exactly when an operator most needed them.
check "every section reports that it could not be read" "4" \
    "$(printf '%s' "$OUT" | grep -c 'COULD NOT BE READ — see the error above')"
# And the report closes by saying it is incomplete, so a reader who skimmed the middle still
# knows not to trust it.
check "the report declares itself INCOMPLETE at the end" "yes" "$(said 'INCOMPLETE')"
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

# ── notify raises a real signal: one call per PROJECT, a kind and a number ───────────
#
# Asserted against the stub's recorded argv, not just its exit status. The distinction that
# matters: a command that invoked the CLI with no arguments at all would exit 0 here too.
reset_all
make_project "$WORK/proj-n1"
tmux_session ccy-n1 "$WORK/proj-n1"
run_sessions notify reboot-warning --minutes 5
check "notify succeeds when every project can be signalled" "0" "$RC"
check "and names the project it signalled" "yes" "$(said 'proj-n1')"
check "invoking that project's own CLI with the signal verb and kind" "yes" \
    "$(said_in_log "$WORK/proj-n1" 'signal reboot-warning')"
check "passing the minutes through" "yes" "$(said_in_log "$WORK/proj-n1" '--minutes 5')"
check "asking for every session of that project" "yes" \
    "$(said_in_log "$WORK/proj-n1" '--all-sessions')"
check "scoped to that project root" "yes" \
    "$(said_in_log "$WORK/proj-n1" "--project-root $WORK/proj-n1")"

# Two sessions in ONE project are one signal, because --all-sessions covers the project. A
# per-session loop would call twice and warn the same sessions twice over.
reset_all
make_project "$WORK/proj-shared"
tmux_session ccy-s1 "$WORK/proj-shared"
tmux_session ccy-s2 "$WORK/proj-shared"
run_sessions notify reboot-warning --minutes 3
check "two sessions in one project take ONE call" "1" "$(cli_call_count "$WORK/proj-shared")"
check "and the command succeeds" "0" "$RC"

# reboot-cancelled carries NO number. The daemon refuses one; passing it anyway would turn a
# retraction into a refusal the operator did not ask for.
reset_all
make_project "$WORK/proj-n2"
tmux_session ccy-n2 "$WORK/proj-n2"
run_sessions notify reboot-cancelled
check "reboot-cancelled succeeds" "0" "$RC"
check "and passes no --minutes at all" "no" "$(said_in_log "$WORK/proj-n2" '--minutes')"

# A project whose signal FAILS fails the whole command. The daemon exits non-zero when a
# project has no live session of its own, so this branch is reached on real machines.
reset_all
make_project "$WORK/proj-ok"
make_project "$WORK/proj-fails" failing-cli
tmux_session ccy-ok "$WORK/proj-ok"
tmux_session ccy-fails "$WORK/proj-fails"
run_sessions notify reboot-warning --minutes 5
check "one failing project fails the whole command" "1" "$RC"
check "naming the project that was NOT signalled" "yes" "$(said 'proj-fails')"
check "and saying a partial warning reads as a complete one" "yes" "$(said 'partial warning')"
check "while still reporting the one that did go out" "yes" "$(said '1 were')"

# Nothing running is a SUCCESS with a sentence, not silence: there is genuinely nobody to warn.
reset_all
run_sessions notify reboot-warning --minutes 5
check "notify with no sessions running succeeds" "0" "$RC"
check "and says there is nobody to signal" "yes" "$(said 'nobody to signal')"

# The minutes rules are enforced here for the MESSAGE, and by the daemon for real.
reset_all
run_sessions notify reboot-warning
check "a warning with no --minutes is refused" "64" "$RC"
check "explaining that the signal carries how long is left" "yes" "$(said 'how long is left')"

reset_all
run_sessions notify reboot-warning --minutes 0
check "a warning of zero minutes is refused" "64" "$RC"

reset_all
run_sessions notify reboot-cancelled --minutes 5
check "a cancel WITH --minutes is refused" "64" "$RC"
check "saying there is no deadline to carry" "yes" "$(said 'no deadline to carry')"

reset_all
run_sessions notify shutdown-warning --minutes 2
check "shutdown-warning is a kind this command knows" "0" "$RC"

# ── reboot --in N still refuses, and must signal NOTHING while doing so ──────────────
#
# The refusal's reason changed but its shape must not: warning every session and then not
# rebooting is a new wrong state, not a safe subset. So the interesting assertion is not the
# exit code, it is that no signal went out.
reset_all
run_sessions reboot --in 5
check "reboot --in refuses even with no sessions running" "1" "$RC"
check "saying the countdown and reboot call are undesigned" "yes" "$(said 'undesigned')"
check "and that nothing was rebooted" "yes" "$(said 'NOTHING WAS REBOOTED')"
check "pointing at the notify that does work" "yes" "$(said 'notify reboot-warning')"

reset_all
make_project "$WORK/proj-r"
tmux_session ccy-r "$WORK/proj-r"
run_sessions reboot --in 5
check "reboot --in signals NOTHING before refusing" "0" "$(cli_call_count "$WORK/proj-r")"
check "and still refuses with sessions running" "1" "$RC"
check "after showing what it would have warned" "yes" "$(said 'ccy-r')"

# The dry run is the half that works, and it must NOT refuse.
reset_all
run_sessions reboot --dry-run
check "reboot --dry-run succeeds with no sessions" "0" "$RC"
check "and says so rather than printing an empty table" "yes" "$(said 'no sessions are running')"
check "and says it signalled and rebooted nothing" "yes" "$(said 'nothing was rebooted')"

# ── the audit, with sessions actually running ────────────────────────────────────────
#
# The part an operator uses before a reboot: what is running, and can each one be warned.

reset_all
make_project "$WORK/proj-a"
make_project "$WORK/proj-b"
tmux_session ccy-proj-a "$WORK/proj-a"
tmux_session ccy-proj-b "$WORK/proj-b"
run_sessions reboot --dry-run
check "every running session is listed" "yes" "$(said 'ccy-proj-a')"
check "including the second" "yes" "$(said 'ccy-proj-b')"
# Counted on the whole ROW, not the substring "ready": "NO DAEMON CLI" does not contain it today,
# but a wording change that made it "not ready" would have this assertion pass while counting the
# opposite of what it claims — which is the trap `enabled` vs `enabled-no-linger` set thirty
# lines from here.
check "each is marked ready when its project has the daemon CLI" "2" \
    "$(printf '%s' "$OUT" | grep -cE '^ +ccy-proj-[ab] +ready +')"
check "an all-ready audit succeeds" "0" "$RC"

# A project with no daemon CLI cannot be warned. The audit REFUSES rather than warning the
# others: a partial warning is worse than none, because the operator believes every session was
# told and reboots on that belief.
reset_all
make_project "$WORK/proj-ready"
make_project "$WORK/proj-nocli" without-cli
tmux_session ccy-ready "$WORK/proj-ready"
tmux_session ccy-nocli "$WORK/proj-nocli"
run_sessions reboot --dry-run
check "a project with no daemon CLI is named as such" "yes" "$(said 'NO DAEMON CLI')"
check "and the audit refuses rather than warning only some" "1" "$RC"
check "saying how many of how many could not be warned" "yes" "$(said '1 of 2')"

# The failure that must never read as "nothing is running": tmux itself failing. Reported as
# "no sessions", an operator would reboot believing the machine was idle.
reset_all
printf '1' >"$WORK/tmux-rc"
run_sessions reboot --dry-run
check "a tmux listing failure does not report an idle machine" "no" "$(said 'no sessions are running')"
check "it says what a reboot would interrupt is UNKNOWN" "yes" "$(said 'UNKNOWN')"
check "and says that is not the same as nothing running" "yes" "$(said 'not the same as nothing running')"
check "and exits non-zero" "1" "$RC"

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
# The help must state which half works and which does not. Asserted on both, because a help
# text that said neither would pass an assertion about only one of them.
check "and marks notify as working" "yes" "$(said 'WORKS')"
check "and marks reboot --in as not built" "yes" "$(said 'NOT BUILT')"
check "and names the kinds notify accepts" "yes" "$(said 'shutdown-warning')"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
