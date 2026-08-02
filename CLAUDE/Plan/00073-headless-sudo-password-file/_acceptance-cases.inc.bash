# _acceptance-cases.inc.bash — leg logic for Plan 00073's acceptance.bash.
#
# WHY THIS IS A SEPARATE FILE. `acceptance.bash` invokes these only indirectly, as
# `plan_gather_leg "<name>" expect_fail "<substr>" VAR=VAL …`. ShellCheck cannot see
# through that indirection and reports every line of both functions as SC2317
# ("Command appears to be unreachable"). Suppression directives are BANNED in this repo,
# so the fix is structural: leg logic lives in a sourced helper, which is exactly the
# carve-out `.claude/rules/plan-script-standards.md` names — "leg logic split out to
# avoid SC2317". Helpers get the line-level rules only, never the orchestrator ones.
#
# Sourced, never executed: no shell options are set here and nothing calls `exit` — the
# caller owns the shell.

# run_headless <VAR=VAL ...> — run.bash --headless as $DROP_USER with ONLY the given
# RUN_BASH_* env, combined output on stdout so the caller can assert on the message.
# `env -i` is what makes the assertion meaningful: without it the invoking shell's own
# RUN_BASH_* values would leak in and a gate could pass for the wrong reason.
run_headless() {
    drop_run env -i PATH="/usr/bin:/bin" HOME=/tmp "$@" \
        bash "${RUN_BASH}" --headless 2>&1
}

# expect_fail <expected-substr> <VAR=VAL ...> — assert run.bash exits NON-ZERO and its
# output contains <expected-substr>.
#
# Both halves matter. Exit code alone is consistent with many stories — including the
# mundane one where the run died for an unrelated reason two gates earlier — so the
# message is what identifies WHICH gate fired (bash-standards §9, "assert the mechanism,
# not just the exit code").
#
# Returns 0 on success so `plan_gather_leg` records the verdict; the leg NAME is the test
# name, so `plan_finish`'s failed-leg summary is the failure report.
expect_fail() {
    local want="$1"
    shift
    local out rc=0
    out="$(run_headless "$@")" || rc=$?
    if [[ "${rc}" -eq 0 ]]; then
        printf '[FAIL] expected non-zero exit, got 0\n' >&2
        printf '       got: %s\n' "$(tr '\n' '|' <<< "${out}")" >&2
        return 1
    fi
    if grep -qF -- "${want}" <<< "${out}"; then
        printf '[OK] exit %s, message present: %s\n' "${rc}" "${want}"
        return 0
    fi
    printf '[FAIL] exit %s but missing message: %s\n' "${rc}" "${want}" >&2
    printf '       got: %s\n' "$(tr '\n' '|' <<< "${out}")" >&2
    return 1
}
