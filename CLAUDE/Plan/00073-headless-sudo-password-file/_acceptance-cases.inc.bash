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

# ── D2 inertness: needs NO sudo binary, so these are meaningful in a container ───────
#
# make_sudo_stub <dir> — install a fake `sudo` in <dir> that REPORTS its argv instead of
# running anything, so `_sudo`'s constructed command line can be measured. Put <dir> first
# on PATH and `_sudo` resolves to it exactly as it would resolve the real binary — a
# closer model than substituting a shell function, and it keeps the stub body inside a
# quoted heredoc where no expansion (or SC2016) can occur.
#
# Bracketed output, [a][b][c], because the defect being ruled out is an EMPTY argument:
# in any unbracketed rendering it vanishes into whitespace and a broken call looks exactly
# like a correct one.
make_sudo_stub() {
    local dir="$1"
    cat > "${dir}/sudo" <<'SUDO_STUB'
#!/usr/bin/env bash
printf '[%s]' "$@"
printf '\n'
SUDO_STUB
    chmod 0755 "${dir}/sudo"
}

# expect_sudo_argv <want> <opts> <cmd ...> — assert the SHIPPED _sudo, with HL_SUDO_OPTS
# set from <opts>, invokes sudo with EXACTLY <want>.
#
# This is Success Criterion 2 ("a NOPASSWD run behaves byte-identically — proven, not
# assumed") reduced to the one part of it a container can actually measure: the argv that
# _sudo constructs. `sudo "" dnf …` would fail on EVERY pre-existing path, so this is the
# highest-consequence assertion in the harness and the cheapest to get wrong silently.
#
# The definition is EXTRACTED from run.bash rather than copied here — a copy would prove
# only that the copy works. An awk that matches nothing emits nothing and still exits 0,
# so the extraction is checked for content, and for the variable it must mention, before
# it is run.
expect_sudo_argv() {
    local want="$1" opts="$2"
    shift 2
    local defn
    defn="$(awk '/^_sudo\(\) \{/,/^\}/' "${RUN_BASH}")"
    if [[ -z "${defn}" ]] || [[ "${defn}" != *HL_SUDO_OPTS* ]]; then
        printf '[FAIL] could not extract _sudo() from %s — got: %s\n' \
            "${RUN_BASH}" "${defn:-<empty>}" >&2
        return 1
    fi
    local script arg
    script="$(printf '%s\n' 'set -u' "${defn}" "HL_SUDO_OPTS=(${opts})")"
    script+=$'\n''_sudo'
    for arg in "$@"; do
        script+=" $(printf '%q' "${arg}")"
    done
    local got rc=0
    got="$(PATH="${SUDO_STUB_DIR}:${PATH}" bash -c "${script}")" || rc=$?
    if [[ "${rc}" -ne 0 ]]; then
        printf '[FAIL] _sudo errored (rc=%s) with HL_SUDO_OPTS=(%s)\n' "${rc}" "${opts}" >&2
        return 1
    fi
    if [[ "${got}" == "${want}" ]]; then
        printf '[OK] HL_SUDO_OPTS=(%s) -> sudo %s\n' "${opts}" "${got}"
        return 0
    fi
    printf '[FAIL] HL_SUDO_OPTS=(%s)\n       want: %s\n       got : %s\n' \
        "${opts}" "${want}" "${got}" >&2
    return 1
}

# expect_colondash_would_break <probe-script> — the NEGATIVE CONTROL for the comment above
# _sudo in run.bash, which justifies NOT using the `[@]:-` fallback that run.bash uses
# elsewhere (for `rm -f`, where an empty argument is harmless). The comment claims that
# idiom would inject an empty first argument here. This asserts the claim rather than
# trusting it: without it, the only reason _sudo looks different from its neighbours is an
# assertion in a comment, and if bash ever changed the behaviour nothing would say so.
expect_colondash_would_break() {
    local probe="$1" got
    got="$(PATH="${SUDO_STUB_DIR}:${PATH}" bash "${probe}")"
    if [[ "${got}" == '[][dnf]' ]]; then
        printf '[OK] the empty-array fallback really does inject an empty arg: %s\n' "${got}"
        return 0
    fi
    printf '[FAIL] expected [][dnf] from the empty-array fallback, got: %s\n' "${got}" >&2
    printf '       run.bash comments justify _sudo using this claim — if it is false, fix them.\n' >&2
    return 1
}

# refuse <reason> — record a leg that could NOT be evaluated. Deliberately returns
# non-zero: a check that did not run has not passed, and plan_finish must exit non-zero so
# nothing downstream reads this run as a verification. Same call as Plan 00072's
# rootless-engine guard — an environment whose answer cannot be obtained has not given a
# safe answer.
refuse() {
    printf '[REFUSED] %s\n' "$1" >&2
    return 1
}

# ── The credential gates: these need a REAL sudo binary ─────────────────────────────
#
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
