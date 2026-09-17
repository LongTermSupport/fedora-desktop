#!/usr/bin/bash
# L0 no-kill safety gate for the container-watch watchdog (Plan 00055 Phase 5).
#
# The shipped watchdog is REPORTING-ONLY: it scans, attributes, and reports
# offending container processes, but it MUST NEVER terminate or throttle one.
# This gate FAILS the build if any *executable* process-termination call site is
# introduced into the watchdog code.
#
# Critically, it scopes to *call-site syntax* (the `(` / argv / shell-invocation
# form), so the word "kill" remains allowed inside guidance string literals such
# as an `exec_hint` value, a help string, or a comment. Those are advice a human
# may choose to run — the tool never executes them.
#
# Usage:
#   qa-nokill-containerwatch.bash              # gate the real watchdog code
#   qa-nokill-containerwatch.bash --self-test  # prove the gate detects a kill
#                                              # and ignores a guidance literal
#
# Exit codes: 0 = clean / self-test passed; 1 = forbidden call site found or
# self-test failed.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# Forbidden EXECUTABLE termination call sites. Each entry is an extended-regex
# matched line-by-line with `grep -nE`. The patterns key on call-site syntax —
# an open paren, an argv token, or a shell-invocation form — never on the bare
# word "kill", so guidance string literals are not flagged.
#
# Covered:
#   Python: os.kill( , .send_signal( , signal.SIG* constants, a subprocess/Popen
#           argv whose first token is kill/pkill, Gio bindings' force_exit/
#           send_signal (also reachable from GJS).
#   JS/GJS: subproc.force_exit( , subproc.send_signal( , Gio.Subprocess argv
#           starting kill/pkill.
#   Shell : a bare `pkill ` or `kill -<signal>` invocation.
FORBIDDEN_PATTERNS=(
    # Python signal delivery to a pid
    '\bos\.kill[[:space:]]*\('
    '\bos\.killpg[[:space:]]*\('
    # signal.SIGKILL / signal.SIGTERM / signal.SIGSTOP … constants (only ever
    # referenced to deliver a signal)
    '\bsignal\.SIG[A-Z]'
    # object.send_signal( — Popen, Gio.Subprocess, asyncio transports
    '\.send_signal[[:space:]]*\('
    # Gio.Subprocess.force_exit( — GJS/Python hard-kill of a spawned child
    '\.force_exit[[:space:]]*\('
    # A spawned argv whose program is kill/pkill (quoted first token), e.g.
    #   ["kill", "-9", pid]  or  ('pkill', '-f', name)
    '\[[[:space:]]*["'"'"'](kill|pkill)["'"'"']'
    '\([[:space:]]*["'"'"'](kill|pkill)["'"'"']'
    # A bare shell invocation: `pkill ...` or `kill -SIG ...` / `kill -9 ...`.
    # `\b` word-boundary anchors the program name so substrings (e.g. "skill")
    # are not matched; the trailing form distinguishes an invocation from prose.
    '\bpkill[[:space:]]'
    '\bkill[[:space:]]+-'
    # ENDING A CONTAINER, which every pattern above misses. All of them key on
    # delivering a signal to a PID, and `["podman", "stop", cid]` delivers none —
    # it asks the engine to do it. That is the exact shape Plan 00132 Phase 6
    # proposes to add deliberately, so the gate holding the reporting-only line
    # until then has to be able to see it.
    #
    # Matched as an ARGV element, never as prose: the report legitimately tells a
    # human "podman stop <container>", and a gate that cannot tell guidance from
    # execution would force that guidance to be removed to stay green.
    '["'"'"'](podman|docker)["'"'"'],[[:space:]]*["'"'"'](stop|kill|rm|pause|restart)["'"'"']'
    '[[:space:](]engine,[[:space:]]*["'"'"'](stop|kill|rm|pause|restart)["'"'"']'
)

# Collect target files. The JS extension dir is created by a sibling task and may
# not exist yet — glob it without failing (nullglob), and gate whatever exists.
collect_targets() {
    local root="$1"
    local -n _out="$2"
    _out=()
    shopt -s nullglob
    local f
    for f in "$root"/helpers/containerwatch/*.py \
             "$root"/extensions/container-watch@fedora-desktop/*.js; do
        # containment.py is the one audited exception and is checked separately by
        # assert_containment_stops_only, against a STRICTER list. Skipping it here
        # is not a hole: it is the only file in the tree that may not name kill,
        # rm, pause or a force flag at all.
        [[ "$f" == */containment.py ]] && continue
        _out+=("$f")
    done
    shopt -u nullglob
}

# Scan a set of files for forbidden call sites. Echoes each "file:line: text"
# offender to stdout and returns 1 if any were found, 0 if clean.
scan_targets() {
    local found=0
    local file pat hit
    for file in "$@"; do
        for pat in "${FORBIDDEN_PATTERNS[@]}"; do
            # grep -n exit status: 0 = match, 1 = no match, >=2 = real error.
            # Capture into a var so `set -e` does not abort on the no-match case;
            # a genuine grep error (rc>=2) is surfaced as a hard failure.
            local rc=0
            hit="$(grep -nE "$pat" "$file" 2>&1)" || rc=$?
            if [[ $rc -eq 0 ]]; then
                while IFS= read -r line; do
                    echo "${file}:${line}"
                    found=1
                done <<< "$hit"
            elif [[ $rc -ge 2 ]]; then
                echo "ERROR: grep failed scanning $file (rc=$rc): $hit" >&2
                exit 2
            fi
        done
    done
    return "$found"
}

# The verbs containment.py must never learn. `stop` is its whole purpose and is
# absent by design; each of these is a different harm:
#   kill  — denies the workload its shutdown path
#   rm    — destroys the container outright
#   pause — freezes it while it STILL HOLDS the transient units that are the damage
#   -f / --force — turns a graceful stop into any of the above
CONTAINMENT_FORBIDDEN=(
    '["'"'"'](kill|rm|pause|unpause|restart)["'"'"']'
    '--force'
    '["'"'"']-f["'"'"']'
)

# containment.py is exempt from the lifecycle patterns and subject to these instead.
assert_containment_stops_only() {
    local file="$1"
    local pat hit rc found=0
    for pat in "${CONTAINMENT_FORBIDDEN[@]}"; do
        rc=0
        # `--` terminates option parsing: one of these patterns is literally
        # `--force`, which grep would otherwise read as an unrecognised option and
        # exit 2 on. The gate's own control fixtures caught that.
        hit="$(grep -nE -- "$pat" "$file" 2>&1)" || rc=$?
        if [[ $rc -eq 0 ]]; then
            while IFS= read -r line; do
                echo "${file}:${line}" >&2
                found=1
            done <<< "$hit"
        elif [[ $rc -ge 2 ]]; then
            echo "ERROR: grep failed scanning $file (rc=$rc): $hit" >&2
            exit 2
        fi
    done

    if [[ $found -eq 1 ]]; then
        echo >&2
        echo "✗ no-kill gate: containment.py may issue a graceful STOP and nothing else." >&2
        echo "  The verbs above are each a different harm — see CONTAINMENT_FORBIDDEN." >&2
        return 1
    fi
    echo "✓ containment.py: stop-only confirmed (the one audited exception)"
    return 0
}

run_self_test() {
    local tmp
    tmp="$(mktemp -d)"

    # Fixture (a): a real executable kill call site — MUST be detected.
    local kill_fixture="$tmp/kill_fixture.py"
    cat > "$kill_fixture" <<'PYEOF'
import os
def reap(pid):
    os.kill(pid, 9)
PYEOF

    # Fixture (b): the word "kill" only inside a guidance string literal — MUST
    # pass (the tool never executes it).
    local hint_fixture="$tmp/hint_fixture.py"
    cat > "$hint_fixture" <<'PYEOF'
def build_hint(pid):
    # Guidance only — the human may run this; the tool never executes it.
    exec_hint = "podman exec -it box ps -o pid,args  # in-container: kill <pid>"
    return exec_hint
PYEOF

    local self_test_ok=1

    # (a) The kill fixture must be DETECTED (scan_targets returns 1).
    local detect_rc=0
    scan_targets "$kill_fixture" > /dev/null || detect_rc=$?
    if [[ $detect_rc -eq 1 ]]; then
        echo "  self-test (a) PASS: os.kill( fixture detected"
    else
        echo "  self-test (a) FAIL: os.kill( fixture NOT detected (rc=$detect_rc)" >&2
        self_test_ok=0
    fi

    # (b) The guidance-literal fixture must PASS (scan_targets returns 0).
    local pass_rc=0
    scan_targets "$hint_fixture" > /dev/null || pass_rc=$?
    if [[ $pass_rc -eq 0 ]]; then
        echo "  self-test (b) PASS: exec_hint guidance literal not flagged"
    else
        echo "  self-test (b) FAIL: guidance literal wrongly flagged (rc=$pass_rc)" >&2
        self_test_ok=0
    fi

    # (c) A container STOP argv must be DETECTED. Nothing above this pattern's
    # addition could see it: it delivers no signal to any pid.
    local stop_fixture="$tmp/stop_fixture.py"
    cat > "$stop_fixture" <<'PYEOF'
import subprocess
def halt(engine, cid):
    subprocess.run([engine, "stop", cid], check=True)
    subprocess.run(["podman", "rm", cid], check=True)
PYEOF

    local stop_rc=0
    scan_targets "$stop_fixture" > /dev/null || stop_rc=$?
    if [[ $stop_rc -eq 1 ]]; then
        echo "  self-test (c) PASS: container stop argv detected"
    else
        echo "  self-test (c) FAIL: container stop argv NOT detected (rc=$stop_rc)" >&2
        self_test_ok=0
    fi

    # (d) The SAME words as prose must PASS. The watchdog's report tells a human
    # to run `podman stop <container>`, and a gate that could not tell that from
    # an argv would force the advice out of the report to stay green.
    local advice_fixture="$tmp/advice_fixture.py"
    cat > "$advice_fixture" <<'PYEOF'
def advise():
    return "  Stop it, or fix why it exits:  podman stop <container>"
PYEOF

    local advice_rc=0
    scan_targets "$advice_fixture" > /dev/null || advice_rc=$?
    if [[ $advice_rc -eq 0 ]]; then
        echo "  self-test (d) PASS: stop advice prose not flagged"
    else
        echo "  self-test (d) FAIL: stop advice prose wrongly flagged (rc=$advice_rc)" >&2
        self_test_ok=0
    fi

    # (e) A containment module that only builds a stop must PASS its stricter check.
    local good_containment="$tmp/good_containment.py"
    cat > "$good_containment" <<'PYEOF'
def build_stop_argv(engine, cid):
    return [engine, "stop", "--time", "10", cid]
PYEOF

    local good_rc=0 good_out
    good_out="$(assert_containment_stops_only "$good_containment" 2>&1)" || good_rc=$?
    if [[ $good_rc -eq 0 ]]; then
        echo "  self-test (e) PASS: stop-only containment accepted"
    else
        echo "  self-test (e) FAIL: stop-only containment wrongly rejected (rc=$good_rc)" >&2
        echo "$good_out" >&2
        self_test_ok=0
    fi

    # (f) A containment module that learned a destructive verb must be REJECTED.
    # This is the check that matters: the exception removes this file from every
    # other pattern, so if this assertion does not bite, nothing does.
    local bad_containment="$tmp/bad_containment.py"
    cat > "$bad_containment" <<'PYEOF'
def build_stop_argv(engine, cid):
    return [engine, "rm", "--force", cid]
PYEOF

    local bad_rc=0 bad_out
    bad_out="$(assert_containment_stops_only "$bad_containment" 2>&1)" || bad_rc=$?
    if [[ $bad_rc -eq 1 ]]; then
        echo "  self-test (f) PASS: destructive containment rejected"
    else
        echo "  self-test (f) FAIL: destructive containment NOT rejected (rc=$bad_rc)" >&2
        echo "$bad_out" >&2
        self_test_ok=0
    fi

    rm -rf "$tmp"

    if [[ $self_test_ok -eq 1 ]]; then
        echo "✓ no-kill gate self-test passed"
        return 0
    fi
    echo "✗ no-kill gate self-test FAILED" >&2
    return 1
}

main() {
    if [[ "${1:-}" == "--self-test" ]]; then
        run_self_test
        return $?
    fi
    if [[ $# -gt 0 ]]; then
        echo "ERROR: unknown argument '$1' (only --self-test is accepted)" >&2
        exit 2
    fi

    # THE ONE AUDITED EXCEPTION, held to a tighter rule than the files around it.
    #
    # containment.py exists to stop a crash-looping container (Plan 00132 Phase 6),
    # so the lifecycle patterns cannot apply to it. Relaxing the gate for the whole
    # tree would have been the easy move and the wrong one: instead this file is
    # named, and then checked AGAINST A STRICTER LIST than any other — it may build
    # a `stop`, and may not name kill, rm, pause or a force flag at all. A gate that
    # merely skipped it would leave the destructive verbs it must never learn
    # completely unguarded.
    local contained="$REPO_ROOT/helpers/containerwatch/containment.py"
    if [[ -f "$contained" ]]; then
        if ! assert_containment_stops_only "$contained"; then
            return 1
        fi
    fi

    local targets
    collect_targets "$REPO_ROOT" targets
    if [[ ${#targets[@]} -eq 0 ]]; then
        echo "ERROR: no container-watch targets found under $REPO_ROOT — expected" >&2
        echo "       helpers/containerwatch/*.py (the watchdog must exist)." >&2
        exit 2
    fi

    local offenders rc=0
    offenders="$(scan_targets "${targets[@]}")" || rc=$?
    if [[ $rc -eq 0 ]]; then
        echo "✓ no-kill gate: ${#targets[@]} container-watch file(s) clean — reporting-only confirmed"
        return 0
    fi

    echo "✗ no-kill gate: executable process-termination call site(s) found in the watchdog:" >&2
    echo "$offenders" >&2
    echo >&2
    echo "The container-watch watchdog is REPORTING-ONLY. Remove the termination call." >&2
    echo "The word 'kill' is allowed ONLY inside guidance string literals (exec_hint)," >&2
    echo "not as an executable call site." >&2
    return 1
}

main "$@"
