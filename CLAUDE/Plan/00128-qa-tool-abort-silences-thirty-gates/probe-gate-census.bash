#!/usr/bin/env bash
# probe-gate-census.bash — how many gates does `qa-all.bash` report, with and without
# `extensions/node_modules`? Appends a markdown section to the report named in $1.
#
# Fact-finding only. Renders no verdict, and changes nothing outside its own temp dir.
#
# Its own script rather than a function in triage.bash, per CLAUDE/PlanScriptStandards.md:
# a leg command is invoked indirectly, so a local function used that way has its whole body
# reported unreachable by `shellcheck -x` (SC2317) — and suppressions are banned.
#
# WHY A CLONE, NOT `mv extensions/node_modules aside`:
#   `extensions/node_modules` is gitignored, so a clone has never had it — the absent state
#   is reached by construction rather than manufactured. A move-it-back probe owns a window
#   in which the developer's working tree is broken, and if it dies in that window it leaves
#   it broken. This touches nothing outside its temp directory.
#
# Usage: bash probe-gate-census.bash <report-path>
set -euo pipefail

report="${1:?usage: probe-gate-census.bash <report-path>}"

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || { printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# A gate's verdict line, as helpers/qa_environment/verdicts.py parses it: one symbol, the
# stage name, a colon. Counting DISTINCT names, because a gate may emit many lines —
# `ansible-syntax` emits one per failing playbook — and a raw line count would read that as
# broad coverage.
census() {
    local capture="$1"
    grep -oE '^[✓✗⚠] [a-z0-9][a-z0-9-]*: ' "${capture}" 2>/dev/null |
        sed 's/^. //; s/: $//' | sort -u
}

run_suite() {
    local root="$1" capture="$2" rc=0
    # `|| rc=$?` and not `!`: exit 2 (tool abort), exit 1 (a gate failed) and exit 0 are
    # three different answers here and the probe reports whichever it got. Treating any
    # non-zero as "the probe failed" would discard the very distinction being measured.
    ( cd "${root}" && ./scripts/qa-all.bash ) > "${capture}" 2>&1 || rc=$?
    printf '%s' "${rc}"
}

printf '[probe] cloning the repository (this is the fresh-checkout population)...\n' >&2
git clone --quiet --no-hardlinks "${repoRoot}" "${work}/clone"

if [[ -d "${work}/clone/extensions/node_modules" ]]; then
    printf '[FATAL] the clone HAS extensions/node_modules, so it is not the population\n' >&2
    printf '        this probe exists to measure. Refusing to report a comparison that\n' >&2
    printf '        would compare a checkout with itself.\n' >&2
    exit 1
fi

printf '[probe] running qa-all.bash in the clone...\n' >&2
clone_rc="$(run_suite "${work}/clone" "${work}/clone.out")"
printf '[probe] running qa-all.bash in this checkout...\n' >&2
here_rc="$(run_suite "${repoRoot}" "${work}/here.out")"

census "${work}/clone.out" > "${work}/clone.gates"
census "${work}/here.out"  > "${work}/here.gates"
clone_n="$(wc -l < "${work}/clone.gates")"
here_n="$(wc -l < "${work}/here.gates")"

{
    # A quoted heredoc, not printf: the markdown carries backticks, and shellcheck reads a
    # backtick inside single quotes as a command substitution it cannot verify (SC2016).
    # `<<'MD'` is literal by definition, so the linter and the reader agree.
    cat <<'MD'

## Gate census: with and without `extensions/node_modules`

| Checkout | Distinct gates reporting a verdict | `qa-all.bash` exit |
| --- | --- | --- |
MD
    printf '| fresh clone (no node_modules) | %s | %s |\n' "${clone_n}" "${clone_rc}"
    printf '| this checkout | %s | %s |\n' "${here_n}" "${here_rc}"
    printf '\n**Gates that lose their verdict on a fresh clone: %s**\n\n' "$(( here_n - clone_n ))"
    printf 'Named, rather than only counted — a bare number cannot show WHICH coverage\n'
    printf 'went missing, and a coverage gap is how a text-matching census fails:\n\n'
    printf '```\n'
    comm -13 "${work}/clone.gates" "${work}/here.gates"
    printf '```\n\n'
    printf 'Reported by the fresh clone:\n\n```\n'
    cat "${work}/clone.gates"
    printf '```\n'
} >> "${report}"

printf '[probe] fresh clone %s gates (exit %s); this checkout %s gates (exit %s)\n' \
    "${clone_n}" "${clone_rc}" "${here_n}" "${here_rc}" >&2

# The probe's own health check. Equal counts mean the comparison established nothing — the
# clone somehow had the deps, or neither run produced a census — and a report saying "0
# gates lost" would read as "the problem is fixed". Zero is the signal, so it is an error.
if [[ "${clone_n}" -eq "${here_n}" ]]; then
    printf '[FATAL] both checkouts reported the same %s gates, so this probe measured\n' "${clone_n}" >&2
    printf '        nothing. That is not evidence the gap is closed — check the captures.\n' >&2
    exit 1
fi
if [[ "${here_n}" -eq 0 ]]; then
    printf '[FATAL] this checkout reported ZERO gates — qa-all.bash did not run at all\n' >&2
    exit 1
fi
