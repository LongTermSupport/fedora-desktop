#!/usr/bin/bash
# Run all QA checks - LLM-friendly
# stdout:  terse — errors + final summary only
# JSON:    /tmp/qa-results.json
#
# jq usage:
#   jq '.status'               # "pass" or "fail"
#   jq '.summary'              # {total, passed, failed}
#   jq '.failures[]'           # all failures across bash + python
#   jq '.checks.bash'          # bash-specific results
#   jq '.checks.python'        # python-specific results
#   jq '.checks.python.ruff_diagnostics[]'  # ruff issues

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The three stage-line readers. Between them they produce the SUMMARY in every stage line
# below — 36 of them, counted: `qa_gate_case_count` 26, `qa_gate_detail` 9,
# `helper_counts_summary` 1. `qa_pass_line` prints it; only `deployed-drift` composes its own
# line, because there the line IS the gate's output rather than a summary of it. Sourced
# rather than inlined so a committed test can drive the real functions — see the library
# header.
# shellcheck source-path=SCRIPTDIR
# shellcheck source=lib/qa-helper-summary.bash
source "$SCRIPT_DIR/lib/qa-helper-summary.bash"

JSON_OUT="/tmp/qa-results.json"
TMP_BASH=$(mktemp)
TMP_PYTHON=$(mktemp)
TMP_PATTERNS=$(mktemp)
TMP_ANSIBLE=$(mktemp)
TMP_ANSIBLE_SYNTAX=$(mktemp)
TMP_JS=$(mktemp)
TMP_DOCS=$(mktemp)
# Every temp file this script owns is cleaned by ONE trap. A second `trap ... EXIT` would
# silently REPLACE this one rather than add to it, leaking the seven above on every run.
TMP_HELPER_ERR=$(mktemp)
TMP_HELPER_OUT=$(mktemp)
TMP_HELPER_COUNTS=$(mktemp)
trap 'rm -f "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" "$TMP_DOCS" "$TMP_HELPER_ERR" "$TMP_HELPER_OUT" "$TMP_HELPER_COUNTS"' EXIT
FAILED=0

# EVERY GATE RUNS, EVEN AFTER ONE FAILS (Plan 00125, Task 4.3).
#
# A hard gate used to `exit 1` on failure, which disabled every gate declared after it. That
# is not a theoretical cost: while `helper-tests` was red the masked set grew 5 -> 11 -> 25,
# and 20 of those gates had never executed in CI even once. The run's output said "one thing
# is broken" when it meant "one thing is broken and 25 things are unknown", and nothing
# distinguished the two.
#
# Seven jq-merged stages already worked this way and accumulate into FAILED; this extends the
# same design to the hard gates rather than inventing one. The `exit 2` missing-tool aborts
# below STAY aborts — a suite that cannot run its tools has nothing to accumulate.
HARD_FAILED=()

# qa_hard_gate_failed <stage-name> <reason> <capture>
#
# The ✗ line is a STAGE LINE and goes to stdout beside the ✓ lines, which is the half that
# is easy to miss. `verdicts.py` matches `^[✓✗⚠] QA (?:passed|FAILED):` as a RUN SUMMARY
# before it tries the stage pattern, so the old `✗ QA FAILED: <prose>` gave the failing gate
# no stage line at all — it erased ITSELF from the census as well as the gates behind it.
# Measured: a three-line sample ending in that abort parses to the two passing stages only.
# The gate's own output is the diagnostic and goes to stderr.
qa_hard_gate_failed() {
    local name="$1" reason="$2" capture="$3"
    # An empty capture is legitimate: `helper_counts_summary` reports on stderr itself and
    # has nothing to hand on. Printing it anyway would put a blank line in the diagnostics.
    if [[ -n "$capture" ]]; then
        printf '%s\n' "$capture" >&2
    fi
    printf '✗ %s: %s\n' "$name" "$reason"
    HARD_FAILED+=("$name")
}

# qa_pass_line <stage-name> <summary> — the ✓ line, unless this gate already failed.
#
# One function owns the ✓ line now, so a gate cannot report both outcomes: without the
# guard, a failed gate would still reach its summary line and print a ✓ built from its own
# failure output. The three readers still produce every SUMMARY; this prints it.
qa_pass_line() {
    local name="$1" summary="$2" failed=""
    for failed in ${HARD_FAILED[@]+"${HARD_FAILED[@]}"}; do
        if [[ "$failed" == "$name" ]]; then
            return 0
        fi
    done
    printf '✓ %s: %s\n' "$name" "$summary"
}

# Toolchain assertion FIRST, because every verdict below is a property of the
# binary that produced it. Reading 44 results and only then learning the linter
# was the wrong version means re-reading all 44 wondering which changed.
#
# It is a hard gate, NOT one of the seven merged stages: it reports on the
# environment rather than on any file, so it has no per-file JSON to contribute
# and must not disturb the positional .[0]..[6] merge.
#
# Deliberately does not abort. A drifted toolchain makes the gates below
# unreliable, not unrunnable, and their output is still the most useful thing
# available while somebody fixes the version.
toolchain_out=""
if ! toolchain_out="$(bash "$SCRIPT_DIR/qa-toolchain.bash" 2>&1)"; then
    qa_hard_gate_failed toolchain \
        "QA tool versions do not match .qa-versions — verdicts below are not comparable" \
        "$toolchain_out"
fi
toolchain_summary=$(qa_gate_detail "$toolchain_out" 'TOOLCHAIN-OK .+')
qa_pass_line toolchain "$toolchain_summary"

# Run sub-checks (each writes JSON to temp file, outputs terse to stdout)
# Exit code 2 = missing required tool — refuse to run entirely
rc=0
QA_JSON_OUT="$TMP_BASH" "$SCRIPT_DIR/qa-bash.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_PYTHON" "$SCRIPT_DIR/qa-python.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_PATTERNS" "$SCRIPT_DIR/qa-patterns.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (semgrep). Install with: pipx install semgrep" >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# Ansible checks (fail-fast patterns + playbook hygiene)
rc=0
QA_JSON_OUT="$TMP_ANSIBLE" "$SCRIPT_DIR/qa-ansible.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools for ansible check. Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_ANSIBLE_SYNTAX" "$SCRIPT_DIR/qa-ansible-syntax.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (ansible-playbook). Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

rc=0
QA_JSON_OUT="$TMP_JS" "$SCRIPT_DIR/qa-js.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: Missing required tools (node / extensions node_modules). Install them and re-run." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# Documentation integrity (Plan 00070): link/anchor resolution, playbook
# catalogue completeness, topic-file index. Scoped to CORE docs — the plan tree
# is excluded so archiving a plan can never change this gate's verdict.
#
# qa-ansible-syntax takes the OPPOSITE view of CLAUDE/Plan/** and both are right:
# a playbook in a plan folder is a playbook and must parse, while a plan's PROSE
# is a working document whose links legitimately churn. Two gates, two policies,
# stated here so neither reads as an oversight.
rc=0
QA_JSON_OUT="$TMP_DOCS" "$SCRIPT_DIR/qa-docs.bash" || rc=$?
if [[ $rc -eq 2 ]]; then
    echo "ERROR: docs gate could not produce a result (zero-file scan or checker crash)." >&2
    exit 2
elif [[ $rc -ne 0 ]]; then
    FAILED=$((FAILED + 1))
fi

# L0 no-kill safety gate (Plan 00055): the container-watch watchdog is
# reporting-only and must never gain a process-termination call site. This is a
# minimal, non-structural HARD gate — deliberately NOT a 7th jq-merged stage, so
# it cannot corrupt the positional .[0]..[6] JSON merge below. It fails the whole
# run immediately if a forbidden kill call site is introduced.
nokill_out=""
if ! nokill_out="$(bash "$SCRIPT_DIR/qa-nokill-containerwatch.bash" 2>&1)"; then
    qa_hard_gate_failed nokill-containerwatch \
        "rejected a process-termination call site in the container-watch watchdog" \
        "$nokill_out"
fi
# Pass line, for the same reason the drift gate below prints one: a gate whose
# only visible output is a failure is indistinguishable from a gate that is not
# running. That rule was written down beside the drift gate and never applied to
# this one, six lines above it.
# The pattern is what the gate ACTUALLY prints, checked against a real run rather than
# assumed: the previous one (`[0-9]+ call site[s]? checked`) never matched in its whole life,
# and the `||` fallback asserted `no forbidden kill call sites` on every run instead.
nokill_summary=$(qa_gate_detail "$nokill_out" '[0-9]+ container-watch file[(]s[)] clean')
qa_pass_line nokill-containerwatch "$nokill_summary"

# Deployed-drift gate (Plan 00099): a repo-owned user script that was changed
# but never deployed means the host is running different code from the one QA
# just passed. Like the no-kill gate above, deliberately NOT a jq-merged stage
# — it inspects the HOST, not the source tree, and self-skips where there is no
# host to inspect (CCY container, clean CI checkout).
drift_out=""
if drift_out="$(bash "$SCRIPT_DIR/qa-deployed-drift.bash" 2>&1)"; then
    # Print the pass line too. A gate whose only visible output is a failure is
    # indistinguishable from a gate that is not running — and "a check that silently
    # does nothing" is precisely the defect Plan 00099 exists to fix.
    #
    # This one composes its own stage line rather than going through `qa_pass_line`,
    # because the line it prints is the gate's whole output, not a summary of it.
    echo "$drift_out"
else
    qa_hard_gate_failed deployed-drift \
        "repo-owned scripts differ from their deployed copies" \
        "$drift_out"
fi

# Helper unit tests + extension GNOME-version compatibility (Plan 00081 F11).
#
# CLAUDE/QA.md says "ALWAYS and ONLY use ./scripts/qa-all.bash" and "NEVER use
# individual scripts directly" — and then documented these two as gates this
# script did not run. Following the stated rule, a helpers/ change got
# "✓ QA passed" with its whole unit suite never executed. The two ways to fix that
# were to run them or to stop claiming qa-all is sufficient; running them is the
# one that makes the instruction people actually follow the correct one.
#
# Hard, non-structural gates like the two above — deliberately NOT jq-merged
# stages, so they cannot disturb the positional .[0]..[6] merge below. Both are
# fast (the suite is ~0.06s; the compat check is static).
# THE COUNTS ARRIVE IN A FILE, NOT IN THE OUTPUT, and that is load-bearing rather than
# tidy. `--counts-file` makes the runner write the two numbers straight from unittest's
# TestResult object, so nothing a test prints shares a channel with them. Four readers that
# scraped this run's text were each defeated by a test printing unittest-shaped output —
# the header of scripts/lib/qa-helper-summary.bash records all four.
#
# The run's human-readable output all goes to stderr, deliberately on one stream so its
# ordering is the true ordering, and is shown in full when the suite fails.
#
# The token detects a counts file written by something that did NOT read this run's argv —
# a stale file, a concurrent run, a hardcoded path. It is not a lock against a test: the
# token travels in the same argv as the path, so anything that finds the file BY READING
# ARGV has the token too. (The qualifier is load-bearing. Without it the sentence says the
# token buys nothing, which contradicts the hardcoded-path case one line above.) What
# actually defeats a clobber from inside the suite is WRITE ORDERING — the runner writes
# after every test has finished, so a forgery landing mid-run is simply overwritten. A
# write that lands AFTER the runner's, from `atexit` or a thread, defeats both; nothing
# here detects that, and saying so is the point of this paragraph.
#
# THREE WAYS TO FAIL, ONE GATE. This is the only gate with more than one failure point, and
# they are sequential rather than independent: if the suite did not run, its stdout and its
# counts file say nothing. So a flag carries the first failure forward and the later checks
# stand down, which keeps `helper-tests` in the failed list exactly once. Recording it three
# times would inflate the census the ✗ stage lines exist to make accurate.
TMP_HELPER_TOKEN="qa-all-$$-$(date +%s%N)"
helper_tests_ok=1
if ! bash "$SCRIPT_DIR/qa-helper-tests.bash" --counts-file "$TMP_HELPER_COUNTS" \
    --counts-token "$TMP_HELPER_TOKEN" >"$TMP_HELPER_OUT" 2>"$TMP_HELPER_ERR"; then
    qa_hard_gate_failed helper-tests \
        "the helper unit suite failed" \
        "$(cat "$TMP_HELPER_OUT" "$TMP_HELPER_ERR")"
    helper_tests_ok=0
fi

# The child's stdout is CAPTURED, like every other hard gate's, and then required to be
# empty. Leaving it inherited put it in this script's own stdout — the stream `verdicts.py`
# parses for `^[✓✗⚠] name: ` stage lines — so a single `print()` anywhere in the suite could
# forge a stage line or split this one. An unenforced precondition that broad is exactly the
# shape this plan exists to remove, so it is a gate rather than a comment. (The test count
# was written here and in CLAUDE/QA.md until both had rotted and disagreed; the stage line
# prints the live number every run, which is where a count belongs.)
if [[ $helper_tests_ok -eq 1 && -s "$TMP_HELPER_OUT" ]]; then
    qa_hard_gate_failed helper-tests \
        "wrote to stdout, which is this suite's verdict stream" \
        "$(printf '%s\n' \
            "  A test printing here can forge or split a stage line. If it is a test's own" \
            "  print, wrap it in contextlib.redirect_stdout; if it is a subprocess a test" \
            "  spawned, capture that subprocess rather than letting it inherit." \
            "$(cat "$TMP_HELPER_OUT")")"
    helper_tests_ok=0
fi
# The skip count travels with the line because `unittest` counts a SKIPPED test inside
# testsRun: "Ran 1456 tests" is byte-identical whether a test asserted or skipped itself.
# Two machines then report the same verdict over different executed populations — which is
# this repo's own machine-dependence defect appearing inside the line used to detect it.
# Measured: a container asserts the DisplayLink sysfs pair against a real connector while a
# VM runner skips both, and before this the two lines agreed exactly.
#
# The reader lives in scripts/lib/ rather than here because this line has been wrong four
# times and each hand-check was thrown away with the session that made it.
# scripts/test-qa-helper-summary.bash drives it, and its own gate runs below.
if [[ $helper_tests_ok -eq 1 ]]; then
    if helper_summary="$(helper_counts_summary "$TMP_HELPER_COUNTS" "$TMP_HELPER_TOKEN")"; then
        qa_pass_line helper-tests "$helper_summary"
    else
        qa_hard_gate_failed helper-tests \
            "ran, but its counts could not be read" \
            ""
    fi
fi

# The pre-commit secret scanner's own unit suite (scripts/test-secret-scan.bash).
#
# The scanner LIBRARY runs on every commit, but a false-NEGATIVE regression in it
# is silent by construction — a leak it stopped catching produces no signal at
# all. That is the whole reason the suite exists, so "it is exercised on every
# commit anyway" is not a reason to leave it unwired. Every case is synthetic and
# the whole suite is sub-second. Same shape as the helper-tests gate above.
scan_out=""
if ! scan_out="$(bash "$SCRIPT_DIR/test-secret-scan.bash" 2>&1)"; then
    qa_hard_gate_failed secret-scan-tests \
        "secret scanner unit tests failed" \
        "$scan_out"
fi
scan_summary=$(qa_gate_case_count "$scan_out")
qa_pass_line secret-scan-tests "$scan_summary"

# The plan-script library's own regression suite (scripts/test-planlib.bash).
#
# _planlib.inc.bash backs EVERY plan script in the repo — the deploy, triage and
# acceptance scripts a human runs on the host — so a regression in it breaks the
# tooling of plans that are not being worked on and whose scripts nobody will run
# for months. One of its cases pins a defect that broke a live host deploy.
# Nothing ran it automatically, which made it a suite whose passing was a matter
# of someone remembering. Sub-second, same shape as the two gates above.
planlib_out=""
if ! planlib_out="$(bash "$SCRIPT_DIR/test-planlib.bash" 2>&1)"; then
    qa_hard_gate_failed planlib-tests \
        "plan-script library regression tests failed" \
        "$planlib_out"
fi
planlib_summary=$(qa_gate_detail "$planlib_out" 'PASSED [(]library version [0-9.]+[)]')
qa_pass_line planlib-tests "$planlib_summary"

# ccy's rootless-engine guard (Plan 00072), wired in by Plan 00081.
#
# This ran in .github/workflows/qa.yml and NOWHERE locally, which is a worse
# version of the hole the helper-tests gate above exists to close: not
# "documented but unrun", but "green here, red in CI" — so this file's own
# promise that qa-all.bash is sufficient was false for anyone touching
# lib/common-pure.bash. The decision under test is a pure function of the
# engine's report, so it needs no podman and no daemon. 15 cases, ~0.03s.
rootless_out=""
if ! rootless_out="$(bash "$SCRIPT_DIR/test-ccy-rootless-guard.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-rootless-guard \
        "ccy rootless-engine guard unit tests failed" \
        "$rootless_out"
fi
rootless_summary=$(qa_gate_case_count "$rootless_out")
qa_pass_line ccy-rootless-guard "$rootless_summary"

# select_token's per-mode answer to an unusable token pool (Plan 00048, CCY 3.50.0).
#
# select_token is shared by both launchers, and the two modes must answer an
# unusable pool differently — ccy hard-stops, host cc offers Desktop. A bug shipped
# because one branch conflated "the pool is empty" (falling through to Desktop is
# the design) with "the pool has tokens, all past their GUESSED +90d stamp"
# (falling through silently authenticates as a DIFFERENT Claude account, with no
# error and no non-zero exit). Nothing was red while that was true.
#
# Every case returns before select_token's `read -p` menu, so this needs no
# terminal, no claude binary and no real credential — only filenames, which is all
# is_token_valid reads. The suite asserts container mode is unchanged as well as
# the fix itself, because the edit is in a function ccy also calls.
token_mode_out=""
if ! token_mode_out="$(bash "$SCRIPT_DIR/test-ccy-token-mode.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-token-mode \
        "ccy token-mode unit tests failed" \
        "$token_mode_out"
fi
token_mode_summary=$(qa_gate_case_count "$token_mode_out")
qa_pass_line ccy-token-mode "$token_mode_summary"

# ccy's SSH identity resolution (Plan 00116, CCY 3.54.0).
#
# A box provisioned with per-repository deploy keys and no GitHub account holds no
# ~/.ssh/github_* key, and ccy used to see nothing else — it warned "No github_
# SSH keys found" in a project whose remote named a working key through an
# ssh-config alias. The suite drives alias resolution (`ssh -G`), remote-URL
# parsing, the container stanza, the agent checks and deploy-key classification
# against a stub ssh/ssh-add on PATH: no network, no agent, no real key.
ssh_handling_out=""
if ! ssh_handling_out="$(bash "$SCRIPT_DIR/test-ccy-ssh-handling.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-ssh-handling \
        "ccy ssh-handling unit tests failed" \
        "$ssh_handling_out"
fi
ssh_handling_summary=$(qa_gate_case_count "$ssh_handling_out")
qa_pass_line ccy-ssh-handling "$ssh_handling_summary"

# ccy's commit signing in the container (Plan 00139, CCY 3.66.0): the signing key staged
# beside the gitconfig copy and the copy repointed at it, and a launch refused when
# signing is on with no usable key. Fixture keys and configs only; no container.
git_signing_out=""
if ! git_signing_out="$(bash "$SCRIPT_DIR/test-ccy-git-signing.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-git-signing \
        "ccy commit-signing unit tests failed" \
        "$git_signing_out"
fi
git_signing_summary=$(qa_gate_case_count "$git_signing_out")
qa_pass_line ccy-git-signing "$git_signing_summary"

# gh-scope-outside-ssot (Defence Before Fix; CLAUDE/QA.md). A GitHub OAuth scope named
# anywhere but vars/github-required-scopes.yml and helpers/github_scopes/ is a second copy
# that drifts, and the owner then authorises GitHub more than once.
gh_scopes_out=""
if ! gh_scopes_out="$(bash "$SCRIPT_DIR/qa-gh-scopes.bash" 2>&1)"; then
    qa_hard_gate_failed gh-scope-outside-ssot \
        "GitHub scopes named outside vars/github-required-scopes.yml" \
        "$gh_scopes_out"
fi
gh_scopes_summary=$(qa_gate_case_count "$gh_scopes_out")
qa_pass_line gh-scope-outside-ssot "$gh_scopes_summary"

# ccy's SELinux relabel decision (Plan 00118, CCY 3.55.0).
#
# On an Enforcing host container_t may not read user_home_t, so a ccy container
# could not read the project it was handed; desktops never showed it because they
# are not enforcing. The decision is a pure function of getenforce's text and the
# engine's own report, driven here across every pair a real host could produce.
selinux_verdict_out=""
if ! selinux_verdict_out="$(bash "$SCRIPT_DIR/test-ccy-selinux-verdict.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-selinux-verdict \
        "ccy selinux-verdict unit tests failed" \
        "$selinux_verdict_out"
fi
selinux_verdict_summary=$(qa_gate_case_count "$selinux_verdict_out")
qa_pass_line ccy-selinux-verdict "$selinux_verdict_summary"

# gpu_device_flags (Plan 00120): the GPU device is handed to the container only where the host
# has /dev/dri; a headless server used to abort the run. Driven with a present directory, an
# absent path and a plain file, plus a check that the launcher consumes the array.
gpu_device_out=""
if ! gpu_device_out="$(bash "$SCRIPT_DIR/test-ccy-gpu-device.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-gpu-device \
        "ccy gpu-device unit tests failed" \
        "$gpu_device_out"
fi
gpu_device_summary=$(qa_gate_case_count "$gpu_device_out")
qa_pass_line ccy-gpu-device "$gpu_device_summary"

# ccy_host_hostname (Plan 00121): CCY_HOST_HOSTNAME tells the container which MACHINE it is
# on, since its own HOSTNAME is the container id. The value reaches a `podman run -e`
# argument and is then read by shells in the container, so the grammar is the guard — driven
# through plain names, FQDNs, and the refusals including shell metacharacters and an empty
# nodename.
host_hostname_out=""
if ! host_hostname_out="$(bash "$SCRIPT_DIR/test-ccy-host-hostname.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-host-hostname \
        "ccy host-hostname unit tests failed" \
        "$host_hostname_out"
fi
host_hostname_summary=$(qa_gate_case_count "$host_hostname_out")
qa_pass_line ccy-host-hostname "$host_hostname_summary"

# The ccy-sessions network column: a tmux session and a container know nothing about each
# other, so the picker joins them through the process tree. Get the walk wrong and a row
# labels a session with ANOTHER session's network — worse than showing nothing, because a
# reader would act on it. Driven across both engines, `--name` in both spellings, podman's
# re-exec, a non-engine process carrying `--name`, a container outside every session tree,
# and both engines' renderings of an empty network list.
session_network_out=""
if ! session_network_out="$(bash "$SCRIPT_DIR/test-ccy-session-network.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-session-network \
        "ccy session-network unit tests failed" \
        "$session_network_out"
fi
session_network_summary=$(qa_gate_case_count "$session_network_out")
qa_pass_line ccy-session-network "$session_network_summary"

# `ccy --disconnect`, the undo for a wrong `--connect`. --connect also saves the network as
# the project's default, which every later launch reconnects to, so the undo must clear
# that default exactly when it names the network: every case checks the saved default as
# well as the engine calls, against a stubbed engine, the picker's re-prompt included.
network_disconnect_out=""
if ! network_disconnect_out="$(bash "$SCRIPT_DIR/test-ccy-network-disconnect.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-network-disconnect \
        "ccy --disconnect unit tests failed" \
        "$network_disconnect_out"
fi
network_disconnect_summary=$(qa_gate_case_count "$network_disconnect_out")
qa_pass_line ccy-network-disconnect "$network_disconnect_summary"

# ssh-suspend-guard's session detection: the guard asked `ss` about port 22 and grepped for
# sshd, so on a host whose sshd listens elsewhere it never took the inhibit lock and the
# machine suspended mid-session — the one outcome it exists to prevent, arriving silently.
# It had no test at all: a daemon loop whose failure only shows on a host that moved its
# port. The detection is a function now, driven against captured `ss` output.
#
# The summary is read with qa_gate_detail rather than qa_gate_case_count: this suite reports
# `VERDICT: PASS`, not a `passed: N` line, and qa_gate_case_count's fallback would print a
# bare `passed` — reachable by no other gate, and exactly the blind-reader shape the library
# header warns about.
ssh_suspend_guard_out=""
if ! ssh_suspend_guard_out="$(bash "$SCRIPT_DIR/test-ssh-suspend-guard.bash" 2>&1)"; then
    qa_hard_gate_failed ssh-suspend-guard \
        "ssh-suspend-guard session-detection unit tests failed" \
        "$ssh_suspend_guard_out"
fi
ssh_suspend_guard_summary=$(qa_gate_detail "$ssh_suspend_guard_out" 'VERDICT: PASS')
qa_pass_line ssh-suspend-guard "$ssh_suspend_guard_summary"

# The ccy session registry (Plan 00135): a restore after a reboot reads nothing but this. A
# record must appear on start, go when the launcher returns, and STAY when the pane is
# killed — the real trampoline is run and kill -KILLed here. The replay filter is the other
# half: `--prevent` in a record would switch ccy off for the project it restores, so the
# one-shot set is driven case by case, with `--model opus` kept and a bare first message
# dropped. The restore itself is tested as a translation over a stubbed live list.
session_registry_out=""
if ! session_registry_out="$(bash "$SCRIPT_DIR/test-ccy-session-registry.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-session-registry \
        "ccy session-registry unit tests failed" \
        "$session_registry_out"
fi
session_registry_summary=$(qa_gate_case_count "$session_registry_out")
qa_pass_line ccy-session-registry "$session_registry_summary"

# ccy-sessions notify / reboot / restore (Plan 00135): the REAL executable under a fake
# tmux, a fake systemctl and a per-project stand-in for the daemon CLI. Every project
# signalled exactly once; a project with no daemon CLI refuses BEFORE anything is signalled
# and reboots nothing; --dry-run touches neither; the one-minute second warning fires;
# `systemctl reboot` is the last call; the bare picker path still demands a terminal.
sessions_reboot_out=""
if ! sessions_reboot_out="$(bash "$SCRIPT_DIR/test-ccy-sessions-reboot.bash" 2>&1)"; then
    qa_hard_gate_failed ccy-sessions-reboot \
        "ccy-sessions reboot/notify/restore unit tests failed" \
        "$sessions_reboot_out"
fi
sessions_reboot_summary=$(qa_gate_case_count "$sessions_reboot_out")
qa_pass_line ccy-sessions-reboot "$sessions_reboot_summary"

# host_only_preflight (Plan 00121): the host-CLI gate on a scenario that puts a real GitHub
# PAT into a guest. One of three independent gates — the other two are the bridge allowlist
# and bridge_run's manifest refusal — and the one a human types past. Driven through the
# bridge marker, both deployed enumerations, and every way a secret file can be wrong.
host_only_gate_out=""
if ! host_only_gate_out="$(bash "$SCRIPT_DIR/test-vmtest-host-only-gate.bash" 2>&1)"; then
    qa_hard_gate_failed vmtest-host-only-gate \
        "vmtest host-only gate unit tests failed" \
        "$host_only_gate_out"
fi
host_only_gate_summary=$(qa_gate_case_count "$host_only_gate_out")
qa_pass_line vmtest-host-only-gate "$host_only_gate_summary"

# reboot_guest / guest_prepare (Plan 00109): whether a run is judged before or after a
# fresh boot. A profile with no reboot mechanics, or a fixture that failed, would leave
# the checker judging the boot that provisioned the guest — a green transcript for a
# scenario that never happened, with no symptom anywhere else.
reboot_dispatch_out=""
if ! reboot_dispatch_out="$(bash "$SCRIPT_DIR/test-vmtest-reboot-dispatch.bash" 2>&1)"; then
    qa_hard_gate_failed vmtest-reboot-dispatch \
        "vmtest reboot dispatch unit tests failed" \
        "$reboot_dispatch_out"
fi
reboot_dispatch_summary=$(qa_gate_case_count "$reboot_dispatch_out")
qa_pass_line vmtest-reboot-dispatch "$reboot_dispatch_summary"

# The kernel selection inside that fixture (Plan 00109). The only step of the route no
# machine here can reach: this container has no dnf, rpm or grubby, and the only other
# executor is a guest twenty minutes into a provisioning run. Every check in the scenario
# stands on it — a guest that reboots into the kernel it already ran makes the claim under
# test vacuously false, and the fourteen checks after it judge nothing.
kernel_selection_out=""
if ! kernel_selection_out="$(bash "$SCRIPT_DIR/test-vmtest-kernel-selection.bash" 2>&1)"; then
    qa_hard_gate_failed vmtest-kernel-selection \
        "vmtest kernel selection unit tests failed" \
        "$kernel_selection_out"
fi
kernel_selection_summary=$(qa_gate_case_count "$kernel_selection_out")
qa_pass_line vmtest-kernel-selection "$kernel_selection_summary"

# The fixture→checker record contract (Plan 00109). One scenario's fixture writes a file
# the checker sources, and that seam is invisible to every other gate: a value carrying a
# shell metacharacter aborts the source and unsets every key after it, while the file
# still exists and the source still "happened".
prepare_record_out=""
if ! prepare_record_out="$(bash "$SCRIPT_DIR/test-vmtest-prepare-record.bash" 2>&1)"; then
    qa_hard_gate_failed vmtest-prepare-record \
        "vmtest prepare-record contract tests failed" \
        "$prepare_record_out"
fi
prepare_record_summary=$(qa_gate_case_count "$prepare_record_out")
qa_pass_line vmtest-prepare-record "$prepare_record_summary"

# The panel's decisions (Plan 00109): the shipped statusDocument.js and sections/health.js
# driven against boot-stale, malformed and state-disagreeing documents. The contract gate
# beside this one proves the two languages share a vocabulary; it cannot tell a demoted
# finding from a current one, and on the primary surface for these findings that is the
# whole question.
panel_sections_out=""
if ! panel_sections_out="$(bash "$SCRIPT_DIR/test-panel-sections.bash" 2>&1)"; then
    qa_hard_gate_failed panel-sections \
        "panel section unit tests failed" \
        "$panel_sections_out"
fi
panel_sections_summary=$(qa_gate_case_count "$panel_sections_out")
qa_pass_line panel-sections "$panel_sections_summary"

# hl_write_localhost_yml (Plan 00119): the headless localhost.yml writer, driven through the
# 443 flag on/off/unset, the empty-identity path and the keep-existing-file promise.
localhost_yml_out=""
if ! localhost_yml_out="$(bash "$SCRIPT_DIR/test-run-bash-headless-localhost-yml.bash" 2>&1)"; then
    qa_hard_gate_failed run-bash-headless-localhost-yml \
        "run.bash headless localhost.yml unit tests failed" \
        "$localhost_yml_out"
fi
localhost_yml_summary=$(qa_gate_case_count "$localhost_yml_out")
qa_pass_line run-bash-headless-localhost-yml "$localhost_yml_summary"

# hl_ssh_agent_stop (Plan 00063 Task 3.4): the headless ssh-agent teardown, driven through a
# clean kill, an already-gone agent and — the case that matters — an agent that SURVIVES the
# kill. `ssh-agent -k` returns non-zero for both of the last two, and reporting the survivor
# as the harmless one left an unlocked key reachable through $SSH_AUTH_SOCK for the rest of
# the run while exiting 0.
ssh_agent_out=""
if ! ssh_agent_out="$(bash "$SCRIPT_DIR/test-run-bash-ssh-agent-teardown.bash" 2>&1)"; then
    qa_hard_gate_failed run-bash-ssh-agent-teardown \
        "run.bash ssh-agent teardown unit tests failed" \
        "$ssh_agent_out"
fi
ssh_agent_summary=$(qa_gate_case_count "$ssh_agent_out")
qa_pass_line run-bash-ssh-agent-teardown "$ssh_agent_summary"

# run.bash single-play mode (Plan 00137 T1.4 + T2.1): the real run.bash, in a throwaway
# checkout, runs one play unattended (sudo-only preflight, password via an inherited
# descriptor, stdin closed, the play's own exit status), and every single play takes the
# host's play lock, so a second run exits 75 and a delegated descriptor is proven first.
single_play_out=""
if ! single_play_out="$(bash "$SCRIPT_DIR/test-run-bash-single-play.bash" 2>&1)"; then
    qa_hard_gate_failed run-bash-single-play \
        "run.bash single-play / play-lock tests failed" \
        "$single_play_out"
fi
single_play_summary=$(qa_gate_case_count "$single_play_out")
qa_pass_line run-bash-single-play "$single_play_summary"

# GitHub scopes in one pass (Plan 00139 Task 1.3): run.bash's first login asks for every
# scope in vars/github-required-scopes.yml, a short token gets ONE refresh carrying all it
# lacks, and a headless run - run.bash's or gh-account-setup.bash's - fails once naming
# every account and every scope, against a stubbed gh and the real helper.
gh_scopes_out=""
if ! gh_scopes_out="$(bash "$SCRIPT_DIR/test-run-bash-gh-scopes.bash" 2>&1)"; then
    qa_hard_gate_failed run-bash-gh-scopes \
        "run.bash / gh-account-setup.bash single-pass scope tests failed" \
        "$gh_scopes_out"
fi
gh_scopes_summary=$(qa_gate_case_count "$gh_scopes_out")
qa_pass_line run-bash-gh-scopes "$gh_scopes_summary"

# The unattended self-update cycle (Plan 00137), driven through the real root wrapper
# against a signed git fixture, with runuser and systemctl stubbed: the order of play,
# warning and reboot, the passwords handed as descriptors, and the pinned system ansible.
self_update_cycle_out=""
if ! self_update_cycle_out="$(bash "$SCRIPT_DIR/test-self-update-cycle.bash" 2>&1)"; then
    qa_hard_gate_failed self-update-cycle \
        "fedora-desktop-self-update end-to-end tests failed" \
        "$self_update_cycle_out"
fi
self_update_cycle_summary=$(qa_gate_case_count "$self_update_cycle_out")
qa_pass_line self-update-cycle "$self_update_cycle_summary"

# The run-log secret scrubber (Plan 00121). Redaction is the easy half; what this gate exists
# for is `scrub_verify` REFUSING an artefact where redaction missed a secret. A scrubber is
# fail-open by nature — it writes a file it believes is clean and a miss is silent — so the
# assertion that matters is driven by a fixture where the redactor was deliberately not told
# about one of the secrets.
run_log_scrub_out=""
if ! run_log_scrub_out="$(bash "$SCRIPT_DIR/test-run-log-scrub.bash" 2>&1)"; then
    qa_hard_gate_failed run-log-scrub \
        "run-log secret scrubber unit tests failed" \
        "$run_log_scrub_out"
fi
run_log_scrub_summary=$(qa_gate_case_count "$run_log_scrub_out")
qa_pass_line run-log-scrub "$run_log_scrub_summary"

# The shared freeze library (Plan 00122 Task 4.2), which podfreeze and lxcfreeze both
# source: the group menu, the drill-down, the derived verb, the dry run and the act loop.
# A defect in here is a defect in BOTH tools at once, which is why it has a gate of its
# own rather than being covered incidentally by theirs. The cases neither tool's suite can
# make: every decision driven under BOTH engines' state vocabularies (a hardcoded
# `running` passes one pass and fails the other), `do_action`'s act/skip/vanished split,
# and the interactive loop re-prompting on a group that went away instead of ending the
# session.
freezelib_out=""
if ! freezelib_out="$(bash "$SCRIPT_DIR/test-freezelib.bash" 2>&1)"; then
    qa_hard_gate_failed freezelib \
        "shared freeze library unit tests failed" \
        "$freezelib_out"
fi
freezelib_summary=$(qa_gate_case_count "$freezelib_out")
qa_pass_line freezelib "$freezelib_summary"

# lxcfreeze's decisions (Plan 00122). The tool itself cannot run here — this container has
# no lxc, and a freeze tool that would report an empty machine from inside a container
# refuses to start by design. So its decisions are pure functions and this drives them
# directly, which is the same split scripts/test-ccy-rootless-guard.bash makes and for the
# same reason. The cases that matter are the refusals: `lxc-info` output that cannot be read
# must not resolve to STOPPED, and a container config that cannot be read must not report as
# having no network — both would be a confident claim about a host from a probe that failed.
lxcfreeze_out=""
if ! lxcfreeze_out="$(bash "$SCRIPT_DIR/test-lxcfreeze.bash" 2>&1)"; then
    qa_hard_gate_failed lxcfreeze \
        "lxcfreeze decision unit tests failed" \
        "$lxcfreeze_out"
fi
lxcfreeze_summary=$(qa_gate_case_count "$lxcfreeze_out")
qa_pass_line lxcfreeze "$lxcfreeze_summary"

# podfreeze's decisions (Plan 00122 Task 4.1), pinned BEFORE the shared library is
# extracted out of it — a suite written after that move would only prove the refactor
# agrees with itself. The tool has no sourcing guard, so this sources the definitions above
# its argument loop rather than the whole file, and the boundary is derived from the file's
# own content. What it guards is the distinctions a refactor loses without a symptom: a CCY
# session with no identity is not one that predates the labels, an identity label holding
# the field separator is fatal rather than mis-grouped, and an unknown network RETURNS so
# the menu can re-prompt instead of ending the session.
podfreeze_out=""
if ! podfreeze_out="$(bash "$SCRIPT_DIR/test-podfreeze.bash" 2>&1)"; then
    qa_hard_gate_failed podfreeze \
        "podfreeze decision unit tests failed" \
        "$podfreeze_out"
fi
podfreeze_summary=$(qa_gate_case_count "$podfreeze_out")
qa_pass_line podfreeze "$podfreeze_summary"

# The server login snippet (Plan 00109 Task 3.2). It is the first thing this repo puts in
# ~/.bashrc-includes that PRINTS, and bash reads ~/.bashrc for a non-interactive shell too
# when sshd started it — so a missing interactive guard breaks scp, sftp and rsync to the
# host. The guard is driven against a findings document, because with a clean one the
# snippet is silent for the wrong reason and the assertion passes with the guard deleted.
login_snippet_out=""
if ! login_snippet_out="$(bash "$SCRIPT_DIR/test-host-health-login-snippet.bash" 2>&1)"; then
    qa_hard_gate_failed host-health-login-snippet \
        "host-health login snippet unit tests failed" \
        "$login_snippet_out"
fi
login_snippet_summary=$(qa_gate_case_count "$login_snippet_out")
qa_pass_line host-health-login-snippet "$login_snippet_summary"

# Ctrl+R history search (Plan 00138). The recorder runs at every prompt of every terminal,
# so a defect files commands under the wrong directory, or writes a command the user kept
# out of history with a leading space. Driven in a real interactive bash, since history
# numbering, HISTCONTROL and PROMPT_COMMAND only behave as on a host inside one.
history_search_out=""
if ! history_search_out="$(bash "$SCRIPT_DIR/test-bash-history-search.bash" 2>&1)"; then
    qa_hard_gate_failed bash-history-search \
        "bash history search unit tests failed" \
        "$history_search_out"
fi
history_search_summary=$(qa_gate_case_count "$history_search_out")
qa_pass_line bash-history-search "$history_search_summary"

# Up-arrow is this terminal's own history, while every command still reaches the shared
# file Ctrl+R searches (Plan 00138). A shell that stopped loading the file but also stopped
# appending to it would lose history silently, so both halves are driven in real shells.
history_session_out=""
if ! history_session_out="$(bash "$SCRIPT_DIR/test-bash-history-session.bash" 2>&1)"; then
    qa_hard_gate_failed bash-history-session \
        "bash history session unit tests failed" \
        "$history_session_out"
fi
history_session_summary=$(qa_gate_case_count "$history_session_out")
qa_pass_line bash-history-session "$history_session_summary"

# The on-demand report command (Plan 00136). The login snippet's reminder and the panel's
# terminal row both hand a person to it and walk away, so it has to answer on its own: a
# sentence on a clean host, `--hold` released by Enter or EOF, and a missing checkout named
# without a traceback.
health_command_out=""
if ! health_command_out="$(bash "$SCRIPT_DIR/test-fedora-desktop-health.bash" 2>&1)"; then
    qa_hard_gate_failed fedora-desktop-health \
        "fedora-desktop-health command unit tests failed" \
        "$health_command_out"
fi
health_command_summary=$(qa_gate_case_count "$health_command_out")
qa_pass_line fedora-desktop-health "$health_command_summary"

# The fail-fast directive pattern's own unit suite (Plan 00081 F10).
#
# qa-ansible.bash enforces this repo's #1 rule with one regex, and that regex was
# asymmetric for months — `failed_when: no` earned a green tick. The fix landed
# with no test, so reverting it turned nothing red. This drives the shipped
# definitions, read out of qa-ansible.bash rather than copied.
failfast_out=""
if ! failfast_out="$(bash "$SCRIPT_DIR/test-qa-ansible-failfast.bash" 2>&1)"; then
    qa_hard_gate_failed failfast-pattern-tests \
        "fail-fast directive pattern unit tests failed" \
        "$failfast_out"
fi
failfast_summary=$(qa_gate_case_count "$failfast_out")
qa_pass_line failfast-pattern-tests "$failfast_summary"

# The reader behind THIS script's own helper-tests line (Plan 00125).
#
# Same argument as the gate above, one level closer to home: that line has been wrong four
# times, each time by becoming unable to tell a clean result from a blind one, and each
# hand-check died with the session that ran it. The suite drives the sourced function, and
# its last case runs the real runner end to end, so a format change on either side is what
# turns it red.
helper_summary_out=""
if ! helper_summary_out="$(bash "$SCRIPT_DIR/test-qa-helper-summary.bash" 2>&1)"; then
    qa_hard_gate_failed helper-counts-reader \
        "helper-tests counts reader unit tests failed" \
        "$helper_summary_out"
fi
helper_summary_tests=$(qa_gate_case_count "$helper_summary_out")
qa_pass_line helper-counts-reader "$helper_summary_tests"

# The docs gate's own exit codes, driven against fixture trees (Plan 00125).
#
# `qa-docs.bash` documents three exit codes and only one of them had ever been produced by
# running it: the other two need a tree this repository is not. The one that matters is the
# exit 2 that stops a CRASHED checker reading as a clean run — the interpreter exits 1, which
# that gate treats as its ordinary findings status, and only the payload validation turns it
# into a refusal. Two hops, neither asserted, either of which would make a traceback look
# like "no findings".
docs_exit_out=""
if ! docs_exit_out="$(bash "$SCRIPT_DIR/test-qa-docs-exit-codes.bash" 2>&1)"; then
    qa_hard_gate_failed docs-exit-codes \
        "docs gate exit-code contract failed" \
        "$docs_exit_out"
fi
docs_exit_summary=$(qa_gate_case_count "$docs_exit_out")
qa_pass_line docs-exit-codes "$docs_exit_summary"

compat_out=""
if ! compat_out="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_extension_compat 2>&1)"; then
    qa_hard_gate_failed extension-compat \
        "an extension does not declare the GNOME Shell this Fedora ships" \
        "$compat_out"
fi
# Print the pass line, for the same reason the drift gate does: a gate whose only
# visible output is a failure is indistinguishable from a gate that is not
# running — which is exactly how these two spent months documented but unrun.
compat_summary=$(qa_gate_detail "$compat_out" 'All [0-9]+ extension[(]s[)] cover the GNOME Shell that Fedora [0-9]+ ships')
qa_pass_line extension-compat "$compat_summary"

# The host status document is written by Python and read by the panel's JavaScript, so
# its file name, schema number and three state strings are each declared twice. A
# disagreement is SILENT: the panel reports `unavailable`, which by design means "nothing
# is known about this host" and is indistinguishable from a producer that never ran. So
# the panel would confidently report ignorance about a machine whose file it simply
# cannot find. Nothing at runtime can catch that, which is what makes it a gate.
panel_contract_out=""
if ! panel_contract_out="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_panel_contract . 2>&1)"; then
    qa_hard_gate_failed panel-contract \
        "the panel and the status document producer disagree" \
        "$panel_contract_out"
fi
panel_contract_summary=$(qa_gate_detail "$panel_contract_out" '[0-9]+ constant[(]s[)] agree between .+')
qa_pass_line panel-contract "$panel_contract_summary"

# The VM-test scenario manifest (Plan 00110). vars/vm-test-scenarios.yml is the
# source of the bridge's scenario allowlist, and the stdlib-only helper that
# validates it cannot read YAML — so without this gate a malformed manifest is
# first discovered by the playbook on the host. Same hard, non-merged shape as
# the gates above; the script rejects a broken control before judging the real
# file, so a validator that stopped judging fails the gate rather than passing it.
manifest_out=""
if ! manifest_out="$(bash "$SCRIPT_DIR/qa-vmtest-manifest.bash" 2>&1)"; then
    qa_hard_gate_failed vmtest-manifest \
        "the VM-test scenario manifest is not valid" \
        "$manifest_out"
fi
# THIS GATE PRINTS THREE LINES, and interpolating the capture made the stage line three
# lines long — `verdicts.py` keeps the first and drops the other two, both of which are
# coverage measurements. Three scoped reads joined into one line rather than one chosen
# line, because each is a different measurement and picking one would discard two on
# purpose where the old code discarded them by accident.
manifest_counts=$(qa_gate_detail "$manifest_out" 'vars/[a-z-]+[.]yml: scenarios=[0-9]+ runnable=[0-9]+ bridge=[0-9]+ host_only=[0-9]+ bases=[0-9]+')
manifest_checkers=$(qa_gate_detail "$manifest_out" '[0-9]+ scenario[(]s[)] agree with their guest checker')
manifest_reboot=$(qa_gate_detail "$manifest_out" '[0-9]+ scenario fixture[(]s[)] run before a declared reboot')
qa_pass_line vmtest-manifest "$manifest_counts; $manifest_checkers; $manifest_reboot"

# Plan-script run logging (Plan 00130). `exec > >(tee "$LOG") 2>&1` redirects into a
# process substitution the shell cannot wait on, so the run log loses its last chunk —
# reliably the part naming what failed. No other gate here executes a plan script, and
# lint cannot see the defect, so the pattern survived in ten scripts across seven plans.
# The gate rejects a control fixture carrying the pattern before judging the tree, and
# states how many scripts it EXAMINED: "no offences" and "the glob matched nothing"
# print identically otherwise.
plan_logging_out=""
if ! plan_logging_out="$(bash "$SCRIPT_DIR/qa-plan-script-logging.bash" 2>&1)"; then
    qa_hard_gate_failed plan-script-logging \
        "a plan script uses the un-waitable run-log pattern, or keeps a plan-local logs/ dir" \
        "$plan_logging_out"
fi
qa_pass_line plan-script-logging \
    "$(qa_gate_detail "$plan_logging_out" '[0-9]+ plan script[(]s[)] examined, no offences')"

# The upstream version-pin manifest (Plan 00109). vars/version-pins.yml says where
# every pinned version lives, and neither of its two consumers runs here — the
# review tool needs an authenticated gh, the installed-vs-pinned check needs a real
# host. So without this gate a row that had drifted away from the playbooks would
# surface only when somebody happened to run a review tool, and a row naming a
# renamed var reports the old value for ever. Same hard, non-merged shape as above.
pins_out=""
if ! pins_out="$(bash "$SCRIPT_DIR/qa-version-pins.bash" 2>&1)"; then
    qa_hard_gate_failed version-pins \
        "the upstream version-pin manifest is not valid" \
        "$pins_out"
fi
pins_summary=$(qa_gate_detail "$pins_out" 'vars/[a-z-]+[.]yml: VERSION-PINS-OK .+')
qa_pass_line version-pins "$pins_summary"

# Merge JSON from all checks.
#
# HARD GATES ARE NOT IN THIS JSON and cannot be: the merge below is positional over the seven
# temp files, and each of those gates writes its own document. A hard gate's verdict lives in
# its stage line and in HARD_FAILED, which is why the final summary reads both rather than
# taking `.summary.failed` as the whole story.
STATUS="pass"
if [[ $FAILED -gt 0 || ${#HARD_FAILED[@]} -gt 0 ]]; then
    STATUS="fail"
fi

jq -s \
    --arg status "$STATUS" \
    '{
        "status": $status,
        "summary": {
            "total":  ([.[].summary.total]  | add // 0),
            "passed": ([.[].summary.passed] | add // 0),
            "failed": ([.[].summary.failed] | add // 0)
        },
        "failures": [.[].failures[]],
        "checks": {
            "bash":            .[0],
            "python":          .[1],
            "patterns":        .[2],
            "ansible":         .[3],
            "ansible_syntax":  .[4],
            "js":              .[5],
            "docs":            .[6]
        }
    }' "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" "$TMP_DOCS" > "$JSON_OUT"

# Final terse summary.
#
# It names the failing HARD GATES rather than only counting file errors, because those two
# numbers answer different questions and the old line answered only one of them. `.summary`
# covers the seven merged stages; a hard gate contributes no files, so a run whose only
# failures were hard gates used to abort long before this line and never reach it at all.
TOTAL=$(jq '.summary.total' "$JSON_OUT")
if [[ $FAILED -eq 0 && ${#HARD_FAILED[@]} -eq 0 ]]; then
    echo "✓ QA passed: $TOTAL files checked"
    exit 0
fi

NERRORS=$(jq '.summary.failed' "$JSON_OUT")
echo "✗ QA FAILED: $NERRORS errors in $TOTAL files"
if [[ ${#HARD_FAILED[@]} -gt 0 ]]; then
    # Every gate ran, so this list is the whole of what is broken — not the first thing that
    # happened to break. That is the difference this task exists for.
    echo "  ${#HARD_FAILED[@]} gate(s) failed: ${HARD_FAILED[*]}"
fi
echo "  Details: jq '.failures[]' $JSON_OUT"
exit 1
