#!/usr/bin/env bash
#
# probe-signal-writer.bash — establish the WRITER half of the operator-signal
# contract: what `hooks-daemon signal` accepts, refuses, and writes.
#
# FACT-FINDING ONLY (R9). Renders no verdict and asserts no expectation: every
# probe records what the installed daemon actually did. FACTS-ccy-mechanics.md
# F7 recorded this command as not existing, so "does it exist" is itself one of
# the facts, not a precondition.
#
# Changes nothing that matters: every signal is raised against a THROWAWAY
# project root created under the run directory, never against this repository.
# Raising a real `reboot-warning` against the live project root would signal the
# session running this probe — the supervisor watches that directory — so the
# scratch root is a safety property, not tidiness.
#
# Usage: probe-signal-writer.bash <report-file>
set -euo pipefail

REPORT="${1:?usage: probe-signal-writer.bash <report-file>}"

# Bounded, script-relative repo-root walk (R1): no `git rev-parse`, no
# hardcoded /workspace, no fixed-depth ../.. hop. Bounded at `.git` because this
# repo is routinely checked out inside another that also has an ansible.cfg.
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}

CLI="${repoRoot}/.claude/hooks-daemon/bin/hooks-daemon"
CONFIG="${repoRoot}/.claude/hooks-daemon.yaml"

if [[ ! -x "${CLI}" ]]; then
    printf '[FATAL] daemon CLI not executable: %s\n' "${CLI}" >&2
    printf '        Without it nothing here can be established either way.\n' >&2
    exit 1
fi
if [[ ! -r "${CONFIG}" ]]; then
    printf '[FATAL] no daemon config to seed a scratch project root: %s\n' "${CONFIG}" >&2
    exit 1
fi

SCRATCH="$(dirname "${REPORT}")/scratch-project-root"
SIDECAR_DIR="${SCRATCH}/.claude/hooks-daemon/untracked/context-sidecar"
rm -rf "${SCRATCH}"
mkdir -p "${SCRATCH}/.claude" "${SIDECAR_DIR}"
cp "${CONFIG}" "${SCRATCH}/.claude/hooks-daemon.yaml"

# A literal backtick, built from its octal code. Writing one inside a
# single-quoted printf format is read by shellcheck as command substitution
# (SC2016), and suppressions are banned (R11) — so the character arrives as
# data instead.
BT="$(printf '\140')"

# Record one fact in both places: the report file (durable) and stdout (the leg
# log). `record` is the only thing that writes findings, so the two cannot drift.
record() {
    local id="$1" what="$2" value="$3"
    printf -- '- **%s** — %s\n  - %s%s%s\n' \
        "${id}" "${what}" "${BT}" "${value}" "${BT}" >> "${REPORT}"
    printf 'FACT %s: %s -> %s\n' "${id}" "${what}" "${value}"
}

# Run the signal command against the scratch root and echo "<exit>|<output>".
# stderr is folded into stdout DELIBERATELY: the refusal messages are the
# finding here, and this function's stdout is consumed by the caller rather
# than being a payload anything parses as JSON.
signal_run() {
    local out status
    if out="$(CLAUDE_CODE_SESSION_ID=probe-session "${CLI}" signal "$@" --project-root "${SCRATCH}" 2>&1)"; then
        status=0
    else
        status=$?
    fi
    printf '%s|%s' "${status}" "${out}"
}

{
    printf '\n## Writer half — %shooks-daemon signal%s\n\n' "${BT}" "${BT}"
    printf 'Probed against a throwaway project root, never this repository.\n\n'
} >> "${REPORT}"

# --- W1: does the command exist at all? --------------------------------------
# F7's whole finding was that it did not. Read the command list rather than
# trying to run it, so "absent" is reported as absent instead of as a crash.
help_out="$("${CLI}" --help)"
if [[ "${help_out}" == *",signal,"* ]]; then
    record W1 "the 'signal' subcommand is present in the CLI command list" "present"
else
    record W1 "the 'signal' subcommand is present in the CLI command list" "ABSENT — F7 still holds"
    printf '\nW1 is absent, so nothing below can be established. Stopping.\n' >> "${REPORT}"
    printf 'FACT W1 absent — the rest of the writer contract is unestablished.\n'
    exit 0
fi

# --- W2: the kind set is CLOSED, and these are its members -------------------
kinds_line="$("${CLI}" signal --help)"
kind_set="$(printf '%s' "${kinds_line}" | grep -oE '\{[a-z,-]+\}' | head -n1)"
record W2 "the accepted signal kinds, as the parser itself lists them" "${kind_set}"

# --- W3/W4/W5: what each kind requires, and how a refusal exits --------------
# The exit CODE matters to a caller that wants to tell "you asked wrongly" from
# "the write failed": argparse rejects an unknown kind before the command runs.
for probe in \
    "W3a|reboot-warning with --minutes|reboot-warning --minutes 5" \
    "W3b|reboot-cancelled with NO --minutes|reboot-cancelled" \
    "W3c|reboot-cancelled WITH --minutes|reboot-cancelled --minutes 5" \
    "W4a|reboot-warning with NO --minutes|reboot-warning" \
    "W4b|reboot-warning with --minutes 0|reboot-warning --minutes 0" \
    "W5|an unrecognised kind|definitely-not-a-kind --minutes 5"; do
    IFS='|' read -r pid pwhat pargs <<< "${probe}"
    # shellcheck disable=SC2086
    result="$(signal_run ${pargs})"
    pstatus="${result%%|*}"
    pout="${result#*|}"
    first_line="$(printf '%s' "${pout}" | awk 'NR==1')"
    record "${pid}" "${pwhat}" "exit ${pstatus} — ${first_line}"
done

# --- W6: the artefact — its path, and its EXACT field set --------------------
# The closed-shape claim this feature's security rests on is falsifiable here:
# if the payload carried any free-text field, a host-side caller could put words
# in front of the agent. Enumerate the keys rather than looking for known ones,
# so a NEW field shows up instead of being skipped.
warn_result="$(signal_run reboot-warning --minutes 7)"
warn_status="${warn_result%%|*}"
if [[ "${warn_status}" != "0" ]]; then
    record W6 "could not raise a warning to inspect its artefact" "exit ${warn_status}"
else
    artefacts="$(find "${SIDECAR_DIR}" -maxdepth 1 -type f -name '*.operator-signal' -printf '%P\n' | sort | paste -sd, -)"
    record W6a "the artefact written for one session" "${artefacts}"
    signal_file="$(find "${SIDECAR_DIR}" -maxdepth 1 -type f -name '*.operator-signal' -print -quit)"
    keys="$(python3 -c 'import json,sys; print(",".join(sorted(json.load(open(sys.argv[1])).keys())))' "${signal_file}")"
    record W6b "every key in a reboot-warning payload" "${keys}"
    kind_val="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["kind"])' "${signal_file}")"
    minutes_val="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("minutes","<absent>"))' "${signal_file}")"
    record W6c "the kind and minutes it carries" "kind=${kind_val} minutes=${minutes_val}"

    # W7: does a cancel SUPERSEDE a warning, or sit beside it? A reboot that is
    # called off must not leave the warning readable, and the answer is a
    # property of the path, not of the payload.
    cancel_result="$(signal_run reboot-cancelled)"
    cancel_status="${cancel_result%%|*}"
    after="$(find "${SIDECAR_DIR}" -maxdepth 1 -type f -name '*.operator-signal' -printf '%P\n' | sort | paste -sd, -)"
    if [[ "${cancel_status}" == "0" ]]; then
        now_kind="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["kind"])' "${signal_file}")"
        record W7 "after a cancel, the files present and the kind the session would read" \
            "files=${after} kind=${now_kind}"
    else
        record W7 "cancel could not be raised" "exit ${cancel_status}"
    fi
fi

# --- W8: --all-sessions with nothing live ------------------------------------
# The one outcome a deliberately-safe reboot command must never produce is
# "warned nobody, exited 0". Whether the CLI refuses or succeeds vacuously
# decides how ccy-sessions must treat its exit status, so it is a fact worth
# having rather than assuming.
rm -f "${SIDECAR_DIR}"/*.operator-signal
empty_result="$(signal_run reboot-warning --minutes 5 --all-sessions)"
empty_status="${empty_result%%|*}"
empty_out="${empty_result#*|}"
record W8a "--all-sessions when NO session sidecar exists" \
    "exit ${empty_status} — $(printf '%s' "${empty_out}" | awk 'NR==1')"

# Now give it one live sidecar and ask again. A sidecar is just a `<id>.json`
# file in the same directory (the daemon globs that extension to enumerate
# sessions), so a synthetic one exercises the real discovery path.
printf '{}' > "${SIDECAR_DIR}/synthetic-session.json"
live_result="$(signal_run reboot-warning --minutes 5 --all-sessions)"
live_status="${live_result%%|*}"
live_files="$(find "${SIDECAR_DIR}" -maxdepth 1 -type f -name '*.operator-signal' -printf '%P\n' | sort | paste -sd, -)"
record W8b "--all-sessions with one synthetic sidecar present" \
    "exit ${live_status} files=${live_files}"

printf '\n' >> "${REPORT}"
