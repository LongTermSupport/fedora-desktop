#!/usr/bin/env bash
# Plan 00109 — acceptance.bash
#
# The VERDICT. `triage.bash` gathers facts and renders none; this decides whether the
# plan's HOST-blocked claims are established (CLAUDE/PlanScriptStandards.md R9).
#
# HOST ONLY (R2). Every fact below is about this machine's systemd user manager, its
# GNOME session, its XDG state directory, its DKMS tree and its git remote. A CCY
# container has a different answer for all of them and would not fail — it would answer
# confidently about the wrong computer, which is the failure class R2 exists for.
#
# IT JUDGES THE PLAN, NOT THE HOST'S CLEANLINESS. That distinction is load-bearing and
# easy to get backwards. A host with a genuinely drifted pin, a stale play or a failed
# unit is a host where this plan is WORKING: the checks ran, compared, and said so. So a
# real drift finding is REPORTED here and does not reject. What rejects is a check that
# could not run, compared nothing, or gave no answer — the silence this plan exists to
# abolish. A gate that rejected on host findings could never accept, and the plan could
# never close.
#
# COVERAGE IS STATED, NOT INFERRED. The verdict prints `COVERAGE: n of m checks executed`
# and REJECTS an incomplete run even with zero failures, because a pass count cannot carry
# it: several sections emit one pass or several fails, so "17 passed" reads identically
# whether 17 of 19 ran or 19 of 19. Coverage implied by a count is this repo's named
# recurring defect.
#
# NOT READ-ONLY IN ONE RESPECT, and it is worth stating rather than glossing. Check [9]
# runs the login report for real, and the freshness check inside it does a `git fetch`
# (refs only — never the working tree) and stamps the fetch clock in the ledger
# directory. `--no-handoff` keeps it from overwriting the handoff file and the status
# document that the last real login left, which are the artefacts checks [1]–[5] judge.
# Check [8] runs `ansible-playbook --list-tasks`, which applies nothing.
#
# Usage: ./acceptance.bash [-h|--help]
# Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH.
set -euo pipefail

# ── R1 bootstrap: script-relative, filesystem-only, bounded at the repo boundary ──────────
scriptDir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
repoRoot="${scriptDir}"
while [[ "${repoRoot}" != "/" ]] && [[ ! -e "${repoRoot}/ansible.cfg" ]]; do
    if [[ -e "${repoRoot}/.git" ]]; then
        printf '[FATAL] no ansible.cfg between %s and the repo root %s\n' "${scriptDir}" "${repoRoot}" >&2
        exit 1
    fi
    repoRoot="$(dirname "${repoRoot}")"
done
[[ -e "${repoRoot}/ansible.cfg" ]] || {
    printf '[FATAL] no ansible.cfg above %s\n' "${scriptDir}" >&2
    exit 1
}
# shellcheck source-path=SCRIPTDIR
# shellcheck source=../_planlib.inc.bash
source "${repoRoot}/CLAUDE/Plan/_planlib.inc.bash"
plan_init "${BASH_SOURCE[0]}"

PLAN_USAGE="usage: acceptance.bash [-h|--help]

Plan 00109 acceptance gate. Run ./deploy.bash first, then LOG OUT AND BACK IN —
on Wayland that is the only way the shell loads the panel, and it is also what
fires the login-time health unit that checks [1]-[5] and [12] read.

Checks, against the DEPLOYED artefacts and this machine's live state:

   0. precondition: a desktop profile, and the helpers importable from this checkout
   1. the status document exists where the producer says it lives
   2. the document is structurally sound, not merely parseable
   3. it names all four checks the panel's health section renders
   4. installed-vs-pinned compared something — the axis the 2026-09-11 incident broke
   5. play-freshness gave an answer rather than declining to
   6. the ledger is present, complete and trusted
   7. the ledger holds a run record for each play deploy.bash runs
   8. a non-applying ansible run adds no ledger row
   9. the login report honours its exit contract: 0 and silent, or 3 with findings
  10. notify-send is installed — the desktop delivery's only channel
  11. host-health.service is enabled AND wanted by graphical-session.target
  12. it has actually run at a login, and did not fail
  13. the panel extension is deployed complete and declared enabled
  14. the deployed panel agrees with the producer about where the document lives
  15. the running shell has loaded the panel extension
  16. the vm-test-lab scenario reached the deployed allowlist with its guest scripts
  17. this checkout has a remote a timer can fetch with no agent present
  18. the DisplayLink background-recovery action is deployed and armed

Host findings — a real drifted pin, a stale play, a failed unit — are REPORTED,
not rejected: a host with findings is this plan working. What rejects is a check
that could not run, compared nothing, or gave no answer.

Anything a script cannot honestly establish is listed at the end FOR THE HUMAN
and is never counted as a passed check.

Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi
if [[ "${PLAN_CHECK}" == "1" ]]; then
    printf '[FATAL] --check has no meaning for an acceptance gate: it changes nothing\n' >&2
    printf '        already, so a dry run of it is the same run with the verdict removed.\n' >&2
    exit 64
fi

plan_require_host "every fact below is this machine's systemd user manager, GNOME session, XDG state directory, DKMS tree and git remote"
plan_start_log auto

# ── the vocabulary this gate judges against ──────────────────────────────────────────────
#
# Repo artefact names, not install identifiers: no host, user or checkout path appears in
# this file. Every host-specific path below is resolved at runtime from $HOME or from the
# production helpers themselves, which is also what stops this gate and the producer
# disagreeing about where anything lives.
EXT_UUID="fedora-desktop@fedora-desktop"
EXT_FILES=(extension.js metadata.json statusDocument.js stylesheet.css sections/health.js)
HEALTH_UNIT="host-health.service"
HEALTH_TARGET="graphical-session.target"
VMTEST_SCENARIO="server-host-health-kernel-change"
VMTEST_SCRIPTS=(
    guest-prepare-server-host-health-kernel-change.bash
    guest-acceptance-server-host-health-kernel-change.bash
)
DEPLOY_PLAYS=(
    playbooks/imports/optional/common/play-host-health-login-report.yml
    playbooks/imports/optional/common/play-fedora-desktop-panel.yml
    playbooks/imports/optional/common/play-vm-test-lab.yml
    playbooks/imports/optional/hardware-specific/play-displaylink.yml
)
# The four section ids the panel's health section registers. Spelled here because the
# claim under test is that the DOCUMENT carries all four; reading them from the producer
# would make the check agree with itself whatever the producer had dropped.
DOC_SECTIONS=(post-boot-health play-ledger play-freshness installed-vs-pinned)
RECOVERY_DIR="/usr/local/lib/displaylink-recovery/helpers/displaylink_recovery"
RECOVERY_RULE="/etc/udev/rules.d/99-displaylink-dock-recovery.rules"
RECOVERY_DOCK_UNIT="/etc/systemd/system/displaylink-dock-recovery.service"
RECOVERY_SUSPEND_UNIT="displaylink-suspend.service"

# The checks this gate is expected to run. COVERAGE is stated against this list.
EXPECTED_CHECKS=(0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18)
RAN_CHECKS=()

PASS=0
FAIL=0
NOTES=()
HUMAN=()

STATUS_PROBE="${PLAN_RUN_DIR}/status-document-report.txt"
LEDGER_PROBE="${PLAN_RUN_DIR}/ledger-report.txt"
LOGIN_ERR="${PLAN_RUN_DIR}/login-report.stderr.txt"
VERDICT_REPORT="${PLAN_RUN_DIR}/acceptance-report.txt"

# Announce a check AND record that it ran. Every numbered section starts here; a section
# that prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    printf '[%s] %s\n' "$1" "$2"
}
ok() {
    printf '  PASS  %s\n' "$1"
    PASS=$((PASS + 1))
}
bad() {
    printf '  FAIL  %s\n' "$1" >&2
    if [[ "$#" -gt 1 ]]; then
        printf '        %s\n' "$2" >&2
    fi
    FAIL=$((FAIL + 1))
}
# A fact about the host that is not a verdict on the plan — a real drift finding, a
# CLEARED ledger. Recorded and printed, never counted as a pass or a fail.
note() {
    NOTES+=("$1")
    printf '  NOTE  %s\n' "$1"
}
# Something this script cannot honestly establish. Listed for the human at the end and
# never counted as a check, which is the whole point of naming it separately.
human() {
    HUMAN+=("$1")
}
# The gate cannot render a verdict at all. Distinct from REJECTED, and the distinction
# matters: rejected means the plan is not done, this means nobody has been told either way.
abort() {
    printf '\n' >&2
    printf '  ABORT  %s\n' "$1" >&2
    if [[ "$#" -gt 1 ]]; then
        printf '         %s\n' "$2" >&2
    fi
    printf '  COULD NOT ESTABLISH — no verdict rendered.\n' >&2
    exit 2
}

# One `KEY value` line from a probe report, into the named variable. The probe prints every
# key unconditionally, so a missing one means the probe itself is broken — a
# could-not-establish, not a failed check.
#
# IT ASSIGNS RATHER THAN PRINTING, and that is the whole reason it is shaped this way:
# `x="$(f)"` runs f in a SUBSHELL, where `abort`'s `exit 2` would end only that subshell
# and the run would carry on with a half-rendered verdict — the "control that silently
# becomes a no-op" failure CLAUDE/PlanScriptStandards.md exists to prevent. Assigning keeps
# the abort in the caller's shell, where it means what it says.
read_field() {
    local target="$1" file="$2" key="$3" line=""
    if ! line="$(grep -m1 -e "^${key} " "${file}")"; then
        abort "the probe report ${file} has no '${key}' line" \
            "this gate cannot read its own evidence, so it will not guess at a verdict"
    fi
    printf -v "${target}" '%s' "${line#"${key} "}"
}

# Whitespace-separated membership, without a subshell or a grep per call.
contains_word() {
    case " $1 " in
        *" $2 "*) return 0 ;;
        *) return 1 ;;
    esac
}

RUNNING_KERNEL="$(uname -r)"

printf '==============================================================\n'
printf 'Plan 00109 acceptance — desktop drift detection and the panel\n'
printf 'running kernel: %s\n' "${RUNNING_KERNEL}"
printf '==============================================================\n\n'

# --- 0. precondition: the gate can see what it judges ------------------------------------
#
# Without this, every later check answers about the wrong thing rather than failing: an
# unimportable helpers package makes the two probes empty, and a server profile has no
# panel, no graphical-session.target and no session bus — so checks [11]-[15] would all
# fail for a reason that is not a defect in this plan. Refuse a verdict instead of issuing
# a false REJECTED.
check 0 "precondition: a desktop profile, and the helpers importable from this checkout"

if ! command -v python3 > /dev/null; then
    abort "python3 is not on PATH" \
        "the report's own units run /usr/bin/python3, so this host cannot run them either"
fi
if ! command -v systemctl > /dev/null; then
    abort "systemctl is not on PATH" "every unit fact below is unobtainable"
fi

DEFAULT_TARGET=""
if ! DEFAULT_TARGET="$(systemctl get-default)"; then
    abort "systemctl get-default failed" \
        "the provisioning profile is derived from it, so this gate cannot tell which delivery to judge"
fi
if [[ "${DEFAULT_TARGET}" != "graphical.target" ]]; then
    abort "the default target is ${DEFAULT_TARGET}, so this host provisions as 'server'" \
        "this gate judges the DESKTOP delivery. The server route is judged by the VM scenario ${VMTEST_SCENARIO} — see the FOR THE HUMAN list in PLAN.md Task 3.2."
fi
ok "default target is graphical.target — the desktop delivery is what is under test"

# One probe, four checks. Run through the PRODUCTION reader (`status_document.read`) and
# the PRODUCTION path (`status_document.path` over `ledger.state_dir`) rather than a
# hand-rolled JSON parse against a hand-rolled path: a gate with its own copy of either can
# pass while the thing it vouches for is broken, which is the defect class this plan is about.
if ! (cd "${PLAN_REPO_ROOT}" && python3 -c '
import os
import sys

sys.path.insert(0, os.getcwd())
from helpers.host_health import status_document
from helpers.play_ledger import ledger

base = ledger.state_dir(os.environ, os.path.expanduser("~"))
target = status_document.path(base)
print("FILENAME " + status_document.FILE_NAME)
print("STATEDIR " + ledger.STATE_DIR_NAME)
print("PATH " + target)
print("EXISTS " + ("yes" if os.path.exists(target) else "no"))

document = status_document.read(target)
sections = document.get("sections", {})
print("SECTIONS " + " ".join(sorted(sections)))
print("SELF " + ("yes" if status_document.SELF_SECTION in sections else "no"))
print("KERNEL " + str(status_document.collected_kernel(document)))
print("GENERATED " + str(document.get("generated_at", "")))
print("HANDOFF " + str(document.get("handoff", "")))
for name in sorted(sections):
    entry = sections[name]
    if not isinstance(entry, dict):
        print("STATE " + name + " unreadable")
        continue
    print("STATE " + name + " " + str(entry.get("state")))
    # The key name comes from the producer, not a literal spelled here: a gate with its
    # own copy of an interface string keeps passing after the producer renames it.
    stated = entry.get(status_document.COVERAGE_KEY)
    if stated:
        print("COVERAGE " + name + " " + " ".join(str(stated).split()))
    reported = list(entry.get("findings") or []) + list(entry.get("unchecked") or [])
    for text in reported:
        print("TEXT " + name + " " + " ".join(str(text).split()))
for reason in status_document.unreadable_reasons(document):
    print("UNREADABLE " + " ".join(reason.split()))
' > "${STATUS_PROBE}"); then
    abort "the status-document probe could not run from ${PLAN_REPO_ROOT}" \
        "see ${STATUS_PROBE}; the helpers package must import from the repo root, which is what the units' WorkingDirectory gives them"
fi
ok "the helpers package imports and the status-document probe ran"

# The ledger probe, same principle: `store.read_lines` and `ledger.fold_latest` are the
# production reader, so "the ledger parses" means it parses for the checks that read it.
if ! (cd "${PLAN_REPO_ROOT}" && python3 -c '
import json
import os
import sys

sys.path.insert(0, os.getcwd())
from helpers.play_ledger import fetch_clock, ledger, store

base = ledger.ledger_dir(os.environ, os.path.expanduser("~"))
runs = ledger.runs_path(base)
print("BASE " + base)
print("RUNS " + runs)
print("EXISTS " + ("yes" if os.path.exists(runs) else "no"))

reason = store.broken_reason(base)
print("BROKEN " + ("yes" if reason else "no"))
print("BROKENWHY " + (" ".join(reason.split()) if reason else "-"))
print("CLEARED " + ("yes" if os.path.exists(ledger.cleared_path(base)) else "no"))
print("FETCHCLOCK " + (fetch_clock.last_success(base) or "-"))

lines = store.read_lines(base)
print("LINES " + str(len(lines)))

genesis = 0
counts = {}
malformed = ""
for number, line in enumerate(lines, start=1):
    text = line.strip()
    if not text:
        continue
    try:
        record = json.loads(text)
    except ValueError as error:
        malformed = "line " + str(number) + ": " + str(error)
        break
    if record.get("kind") == "genesis":
        genesis += 1
        continue
    play = record.get("play")
    if isinstance(play, str) and play:
        counts[play] = counts.get(play, 0) + 1
print("GENESIS " + str(genesis))
print("MALFORMED " + (" ".join(malformed.split()) if malformed else "-"))
for play in sorted(counts):
    print("PLAY " + str(counts[play]) + " " + play)

try:
    folded = ledger.fold_latest(store.read_lines(base))
except (OSError, ValueError) as error:
    print("FOLD fail")
    print("FOLDWHY " + " ".join(str(error).split()))
else:
    print("FOLD ok")
    print("FOLDWHY " + str(len(folded)) + " plays folded")
' > "${LEDGER_PROBE}"); then
    abort "the ledger probe could not run from ${PLAN_REPO_ROOT}" "see ${LEDGER_PROBE}"
fi
ok "the ledger probe ran"
printf '\n'

DOC_PATH="" DOC_EXISTS="" DOC_SELF="" DOC_NAMES=""
DOC_FILENAME="" DOC_STATEDIR="" DOC_GENERATED="" DOC_KERNEL=""
read_field DOC_PATH "${STATUS_PROBE}" PATH
read_field DOC_EXISTS "${STATUS_PROBE}" EXISTS
read_field DOC_SELF "${STATUS_PROBE}" SELF
read_field DOC_NAMES "${STATUS_PROBE}" SECTIONS
read_field DOC_FILENAME "${STATUS_PROBE}" FILENAME
read_field DOC_STATEDIR "${STATUS_PROBE}" STATEDIR
read_field DOC_GENERATED "${STATUS_PROBE}" GENERATED
read_field DOC_KERNEL "${STATUS_PROBE}" KERNEL

# --- 1. the document exists where the producer says it lives -----------------------------
check 1 "the status document exists at the producer's own path"
if [[ "${DOC_EXISTS}" == "yes" ]]; then
    ok "present at ${DOC_PATH} (collected ${DOC_GENERATED:-?} under kernel ${DOC_KERNEL:-?})"
    if [[ -n "${DOC_KERNEL}" ]] && [[ "${DOC_KERNEL}" != "${RUNNING_KERNEL}" ]]; then
        # Not a failure: the producer and both consumers are built for exactly this, and
        # demoting the boot-scoped section is the behaviour under test elsewhere. It is a
        # fact the reader of this verdict needs, because checks [4] and [5] then describe
        # a collection made under another kernel.
        note "the document was collected under kernel ${DOC_KERNEL}, not the running ${RUNNING_KERNEL}"
    fi
else
    bad "no status document at ${DOC_PATH}" \
        "the health unit has never completed a run here. Log out and log back in, then re-run this gate."
fi
printf '\n'

# --- 2. structurally sound, not merely parseable -----------------------------------------
#
# `read` turns absent, unparseable and unknown-schema into the self-section; a document
# that gets past all three and then carries sections a reader cannot interpret is the gap
# `unreadable_reasons` covers. Both are failures here, and they are distinguished, because
# "no document" and "a malformed document" need different things done about them.
check 2 "the document is structurally sound"
if [[ "${DOC_SELF}" == "yes" ]]; then
    self_reason=""
    if self_reason="$(grep -m1 -e "^TEXT status " "${STATUS_PROBE}")"; then
        bad "the reader could not use the document" "${self_reason#TEXT status }"
    else
        bad "the reader could not use the document" "and it gave no reason, which is its own defect"
    fi
elif grep -q -e '^UNREADABLE ' "${STATUS_PROBE}"; then
    unreadable_count="$(grep -c -e '^UNREADABLE ' "${STATUS_PROBE}")"
    bad "the document parses but ${unreadable_count} part(s) cannot be interpreted" \
        "see the UNREADABLE lines in ${STATUS_PROBE} — a consumer that substituted 'nothing' for each would read this as a clean host"
else
    ok "schema recognised, every section interpretable"
fi
printf '\n'

# --- 3. all four checks are in the document ----------------------------------------------
#
# This is what the panel's health section renders. A section the producer dropped becomes
# four derived "no such section" lines on that surface, or nothing at all.
check 3 "the document names all four checks"
missing_sections=()
for wanted in "${DOC_SECTIONS[@]}"; do
    if ! contains_word "${DOC_NAMES}" "${wanted}"; then
        missing_sections+=("${wanted}")
    fi
done
if [[ "${#missing_sections[@]}" -eq 0 ]]; then
    ok "all four present: ${DOC_NAMES}"
else
    bad "the document is missing ${#missing_sections[@]} section(s): ${missing_sections[*]}" \
        "the panel's health section has nothing to render for them; found: ${DOC_NAMES:-none}"
fi
printf '\n'

# --- 4. installed-vs-pinned compared every tracked pin -----------------------------------
#
# THE AXIS THE 2026-09-11 INCIDENT HAPPENED ON, and the one whose silence looks exactly
# like health. The section STATES the population it compared, on every run including a
# clean one, and this asserts the numbers.
#
# It used to grep for the literal phrase `compared 0 of ` and treat its absence as a real
# population. Two reachable states have no such phrase and no comparisons: a pin whose
# probe RAISED (the error finding suppresses the coverage sentence entirely), and PARTIAL
# coverage, whose wording is `compared 1 of 2`. Both printed PASS. That is coverage
# inferred from the absence of a complaint, under a file header promising the opposite,
# in the gate that vouches for the fix for exactly this defect.
#
# A drifted pin is a finding ABOUT the host and is noted, not rejected — that is the check
# working. Nothing compared is a finding about the CHECK, and rejects.
check 4 "installed-vs-pinned compared every tracked pin on this host"
pins_line=""
pins_state=""
pins_cov=""
if ! pins_line="$(grep -m1 -e '^STATE installed-vs-pinned ' "${STATUS_PROBE}")"; then
    bad "the document carries no installed-vs-pinned state" \
        "check [3] names what it does carry; without this section the axis is not merely quiet, it is absent"
elif pins_cov="$(grep -m1 -e '^COVERAGE installed-vs-pinned no tracked pin applies ' "${STATUS_PROBE}")"; then
    # The owner's decision: on a host with no DKMS subsystem a DKMS-resolved pin does
    # not apply, and when that is every tracked pin nothing is owed. Stated in words by
    # the producer, so it is shown rather than inferred.
    pins_state="${pins_line#STATE installed-vs-pinned }"
    ok "${pins_cov#COVERAGE installed-vs-pinned } (section state: ${pins_state})"
elif ! pins_cov="$(grep -m1 -e '^COVERAGE installed-vs-pinned compared ' "${STATUS_PROBE}")"; then
    bad "the pin section states no coverage, so there is no population to assert on" \
        "check_pins states 'compared N of M tracked pins' on every run; its absence means the producer changed and this gate stopped measuring"
else
    # "compared N of M tracked pins" — both numbers, always, so neither the zero case nor
    # the partial one can hide in a sentence that reads fine.
    pins_stated="${pins_cov#COVERAGE installed-vs-pinned compared }"
    pins_compared="${pins_stated%% *}"
    pins_rest="${pins_stated#* of }"
    pins_tracked="${pins_rest%% *}"
    pins_state="${pins_line#STATE installed-vs-pinned }"
    if ! [[ "${pins_compared}" =~ ^[0-9]+$ ]] || ! [[ "${pins_tracked}" =~ ^[0-9]+$ ]]; then
        bad "the pin section's coverage does not carry two numbers" \
            "read: ${pins_cov}. A reworded sentence breaks this gate silently, which is why the wording is asserted in the helper's own suite too"
    elif [[ "${pins_tracked}" -eq 0 ]]; then
        # `0 of 0` satisfies compared == tracked while describing a host that held
        # nothing against the repo. Vacuous, not clean.
        bad "the repo tracks no pin's install state, so this axis cannot fail" \
            "every pin in vars/version-pins.yml is declared untracked; nothing on this host was compared against the repo's versions"
    elif [[ "${pins_compared}" -ne "${pins_tracked}" ]]; then
        bad "the pin check compared ${pins_compared} of ${pins_tracked} tracked pins — the rest of this axis is dark" \
            "a host that compares some of its pins renders identically to one that compared them all; see the section's own findings below"
    else
        ok "compared ${pins_compared} of ${pins_tracked} tracked pins (section state: ${pins_state})"
    fi
    if [[ "${pins_state}" != "ok" ]]; then
        while IFS= read -r finding_line; do
            note "installed-vs-pinned: ${finding_line#TEXT installed-vs-pinned }"
        done < <(grep -e '^TEXT installed-vs-pinned ' "${STATUS_PROBE}")
    fi
fi
printf '\n'

# --- 5. play-freshness gave an answer ----------------------------------------------------
#
# Clean-and-silent, findings, and "I cannot tell you" are three different answers. The
# third is what a BROKEN sentinel or an unresolvable ledgered commit produces, and
# reporting it as clean is this plan's defect one layer up. Only the third rejects.
check 5 "play-freshness gave an answer rather than declining to"
fresh_line=""
fresh_state=""
declined=""
if ! fresh_line="$(grep -m1 -e '^STATE play-freshness ' "${STATUS_PROBE}")"; then
    bad "the document carries no play-freshness state" \
        "check [3] names what it does carry"
elif declined="$(grep -m1 -e '^TEXT play-freshness .*could not give an answer' "${STATUS_PROBE}")"; then
    bad "play-freshness judged no play at all" "${declined#TEXT play-freshness }"
else
    fresh_state="${fresh_line#STATE play-freshness }"
    ok "play-freshness answered (section state: ${fresh_state})"
    if [[ "${fresh_state}" != "ok" ]]; then
        while IFS= read -r finding_line; do
            note "play-freshness: ${finding_line#TEXT play-freshness }"
        done < <(grep -e '^TEXT play-freshness ' "${STATUS_PROBE}")
    fi
fi
printf '\n'

LEDGER_RUNS="" LEDGER_EXISTS="" LEDGER_BROKEN="" LEDGER_BROKENWHY="" LEDGER_CLEARED=""
LEDGER_GENESIS="" LEDGER_MALFORMED="" LEDGER_FOLD="" LEDGER_FOLDWHY="" LEDGER_CLOCK=""
read_field LEDGER_RUNS "${LEDGER_PROBE}" RUNS
read_field LEDGER_EXISTS "${LEDGER_PROBE}" EXISTS
read_field LEDGER_BROKEN "${LEDGER_PROBE}" BROKEN
read_field LEDGER_BROKENWHY "${LEDGER_PROBE}" BROKENWHY
read_field LEDGER_CLEARED "${LEDGER_PROBE}" CLEARED
read_field LEDGER_GENESIS "${LEDGER_PROBE}" GENESIS
read_field LEDGER_MALFORMED "${LEDGER_PROBE}" MALFORMED
read_field LEDGER_FOLD "${LEDGER_PROBE}" FOLD
read_field LEDGER_FOLDWHY "${LEDGER_PROBE}" FOLDWHY
read_field LEDGER_CLOCK "${LEDGER_PROBE}" FETCHCLOCK

# --- 6. the ledger is present, complete and trusted --------------------------------------
#
# Task 1.2's HOST item: verify the callback against a real run. Every Phase 2 check
# compares against this file, so a silently wrong one makes every check downstream
# silently wrong — which is why the genesis record and the fold both have to hold, not
# just the file's existence.
check 6 "the ledger is present, complete and trusted"
ledger_ok=1
if [[ "${LEDGER_EXISTS}" != "yes" ]]; then
    bad "no ledger at ${LEDGER_RUNS}" \
        "no play has been recorded on this host. Run ./deploy.bash — the callback writes on every applying run."
    ledger_ok=0
fi
if [[ "${LEDGER_BROKEN}" == "yes" ]]; then
    bad "the ledger carries a BROKEN sentinel — recording is failing" "${LEDGER_BROKENWHY}"
    ledger_ok=0
fi
if [[ "${LEDGER_MALFORMED}" != "-" ]]; then
    bad "the ledger has a malformed line" "${LEDGER_MALFORMED}"
    ledger_ok=0
fi
if [[ "${LEDGER_FOLD}" != "ok" ]]; then
    bad "the production reader refuses the ledger" "${LEDGER_FOLDWHY}"
    ledger_ok=0
fi
if [[ "${LEDGER_EXISTS}" == "yes" ]] && [[ "${LEDGER_GENESIS}" -lt 1 ]]; then
    # Without it, "no record for this play" cannot be told from "run before the ledger
    # existed", and Task 1.3's silence-is-correct rule loses its foundation.
    bad "the ledger has no genesis record" \
        "every silence about a play then becomes a guess rather than an answer"
    ledger_ok=0
fi
if [[ "${ledger_ok}" -eq 1 ]]; then
    ok "genesis present, ${LEDGER_FOLDWHY}, no hole recorded"
fi
if [[ "${LEDGER_CLEARED}" == "yes" ]]; then
    # Deliberately a note, not a failure. A cleared hole is a recorded, handled state —
    # `plays_run_here` keeps answering None for it, so nothing downstream is silently
    # narrowed — and the marker never goes away, so rejecting on it would reject for ever.
    note "the ledger carries a CLEARED marker: a past hole was forgiven, so its record set is a permanent lower bound"
fi
printf '\n'

# --- 7. a run record for each play deploy.bash runs ---------------------------------------
#
# The claim Task 1.2 actually makes: a real run produces one row per play. Checked against
# the four plays deploy.bash runs rather than against "some rows exist", because a ledger
# full of unrelated rows says nothing about whether these plays were recorded.
check 7 "the ledger holds a run record for each play deploy.bash runs"
unrecorded=()
for play in "${DEPLOY_PLAYS[@]}"; do
    if ! grep -q -e "^PLAY [0-9][0-9]* ${play}\$" "${LEDGER_PROBE}"; then
        unrecorded+=("${play}")
    fi
done
if [[ "${#unrecorded[@]}" -eq 0 ]]; then
    ok "all ${#DEPLOY_PLAYS[@]} plays recorded"
    # The counts are the evidence for "a second run appends": run ./deploy.bash again and
    # every number here goes up by one. Printed rather than asserted, because a single
    # deploy legitimately leaves every count at 1.
    while read -r _ play_count play_path; do
        printf '        %s run(s) recorded for %s\n' "${play_count}" "${play_path}"
    done < <(grep -e '^PLAY ' "${LEDGER_PROBE}")
else
    bad "${#unrecorded[@]} of ${#DEPLOY_PLAYS[@]} plays have no ledger record" \
        "${unrecorded[*]} — run ./deploy.bash, or the callback is not recording"
fi
printf '\n'

# --- 8. a non-applying run adds no ledger row --------------------------------------------
#
# `plugin_support.should_record` suppresses recording for check, syntax, list-hosts,
# list-tags and list-tasks alike — one guard, one flag set. `--list-tasks` is the member
# of that set this gate can safely exercise: it still drives the play loop, so the callback
# really is asked and really does decline, and it applies nothing.
#
# `--check` is NOT used, and deliberately: two of this plan's plays read a command task's
# registered stdout in a later task, and check mode skips command tasks, so a `--check`
# run fails on an undefined attribute before it proves anything about the ledger.
check 8 "a non-applying ansible run adds no ledger row"
if [[ "${LEDGER_EXISTS}" != "yes" ]]; then
    bad "no ledger to compare before and after" "check [6] explains why"
else
    size_before=""
    size_after=""
    list_out=""
    size_before="$(wc -c < "${LEDGER_RUNS}")"
    if ! list_out="$(plan_ansible_playbook playbooks/imports/optional/common/play-fedora-desktop-panel.yml --list-tasks 2>&1)"; then
        bad "ansible-playbook --list-tasks failed, so nothing was established" "${list_out}"
    else
        size_after="$(wc -c < "${LEDGER_RUNS}")"
        if [[ "${size_before}" == "${size_after}" ]]; then
            ok "the ledger is byte-identical across a --list-tasks run (${size_after} bytes)"
        else
            bad "a non-applying run wrote to the ledger (${size_before} -> ${size_after} bytes)" \
                "every drift verdict downstream would then rest on runs that applied nothing"
        fi
    fi
fi
printf '\n'

# --- 9. the login report's exit contract --------------------------------------------------
#
# Both halves of Task 3.2's "silent when clean" are asserted here, and they are one
# contract rather than two: exit 0 MUST come with no output, and exit 3 MUST come with
# some. Exit 3 rather than 1 is itself load-bearing — the unit declares 3 a success, so a
# crash (Python's 1) stays a unit failure instead of being declared healthy.
#
# This is the one check that runs something rather than reading what a run left.
check 9 "the login report honours its exit contract"
lr_out=""
lr_rc=0
if lr_out="$( (cd "${PLAN_REPO_ROOT}" && python3 -m helpers.host_health.login_report --no-notify --no-handoff) 2> "${LOGIN_ERR}" )"; then
    lr_rc=0
else
    lr_rc=$?
fi
if [[ "${lr_rc}" -eq 0 ]] && [[ -z "${lr_out}" ]]; then
    ok "clean and silent: exit 0, no output — which is what a clean login produces"
elif [[ "${lr_rc}" -eq 3 ]] && [[ -n "${lr_out}" ]]; then
    ok "findings reported with the documented exit status 3"
    while IFS= read -r finding_line; do
        note "login report: ${finding_line}"
    done <<< "${lr_out}"
elif [[ "${lr_rc}" -eq 0 ]]; then
    bad "exit 0 but the report spoke" "a clean login must be silent; it said: ${lr_out}"
elif [[ "${lr_rc}" -eq 3 ]]; then
    bad "exit 3 but the report said nothing" "the user is told there is a problem and never told which"
else
    bad "the login report exited ${lr_rc}, which is neither 0 nor 3" \
        "that is a crash, not a verdict — diagnostics in ${LOGIN_ERR}"
fi
printf '        the report'"'"'s own diagnostics (not findings) are in %s\n' "${LOGIN_ERR}"
printf '\n'

# --- 10. the notification's channel exists -------------------------------------------------
#
# Whether a notification actually APPEARS needs a human looking at a screen and is listed
# for them below. Whether the binary the desktop delivery calls is installed at all does
# not, and its absence would make every desktop login silently degrade to stdout that
# nobody reads.
check 10 "notify-send is installed"
if command -v notify-send > /dev/null; then
    ok "notify-send is on PATH"
else
    bad "notify-send is missing" \
        "play-gnome-shell.yml owns libnotify — the desktop delivery has no channel without it"
fi
printf '\n'

# --- 11. the unit is enabled AND wanted ----------------------------------------------------
#
# Task 3.1's HOST item, in its own words: "the play succeeded" is a DIFFERENT claim.
# `WantedBy=` in `[Install]` does nothing until `enable` writes the .wants symlink, and a
# deployed-but-unenabled unit never runs — which looks exactly like a healthy host.
check 11 "${HEALTH_UNIT} is enabled and wanted by ${HEALTH_TARGET}"
unit_enabled=""
if ! unit_enabled="$(systemctl --user is-enabled "${HEALTH_UNIT}")"; then
    bad "systemctl --user is-enabled ${HEALTH_UNIT} says '${unit_enabled:-nothing}'" \
        "run ./deploy.bash; if the user manager is unreachable, run this gate from inside the graphical session"
else
    deps=""
    # `--no-pager` for parity with triage.bash's probe. Without it the two commands are not
    # the same command, and a run where triage found the unit in the live graph and this
    # check did not is exactly the disagreement that must not be left to guesswork.
    if ! deps="$(systemctl --user list-dependencies "${HEALTH_TARGET}" --no-pager)"; then
        bad "systemctl --user list-dependencies ${HEALTH_TARGET} failed" \
            "the unit reads as '${unit_enabled}', but nothing confirms the target pulls it in"
    # NOT `printf … | grep -q`. `grep -q` exits the instant it matches, so printf dies of
    # SIGPIPE writing to the closed pipe, and `set -o pipefail` reports the pipeline as
    # FAILED — turning a match into a miss. It only bites once the text exceeds the 64 KiB
    # pipe buffer, which is why it read as intermittent: this check's `list-dependencies`
    # output is 330 KiB, so it inverted every run, while the small-output checks never did.
    # A bash pattern match spawns nothing and cannot be signalled.
    elif [[ "${deps}" == *"${HEALTH_UNIT}"* ]]; then
        ok "${unit_enabled}, and ${HEALTH_TARGET} names it among its dependencies"
    else
        # KEEP THE EVIDENCE. A previous run had triage find the unit in the live graph and
        # this check miss it minutes later, and nothing survived to say which reading was
        # wrong. The captured output makes the next occurrence answerable instead of a
        # second round of speculation.
        # PLAN_RUN_DIR, exported by plan_start_log: run logs live under untracked/ and are
        # unscrubbed, so this capture must never land beside the script in the plan folder.
        depsCapture="${PLAN_RUN_DIR}/check-11-list-dependencies.txt"
        printf '%s\n' "${deps}" > "${depsCapture}"
        bad "${HEALTH_UNIT} is '${unit_enabled}' but ${HEALTH_TARGET} does not name it" \
            "it will never fire at login; the .wants symlink under the user unit directory is missing or stale — the ${#deps} bytes this check actually read are in ${depsCapture}"
    fi
fi
printf '\n'

# --- 12. it has actually run, and did not fail ---------------------------------------------
#
# The step after [11], and not implied by it: a unit can be enabled and wanted and still
# never have fired, which is exactly the state after a deploy with no logout. An empty
# start timestamp is therefore a precise, actionable answer rather than a mystery.
check 12 "${HEALTH_UNIT} has run at a login and did not fail"
started=""
result=""
# `-P` (one property, value only) rather than parsing a two-property block: systemd does
# not promise the order it prints them in, and a prefix-strip that silently reads the
# wrong line would report a timestamp as a result.
if ! started="$(systemctl --user show "${HEALTH_UNIT}" -P ExecMainStartTimestamp)"; then
    bad "systemctl --user show ${HEALTH_UNIT} failed" "nothing can be said about whether it ran"
elif ! result="$(systemctl --user show "${HEALTH_UNIT}" -P Result)"; then
    bad "systemctl --user show ${HEALTH_UNIT} gave no Result" "it started at ${started}, but how it ended is unknown"
else
    if [[ -z "${started}" ]]; then
        bad "${HEALTH_UNIT} has never started" \
            "log out and log back in — that is what fires it, and it is also the only way the shell loads the panel"
    elif [[ "${result}" != "success" ]]; then
        bad "${HEALTH_UNIT} last ran at ${started} and ended '${result}'" \
            "SuccessExitStatus=3 already covers 'there were findings', so this is a real failure — journalctl --user -u ${HEALTH_UNIT} --no-pager"
    else
        ok "last ran ${started}, result ${result}"
    fi
fi
printf '\n'

# --- 13. the panel extension is deployed complete and declared enabled ----------------------
#
# Both halves matter and they fail differently. A missing file fails at runtime with
# nothing on screen; a complete extension that is not in the gsettings key is never loaded
# at all, because that key is the only thing the shell reads at session start.
check 13 "the panel extension is deployed complete and declared enabled"
ext_dir="${HOME}/.local/share/gnome-shell/extensions/${EXT_UUID}"
missing_files=()
for relative in "${EXT_FILES[@]}"; do
    if [[ ! -f "${ext_dir}/${relative}" ]]; then
        missing_files+=("${relative}")
    fi
done
if [[ ! -d "${ext_dir}" ]]; then
    bad "the extension is not deployed at ${ext_dir}" "run ./deploy.bash"
elif [[ "${#missing_files[@]}" -ne 0 ]]; then
    bad "${#missing_files[@]} shipped file(s) missing from the deployed extension" \
        "${missing_files[*]} — a file that exists in the repo and never deploys fails silently at runtime"
else
    declared=""
    if ! declared="$(gsettings get org.gnome.shell enabled-extensions)"; then
        bad "gsettings could not read org.gnome.shell enabled-extensions" \
            "that key is the only thing the shell reads at session start, so nothing confirms the panel will load"
    elif [[ "${declared}" == *"${EXT_UUID}"* ]]; then
        ok "all ${#EXT_FILES[@]} files deployed, and the uuid is in enabled-extensions"
    else
        bad "the extension is deployed but its uuid is not in enabled-extensions" \
            "it will never load; re-run ./deploy.bash — the play merges the uuid without removing others"
    fi
fi
printf '\n'

# --- 14. the deployed panel agrees about where the document lives ---------------------------
#
# Checked against the DEPLOYED copy, not the repo's. The repo half is already a QA gate;
# what nothing at runtime announces is a deployed panel reading the wrong path, because it
# then reports `unavailable` for ever — indistinguishable from a producer that never ran.
# Both halves of the path are compared: the wrong directory and the wrong file name fail
# identically and silently.
check 14 "the deployed panel and the producer agree on the document path"
deployed_reader="${ext_dir}/statusDocument.js"
if [[ ! -f "${deployed_reader}" ]]; then
    bad "no deployed statusDocument.js at ${deployed_reader}" "check [13] explains why"
else
    path_mismatch=()
    if ! grep -q -F -e "${DOC_FILENAME}" "${deployed_reader}"; then
        path_mismatch+=("file name ${DOC_FILENAME}")
    fi
    if ! grep -q -F -e "${DOC_STATEDIR}" "${deployed_reader}"; then
        path_mismatch+=("state directory ${DOC_STATEDIR}")
    fi
    if [[ "${#path_mismatch[@]}" -eq 0 ]]; then
        ok "the deployed reader declares the producer's ${DOC_STATEDIR}/${DOC_FILENAME}"
    else
        bad "the deployed panel does not declare the producer's path" \
            "missing: ${path_mismatch[*]} — the panel would report 'unavailable' for ever"
    fi
fi
printf '\n'

# --- 15. the running shell has loaded it ----------------------------------------------------
#
# Driven through `helpers.gnome.verify_extension`, the production verifier, so this gate
# and the plays agree about what "loaded" means. Its pending_reload / pending_scan verdicts
# are NOT failures to that helper — they mean the shell has not scanned or reloaded yet —
# but they ARE failures here, because Task 4.5's claim is specifically about the state
# AFTER the logout it asks for.
check 15 "the running shell has loaded the panel extension"
verify_out=""
verify_rc=0
if verify_out="$( (cd "${PLAN_REPO_ROOT}" && python3 -m helpers.gnome.verify_extension --uuid "${EXT_UUID}") 2>&1 )"; then
    verify_rc=0
else
    verify_rc=$?
fi
if [[ "${verify_rc}" -ne 0 ]]; then
    bad "the shell reports the extension as broken" "${verify_out}"
elif [[ "${verify_out}" == *"[ok]"* ]]; then
    ok "${verify_out}"
else
    bad "the extension is not loaded in this session" \
        "${verify_out} — log out and log back in; on Wayland nothing else loads new extension code"
fi
printf '\n'

# --- 16. the vm-test-lab scenario reached the deployed allowlist -----------------------------
#
# Task 3.2's HOST item, and the reason it is a HOST item: the bridge's authority is the
# DEPLOYED allowlist, and it refuses an id that exists only in the tracked manifest. Until
# this passes the VM leg below cannot even be requested.
check 16 "the ${VMTEST_SCENARIO} scenario reached the deployed allowlist"
lab_dir="${HOME}/.local/share/vmtest"
allowlist="${lab_dir}/scenarios.allowlist"
if [[ ! -f "${allowlist}" ]]; then
    bad "no deployed allowlist at ${allowlist}" "run ./deploy.bash — play-vm-test-lab.yml renders it"
elif ! grep -q -F -x -e "${VMTEST_SCENARIO}" "${allowlist}"; then
    bad "${VMTEST_SCENARIO} is not in the deployed allowlist" \
        "the bridge will refuse the request; re-run ./deploy.bash from a checkout that has the scenario in vars/vm-test-scenarios.yml"
else
    absent_scripts=()
    for guest_script in "${VMTEST_SCRIPTS[@]}"; do
        if [[ ! -x "${lab_dir}/${guest_script}" ]]; then
            absent_scripts+=("${guest_script}")
        fi
    done
    if [[ "${#absent_scripts[@]}" -eq 0 ]]; then
        ok "the scenario is allowlisted and both guest scripts are deployed executable"
    else
        bad "${#absent_scripts[@]} guest script(s) missing or not executable" \
            "${absent_scripts[*]} in ${lab_dir} — the run would fail in the guest with no checker"
    fi
fi
printf '\n'

# --- 17. a remote a timer can fetch with no agent present -------------------------------------
#
# Task 3.2's last HOST item. The freshness axis fetches from an unattended user unit, so
# the question is not "is there a remote" but "does it resolve WITHOUT a human at a
# prompt". BatchMode and GIT_TERMINAL_PROMPT=0 make an agent-dependent or
# passphrase-dependent remote fail here rather than hang there for ever.
check 17 "this checkout has a remote a timer can fetch with no agent present"
remote_url=""
if ! remote_url="$(git -C "${PLAN_REPO_ROOT}" remote get-url origin 2>&1)"; then
    bad "this checkout has no 'origin' remote" \
        "the freshness axis would report 'never reached the remote' for ever: ${remote_url}"
else
    ls_out=""
    if ls_out="$(GIT_TERMINAL_PROMPT=0 GIT_SSH_COMMAND='ssh -o BatchMode=yes' \
        git -C "${PLAN_REPO_ROOT}" ls-remote --exit-code origin HEAD 2>&1)"; then
        ok "origin (${remote_url}) resolves non-interactively"
        printf '        last recorded successful fetch: %s\n' "${LEDGER_CLOCK}"
    else
        bad "origin (${remote_url}) does not resolve without a prompt or an agent" \
            "${ls_out} — if this host is simply offline, re-run when it is not: this asks whether the remote resolves unattended, and offline cannot answer that either way"
    fi
fi
printf '\n'

# --- 18. the background-recovery action is deployed and armed ----------------------------------
#
# Task 5.4's HOST deploy half. The file contents are checked, not just the paths: the play
# has deployed a recovery tree since before this plan, so a present run_recovery.py says
# nothing about whether THIS plan's action reached the host.
check 18 "the DisplayLink background-recovery action is deployed and armed"
recovery_missing=()
for recovery_file in "${RECOVERY_DIR}/recovery.py" "${RECOVERY_DIR}/run_recovery.py" \
    "${RECOVERY_RULE}" "${RECOVERY_DOCK_UNIT}"; do
    if [[ ! -f "${recovery_file}" ]]; then
        recovery_missing+=("${recovery_file}")
    fi
done
if [[ "${#recovery_missing[@]}" -ne 0 ]]; then
    bad "${#recovery_missing[@]} recovery artefact(s) not deployed" \
        "${recovery_missing[*]} — run ./deploy.bash"
elif ! grep -q -F -e "REFRESH_BACKGROUND" "${RECOVERY_DIR}/recovery.py"; then
    bad "the deployed recovery logic has no REFRESH_BACKGROUND action" \
        "this is the pre-Task-5.4 build: the tree is deployed but the action this plan added is not"
else
    suspend_enabled=""
    if ! suspend_enabled="$(systemctl is-enabled "${RECOVERY_SUSPEND_UNIT}")"; then
        bad "${RECOVERY_SUSPEND_UNIT} is '${suspend_enabled:-not enabled}'" \
            "the resume path would never fire; the dock udev path is unaffected"
    else
        ok "recovery tree, udev rule and dock unit deployed; ${RECOVERY_SUSPEND_UNIT} is ${suspend_enabled}"
    fi
fi
printf '\n'

# ── what this script will not pretend to have established ────────────────────────────────
#
# Named, never faked, and never counted toward COVERAGE. Each line says WHY a script
# cannot settle it, because "a human must do it" with no reason is how an item quietly
# becomes nobody's.
human "Task 0.2 — run ./triage.bash and read its rpm-ownership section. Whether each /usr/src/evdi-* tree is rpm-owned or unowned decides which cleanup mechanism the play gets, and the two are opposites. A script cannot choose; only the answer can."
human "Task 3.2 (VM) — ./scripts/vmtest-request.bash run-scenario ${VMTEST_SCENARIO}. It builds and boots a guest across a kernel change. Nothing in that claim is about THIS machine, so this gate cannot make it; check [16] establishes only that the request will be accepted."
human "Task 3.2 — that a notification actually APPEARS on screen at login. Check [10] proves the channel exists and check [12] proves the unit ran; whether a human saw it needs a human. Sending a test notification from here would put a popup on the screen, which a read-only gate must not do."
human "Task 4.2 — whether St renders the demoted lines legibly and whether the icon is the right thing to look at. Only a Wayland session and a pair of eyes can say, and the test harness deliberately does not claim to."
human "Task 4.5 — that the panel icon is visibly in the top bar. Checks [13]-[15] establish that it is deployed, declared and loaded by the shell, which is everything short of seeing it."
human "Task 5.4 — that the refresh actually clears a BLACK background. It needs the symptom present, and the symptom is an upstream mutter bug that cannot be induced on demand. Check [18] establishes only that the action is deployed and armed."
human "Task 5.4a — cover the unlock case. OWNER'S CALL between the panel and a user unit, not unwritten code. See DESIGN-panel.md §12."
human "Task 4.3 — the play/task runner is not written. It needs the ledger's real contents, which check [7] is the first thing to produce."
human "Task 0.3 — the vault password file's permissions. Human-only: the path is guarded, so no agent can name it in a script, a command or a play."
human "Success criterion — installed-vs-pinned FAILING when pointed at the 2026-09-11 state. That state is in the past and cannot be re-observed read-only; it is proven by the unit tests under tests/helpers/version_pins/, not on a host. Check [4] establishes the other direction."
human "Success criterion — freshness reporting a play edited AFTER its ledgered run. Establishing it means editing a play, which a read-only gate must not do. Check [5] establishes that the axis answers at all."
human "Success criterion — host-only checks skipping cleanly in CI. Needs a green run on a pushed branch; a host cannot observe CI."
human "Success criterion — a second qa-reviewer pass over the full plan diff. An agent task, not a host one."

# ── verdict ──────────────────────────────────────────────────────────────────────────────
#
# A check that is deleted, renumbered or skipped by an early return disappears from
# RAN_CHECKS and is NAMED here, and an incomplete run is REJECTED even with zero failures:
# a gate that did not run all of its checks has not established what it claims to.
missing_checks=()
for expected in "${EXPECTED_CHECKS[@]}"; do
    if ! contains_word "${RAN_CHECKS[*]}" "${expected}"; then
        missing_checks+=("${expected}")
    fi
done

if [[ "${#NOTES[@]}" -ne 0 ]]; then
    printf '==============================================================\n'
    printf 'FINDINGS ABOUT THIS HOST (reported, not rejected — the checks worked)\n'
    for entry in "${NOTES[@]}"; do
        printf '  - %s\n' "${entry}"
    done
    printf '\n'
fi

printf '==============================================================\n'
printf 'FOR THE HUMAN — not establishable by this script, not counted\n'
for entry in "${HUMAN[@]}"; do
    printf '  - %s\n' "${entry}"
done
printf '\n'

printf '==============================================================\n'
printf 'COVERAGE: %d of %d checks executed (%d assertion(s) passed, %d failed)\n' \
    "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}"
if [[ "${#missing_checks[@]}" -ne 0 ]]; then
    printf '  NOT RUN: %s\n' "${missing_checks[*]}" >&2
fi

# Written here rather than streamed, so the file holds the verdict and not a partial run.
{
    printf 'Plan 00109 acceptance — %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
    printf 'kernel: %s\n' "${RUNNING_KERNEL}"
    printf 'coverage: %d of %d checks executed, %d passed, %d failed\n' \
        "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}"
    if [[ "${#missing_checks[@]}" -ne 0 ]]; then
        printf 'not run: %s\n' "${missing_checks[*]}"
    fi
    printf '\nfindings about this host (reported, not rejected):\n'
    if [[ "${#NOTES[@]}" -eq 0 ]]; then
        printf '  (none)\n'
    else
        for entry in "${NOTES[@]}"; do
            printf '  - %s\n' "${entry}"
        done
    fi
    printf '\nfor the human — not establishable by this script:\n'
    for entry in "${HUMAN[@]}"; do
        printf '  - %s\n' "${entry}"
    done
} > "${VERDICT_REPORT}"

# plan_finish is NOT the closer here, and that is deliberate rather than an omission: it
# exits 0 or 1, and this gate has a third answer. "Could not establish" is not "rejected"
# — one says the plan is not done, the other says nobody has been told either way — and
# collapsing them is the same conflation this plan exists to abolish. R10 still holds:
# plan_list_reports names every report the run wrote, and the EXIT trap still drains the
# run log.
plan_list_reports
if [[ -n "${PLAN_RUN_LOG}" ]]; then
    printf '==> run log: %s\n' "${PLAN_RUN_LOG}"
fi

if [[ "${FAIL}" -eq 0 ]] && [[ "${#missing_checks[@]}" -eq 0 ]]; then
    printf 'ACCEPTED — every declared check ran and every assertion passed.\n'
    printf '           The FOR THE HUMAN list above is still outstanding.\n'
    printf '==============================================================\n'
    exit 0
fi
if [[ "${FAIL}" -eq 0 ]]; then
    printf 'REJECTED — no assertion failed, but %d declared check(s) never ran.\n' "${#missing_checks[@]}" >&2
else
    printf 'REJECTED — %d assertion(s) failed, %d passed.\n' "${FAIL}" "${PASS}" >&2
fi
printf '           Fix what is named above, re-run ./deploy.bash if a play is\n' >&2
printf '           implicated, log out and back in, then re-run this gate.\n' >&2
printf '==============================================================\n' >&2
exit 1
