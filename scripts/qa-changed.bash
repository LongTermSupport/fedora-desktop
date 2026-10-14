#!/usr/bin/bash
# Diff-scoped QA for use while iterating. Runs only the checks the CHANGED files need, so the
# full ./scripts/qa-all.bash (every gate, every suite) is needed only before a push.
#
# Usage:
#   ./scripts/qa-changed.bash                 files changed vs HEAD, plus untracked (not ignored)
#   ./scripts/qa-changed.bash --base <ref>    files changed vs <ref> (e.g. origin/F44), so a burst
#                                             of local commits is covered, plus untracked
#   ./scripts/qa-changed.bash --plan          print the chosen checks, run nothing
#   ./scripts/qa-changed.bash --files F...    use exactly these repo-relative files (no git diff)
#
# What each kind of file gets (the gate scripts take no file list, so the same tools the gates
# run are invoked on the changed files; file CLASSIFICATION is qa-discovery.bash's, shared):
#   shell (.sh/.bash/shebang)  bash -n, shellcheck, the semgrep bash rules
#   python                     ruff (the .qa-versions pin, asserted by qa-toolchain.bash) and the
#                              mirrored unit test: helpers/x/y.py -> tests/helpers/x/test_y.py;
#                              a changed test_*.py runs itself
#   playbook / task / vars yml ansible-syntax for the playbook, or for every playbook that names
#                              the task/vars file by basename (one level), plus the ansible
#                              fail-fast pattern gate (qa-ansible.bash, whole: it is fast)
#   markdown, playbooks yml    the docs gate (qa-docs.bash, whole: it is fast)
#   src-go/ (any file)         the interim Go gate (qa-go.bash, whole: it is fast)
#   any bash file, the bash-   the bash-limits gate (qa-bash-limits.bash, whole: ~0.5s), and its Go
#   limits allowlist or a      source; a path on the allowlist counts as bash even when deleted
#   path on the allowlist
#   js/mjs                     node --check; extensions/ files also get the project's eslint
#   src-go/ in a command's     that command's suites too (ccy-teams: test-ccy-teams-command.bash;
#   build closure              ccy-sessions: its four test-ccy-* suites): a file in cmd/<name> or a
#                              package it imports (go list -deps), go.mod, go.sum
#   any non-markdown file      every scripts/test-*.bash suite (and hard gate script) that
#                              qa-all.bash runs and whose text names it by path or basename,
#                              plus any suite that itself changed
# Left to qa-all.bash: the python-template compile, semgrep parse-coverage assertions, the
# helper-tests count/coverage assertions, extension-compat, panel-contract, deployed-drift and
# every suite no changed file is named in.
#
# A changed file that maps to no check is reported "not covered by qa-changed", never silently.
#
# Exit codes: 0 pass (or nothing changed), 1 a check failed, 2 missing tool / bad usage.
# stdout is the report; failure detail goes to stderr.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# QA_CHANGED_ROOT lets scripts/test-qa-changed.bash point the git-diff collection at a scratch
# repository; the discovery library and the suites always come from this checkout.
REPO_ROOT="${QA_CHANGED_ROOT:-$(cd "$SCRIPT_DIR/.." && pwd)}"

# shellcheck source-path=SCRIPTDIR
# shellcheck source=qa-discovery.bash
source "$SCRIPT_DIR/qa-discovery.bash"
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/qa-helper-summary.bash
source "$SCRIPT_DIR/lib/qa-helper-summary.bash"

usage() {
    awk 'NR>=2 && /^#/{sub(/^# ?/,""); print; next} NR>2{exit}' "${BASH_SOURCE[0]}" >&2
}

BASE="HEAD"
PLAN_ONLY=0
EXPLICIT_FILES=()
USE_EXPLICIT=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --base)
            if [[ $# -lt 2 ]]; then
                echo "ERROR: --base needs a ref" >&2
                exit 2
            fi
            BASE="$2"
            shift 2
            ;;
        --plan)
            PLAN_ONLY=1
            shift
            ;;
        --files)
            USE_EXPLICIT=1
            shift
            EXPLICIT_FILES=("$@")
            break
            ;;
        -h | --help)
            usage
            exit 0
            ;;
        *)
            echo "ERROR: unknown argument '$1'" >&2
            usage
            exit 2
            ;;
    esac
done

TMPS=()
cleanup() {
    local t
    for t in ${TMPS[@]+"${TMPS[@]}"}; do
        rm -rf "$t"
    done
}
trap cleanup EXIT

cd "$REPO_ROOT"

# --- collect changed files -----------------------------------------------------------------
CHANGED=()
if [[ $USE_EXPLICIT -eq 1 ]]; then
    candidates=("${EXPLICIT_FILES[@]}")
else
    candidates=()
    while IFS= read -r -d '' f; do
        candidates+=("$f")
    done < <(
        {
            git diff --name-only -z --diff-filter=d "$BASE" --
            git ls-files -z --others --exclude-standard
        } | sort -zu
    )
fi
for f in ${candidates[@]+"${candidates[@]}"}; do
    # A deleted file has nothing left to check.
    [[ -f "$f" ]] && CHANGED+=("$f")
done

if [[ ${#CHANGED[@]} -eq 0 ]]; then
    echo "qa-changed: no changed files (vs $BASE, plus untracked) — nothing to check."
    exit 0
fi

# --- classify -------------------------------------------------------------------------------
SH_FILES=()
PY_FILES=()
JS_FILES=()
ESLINT_FILES=()
PLAYBOOKS=()
UNIT_MODULES=()
NOTES=()
SKIPPED=()
NEED_DOCS=0
NEED_ANSIBLE_PATTERNS=0
NEED_GO=0
NEED_BASH_LIMITS=0
SRCGO_FILES=()
declare -A COVERED=()
# The paths on the bash-limits allowlist (each line starts with its path and a tab), so a change to
# a listed file, even a deletion or an extensionless one, runs that gate.
BASH_LIMITS_LISTED=""
[[ -f "$SCRIPT_DIR/bash-limits.allowlist" ]] && BASH_LIMITS_LISTED=$'\n'"$(<"$SCRIPT_DIR/bash-limits.allowlist")"

ALL_PLAYBOOKS=()
ALL_PLAYBOOKS_LOADED=0
load_all_playbooks() {
    [[ $ALL_PLAYBOOKS_LOADED -eq 1 ]] && return 0
    local yml=() rc=0 f
    while IFS= read -r -d '' f; do
        yml+=("$f")
    done < <(git ls-files -z -- '*.yml' '*.yaml')
    if [[ ${#yml[@]} -gt 0 ]]; then
        while IFS= read -r -d '' f; do
            qa_is_excluded "$f" || ALL_PLAYBOOKS+=("$f")
        done < <(grep -lZE '^\s*-\s+(hosts|import_playbook):' "${yml[@]}" || rc=$?)
        # grep: 1 = no playbook at all, which this repo can never be; >=2 = grep itself broke.
        if [[ $rc -ge 2 ]]; then
            echo "ERROR: grep failed while listing playbooks (rc=$rc)" >&2
            exit 2
        fi
    fi
    ALL_PLAYBOOKS_LOADED=1
}

is_playbook_file() {
    grep -Eq '^\s*-\s+(hosts|import_playbook):' "$1"
}

classify_yaml() {
    local rel="$1" base pb rc=0 refs=()
    if is_playbook_file "$rel"; then
        PLAYBOOKS+=("$rel")
        NEED_ANSIBLE_PATTERNS=1
        NEED_DOCS=1
        COVERED[$rel]=1
        return 0
    fi
    case "$rel" in
        playbooks/* | tasks/* | vars/* | environment/* | roles/*)
            NEED_ANSIBLE_PATTERNS=1
            COVERED[$rel]=1
            [[ "$rel" == playbooks/* ]] && NEED_DOCS=1
            base="${rel##*/}"
            load_all_playbooks
            for pb in ${ALL_PLAYBOOKS[@]+"${ALL_PLAYBOOKS[@]}"}; do
                grep -qF -- "$base" "$pb" || rc=$?
                if [[ $rc -eq 0 ]]; then
                    refs+=("$pb")
                elif [[ $rc -ge 2 ]]; then
                    echo "ERROR: grep failed on $pb" >&2
                    exit 2
                fi
                rc=0
            done
            PLAYBOOKS+=(${refs[@]+"${refs[@]}"})
            if [[ ${#refs[@]} -eq 0 ]]; then
                NOTES+=("$rel: no playbook names '$base', so no ansible-syntax target (patterns gate only)")
            fi
            ;;
    esac
}

for rel in "${CHANGED[@]}"; do
    # The Go tests execute these files as parity references, so a change to one must run the Go
    # gate. No continue: the file is still classified (bash, Python) below.
    case "$rel" in
        files/var/local/claude-yolo/lib/common-pure.bash | files/var/local/claude-yolo/lib/session-registry.bash | helpers/agent_bus/declare.py)
            NEED_GO=1
            ;;
    esac
    # The bash-limits gate (whole: it is fast) reads every bash file and the allowlist. No
    # continue, for the same reason.
    case "$rel" in
        *.bash | *.sh | *.j2 | src-go/internal/bashlimits/* | src-go/cmd/qa-bash-limits/*)
            NEED_BASH_LIMITS=1
            ;;
        scripts/bash-limits.allowlist)
            NEED_BASH_LIMITS=1
            COVERED[$rel]=1
            ;;
    esac
    [[ "$BASH_LIMITS_LISTED" == *$'\n'"$rel"$'\t'* ]] && NEED_BASH_LIMITS=1
    case "$rel" in
        *.md)
            NEED_DOCS=1
            COVERED[$rel]=1
            continue
            ;;
        *.yml | *.yaml)
            if qa_is_excluded "$rel"; then
                SKIPPED+=("$rel")
            else
                classify_yaml "$rel"
            fi
            continue
            ;;
        *.js | *.mjs)
            if qa_is_excluded "$rel"; then
                SKIPPED+=("$rel")
            else
                JS_FILES+=("$rel")
                [[ "$rel" == extensions/* ]] && ESLINT_FILES+=("$rel")
                COVERED[$rel]=1
            fi
            continue
            ;;
        src-go/*)
            # Go files, go.mod and the lint config: the interim qa-go.bash gate runs whole.
            NEED_GO=1
            SRCGO_FILES+=("$rel")
            COVERED[$rel]=1
            continue
            ;;
        *.j2)
            # A template is not valid shell or Python until rendered; qa-all does not lint it
            # either, so only a suite that names it can cover it.
            continue
            ;;
    esac
    if [[ "$rel" == *.py ]] || qa_has_python_shebang "$rel"; then
        if qa_py_is_excluded "$rel"; then
            SKIPPED+=("$rel")
        else
            PY_FILES+=("$rel")
            COVERED[$rel]=1
            base="${rel##*/}"
            case "$rel" in
                tests/helpers/*/test_*.py | tests/helpers/test_*.py)
                    mod="${rel%.py}"
                    UNIT_MODULES+=("${mod//\//.}")
                    ;;
                helpers/*.py)
                    mirror="tests/helpers/${rel#helpers/}"
                    mirror="${mirror%/*}/test_${base}"
                    if [[ -f "$mirror" ]]; then
                        mod="${mirror%.py}"
                        UNIT_MODULES+=("${mod//\//.}")
                    elif [[ "$base" != "__init__.py" ]]; then
                        NOTES+=("$rel: no mirrored unit test ($mirror)")
                    fi
                    ;;
            esac
        fi
        continue
    fi
    if [[ "$rel" == *.sh || "$rel" == *.bash ]] || qa_has_shell_shebang "$rel"; then
        if qa_is_excluded "$rel"; then
            SKIPPED+=("$rel")
        else
            SH_FILES+=("$rel")
            COVERED[$rel]=1
        fi
    fi
done

# Unique-ify the lists that can repeat (several task files naming the same playbook).
uniq_array() {
    local -n arr="$1"
    local out=() seen="" item
    for item in ${arr[@]+"${arr[@]}"}; do
        case "$seen" in
            *$'\n'"$item"$'\n'*) ;;
            *)
                out+=("$item")
                seen+=$'\n'"$item"$'\n'
                ;;
        esac
    done
    arr=(${out[@]+"${out[@]}"})
}
uniq_array PLAYBOOKS
uniq_array UNIT_MODULES

# --- suites ---------------------------------------------------------------------------------
# Population: exactly the test-*/qa-* scripts qa-all.bash invokes by name, minus the gates that
# are run per file above or that inspect the host.
PER_FILE_GATES=" qa-bash qa-python qa-patterns qa-ansible qa-ansible-syntax qa-js qa-docs qa-go qa-bash-limits qa-toolchain qa-deployed-drift qa-helper-tests qa-discovery "
SUITES=()
while IFS= read -r name; do
    [[ "$PER_FILE_GATES" == *" ${name%.bash} "* ]] && continue
    [[ -f "$SCRIPT_DIR/$name" ]] || continue
    SUITES+=("scripts/$name")
done < <(grep -vE '^[[:space:]]*#' "$SCRIPT_DIR/qa-all.bash" \
    | grep -oE "[\$]SCRIPT_DIR/(test|qa)-[A-Za-z0-9._-]+[.]bash" \
    | awk '{sub(/.*\//, ""); if (!seen[$0]++) print}')
if [[ ${#SUITES[@]} -eq 0 ]]; then
    echo "ERROR: found no suites in qa-all.bash — its invocation shape changed; fix this script." >&2
    exit 2
fi

PATTERN_FILE="$(mktemp)"
TMPS+=("$PATTERN_FILE")
declare -A PATTERN_OWNER=()
for rel in "${CHANGED[@]}"; do
    [[ "$rel" == *.md ]] && continue
    for pat in "$rel" "${rel##*/}"; do
        [[ -n "$pat" ]] || continue
        PATTERN_OWNER[$pat]="$rel"
    done
done
for pat in "${!PATTERN_OWNER[@]}"; do
    printf '%s\n' "$pat" >> "$PATTERN_FILE"
done

# A deployed Go binary is built from its cmd/<name> and the packages it imports, so a change
# to any of them is exercised by the binary's suites although the suites name none of these
# files. The closure comes from the go tool, so it cannot drift from the imports. Each entry
# is "<command>: <suites that build and drive it>".
GO_BINARY_SUITES=(
    "ccy-teams: scripts/test-ccy-teams-command.bash"
    "ccy-sessions: scripts/test-ccy-sessions-reboot.bash scripts/test-ccy-sessions-take-over.bash scripts/test-ccy-tmux-targets.bash scripts/test-ccy-restore-askpass.bash"
)
declare -A GO_SUITE_OWNERS=()
if [[ ${#SRCGO_FILES[@]} -gt 0 ]]; then
    for entry in "${GO_BINARY_SUITES[@]}"; do
        cmd="${entry%%:*}"
        read -r -a cmd_suites <<<"${entry#*: }"
        if ! cmd_dirs="$(cd "$REPO_ROOT/src-go" && go list -deps -f '{{if not .Standard}}{{.Dir}}{{end}}' "./cmd/$cmd")"; then
            echo "ERROR: go list -deps ./cmd/$cmd failed; cannot map src-go/ changes to the $cmd suites." >&2
            exit 2
        fi
        for rel in "${SRCGO_FILES[@]}"; do
            owned=0
            case "$rel" in
                src-go/go.mod | src-go/go.sum) owned=1 ;;
                *)
                    while IFS= read -r dir; do
                        [[ -n "$dir" ]] || continue
                        if [[ "$REPO_ROOT/$rel" == "$dir"/* ]]; then
                            owned=1
                            break
                        fi
                    done <<<"$cmd_dirs"
                    ;;
            esac
            if [[ $owned -eq 1 ]]; then
                for cmd_suite in "${cmd_suites[@]}"; do
                    GO_SUITE_OWNERS[$cmd_suite]+="$rel"$'\n'
                done
            fi
        done
    done
fi

PICKED=()
PICKED_WHY=()
if [[ -s "$PATTERN_FILE" ]]; then
    for suite in "${SUITES[@]}"; do
        why=()
        declare -A named=()
        while IFS= read -r owner; do
            [[ -n "$owner" ]] || continue
            why+=("builds from $owner")
        done <<<"${GO_SUITE_OWNERS[$suite]:-}"
        # The suite itself changed.
        if [[ -n "${PATTERN_OWNER[$suite]:-}" ]]; then
            why+=("the suite itself changed")
            COVERED[${PATTERN_OWNER[$suite]}]=1
        fi
        rc=0
        hits="$(grep -oFf "$PATTERN_FILE" "$SCRIPT_DIR/${suite#scripts/}" | sort -u)" || rc=$?
        if [[ $rc -ge 2 ]]; then
            echo "ERROR: grep failed on $suite" >&2
            exit 2
        fi
        while IFS= read -r hit; do
            [[ -n "$hit" ]] || continue
            owner="${PATTERN_OWNER[$hit]}"
            # A suite naming itself is the case above, not a second reason.
            [[ "$owner" == "$suite" ]] && continue
            # Path and basename can both hit; one reason per file.
            [[ -n "${named[$owner]:-}" ]] && continue
            named[$owner]=1
            why+=("names $owner")
            COVERED[$owner]=1
        done <<<"$hits"
        if [[ ${#why[@]} -gt 0 ]]; then
            PICKED+=("$suite")
            reasons="$(printf '%s; ' "${why[@]}")"
            PICKED_WHY+=("${reasons%; }")
        fi
    done
fi

# --- the plan -------------------------------------------------------------------------------
UNCOVERED=()
for rel in "${CHANGED[@]}"; do
    [[ -n "${COVERED[$rel]:-}" ]] && continue
    # Skipped (excluded tree) files are reported as skipped, not as a coverage gap.
    skipped=0
    for s in ${SKIPPED[@]+"${SKIPPED[@]}"}; do
        [[ "$s" == "$rel" ]] && skipped=1
    done
    [[ $skipped -eq 1 ]] || UNCOVERED+=("$rel")
done

NEED_TOOLCHAIN=0
if [[ ${#SH_FILES[@]} -gt 0 || ${#PY_FILES[@]} -gt 0 ]]; then
    NEED_TOOLCHAIN=1
fi
# An extensionless shell script (a shebang, no .bash) is only known to be shell here.
[[ ${#SH_FILES[@]} -gt 0 ]] && NEED_BASH_LIMITS=1

print_plan() {
    echo "plan: ${#CHANGED[@]} changed file(s) vs $BASE"
    [[ $NEED_TOOLCHAIN -eq 1 ]] && echo "check toolchain"
    if [[ ${#SH_FILES[@]} -gt 0 ]]; then
        echo "check bash-syntax: ${SH_FILES[*]}"
        echo "check shellcheck: ${SH_FILES[*]}"
        echo "check semgrep: ${SH_FILES[*]}"
    fi
    [[ ${#PY_FILES[@]} -gt 0 ]] && echo "check ruff: ${PY_FILES[*]}"
    [[ ${#UNIT_MODULES[@]} -gt 0 ]] && echo "check unit-tests: ${UNIT_MODULES[*]}"
    [[ ${#PLAYBOOKS[@]} -gt 0 ]] && echo "check ansible-syntax: ${PLAYBOOKS[*]}"
    [[ $NEED_ANSIBLE_PATTERNS -eq 1 ]] && echo "check ansible-patterns"
    [[ $NEED_DOCS -eq 1 ]] && echo "check docs"
    [[ $NEED_GO -eq 1 ]] && echo "check go"
    [[ $NEED_BASH_LIMITS -eq 1 ]] && echo "check bash-limits"
    [[ ${#JS_FILES[@]} -gt 0 ]] && echo "check node-check: ${JS_FILES[*]}"
    [[ ${#ESLINT_FILES[@]} -gt 0 ]] && echo "check eslint: ${ESLINT_FILES[*]}"
    local i
    for i in "${!PICKED[@]}"; do
        echo "suite ${PICKED[$i]}: ${PICKED_WHY[$i]}"
    done
    for rel in ${SKIPPED[@]+"${SKIPPED[@]}"}; do
        echo "skipped (excluded tree): $rel"
    done
    for rel in ${NOTES[@]+"${NOTES[@]}"}; do
        echo "note: $rel"
    done
    for rel in ${UNCOVERED[@]+"${UNCOVERED[@]}"}; do
        echo "⚠ not covered by qa-changed: $rel"
    done
}

print_plan
if [[ $PLAN_ONLY -eq 1 ]]; then
    exit 0
fi

# --- preflight: every tool a planned check needs, before any check runs ---------------------
need_tool() {
    local tool="$1" fix="$2"
    if ! command -v "$tool" > /dev/null; then
        echo "ERROR: $tool not found — a planned check needs it." >&2
        echo "  $fix" >&2
        echo "  Do NOT install it by hand." >&2
        exit 2
    fi
}
[[ ${#SH_FILES[@]} -gt 0 ]] && need_tool shellcheck "Deploy it: ansible-playbook playbooks/imports/play-devtools.yml"
[[ ${#SH_FILES[@]} -gt 0 ]] && need_tool semgrep "Deploy it: ansible-playbook playbooks/imports/play-python.yml"
[[ ${#PY_FILES[@]} -gt 0 ]] && need_tool ruff "Deploy it: ansible-playbook playbooks/imports/play-python.yml"
[[ ${#PLAYBOOKS[@]} -gt 0 ]] && need_tool ansible-playbook "Install ansible (sudo dnf install ansible) via the repo's IaC."
if [[ ${#JS_FILES[@]} -gt 0 ]]; then
    need_tool node "Deploy it: ansible-playbook playbooks/imports/play-nvm-install.yml"
fi
if [[ ${#ESLINT_FILES[@]} -gt 0 && ! -x extensions/node_modules/.bin/eslint ]]; then
    echo "ERROR: eslint for extensions/ is not set up (cd extensions && npm ci)." >&2
    exit 2
fi

# --- run ------------------------------------------------------------------------------------
FAILED=()
pass() { printf '✓ %s: %s\n' "$1" "$2"; }
fail() {
    printf '✗ %s: %s\n' "$1" "$2"
    FAILED+=("$1")
}

# run_check <name> <summary> <command...> — pass/fail on the command's status, its output to
# stderr on failure.
run_check() {
    local name="$1" summary="$2" captured=""
    shift 2
    if captured="$("$@" 2>&1)"; then
        pass "$name" "$summary"
    else
        fail "$name" "$summary"
        printf '%s\n' "$captured" >&2
    fi
}

bash_syntax() {
    local f rc=0
    for f in "${SH_FILES[@]}"; do
        bash -n "$f" || rc=1
    done
    return $rc
}

semgrep_changed() {
    local mirror json f m rc=0 findings
    mirror="$(mktemp -d)"
    json="$(mktemp)"
    TMPS+=("$mirror" "$json")
    for f in "${SH_FILES[@]}"; do
        m="$f"
        [[ "$f" == *.sh || "$f" == *.bash ]] || m="$f.bash"
        mkdir -p "$mirror/$(dirname "$m")"
        cp "$f" "$mirror/$m"
    done
    (
        cd "$mirror"
        semgrep --config "$REPO_ROOT/.semgrep/bash-conventions.yml" \
            --json-output "$json" --metrics=off --quiet . 2>&1
    ) || rc=$?
    if [[ $rc -ge 2 ]]; then
        echo "semgrep failed (rc=$rc)"
        return 2
    fi
    findings="$(jq -r '.results[] | "\(.path):\(.start.line) \(.check_id) \(.extra.message)"' "$json")"
    if [[ -n "$findings" ]]; then
        printf '%s\n' "$findings"
        return 1
    fi
}

run_unit_tests() {
    GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1 python3 -m unittest "${UNIT_MODULES[@]}"
}

ansible_syntax_changed() {
    local f rc=0
    export ANSIBLE_CONFIG="$REPO_ROOT/ansible.cfg"
    for f in "${PLAYBOOKS[@]}"; do
        if ! ansible-playbook --syntax-check "$f" > /dev/null; then
            echo "^^ $f"
            rc=1
        fi
    done
    return $rc
}

node_check() {
    local f rc=0
    for f in "${JS_FILES[@]}"; do
        node --check "$f" || rc=1
    done
    return $rc
}

eslint_changed() {
    local rel=() f
    for f in "${ESLINT_FILES[@]}"; do
        rel+=("${f#extensions/}")
    done
    (cd extensions && ./node_modules/.bin/eslint "${rel[@]}")
}

gate_json() {
    local j
    j="$(mktemp)"
    TMPS+=("$j")
    printf '%s' "$j"
}

if [[ $NEED_TOOLCHAIN -eq 1 ]]; then
    run_check toolchain "QA tool versions match .qa-versions" bash "$SCRIPT_DIR/qa-toolchain.bash"
fi
if [[ ${#SH_FILES[@]} -gt 0 ]]; then
    run_check bash-syntax "${#SH_FILES[@]} file(s)" bash_syntax
    # --severity=warning: qa-bash.bash gates on error and warning level only.
    run_check shellcheck "${#SH_FILES[@]} file(s)" shellcheck --severity=warning "${SH_FILES[@]}"
    run_check semgrep "${#SH_FILES[@]} file(s)" semgrep_changed
fi
if [[ ${#PY_FILES[@]} -gt 0 ]]; then
    run_check ruff "${#PY_FILES[@]} file(s)" ruff check "${PY_FILES[@]}"
fi
if [[ ${#UNIT_MODULES[@]} -gt 0 ]]; then
    run_check unit-tests "${#UNIT_MODULES[@]} module(s)" run_unit_tests
fi
if [[ ${#PLAYBOOKS[@]} -gt 0 ]]; then
    run_check ansible-syntax "${#PLAYBOOKS[@]} playbook(s)" ansible_syntax_changed
fi
if [[ $NEED_ANSIBLE_PATTERNS -eq 1 ]]; then
    run_check ansible-patterns "fail-fast patterns, whole gate" \
        env QA_JSON_OUT="$(gate_json)" bash "$SCRIPT_DIR/qa-ansible.bash"
fi
if [[ $NEED_DOCS -eq 1 ]]; then
    run_check docs "whole gate" env QA_JSON_OUT="$(gate_json)" bash "$SCRIPT_DIR/qa-docs.bash"
fi
if [[ $NEED_GO -eq 1 ]]; then
    run_check go "whole gate (interim qa-go.bash)" bash "$SCRIPT_DIR/qa-go.bash"
fi
if [[ $NEED_BASH_LIMITS -eq 1 ]]; then
    run_check bash-limits "whole gate (tooling is Go; allowlist ratchet)" bash "$SCRIPT_DIR/qa-bash-limits.bash"
fi
if [[ ${#JS_FILES[@]} -gt 0 ]]; then
    run_check node-check "${#JS_FILES[@]} file(s)" node_check
fi
if [[ ${#ESLINT_FILES[@]} -gt 0 ]]; then
    run_check eslint "${#ESLINT_FILES[@]} file(s)" eslint_changed
fi
for i in "${!PICKED[@]}"; do
    suite="${PICKED[$i]}"
    suite_out=""
    if suite_out="$(bash "$SCRIPT_DIR/${suite#scripts/}" 2>&1)"; then
        pass "suite ${suite#scripts/}" "$(qa_gate_case_count "$suite_out") (${PICKED_WHY[$i]})"
    else
        fail "suite ${suite#scripts/}" "(${PICKED_WHY[$i]})"
        printf '%s\n' "$suite_out" >&2
    fi
done

if [[ ${#UNCOVERED[@]} -gt 0 ]]; then
    echo "⚠ ${#UNCOVERED[@]} changed file(s) not covered by qa-changed — qa-all.bash is the only check"
fi
if [[ ${#FAILED[@]} -gt 0 ]]; then
    echo "✗ qa-changed FAILED: ${FAILED[*]}"
    exit 1
fi
echo "✓ qa-changed passed: ${#CHANGED[@]} changed file(s). Run ./scripts/qa-all.bash before pushing."
