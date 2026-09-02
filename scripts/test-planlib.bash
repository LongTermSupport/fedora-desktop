#!/usr/bin/env bash
# Unit tests for CLAUDE/Plan/_planlib.inc.bash — the plan-script helper library.
#
# No network, no ansible, no sudo password, no host mutation. Every assertion is pure logic, a
# temp-directory fixture, or a structural invariant read from the library's OWN source. The
# tee'd run log IS exercised for real, because a run log that silently truncates is the
# failure mode that matters most.
#
#   scripts/test-planlib.bash
#
# Exits non-zero if any assertion failed; CI-able and safe to run inside the CCY container.
#
# Style note 1: assertions that must observe a global the library sets run the library
# function IN THIS SHELL. Functions that legitimately `exit` (plan_deploy_leg, plan_finish,
# --help) are run in an explicit subshell — otherwise the test process would exit mid-suite
# and report success for everything it never reached.
#
# Style note 2: the banned-idiom needles are ASSEMBLED FROM FRAGMENTS. Written literally,
# this file would contain the very error-hiding patterns it exists to forbid.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly HERE
REPO_ROOT="$(cd "${HERE}/.." && pwd -P)"
readonly REPO_ROOT
LIB="${REPO_ROOT}/CLAUDE/Plan/_planlib.inc.bash"
readonly LIB

if [[ ! -e "${LIB}" ]]; then
    printf 'FATAL: library not found: %s\n' "${LIB}" >&2
    exit 1
fi

# shellcheck source-path=SCRIPTDIR
# shellcheck source=../CLAUDE/Plan/_planlib.inc.bash
source "${LIB}"

LIB_SRC="$(cat "${LIB}")"
readonly LIB_SRC

TMPROOT="$(mktemp -d)"
readonly TMPROOT
trap 'rm -rf "${TMPROOT}"' EXIT
CAPTURE_FILE="${TMPROOT}/capture.txt"
readonly CAPTURE_FILE

FAILED=0
pass() { printf 'PASS: %s\n' "$1"; }
fail() {
    printf 'FAIL: %s\n     expected: %s\n     actual:   %s\n' "$1" "$2" "$3"
    FAILED=1
}
# A check we could not run at all. Counted and reported distinctly, because "did not run" is
# not "passed" — carry the third state rather than rounding it up to green.
UNVERIFIED=0
unverified() {
    printf 'UNVERIFIED: %s (%s)\n' "$1" "$2"
    UNVERIFIED=$((UNVERIFIED + 1))
}

assert_eq() {
    local label="$1" want="$2" got="$3"
    if [[ "${want}" == "${got}" ]]; then pass "${label}"; else fail "${label}" "${want}" "${got}"; fi
}
assert_contains() {
    local label="$1" needle="$2" haystack="$3"
    if [[ "${haystack}" == *"${needle}"* ]]; then
        pass "${label}"
    else
        fail "${label}" "text containing '${needle}'" "${haystack}"
    fi
}
assert_not_contains() {
    local label="$1" needle="$2" haystack="$3"
    if [[ "${haystack}" != *"${needle}"* ]]; then
        pass "${label}"
    else
        # Report the OFFENDING LINES, not the whole haystack: dumping a whole file into a
        # failure message buries the one line that matters.
        local hits
        hits="$(printf '%s\n' "${haystack}" | grep -n -F -- "${needle}")"
        fail "${label}" "text WITHOUT '${needle}'" "found at: ${hits}"
    fi
}

# run_capture <cmd...> — run it IN THIS SHELL, combined output in OUT, status in RC.
#
# Output goes through a temp FILE rather than OUT="$(cmd 2>&1)" for two reasons, both learned
# by writing it the wrong way first in the sibling repo:
#   1. Command substitution forks a SUBSHELL, so every global the library sets is discarded
#      on return and assertions on them silently compare empty strings.
#   2. BASH_SUBSHELL is non-zero inside a command substitution, so plan_deploy_leg's misuse
#      guard fires and its `kill -TERM $$` takes down the TEST process partway through.
OUT=""
RC=0
run_capture() {
    "$@" >"${CAPTURE_FILE}" 2>&1
    RC=$?
    OUT="$(cat "${CAPTURE_FILE}")"
}

# make_repo <path> [--no-marker] [--no-git] — build a fake repo fixture.
make_repo() {
    local path="$1" marker=1 gitdir=1 arg
    shift
    for arg in "$@"; do
        case "${arg}" in
            --no-marker) marker=0 ;;
            --no-git) gitdir=0 ;;
            *)
                printf 'FATAL: make_repo got unknown arg %s\n' "${arg}" >&2
                exit 1
                ;;
        esac
    done
    mkdir -p "${path}/CLAUDE/Plan"
    if [[ "${marker}" -eq 1 ]]; then : >"${path}/ansible.cfg"; fi
    if [[ "${gitdir}" -eq 1 ]]; then mkdir -p "${path}/.git"; fi
}

# ── R1: repo-root resolution — the incident this library exists to prevent ────────────────
#
# Plan 00068's triage.bash used `git rev-parse --show-toplevel` (which THIS repo's
# PlanWorkflow.md recommended), and that is CWD-relative. Run by path from a different repo's
# root it resolved to that repo, wrote its report there, and the deployed-vs-checkout probe
# compared against a path that does not exist. These cases pin the replacement behaviour.

make_repo "${TMPROOT}/a/repo"
mkdir -p "${TMPROOT}/a/repo/CLAUDE/Plan/00001-x/deep/deeper"
run_capture _plan_find_repo_root "${TMPROOT}/a/repo/CLAUDE/Plan/00001-x/deep/deeper"
assert_eq "find_repo_root resolves the root from a deep plan subdir" "${TMPROOT}/a/repo" "${OUT}"

# THE case that matters most for THIS repo: it is routinely checked out INSIDE lts-infra at
# untracked/repos/fedora-desktop, and BOTH repos have an ansible.cfg at their root. An
# unbounded walk from a plan script here finds the OUTER repo's marker and appears to work.
make_repo "${TMPROOT}/b/outer"
make_repo "${TMPROOT}/b/outer/untracked/repos/fedora-desktop" --no-marker
mkdir -p "${TMPROOT}/b/outer/untracked/repos/fedora-desktop/CLAUDE/Plan/00066-y"
run_capture _plan_find_repo_root "${TMPROOT}/b/outer/untracked/repos/fedora-desktop/CLAUDE/Plan/00066-y"
assert_eq "a nested checkout with no marker FAILS rather than escaping to the outer repo" "1" "${RC}"
assert_not_contains "the outer repo path is never returned" "${TMPROOT}/b/outer" "${OUT}"

# Same nesting, inner repo has its own marker (the real-world shape): it must win.
make_repo "${TMPROOT}/c/outer"
make_repo "${TMPROOT}/c/outer/untracked/repos/fedora-desktop"
mkdir -p "${TMPROOT}/c/outer/untracked/repos/fedora-desktop/CLAUDE/Plan/00066-y"
run_capture _plan_find_repo_root "${TMPROOT}/c/outer/untracked/repos/fedora-desktop/CLAUDE/Plan/00066-y"
assert_eq "the nested checkout WITH a marker resolves to itself, not the outer repo" \
    "${TMPROOT}/c/outer/untracked/repos/fedora-desktop" "${OUT}"

# A worktree's `.git` is a FILE, not a directory. The bound uses -e so both shapes stop the
# walk; a `-d` test would leak out of a worktree.
make_repo "${TMPROOT}/d/wt" --no-marker --no-git
: >"${TMPROOT}/d/wt/.git"
mkdir -p "${TMPROOT}/d/wt/CLAUDE/Plan/00001-z"
: >"${TMPROOT}/d/ansible.cfg"
run_capture _plan_find_repo_root "${TMPROOT}/d/wt/CLAUDE/Plan/00001-z"
assert_eq "a worktree .git FILE bounds the walk (not just a .git dir)" "1" "${RC}"

mkdir -p "${TMPROOT}/e/nothing/here"
run_capture _plan_find_repo_root "${TMPROOT}/e/nothing/here"
assert_eq "no marker and no repo boundary anywhere above => failure" "1" "${RC}"

# ── plan_init ────────────────────────────────────────────────────────────────────────────

make_repo "${TMPROOT}/f/repo"
mkdir -p "${TMPROOT}/f/repo/CLAUDE/Plan/00007-init"
FAKE_SCRIPT="${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/deploy.bash"
: >"${FAKE_SCRIPT}"

run_capture plan_init "${FAKE_SCRIPT}"
assert_eq "plan_init succeeds inside a well-formed repo" "0" "${RC}"
assert_eq "plan_init exports PLAN_REPO_ROOT" "${TMPROOT}/f/repo" "${PLAN_REPO_ROOT}"
assert_eq "plan_init exports PLAN_SCRIPT_DIR (the plan folder, not the cwd)" \
    "${TMPROOT}/f/repo/CLAUDE/Plan/00007-init" "${PLAN_SCRIPT_DIR}"

# plan_init must be immune to the caller's cwd — that is the entire point.
run_capture bash -c "cd / && source '${LIB}' && plan_init '${FAKE_SCRIPT}' && printf '%s' \"\${PLAN_REPO_ROOT}\""
assert_eq "plan_init resolves the same root when invoked from an unrelated cwd" \
    "${TMPROOT}/f/repo" "${OUT}"

run_capture plan_init "${TMPROOT}/e/nothing/here/x.bash"
assert_eq "plan_init fails when the marker walk finds nothing" "1" "${RC}"
assert_contains "plan_init failure names the marker it looked for" "ansible.cfg" "${OUT}"
assert_contains "plan_init failure explains the deliberate boundary bound" "ON PURPOSE" "${OUT}"

# ── the container guard (new here; not in the donor) ──────────────────────────────────────
#
# Both branches are exercised. A guard that has only ever been tested one way is a guard
# nobody has actually verified — and this one exists precisely because a nested podman probe
# returned a confident wrong answer.

run_capture _plan_in_container "${TMPROOT}/definitely-absent-marker"
assert_eq "in_container returns 1 when no marker exists" "1" "${RC}"

MARKER="${TMPROOT}/fake-containerenv"
: >"${MARKER}"
run_capture _plan_in_container "${TMPROOT}/definitely-absent-marker" "${MARKER}"
assert_eq "in_container returns 0 when any marker exists" "0" "${RC}"
assert_eq "in_container echoes the marker it found" "${MARKER}" "${OUT}"

PLAN_CONTAINER_MARKERS=("${MARKER}")
run_capture plan_require_host "this probes the host container engine"
assert_eq "require_host REFUSES when a container marker is present" "1" "${RC}"
assert_contains "the refusal names the marker found" "${MARKER}" "${OUT}"
assert_contains "the refusal says a result here would be a wrong answer, not a missing one" \
    "confident wrong answer" "${OUT}"
assert_contains "the refusal tells the operator what to do instead" "on the HOST" "${OUT}"

PLAN_CONTAINER_MARKERS=("${TMPROOT}/definitely-absent-marker")
run_capture plan_require_host "this probes the host container engine"
assert_eq "require_host PASSES when no container marker is present" "0" "${RC}"
assert_contains "the pass is announced, never silent" "running on the host" "${OUT}"

# The real markers: this suite is expected to run inside the CCY container, so assert the
# guard actually fires there. If it does not, we are on the host and the check is reported as
# unverified rather than quietly passing.
PLAN_CONTAINER_MARKERS=(/run/.containerenv /.dockerenv)
if _plan_in_container "${PLAN_CONTAINER_MARKERS[@]}" >/dev/null; then
    run_capture plan_require_host "probe host state"
    assert_eq "with the REAL markers, require_host refuses inside this container" "1" "${RC}"
else
    unverified "require_host against the real container markers" \
        "this run is on the host, so there is no container to detect"
fi

# ── the mirror guard: plan_require_container ──────────────────────────────────────────────
#
# Both branches again, for the same reason. This one refuses on the HOST, so its refusing
# branch is the one a container-run suite would never reach by accident.

PLAN_CONTAINER_MARKERS=("${TMPROOT}/definitely-absent-marker")
run_capture plan_require_container "this probes what the entrypoint installed"
assert_eq "require_container REFUSES when no container marker is present" "1" "${RC}"
assert_contains "the refusal says a result here would be a wrong answer, not a missing one" \
    "confident wrong answer" "${OUT}"
assert_contains "the refusal tells the operator what to do instead" "INSIDE the CCY container" \
    "${OUT}"

PLAN_CONTAINER_MARKERS=("${MARKER}")
run_capture plan_require_container "this probes what the entrypoint installed"
assert_eq "require_container PASSES when a container marker is present" "0" "${RC}"
assert_contains "the pass names the marker found, never silent" "${MARKER}" "${OUT}"

# The two guards must disagree on every input, or one of them is wrong. Assert the
# opposition directly rather than trusting two independent reads of the same predicate.
PLAN_CONTAINER_MARKERS=("${MARKER}")
run_capture plan_require_host "probe host state"
HOST_RC_IN_CONTAINER="${RC}"
run_capture plan_require_container "probe container state"
assert_eq "with a marker present, require_host and require_container disagree" \
    "1 0" "${HOST_RC_IN_CONTAINER} ${RC}"

PLAN_CONTAINER_MARKERS=("${TMPROOT}/definitely-absent-marker")
run_capture plan_require_host "probe host state"
HOST_RC_ON_HOST="${RC}"
run_capture plan_require_container "probe container state"
assert_eq "with no marker present, require_host and require_container disagree" \
    "0 1" "${HOST_RC_ON_HOST} ${RC}"

# ── pure helpers ─────────────────────────────────────────────────────────────────────────

assert_eq "strip_cr removes a single trailing CR" "yes" "$(_plan_strip_cr $'yes\r')"
assert_eq "strip_cr is a no-op without a CR" "yes" "$(_plan_strip_cr 'yes')"
assert_eq "strip_cr leaves an interior CR alone" $'a\rb' "$(_plan_strip_cr $'a\rb')"

if _plan_mode_allows_leg deploy deploy && _plan_mode_allows_leg gather gather &&
    ! _plan_mode_allows_leg deploy gather && ! _plan_mode_allows_leg gather deploy; then
    pass "mode state machine pairs deploy<->deploy_leg and gather<->gather_leg only"
else
    fail "mode state machine" "deploy/deploy + gather/gather only" "mismatched pairing accepted"
fi

# ── plan_mode ────────────────────────────────────────────────────────────────────────────

run_capture plan_mode deploy
assert_eq "plan_mode accepts deploy" "0" "${RC}"
run_capture plan_mode gather
assert_eq "plan_mode accepts gather" "0" "${RC}"
run_capture plan_mode bogus
assert_eq "plan_mode rejects an unknown mode" "1" "${RC}"
assert_contains "plan_mode rejection names the legal values" "deploy" "${OUT}"

# ── sudo priming order (the localhost analogue of loading an ssh key) ─────────────────────

PLAN_LOG_STARTED=1
run_capture plan_prime_sudo
assert_eq "prime_sudo is refused after start_log" "1" "${RC}"
assert_contains "the refusal explains the ordering requirement" "plan_start_log" "${OUT}"
assert_contains "the refusal says why the ordering matters" "flooded and garbled" "${OUT}"
PLAN_LOG_STARTED=0

# ── the change gate ──────────────────────────────────────────────────────────────────────

PLAN_MODE="gather"
run_capture plan_gate_change "something"
assert_eq "gate_change is refused in gather mode (a read-only run has nothing to gate)" "1" "${RC}"
assert_contains "the refusal explains why a read-only run must not gate" "read-only" "${OUT}"

PLAN_MODE=""
run_capture plan_gate_change "something"
assert_eq "gate_change requires a declared mode" "1" "${RC}"

PLAN_MODE="deploy"
PLAN_CHECK=1
PLAN_GATE_PASSED=0
run_capture plan_gate_change "a dry run"
assert_eq "gate_change auto-passes under --check (a dry run changes nothing)" "0" "${RC}"
assert_eq "the auto-pass records the gate as passed" "1" "${PLAN_GATE_PASSED}"
PLAN_CHECK=0

PLAN_GATE_PASSED=0
PLAN_ASSUME_YES=1
run_capture plan_gate_change "an explicitly consented change"
assert_eq "gate_change honours -y/PLAN_ASSUME_YES without a tty" "0" "${RC}"
assert_eq "the -y pass records the gate as passed" "1" "${PLAN_GATE_PASSED}"
PLAN_ASSUME_YES=0

# ── ansible guards (no ansible is invoked; the refusals happen first) ─────────────────────

run_capture plan_init "${FAKE_SCRIPT}"
assert_eq "re-init for the ansible guard tests" "0" "${RC}"

PLAN_MODE="gather"
run_capture plan_ansible_playbook "${TMPROOT}/no-such-play.yml"
assert_eq "a missing playbook is refused before ansible is invoked" "1" "${RC}"
assert_contains "the missing-playbook error names the path" "no-such-play.yml" "${OUT}"

# In deploy mode nothing may reach ansible until the gate has passed — the backstop for a
# script that forgets plan_gate_change.
PLAY="${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/play.yml"
: >"${PLAY}"
PLAN_MODE="deploy"
PLAN_GATE_PASSED=0
run_capture plan_ansible_playbook "${PLAY}"
assert_eq "a deploy-mode play is refused before the change gate passes" "1" "${RC}"
assert_contains "the refusal names the gate the script must call" "plan_gate_change" "${OUT}"
run_capture plan_ansible_adhoc localhost -m ping
assert_eq "a deploy-mode ad-hoc run is refused before the change gate passes" "1" "${RC}"
PLAN_MODE=""
PLAN_GATE_PASSED=0

# ── legs ─────────────────────────────────────────────────────────────────────────────────

PLAN_MODE="gather"
PLAN_FAILED_LEGS=""
run_capture plan_gather_leg "ok-leg" true
assert_eq "a passing gather leg returns 0" "0" "${RC}"
assert_eq "a passing gather leg records no failure" "" "${PLAN_FAILED_LEGS}"

run_capture plan_gather_leg "bad-leg" false
assert_eq "a failing gather leg CONTINUES (returns 0)" "0" "${RC}"
assert_eq "a failing gather leg is recorded by name" "bad-leg" "${PLAN_FAILED_LEGS}"
run_capture plan_gather_leg "worse-leg" false
assert_eq "multiple failing gather legs accumulate" "bad-leg worse-leg" "${PLAN_FAILED_LEGS}"

run_capture plan_deploy_leg "wrong-mode" true
assert_eq "deploy_leg is refused in gather mode" "1" "${RC}"
PLAN_MODE="deploy"
run_capture plan_gather_leg "wrong-mode" true
assert_eq "gather_leg is refused in deploy mode" "1" "${RC}"

OUT="$(bash -c "
    source '${LIB}'
    PLAN_MODE=deploy
    PLAN_GATE_PASSED=1
    plan_deploy_leg 'canary' false
    printf 'REACHED-NEXT-LEG'
" 2>&1)"
RC=$?
assert_eq "a failed deploy leg exits non-zero" "1" "${RC}"
assert_not_contains "a failed deploy leg never reaches the next leg" "REACHED-NEXT-LEG" "${OUT}"
assert_contains "a failed deploy leg says it is aborting" "ABORT" "${OUT}"

# The subshell guard: inside $(...) the abort would kill only the subshell and control would
# flow on to the NEXT leg. Misuse must take the whole run down.
OUT="$(bash -c "
    source '${LIB}'
    PLAN_MODE=deploy
    PLAN_GATE_PASSED=1
    captured=\$(plan_deploy_leg 'in-substitution' true)
    printf 'REACHED-NEXT-LEG %s' \"\${captured}\"
" 2>&1)"
RC=$?
assert_not_contains "deploy_leg inside a command substitution does not let the run continue" \
    "REACHED-NEXT-LEG" "${OUT}"
assert_contains "the subshell abort names BASH_SUBSHELL so the cause is obvious" \
    "BASH_SUBSHELL" "${OUT}"
if [[ "${RC}" -ne 0 ]]; then
    pass "deploy_leg misused in a subshell exits non-zero"
else
    fail "deploy_leg misused in a subshell exits non-zero" "non-zero" "${RC}"
fi

PLAN_MODE=""
PLAN_FAILED_LEGS=""

# ── plan_finish ──────────────────────────────────────────────────────────────────────────

OUT="$(bash -c "source '${LIB}'; PLAN_FAILED_LEGS=''; plan_finish" 2>&1)"
RC=$?
assert_eq "plan_finish exits 0 when every leg passed" "0" "${RC}"
assert_contains "plan_finish says all legs were OK" "all legs OK" "${OUT}"

OUT="$(bash -c "source '${LIB}'; PLAN_FAILED_LEGS='a b'; plan_finish" 2>&1)"
RC=$?
assert_eq "plan_finish exits non-zero when a leg failed" "1" "${RC}"
assert_contains "plan_finish names the failed legs" "a b" "${OUT}"

# ── plan_parse_common_flags ──────────────────────────────────────────────────────────────

PLAN_CHECK=0
PLAN_ASSUME_YES=0
PLAN_CHECK_ARGS=()
plan_parse_common_flags --check -y --only-this extra
assert_eq "--check sets PLAN_CHECK" "1" "${PLAN_CHECK}"
assert_eq "--check populates the ansible --check argv" "--check" "${PLAN_CHECK_ARGS[*]}"
assert_eq "-y sets PLAN_ASSUME_YES" "1" "${PLAN_ASSUME_YES}"
assert_eq "unknown args are left for the plan script" "--only-this extra" "${PLAN_REMAINING_ARGS[*]}"

PLAN_CHECK=0
PLAN_ASSUME_YES=0
PLAN_CHECK_ARGS=()
plan_parse_common_flags
assert_eq "no args leaves an empty remainder" "0" "${#PLAN_REMAINING_ARGS[@]}"

OUT="$(bash -c "source '${LIB}'; PLAN_USAGE='USAGE-SENTINEL'; plan_parse_common_flags --help" 2>&1)"
RC=$?
assert_eq "--help exits 0" "0" "${RC}"
assert_contains "--help prints PLAN_USAGE" "USAGE-SENTINEL" "${OUT}"

# ── plan_confirm without a tty ───────────────────────────────────────────────────────────

PLAN_ASSUME_YES=1
run_capture plan_confirm "auto path"
assert_eq "plan_confirm returns 0 under PLAN_ASSUME_YES with no tty" "0" "${RC}"
assert_contains "the auto-confirm is announced, never silent" "auto-confirmed" "${OUT}"
PLAN_ASSUME_YES=0

if [[ -n "$(command -v setsid)" ]]; then
    OUT="$(setsid bash -c "source '${LIB}'; plan_confirm 'needs a tty'" </dev/null 2>&1)"
    RC=$?
    assert_eq "plan_confirm with no controlling terminal fails (never hangs)" "1" "${RC}"
    assert_contains "the no-tty failure names the -y escape hatch" "--yes" "${OUT}"
    assert_not_contains "the no-tty failure is not a raw bash error" "unbound variable" "${OUT}"
else
    unverified "plan_confirm no-tty path" "setsid not available in this environment"
fi

# ── the tee'd run log, end to end ────────────────────────────────────────────────────────
#
# A run log that loses its final buffered chunk is worse than no log: the missing lines are
# exactly the ones written as the run died.

LOGTEST="${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/logtest.bash"
cat >"${LOGTEST}" <<LOGTEST_EOF
#!/usr/bin/env bash
set -euo pipefail
source '${LIB}'
plan_init "\${BASH_SOURCE[0]}"
plan_mode gather
plan_start_log auto
printf 'FIRST-LINE\n'
i=0
while [ "\${i}" -lt 400 ]; do
    printf 'filler line %s\n' "\${i}"
    i=\$((i + 1))
done
printf '\033[31mCOLOURED-LINE\033[0m\n'
printf 'LAST-LINE-BEFORE-EXIT\n'
LOGTEST_EOF
chmod +x "${LOGTEST}"

run_capture bash "${LOGTEST}"
assert_eq "a script using plan_start_log exits 0" "0" "${RC}"
assert_contains "the run log path is announced to the operator" "logtest-runs/" "${OUT}"

LOGFILE="$(find "${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/logtest-runs" -name 'logtest.log' -type f)"
if [[ -z "${LOGFILE}" ]]; then
    fail "start_log writes a log file under <script>-runs/<timestamp>/" "a logtest.log" "none found"
else
    pass "start_log writes a log file under <script>-runs/<timestamp>/"
    LOGBODY="$(cat "${LOGFILE}")"
    assert_contains "the log captures the first line" "FIRST-LINE" "${LOGBODY}"
    assert_contains "the log captures the LAST line (deterministic tee drain)" \
        "LAST-LINE-BEFORE-EXIT" "${LOGBODY}"
    assert_contains "the log captures all 400 filler lines" "filler line 399" "${LOGBODY}"
    assert_contains "the log keeps the coloured line's text" "COLOURED-LINE" "${LOGBODY}"
    assert_not_contains "the log strips ANSI escapes (logs stay monochrome)" \
        $'\033[' "${LOGBODY}"
    if [[ -n "$(find "${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/logtest-runs" -name '*.fifo')" ]]; then
        fail "start_log leaves no stray fifo in the run dir" "no .fifo" "a .fifo remains"
    else
        pass "start_log leaves no stray fifo in the run dir"
    fi
fi

# No secret scrubber exists in this repo, so start_log must SAY the log is unscrubbed. A
# silent omission of the donor's scrub step would ship its shape without its safety.
assert_contains "the operator is told the run log is UNSCRUBBED and gitignored" "UNSCRUBBED" "${OUT}"

# A Ctrl-C'd run must still flush the log.
INTTEST="${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/inttest.bash"
cat >"${INTTEST}" <<INTTEST_EOF
#!/usr/bin/env bash
set -euo pipefail
source '${LIB}'
plan_init "\${BASH_SOURCE[0]}"
plan_mode gather
plan_start_log auto
printf 'BEFORE-SIGNAL\n'
kill -INT \$\$
printf 'AFTER-SIGNAL-SHOULD-NOT-APPEAR\n'
INTTEST_EOF
chmod +x "${INTTEST}"

run_capture bash "${INTTEST}"
if [[ "${RC}" -eq 0 ]]; then
    fail "an interrupted run exits non-zero" "non-zero" "${RC}"
else
    pass "an interrupted run exits non-zero"
fi
INTLOG="$(find "${TMPROOT}/f/repo/CLAUDE/Plan/00007-init/inttest-runs" -name 'inttest.log' -type f)"
if [[ -z "${INTLOG}" ]]; then
    fail "an interrupted run still leaves a log" "inttest.log" "none found"
else
    INTBODY="$(cat "${INTLOG}")"
    assert_contains "an interrupted run's log is still flushed to disk" "BEFORE-SIGNAL" "${INTBODY}"
    assert_not_contains "an interrupted run stops where it was interrupted" \
        "AFTER-SIGNAL-SHOULD-NOT-APPEAR" "${INTBODY}"
fi

# ── structural invariants over the library's own source ──────────────────────────────────
#
# Checked against the source with whole-line comments removed: the header DOCUMENTS the banned
# idioms by name, and a check that cannot tell an explanation from an occurrence would force
# the library to stop explaining itself.
LIB_CODE="$(printf '%s\n' "${LIB_SRC}" | grep -v '^[[:space:]]*#')"
readonly LIB_CODE

DEVNULL='/dev'"/null"
NEEDLE_DISCARD_ALL=">${DEVNULL} 2>&"'1'
NEEDLE_DISCARD_ERR='2>'"${DEVNULL}"
NEEDLE_OR_TRUE='||'" true"
NEEDLE_OR_NOOP='||'" :"
NEEDLE_SET_PLUS_E='set'" +e"

assert_not_contains "the library code never calls git rev-parse (R1)" "git rev-parse" "${LIB_CODE}"
assert_not_contains "the library code never hardcodes the container path (R1)" \
    "/work""space" "${LIB_CODE}"
assert_not_contains "the library never discards stdout+stderr" "${NEEDLE_DISCARD_ALL}" "${LIB_CODE}"
assert_not_contains "the library never discards stderr alone" "${NEEDLE_DISCARD_ERR}" "${LIB_CODE}"
assert_not_contains "the library never swallows a non-zero exit" "${NEEDLE_OR_TRUE}" "${LIB_CODE}"
assert_not_contains "the library never swallows a non-zero exit via a no-op" \
    "${NEEDLE_OR_NOOP}" "${LIB_CODE}"
assert_not_contains "the library never disables errexit" "${NEEDLE_SET_PLUS_E}" "${LIB_CODE}"
assert_not_contains "the library carries no linter suppression" "shellcheck dis""able" "${LIB_CODE}"

# A library never sets shell options — the caller owns the shell.
if printf '%s\n' "${LIB_CODE}" | grep -Eq '^[[:space:]]*set -[euo]'; then
    fail "the library sets no shell options" "no set -euo" "a set -… is present"
else
    pass "the library sets no shell options"
fi

# funcs_containing <pattern> — the sorted set of function names containing a matching line.
funcs_containing() {
    local pattern="$1"
    printf '%s\n' "${LIB_SRC}" | awk -v pat="${pattern}" '
        /^[A-Za-z_][A-Za-z0-9_]*\(\)[[:space:]]*\{/ { fn = $0; sub(/\(\).*/, "", fn) }
        $0 ~ pat { if (fn != "") print fn }
    ' | sort -u | tr '\n' ' '
}

# Three functions deviate from "a library never exits", deliberately. Pin it to exactly those
# three so no fourth can grow one unnoticed. The pattern is anchored to the start of a line so
# it matches a real `exit` STATEMENT, not the word inside a diagnostic string.
EXITING_FUNCS="$(funcs_containing '^[[:space:]]*exit ')"
assert_eq "only deploy_leg, finish and --help abort (the documented deviation)" \
    "plan_deploy_leg plan_finish plan_parse_common_flags " "${EXITING_FUNCS}"

extract_func() {
    printf '%s\n' "${LIB_SRC}" | awk -v fn="$1() {" 'index($0, fn)==1{ins=1} ins{print} ins && $0=="}"{ins=0}'
}

START_LOG_BODY="$(extract_func plan_start_log)"
for sig in EXIT INT TERM HUP; do
    if printf '%s\n' "${START_LOG_BODY}" | grep -Eq "trap .* ${sig}\b"; then
        pass "start_log arms the finalize handler for ${sig}"
    else
        fail "start_log arms the finalize handler for ${sig}" "a trap on ${sig}" "no trap on ${sig}"
    fi
done

FINALIZE_BODY="$(extract_func _plan_finalize_log)"
WAIT_LINE="$(printf '%s\n' "${FINALIZE_BODY}" | grep -n 'wait "' | cut -d: -f1)"
NOTICE_LINE="$(printf '%s\n' "${FINALIZE_BODY}" | grep -n 'UNSCRUBBED' | cut -d: -f1)"
if [[ -n "${WAIT_LINE}" ]] && [[ -n "${NOTICE_LINE}" ]] && [[ "${WAIT_LINE}" -lt "${NOTICE_LINE}" ]]; then
    pass "finalize waits for the tee drain BEFORE reporting on the log"
else
    fail "finalize waits for the tee drain BEFORE reporting on the log" \
        "a wait before the notice" "wait=${WAIT_LINE} notice=${NOTICE_LINE}"
fi

CONFIRM_BODY="$(extract_func plan_confirm)"
if printf '%s\n' "${CONFIRM_BODY}" | grep -Eq '>[[:space:]]*/dev/tty'; then
    fail "plan_confirm routes the prompt through ordered stdout" \
        "no write to /dev/tty" "a direct /dev/tty write (races ahead of the tee'd banner)"
else
    pass "plan_confirm routes the prompt through ordered stdout"
fi
assert_contains "plan_confirm reads the reply from /dev/tty (ansible drains stdin)" \
    "</dev/tty" "${CONFIRM_BODY}"

DEPLOY_BODY="$(extract_func plan_deploy_leg)"
assert_contains "deploy_leg carries the BASH_SUBSHELL guard" "BASH_SUBSHELL" "${DEPLOY_BODY}"

# The ansible wrappers MUST cd to the repo root: ansible.cfg's inventory, roles_path, fact
# cache and vault_password_file are all RELATIVE, so running from elsewhere silently picks up
# different ones. This is the fedora-desktop-specific invariant.
# Needle assembled from fragments: written literally it is a single-quoted expansion, which
# the linter reads as an accidental non-expansion.
CD_NEEDLE='cd "'"${DOLLAR_SIGN:=$}"'{PLAN_REPO_ROOT}"'
for fn in plan_ansible_playbook plan_ansible_adhoc; do
    body="$(extract_func "${fn}")"
    assert_contains "${fn} cds to PLAN_REPO_ROOT (ansible.cfg paths are relative)" \
        "${CD_NEEDLE}" "${body}"
    assert_contains "${fn} closes stdin so ansible cannot drain it" "</dev/null" "${body}"
done

# Every array expansion must use the set -u-safe [@]+ form: a bare "${arr[@]}" aborts on an
# empty array under `set -u` on bash < 4.4, a hard failure in a caller running set -euo.
DOLLAR='$'
LENGTH_PREFIX="${DOLLAR}{#"
UNGUARDED_LINES=""
while IFS= read -r line; do
    case "${line}" in
        *'[@]+'*) ;;
        *"${LENGTH_PREFIX}"*) ;;
        *) UNGUARDED_LINES="${UNGUARDED_LINES}${line}
" ;;
    esac
done < <(printf '%s\n' "${LIB_CODE}" | grep -F '[@]')
assert_eq "every array expansion uses the set -u-safe bracket-at-plus form" "" "${UNGUARDED_LINES}"

# ── summary ──────────────────────────────────────────────────────────────────────────────

printf '\n'
if [[ "${UNVERIFIED}" -gt 0 ]]; then
    printf '%s check(s) could not be run in this environment (reported above).\n' "${UNVERIFIED}"
fi
if [[ "${FAILED}" -eq 0 ]]; then
    printf 'test-planlib: PASSED (library version %s)\n' "${PLANLIB_VERSION}"
    exit 0
fi
printf 'test-planlib: FAILED\n' >&2
exit 1
