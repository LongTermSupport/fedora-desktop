#!/usr/bin/env bash
# Plan 00254 — acceptance.bash: the verdict on Task 5.5, each team that opens in Lightning.
#
# RUN ON THE HOST, as the desktop user (user_login, not root), after
# play-agent-bus-element.yml: through CLAUDE/Plan/meta-deploy.bash (its entry follows the
# play's), or ./CLAUDE/Plan/00254-lightning-default-team-client/acceptance.bash
#
# READ-ONLY: it reads the host vars, the desktop user's Lightning folders and launchers, and
# each https team's approval, and starts Lightning's sandbox once per https team for a TLS
# handshake (no window). It changes nothing and needs no sudo. Safe to re-run.
#
# WHICH TEAMS: acceptance_lightning.py reads environment/localhost/host_vars/localhost.yml
# through the play's own readers (helpers.agent_bus.declare for the lists, element.py's
# parse_profiles for the entries) and resolves each team's client as the play does: its
# client:, else agent_bus_matrix_client, else lightning; a team declared absent is left out.
# It refuses a user holding ~/.config/pingbus/ bundles, by element.py's check_user.
#
# CHECKS, for each lightning team (each named by team and file when it fails):
#   - teams/, teams/<team>/ and its data, config and cache folders under
#     ~/.var/app/org.lightning_matrix.Lightning/: real directories, this user's, mode 0700;
#   - its settings file config/MatrixClient/matrix-client.conf: a regular file, mode 0600,
#     whose [homeserver] url is the team's base_url, or Lightning's canonical form of it (the
#     same scheme, host, port and path, with a default port or a trailing slash added or
#     dropped: Lightning rewrites the key once it has discovered the server);
#   - its launcher ~/.local/share/applications/agent-bus-<team>-lightning.desktop: mode 0644,
#     with exactly the Exec line agent-bus-lightning writes;
#   - no agent-bus-<team>-element.desktop launcher;
#   - for an https team (a ca_file in its entry, or an https approved url): <team>.pem in
#     Lightning's teamchat-ca folder, a PEM certificate, mode 0644; the tls-trust-check the play
#     built; and a passing handshake to the team's url from Lightning's sandbox, in the play's
#     form (flatpak run --command=env with SSL_CERT_FILE and SSL_CERT_DIR).
# And for each element team: no agent-bus-<team>-lightning.desktop launcher.
# NOT ESTABLISHABLE by script, listed for the owner at the end: each window opens on its own
# login page with the homeserver pre-filled, and signing in works.
#
# EXIT CODES: 0 ACCEPTED (every check passed); 1 REJECTED (a check failed, or the run was
# refused: root, a user holding pingbus bundles, an entry the play would refuse); 2 COULD NOT
# ESTABLISH (the host vars or an approval could not be read, or no declared team opens in
# Lightning, so there is nothing to check); 64 usage.
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
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
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Plan 00254 Task 5.5: for each declared team that opens in Lightning, its instance folders,
settings file and launcher as agent-bus-lightning writes them, no Element launcher beside it,
and for an https team its CA and a handshake from Lightning's sandbox; for each Element team,
no Lightning launcher. Read-only, as the desktop user, after play-agent-bus-element.yml.
Exit 0 ACCEPTED, 1 REJECTED, 2 COULD NOT ESTABLISH (nothing readable, or no Lightning team),
64 usage."

plan_mode gather
plan_parse_common_flags "$@"
if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n%s\n' "${PLAN_REMAINING_ARGS[*]}" "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has nothing to rehearse here: every check only reads\n' >&2
    exit 64
fi

plan_require_host "it reads the desktop user's Lightning instances and runs Lightning's Flatpak sandbox"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the instances are that user'"'"'s\n' >&2
    exit 1
fi
plan_start_log auto

readonly HOST_VARS="${PLAN_REPO_ROOT}/environment/localhost/host_vars/localhost.yml"
# paths.StatusRoot/teams in src-go/internal/teamchat/paths, as the play passes it.
readonly APPROVED_ROOT="/var/lib/teamchat-status/teams"
readonly CHECKER="${PLAN_SCRIPT_DIR}/acceptance_lightning.py"
readonly FINDINGS="${PLAN_RUN_DIR}/acceptance-findings.tsv"
readonly REPORT="${PLAN_RUN_DIR}/acceptance-report.txt"

printf '\n==> [check] the instances, settings files and launchers (acceptance_lightning.py)\n'
checkStatus=0
python3 -I "${CHECKER}" check --file "${HOST_VARS}" --approved-root "${APPROVED_ROOT}" >"${FINDINGS}" ||
    checkStatus=$?
cat "${FINDINGS}"
cp "${FINDINGS}" "${REPORT}"
case "${checkStatus}" in
    0 | 1) ;;
    2)
        printf 'COULD NOT ESTABLISH: the reason is above; nothing was judged\n' | tee -a "${REPORT}" >&2
        exit 2
        ;;
    *)
        printf 'REJECTED: acceptance_lightning.py exited %s\n' "${checkStatus}" | tee -a "${REPORT}" >&2
        exit 1
        ;;
esac

handshakeFailed=0
if grep -q '^HANDSHAKE' "${FINDINGS}"; then
    printf '\n==> [check] a TLS handshake from Lightning'"'"'s sandbox to each https team\n'
    command -v flatpak >/dev/null || {
        printf 'REJECTED: flatpak is not installed; play-agent-bus-element.yml installs Lightning with it\n' |
            tee -a "${REPORT}" >&2
        exit 1
    }
    export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/run/user/${EUID}}"
    while IFS=$'\t' read -r kind team url caDir checkDir appId; do
        [[ "${kind}" == "HANDSHAKE" ]] || continue
        out="${PLAN_RUN_DIR}/handshake-${team}.txt"
        if flatpak run --command=env "--filesystem=${checkDir}:ro" "${appId}" \
            SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt "SSL_CERT_DIR=${caDir}" \
            "${checkDir}/tls-trust-check" --url "${url}" </dev/null >"${out}" 2>&1; then
            printf 'PASS\t%s\t%s\ta handshake from the sandbox to %s trusts the team CA\n' \
                "${team}" "${caDir}/${team}.pem" "${url}" | tee -a "${REPORT}"
        else
            handshakeFailed=1
            printf 'FAIL\t%s\t%s\tthe handshake from the sandbox to %s failed: %s\n' \
                "${team}" "${caDir}/${team}.pem" "${url}" "$(tr '\n' ' ' <"${out}")" | tee -a "${REPORT}"
        fi
    done <"${FINDINGS}"
fi

{
    printf '\nNOT ESTABLISHABLE by script, for the owner to check and report as PASS, FAIL or NOT TESTED:\n'
    while IFS=$'\t' read -r kind team client _; do
        [[ "${kind}" == "TEAM" && "${client}" == "lightning" ]] || continue
        printf '  - %s: open "%s team (Lightning)"; its own window opens on the login page with the homeserver filled in, and signing in works\n' \
            "${team}" "${team}"
    done <"${FINDINGS}"
    printf '  - with two or more teams open at once, each is its own window, signed in to its own homeserver only\n'
} | tee -a "${REPORT}"

if [[ "${checkStatus}" -ne 0 || "${handshakeFailed}" -ne 0 ]]; then
    printf '\nREJECTED: the FAIL lines above name each team, file and cause (report: %s)\n' "${REPORT}" | tee -a "${REPORT}" >&2
    exit 1
fi
printf '\nACCEPTED: every Lightning team'"'"'s instance, settings file and launcher is as written, and each https team'"'"'s CA is trusted (report: %s)\n' "${REPORT}" | tee -a "${REPORT}"
exit 0
