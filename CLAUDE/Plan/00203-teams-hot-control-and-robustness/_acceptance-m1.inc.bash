# shellcheck shell=bash
# _acceptance-m1.inc.bash — Plan 00203 milestone M1, the seat keeper and the membership reader
# (SPEC-hot-control.md section 6, M1 a-b; ACCEPTANCE.md "M1"), sourced by the acceptance.bash
# dispatcher, which runs acceptance_m1 after acceptance_m2. Sourced, never executed: no shell
# options, no `exit`. A leg runs where errexit is off, so every command that can fail returns
# non-zero from its function explicitly. REPORT and check_leg are the dispatcher's; the session
# launch and its cleanup are M2's (m2_launch, m2_container_up, m2_cleanup, M2_NAMES).
#
# WHAT IT CHANGES, and removes on the way out however the run ends: one throwaway headless
# plain `ccy` in this checkout, m1-plain-<pid>, started and removed exactly as M2's are (its
# name is added to M2_NAMES, so M2's registered cleanup removes it on any exit).
#
# CHECKS
#   (a)  ./scripts/qa-helper-tests.bash is green (every pingbus unit test, the keeper's included)
#   (b)  in the session: exactly one pingbus process, `pingbus seat keep --launch --parent ...`;
#        it holds the session home's keeper.lock; no other process holds a keeper or seat lock.
#        The same with a seat held is the owner's step (ACCEPTANCE.md M1 b): it needs a team.

readonly M1_NAME="m1-plain-$$"
#: The bus-process probe, relative to the checkout, which a ccy container sees at /workspace.
readonly M1_PROCS_REL="CLAUDE/Plan/${PLAN_SCRIPT_DIR#*/CLAUDE/Plan/}/m2_procs.py"

m1_helper_tests() {
    (cd -- "${PLAN_REPO_ROOT}" && ./scripts/qa-helper-tests.bash) || return 1
}

m1_start() {
    M2_NAMES+=("${M1_NAME}")
    m2_launch "${M1_NAME}" || return 1
    m2_container_up "${M1_NAME}" || return 1
}

# m1_keeper_only — m2_procs.py in the session's container: a bus process is its program (the
# pingbus or agent-bus entry point), never a word in its argv, since claude's own names the
# plugin directory .../plugin/pingbus. Its table, "<pid> <locks it holds, or -> <argv>" per
# process, is kept as evidence.
m1_keeper_only() {
    local ctr="${M2_CTR[${M1_NAME}]}" holders="${M2_EV}/${M1_NAME}.holders"
    if ! podman exec "${ctr}" python3 -I "/workspace/${M1_PROCS_REL}" keeper-only /proc >"${holders}"; then
        printf '[FAIL] the keeper is not the session'\''s only bus process and only lock holder (above)\n' >&2
        return 1
    fi
    printf 'the keeper is the only bus process and the only lock holder (table: %s)\n' "${holders}"
}

acceptance_m1() {
    printf '## M1: the seat keeper and the membership reader\n\n' >>"${REPORT}"
    check_leg "(a) the helper tests, the keeper's included" m1_helper_tests
    plan_deploy_leg "a throwaway plain ccy session in ccy's tmux" m1_start
    check_leg "(b) the keeper is the session's only pingbus process and only lock holder" m1_keeper_only
    plan_deploy_leg "remove the throwaway session" m2_cleanup
    m2_note "M1: (a) and (b) passed; (b) with a seat held is the owner's step in ACCEPTANCE.md"
}
