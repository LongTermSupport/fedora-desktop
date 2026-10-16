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

m1_helper_tests() {
    (cd -- "${PLAN_REPO_ROOT}" && ./scripts/qa-helper-tests.bash) || return 1
}

m1_start() {
    M2_NAMES+=("${M1_NAME}")
    m2_launch "${M1_NAME}" || return 1
    m2_container_up "${M1_NAME}" || return 1
}

# m1_holders <container> — one line per process: "<pid> <locks it holds> <cmdline>", where the
# locks are the lock files among its descriptors (keeper.lock, seat.lock), or "-".
m1_holders() {
    podman exec "$1" sh -c '
        for p in /proc/[0-9]*; do
            cmd=$(tr "\000" " " <"$p/cmdline" 2>/dev/null) || continue
            [ -n "$cmd" ] || continue
            locks=$(ls -l "$p/fd" 2>/dev/null | sed -n "s/.*\/\([a-z]*\.lock\)$/\1/p" | sort -u | tr "\n" ",")
            printf "%s %s %s\n" "${p#/proc/}" "${locks:--}" "$cmd"
        done'
}

m1_keeper_only() {
    local ctr="${M2_CTR[${M1_NAME}]}" table verdict
    table="$(m1_holders "${ctr}")" || return 1
    printf '%s\n' "${table}" >"${M2_EV}/${M1_NAME}.holders" || return 1
    # One verdict line: pingbus processes (a pingbus subcommand; claude's own argv names the
    # plugin directory .../plugin/pingbus, which is not one), of them keepers holding
    # keeper.lock, and lock holders that are not the keeper.
    verdict="$(printf '%s\n' "${table}" | awk '
        /\/pingbus [a-z]/ { pingbus++ }
        /pingbus seat keep --launch --parent / && $2 ~ /keeper\.lock/ { keeper++ }
        $2 != "-" && !/pingbus seat keep/ { other++ }
        END { printf "%d %d %d\n", pingbus, keeper, other }')" || return 1
    if [[ "${verdict}" != "1 1 0" ]]; then
        printf '[FAIL] pingbus processes, keepers holding keeper.lock, other lock holders: %s (want 1 1 0); the table is %s:\n%s\n' \
            "${verdict}" "${M2_EV}/${M1_NAME}.holders" "${table}" >&2
        return 1
    fi
    printf 'the keeper is the only pingbus process and the only lock holder (table: %s)\n' "${M2_EV}/${M1_NAME}.holders"
}

acceptance_m1() {
    printf '## M1: the seat keeper and the membership reader\n\n' >>"${REPORT}"
    check_leg "(a) the helper tests, the keeper's included" m1_helper_tests
    plan_deploy_leg "a throwaway plain ccy session in ccy's tmux" m1_start
    check_leg "(b) the keeper is the session's only pingbus process and only lock holder" m1_keeper_only
    plan_deploy_leg "remove the throwaway session" m2_cleanup
    m2_note "M1: (a) and (b) passed; (b) with a seat held is the owner's step in ACCEPTANCE.md"
}
