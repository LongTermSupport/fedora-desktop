#!/usr/bin/env bash
# Plan 00150 — acceptance.bash: the pass/fail gate for imgpaste on the HOST.
#
# Run on the HOST after deploy.bash. It changes nothing outside this run's own log
# directory: every image it encodes or decodes is written there.
#
# It checks what a script can establish: the deployed command is the repo's, it is on PATH,
# --help answers, the example screenshot round-trips (encode, run the block, sha256 OK,
# decoded file is WebP), and four inputs are refused with nothing on stdout and for the
# right reason: a non-image, an image too busy for the block budget, one over the pixel
# limit and one over the byte limit. It prints COVERAGE and names what it CANNOT establish.
#
# Usage: ./acceptance.bash [-h|--help]
#
# EXIT CODES: 0 every established check passed; 1 at least one failed; 64 usage error.
set -euo pipefail

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

Checks, on the HOST after deploy.bash: ~/.local/bin/imgpaste is the repo's copy and on
PATH; --help answers; the example screenshot round-trips with sha256 OK into a WebP; a
non-image and images over the block budget, the pixel limit and the byte limit are each
refused with nothing on stdout, for the stated reason. Prints COVERAGE."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "it checks the command play-cli-tools.yml deployed to ~/.local/bin"

plan_start_log auto

deployed="${HOME}/.local/bin/imgpaste"
source_copy="${PLAN_REPO_ROOT}/files/home/.local/bin/imgpaste"
fixture="${PLAN_SCRIPT_DIR}/assets/example-terminal-screenshot.png"
work="${PLAN_RUN_DIR}/work"
mkdir -p "${work}"

total=0
passed=0
failed=0

check() {
    local label="${1}" ok="${2}"
    total=$((total + 1))
    if [[ "${ok}" == "yes" ]]; then
        passed=$((passed + 1))
        printf '  PASS: %s\n' "${label}"
    else
        failed=$((failed + 1))
        printf '  FAIL: %s\n' "${label}" >&2
    fi
}

# --- the deployed command is the repo's, executable, and the one PATH finds ----------------
if [[ -x "${deployed}" ]] && cmp -s "${deployed}" "${source_copy}"; then
    check "${deployed} is executable and identical to the repo copy" yes
else
    check "${deployed} is executable and identical to the repo copy (run deploy.bash)" no
fi

resolved=""
if resolved="$(command -v imgpaste)" && [[ "${resolved}" == "${deployed}" ]]; then
    check "imgpaste on PATH resolves to ${deployed}" yes
else
    check "imgpaste on PATH resolves to ${deployed} (got: ${resolved:-nothing})" no
fi

if "${deployed}" --help > "${work}/help.txt" && [[ -s "${work}/help.txt" ]]; then
    check "imgpaste --help exits 0 and prints usage" yes
else
    check "imgpaste --help exits 0 and prints usage" no
fi

# --- the example round-trips: encode, run the block where it lands, verify ----------------
# The block decodes into its current directory, so it is run from inside the work dir.
roundtrip="no"
if "${deployed}" "${fixture}" > "${work}/block.txt" 2> "${work}/encode.stderr" \
    && verify="$(cd "${work}" && bash "${work}/block.txt" 2>&1)" \
    && [[ "${verify}" == *": OK"* ]]; then
    decoded=("${work}"/imgpaste-*.webp)
    if [[ -f "${decoded[0]}" ]] && [[ "$(file --brief --mime-type -- "${decoded[0]}")" == "image/webp" ]]; then
        roundtrip="yes"
    fi
fi
check "the example screenshot round-trips: block runs, sha256 OK, decoded file is image/webp" "${roundtrip}"

# --- refusals: non-zero, nothing on stdout, and refused for the RIGHT reason -----------------
# Nothing on stdout means a refused run can never be pasted as a block. The stderr reason is
# asserted too: without it, any failure (a broken ImageMagick, say) would read as a pass.
refused() {
    local label="${1}" input="${2}" reason="${3}" name
    name="$(basename "${input}")"
    if "${deployed}" "${input}" > "${work}/${name}.out" 2> "${work}/${name}.err"; then
        check "${label} is refused (it was accepted)" no
    elif [[ -s "${work}/${name}.out" ]]; then
        check "${label} is refused with nothing on stdout (stdout was not empty)" no
    elif ! grep -q -F -e "${reason}" "${work}/${name}.err"; then
        check "${label} is refused for '${reason}' (stderr: $(head -c 200 "${work}/${name}.err"))" no
    else
        check "${label} is refused, nothing on stdout, reason '${reason}'" yes
    fi
}

printf 'not an image\n' > "${work}/not-an-image.txt"
refused "a text file" "${work}/not-an-image.txt" "not an image"

magick -size 2000x2000 xc: +noise Random "${work}/noise.png"
refused "random noise (no step fits the block budget)" "${work}/noise.png" "no setting fits"

magick -size 9000x9000 xc:white "${work}/over-pixels.png"
refused "a 9000x9000 image (over the pixel limit)" "${work}/over-pixels.png" "9000x9000, limit is"

# A valid PNG header padded to 51 MiB: `file` still calls it an image, so the byte limit,
# not the mime check, is what refuses it. truncate makes it sparse, so it costs no disk.
cp "${fixture}" "${work}/over-bytes.png"
truncate -s 51M "${work}/over-bytes.png"
refused "a 51 MiB image (over the byte limit)" "${work}/over-bytes.png" "bytes, limit is"

printf '\nCOVERAGE: %d of %d established checks passed\n' "${passed}" "${total}"
printf '\nNOT ESTABLISHABLE by this script (a human must confirm, Task 1.4):\n'
printf '  - that an agent in a fresh session runs a pasted block from its instruction line alone\n'
printf '  - that an agent can read the decoded image (Read tool), not just that it is valid WebP\n'
printf '\n==> run log: %s\n' "${PLAN_RUN_LOG}"

if [[ "${failed}" -gt 0 ]]; then
    printf '==> %d check(s) FAILED\n' "${failed}" >&2
    exit 1
fi
printf '==> all established checks passed\n'
