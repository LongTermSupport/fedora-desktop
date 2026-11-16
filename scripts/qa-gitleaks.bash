#!/usr/bin/env bash
# qa-gitleaks.bash — CI's gitleaks secret scan, run before a push (issue #329), over exactly what
# CI would scan: a checkout of the commit. Two modes:
#   qa-gitleaks.bash                  what a commit of this work tree would carry: the tracked
#                                     files as they are on disk and the untracked files git
#                                     would add; never an ignored file (untracked/), which no
#                                     checkout has. qa-all and qa-changed run this.
#   qa-gitleaks.bash push REMOTE URL  each pushed commit's own tree (git's ref lines on stdin);
#                                     the pre-push hook runs this.
# Each tree is copied into a scratch directory and scanned there with CI's command,
# `gitleaks dir . --redact --no-banner --verbose`, which reads the tree's own .gitleaks.toml.
# Findings are redacted. Exit codes: 0 no secret, 1 a secret, 2 bad usage or no verdict.
set -euo pipefail

die() {
    echo "qa-gitleaks: $*" >&2
    exit 2
}

# scan DIR WHAT runs gitleaks over DIR; a secret is status 1, a scan that failed is exit 2.
scan() {
    local rc=0
    (cd "$1" && gitleaks dir . --redact --no-banner --verbose --exit-code 3) || rc=$?
    case "${rc}" in
        0) return 0 ;;
        3)
            echo "✗ qa-gitleaks: gitleaks found a secret in $2 (redacted above). Remove it; a value that is no secret is fixed in .gitleaks.toml (CLAUDE/QA.md gitleaks-config-not-as-written)." >&2
            return 1
            ;;
        *) die "gitleaks could not scan $2 (exit ${rc})" ;;
    esac
}

# work_tree copies into DIR the files a commit of the work tree would carry.
work_tree() {
    git ls-files -z --cached --others --exclude-standard |
        tar --null --no-recursion --ignore-failed-read -cf - --files-from=- |
        tar -xf - -C "$1"
}

# pushed scans each pushed commit's tree under DIR; a deleted ref has no tree to scan.
pushed() {
    local dir="$1" localref sha remoteref remotesha n=0 found=0
    while read -r localref sha remoteref remotesha; do
        [[ -n "${localref}" && -n "${remotesha}" ]] || die "a ref line git did not write: ${localref} ${sha} ${remoteref}"
        [[ "${sha}" =~ ^0+$ ]] && continue
        n=$((n + 1))
        mkdir "${dir}/${n}"
        git archive "${sha}" | tar -xf - -C "${dir}/${n}"
        scan "${dir}/${n}" "${sha} (to ${remoteref})" || found=1
    done
    [[ "${found}" -eq 0 ]] || exit 1
    echo "GITLEAKS-OK no secret in ${n} pushed commit(s) (gitleaks ${version})"
}

case "${1-}" in
    "") mode=tree ;;
    push) mode=push ;;
    *) die "usage: qa-gitleaks.bash [push REMOTE URL]" ;;
esac
command -v gitleaks >/dev/null ||
    die "gitleaks is not installed: on the host run ./playbooks/imports/play-python.yml; in a ccy container, ccy --rebuild"
version="$(gitleaks version)"
unset GITLEAKS_CONFIG GITLEAKS_CONFIG_TOML
cd "$(git rev-parse --show-toplevel)"
scratch="$(mktemp -d)"
trap 'rm -rf "${scratch}"' EXIT
if [[ "${mode}" == push ]]; then
    pushed "${scratch}"
    exit 0
fi
work_tree "${scratch}"
scan "${scratch}" "the work tree" || exit 1
echo "GITLEAKS-OK no secret in the work tree (gitleaks ${version})"
