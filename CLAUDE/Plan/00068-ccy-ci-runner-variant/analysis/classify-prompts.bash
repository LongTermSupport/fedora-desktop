#!/usr/bin/env bash
# classify-prompts.bash — reproduce the Round-3 prompt classification (Plan 00068 Task 7.3).
#
# Plan-local HELPER, not an orchestrator: it reads the claude-yolo sources in this checkout
# and prints a classification. It deploys nothing, touches no host, and needs no ssh.
#
# Every measurement here carries a check that can FAIL. That is the point: the first version
# of the block tracker silently mis-parsed claude-yolo's multi-line banner strings, and only
# the EOF-balance invariant exposed it. Do not remove a check to make this script "work".
#
# EXIT CODES:
#   0  all invariants held; classification printed
#   1  an invariant failed — the classification is NOT trustworthy, do not quote it
#  64  usage error
set -euo pipefail

scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
readonly scriptDir

if [[ $# -gt 0 ]]; then
    printf 'usage: classify-prompts.bash            (no arguments)\n' >&2
    exit 64
fi

# Repo root: script-relative marker walk, bounded by the repository boundary. Never
# `git rev-parse --show-toplevel`, which answers about the cwd (see PlanScriptStandards R1).
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    repoRoot="$(dirname "${repoRoot}")"
done
if [[ ! -e "${repoRoot}/ansible.cfg" ]]; then
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
fi
readonly repoRoot

readonly ccyDir="${repoRoot}/files/var/local/claude-yolo"
if [[ ! -d "${ccyDir}" ]]; then
    printf '[FATAL] claude-yolo sources not found at %s\n' "${ccyDir}" >&2
    exit 1
fi

workDir="$(mktemp -d)"
readonly workDir
cleanup() { rm -rf "${workDir}"; }
trap cleanup EXIT

readonly files=(
    "${ccyDir}/claude-yolo"
    "${ccyDir}/entrypoint.sh"
    "${ccyDir}/lib/common-pure.bash"
    "${ccyDir}/lib/common.bash"
    "${ccyDir}/lib/docker-health.bash"
    "${ccyDir}/lib/dockerfile-custom.bash"
    "${ccyDir}/lib/network-management.bash"
    "${ccyDir}/lib/ssh-handling.bash"
    "${ccyDir}/lib/token-management.bash"
)

failures=0
note_failure() {
    printf '  [INVARIANT FAILED] %s\n' "$1" >&2
    failures=$((failures + 1))
}

# ── 1. prompt sites ───────────────────────────────────────────────────────────────────────
# `read` with -p in ANY flag order. Round 1 required `-rp` and so could not see
# token-management.bash at all (Task 7.2).
printf '== 1. prompt sites ==\n'
awk '
  /(^|[^[:alnum:]_])read([[:space:]]+-[A-Za-z]+)*[[:space:]]+-[A-Za-z]*p([[:space:]]|$)/ {
      line = $0; sub(/^[[:space:]]*/, "", line)
      printf "%s\t%d\t%s\n", FILENAME, FNR, line
  }
' "${files[@]}" >"${workDir}/sites.tsv"

siteCount="$(awk 'END { print NR }' "${workDir}/sites.tsv")"
printf '   prompt sites found: %s\n' "${siteCount}"
if [[ "${siteCount}" -ne 46 ]]; then
    note_failure "expected 46 prompt sites, found ${siteCount} — the sources changed, re-do the classification"
fi

# The census pattern is a known-fragile control (Task 7.2): assert it against a file KNOWN to
# use `read -r -p`, so a pattern that matches nothing can never report a clean sweep again.
tokenSites="$(awk -F'\t' '$1 ~ /token-management/ { n++ } END { print n + 0 }' "${workDir}/sites.tsv")"
if [[ "${tokenSites}" -lt 1 ]]; then
    note_failure "pattern matched 0 prompts in token-management.bash, which is known to contain them"
fi

# ── 2. function boundaries, validated by re-parsing ───────────────────────────────────────
printf '== 2. function boundaries (validated with bash -n) ==\n'
python3 "${scriptDir}/fnmap.py" "${files[@]}" >"${workDir}/fnmap.tsv"

parsed=0
rejectedWhenTruncated=0
endLineNotBrace=0
while IFS=$'\t' read -r tag path name start end; do
    [[ "${tag}" == "FN" ]] || continue
    : "${name}"

    awk -v s="${start}" -v e="${end}" 'NR >= s && NR <= e' "${path}" >"${workDir}/fn.bash"
    if parseErr="$(bash -n "${workDir}/fn.bash" 2>&1)" && [[ -z "${parseErr}" ]]; then
        parsed=$((parsed + 1))
    else
        note_failure "extracted body of ${name} (${path}:${start}-${end}) does not parse: ${parseErr}"
    fi

    # Mutation control: truncating by one line MUST break it, or the check above has no teeth.
    awk -v s="${start}" -v e="$((end - 1))" 'NR >= s && NR <= e' "${path}" >"${workDir}/fn-short.bash"
    if truncErr="$(bash -n "${workDir}/fn-short.bash" 2>&1)"; then
        if [[ -n "${truncErr}" ]]; then
            rejectedWhenTruncated=$((rejectedWhenTruncated + 1))
        fi
    else
        rejectedWhenTruncated=$((rejectedWhenTruncated + 1))
    fi

    # The end is pinned from the other side: the last line must be the closing brace.
    endText="$(awk -v e="${end}" 'NR == e' "${path}")"
    if ! printf '%s\n' "${endText}" | grep -qxE '[[:space:]]*\}[[:space:]]*'; then
        endLineNotBrace=$((endLineNotBrace + 1))
        note_failure "${name} end line ${path}:${end} is not a closing brace: ${endText}"
    fi
done <"${workDir}/fnmap.tsv"

fnCount="$(awk -F'\t' '$1 == "FN" { n++ } END { print n + 0 }' "${workDir}/fnmap.tsv")"
printf '   functions: %s   bodies that re-parse: %s   reject when truncated: %s   bad end lines: %s\n' \
    "${fnCount}" "${parsed}" "${rejectedWhenTruncated}" "${endLineNotBrace}"
if [[ "${parsed}" -ne "${fnCount}" ]]; then
    note_failure "only ${parsed}/${fnCount} function bodies re-parse"
fi
if [[ "${rejectedWhenTruncated}" -ne "${fnCount}" ]]; then
    note_failure "truncation control is blunt: only ${rejectedWhenTruncated}/${fnCount} bodies break when shortened"
fi

# ── 3. block nesting must balance at EOF ──────────────────────────────────────────────────
# This is the invariant that caught the multi-line-string bug. claude-yolo builds banners as
# multi-line double-quoted strings whose PROSE contains `if`, `for` and `done`.
printf '== 3. block nesting balance ==\n'
balanced=0
for f in "${files[@]}"; do
    depth="$(PYTHONPATH="${scriptDir}" python3 -c '
import sys
from bashctx import block_stack
_lines, per_line = block_stack(sys.argv[1])
stack, _cond = per_line[max(per_line)]
print(len(stack))
' "${f}")"
    if [[ "${depth}" -ne 0 ]]; then
        note_failure "$(basename "${f}") leaves ${depth} unclosed block(s) at EOF — the tracker is mis-parsing"
    else
        balanced=$((balanced + 1))
    fi
done
printf '   files balancing at EOF: %s of %s\n' "${balanced}" "${#files[@]}"

# ── 4. suspension markers on the prompt lines themselves ──────────────────────────────────
printf '== 4. prompt-line suspension markers ==\n'
marked="$(awk -F'\t' '
  { l = $3 }
  l ~ /^(if|while|until)[[:space:]]/ || l ~ /^![[:space:]]/ || l ~ /\|\|/ || l ~ /&&/ { n++ }
  END { print n + 0 }
' "${workDir}/sites.tsv")"
printf '   sites carrying their own suspension marker: %s of %s\n' "${marked}" "${siteCount}"
printf '   => errexit state at each read is decided ENTIRELY by the call context.\n'

printf '\n'
if [[ "${failures}" -ne 0 ]]; then
    printf '%s invariant(s) FAILED — the classification is not trustworthy.\n' "${failures}" >&2
    exit 1
fi
printf 'All invariants held. Classification: reports/prompt-classification-round3.md\n'
