#!/usr/bin/bash
# Shared source-file discovery for the QA gates. SOURCE this; do not run it.
#
# `qa-bash.bash` (bash -n + shellcheck) and `qa-patterns.bash` (semgrep) must
# agree, byte for byte, on what "a repo-owned shell script" means. When they
# disagree the disagreement is invisible: each prints its own file count and
# each count looks like the whole repo. Plan 00076 is what that costs — 27
# scripts were outside BOTH gates for months while both reported passes.
#
# `qa-python.bash` had the SAME defect and did not learn from it (Plan 00081 F3):
# it discovered by extension or by the EXECUTE BIT, so 6 tracked Python programs
# of ~4,000 lines, mode 0644 with a `#!/usr/bin/env python3` shebang, were never
# compiled or linted while the gate printed "✓ python: 35 files OK". Hence one
# library rather than a second copy of the same mechanism — the file was renamed
# from qa-shell-discovery.bash when Python moved in.
#
# The two languages need DIFFERENT exclusion lists (see QA_PY_EXCLUDE_DIRS), so
# the lists are separate data over one shared mechanism.
#
# Provides:
#   qa_discover_shell_files <repo_root>   -> QA_SHELL_FILES         (absolute)
#   qa_tracked_shell_scripts <repo_root>  -> QA_TRACKED_SHELL_FILES (repo-relative)
#   qa_discover_python_files <repo_root>  -> QA_PYTHON_FILES        (absolute)
#   qa_tracked_python_files <repo_root>   -> QA_TRACKED_PYTHON_FILES (repo-relative)
#   qa_is_excluded <repo_relative_path>        (shell exclusion list)
#   qa_py_is_excluded <repo_relative_path>     (python exclusion list)
#   qa_has_shell_shebang <path>
#   qa_has_python_shebang <path>

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

# Python's exclusion list is NARROWER than the shell one inside .claude/ccy:
# .claude/ccy/claude-supervise.py is TRACKED and must stay gated, while the CCY
# runtime's plugins/ and file-history/ trees must not. Excluding the whole
# .claude/ccy tree (as the shell list does) would DROP a real file from the
# gate — this plan's own defect, committed inside the fix for it.
#
# Tracked is not the same as ours: that file is DAEMON-OWNED and is rewritten on
# every hooks-daemon upgrade, so a finding in it is never fixed by editing it.
# Upstream guarantees it clean under ruff's DEFAULT rules; a finding from a rule
# this repo selected is excluded here, not patched. See CLAUDE/QA.md.
QA_PY_EXCLUDE_DIRS=(
    ".git"
    ".ansible/roles"
    ".claude/hooks-daemon"
    ".claude/ccy/plugins"
    ".claude/ccy/file-history"
    ".claude/worktrees"
    "roles/vendor"
    "node_modules"
    "untracked"
    "__pycache__"
    ".venv"
    "venv"
)

# Shared mechanism: substring containment on a slash-delimited path, so no glob
# quoting subtleties.
_qa_path_in_dirs() {
    local rel="/$1/"
    shift
    local dir
    for dir in "$@"; do
        [[ "$rel" == *"/$dir/"* ]] && return 0
    done
    return 1
}

# True when the repo-relative path falls inside an excluded tree.
qa_is_excluded() { _qa_path_in_dirs "$1" "${QA_EXCLUDE_DIRS[@]}"; }
qa_py_is_excluded() { _qa_path_in_dirs "$1" "${QA_PY_EXCLUDE_DIRS[@]}"; }

# Read a file's first line, or refuse to classify it.
#
# An unreadable file must not be quietly classified as "not a shell script" /
# "not Python" — that is this gate's own defect class. Stop instead.
#
# A builtin read, not head+grep. This runs against every file in the repo, where
# two forks per file measured ~9s against ~0.1s for the builtin. `read` returns
# non-zero at EOF (a first line with no trailing newline, or an empty file) but
# still sets the value, and the value is all we inspect.
_qa_first_line() {
    local first_line=""
    if [[ ! -r "$1" ]]; then
        echo "✗ qa-discovery: cannot read $1" >&2
        echo "  Refusing to classify a file this gate could not open." >&2
        exit 2
    fi
    if ! IFS= read -r first_line < "$1"; then
        [[ -n "$first_line" ]] || return 1
    fi
    printf '%s' "$first_line"
}

# A shell shebang, by ONE test used everywhere — discovery and the coverage
# assertions all call this, so an assertion can never be a second opinion about
# what counts as a shell script.
qa_has_shell_shebang() {
    local first_line
    first_line=$(_qa_first_line "$1") || return 1
    [[ "$first_line" =~ ^#!/.*bash ]] && return 0
    [[ "$first_line" == "#!/bin/sh" ]] && return 0
    [[ "$first_line" == "#!/usr/bin/sh" ]] && return 0
    return 1
}

# A python shebang, by the same single test. Matches `#!/usr/bin/python3`,
# `#!/usr/bin/env python3` and the unversioned forms.
qa_has_python_shebang() {
    local first_line
    first_line=$(_qa_first_line "$1") || return 1
    [[ "$first_line" =~ ^#!.*python ]] && return 0
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
    local repo_root="$1" file rel

    # Exclusion is applied AFTER stripping the repo_root prefix, via the same
    # qa_is_excluded() the git-based qa_tracked_shell_scripts() below already
    # uses — one exclusion mechanism, operating only on the REPO-RELATIVE path.
    # An earlier version passed `! -path "*/$dir/*"` straight to find against
    # the ABSOLUTE path, which matches an excluded name ANYWHERE in the path —
    # including an ANCESTOR of repo_root itself. A checkout at a path like
    # .../untracked/repos/fedora-desktop then had every file excluded by the
    # "untracked" entry (meant to exclude the repo's OWN untracked/ subdir),
    # silently discovering 0 files. Traversal cost is unchanged: find already
    # visited every path either way (`! -path` filters output, it does not
    # `-prune` the walk), only the exclusion test moved.
    QA_SHELL_FILES=()
    while IFS= read -r -d '' file; do
        rel="${file#"$repo_root"/}"
        qa_is_excluded "$rel" && continue
        QA_SHELL_FILES+=("$file")
    done < <(find "$repo_root" -type f \( -name "*.sh" -o -name "*.bash" \) -print0)

    while IFS= read -r -d '' file; do
        rel="${file#"$repo_root"/}"
        qa_is_excluded "$rel" && continue
        qa_has_shell_shebang "$file" && QA_SHELL_FILES+=("$file")
    done < <(find "$repo_root" -type f \
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

# Populate QA_PYTHON_FILES with every repo-owned Python file, as absolute paths.
#
# Shebang discovery is deliberately NOT restricted to `-executable`, for exactly
# the reason Plan 00076 gave for the shell gates and Plan 00081 found had never
# been applied here: 6 tracked Python programs are mode 0644 with a python
# shebang (their plays deploy them 0755), so the execute-bit filter skipped
# ~4,000 lines while the gate reported a pass over the rest. Coverage must depend
# on what a file IS, not on a permission bit any commit can drop.
#
# `.j2` is skipped: a Jinja template is not valid Python until it is rendered.
qa_discover_python_files() {
    local repo_root="$1" file rel

    # Exclusion applied on the REPO-RELATIVE path via qa_py_is_excluded() —
    # see qa_discover_shell_files() above for why (an absolute-path `-path`
    # predicate can match an excluded name in an ANCESTOR of repo_root, not
    # just inside the repo).
    QA_PYTHON_FILES=()
    while IFS= read -r -d '' file; do
        rel="${file#"$repo_root"/}"
        qa_py_is_excluded "$rel" && continue
        QA_PYTHON_FILES+=("$file")
    done < <(find "$repo_root" -type f -name "*.py" -print0)

    while IFS= read -r -d '' file; do
        rel="${file#"$repo_root"/}"
        qa_py_is_excluded "$rel" && continue
        qa_has_python_shebang "$file" && QA_PYTHON_FILES+=("$file")
    done < <(find "$repo_root" -type f \
        ! -name "*.py" \
        ! -name "*.j2" \
        -print0)

    # Total contract — see qa_discover_shell_files.
    return 0
}

# Populate QA_TRACKED_PYTHON_FILES with every TRACKED Python file, repo-relative.
# The yardstick the python gate measures its own coverage against, exactly as
# qa_tracked_shell_scripts is for the bash gates.
qa_tracked_python_files() {
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

    QA_TRACKED_PYTHON_FILES=()
    while IFS= read -r -d '' rel; do
        [[ -f "$repo_root/$rel" ]] || continue
        qa_py_is_excluded "$rel" && continue
        [[ "$rel" == *.j2 ]] && continue
        if [[ "$rel" != *.py ]] \
            && ! qa_has_python_shebang "$repo_root/$rel"; then
            continue
        fi
        QA_TRACKED_PYTHON_FILES+=("$rel")
    done < <(git -C "$repo_root" ls-files -z)

    # Total contract — see qa_discover_shell_files.
    return 0
}
