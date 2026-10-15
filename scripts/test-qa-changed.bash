#!/usr/bin/env bash
# Black-box test of scripts/qa-changed.bash (the build-and-run wrapper over the Go tool in
# src-go/qa/changed): its file -> check mapping through --plan, which prints the chosen checks
# without running any of them, and its command line and exit codes. The mapping itself is data:
# scripts/qa-routing.tsv (rules) and scripts/qa-trees.tsv (excluded trees); a change to either
# selects this suite.
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
SCRATCH="$(mktemp -d)"
trap 'rm -rf "$SCRATCH"' EXIT

# The cases run the Go binary, built once here (a build per case made this suite slower than the
# bash it tests); QA_CHANGED_BIN names a prebuilt one instead. The wrapper itself, which builds
# and runs the tool, is exercised by its own section at the end ("the wrapper").
if [[ -n "${QA_CHANGED_BIN:-}" ]]; then
    BIN="$QA_CHANGED_BIN"
    if [[ ! -x "$BIN" ]]; then
        echo "ERROR: QA_CHANGED_BIN=$BIN is not an executable — nothing to test." >&2
        exit 2
    fi
else
    BIN="$SCRATCH/qa-changed"
    go build -C "$SCRIPT_DIR/../src-go/qa" -o "$BIN" ./cmd/qa-changed
fi
TARGET=("$BIN" --scripts "$SCRIPT_DIR")
if [[ ! -f "$SCRIPT_DIR/qa-changed.bash" ]]; then
    echo "ERROR: $SCRIPT_DIR/qa-changed.bash not found — nothing to test." >&2
    exit 2
fi

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
    "${TARGET[@]}" --plan --files "$@"
}

# capture <command...> — run it, keeping stdout in OUT, stderr in ERR and the status in RC.
capture() {
    RC=0
    OUT="$("$@" 2> "$SCRATCH/err")" || RC=$?
    ERR="$(<"$SCRATCH/err")"
}
expect_rc() {
    local label="$1" want="$2"
    if [[ "$RC" -eq "$want" ]]; then
        printf '  ok    %s\n' "$label"
        PASS=$((PASS + 1))
    else
        printf '  FAIL  %s — exit %s, wanted %s\n' "$label" "$RC" "$want"
        FAIL=$((FAIL + 1))
    fi
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
expect_has "vars yml: the version-pins gate, whole" "$plan" "check version-pins"
expect_lacks "vars yml: its gate is a check, not also a suite" "$plan" "suite scripts/qa-version-pins.bash"

plan="$(plan_for scripts/release.bash)"
expect_has "suite picked by name, with the reason" "$plan" "suite scripts/test-release.bash: names scripts/release.bash"

plan="$(plan_for scripts/qa-gates.tsv)"
expect_has "the gate table: the Go tests read the real one" "$plan" "check go"
expect_has "the gate table: the suites that name it run" "$plan" "suite scripts/test-qa-docs-exit-codes.bash: names scripts/qa-gates.tsv"

plan="$(plan_for scripts/test-planlib.bash)"
expect_has "a changed suite runs itself" "$plan" "suite scripts/test-planlib.bash: the suite itself changed"

# Built from parts: this suite's own text would otherwise name the file, and a suite that names
# a changed file covers it — which would turn the "uncovered" fixture into a covered one.
wf_dir=".github/workflows"
wf_name="qa"
plan="$(plan_for "$wf_dir/${wf_name}.yml")"
expect_has "unmapped file is reported" "$plan" "not covered by qa-changed: $wf_dir/${wf_name}.yml"

capture plan_for no/such/file.bash scripts/qa-bash.bash
expect_rc "nonexistent file beside a real one: exit 0" 0
expect_has "nonexistent file is dropped, the others planned" "$OUT" "check shellcheck: scripts/qa-bash.bash"
expect_lacks "nonexistent file is not planned" "$OUT" "no/such/file.bash"

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
    QA_CHANGED_ROOT="$REPO" "${TARGET[@]}" --plan "$@"
}

plan="$(scratch_plan)"
expect_has "modified file is collected" "$plan" "check bash-syntax: kept.bash"
expect_has "untracked file is collected" "$plan" "check ruff: new.py"
# A deleted file has nothing left to check, so no per-file check lists it (it may still run a
# whole gate, or a suite that names it).
expect_lacks "deleted file: no per-file check lists it" "$plan" "kept.bash gone.bash"
expect_lacks "deleted file: not checked per file" "$plan" "check shellcheck: gone.bash"
expect_lacks "ignored file is dropped" "$plan" "ignored.bash"

git_s add -A
git_s commit -q -m second
plan="$(scratch_plan)"
expect_has "clean tree says so" "$plan" "no changed files"

rc=0
QA_CHANGED_ROOT="$REPO" "${TARGET[@]}" > /dev/null || rc=$?
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

plan="$(plan_for src-go/qa/rules/seamexec.go src-go/qa/go.mod src-go/qa/rules/testdata/src/fix/internal/badexec/bad.go)"
expect_has "go: a project-rules file plans the gate" "$plan" "check go"
expect_lacks "go: the nested rules module is covered" "$plan" "not covered by qa-changed: src-go/qa/"
expect_lacks "go: the rules module builds no binary, so picks no ccy suite" "$plan" "suite scripts/test-ccy"

plan="$(plan_for scripts/qa-bash.bash)"
expect_lacks "go: untouched when no src-go file changed" "$plan" "check go"

for ref in files/var/local/claude-yolo/lib/session-registry.bash files/var/local/claude-yolo/lib/common-pure.bash helpers/agent_bus/declare.py; do
    plan="$(plan_for "$ref")"
    expect_has "go: parity reference $ref runs the Go gate" "$plan" "check go"
done

echo
echo "== bash files, the allowlist and its Go source -> the bash-limits gate =="
for changed in scripts/qa-bash.bash scripts/bash-limits.allowlist src-go/qa/bashlimits/measure.go src-go/qa/cmd/qa-bash-limits/main.go files/home/.local/bin/ftp-camera files/home/.local/bin/fedora-desktop-health.j2; do
    plan="$(plan_for "$changed")"
    expect_has "bash-limits: $changed runs the gate" "$plan" "check bash-limits"
done
plan="$(plan_for scripts/bash-limits.allowlist)"
expect_lacks "bash-limits: the allowlist is covered" "$plan" "not covered by qa-changed: scripts/bash-limits.allowlist"
expect_lacks "bash-limits: the gate script is not also picked as a suite" "$plan" "suite scripts/qa-bash-limits.bash"
# By content, not name: an extensionless tool that is not on the allowlist (yet) is bash because of
# its shebang; a file of the same kind with a Python shebang is not.
plan="$(plan_for files/home/.local/bin/tmate-share)"
expect_has "bash-limits: an extensionless bash tool off the allowlist runs the gate" "$plan" "check bash-limits"
plan="$(plan_for files/home/.local/bin/clip-scan)"
expect_lacks "bash-limits: a python-shebang tool does not" "$plan" "check bash-limits"
plan="$(plan_for src-go/internal/ui/ui.go)"
expect_lacks "bash-limits: untouched by an unrelated Go file" "$plan" "check bash-limits"
plan="$(plan_for CLAUDE/QA.md)"
expect_lacks "bash-limits: untouched by markdown" "$plan" "check bash-limits"

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
# scripts/go-binary-suites.tsv is a hand-kept table; a new src-go/cmd/<x> missing from it would be
# built and deployed with no suite selected for its changes and no warning.
# The unmapped command's file does not exist (the control is its absence from the table); README.md
# is the one real file the list needs and selects no suite.
capture plan_for src-go/cmd/ccy-unmapped/main.go README.md
expect_rc "binaries: the control plans: exit 0" 0
expect_lacks "binaries: the control, a command with no entry, selects no suite" "$OUT" "suite scripts/"
for cmd_dir in "$SCRIPT_DIR"/../src-go/cmd/*/; do
    cmd_name="$(basename "$cmd_dir")"
    entry="$(awk -F'\t' -v c="$cmd_name" '$1 == c {print $2}' "$SCRIPT_DIR/go-binary-suites.tsv")"
    plan="$(plan_for "src-go/cmd/$cmd_name/main.go")"
    if [[ "$entry" == "qa-go" ]]; then
        # Its oracle is a Go test: the whole Go gate is the check, and no bash suite is picked.
        expect_has "binaries: $cmd_name's oracle is a Go test, which the Go gate runs" "$plan" "check go"
        expect_lacks "binaries: $cmd_name picks no bash suite for its build closure" "$plan" "builds from"
    else
        expect_has "binaries: $cmd_name has a suite entry" "$plan" "suite scripts/"
    fi
done

echo
echo "== command line and exit codes =="

capture "${TARGET[@]}" --help
expect_rc "--help: exit 0" 0
expect_has "--help: usage on stdout" "$OUT" "usage: qa-changed"
expect_lacks "--help: nothing on stderr" "$ERR" "usage"
capture "${TARGET[@]}" --nope
expect_rc "unknown argument: exit 2" 2
expect_has "unknown argument: named on stderr" "$ERR" "ERROR: unknown argument '--nope'"
capture "${TARGET[@]}" --base
expect_rc "--base without a ref: exit 2" 2
capture "${TARGET[@]}" --base --plan
expect_rc "--base does not take a flag as its ref: exit 2" 2
capture "${TARGET[@]}" --plan --files
expect_rc "--files without a file: exit 2" 2
capture "${TARGET[@]}" --files scripts/qa-bash.bash --plan
expect_rc "--files then --plan: plans" 0
expect_has "--files then --plan: the plan is printed" "$OUT" "check bash-syntax: scripts/qa-bash.bash"
expect_lacks "--files then --plan: nothing ran" "$OUT" "✓"
capture "${TARGET[@]}" --plan --files ./scripts/qa-bash.bash scripts/qa-bash.bash
expect_has "an explicit path is cleaned of ./" "$OUT" "check bash-syntax: scripts/qa-bash.bash"
expect_lacks "a file named twice is one file" "$OUT" "scripts/qa-bash.bash scripts/qa-bash.bash"
capture "${TARGET[@]}" --plan --files no/such/file.bash
expect_rc "a list naming no existing file: exit 2, not nothing to check" 2
expect_has "a list naming no existing file says so" "$ERR" "names no existing file"
capture "${TARGET[@]}" --plan --files ../outside.bash
expect_rc "a path outside the repository: exit 2" 2
capture env QA_CHANGED_ROOT=/no/such/directory "${TARGET[@]}" --plan
expect_rc "an unreadable root: exit 2" 2
capture env QA_CHANGED_TIMEOUT=soon "${TARGET[@]}" --plan --files scripts/qa-bash.bash
expect_rc "a bad QA_CHANGED_TIMEOUT: exit 2" 2

echo
echo "== a deleted allowlisted tool, a bad base, the lint config, task files =="
capture env QA_CHANGED_ROOT="$REPO" "${TARGET[@]}" --plan --base nosuchref
expect_rc "a bad --base is exit 2, not 'nothing to check'" 2
expect_lacks "a bad --base does not say nothing to check" "$OUT" "nothing to check"

mkdir -p "$REPO/files/home/.local/bin"
printf '#!/usr/bin/env python3\n' > "$REPO/files/home/.local/bin/ftp-camera"
git_s add -A
git_s commit -q -m "a listed tool"
rm "$REPO/files/home/.local/bin/ftp-camera"
capture env QA_CHANGED_ROOT="$REPO" "${TARGET[@]}" --plan
expect_has "a deleted allowlisted tool runs the bash-limits gate" "$OUT" "check bash-limits"
expect_lacks "a deleted file is not checked per file" "$OUT" "check shellcheck: files/home"

capture "${TARGET[@]}" --plan --files src-go/.golangci.yml
expect_has "the Go lint config runs the Go gate" "$OUT" "check go"
expect_lacks "the Go lint config is covered" "$OUT" "not covered by qa-changed: src-go/.golangci.yml"
capture "${TARGET[@]}" --plan --files tasks/build-go-command.yml
expect_has "a task file runs the patterns gate" "$OUT" "check ansible-patterns"
expect_has "a task file maps to the playbooks that name it" "$OUT" "check ansible-syntax: "

echo
echo "== the wrapper (the one section that builds and runs the tool through qa-changed.bash) =="
capture bash "$SCRIPT_DIR/qa-changed.bash" --plan --files scripts/qa-bash.bash
expect_rc "the wrapper builds the tool and runs it: exit 0" 0
expect_has "the wrapper passes its arguments through" "$OUT" "check shellcheck: scripts/qa-bash.bash"
capture env -u HOME -u XDG_CACHE_HOME bash "$SCRIPT_DIR/qa-changed.bash" --plan --files scripts/qa-bash.bash
expect_rc "no home and no cache directory: exit 2" 2
expect_has "no home names the missing variable" "$ERR" "neither XDG_CACHE_HOME nor HOME is set"

echo
echo "=============================================================="
printf 'passed: %d   failed: %d\n' "$PASS" "$FAIL"
if [[ "$FAIL" -gt 0 ]]; then
    echo "VERDICT: FAIL — qa-changed maps files to checks wrongly."
    exit 1
fi
echo "VERDICT: PASS"
echo "=============================================================="
