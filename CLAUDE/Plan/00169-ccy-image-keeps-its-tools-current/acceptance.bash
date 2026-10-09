#!/usr/bin/env bash
# Plan 00169 — acceptance.bash (Task 1.3): a ccy session whose container git cannot read the
# repository stops, printing git's own message; a readable one starts.
#
# Run on the HOST, as the desktop user, AFTER deploy.bash (which runs this as its last leg).
#
# WHAT IT DOES: checks the deployed entrypoint and launcher are the checkout's and the
# claude-yolo:latest image carries the Dockerfile's version label; then makes two throwaway
# repositories in this run's directory under untracked/ and launches the deployed ccy in each,
# headless, with --no-ssh --no-network --no-restore --no-supervise and a one-word haiku prompt:
#   refused  core.repositoryformatversion=1 and extensions.relativeWorktrees=true, which a
#            git older than 2.48 refuses. With the image's git below 2.48 the launch must exit
#            non-zero and its output must carry git's "unknown repository extension" line and
#            the entrypoint's "cannot read /workspace" line, with no answer from Claude. With
#            the image's git at 2.48 or later (a Fedora base, Phase 3) the same repository must
#            open and the session must answer: that is the Task 7.2 check, and the refusal is
#            then covered only by scripts/test-ccy-git-preflight.bash, which the coverage line
#            says.
#   clean    a plain `git init`: the launch must exit 0, the entrypoint must say which git read
#            /workspace, and Claude must answer.
# Both repositories are removed on the way out; the launch output stays in the run directory.
#
# TOKEN: a headless launch never asks, so it needs a token by name. --token NAME, or else the
# token this checkout's ccy last launched with (LAST_TOKEN in .claude/ccy/.last-launch.conf,
# read as text, never sourced). Each launch costs one short haiku turn.
#
# Usage: ./acceptance.bash [-h|--help] [--token NAME]
#
# EXIT CODES: 0 every check passed; 1 at least one failed or could not be established; 64 usage.
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

PLAN_USAGE="usage: acceptance.bash [-h|--help] [--token NAME]

Checks, on the HOST after deploy.bash: the deployed entrypoint and launcher are this
checkout's, the image carries the Dockerfile's version label, a throwaway repository the
image's git cannot read stops the ccy launch with git's own message, and a clean throwaway
repository starts a session. --token NAME picks the ccy token (default: the one this
checkout's ccy last launched with)."

plan_mode gather
plan_parse_common_flags "$@"

tokenName=""
expectToken=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    if [[ "${expectToken}" -eq 1 ]]; then
        tokenName="${arg}"
        expectToken=0
        continue
    fi
    case "${arg}" in
        --token) expectToken=1 ;;
        --token=*) tokenName="${arg#--token=}" ;;
        *)
            printf '[FATAL] unknown argument: %s\n' "${arg}" >&2
            printf '%s\n' "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${expectToken}" -eq 1 ]]; then
    printf '[FATAL] --token needs a NAME\n%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it launches the deployed ccy, which starts rootless containers on the host"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user: ccy refuses root and its containers are per user\n' >&2
    exit 1
fi
plan_start_log auto

readonly CCY_LAUNCHER="/var/local/claude-yolo/claude-yolo"
readonly ENTRYPOINT_DEPLOYED="/opt/claude-yolo/entrypoint.sh"
readonly IMAGE="claude-yolo:latest"
readonly ENGINE="${CCY_CONTAINER_ENGINE:-podman}"   # the launcher's own default (lib/common.bash)
readonly GIT_FLOOR="2.48"
readonly PROMPT="Reply with the single word READY and nothing else."
readonly LAUNCH_TIMEOUT_S=1800
sourceDir="${PLAN_REPO_ROOT}/files/var/local/claude-yolo"
reposDir="${PLAN_RUN_DIR}/repos"

total=0
passed=0
failed=0
notEstablished=()
check() {
    local label="$1" ok="$2"
    total=$((total + 1))
    if [[ "${ok}" == "yes" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "${label}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s\n' "${label}"
    fi
}
yes_if() { if "$@"; then echo yes; else echo no; fi; }

# answered <file> — "yes" when Claude's reply is a line of its own. The launcher echoes the
# claude command line, prompt included, before every start, so a bare grep for READY always hits.
answered() { yes_if grep -qxE '[[:space:]]*READY\.?[[:space:]]*' "$1"; }

# version_at_least <have> <floor> — sort -V puts the smaller first.
version_at_least() {
    [[ "$(printf '%s\n%s\n' "$2" "$1" | sort -V | head -n 1)" == "$2" ]]
}

remove_repos() {
    if [[ -d "${reposDir}" ]]; then
        rm -rf -- "${reposDir}"
    fi
}
plan_on_cleanup remove_repos

# launch <name> — the deployed ccy, headless, in ${reposDir}/<name>. Writes the combined
# output to ${PLAN_RUN_DIR}/<name>.out and the exit status to LAUNCH_STATUS.
LAUNCH_STATUS=0
launch() {
    local name="$1"
    printf '==> launching ccy in the %s repository (up to %s s; an image rebuild is slow)\n' \
        "${name}" "${LAUNCH_TIMEOUT_S}"
    if (cd -- "${reposDir}/${name}" && exec timeout "${LAUNCH_TIMEOUT_S}" "${CCY_LAUNCHER}" \
        --headless --no-restore --no-supervise --no-ssh --no-network --token "${tokenName}" \
        --prompt "${PROMPT}" -- --model haiku) </dev/null >"${PLAN_RUN_DIR}/${name}.out" 2>&1; then
        LAUNCH_STATUS=0
    else
        LAUNCH_STATUS=$?
    fi
    printf '==> exit status %s; output in %s\n' "${LAUNCH_STATUS}" "${PLAN_RUN_DIR}/${name}.out"
}

printf '=== deployed files and the image ===\n'
deployedPairs=("${sourceDir}/entrypoint.sh:${ENTRYPOINT_DEPLOYED}" "${sourceDir}/claude-yolo:${CCY_LAUNCHER}")
for lib in "${sourceDir}"/lib/*.bash; do
    deployedPairs+=("${lib}:${CCY_LAUNCHER%/*}/lib/${lib##*/}")
done
for pair in "${deployedPairs[@]}"; do
    src="${pair%%:*}"
    dst="${pair#*:}"
    check "${dst} is deployed and identical to the checkout" "$(yes_if cmp -s "${src}" "${dst}")"
done
wantVersion="$(awk -F'"' '/^LABEL claude-yolo-version=/ {print $2}' "${sourceDir}/Dockerfile")"
haveVersion=""
if "${ENGINE}" image exists "${IMAGE}"; then
    haveVersion="$("${ENGINE}" image inspect --format '{{index .Config.Labels "claude-yolo-version"}}' "${IMAGE}")"
fi
check "${IMAGE} carries claude-yolo-version ${wantVersion} (has: ${haveVersion:-absent})" \
    "$(yes_if test -n "${wantVersion}" -a "${haveVersion}" = "${wantVersion}")"

printf '=== git versions ===\n'
hostGit="$(git --version | awk '{print $3}')"
check "the host's git ${hostGit} is ${GIT_FLOOR} or later (it must read the refused repository)" \
    "$(yes_if version_at_least "${hostGit}" "${GIT_FLOOR}")"
imageGit=""
if imageGitOut="$("${ENGINE}" run --rm --network none --entrypoint git "${IMAGE}" --version 2>&1)"; then
    imageGit="$(awk '{print $3}' <<<"${imageGitOut}")"
    printf "  the image's git: %s\n" "${imageGit}"
else
    check "git --version runs in ${IMAGE} (${imageGitOut})" no
fi

printf '=== the token ===\n'
if [[ -z "${tokenName}" ]]; then
    launchConf="${PLAN_REPO_ROOT}/.claude/ccy/.last-launch.conf"
    if [[ -r "${launchConf}" ]]; then
        tokenName="$(awk -F'"' '/^LAST_TOKEN=/ {print $2}' "${launchConf}")"
    fi
fi
check "a ccy token is named (--token NAME, or this checkout's last launch)" \
    "$(yes_if test -n "${tokenName}")"

if [[ "${failed}" -ne 0 || -z "${imageGit}" ]]; then
    notEstablished+=("both launches: a precondition above failed")
else
    mkdir -p -- "${reposDir}"

    printf '=== refused: extensions.relativeWorktrees under repositoryformatversion 1 ===\n'
    git init -q "${reposDir}/refused"
    git -C "${reposDir}/refused" config core.repositoryformatversion 1
    git -C "${reposDir}/refused" config extensions.relativeWorktrees true
    launch refused
    refusedOut="${PLAN_RUN_DIR}/refused.out"
    if version_at_least "${imageGit}" "${GIT_FLOOR}"; then
        check "the image's git ${imageGit} opens it: the launch exits 0" \
            "$(yes_if test "${LAUNCH_STATUS}" -eq 0)"
        check "Claude answered" "$(answered "${refusedOut}")"
        notEstablished+=("the refusal itself: the image's git ${imageGit} reads this repository, so only scripts/test-ccy-git-preflight.bash exercises it")
    else
        check "the image's git ${imageGit} refuses it: the launch exits non-zero" \
            "$(yes_if test "${LAUNCH_STATUS}" -ne 0)"
        check "the output carries git's 'unknown repository extension' line" \
            "$(yes_if grep -q 'unknown repository extension' "${refusedOut}")"
        check "the output carries the entrypoint's 'cannot read /workspace' line" \
            "$(yes_if grep -q "cannot read /workspace" "${refusedOut}")"
        check "Claude did not answer" "$(yes_if test "$(answered "${refusedOut}")" = no)"
    fi

    printf '=== clean: a plain git init ===\n'
    git init -q "${reposDir}/clean"
    launch clean
    cleanOut="${PLAN_RUN_DIR}/clean.out"
    check "the launch exits 0" "$(yes_if test "${LAUNCH_STATUS}" -eq 0)"
    check "the entrypoint says which git read /workspace" \
        "$(yes_if grep -q '✓ git version .* reads /workspace' "${cleanOut}")"
    check "Claude answered" "$(answered "${cleanOut}")"
fi

remove_repos # the same call an interrupted run makes through plan_on_cleanup

printf '\nCOVERAGE: %d of %d checks passed\n' "${passed}" "${total}"
if [[ "${#notEstablished[@]}" -gt 0 ]]; then
    printf 'NOT ESTABLISHED:\n'
    printf '  - %s\n' "${notEstablished[@]}"
fi
if [[ "${failed}" -ne 0 ]]; then
    PLAN_FAILED_LEGS="checks"
fi
plan_finish
