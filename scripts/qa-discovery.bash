#!/usr/bin/bash
# Shared source-file discovery for the QA gates. SOURCE this; do not run it.
#
# `qa-bash.bash` (bash -n + shellcheck) and `qa-python.bash` must agree on what "a repo-owned
# shell script" and "a repo-owned Python file" mean, and each measures its own coverage against
# the tracked files (Plan 00076, Plan 00081: files outside every gate while every gate reported
# a pass). One definition, so no gate holds a second opinion.
#
# The definition is Go (src-go/qa/discovery, command src-go/qa/cmd/qa-discovery); lib/go-tool.bash
# builds it into a private directory under the cache and runs it. Specification:
# CLAUDE/Plan/00192-.../SPEC-qa-discovery.md. It lists what git would publish, never gitignored files.
#
# Provides:
#   qa_discover <list> <repo_root>   fills QA_FILES with the list. <list> is one of
#       shell | python | python-templates | js          absolute paths
#       tracked-shell | tracked-python | tracked-playbooks | tracked-helper-tests   repo-relative
#       A list that cannot be made (git failing, a file unreadable, an empty list but
#       python-templates) exits the caller with status 2 and the tool's message on stderr.
#   qa_is_excluded <repo_relative_path>      shell exclusion list (QA_EXCLUDE_DIRS)
#   qa_py_is_excluded <repo_relative_path>   python exclusion list (QA_PY_EXCLUDE_DIRS)
# The two lists are scripts/qa-trees.tsv, read by lib/qa-trees.bash (also read by the Go tool and by
# qa-changed). The exclusion test stays in bash: qa-js and qa-ansible-syntax ask it once per file.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/qa-trees.bash
source "$(dirname "${BASH_SOURCE[0]}")/lib/qa-trees.bash"
_QA_DISCOVERY_SCRIPTS="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Substring containment on a slash-delimited path, so no glob quoting subtleties.
_qa_path_in_dirs() {
    local rel="/$1/" dir
    shift
    for dir in "$@"; do
        [[ "$rel" == *"/$dir/"* ]] && return 0
    done
    return 1
}

qa_is_excluded() { _qa_path_in_dirs "$1" "${QA_EXCLUDE_DIRS[@]}"; }
qa_py_is_excluded() { _qa_path_in_dirs "$1" "${QA_PY_EXCLUDE_DIRS[@]}"; }

qa_discover() {
    local out file status=0
    out=$(mktemp)
    # A subshell: run_go_tool sets an EXIT trap that must not replace the caller's.
    (
        # shellcheck source=lib/go-tool.bash
        source "$_QA_DISCOVERY_SCRIPTS/lib/go-tool.bash"
        run_go_tool qa-discovery --root "$2" --scripts "$_QA_DISCOVERY_SCRIPTS" "$1"
    ) > "$out" || status=$?
    if [[ "$status" -ne 0 ]]; then
        rm -f "$out"
        exit 2
    fi
    QA_FILES=()
    while IFS= read -r -d '' file; do
        QA_FILES+=("$file")
    done < "$out"
    rm -f "$out"
}
