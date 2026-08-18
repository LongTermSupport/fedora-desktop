#!/usr/bin/bash
# Shared shell-script discovery for the bash QA gates. SOURCE this; do not run it.
#
# `qa-bash.bash` (bash -n + shellcheck) and `qa-patterns.bash` (semgrep) must
# agree, byte for byte, on what "a repo-owned shell script" means. When they
# disagree the disagreement is invisible: each prints its own file count and
# each count looks like the whole repo. Plan 00076 is what that costs — 27
# scripts were outside BOTH gates for months while both reported passes.
#
# Provides:
#   qa_discover_shell_files <repo_root>   -> QA_SHELL_FILES        (absolute)
#   qa_tracked_shell_scripts <repo_root>  -> QA_TRACKED_SHELL_FILES (repo-relative)
#   qa_is_excluded <repo_relative_path>
#   qa_has_shell_shebang <path>

# Upstream/vendor/runtime trees the project must not gate on:
#   .claude/hooks-daemon  — upstream dependency (also excluded in qa-python.bash)
#   .claude/ccy           — whole CCY runtime tree (snapshots, plugins, history)
#   .claude/skills        — installed skill payloads
#   .claude/worktrees     — checkouts of other branches; gated on their own branch
#   roles/vendor          — vendored Ansible roles
#   .semgrep              — annotated rule FIXTURES. Deliberately full of the
#                           broken patterns the rules must catch, so linting it
#                           reports faults that are the point of the file. It is
#                           validated by `semgrep --test` in qa-patterns.bash,
#                           which is the check that actually belongs to it.
QA_EXCLUDE_DIRS=(
    ".git"
    ".ansible/roles"
    ".claude/hooks-daemon"
    ".claude/ccy"
    ".claude/skills"
    ".claude/worktrees"
    "roles/vendor"
    ".semgrep"
    "node_modules"
    "untracked"
)

# True when the repo-relative path falls inside an excluded tree.
# Substring containment on a slash-delimited path, so no glob quoting subtleties.
qa_is_excluded() {
    local rel="/$1/" dir
    for dir in "${QA_EXCLUDE_DIRS[@]}"; do
        [[ "$rel" == *"/$dir/"* ]] && return 0
    done
    return 1
}

# A shell shebang, by ONE test used everywhere — discovery and the coverage
# assertions all call this, so an assertion can never be a second opinion about
# what counts as a shell script.
qa_has_shell_shebang() {
    local first_line=""
    # An unreadable file must not be quietly classified as "not a shell script"
    # — that is this gate's own defect class. Stop instead.
    if [[ ! -r "$1" ]]; then
        echo "✗ qa-discovery: cannot read $1" >&2
        echo "  Refusing to classify a file this gate could not open." >&2
        exit 2
    fi
    # A builtin read, not head+grep. This runs against every file in the repo,
    # where two forks per file measured ~9s against ~0.1s for the builtin.
    # `read` returns non-zero at EOF (a first line with no trailing newline, or
    # an empty file) but still sets the value, and the value is all we inspect.
    if ! IFS= read -r first_line < "$1"; then
        [[ -n "$first_line" ]] || return 1
    fi
    [[ "$first_line" =~ ^#!/.*bash ]] && return 0
    [[ "$first_line" == "#!/bin/sh" ]] && return 0
    [[ "$first_line" == "#!/usr/bin/sh" ]] && return 0
    return 1
}

# Populate QA_SHELL_FILES with every repo-owned shell script, as absolute paths.
#
# Shebang discovery is deliberately NOT restricted to `-executable` (Plan 00076).
# It used to be, and 27 of this repo's scripts — mode 0644, no extension, real
# bash, most of them deployed 0755 by their plays — were therefore never opened
# by any gate, while the gates printed passes over the rest. Coverage must depend
# on what a file IS, not on a permission bit any commit can drop.
qa_discover_shell_files() {
    local repo_root="$1" file dir
    local find_exclude=()
    for dir in "${QA_EXCLUDE_DIRS[@]}"; do
        find_exclude+=(! -path "*/$dir/*")
    done

    QA_SHELL_FILES=()
    while IFS= read -r -d '' file; do
        QA_SHELL_FILES+=("$file")
    done < <(find "$repo_root" -type f \( -name "*.sh" -o -name "*.bash" \) \
        "${find_exclude[@]}" \
        -print0)

    while IFS= read -r -d '' file; do
        qa_has_shell_shebang "$file" && QA_SHELL_FILES+=("$file")
    done < <(find "$repo_root" -type f \
        "${find_exclude[@]}" \
        ! -name "*.sh" \
        ! -name "*.bash" \
        ! -name "*.j2" \
        -print0)

    # A `while read` loop ends with read's EOF status of 1, which would become
    # this function's return value and abort the caller under `set -e`. The
    # contract here is total: it populates the array, and a genuine failure
    # (an unreadable file) exits outright rather than returning.
    return 0
}

# Populate QA_TRACKED_SHELL_FILES with every TRACKED shell script, repo-relative.
#
# This is the yardstick the gates measure their own coverage against. `find`
# stays the discovery source above so a brand-new, not-yet-`git add`ed script is
# still gated; git is the independent second opinion.
qa_tracked_shell_scripts() {
    local repo_root="$1" rel git_probe
    if ! command -v git > /dev/null; then
        echo "ERROR: git not found — QA coverage cannot be verified." >&2
        echo "  These gates will not report a pass they cannot show they earned." >&2
        exit 2
    fi
    if ! git_probe=$(git -C "$repo_root" rev-parse --git-dir 2>&1); then
        echo "ERROR: $repo_root is not a git checkout, so coverage cannot be verified." >&2
        echo "  git said: $git_probe" >&2
        exit 2
    fi

    QA_TRACKED_SHELL_FILES=()
    while IFS= read -r -d '' rel; do
        # Staged-but-deleted paths and submodule gitlinks are listed but absent.
        [[ -f "$repo_root/$rel" ]] || continue
        qa_is_excluded "$rel" && continue
        [[ "$rel" == *.j2 ]] && continue
        if [[ "$rel" != *.sh && "$rel" != *.bash ]] \
            && ! qa_has_shell_shebang "$repo_root/$rel"; then
            continue
        fi
        QA_TRACKED_SHELL_FILES+=("$rel")
    done < <(git -C "$repo_root" ls-files -z)

    # Total contract, same as above — see qa_discover_shell_files.
    return 0
}
