#!/usr/bin/env bash
# Unit-test reboot_guest and guest_prepare (files/home/.local/bin/vmtest, Plan 00109 Task 3.2).
#
# Extracts the two functions under test out of `vmtest` with awk into a temp file and
# sources that — `vmtest` calls main "$@" on load and would try to drive libvirt. Each
# function is bounded by its `<name>() {` line and the first `^}` after it; a refactor
# that moves one keeps working, one that renames it fails loudly at extraction.
#
# WHY THIS EXISTS. Whether a run reboots before it is judged is the scenario's answer,
# and the profile supplies only the mechanics. That split has a failure mode with no
# symptom: a profile nobody wrote mechanics for, or a fixture nobody deployed, would
# leave the guest un-rebooted and the checker judging the boot that provisioned it — a
# green transcript for a scenario that never happened. Both are refusals here, and a
# refusal is only worth having if something proves it refuses.
#
# The acceptance cases come FIRST, so every refusal below is a change from a known-good
# baseline rather than an assertion about a dispatcher that might refuse everything.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VMTEST="$REPO_ROOT/files/home/.local/bin/vmtest"

if [ ! -f "$VMTEST" ]; then
    echo "FAIL: vmtest not found at $VMTEST" >&2
    exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

extract() {
    # extract <function-name> <destination>
    awk -v fn="$1() {" '$0 == fn {p=1} p {print} p && /^\}/ {exit}' "$VMTEST" >>"$2"
    if ! grep -q "^$1() {" "$2"; then
        echo "FAIL: could not extract $1 from vmtest" >&2
        exit 1
    fi
}

FN="$work/fn.bash"
: >"$FN"
extract reboot_guest "$FN"
extract guest_prepare "$FN"

passed=0
failed=0

report() {
    # report <status> <name> [detail]
    local status="$1" name="$2" detail="${3:-}"
    if [ "$status" = pass ]; then
        passed=$((passed + 1))
        printf 'ok   %s\n' "$name"
    else
        failed=$((failed + 1))
        printf 'FAIL %s%s\n' "$name" "${detail:+ — $detail}" >&2
    fi
}

# The collaborators. `die` belongs to the CLI and EXITS; the stub must exit too, or the
# function would run on past its own refusal and every later step would be exercised in a
# state the real CLI can never reach. Exiting means each call runs in a subshell, so what
# happened travels back through files rather than variables.
STUB_PRELUDE=$(
    cat <<'STUB'
die() { printf 'DIE %s' "$*" >"$TRACE_DIR/die"; exit 1; }
log() { :; }
desktop_reboot_into_session() { printf 'desktop %s\n' "$*" >>"$TRACE_DIR/trace"; }
server_reboot() { printf 'server %s\n' "$*" >>"$TRACE_DIR/trace"; }
guest_scp() { printf 'scp %s\n' "$*" >>"$TRACE_DIR/trace"; }
timeout() { printf 'ssh-prepare\n' >>"$TRACE_DIR/trace"; return "${STUB_PREPARE_RC:-0}"; }
VMTEST_HOME="$STUB_LAB_DIR"
PREPARE_TIMEOUT_SECONDS=1
SSH_KEY=/dev/null
GUEST_PORT=0
CLOUD_USER=stub
STUB
)

# run_case <profile> <command…> — the function under test, with its exit status, its
# refusal message and what it called all recovered.
TRACE=""
DIE_MESSAGE=""
run_case() {
    local profile="$1"
    shift
    rm -rf "$work/trace"
    mkdir -p "$work/trace"
    (
        export TRACE_DIR="$work/trace"
        export STUB_LAB_DIR="$work/lab"
        eval "$STUB_PRELUDE"
        # Exported because the function under test reads it from the shell it is
        # sourced into, exactly as it does inside the real `run`.
        export BASE_PROFILE="$profile"
        # shellcheck source=/dev/null
        source "$FN"
        "$@"
    ) >"$work/out" 2>&1
    local rc=$?
    # Either file is absent when nothing wrote it — no collaborator called, or no
    # refusal — and that absence is the observation, not a read that went wrong.
    TRACE=""
    if [ -r "$work/trace/trace" ]; then
        TRACE="$(cat "$work/trace/trace")"
    fi
    DIE_MESSAGE=""
    if [ -r "$work/trace/die" ]; then
        DIE_MESSAGE="$(cat "$work/trace/die")"
    fi
    return "$rc"
}

mkdir -p "$work/lab"
TRANSCRIPT="$work/transcript.log"
: >"$TRANSCRIPT"

# ── 1. a server scenario reboots, by the server's mechanics ───────────────────────────
if run_case server reboot_guest "$work/base" "$TRANSCRIPT" && [[ "$TRACE" == server* ]]; then
    report pass server-profile-uses-the-server-reboot
else
    report fail server-profile-uses-the-server-reboot "trace: ${TRACE:-none}"
fi

# ── 2. a desktop scenario reboots into its session ────────────────────────────────────
if run_case desktop reboot_guest "$work/base" "$TRANSCRIPT" && [[ "$TRACE" == desktop* ]]; then
    report pass desktop-profile-reboots-into-the-session
else
    report fail desktop-profile-reboots-into-the-session "trace: ${TRACE:-none}"
fi

# ── 3. a profile with no mechanics REFUSES ────────────────────────────────────────────
# The one that has no symptom otherwise: not rebooting looks exactly like rebooting to
# everything downstream, and the checker would judge the boot that provisioned the guest.
if run_case appliance reboot_guest "$work/base" "$TRANSCRIPT"; then
    report fail unknown-profile-refuses "it returned success instead of refusing"
elif [[ "$DIE_MESSAGE" == *"no reboot mechanics for profile appliance"* ]]; then
    report pass unknown-profile-refuses
else
    report fail unknown-profile-refuses "refused without naming the profile: ${DIE_MESSAGE:-none}"
fi

# ── 4. a scenario with no fixture proceeds ────────────────────────────────────────────
# Absence is not an error: a fixture is particular to one scenario and most have none.
if run_case server guest_prepare no-fixture-here abc123 "$TRANSCRIPT" && [ -z "$TRACE" ]; then
    report pass a-scenario-without-a-fixture-runs-nothing
else
    report fail a-scenario-without-a-fixture-runs-nothing "trace: ${TRACE:-none}"
fi

# ── 5. a scenario WITH a fixture runs it in the guest ─────────────────────────────────
printf '#!/usr/bin/bash\n' >"$work/lab/guest-prepare-has-fixture.bash"
if run_case server guest_prepare has-fixture abc123 "$TRANSCRIPT" &&
    [[ "$TRACE" == *scp* && "$TRACE" == *ssh-prepare* ]]; then
    report pass a-scenario-with-a-fixture-runs-it
else
    report fail a-scenario-with-a-fixture-runs-it "trace: ${TRACE:-none}"
fi

# ── 6. a fixture that fails ABORTS the run ────────────────────────────────────────────
# A half-applied fixture leaves the checker judging a guest nobody set up, and its
# failures would read as defects in the code under test rather than in the fixture.
if STUB_PREPARE_RC=7 run_case server guest_prepare has-fixture abc123 "$TRANSCRIPT"; then
    report fail a-failed-fixture-aborts "it returned success after the fixture failed"
elif [[ "$DIE_MESSAGE" == *"has-fixture fixture exited 7"* ]]; then
    report pass a-failed-fixture-aborts
else
    report fail a-failed-fixture-aborts "aborted without naming the exit status: ${DIE_MESSAGE:-none}"
fi

printf 'passed: %d\n' "$passed"
if [ "$failed" -gt 0 ]; then
    printf 'failed: %d\n' "$failed" >&2
    exit 1
fi
exit 0
