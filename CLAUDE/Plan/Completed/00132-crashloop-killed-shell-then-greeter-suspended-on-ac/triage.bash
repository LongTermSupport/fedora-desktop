#!/usr/bin/env bash
# Plan 00132 triage — re-derive every fact this plan asserts.
#
# Run on the HOST — enforced by plan_require_host (CLAUDE/PlanScriptStandards.md
# R2). The greeter's dconf scope, the session bus, the user systemd manager and
# the container engine are all host facts; asked from inside a CCY container
# every one of them answers about the wrong machine rather than failing.
#
# READ-ONLY. It queries, reads and inspects. It starts, stops, creates, removes
# and configures NOTHING. It does not stop the crash loop — see P8.
#
# Its whole stdout IS the report, and plan_start_log auto (R4) tees it to a
# per-run directory under untracked/plan-runs/. UNSCRUBBED — it names containers
# and a hostname, so never paste it into an issue, a PR or a tracked file.
#
# Probes map to PLAN.md tasks and to the supporting documents:
#   P1  1.1       the host did NOT reboot; boot id, suspend and resume times
#   P2  1.2       the first failure, and that the SEGV is DOWNSTREAM of it
#   P3  1.3/2.1   the greeter's effective power policy and the dconf stack that supplies it
#   P4  1.3       the human user's power policy, for the contrast
#   P5  2.3       --max-bytes is NOT derived from the session.conf XML limits
#   P6  2.4       D-Bus accounting: why there is no headroom figure to threshold on
#   P7  1.4       podman restart policy semantics and .scope registration
#   P8  1.5/3.3   the crash loop: RestartCount separation, and whether it is still live
set -euo pipefail
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
  if [[ -e "${repoRoot}/.git" ]]; then
    printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
    exit 1
  fi
  repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

plan_mode gather

usage() {
    cat << 'EOF'
Plan 00132 — crash loop killed the shell, then the greeter suspended on AC (HOST ONLY)

Usage: triage.bash [--since '<journal time>'] [--help]

  --since   Window for the incident probes P1 and P2. Defaults to the recorded
            incident window. Use it to re-point them at a later recurrence.
  --help    Show this help and exit.

Wholly read-only: it never stops the crash loop, never writes a dconf file and
never runs a play. Stopping the loop is Task 5.5 and must come AFTER the
detection defence has been shown to fire against it.

Writes its report to a per-run directory under untracked/plan-runs/; the path is
printed at the start and end of the run.

PRIVACY: the report names containers and this host. untracked/ is gitignored for
exactly that reason — do not copy names out of it into a tracked file.
EOF
}

# The incident this plan was opened for. Anchors P1/P2 to evidence that is now
# historical, so the script keeps working as a regression check once the journal
# has rotated past it -- see the vacuous-pass guard in P2.
INCIDENT_SINCE='2026-09-17 05:50:00'
INCIDENT_UNTIL='2026-09-17 08:45:00'
SINCE="$INCIDENT_SINCE"
UNTIL="$INCIDENT_UNTIL"

# What P3 asserts the greeter's AC policy to be. Defaults to the PRE-fix value,
# so a clean run today is a run that reproduces the defect. After Task 5.3 ships
# the drop-in, re-run with --expect-greeter nothing: that inverts the assertion
# and turns this script into the regression test for the fix.
GREETER_EXPECT="'suspend'"

while [ "$#" -gt 0 ]; do
    case "$1" in
        -h | --help)
            usage
            exit 0
            ;;
        --since)
            if [ "$#" -lt 2 ]; then
                echo "ERROR: --since needs a value, e.g. --since '2 hours ago'" >&2
                exit 1
            fi
            SINCE="$2"
            UNTIL="now"
            shift 2
            continue
            ;;
        --expect-greeter)
            if [ "$#" -lt 2 ]; then
                echo "ERROR: --expect-greeter needs 'suspend' or 'nothing'" >&2
                exit 1
            fi
            case "$2" in
                suspend | nothing)
                    GREETER_EXPECT="'$2'"
                    ;;
                *)
                    echo "ERROR: --expect-greeter takes 'suspend' or 'nothing', got: $2" >&2
                    exit 1
                    ;;
            esac
            shift 2
            continue
            ;;
        *)
            echo "ERROR: unknown argument: $1" >&2
            echo "  Try: triage.bash --help" >&2
            exit 1
            ;;
    esac
done

plan_require_host "it reads the greeter's dconf scope, the session bus, the user systemd manager and the host container engine"

if ! command -v podman > /dev/null; then
    echo "ERROR: podman is not installed." >&2
    echo "  It is declared in playbooks/imports/play-podman.yml. Deploy it:" >&2
    echo "    ansible-playbook playbooks/imports/play-podman.yml" >&2
    echo "  Do NOT install it by hand." >&2
    exit 1
fi

# BEFORE plan_start_log, and the library enforces the ordering: P3 reads the
# greeter's dconf scope through `sudo -u gdm`, and a sudo prompt issued after the
# tee redirect is flooded and garbled (R3).
plan_prime_sudo

plan_start_log auto

# probe -- for questions where a non-zero exit is DATA, not a failure. Several
# probes here ask things whose answer IS "this command finds nothing": P3b's
# grep returns 1 today and that is the current, correct state.
probe() {
    local label="$1"
    shift
    local out rc
    if out="$("$@" 2>&1)"; then rc=0; else rc=$?; fi
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    return 0
}

# probe_required -- for questions where a non-zero exit means the FACT-FINDING
# WAS INCOMPLETE, not that the answer is no. Routed through plan_gather_leg so
# the failure lands in PLAN_FAILED_LEGS and plan_finish exits non-zero.
#
# Without this the script exited 0 whether it established everything or nothing:
# a run with an unreadable journal, a failed `ps` and a broken podman printed
# warnings and still claimed success. PlanScriptStandards R9 -- "a triage
# script's non-zero exit says the fact-finding was incomplete, which is
# precisely why it must still be non-zero rather than swallowed".
# emit -- print a captured probe block, and for a REQUIRED leg record the
# failure so plan_finish exits non-zero.
#
# The required legs call their function DIRECTLY at the call site and pass the
# result here, rather than handing a function name to a dispatcher. That is
# deliberate: an indirect `dispatch "$@"` hides the call from shellcheck, which
# then reports every probe function as never invoked -- and the remedy for that
# would be a suppression comment, which this project forbids outright.
emit() {
    local label="$1" rc="$2" out="$3" required="${4:-optional}"
    printf '### %s  (rc=%d)\n%s\n\n' "$label" "$rc" "${out:-(no output)}"
    if [ "$required" = 'required' ] && [ "$rc" -ne 0 ]; then
        printf '[WARN] required leg "%s" failed (continuing)\n' "$label" >&2
        PLAN_FAILED_LEGS="${PLAN_FAILED_LEGS}${PLAN_FAILED_LEGS:+ }${label}"
    fi
}

PROBE_OUT=""
PROBE_RC=0

# Coverage is stated as a NUMBER, not as a list that implies totality -- this
# repo's recurring "a partial result read as a complete one" defect. Update both
# halves together when a probe is added.
TASKS_COVERED=(1.1 1.2 1.3 1.4 1.5 2.1 2.2 2.3 2.4 3.3)
TASKS_NOT_COVERED='1.6 (this script itself); 3.1 3.2 3.4 3.5 (specifications, with nothing live to read)'

echo "================================================================"
echo "Plan 00132 triage"
echo "  incident window: ${SINCE}  ->  ${UNTIL}"
echo "  COVERAGE: ${#TASKS_COVERED[@]} task(s) probed: ${TASKS_COVERED[*]}"
echo "  NOT probed: ${TASKS_NOT_COVERED}"
echo "================================================================"
echo

# =============================================================================
# P1 -- Task 1.1. The reflex reading of this incident is "it rebooted".
# =============================================================================
echo "### READ THIS FOR: P1/Task 1.1 -- the host did NOT reboot"
echo "###   ONE boot id spanning the window is the whole proof. A reboot would"
echo "###   show two, and every 'the machine restarted overnight' explanation"
echo "###   dies on that single fact. The suspend/resume pair then shows what"
echo "###   DID happen in the gap."
probe "boot ids (expect exactly one covering the window)" \
    journalctl --list-boots --no-pager
probe "uptime (expect longer than the incident)" uptime --pretty
probe "suspend / resume events" \
    journalctl --no-pager --since "$SINCE" --until "$UNTIL" \
    --unit systemd-suspend.service -o short-precise

# =============================================================================
# P2 -- Task 1.2. The obvious reading of the journal is the WRONG one.
# =============================================================================
quota_then_shell() {
    local journal grep_rc matches

    # Split the read from the filter deliberately. Folded into one pipeline, a
    # journalctl that cannot read the user journal is indistinguishable from a
    # journal containing no quota breach -- and those two must never print the
    # same thing, because one of them is "you are fine" and the other is "this
    # probe did not run".
    if ! journal="$(journalctl --user --no-pager --since "$SINCE" --until "$UNTIL" \
        -o short-precise 2>&1)"; then
        echo "  ERROR: could not read the user journal:"
        printf '%s\n' "$journal"
        echo "  This probe did NOT run. Do not read it as an all-clear."
        return 1
    fi

    if matches="$(printf '%s\n' "$journal" |
        grep -E 'exceeded its|being disconnected|Shutting down GNOME Shell|status=11/SEGV')"; then
        grep_rc=0
    else
        grep_rc=$?
    fi

    if [ "$grep_rc" -eq 0 ]; then
        printf '%s\n' "$matches"
        return 0
    fi
    if [ "$grep_rc" -eq 1 ]; then
        echo "  NO MATCHES in this window."
        echo "  This is NOT evidence that the breach did not occur -- the user"
        echo "  journal may simply have rotated past ${SINCE}. Check the window"
        echo "  before reading this as a clean bill of health."
        return 0
    fi
    echo "  ERROR: grep failed (rc=${grep_rc}); this probe did not run."
    return 1
}

echo "### READ THIS FOR: P2/Task 1.2 -- ORDERING is the finding, not the SEGV"
echo "###   Expected order, and it matters to the millisecond:"
echo "###     1. dbus-broker: UID exceeded its 'bytes' quota"
echo "###     2. peers disconnected 'as it does not have the resources to"
echo "###        RECEIVE a signal it subscribed to'  <- victims are RECEIVERS"
echo "###     3. ~4ms later: Shutting down GNOME Shell"
echo "###     4. ~9s later:  status=11/SEGV"
echo "###   The SEGV is a teardown artefact of a shutdown already underway."
echo "###   Reading it as the cause inverts the entire causal chain."
if PROBE_OUT="$(quota_then_shell 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "quota breach, disconnects, shell shutdown and SEGV, in order" "$PROBE_RC" "$PROBE_OUT" required

# =============================================================================
# P3 -- Tasks 1.3 and 2.1. The gating fact for the greeter fix.
# =============================================================================
echo "### READ THIS FOR: P3/Tasks 1.3+2.1 -- the greeter's power policy"
echo "###   sleep-inactive-ac-type='suspend' with a 900s timeout is the defect:"
echo "###   the greeter idle-suspends a PLUGGED-IN machine, inverting this"
echo "###   repo's documented intent the moment the user session dies."
echo "###   After the Task 5.3 fix this MUST read 'nothing'. A gdm.d file that"
echo "###   exists while this still says 'suspend' is a FAILED fix, not an"
echo "###   applied one -- this probe is that read-back assertion."
greeter_ac_policy() {
    local actual

    if ! actual="$(sudo -u gdm env DCONF_PROFILE=gdm gsettings get \
        org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type 2>&1)"; then
        echo "  could not read the greeter's dconf scope:"
        printf '%s\n' "$actual"
        return 1
    fi

    printf 'expected : %s\n' "$GREETER_EXPECT"
    printf 'actual   : %s\n' "$actual"

    # A real comparison, not a print. Called "the read-back assertion" in three
    # places, so it had better assert: before the fix this must read 'suspend',
    # after it 'nothing'. A version that only printed the value would exit 0 on a
    # failed fix and look identical to a successful one.
    if [ "$actual" = "$GREETER_EXPECT" ]; then
        echo "ASSERTION PASSED"
        return 0
    fi
    echo "ASSERTION FAILED -- the greeter policy is not what was expected."
    if [ "$GREETER_EXPECT" = "'nothing'" ]; then
        echo "  The gdm.d drop-in has NOT taken effect. A file that exists while"
        echo "  this still reads 'suspend' is a FAILED fix, not an applied one."
        echo "  Did 'dconf update' run after the file was written?"
    else
        echo "  If this now reads 'nothing', the fix is already deployed --"
        echo "  re-run with --expect-greeter nothing."
    fi
    return 1
}

if PROBE_OUT="$(greeter_ac_policy 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "greeter sleep-inactive-ac-type read-back" "$PROBE_RC" "$PROBE_OUT" required
probe "greeter effective sleep-inactive-ac-timeout (900 = the observed delay)" \
    sudo -u gdm env DCONF_PROFILE=gdm gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-timeout

echo "### READ THIS FOR: P3b/Task 2.1 -- WHY a gdm.d drop-in is sufficient"
echo "###   The profile ships with the gdm package in /usr/share, NOT in /etc."
echo "###   It already stacks 'system-db:gdm' FIRST among the system databases,"
echo "###   so a gdm.d drop-in is read and outranks local, site, distro and the"
echo "###   greeter's own defaults. Nothing needs creating under /etc/dconf/profile."
echo "###   This plan initially recorded the opposite, from looking only in /etc."
probe "the shipped gdm dconf profile" cat /usr/share/dconf/profile/gdm
probe "gdm.d drop-in directory (empty until Task 5.3)" ls -la /etc/dconf/db/gdm.d/
probe "does any db set a power key? (rc=1 means no, which is the current state)" \
    grep -rn 'sleep-inactive' /etc/dconf/db/gdm.d/ /etc/dconf/db/distro.d/ /usr/share/gdm/greeter-dconf-defaults

greeter_user_db() {
    local db='/var/lib/gdm/.config/dconf/user'

    if ! sudo test -f "$db"; then
        echo "  $db is absent."
        echo "  => nothing outranks system-db:gdm, so no lock is needed."
        return 0
    fi

    printf '%s exists (%s bytes)\n' "$db" "$(sudo stat -c '%s' "$db")"
    # user-db:user is the ONLY database above system-db:gdm in the profile, so
    # this file is the sole thing that could override the drop-in. If it ever
    # gains a sleep-inactive key, Task 2.2's "no lock required" is falsified.
    if sudo strings "$db" | grep -q 'sleep-inactive'; then
        echo "  !! It contains a sleep-inactive key. It OUTRANKS system-db:gdm,"
        echo "     so Task 2.2's 'no lock required' is FALSIFIED -- a lock in"
        echo "     /etc/dconf/db/gdm.d/locks/ becomes load-bearing. Re-open it."
        return 1
    fi
    echo "  no sleep-inactive key in it : CONFIRMED"
    echo "  => the only database that outranks system-db:gdm does not compete"
    echo "     for this key, so a dconf lock would defend against a write that"
    echo "     nothing performs. Task 2.2 answered: no lock."
}

echo "### READ THIS FOR: P3c/Task 2.2 -- why NO dconf lock is required"
echo "###   user-db:user is the only database above system-db:gdm in the"
echo "###   profile. For the greeter that resolves to gdm's own dconf user db."
echo "###   This probe is the whole evidence base for omitting the lock, and"
echo "###   it is written to FAIL if that evidence ever stops holding."
if PROBE_OUT="$(greeter_user_db 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "greeter user-db does not compete for this key" "$PROBE_RC" "$PROBE_OUT" required

# =============================================================================
# P4 -- Task 1.3. The contrast that shows the policy is per-user, not per-host.
# =============================================================================
echo "### READ THIS FOR: P4/Task 1.3 -- the human user, for contrast"
echo "###   'nothing' here against 'suspend' in P3 IS the defect: the policy is"
echo "###   a property of one user's session, not of the host. Both plays apply"
echo "###   it become_user-scoped, so the unmanaged gdm account never receives"
echo "###   it and takes over the display whenever that session ends."
probe "user effective sleep-inactive-ac-type" \
    gsettings get org.gnome.settings-daemon.plugins.power sleep-inactive-ac-type

# =============================================================================
# P5 -- Task 2.3. Guards against an inert fix.
# =============================================================================
broker_max_bytes() {
    local args
    if ! args="$(ps -eo args=)"; then
        echo "  ERROR: could not list processes; this probe did not run."
        return 1
    fi
    # Matching on the broker's own argv prefix, which this function never has,
    # so the probe cannot count itself.
    printf '%s\n' "$args" |
        grep -E '^dbus-broker --log' |
        grep -oE -e '--max-bytes [0-9]+' |
        sort |
        uniq -c
}

echo "### READ THIS FOR: P5/Task 2.3 -- --max-bytes is NOT read from the XML"
echo "###   session.conf declares max_incoming_bytes / max_outgoing_bytes as"
echo "###   1000000000 (1e9). The brokers run with scope-dependent CONSTANTS:"
echo "###   the session bus at 1e14 and the system bus at 536870912. Neither"
echo "###   equals 1e9, and the two scopes differ while their config files agree."
echo "###   So editing session.conf would change a file, pass an assertion and"
echo "###   alter NOTHING. That is the inert fix this probe exists to prevent."
if PROBE_OUT="$(broker_max_bytes 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "every running broker's --max-bytes, counted" "$PROBE_RC" "$PROBE_OUT" required
probe "the byte limits session.conf actually declares" \
    grep -E 'max_(incoming|outgoing)_bytes' /usr/share/dbus-1/session.conf

# =============================================================================
# P6 -- Task 2.4. Why the class-level defence is unavailable.
# =============================================================================
echo "### READ THIS FOR: P6/Task 2.4 -- there is no headroom ratio to threshold"
echo "###   PeerAccounting reports each peer's USAGE but not the limit that"
echo "###   usage is charged against. Against a 1e14 ceiling the ratio is"
echo "###   permanently near zero, and the limit that actually broke is a"
echo "###   per-peer receive share the broker derives internally and exposes"
echo "###   through no flag, file or bus method. Quota-headroom monitoring was"
echo "###   REJECTED on this evidence, not on preference -- see detection-gap.md."
accounting_keys() {
    local stats keys

    # CALL the method, do not introspect for it. The first version of this probe
    # ran `gdbus introspect --only-properties`, which cannot list an interface's
    # METHODS -- and Debug.Stats is a method interface. It returned rc=0 and 328
    # bytes mentioning neither Stats nor PeerAccounting, so it neither confirmed
    # nor denied the claim it was named after, while looking green.
    if ! stats="$(gdbus call --session --dest org.freedesktop.DBus \
        --object-path /org/freedesktop/DBus \
        --method org.freedesktop.DBus.Debug.Stats.GetStats 2>&1)"; then
        echo "  Debug.Stats.GetStats FAILED -- the interface is not callable here:"
        printf '%s\n' "$stats"
        return 1
    fi

    printf 'GetStats returned %s bytes\n' "${#stats}"

    if printf '%s' "$stats" | grep -q 'PeerAccounting'; then
        echo "PeerAccounting : PRESENT"
    else
        echo "PeerAccounting : ABSENT -- detection-gap.md's availability claim does not hold"
        return 1
    fi

    keys="$(printf '%s' "$stats" |
        grep -oE -e "'(Incoming|Outgoing)?(Bytes|Fds)'|'Matches'|'MatchBytes'" |
        sort -u |
        tr '\n' ' ')"
    printf 'accounting keys : %s\n' "$keys"

    # THE assertion Task 2.4 turns on. Usage is reported; the LIMIT that usage is
    # charged against is not, so the headroom ratio a monitor would need cannot
    # be computed from this interface. If a limit-shaped key ever appears here,
    # that conclusion is falsified and quota-headroom monitoring is back on.
    if printf '%s' "$stats" | grep -qiE "'(Max|Limit|Quota)[A-Za-z]*'"; then
        echo "  !! A limit-shaped key IS present -- Task 2.4's rejection of"
        echo "     quota-headroom monitoring is FALSIFIED. Re-open it."
        return 1
    fi
    echo "no limit/max/quota key : CONFIRMED"
    echo "  => usage is observable, the limit it is charged against is not, so"
    echo "     there is no headroom ratio to threshold on. This is why Task 2.4"
    echo "     rejected quota-headroom monitoring and adopted the restart proxy."
}

if PROBE_OUT="$(accounting_keys 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "D-Bus accounting: usage present, limit absent" "$PROBE_RC" "$PROBE_OUT" required

# =============================================================================
# P7 -- Task 1.4. Why systemd's start-rate limiter never applied.
# =============================================================================
echo "### READ THIS FOR: P7/Task 1.4 -- why nothing rate-limited the loop"
echo "###   Containers register as libpod-<id>.scope. systemd.scope(5): 'Unlike"
echo "###   service units, scope units manage externally created processes' --"
echo "###   so Restart=, StartLimitBurst= and StartLimitIntervalSec= do not"
echo "###   apply. podman supplies no backoff of its own either: 'backoff',"
echo "###   'restart-sec' and 'restart-delay' have ZERO matches in podman-run(1)."
echo "###   Nothing in the stack caps the rate. That is the defect."
probe "podman units under the user manager (nothing supervises the workload)" \
    systemctl --user list-units --no-pager --all 'podman*'
backoff_vocabulary() {
    local page='/usr/share/man/man1/podman-run.1.gz'
    local hits control

    # R11: a check whose tool or input is absent FAILS the leg, it does not skip.
    if [ ! -f "$page" ]; then
        echo "  ERROR: $page is absent; this probe did not run."
        echo "  podman-run(1) is shipped by the podman package (play-podman.yml)."
        return 1
    fi

    # zgrep, NOT grep. The page is gzipped, so a plain grep reads compressed
    # bytes and returns zero matches for EVERY pattern -- the right answer here
    # for entirely the wrong reason. The first version of this probe did exactly
    # that and looked like it confirmed the finding.
    if hits="$(zgrep -cE 'backoff|restart-sec|restart-delay' "$page")"; then
        :
    else
        hits=0
    fi

    # The control is what makes a zero meaningful. 'restart' is certainly in
    # podman-run(1), so a zero HERE would prove the probe is reading nothing,
    # and the finding above would be vacuous rather than established.
    if control="$(zgrep -cE 'restart' "$page")"; then
        :
    else
        control=0
    fi

    printf 'backoff|restart-sec|restart-delay : %s\n' "$hits"
    printf 'control, plain "restart"          : %s\n' "$control"
    if [ "$control" -eq 0 ]; then
        echo "  ERROR: the control found nothing either, so this probe is not"
        echo "  reading the man page at all. The zero above means NOTHING."
        return 1
    elif [ "$hits" -eq 0 ]; then
        echo "  ^ The page IS being read ($control hits for 'restart'), and the"
        echo "    backoff vocabulary is genuinely absent. podman offers no"
        echo "    restart backoff of any kind."
    fi
}

if PROBE_OUT="$(backoff_vocabulary 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "backoff vocabulary in podman-run(1), with a control" "$PROBE_RC" "$PROBE_OUT" required

# =============================================================================
# P8 -- Tasks 1.5 and 3.3. The detection signal, and the live test case.
# =============================================================================
restart_counts() {
    local ids id count name
    local -a rows=()

    if ! ids="$(podman ps -aq)"; then
        echo "  ERROR: could not list containers; this probe did not run."
        return 1
    fi

    while read -r id; do
        if [ -z "$id" ]; then
            continue
        fi
        if ! count="$(podman inspect --format '{{.RestartCount}}' "$id")"; then
            count="ERR"
        fi
        if ! name="$(podman inspect --format '{{.Name}}' "$id")"; then
            name="(name unreadable)"
        fi
        rows+=("$(printf '%12s  %s' "$count" "$name")")
    done <<< "$ids"

    if [ "${#rows[@]}" -eq 0 ]; then
        echo "  no containers at all -- nothing to measure"
        return 0
    fi
    printf '%s\n' "${rows[@]}" | sort -rn
}

libpod_start_rate() {
    local journal n

    if ! journal="$(journalctl --user --no-pager --since '120 seconds ago' -o cat)"; then
        echo "  ERROR: could not read the user journal; this probe did not run."
        echo "  Do NOT read this as 'no crash loop'."
        return 1
    fi

    # grep -c exits 1 on zero matches, which here is a real and welcome answer
    # rather than an error, so the count is taken from the matched line total.
    n="$(printf '%s\n' "$journal" | grep -c 'Started libpod' || :)"
    if [ -z "$n" ]; then
        n=0
    fi

    printf 'container starts in the last 120s: %s\n' "$n"
    if [ "$n" -ge 20 ]; then
        echo "  ^ A CRASH LOOP IS LIVE RIGHT NOW."
        echo "  Do NOT stop it before the detection probe has been shown to fire"
        echo "  against it (Task 5.2). It is the only unsimulated test case that"
        echo "  exists, and stopping the container destroys it."
    else
        echo "  ^ no storm in this sample"
    fi
}

echo "### READ THIS FOR: P8/Tasks 1.5+3.3 -- THE DETECTION SIGNAL"
echo "###   RestartCount separates a crash loop from a healthy host by four"
echo "###   orders of magnitude: when this plan was written the offender read"
echo "###   124873, the next-highest container 19, and every other one 0."
echo "###   This is the signal Task 5.1 builds the probe on. It needs no"
echo "###   careful tuning to discriminate -- healthy containers sit at zero."
echo "###   NOTE: names below are private identifiers. Read them here; do not"
echo "###   copy them into PLAN.md, a research doc, a journal or a commit."
if PROBE_OUT="$(restart_counts 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "RestartCount, descending" "$PROBE_RC" "$PROBE_OUT" required
probe "container status" podman ps -a --format '{{.Names}}\t{{.Status}}'
if PROBE_OUT="$(libpod_start_rate 2>&1)"; then PROBE_RC=0; else PROBE_RC=$?; fi
emit "is the loop live right now?" "$PROBE_RC" "$PROBE_OUT" required

echo
echo "================================================================"
echo "END OF REPORT."
echo
echo "Read P2 first -- the ordering is the finding, and the SEGV is a decoy."
echo "P3 is the read-back that proves or disproves the greeter fix."
echo "P8 tells you whether the live test case still exists. If it does, build"
echo "the detection probe BEFORE stopping it."
echo "================================================================"

# plan_finish exits non-zero if any gather leg failed, so this script's EXIT
# CODE agrees with its text. Anything after this call is dead code.
plan_finish
