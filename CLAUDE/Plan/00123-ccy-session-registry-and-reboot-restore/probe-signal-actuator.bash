#!/usr/bin/env bash
#
# probe-signal-actuator.bash — establish the READER half of the operator-signal
# contract: whether anything CONSUMES a signal, and whether its idea of the
# contract matches the writer's.
#
# This half is the one that decides whether the feature works. A signal file
# that is written and never read is not a capability — and it fails silently,
# because the writer exits 0 either way. The writer probe cannot see that, so
# this is a separate probe rather than more legs of the same one.
#
# The actuator is `.claude/ccy/claude-supervise.py`, which is stdlib-only and
# CANNOT import the daemon package — so the two sides hold SEPARATE copies of
# the suffix and the kind strings. Two copies that must agree can agree while
# both are wrong, but they can also DISAGREE, and disagreement is the failure
# this probe is for: the writer would keep reporting success while nothing
# rendered a word to any agent.
#
# FACT-FINDING ONLY (R9). Reads files; runs nothing and changes nothing.
#
# Usage: probe-signal-actuator.bash <report-file>
set -euo pipefail

REPORT="${1:?usage: probe-signal-actuator.bash <report-file>}"

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

BT="$(printf '\140')"

SUPERVISOR="${repoRoot}/.claude/ccy/claude-supervise.py"
WRITER_MODULE="${repoRoot}/.claude/hooks-daemon/src/claude_code_hooks_daemon/utils/operator_signal.py"

record() {
    local id="$1" what="$2" value="$3"
    printf -- '- **%s** — %s\n  - %s%s%s\n' \
        "${id}" "${what}" "${BT}" "${value}" "${BT}" >> "${REPORT}"
    printf 'FACT %s: %s -> %s\n' "${id}" "${what}" "${value}"
}

# Count matching lines. `grep -c` exits 1 for ZERO matches — a result, not a
# failure — and 2 for a real error such as an unreadable file. Collapsing the
# two is how a count silently becomes 0 when the read actually broke, so they
# are separated here and only the genuine error aborts.
count_matches() {
    local pattern="$1" file="$2" out status
    if out="$(grep -c -- "${pattern}" "${file}")"; then
        status=0
    else
        status=$?
    fi
    if [[ "${status}" -gt 1 ]]; then
        printf '[FATAL] grep failed (exit %s) reading %s\n' "${status}" "${file}" >&2
        exit 1
    fi
    printf '%s' "${out}"
}

{
    printf '\n## Reader half — the ccy supervisor\n\n'
    printf 'Whether a written signal is consumed, and whether both sides agree.\n\n'
} >> "${REPORT}"

# --- A1: is there an actuator in THIS checkout? ------------------------------
# Deliberately checked in the checkout the script is running from, not in a
# fixed path: `.claude/ccy/claude-supervise.py` is TRACKED, so a feature branch
# carries whatever version its history has. A branch that predates the daemon
# upgrade has no reader half at all, and that is invisible from the writer side.
if [[ ! -r "${SUPERVISOR}" ]]; then
    record A1 "the supervisor script in this checkout" "ABSENT at ${SUPERVISOR}"
    printf '\nNo actuator in this checkout, so nothing below can be established.\n' >> "${REPORT}"
    exit 0
fi
record A1 "the supervisor script in this checkout" "present"

# --- A2: does it carry the reader, and is the reader WIRED IN? ---------------
# Two different facts. A function that exists but is never called consumes
# nothing, and a grep for its definition would pass either way — so the call
# sites are counted separately from the definition.
reader_def_count="$(count_matches '^def load_operator_signal' "${SUPERVISOR}")"
if [[ "${reader_def_count}" -gt 0 ]]; then
    reader_def="defined"
else
    reader_def="NOT DEFINED — this checkout predates the capability"
fi
record A2a "load_operator_signal in the supervisor" "${reader_def}"

if [[ "${reader_def}" == "defined" ]]; then
    mention_count="$(count_matches 'load_operator_signal(' "${SUPERVISOR}")"
    # The definition line itself matches, so a wired-in reader has MORE than
    # one hit. Exactly one means defined and never called — consuming nothing.
    if [[ "${mention_count}" -gt 1 ]]; then
        wired="wired in (${mention_count} mentions, definition included)"
    else
        wired="DEFINED BUT NEVER CALLED (${mention_count} mention)"
    fi
    record A2b "is the reader actually called" "${wired}"
else
    {
        printf '\nThe reader is not defined here, so the remaining facts describe a\n'
        printf 'contract this checkout cannot honour. Reported, not asserted.\n'
    } >> "${REPORT}"
fi

# --- A3: do the two independent copies of the contract AGREE? ---------------
# The suffix and the kind strings are duplicated by necessity. Each side's own
# literals are extracted and the SETS compared, rather than checking that one
# side contains a string hardcoded in this probe — a third copy here would be
# one more thing to drift.
extract_kinds() {
    local file="$1" out status
    if out="$(grep -oE '"(reboot-warning|shutdown-warning|reboot-cancelled)"' "${file}")"; then
        status=0
    else
        status=$?
    fi
    if [[ "${status}" -gt 1 ]]; then
        printf '[FATAL] grep failed (exit %s) reading %s\n' "${status}" "${file}" >&2
        exit 1
    fi
    printf '%s' "${out}" | tr -d '"' | sort -u | paste -sd, -
}

if [[ -r "${WRITER_MODULE}" ]]; then
    writer_kinds="$(extract_kinds "${WRITER_MODULE}")"
    reader_kinds="$(extract_kinds "${SUPERVISOR}")"
    record A3a "kind strings the WRITER module defines" "${writer_kinds:-<none>}"
    record A3b "kind strings the READER script defines" "${reader_kinds:-<none>}"
    if [[ "${writer_kinds}" == "${reader_kinds}" ]] && [[ -n "${writer_kinds}" ]]; then
        record A3c "do the two sets agree" "YES"
    else
        record A3c "do the two sets agree" \
            "NO — a kind only one side knows is written and never rendered"
    fi
else
    record A3 "the writer module could not be read for comparison" "absent at ${WRITER_MODULE}"
fi

# --- A4: who composes the words the agent sees? -----------------------------
# The security argument for this channel is that the host names a kind and a
# number and NOTHING ELSE, with the sentence composed inside the container. If
# the renderer lives on the reader side, a host-side caller cannot put words in
# front of an agent even if it wants to.
renderer_count="$(count_matches '_render_operator_message' "${SUPERVISOR}")"
if [[ "${renderer_count}" -gt 0 ]]; then
    record A4 "where the agent-visible wording is composed" \
        "in the reader (_render_operator_message, ${renderer_count} mentions)"
else
    record A4 "where the agent-visible wording is composed" "no renderer found in the reader"
fi

# --- A5: what this probe CANNOT establish -----------------------------------
# Named explicitly, because a report that simply omits them reads as if every
# question had been answered. Live delivery needs a real ccy session with a real
# supervisor attached, which exists only on the host.
{
    printf -- '- **A5** — NOT ESTABLISHED here, and not establishable in a container:\n'
    printf -- '  - that a LIVE session is interrupted and shown the sentence\n'
    printf -- '  - that the supervisor picks a signal up within its poll interval\n'
    printf -- '  - that a session started BEFORE the signal still receives it\n'
    printf -- '  - These need a host with a real ccy session attached, so they belong\n'
    printf -- '    to a host run rather than to this probe.\n'
} >> "${REPORT}"
printf 'FACT A5: live delivery is NOT established here (needs a host session).\n'

printf '\n' >> "${REPORT}"
