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
JSON_OUT="/tmp/qa-results.json"
TMP_BASH=$(mktemp)
TMP_PYTHON=$(mktemp)
TMP_PATTERNS=$(mktemp)
TMP_ANSIBLE=$(mktemp)
TMP_ANSIBLE_SYNTAX=$(mktemp)
TMP_JS=$(mktemp)
TMP_DOCS=$(mktemp)
trap 'rm -f "$TMP_BASH" "$TMP_PYTHON" "$TMP_PATTERNS" "$TMP_ANSIBLE" "$TMP_ANSIBLE_SYNTAX" "$TMP_JS" "$TMP_DOCS"' EXIT
FAILED=0

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
    echo "$nokill_out" >&2
    echo "✗ QA FAILED: no-kill safety gate (container-watch) rejected a process-termination call site" >&2
    exit 1
fi
# Pass line, for the same reason the drift gate below prints one: a gate whose
# only visible output is a failure is indistinguishable from a gate that is not
# running. That rule was written down beside the drift gate and never applied to
# this one, six lines above it.
nokill_summary=$(printf '%s' "$nokill_out" | grep -oE '[0-9]+ call site[s]? checked') ||
    nokill_summary="no forbidden kill call sites"
printf '✓ nokill-containerwatch: %s\n' "$nokill_summary"

# Deployed-drift gate (Plan 00099): a repo-owned user script that was changed
# but never deployed means the host is running different code from the one QA
# just passed. Like the no-kill gate above, deliberately NOT a jq-merged stage
# — it inspects the HOST, not the source tree, and self-skips where there is no
# host to inspect (CCY container, clean CI checkout).
drift_out=""
if ! drift_out="$(bash "$SCRIPT_DIR/qa-deployed-drift.bash" 2>&1)"; then
    echo "$drift_out" >&2
    echo "✗ QA FAILED: repo-owned scripts differ from their deployed copies" >&2
    exit 1
fi
# Print the pass line too. A gate whose only visible output is a failure is
# indistinguishable from a gate that is not running — and "a check that silently
# does nothing" is precisely the defect Plan 00099 exists to fix.
echo "$drift_out"

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
helper_out=""
if ! helper_out="$(bash "$SCRIPT_DIR/qa-helper-tests.bash" 2>&1)"; then
    echo "$helper_out" >&2
    echo "✗ QA FAILED: helper unit tests" >&2
    exit 1
fi
helper_summary=$(printf '%s' "$helper_out" | grep -oE 'Ran [0-9]+ tests?') || helper_summary="passed"
printf '✓ helper-tests: %s\n' "$helper_summary"

# The pre-commit secret scanner's own unit suite (scripts/test-secret-scan.bash).
#
# The scanner LIBRARY runs on every commit, but a false-NEGATIVE regression in it
# is silent by construction — a leak it stopped catching produces no signal at
# all. That is the whole reason the suite exists, so "it is exercised on every
# commit anyway" is not a reason to leave it unwired. Every case is synthetic and
# the whole suite is sub-second. Same shape as the helper-tests gate above.
scan_out=""
if ! scan_out="$(bash "$SCRIPT_DIR/test-secret-scan.bash" 2>&1)"; then
    echo "$scan_out" >&2
    echo "✗ QA FAILED: secret scanner unit tests" >&2
    exit 1
fi
scan_summary=$(printf '%s' "$scan_out" | grep -oE 'passed: [0-9]+') || scan_summary="passed"
printf '✓ secret-scan-tests: %s\n' "$scan_summary"

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
    echo "$planlib_out" >&2
    echo "✗ QA FAILED: plan-script library regression tests" >&2
    exit 1
fi
planlib_summary=$(printf '%s' "$planlib_out" | grep -oE 'PASSED \(library version [0-9.]+\)') ||
    planlib_summary="passed"
printf '✓ planlib-tests: %s\n' "$planlib_summary"

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
    echo "$rootless_out" >&2
    echo "✗ QA FAILED: ccy rootless-engine guard unit tests" >&2
    exit 1
fi
rootless_summary=$(printf '%s' "$rootless_out" | grep -oE 'passed: [0-9]+') ||
    rootless_summary="passed"
printf '✓ ccy-rootless-guard: %s\n' "$rootless_summary"

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
    echo "$token_mode_out" >&2
    echo "✗ QA FAILED: ccy token-mode unit tests" >&2
    exit 1
fi
token_mode_summary=$(printf '%s' "$token_mode_out" | grep -oE 'passed: [0-9]+') ||
    token_mode_summary="passed"
printf '✓ ccy-token-mode: %s\n' "$token_mode_summary"

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
    echo "$ssh_handling_out" >&2
    echo "✗ QA FAILED: ccy ssh-handling unit tests" >&2
    exit 1
fi
ssh_handling_summary=$(printf '%s' "$ssh_handling_out" | grep -oE 'passed: [0-9]+') ||
    ssh_handling_summary="passed"
printf '✓ ccy-ssh-handling: %s\n' "$ssh_handling_summary"

# ccy's SELinux relabel decision (Plan 00118, CCY 3.55.0).
#
# On an Enforcing host container_t may not read user_home_t, so a ccy container
# could not read the project it was handed; desktops never showed it because they
# are not enforcing. The decision is a pure function of getenforce's text and the
# engine's own report, driven here across every pair a real host could produce.
selinux_verdict_out=""
if ! selinux_verdict_out="$(bash "$SCRIPT_DIR/test-ccy-selinux-verdict.bash" 2>&1)"; then
    echo "$selinux_verdict_out" >&2
    echo "✗ QA FAILED: ccy selinux-verdict unit tests" >&2
    exit 1
fi
selinux_verdict_summary=$(printf '%s' "$selinux_verdict_out" | grep -oE 'passed: [0-9]+') ||
    selinux_verdict_summary="passed"
printf '✓ ccy-selinux-verdict: %s\n' "$selinux_verdict_summary"

# gpu_device_flags (Plan 00120): the GPU device is handed to the container only where the host
# has /dev/dri; a headless server used to abort the run. Driven with a present directory, an
# absent path and a plain file, plus a check that the launcher consumes the array.
gpu_device_out=""
if ! gpu_device_out="$(bash "$SCRIPT_DIR/test-ccy-gpu-device.bash" 2>&1)"; then
    echo "$gpu_device_out" >&2
    echo "✗ QA FAILED: ccy gpu-device unit tests" >&2
    exit 1
fi
gpu_device_summary=$(printf '%s' "$gpu_device_out" | grep -oE 'passed: [0-9]+') ||
    gpu_device_summary="passed"
printf '✓ ccy-gpu-device: %s\n' "$gpu_device_summary"

# ccy_host_hostname (Plan 00121): CCY_HOST_HOSTNAME tells the container which MACHINE it is
# on, since its own HOSTNAME is the container id. The value reaches a `podman run -e`
# argument and is then read by shells in the container, so the grammar is the guard — driven
# through plain names, FQDNs, and the refusals including shell metacharacters and an empty
# nodename.
host_hostname_out=""
if ! host_hostname_out="$(bash "$SCRIPT_DIR/test-ccy-host-hostname.bash" 2>&1)"; then
    echo "$host_hostname_out" >&2
    echo "✗ QA FAILED: ccy host-hostname unit tests" >&2
    exit 1
fi
host_hostname_summary=$(printf '%s' "$host_hostname_out" | grep -oE 'passed: [0-9]+') ||
    host_hostname_summary="passed"
printf '✓ ccy-host-hostname: %s\n' "$host_hostname_summary"

# host_only_preflight (Plan 00121): the host-CLI gate on a scenario that puts a real GitHub
# PAT into a guest. One of three independent gates — the other two are the bridge allowlist
# and bridge_run's manifest refusal — and the one a human types past. Driven through the
# bridge marker, both deployed enumerations, and every way a secret file can be wrong.
host_only_gate_out=""
if ! host_only_gate_out="$(bash "$SCRIPT_DIR/test-vmtest-host-only-gate.bash" 2>&1)"; then
    echo "$host_only_gate_out" >&2
    echo "✗ QA FAILED: vmtest host-only gate unit tests" >&2
    exit 1
fi
host_only_gate_summary=$(printf '%s' "$host_only_gate_out" | grep -oE 'passed: [0-9]+') ||
    host_only_gate_summary="passed"
printf '✓ vmtest-host-only-gate: %s\n' "$host_only_gate_summary"

# reboot_guest / guest_prepare (Plan 00109): whether a run is judged before or after a
# fresh boot. A profile with no reboot mechanics, or a fixture that failed, would leave
# the checker judging the boot that provisioned the guest — a green transcript for a
# scenario that never happened, with no symptom anywhere else.
reboot_dispatch_out=""
if ! reboot_dispatch_out="$(bash "$SCRIPT_DIR/test-vmtest-reboot-dispatch.bash" 2>&1)"; then
    echo "$reboot_dispatch_out" >&2
    echo "✗ QA FAILED: vmtest reboot dispatch unit tests" >&2
    exit 1
fi
reboot_dispatch_summary=$(printf '%s' "$reboot_dispatch_out" | grep -oE 'passed: [0-9]+') ||
    reboot_dispatch_summary="passed"
printf '✓ vmtest-reboot-dispatch: %s\n' "$reboot_dispatch_summary"

# The kernel selection inside that fixture (Plan 00109). The only step of the route no
# machine here can reach: this container has no dnf, rpm or grubby, and the only other
# executor is a guest twenty minutes into a provisioning run. Every check in the scenario
# stands on it — a guest that reboots into the kernel it already ran makes the claim under
# test vacuously false, and the fourteen checks after it judge nothing.
kernel_selection_out=""
if ! kernel_selection_out="$(bash "$SCRIPT_DIR/test-vmtest-kernel-selection.bash" 2>&1)"; then
    echo "$kernel_selection_out" >&2
    echo "✗ QA FAILED: vmtest kernel selection unit tests" >&2
    exit 1
fi
kernel_selection_summary=$(printf '%s' "$kernel_selection_out" | grep -oE 'passed: [0-9]+') ||
    kernel_selection_summary="passed"
printf '✓ vmtest-kernel-selection: %s\n' "$kernel_selection_summary"

# The fixture→checker record contract (Plan 00109). One scenario's fixture writes a file
# the checker sources, and that seam is invisible to every other gate: a value carrying a
# shell metacharacter aborts the source and unsets every key after it, while the file
# still exists and the source still "happened".
prepare_record_out=""
if ! prepare_record_out="$(bash "$SCRIPT_DIR/test-vmtest-prepare-record.bash" 2>&1)"; then
    echo "$prepare_record_out" >&2
    echo "✗ QA FAILED: vmtest prepare-record contract tests" >&2
    exit 1
fi
prepare_record_summary=$(printf '%s' "$prepare_record_out" | grep -oE 'passed: [0-9]+') ||
    prepare_record_summary="passed"
printf '✓ vmtest-prepare-record: %s\n' "$prepare_record_summary"

# The panel's decisions (Plan 00109): the shipped statusDocument.js and sections/health.js
# driven against boot-stale, malformed and state-disagreeing documents. The contract gate
# beside this one proves the two languages share a vocabulary; it cannot tell a demoted
# finding from a current one, and on the primary surface for these findings that is the
# whole question.
panel_sections_out=""
if ! panel_sections_out="$(bash "$SCRIPT_DIR/test-panel-sections.bash" 2>&1)"; then
    echo "$panel_sections_out" >&2
    echo "✗ QA FAILED: panel section unit tests" >&2
    exit 1
fi
panel_sections_summary=$(printf '%s' "$panel_sections_out" | grep -oE 'passed: [0-9]+') ||
    panel_sections_summary="passed"
printf '✓ panel-sections: %s\n' "$panel_sections_summary"

# hl_write_localhost_yml (Plan 00119): the headless localhost.yml writer, driven through the
# 443 flag on/off/unset, the empty-identity path and the keep-existing-file promise.
localhost_yml_out=""
if ! localhost_yml_out="$(bash "$SCRIPT_DIR/test-run-bash-headless-localhost-yml.bash" 2>&1)"; then
    echo "$localhost_yml_out" >&2
    echo "✗ QA FAILED: run.bash headless localhost.yml unit tests" >&2
    exit 1
fi
localhost_yml_summary=$(printf '%s' "$localhost_yml_out" | grep -oE 'passed: [0-9]+') ||
    localhost_yml_summary="passed"
printf '✓ run-bash-headless-localhost-yml: %s\n' "$localhost_yml_summary"

# hl_ssh_agent_stop (Plan 00063 Task 3.4): the headless ssh-agent teardown, driven through a
# clean kill, an already-gone agent and — the case that matters — an agent that SURVIVES the
# kill. `ssh-agent -k` returns non-zero for both of the last two, and reporting the survivor
# as the harmless one left an unlocked key reachable through $SSH_AUTH_SOCK for the rest of
# the run while exiting 0.
ssh_agent_out=""
if ! ssh_agent_out="$(bash "$SCRIPT_DIR/test-run-bash-ssh-agent-teardown.bash" 2>&1)"; then
    echo "$ssh_agent_out" >&2
    echo "✗ QA FAILED: run.bash ssh-agent teardown unit tests" >&2
    exit 1
fi
ssh_agent_summary=$(printf '%s' "$ssh_agent_out" | grep -oE 'passed: [0-9]+') ||
    ssh_agent_summary="passed"
printf '✓ run-bash-ssh-agent-teardown: %s\n' "$ssh_agent_summary"

# The run-log secret scrubber (Plan 00121). Redaction is the easy half; what this gate exists
# for is `scrub_verify` REFUSING an artefact where redaction missed a secret. A scrubber is
# fail-open by nature — it writes a file it believes is clean and a miss is silent — so the
# assertion that matters is driven by a fixture where the redactor was deliberately not told
# about one of the secrets.
run_log_scrub_out=""
if ! run_log_scrub_out="$(bash "$SCRIPT_DIR/test-run-log-scrub.bash" 2>&1)"; then
    echo "$run_log_scrub_out" >&2
    echo "✗ QA FAILED: run-log secret scrubber unit tests" >&2
    exit 1
fi
run_log_scrub_summary=$(printf '%s' "$run_log_scrub_out" | grep -oE 'passed: [0-9]+') ||
    run_log_scrub_summary="passed"
printf '✓ run-log-scrub: %s\n' "$run_log_scrub_summary"

# lxcfreeze's decisions (Plan 00122). The tool itself cannot run here — this container has
# no lxc, and a freeze tool that would report an empty machine from inside a container
# refuses to start by design. So its decisions are pure functions and this drives them
# directly, which is the same split scripts/test-ccy-rootless-guard.bash makes and for the
# same reason. The cases that matter are the refusals: `lxc-info` output that cannot be read
# must not resolve to STOPPED, and a container config that cannot be read must not report as
# having no network — both would be a confident claim about a host from a probe that failed.
lxcfreeze_out=""
if ! lxcfreeze_out="$(bash "$SCRIPT_DIR/test-lxcfreeze.bash" 2>&1)"; then
    echo "$lxcfreeze_out" >&2
    echo "✗ QA FAILED: lxcfreeze decision unit tests" >&2
    exit 1
fi
lxcfreeze_summary=$(printf '%s' "$lxcfreeze_out" | grep -oE 'passed: [0-9]+') ||
    lxcfreeze_summary="passed"
printf '✓ lxcfreeze: %s\n' "$lxcfreeze_summary"

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
    echo "$podfreeze_out" >&2
    echo "✗ QA FAILED: podfreeze decision unit tests" >&2
    exit 1
fi
podfreeze_summary=$(printf '%s' "$podfreeze_out" | grep -oE 'passed: [0-9]+') ||
    podfreeze_summary="passed"
printf '✓ podfreeze: %s\n' "$podfreeze_summary"

# The server login snippet (Plan 00109 Task 3.2). It is the first thing this repo puts in
# ~/.bashrc-includes that PRINTS, and bash reads ~/.bashrc for a non-interactive shell too
# when sshd started it — so a missing interactive guard breaks scp, sftp and rsync to the
# host. The guard is driven against a findings document, because with a clean one the
# snippet is silent for the wrong reason and the assertion passes with the guard deleted.
login_snippet_out=""
if ! login_snippet_out="$(bash "$SCRIPT_DIR/test-host-health-login-snippet.bash" 2>&1)"; then
    echo "$login_snippet_out" >&2
    echo "✗ QA FAILED: host-health login snippet unit tests" >&2
    exit 1
fi
login_snippet_summary=$(printf '%s' "$login_snippet_out" | grep -oE 'passed: [0-9]+') ||
    login_snippet_summary="passed"
printf '✓ host-health-login-snippet: %s\n' "$login_snippet_summary"

# The fail-fast directive pattern's own unit suite (Plan 00081 F10).
#
# qa-ansible.bash enforces this repo's #1 rule with one regex, and that regex was
# asymmetric for months — `failed_when: no` earned a green tick. The fix landed
# with no test, so reverting it turned nothing red. This drives the shipped
# definitions, read out of qa-ansible.bash rather than copied.
failfast_out=""
if ! failfast_out="$(bash "$SCRIPT_DIR/test-qa-ansible-failfast.bash" 2>&1)"; then
    echo "$failfast_out" >&2
    echo "✗ QA FAILED: fail-fast directive pattern unit tests" >&2
    exit 1
fi
failfast_summary=$(printf '%s' "$failfast_out" | grep -oE 'passed: [0-9]+') ||
    failfast_summary="passed"
printf '✓ failfast-pattern-tests: %s\n' "$failfast_summary"

compat_out=""
if ! compat_out="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_extension_compat 2>&1)"; then
    echo "$compat_out" >&2
    echo "✗ QA FAILED: an extension does not declare the GNOME Shell this Fedora ships" >&2
    exit 1
fi
# Print the pass line, for the same reason the drift gate does: a gate whose only
# visible output is a failure is indistinguishable from a gate that is not
# running — which is exactly how these two spent months documented but unrun.
compat_summary=$(printf '%s' "$compat_out" | grep -E '^All [0-9]+ extension') || compat_summary="OK"
printf '✓ extension-compat: %s\n' "$compat_summary"

# The host status document is written by Python and read by the panel's JavaScript, so
# its file name, schema number and three state strings are each declared twice. A
# disagreement is SILENT: the panel reports `unavailable`, which by design means "nothing
# is known about this host" and is indistinguishable from a producer that never ran. So
# the panel would confidently report ignorance about a machine whose file it simply
# cannot find. Nothing at runtime can catch that, which is what makes it a gate.
panel_contract_out=""
if ! panel_contract_out="$(cd "$SCRIPT_DIR/.." && python3 -m helpers.gnome.check_panel_contract . 2>&1)"; then
    echo "$panel_contract_out" >&2
    echo "✗ QA FAILED: the panel and the status document producer disagree" >&2
    exit 1
fi
panel_contract_summary=$(printf '%s' "$panel_contract_out" | grep -E '^PANEL-CONTRACT-OK') ||
    panel_contract_summary="OK"
printf '✓ panel-contract: %s\n' "${panel_contract_summary#PANEL-CONTRACT-OK }"

# The VM-test scenario manifest (Plan 00110). vars/vm-test-scenarios.yml is the
# source of the bridge's scenario allowlist, and the stdlib-only helper that
# validates it cannot read YAML — so without this gate a malformed manifest is
# first discovered by the playbook on the host. Same hard, non-merged shape as
# the gates above; the script rejects a broken control before judging the real
# file, so a validator that stopped judging fails the gate rather than passing it.
manifest_out=""
if ! manifest_out="$(bash "$SCRIPT_DIR/qa-vmtest-manifest.bash" 2>&1)"; then
    echo "$manifest_out" >&2
    echo "✗ QA FAILED: the VM-test scenario manifest is not valid" >&2
    exit 1
fi
printf '✓ vmtest-manifest: %s\n' "$manifest_out"

# The upstream version-pin manifest (Plan 00109). vars/version-pins.yml says where
# every pinned version lives, and neither of its two consumers runs here — the
# review tool needs an authenticated gh, the installed-vs-pinned check needs a real
# host. So without this gate a row that had drifted away from the playbooks would
# surface only when somebody happened to run a review tool, and a row naming a
# renamed var reports the old value for ever. Same hard, non-merged shape as above.
pins_out=""
if ! pins_out="$(bash "$SCRIPT_DIR/qa-version-pins.bash" 2>&1)"; then
    echo "$pins_out" >&2
    echo "✗ QA FAILED: the upstream version-pin manifest is not valid" >&2
    exit 1
fi
printf '✓ version-pins: %s\n' "$pins_out"

# Merge JSON from all checks
STATUS="pass"
[[ $FAILED -gt 0 ]] && STATUS="fail"

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

# Final terse summary
TOTAL=$(jq '.summary.total' "$JSON_OUT")
if [[ $FAILED -eq 0 ]]; then
    echo "✓ QA passed: $TOTAL files checked"
    exit 0
else
    NERRORS=$(jq '.summary.failed' "$JSON_OUT")
    echo "✗ QA FAILED: $NERRORS errors in $TOTAL files"
    echo "  Details: jq '.failures[]' $JSON_OUT"
    exit 1
fi
