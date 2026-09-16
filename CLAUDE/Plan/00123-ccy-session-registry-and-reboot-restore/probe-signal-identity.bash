#!/usr/bin/env bash
#
# probe-signal-identity.bash — record WHICH daemon and WHICH checkout answered.
#
# Every other fact in this report is a property of a particular installed
# version and a particular branch. Without those two identifiers the report is
# a set of readings with no subject: re-run it after an upgrade or on another
# branch and it would look like the same evidence about a different thing.
#
# FACT-FINDING ONLY (R9). Reads only; changes nothing.
#
# Usage: probe-signal-identity.bash <report-file>
set -euo pipefail

REPORT="${1:?usage: probe-signal-identity.bash <report-file>}"

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

record() {
    local id="$1" what="$2" value="$3"
    printf -- '- **%s** — %s\n  - %s%s%s\n' \
        "${id}" "${what}" "${BT}" "${value}" "${BT}" >> "${REPORT}"
    printf 'FACT %s: %s -> %s\n' "${id}" "${what}" "${value}"
}

{
    printf '\n## What answered\n\n'
} >> "${REPORT}"

DAEMON_DIR="${repoRoot}/.claude/hooks-daemon"

# The installed daemon version. `git describe` in the daemon clone is the
# reading the upgrade itself uses, and the clone is a real checkout of a tag.
if [[ -e "${DAEMON_DIR}/.git" ]]; then
    daemon_ref="$(git -C "${DAEMON_DIR}" describe --tags --always)"
else
    daemon_ref="<no git metadata in ${DAEMON_DIR}>"
fi
record I1 "installed hooks-daemon version" "${daemon_ref}"

# Which checkout this ran in. The supervisor script is TRACKED, so the branch
# determines whether the reader half is present at all — see the actuator probe.
branch="$(git -C "${repoRoot}" rev-parse --abbrev-ref HEAD)"
head_sha="$(git -C "${repoRoot}" rev-parse --short HEAD)"
record I2 "checkout that ran this triage" "branch=${branch} head=${head_sha}"
record I3 "repository root resolved by the marker walk" "${repoRoot}"

# Is this a linked worktree? It changes which tracked files are in play, and
# the answer is structural rather than a matter of the path's name.
if [[ -f "${repoRoot}/.git" ]]; then
    record I4 "checkout kind" "linked git worktree"
else
    record I4 "checkout kind" "main checkout"
fi

printf '\n' >> "${REPORT}"
