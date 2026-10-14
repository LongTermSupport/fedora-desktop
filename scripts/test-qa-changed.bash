#!/usr/bin/env bash
# Unit-test scripts/qa-changed.bash's file -> check mapping through its --plan mode, which
# prints the chosen checks without running any of them.
#
# Two halves: (1) `--plan --files` against real repo paths, asserting which checks and suites
# each kind of file selects, including the "not covered" warning; (2) the git collection
# (modified, untracked, ignored, deleted, --base) against a scratch repository via
# QA_CHANGED_ROOT. Nothing here runs a real check, so it is fast and cannot be made to pass
# by the tools being installed.
#
# Usage: ./scripts/test-qa-changed.bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TARGET="$SCRIPT_DIR/qa-changed.bash"

if [[ ! -f "$TARGET" ]]; then
    echo "ERROR: $TARGET not found — nothing to test." >&2
    exit 2
fi

SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT

PASS=0
FAIL=0

# expect_has <label> <plan-text> <fixed-substring> — the plan contains it
expect_has() {
    local label="$1" plan="$2" needle="$3"
    if grep -qF -- "$needle" <<<"$plan"; then
        printf '  ok    %s\n' "$label"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s — plan lacks: %s\n' "$label" "$needle"
        FAIL=$((FAIL + 1))
    fi
}

expect_lacks() {
    local label="$1" plan="$2" needle="$3"
    if grep -qF -- "$needle" <<<"$plan"; then
        printf '  FAIL  %s — plan unexpectedly has: %s\n' "$label" "$needle"
        FAIL=$((FAIL + 1))
    else
        printf '  ok    %s\n' "$label"
        PASS=$((PASS + 1))
    fi
}

plan_for() {
    bash "$TARGET" --plan --files "$@"
}

echo "== file -> check mapping (--plan --files, real repo paths) =="

plan="$(plan_for scripts/qa-bash.bash)"
expect_has "shell: syntax" "$plan" "check bash-syntax: scripts/qa-bash.bash"
expect_has "shell: shellcheck" "$plan" "check shellcheck: scripts/qa-bash.bash"
expect_has "shell: semgrep" "$plan" "check semgrep: scripts/qa-bash.bash"
expect_has "shell: toolchain" "$plan" "check toolchain"
expect_lacks "shell: no ruff" "$plan" "check ruff"

plan="$(plan_for files/var/local/claude-yolo/claude-yolo)"
expect_has "extensionless shebang script is shell" "$plan" "check shellcheck: files/var/local/claude-yolo/claude-yolo"

plan="$(plan_for helpers/gnome/check_panel_contract.py)"
expect_has "python: ruff" "$plan" "check ruff: helpers/gnome/check_panel_contract.py"
expect_has "python: mirrored test" "$plan" "check unit-tests: tests.helpers.gnome.test_check_panel_contract"
expect_lacks "python: no shellcheck" "$plan" "check shellcheck"

plan="$(plan_for tests/helpers/gnome/test_check_panel_contract.py)"
expect_has "changed test runs itself" "$plan" "check unit-tests: tests.helpers.gnome.test_check_panel_contract"

plan="$(plan_for playbooks/imports/play-python.yml)"
expect_has "playbook: ansible-syntax" "$plan" "check ansible-syntax: playbooks/imports/play-python.yml"
expect_has "playbook: patterns gate" "$plan" "check ansible-patterns"
expect_has "playbook: docs gate" "$plan" "check docs"

plan="$(plan_for CLAUDE/QA.md)"
expect_has "markdown: docs gate" "$plan" "check docs"
expect_lacks "markdown: nothing else" "$plan" "check ansible"

plan="$(plan_for extensions/speech-to-text@fedora-desktop/extension.js)"
expect_has "js: node --check" "$plan" "check node-check: extensions/speech-to-text@fedora-desktop/extension.js"
expect_has "js: eslint under extensions/" "$plan" "check eslint: extensions/speech-to-text@fedora-desktop/extension.js"

plan="$(plan_for vars/version-pins.yml)"
expect_has "vars yml: patterns gate" "$plan" "check ansible-patterns"
expect_has "vars yml: suite naming it, with the reason" "$plan" "suite scripts/qa-version-pins.bash: names vars/version-pins.yml"

plan="$(plan_for scripts/lib/qa-helper-summary.bash)"
expect_has "suite picked by name, with the reason" "$plan" "suite scripts/test-qa-helper-summary.bash: names scripts/lib/qa-helper-summary.bash"

plan="$(plan_for scripts/test-planlib.bash)"
expect_has "a changed suite runs itself" "$plan" "suite scripts/test-planlib.bash: the suite itself changed"

# Built from parts: this suite's own text would otherwise name the file, and a suite that names
# a changed file covers it — which would turn the "uncovered" fixture into a covered one.
wf_dir=".github/workflows"
wf_name="qa"
plan="$(plan_for "$wf_dir/${wf_name}.yml")"
expect_has "unmapped file is reported" "$plan" "not covered by qa-changed: $wf_dir/${wf_name}.yml"

plan="$(plan_for no/such/file.bash)"
expect_has "nonexistent file is dropped" "$plan" "no changed files"

echo
echo "== git collection (scratch repository) =="

REPO="$SCRATCH/repo"
mkdir -p "$REPO"
git_s() { git -C "$REPO" -c user.name=t -c user.email=t@example.com -c commit.gpgsign=false "$@"; }
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
git_s init -q
printf 'ignored.bash\n' > "$REPO/.gitignore"
printf '#!/usr/bin/bash\n:\n' > "$REPO/kept.bash"
printf '#!/usr/bin/bash\n:\n' > "$REPO/gone.bash"
git_s add -A
git_s commit -q -m base
BASE_SHA="$(git_s rev-parse HEAD)"

printf '#!/usr/bin/bash\n: changed\n' > "$REPO/kept.bash"
rm "$REPO/gone.bash"
printf 'x = 1\n' > "$REPO/new.py"
printf '#!/usr/bin/bash\n:\n' > "$REPO/ignored.bash"

scratch_plan() {
    QA_CHANGED_ROOT="$REPO" bash "$TARGET" --plan "$@"
}

plan="$(scratch_plan)"
expect_has "modified file is collected" "$plan" "check bash-syntax: kept.bash"
expect_has "untracked file is collected" "$plan" "check ruff: new.py"
expect_lacks "deleted file is dropped" "$plan" "gone.bash"
expect_lacks "ignored file is dropped" "$plan" "ignored.bash"

git_s add -A
git_s commit -q -m second
plan="$(scratch_plan)"
expect_has "clean tree says so" "$plan" "no changed files"

rc=0
QA_CHANGED_ROOT="$REPO" bash "$TARGET" > /dev/null || rc=$?
if [[ $rc -eq 0 ]]; then
    printf '  ok    clean tree: exit 0\n'
    PASS=$((PASS + 1))
else
    printf '  FAIL  clean tree: exit %s, wanted 0\n' "$rc"
    FAIL=$((FAIL + 1))
fi

plan="$(scratch_plan --base "$BASE_SHA")"
expect_has "--base covers committed changes" "$plan" "check bash-syntax: kept.bash"
expect_has "--base covers committed additions" "$plan" "check ruff: new.py"

echo
echo "== src-go -> the Go gate =="
plan="$(plan_for src-go/internal/ui/ui.go src-go/go.mod)"
expect_has "go: gate planned" "$plan" "check go"
expect_lacks "go: a Go file is covered" "$plan" "not covered by qa-changed: src-go/internal/ui/ui.go"
expect_lacks "go: go.mod is covered" "$plan" "not covered by qa-changed: src-go/go.mod"
expect_lacks "go: qa-go is not also picked as a suite" "$plan" "suite scripts/qa-go.bash"

plan="$(plan_for scripts/qa-bash.bash)"
expect_lacks "go: untouched when no src-go file changed" "$plan" "check go"

echo
echo "== src-go -> the ccy-teams command suite =="
plan="$(plan_for src-go/internal/teams/app.go)"
expect_has "teams: the package picks the suite" "$plan" "suite scripts/test-ccy-teams-command.bash: builds from src-go/internal/teams/app.go"
plan="$(plan_for src-go/cmd/ccy-teams/main.go)"
expect_has "teams: the command picks the suite" "$plan" "suite scripts/test-ccy-teams-command.bash"
plan="$(plan_for src-go/internal/ui/ui.go)"
expect_has "teams: a shared package it imports picks the suite" "$plan" "suite scripts/test-ccy-teams-command.bash"
plan="$(plan_for src-go/go.mod)"
expect_has "teams: go.mod picks the suite" "$plan" "suite scripts/test-ccy-teams-command.bash"
plan="$(plan_for src-go/internal/transcript/transcript.go)"
expect_lacks "teams: a package it does not import leaves the suite out" "$plan" "test-ccy-teams-command.bash"

echo
echo "== src-go -> the ccy-sessions suites =="
plan="$(plan_for src-go/internal/sessions/notify.go)"
expect_has "sessions: the package picks the reboot suite" "$plan" "suite scripts/test-ccy-sessions-reboot.bash: builds from src-go/internal/sessions/notify.go"
expect_has "sessions: the package picks the take-over suite" "$plan" "suite scripts/test-ccy-sessions-take-over.bash: builds from src-go/internal/sessions/notify.go"
expect_has "sessions: the package picks the restore-askpass suite" "$plan" "suite scripts/test-ccy-restore-askpass.bash: builds from src-go/internal/sessions/notify.go"
plan="$(plan_for src-go/cmd/ccy-sessions/main.go)"
expect_has "sessions: the command picks the suites" "$plan" "suite scripts/test-ccy-sessions-reboot.bash"
plan="$(plan_for src-go/internal/tmuxccy/panes.go)"
expect_has "sessions: a package it imports picks the suites" "$plan" "suite scripts/test-ccy-sessions-take-over.bash"
plan="$(plan_for src-go/go.mod)"
expect_has "sessions: go.mod picks the suites" "$plan" "suite scripts/test-ccy-sessions-reboot.bash"
plan="$(plan_for src-go/internal/agentbus/agentbus.go)"
expect_lacks "sessions:a package it does not import leaves the suites out" "$plan" "test-ccy-sessions-reboot.bash"

echo
echo "== src-go -> every binary has a suite entry =="
# GO_BINARY_SUITES is a hand-kept list; a new src-go/cmd/<x> missing from it would be built and
# deployed with no suite selected for its changes and no warning.
plan="$(plan_for src-go/cmd/ccy-unmapped/main.go)"
expect_lacks "binaries: the control, a command with no entry, selects no suite" "$plan" "suite scripts/"
for cmd_dir in "$SCRIPT_DIR"/../src-go/cmd/*/; do
    cmd_name="$(basename "$cmd_dir")"
    plan="$(plan_for "src-go/cmd/$cmd_name/main.go")"
    expect_has "binaries: $cmd_name has a suite entry" "$plan" "suite scripts/"
done

echo
echo "=============================================================="
printf 'passed: %d   failed: %d\n' "$PASS" "$FAIL"
if [[ "$FAIL" -gt 0 ]]; then
    echo "VERDICT: FAIL — qa-changed maps files to checks wrongly."
    exit 1
fi
echo "VERDICT: PASS"
echo "=============================================================="
