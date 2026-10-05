#!/usr/bin/env bash
# Make a release: a signed tag <fedora-major>.<minor>.<patch> on a release branch F<major>
# (Plan 00153). The major comes from the branch name (F44 gives 44); the owner chooses minor
# or patch. Agents never run this: a release is the owner's call.
#
# Two steps, because the release branch is protected (changes reach it only by pull request):
#
#   release.bash prepare first|minor|patch [--yes] [--dry-run]
#       On a clean F<major> that matches its remote and whose CI passed, writes the changelog
#       entry (the commit subjects since the last tag, for the owner to edit), commits it
#       signed on branch release-<version>, pushes the branch and opens a pull request.
#       `first` is the major's first release (<major>.0.0) and is refused once any exists.
#
#   release.bash tag <version> [--yes] [--dry-run]
#       After that pull request is merged (with a merge commit) and F<major> is pulled: signs
#       the annotated tag on the signed `Release <version>` commit, not on GitHub's merge
#       commit (the self-update trusts only owner-signed commits), pushes it and creates the
#       GitHub Release from the changelog entry. Refused if the release commit is unsigned or
#       its CI did not pass. If the Release step fails after the tag is pushed, run it again:
#       it finishes the Release.
#
# Signatures are checked against the owner's own signing key (git's user.signingkey, with
# user.email as the principal), so no allowed-signers file has to exist on this machine.
#
# Rules and the self-update that consumes the tags:
# CLAUDE/Plan/00153-release-tags-fedora-major-semver/
#
# EXIT CODES: 0 done; 1 refused (the message says why); 64 usage error.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

usage() {
    cat >&2 <<'USAGE'
usage: scripts/release.bash prepare first|minor|patch [--yes] [--dry-run]
       scripts/release.bash tag <major>.<minor>.<patch> [--yes] [--dry-run]
       scripts/release.bash -h|--help
USAGE
}

refuse() {
    printf '[REFUSED] %s\n' "$*" >&2
    exit 1
}

die_usage() {
    printf '[USAGE] %s\n' "$*" >&2
    usage
    exit 64
}

assume_yes=0
dry_run=0
positional=()
for arg in "$@"; do
    case "${arg}" in
        -h | --help)
            usage
            exit 0
            ;;
        --yes) assume_yes=1 ;;
        --dry-run) dry_run=1 ;;
        -*) die_usage "unknown option ${arg}" ;;
        *) positional+=("${arg}") ;;
    esac
done

step="${positional[0]:-}"
argument="${positional[1]:-}"
case "${step}" in
    prepare)
        case "${argument}" in
            first | minor | patch) ;;
            *) die_usage "prepare takes first, minor or patch (major follows the Fedora release)" ;;
        esac
        ;;
    tag)
        [[ "${argument}" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] \
            || die_usage "tag takes a version like 44.1.0"
        ;;
    *) die_usage "the first argument is prepare or tag" ;;
esac

# confirm PROMPT: ask the owner, re-asking on a typo (three tries); --yes answers for them.
confirm() {
    local answer attempt
    [[ "${assume_yes}" -eq 1 ]] && return 0
    [[ -t 0 ]] || refuse "needs a terminal to confirm (or pass --yes)"
    for attempt in 1 2 3; do
        read -r -p "$1 [y/n] " answer || refuse "no answer (end of input)"
        case "${answer}" in
            y | Y | yes) return 0 ;;
            n | N | no) refuse "stopped at the owner's request" ;;
            *) printf 'please answer y or n (try %d of 3)\n' "${attempt}" >&2 ;;
        esac
    done
    refuse "no valid answer"
}

# ci_green SHA: at least one finished run for the commit passed, and none failed or is running.
ci_green() {
    local runs
    runs="$(gh run list --commit "$1" --workflow qa.yml --json status,conclusion)"
    python3 - "${runs}" <<'PY'
import json
import sys

runs = json.loads(sys.argv[1])
if any(r["status"] != "completed" for r in runs):
    sys.exit(1)
if any(r["conclusion"] not in ("success", "cancelled", "skipped") for r in runs):
    sys.exit(1)
sys.exit(0 if any(r["conclusion"] == "success" for r in runs) else 1)
PY
}

signersFile=""
entryFile=""
releaseBranch=""
committed=1
branch=""

# On any exit: remove the temp files and, if `prepare` stopped before its commit, put the tree
# back as it was (the changelog restored, the release branch removed).
cleanup() {
    [[ -z "${signersFile}" ]] || rm -f "${signersFile}"
    [[ -z "${entryFile}" ]] || rm -f "${entryFile}"
    if [[ "${committed}" -eq 0 ]]; then
        if git cat-file -e "${branch}:CHANGELOG.md"; then
            git checkout --quiet "${branch}" -- CHANGELOG.md
        else
            rm -f CHANGELOG.md
        fi
        git switch --quiet "${branch}"
        git branch --quiet -d "${releaseBranch}"
    fi
}
trap cleanup EXIT

# signed_git ARGS...: git, verifying signatures against the owner's own signing key.
signed_git() {
    if [[ -z "${signersFile}" ]]; then
        local key email
        key="$(git config user.signingkey)" || refuse "git user.signingkey is not set; releases are signed"
        email="$(git config user.email)" || refuse "git user.email is not set"
        key="${key#key::}"
        if [[ -r "${key}" ]]; then
            key="$(cut -d' ' -f1,2 "${key}")"
        fi
        signersFile="$(mktemp)"
        printf '%s %s\n' "${email}" "${key}" >"${signersFile}"
    fi
    git -c "gpg.ssh.allowedSignersFile=${signersFile}" "$@"
}

[[ -z "$(git status --porcelain)" ]] || refuse "the working tree is not clean"

branch="$(git branch --show-current)"
[[ "${branch}" =~ ^F([0-9]+)$ ]] || refuse "releases are made on a branch named F<major> (this is '${branch}')"
major="${BASH_REMATCH[1]}"

git fetch --quiet origin "${branch}" --tags
[[ "$(git rev-list --count "HEAD..origin/${branch}")" -eq 0 ]] \
    || refuse "${branch} is behind origin/${branch}; pull first"
[[ "$(git rev-list --count "origin/${branch}..HEAD")" -eq 0 ]] \
    || refuse "${branch} is ahead of origin/${branch}; changes reach it by pull request"

# release_tags: this major's release tags, oldest first. Anything else under <major>.* (a
# release candidate, say) is not a release and is ignored, as the self-update ignores it.
release_tags() {
    git tag --list "${major}.*" \
        | grep -E "^${major}\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\$" \
        | sort -t. -k2,2n -k3,3n || [[ $? -eq 1 ]]
}

if [[ "${step}" == "prepare" ]]; then
    last="$(release_tags | tail -n 1)"
    if [[ "${argument}" == "first" ]]; then
        [[ -z "${last}" ]] || refuse "a release already exists for ${major} (${last}); use minor or patch"
        version="${major}.0.0"
    else
        [[ -n "${last}" ]] || refuse "no release yet for ${major}; make the first with: prepare first"
        IFS=. read -r _ minor patch <<<"${last}"
        if [[ "${argument}" == "minor" ]]; then
            version="${major}.$((minor + 1)).0"
        else
            version="${major}.${minor}.$((patch + 1))"
        fi
    fi
    releaseBranch="release-${version}"
    if git show-ref --quiet "refs/heads/${releaseBranch}" || git show-ref --quiet "refs/remotes/origin/${releaseBranch}"; then
        refuse "branch ${releaseBranch} already exists; finish or delete it first"
    fi

    ci_green "$(git rev-parse HEAD)" || refuse "CI has not passed on $(git rev-parse --short HEAD) (passed, finished, no failures)"

    if [[ "${dry_run}" -eq 1 ]]; then
        printf 'would prepare release %s (last release: %s)\n' "${version}" "${last:-none}"
        exit 0
    fi

    entryFile="$(mktemp)"
    {
        printf '## %s - %s\n\n' "${version}" "$(date -u +%F)"
        if [[ -z "${last}" ]]; then
            printf -- '- First release for Fedora %s.\n' "${major}"
        else
            git log --no-merges --format='- %s' "${last}..HEAD"
        fi
        printf '\n'
    } >"${entryFile}"

    # From here until the commit exists, any stop puts the tree back (see cleanup).
    committed=0
    git switch --quiet -c "${releaseBranch}"
    if [[ -f CHANGELOG.md ]]; then
        newFile="$(mktemp)"
        awk -v entry="${entryFile}" 'NR == 1 { print; print ""; while ((getline line < entry) > 0) print line; next } NR == 2 && $0 == "" { next } { print }' \
            CHANGELOG.md >"${newFile}"
        mv "${newFile}" CHANGELOG.md
    else
        { printf '# Changelog\n\n'; cat "${entryFile}"; } >CHANGELOG.md
    fi

    if [[ "${assume_yes}" -eq 0 ]]; then
        read -r -a editor <<<"${EDITOR:-vi}"
        "${editor[@]}" CHANGELOG.md
    fi
    confirm "Commit the changelog as 'Release ${version}', push ${releaseBranch} and open the pull request?"

    git add CHANGELOG.md
    git commit --quiet -S -m "Release ${version}"
    committed=1
    git push --quiet -u origin "${releaseBranch}" \
        || refuse "pushing ${releaseBranch} failed; the release commit is on that branch locally, push it and open the pull request by hand"
    gh pr create --base "${branch}" --head "${releaseBranch}" --title "Release ${version}" \
        --body "Changelog entry for ${version}. Merge with a merge commit, then run: scripts/release.bash tag ${version}" \
        || refuse "opening the pull request failed; ${releaseBranch} is pushed, open it by hand"
    printf 'merge the pull request with a merge commit, then:\n  git switch %s && git pull --ff-only && scripts/release.bash tag %s\n' "${branch}" "${version}"
    exit 0
fi

# step: tag
version="${argument}"
[[ "${version%%.*}" == "${major}" ]] || refuse "${version} belongs on branch F${version%%.*}; this is ${branch}"

releaseCommit="$(git log --fixed-strings --grep="Release ${version}" --format='%H %s' "origin/${branch}" \
    | awk -v want="Release ${version}" '{ sha = $1; $1 = ""; sub(/^ /, ""); if ($0 == want) { print sha; exit } }')"
[[ -n "${releaseCommit}" ]] || refuse "no commit 'Release ${version}' on ${branch}; run prepare and merge its pull request first"
signed_git verify-commit "${releaseCommit}" || refuse "the release commit ${releaseCommit:0:12} is not validly signed by your signing key"
ci_green "${releaseCommit}" || refuse "CI has not passed on the release commit ${releaseCommit:0:12}"

notes="$(git show "${releaseCommit}:CHANGELOG.md" | awk -v h="## ${version} " 'index($0, h) == 1 { f = 1; next } /^## / { f = 0 } f')" \
    || refuse "CHANGELOG.md is missing at the release commit"
[[ -n "${notes}" ]] || refuse "CHANGELOG.md at the release commit has no entry for ${version}"

tagExists=0
if [[ -n "$(git tag --list "${version}")" ]]; then
    tagExists=1
    [[ "$(git rev-parse "${version}^{commit}")" == "${releaseCommit}" ]] \
        || refuse "tag ${version} already exists on a different commit; a release tag is never moved"
    if ! probe="$(git ls-remote --exit-code --tags origin "refs/tags/${version}" 2>&1)"; then
        refuse "tag ${version} already exists locally but is not pushed; delete it and run again"
    fi
    if probe="$(gh release view "${version}" 2>&1)"; then
        refuse "release ${version} already exists: ${probe%%$'\n'*}"
    fi
fi

if [[ "${dry_run}" -eq 1 ]]; then
    if [[ "${tagExists}" -eq 1 ]]; then
        printf 'would create the GitHub Release for the existing tag %s\n' "${version}"
    else
        printf 'would tag %s on %s, push it and create the GitHub Release\n' "${version}" "${releaseCommit:0:12}"
    fi
    exit 0
fi

confirm "Publish release ${version} (commit ${releaseCommit:0:12})?"

if [[ "${tagExists}" -eq 0 ]]; then
    git tag -s -m "Release ${version}" "${version}" "${releaseCommit}"
    signed_git verify-tag "${version}" || {
        git tag -d "${version}" >&2
        refuse "the new tag did not verify (is the signing key configured?)"
    }
    git push --quiet origin "refs/tags/${version}" || {
        git tag -d "${version}" >&2
        refuse "pushing the tag failed; the local tag was removed, nothing is published"
    }
fi
gh release create "${version}" --verify-tag --title "${version}" --notes "${notes}" \
    || refuse "the tag ${version} is pushed but the GitHub Release failed; run this command again to finish it"
printf 'released %s\n' "${version}"
