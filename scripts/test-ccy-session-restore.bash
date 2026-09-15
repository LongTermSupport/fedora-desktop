#!/usr/bin/env bash
# Unit-test ccy-sessions-restore (files/home/.local/bin/ccy-sessions-restore).
#
# WHY THIS EXISTS. This is the least observable code in the repository: it runs once at boot,
# from a systemd --user unit, with nobody watching, and what it does is START AI AGENT SESSIONS.
# Every decision it takes is therefore a decision nobody will review in the moment, and the two
# ways it can be wrong are both silent:
#
#   - it restores something it should not — a session that is already live, a directory that
#     now holds a different repository, a one-off the operator marked no-restore; or
#   - it drops something without saying why, leaving an operator with a machine that brought
#     back three of four sessions and no way to learn which one, or what happened to it.
#
# So the retirement tree is driven here through EVERY branch, against the real script, with the
# real registry library — and each case asserts BOTH that the right thing happened and that the
# reason was recorded. `--dry-run` is used throughout: it exercises the same decisions and stops
# short of `systemd-run`, which cannot run in a container and would be the only part of the
# script a test could not reach anyway.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CCY_DIR="$REPO_ROOT/files/var/local/claude-yolo"
RESTORE="$REPO_ROOT/files/home/.local/bin/ccy-sessions-restore"
LIB_DIR="$CCY_DIR/lib"

for required in "$RESTORE" "$LIB_DIR/session-registry.bash"; do
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

# A stub PATH, so the script's own tool preflight passes without tmux or systemd-run being
# installed here. Nothing invokes them: every case runs --dry-run, which decides and reports
# but starts nothing.
STUB_BIN="$WORK/bin"
STUB_LOG="$WORK/invocations"
STUB_TMUX_STATE="$WORK/tmux-sessions"
mkdir -p "$STUB_BIN"

# The stubs record their argv AND keep state, because two of the things this script has to get
# right are invisible to a stub that merely exits 0:
#
#   - CCY_UNATTENDED must reach the SESSION, not the tmux client;
#   - the transient scope must be named per session, which only matters once a project has two.
#
# The tmux stub therefore remembers the sessions it creates and lists them back, so
# `ccy_tmux_next_name` has to actually avoid a collision rather than being handed an empty
# server every time. The systemd-run stub execs the command after `--`, so the two chain the way
# they do in production.
cat >"$STUB_BIN/tmux" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >>"$STUB_LOG"
case " \$* " in
*" list-sessions "*)
    if [ -f "$STUB_TMUX_STATE" ]; then cat "$STUB_TMUX_STATE"; fi
    exit 0
    ;;
esac
name=""
dir=""
while [ \$# -gt 0 ]; do
    case "\$1" in
    -s) shift; name="\$1" ;;
    -c) shift; dir="\$1" ;;
    esac
    shift
done
if [ -n "\$name" ]; then printf '%s 0 %s\n' "\$name" "\$dir" >>"$STUB_TMUX_STATE"; fi
exit 0
STUB

cat >"$STUB_BIN/systemd-run" <<STUB
#!/bin/sh
printf '%s\n' "\$*" >>"$STUB_LOG"
while [ \$# -gt 0 ]; do
    if [ "\$1" = "--" ]; then shift; break; fi
    shift
done
if [ \$# -gt 0 ]; then exec "\$@"; fi
exit 0
STUB

chmod +x "$STUB_BIN/tmux" "$STUB_BIN/systemd-run"
printf '#!/bin/sh\nexit 0\n' >"$WORK/fake-launcher"
chmod +x "$WORK/fake-launcher"

STATE="$WORK/state"

# run_restore [args...] — the real script, against a scratch registry. Both streams are
# captured together: a decision's REASON goes to stdout while an error goes to stderr, and a
# case that fails for an unexpected reason must be able to say so.
RESTORE_OUT=""
RESTORE_RC=0
# RUN_MAX_AGE / RUN_EVIDENCE override the retention settings for a case. Left empty they are not
# passed at all, so the script's own defaults apply.
RUN_MAX_AGE=""
RUN_EVIDENCE=""
run_restore() {
    # Through `env`, and built as an array. A `${VAR:+NAME=value}` written in an assignment
    # PREFIX is not an assignment: bash decides what is a prefix before expanding, so the word
    # became a command and every such case returned 127 instead of testing anything.
    local -a extra_env=()
    if [ -n "$RUN_MAX_AGE" ]; then
        extra_env+=("CCY_RESTORE_MAX_AGE_DAYS=$RUN_MAX_AGE")
    fi
    if [ -n "$RUN_EVIDENCE" ]; then
        extra_env+=("CCY_RESTORE_EVIDENCE_DAYS=$RUN_EVIDENCE")
    fi
    RESTORE_OUT="$(
        env PATH="$STUB_BIN:$PATH" \
            XDG_STATE_HOME="$STATE" \
            CCY_LIB="$LIB_DIR" \
            CCY_LAUNCHER="$WORK/fake-launcher" \
            HOME="$WORK" \
            "${extra_env[@]}" \
            bash "$RESTORE" "$@" 2>&1
    )"
    RESTORE_RC=$?
}

SESSIONS="$STATE/ccy/sessions"

reset_registry() {
    rm -rf "$STATE"
    mkdir -p "$SESSIONS"
    : >"$STUB_LOG"
    : >"$STUB_TMUX_STATE"
}

# make_repo <path> [commit-message] — a real git repository, because the fingerprint and the
# work-tree probe both shell out to git and a fake directory would not exercise either.
make_repo() {
    local path="$1" message="${2:-first}"
    mkdir -p "$path"
    git -C "$path" init -q
    git -C "$path" -c user.email=test@example.com -c user.name=test \
        commit -q --allow-empty -m "$message"
}

# record_for <name> <dir> [extra fields...] — a survivor record: a boot id that is NOT this
# boot, so the script treats it as something the reboot interrupted.
# A given field REPLACES its default rather than being appended after it: the library refuses a
# repeated key outright, so appending would make the writer fail instead of producing the record
# the case meant to describe.
record_for() {
    local name="$1" dir="$2"
    shift 2
    local commit="no-commits"
    if [ -d "$dir/.git" ]; then
        commit="$(ccy_registry_fingerprint "$dir")"
    fi
    local -a fields=(
        "project_dir=$dir"
        "project_name=$(basename "$dir")"
        "root_commit=$commit"
        "boot_id=a-previous-boot"
        # A boot that started shortly before this one — i.e. the reboot that just happened.
        # Staleness is measured boot-to-boot, so this is what makes the record a normal
        # survivor rather than an ancient one.
        "boot_time=$(($(ccy_registry_boot_time) - 60))"
        "restore=yes"
    )
    local given key i replaced
    for given in "$@"; do
        key="${given%%=*}"
        replaced=no
        for i in "${!fields[@]}"; do
            if [ "${fields[$i]%%=*}" = "$key" ]; then
                fields[i]="$given"
                replaced=yes
            fi
        done
        [ "$replaced" = no ] && fields+=("$given")
    done
    ccy_registry_write "$SESSIONS" "$name" "${fields[@]}"
}

# verdict_for <record-name> — the verdict the script settled on for that record.
#
# The LAST line for the record, not the first: a record can draw a `note` on the way to its
# verdict (an unknown age, say), and taking the first line would report the note as the outcome.
verdict_for() {
    printf '%s' "$RESTORE_OUT" | awk -v want="$1.record" '$2 == want { last = $1 } END { print last }'
}

# ── the empty case, which must not be confused with anything else ────────────────────
reset_registry
run_restore
check "an empty registry exits 0" "0" "$RESTORE_RC"
check "an empty registry says so in its own words" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'nothing to restore' && echo yes || echo no)"

# ── the ordinary restore ─────────────────────────────────────────────────────────────
reset_registry
make_repo "$WORK/alpha"
record_for ccy-alpha "$WORK/alpha" "token_name=work"
run_restore --dry-run
check "a healthy survivor would be started" "would-start" "$(verdict_for ccy-alpha)"
check "the restore carries --supervise" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q -- '--supervise' && echo yes || echo no)"
check "the restore carries --continue" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q -- '--continue' && echo yes || echo no)"
check "the recorded token is passed back" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q -- '--token work' && echo yes || echo no)"
check "a healthy run exits 0" "0" "$RESTORE_RC"
check "a dry run consumes nothing" "yes" \
    "$([ -f "$SESSIONS/ccy-alpha.record" ] && echo yes || echo no)"

# ── D1: a record from the CURRENT boot is a LIVE session, not a survivor ─────────────
#
# The one that would duplicate a running claude onto a live conversation. It has to be
# distinguishable from a survivor by the record alone, because at boot there is nothing else to
# ask.
reset_registry
make_repo "$WORK/live"
ccy_registry_write "$SESSIONS" ccy-live \
    "project_dir=$WORK/live" "project_name=live" \
    "boot_id=$(ccy_registry_boot_id)" "restore=yes"
run_restore --dry-run
check "a record from THIS boot is treated as live" "live" "$(verdict_for ccy-live)"
check "a live record does not fail the run" "0" "$RESTORE_RC"

# ── D4: every retirement, and every one of them gives its reason ─────────────────────

# no-restore: the operator said so at launch.
reset_registry
make_repo "$WORK/oneoff"
record_for ccy-oneoff "$WORK/oneoff" "restore=no"
run_restore --dry-run
check "a --no-restore session is retired" "would-retire" "$(verdict_for ccy-oneoff)"
check "the no-restore reason is named" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'no-restore' && echo yes || echo no)"

# directory-gone: the project was moved or deleted while the machine was down.
reset_registry
make_repo "$WORK/vanishing"
record_for ccy-vanishing "$WORK/vanishing"
rm -rf "$WORK/vanishing"
run_restore --dry-run
check "a vanished project directory is retired" "would-retire" "$(verdict_for ccy-vanishing)"
check "the directory-gone reason is named" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'directory-gone' && echo yes || echo no)"

# not-a-git-checkout: ccy refuses to run outside a repository, so this session could not start.
# Caught here WITH a reason rather than as a session that dies a second after boot.
reset_registry
mkdir -p "$WORK/plain"
record_for ccy-plain "$WORK/plain"
run_restore --dry-run
check "a directory that is not a git checkout is retired" "would-retire" "$(verdict_for ccy-plain)"
check "the not-a-git-checkout reason is named" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'not-a-git-checkout' && echo yes || echo no)"

# D7 different-project: the directory was reused for another repository. Restoring would have
# --continue resume the PREVIOUS project's conversation inside the new one.
reset_registry
make_repo "$WORK/reused" "original project"
record_for ccy-reused "$WORK/reused"
rm -rf "$WORK/reused"
make_repo "$WORK/reused" "an entirely different project"
run_restore --dry-run
check "a reused directory is retired" "would-retire" "$(verdict_for ccy-reused)"
check "the different-project reason is named" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'different-project' && echo yes || echo no)"

# The other side of D7: the SAME repository with new commits is not a different project. A
# fingerprint that moved with every commit would retire every session on every reboot.
reset_registry
make_repo "$WORK/moving"
record_for ccy-moving "$WORK/moving"
git -C "$WORK/moving" -c user.email=test@example.com -c user.name=test \
    commit -q --allow-empty -m "work done since the reboot"
run_restore --dry-run
check "new commits do NOT make it a different project" "would-start" "$(verdict_for ccy-moving)"

# And the no-commits sentinel: a project that had no commits when recorded, and has one now, is
# still the same project.
reset_registry
mkdir -p "$WORK/fresh"
git -C "$WORK/fresh" init -q
record_for ccy-fresh "$WORK/fresh"
git -C "$WORK/fresh" -c user.email=test@example.com -c user.name=test \
    commit -q --allow-empty -m "its first commit"
run_restore --dry-run
check "a project's first commit does not retire it" "would-start" "$(verdict_for ccy-fresh)"

# stale: a record whose BOOT was long ago — most likely because restore was enabled well after
# the session ran. Restoring a months-old session unasked is a surprise.
reset_registry
make_repo "$WORK/ancient"
record_for ccy-ancient "$WORK/ancient" \
    "boot_time=$(($(ccy_registry_boot_time) - 30 * 86400))"
run_restore --dry-run
check "a record from a long-ago boot is retired as stale" "would-retire" "$(verdict_for ccy-ancient)"
check "the stale reason is named" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'stale' && echo yes || echo no)"

# THE CASE THIS FEATURE EXISTS FOR, and the one the first implementation got backwards.
#
# Age was measured from the record's mtime — which is when the SESSION STARTED. A session
# running permanently for a fortnight was therefore retired as "stale" at the very reboot it was
# meant to survive. Measured boot-to-boot, a long-running session whose boot ended moments ago
# is an ordinary survivor, whatever its own age.
reset_registry
make_repo "$WORK/longrunner"
record_for ccy-longrunner "$WORK/longrunner"
touch -d '30 days ago' "$SESSIONS/ccy-longrunner.record"
run_restore --dry-run
check "a session running for weeks is still restored" "would-start" "$(verdict_for ccy-longrunner)"

# A record with no boot_time cannot have its age established. It is RESTORED with the age
# reported as unknown, not retired: silently dropping a session because its age is unknowable is
# the "could not tell" collapse, and the safe direction here does not lose work.
reset_registry
make_repo "$WORK/noboottime"
record_for ccy-noboottime "$WORK/noboottime" "boot_time="
run_restore --dry-run
check "a record with no boot_time is still restored" "would-start" "$(verdict_for ccy-noboottime)"
check "and its unknown age is reported" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'age is unknown' && echo yes || echo no)"

# ── D3: a malformed record is QUARANTINED and FAILS the run ──────────────────────────
#
# The distinction that matters: a deliberate retirement is a resolved outcome and the run
# succeeds, while a record that cannot be understood is "could not tell" and must not exit 0.
reset_registry
make_repo "$WORK/healthy"
record_for ccy-healthy "$WORK/healthy"
printf 'schema=1\nproject_dir=/x\nboot_id=b\nrestore=yes\n' >"$SESSIONS/ccy-broken.record"
run_restore --dry-run
check "a malformed record is quarantined" "would-quarantine" "$(verdict_for ccy-broken)"
check "a malformed record makes the run FAIL" "1" "$RESTORE_RC"
# ...and it must not stop the healthy ones being dealt with. Aborting on the first bad record
# would leave every other session unrestored, which is the wrong trade at boot.
check "a healthy record beside it is still processed" "would-start" "$(verdict_for ccy-healthy)"

# ── D2: the record is CONSUMED before the session starts ─────────────────────────────
#
# This is what makes a restore loop impossible: after the move there is nothing left to retry,
# so a session that crashes on startup is not restored again and again. Run for real (not
# --dry-run) so the consumption actually happens; the stub systemd-run stands in for the start.
reset_registry
make_repo "$WORK/consumed"
record_for ccy-consumed "$WORK/consumed"
run_restore
check "a started record leaves the live set" "no" \
    "$([ -f "$SESSIONS/ccy-consumed.record" ] && echo yes || echo no)"
check "it is kept as evidence under attempted/" "yes" \
    "$([ -f "$STATE/ccy/restore/attempted/ccy-consumed.record" ] && echo yes || echo no)"
check "the evidence records that a restore was attempted" "restore-attempted" \
    "$(ccy_registry_field_default "$STATE/ccy/restore/attempted/ccy-consumed.record" retired_reason missing)"
# Running again must find nothing: that IS the no-loop property, stated as a test.
run_restore
check "a second run has nothing left to restore" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'nothing to restore' && echo yes || echo no)"

# A real run leaves a note, so restore-status can tell "it ran and did nothing" from "it never
# ran" — which are different answers about a machine.
check "a real run records its outcome" "yes" \
    "$([ -f "$STATE/ccy/restore/last-run" ] && echo yes || echo no)"

# ── what the START COMMAND actually contains ─────────────────────────────────────────
#
# Two properties live only in the constructed command, so they are asserted against what the
# recording stub was handed. A stub that merely exited 0 vouched for neither.
started_cmd="$(cat "$STUB_LOG")"

# CCY_UNATTENDED must reach the SESSION. Handed to systemd-run with --setenv it reaches the tmux
# CLIENT — and when a server is already running, the client only asks that server to create a
# session and the new pane inherits the SERVER's environment. The variable would have arrived
# for the first restored session and quietly not for any after it, which is the worst case:
# those sessions park on a prompt while appearing restored.
check "the environment is delivered inside the tmux command" "yes" \
    "$(printf '%s' "$started_cmd" | grep -q 'env CCY_UNATTENDED=1' && echo yes || echo "no: $started_cmd")"
check "it is NOT handed only to the tmux client via --setenv" "no" \
    "$(printf '%s' "$started_cmd" | grep -q -- '--setenv' && echo yes || echo no)"

# The transient scope is named per SESSION, not per project directory. Keyed on the directory,
# two sessions in one project collided: the second systemd-run failed with a duplicate unit
# name, and its record had already been consumed — so that session was lost.
check "the scope unit is named after the session" "yes" \
    "$(printf '%s' "$started_cmd" | grep -q 'ccy-tmux-restore-ccy-consumed' && echo yes || echo "no: $started_cmd")"

# Driven for real: two survivors in ONE project must both get through, with distinct unit names.
reset_registry
make_repo "$WORK/twin"
record_for ccy-twin "$WORK/twin"
record_for ccy-twin-2 "$WORK/twin"
run_restore
# `^restored ` with the trailing space: the run's own summary line begins "restored:" and would
# otherwise be counted as a third restored session.
check "two sessions in one project are both restored" "2" \
    "$(printf '%s' "$RESTORE_OUT" | grep -cE '^restored +ccy-')"
check "and they get distinct scope unit names" "2" \
    "$(grep -oE 'ccy-tmux-restore-[a-z0-9-]+' "$STUB_LOG" | sort -u | wc -l)"
check "neither is left behind in the live set" "0" \
    "$(find "$SESSIONS" -maxdepth 1 -name '*.record' | wc -l)"

# A retirement, for real, is filed with its reason where restore-status will find it.
reset_registry
mkdir -p "$WORK/notrepo"
record_for ccy-filed "$WORK/notrepo"
run_restore
check "a retired record is filed under retired/" "yes" \
    "$([ -f "$STATE/ccy/restore/retired/ccy-filed.record" ] && echo yes || echo no)"
check "the filed record carries its reason" "not-a-git-checkout" \
    "$(ccy_registry_field_default "$STATE/ccy/restore/retired/ccy-filed.record" retired_reason missing)"
check "a retirement alone does NOT fail the run" "0" "$RESTORE_RC"

# ── the retention settings are validated BEFORE any record is touched ────────────────
#
# Both reach arithmetic deep inside the loop. A mistyped one — set in the unit, or exported for a
# dry run — used to kill the service mid-record: no verdict for the record in hand, no summary,
# no last-run note, and every remaining session left unrestored. A configuration mistake must
# fail before the work starts, and it must be visible that nothing was touched.
reset_registry
make_repo "$WORK/guarded"
record_for ccy-guarded "$WORK/guarded"
RUN_MAX_AGE="notanumber"
run_restore
RUN_MAX_AGE=""
check "a non-numeric max-age is refused at startup" "2" "$RESTORE_RC"
check "and the refusal names the variable" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'CCY_RESTORE_MAX_AGE_DAYS' && echo yes || echo no)"
check "and says nothing was touched" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -qi 'nothing was read' && echo yes || echo no)"
check "and the record is left exactly where it was" "yes" \
    "$([ -f "$SESSIONS/ccy-guarded.record" ] && echo yes || echo no)"
# It fails BEFORE the loop, so no record can have been processed on the way to the refusal.
check "no record was processed before the refusal" "no" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'ccy-guarded.record' && echo yes || echo no)"

RUN_EVIDENCE="7d"
run_restore
RUN_EVIDENCE=""
check "a non-numeric evidence-age is refused too" "2" "$RESTORE_RC"
check "and that refusal names its own variable" "yes" \
    "$(printf '%s' "$RESTORE_OUT" | grep -q 'CCY_RESTORE_EVIDENCE_DAYS' && echo yes || echo no)"

# A negative number is not a whole number of days either, and would sail through a bare
# arithmetic check.
RUN_MAX_AGE="-1"
run_restore
RUN_MAX_AGE=""
check "a negative max-age is refused" "2" "$RESTORE_RC"

# And the ordinary case still works, or every assertion above proves only that the guard fires.
RUN_MAX_AGE="14"
run_restore --dry-run
RUN_MAX_AGE=""
check "a valid max-age is accepted" "0" "$RESTORE_RC"
check "and the record is still processed" "would-start" "$(verdict_for ccy-guarded)"

# ── the environment refusals ─────────────────────────────────────────────────────────
check "--help works and exits 0" "0" \
    "$(PATH="$STUB_BIN:$PATH" bash "$RESTORE" --help >/dev/null; echo $?)"
run_restore --nonsense
check "an unknown option is refused with the usage code" "64" "$RESTORE_RC"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
