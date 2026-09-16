#!/usr/bin/env bash
# Plan 00112 — acceptance.bash
#
# PURPOSE: render the VERDICT on what this plan claims about THIS machine, after deploy.bash
# has run. triage.bash records facts and judges nothing (CLAUDE/PlanScriptStandards.md R9);
# this script is the pass/fail gate.
#
# WHAT IT PROVES HERE: every UUID vars/gnome-shell-extensions.yml declares is on disk, is
# present in org.gnome.shell enabled-extensions, was ADDED to that list without anything
# being removed, is ACTIVE in the live session, and that re-running the play would report no
# change. It reads the population from the same declaration the play reads, and judges the
# live state through the play's own helper, so the gate cannot drift from the code it vouches
# for.
#
# WHAT IT CANNOT PROVE HERE: anything needing the VM lab — Task 2.2 and Success Criterion 1
# (`vmtest run desktop-fresh-install`, green 16/16 in the post-reboot session). Those are
# printed at the end under NOT ESTABLISHABLE HERE and are never counted as passing checks.
#
# READ-ONLY: it reads gsettings, the repo's own helper modules and the run evidence already on
# disk. It runs no playbook and writes nothing but its own report. HOST ONLY (R2) — a verdict
# about a GNOME session cannot be reached from a container.
#
# RUN IT AFTER the Task 2.1 sequence: triage.bash, deploy.bash, deploy.bash, triage.bash.
# Check [5] proves "nothing was removed" by comparing the triage report from BEFORE the
# deploy with the one from AFTER it, so without that pair it reports could-not-establish
# rather than a pass.
#
# EXIT STATUS
#   0  ACCEPTED — every declared check ran and every assertion passed
#   1  REJECTED — an assertion failed, or a declared check never ran
#   2  COULD NOT ESTABLISH — nothing failed, but a check had no evidence to judge (no live
#      GNOME session, or no before/after triage pair). This is NOT a pass
#  64  usage error
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

Renders the verdict on Plan 00112 against THIS host, after deploy.bash has run:

  0. the declared population reads from vars/gnome-shell-extensions.yml
  1. the play derives its population from that same declaration
  2. every declared UUID is deployed on disk
  3. every declared UUID is in org.gnome.shell enabled-extensions
  4. disable-user-extensions is false
  5. the deploy removed nothing from the enabled list (needs the before/after triage pair)
  6. re-running the play would report no change (idempotent declared state)
  7. the play's own verifier fails no declared extension
  8. every declared extension is ACTIVE in the live session
  9. the host's deployed VM-lab guest checker matches this repo's copy

The verdict carries a COVERAGE line counting these against what actually ran, so a
check that stops executing is visible rather than absorbed into a lower pass count.
An incomplete run is REJECTED even with no failures.

Read-only and host-only. It runs no playbook, so --check changes nothing here.

Exit 0 = ACCEPTED, 1 = REJECTED, 2 = COULD NOT ESTABLISH, 64 = usage error."

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "the verdict is about this machine's GNOME session, its dconf database and its deployed VM-lab checker"

plan_start_log auto

VARS_FILE="${PLAN_REPO_ROOT}/vars/gnome-shell-extensions.yml"
PLAY_FILE="${PLAN_REPO_ROOT}/playbooks/imports/play-gnome-shell-extensions.yml"
USER_EXTENSIONS_DIR="${HOME}/.local/share/gnome-shell/extensions"
SYSTEM_EXTENSIONS_DIR="/usr/share/gnome-shell/extensions"
GUEST_CHECKER_RELATIVE="files/home/.local/share/vmtest/guest-acceptance-desktop.bash"
# Where plan_start_log auto puts every run of this plan's scripts, so check [5] can find the
# triage reports that bracket the deploy. Derived from the plan folder's own name, never
# spelled out, so a renumbered plan folder does not silently look at nothing.
RUNS_ROOT="${PLAN_REPO_ROOT}/untracked/plan-runs/$(basename "${PLAN_SCRIPT_DIR}")"
REPORT="${PLAN_RUN_DIR}/plan-00112-acceptance-report.md"
readonly VARS_FILE PLAY_FILE USER_EXTENSIONS_DIR SYSTEM_EXTENSIONS_DIR
readonly GUEST_CHECKER_RELATIVE RUNS_ROOT REPORT

# The checks this gate is expected to run. COVERAGE is printed against this list because the
# PASS count cannot carry it: several sections emit one pass, several emit one per extension,
# and a section that cannot judge emits none at all. "ACCEPTED — 9 passed" reads identically
# whether 9 of 10 ran or 10 of 10, and coverage implied by a count rather than stated is this
# repo's named recurring defect class.
EXPECTED_CHECKS=(0 1 2 3 4 5 6 7 8 9)
RAN_CHECKS=()

PASS=0
FAIL=0
UNKNOWN=0

# Claims that need the VM lab. They are PRINTED, never counted — a gate that quietly folded
# them into its pass count would report this plan complete while its own PLAN.md records it
# blocked.
NOT_ESTABLISHABLE=(
    "Task 2.2 / Success Criterion 1 — 'vmtest run desktop-fresh-install' green 16/16 with deployed-extensions-active passing in the POST-REBOOT session. Needs the VM lab, and is blocked on the lab redeploy check [9] reports on (harness defect: Plan 00117)."
    "The fresh-install property itself — that a machine which never had these extensions ends up with them enabled and no manual step. This host's dconf already carries state from earlier runs, so no run here can distinguish 'the play declared it' from 'it was already declared'. Only a clean guest proves it."
    "Success Criterion 2's 'a host with EXTRA user-enabled extensions' clause, unless this host actually has some. Check [5] proves nothing was removed and reports how many pre-existing UUIDs were not declared by this repo; if that count is 0 the removal path was never exercised with a user's own extension present."
)

report_line() {
    printf -- '%s\n' "$*" >>"${REPORT}"
}

# Announce a check AND record that it ran. Every numbered section starts here; a section that
# prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    printf '[%s] %s\n' "$1" "$2"
    report_line ""
    report_line "### [$1] $2"
}

ok() {
    printf '  PASS  %s\n' "$1"
    PASS=$((PASS + 1))
    report_line "- PASS — $1"
}

bad() {
    printf '  FAIL  %s\n' "$1" >&2
    if [[ "$#" -gt 1 ]]; then
        printf '        %s\n' "$2" >&2
        report_line "- FAIL — $1"
        report_line "  - $2"
    else
        report_line "- FAIL — $1"
    fi
    FAIL=$((FAIL + 1))
}

# A check that had nothing to judge. Deliberately NOT a pass and NOT a failure: "there is no
# live GNOME session" is not evidence that the extensions are healthy, and it is not evidence
# that they are broken either. Counting it either way is the confident-wrong-answer failure
# this repo's plan scripts exist to avoid.
unknown() {
    printf '  UNKNOWN  %s\n' "$1"
    if [[ "$#" -gt 1 ]]; then
        printf '           %s\n' "$2"
        report_line "- COULD NOT ESTABLISH — $1"
        report_line "  - $2"
    else
        report_line "- COULD NOT ESTABLISH — $1"
    fi
    UNKNOWN=$((UNKNOWN + 1))
}

join_csv() {
    local IFS=','
    printf '%s' "$*"
}

# Read one marker value out of the state probe's captured stdout. Returns 1 when the marker is
# absent, so a caller distinguishes "the probe did not report this" from "it reported empty".
probe_marker() {
    local name="$1" line=""
    if ! line="$(grep -E "^${name} " <<<"${PROBE_OUT}")"; then
        return 1
    fi
    printf '%s' "${line#"${name} "}"
}

# The verdict. Called from exactly one place at the end, and from check [0] when the state
# probe fails outright and every later check would be judging nothing.
#
# It does not end at plan_finish on every path: plan_finish knows two outcomes (all legs OK,
# or some failed) and this gate has three, because "could not be established" must not read as
# either. So the accepted path ends at plan_finish, and the other two list the reports the same
# way plan_finish would (R10) and exit with the status the text says.
render_verdict() {
    local expected missing=()
    for expected in "${EXPECTED_CHECKS[@]}"; do
        case " ${RAN_CHECKS[*]} " in
            *" ${expected} "*) ;;
            *) missing+=("${expected}") ;;
        esac
    done

    printf '\n'
    printf '==> NOT ESTABLISHABLE HERE (for the human — never counted as a passed check):\n'
    report_line ""
    report_line "## Not establishable on this machine"
    local item
    for item in "${NOT_ESTABLISHABLE[@]}"; do
        printf -- '  - %s\n' "${item}"
        report_line "- ${item}"
    done

    printf '\n==============================================================\n'
    printf 'COVERAGE: %s of %s checks executed (%s assertion(s) passed, %s failed, %s could not be established)\n' \
        "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}" "${UNKNOWN}"
    report_line ""
    report_line "## Verdict"
    report_line "- COVERAGE: ${#RAN_CHECKS[@]} of ${#EXPECTED_CHECKS[@]} checks executed (${PASS} passed, ${FAIL} failed, ${UNKNOWN} could not be established)"

    if [[ "${#missing[@]}" -ne 0 ]]; then
        printf '  NOT RUN: %s\n' "${missing[*]}" >&2
        report_line "- NOT RUN: ${missing[*]}"
    fi

    if [[ "${FAIL}" -eq 0 ]] && [[ "${UNKNOWN}" -eq 0 ]] && [[ "${#missing[@]}" -eq 0 ]]; then
        printf 'ACCEPTED — every declared check ran and every assertion passed.\n'
        printf 'The VM-lab claims above remain unproven; this gate does not speak for them.\n'
        printf '==============================================================\n'
        report_line "- ACCEPTED"
        plan_finish
    fi

    if [[ "${FAIL}" -ne 0 ]] || [[ "${#missing[@]}" -ne 0 ]]; then
        if [[ "${FAIL}" -ne 0 ]]; then
            printf 'REJECTED — %s assertion(s) failed, %s passed.\n' "${FAIL}" "${PASS}" >&2
            report_line "- REJECTED (${FAIL} assertion(s) failed)"
        else
            printf 'REJECTED — no assertion failed, but %s declared check(s) never ran.\n' "${#missing[@]}" >&2
            report_line "- REJECTED (${#missing[@]} declared check(s) never ran)"
        fi
        printf '  Fix the findings above, re-run deploy.bash, then re-run this gate.\n' >&2
        printf '==============================================================\n' >&2
        plan_list_reports
        exit 1
    fi

    printf 'COULD NOT ESTABLISH — nothing failed, but %s check(s) had no evidence to judge.\n' "${UNKNOWN}" >&2
    printf '  This is NOT a pass. Each one names what it needs above.\n' >&2
    printf '==============================================================\n' >&2
    report_line "- COULD NOT ESTABLISH (${UNKNOWN} check(s) had no evidence)"
    plan_list_reports
    exit 2
}

{
    printf '# Plan 00112 — acceptance verdict\n\n'
    printf 'Generated by CLAUDE/Plan/00112-gnome-extensions-enabled-state-declared/acceptance.bash on the HOST.\n\n'
    printf -- '- repo root (resolved from the script, not the cwd): %s\n' "${PLAN_REPO_ROOT}"
    printf -- '- run directory: %s\n' "${PLAN_RUN_DIR}"
    printf -- '- declaration read: %s\n' "${VARS_FILE}"
} >"${REPORT}"

printf '==============================================================\n'
printf 'Plan 00112 acceptance — the enabled list as declared state\n'
printf '==============================================================\n'

# ── the state probe ───────────────────────────────────────────────────────────────────────
# One read-only probe gathers the facts checks [0], [2], [3], [4] and [6] judge, and it gathers
# them THROUGH THE PRODUCTION CODE: helpers.gnome.enabled_extensions resolves the declared set
# against disk, parses the GVariant list and computes the merge, and helpers.gnome.session_bus
# resolves the bus exactly as the applier does. A gate that re-implemented any of those would
# be testing its own second opinion.
PROBE_PY='
import os
import shutil
import subprocess
import sys

sys.path.insert(0, sys.argv[1])

import yaml

from helpers.gnome import enabled_extensions as ee
from helpers.gnome import session_bus

# An absent tool is an IaC gap (CLAUDE.md, "Missing Dependencies"), NOT a reason to report
# "could not establish". It has to be caught HERE: every command below is run behind a
# dbus-run-session wrapper when there is no live bus, so a missing binary comes back as that
# wrapper exiting 127 — indistinguishable from "there is no session", which is a legitimate
# result. Conflating the two is the same defect this plan fixed one layer down, where
# "the shell has no record of this extension" was reported as "there is no session".
missing_tools = [tool for tool in ("gsettings", "gnome-extensions") if shutil.which(tool) is None]
if missing_tools:
    raise SystemExit(
        "not installed: "
        + ", ".join(missing_tools)
        + ". These come from glib2 and gnome-extensions-app, which "
        "playbooks/imports/play-gnome-shell-extensions.yml installs. Run that play; this gate "
        "will not downgrade a missing tool to an unproven check."
    )

vars_file, user_dir, system_dir = sys.argv[2], sys.argv[3], sys.argv[4]

with open(vars_file, encoding="utf-8") as handle:
    groups = (yaml.safe_load(handle) or {}).get("gnome_shell_extensions")
# Every group, not three names spelled out: a fourth group the play enables must not be
# silently uncounted here. A missing or renamed key is an ERROR, never an empty population,
# because "0 of 0 declared" is a clean pass over a check that examined nothing.
if not isinstance(groups, dict) or not groups:
    raise SystemExit(f"{vars_file}: gnome_shell_extensions is missing or not a mapping of groups")
declared = [entry["uuid"] for entries in groups.values() for entry in entries or []]
if not declared:
    raise SystemExit(f"{vars_file}: declares no extensions at all")
print("ACC-DECLARED " + ",".join(declared))

resolution = ee.resolve_declared([user_dir, system_dir], declared)
print("ACC-DEPLOYED " + ",".join(resolution.found))
print("ACC-NOT-DEPLOYED " + ",".join(resolution.missing))

bus = session_bus.current()
env = session_bus.env_for(bus, os.environ)
print("ACC-BUS " + bus.source)


def gsettings(*args):
    """The value, or None and the reason there is none. A missing gsettings raises."""
    result = subprocess.run(
        [*bus.prefix, "gsettings", *args], text=True, capture_output=True, env=env, check=False
    )
    if result.returncode != 0:
        return None, " ".join((result.stderr + " " + result.stdout).split())
    return result.stdout, ""


disabled, disabled_error = gsettings("get", "org.gnome.shell", "disable-user-extensions")
if disabled is None:
    print("ACC-DISABLE-UNREADABLE " + disabled_error)
else:
    print("ACC-DISABLE " + disabled.strip())

raw, raw_error = gsettings("get", "org.gnome.shell", "enabled-extensions")
if raw is None:
    print("ACC-ENABLED-UNREADABLE " + raw_error)
else:
    try:
        current = ee.parse_string_list(raw)
    except ValueError as error:
        print("ACC-ENABLED-UNREADABLE " + str(error))
    else:
        print("ACC-ENABLED " + ",".join(current))
        merged = ee.merge(current, resolution.found)
        print("ACC-MERGE-CHANGED " + ("yes" if merged.changed else "no"))
        print("ACC-MERGE-WOULD-ADD " + ",".join(merged.added))
'
readonly PROBE_PY

PROBE_OUT=""
probeStatus=0
# stderr is folded into the capture because it IS the diagnosis when the probe fails, and it
# is printed by whichever check reports on it. Nothing is discarded.
PROBE_OUT="$(python3 -c "${PROBE_PY}" \
    "${PLAN_REPO_ROOT}" "${VARS_FILE}" "${USER_EXTENSIONS_DIR}" "${SYSTEM_EXTENSIONS_DIR}" 2>&1)" ||
    probeStatus=$?

# --- 0. precondition: the population this gate judges ------------------------------------
# Without it nothing downstream means anything: every later check would compare an empty set
# and report a clean pass over a population of zero.
check 0 "the declared population reads from vars/gnome-shell-extensions.yml"
DECLARED_CSV=""
if [[ "${probeStatus}" -ne 0 ]]; then
    bad "the state probe failed (exit ${probeStatus}) — this gate can judge nothing" "${PROBE_OUT}"
    render_verdict
fi
if ! DECLARED_CSV="$(probe_marker ACC-DECLARED)" || [[ -z "${DECLARED_CSV}" ]]; then
    bad "the state probe reported no declared extensions" "${PROBE_OUT}"
    render_verdict
fi
readonly DECLARED_CSV
DECLARED_UUIDS=()
IFS=',' read -r -a DECLARED_UUIDS <<<"${DECLARED_CSV}"
DECLARED_COUNT="${#DECLARED_UUIDS[@]}"
readonly DECLARED_COUNT
ok "${DECLARED_COUNT} extension(s) declared: ${DECLARED_CSV}"
report_line "- bus route used to read dconf: $(probe_marker ACC-BUS || printf 'not reported')"

# --- 1. the play judges the same population this gate does -------------------------------
# The blocker recorded in Task 2.2 is exactly this drift: a checker that derived the deployed
# population differently from the play collapsed its expectation to one extension and passed
# on eight. So the gate asserts the play still reads THIS declaration, still takes every group
# from it, and still enables through the declared-state applier rather than by asking the
# running shell.
check 1 "the play derives its UUID population from the same declaration"
playFindings=()
if [[ ! -f "${PLAY_FILE}" ]]; then
    playFindings+=("${PLAY_FILE} does not exist")
else
    if ! grep -qF 'vars/gnome-shell-extensions.yml' "${PLAY_FILE}"; then
        playFindings+=("the play no longer loads vars/gnome-shell-extensions.yml")
    fi
    if ! grep -qF 'gnome_shell_extensions.values()' "${PLAY_FILE}"; then
        playFindings+=("declared_extension_uuids no longer spans EVERY group (.values()), so a group could be enabled and unjudged")
    fi
    if ! grep -qF 'helpers.gnome.apply_enabled_extensions' "${PLAY_FILE}"; then
        playFindings+=("the play no longer enables through the declared-state applier")
    fi
fi
if [[ "${#playFindings[@]}" -eq 0 ]]; then
    ok "the play loads the declaration, spans every group and enables via the applier"
else
    bad "this gate and the play may be judging different populations" "$(
        IFS='; '
        printf '%s' "${playFindings[*]}"
    )"
fi

# --- 2. every declared UUID is on disk ----------------------------------------------------
# Resolved by the applier's own resolve_declared, over the same two search paths the play
# passes it. A declared UUID that is not on disk is the partial install the applier hard-fails
# on, and enabling what did arrive would report it as success.
check 2 "every declared extension is deployed on disk"
notDeployed=""
notDeployed="$(probe_marker ACC-NOT-DEPLOYED || printf '')"
deployedCsv=""
deployedCsv="$(probe_marker ACC-DEPLOYED || printf '')"
if [[ -n "${notDeployed}" ]]; then
    bad "declared but not deployed: ${notDeployed}" \
        "searched ${USER_EXTENSIONS_DIR} and ${SYSTEM_EXTENSIONS_DIR} — re-run deploy.bash"
else
    ok "all ${DECLARED_COUNT} declared extension(s) found under ${USER_EXTENSIONS_DIR} or ${SYSTEM_EXTENSIONS_DIR}"
fi
report_line "- deployed: ${deployedCsv}"

# --- 3. the declared state is actually declared -------------------------------------------
# THE substance of this plan. The play must have put every deployed UUID into the gsettings
# key the shell reads at session start; anything short of that is the defect Plan 00110 found.
check 3 "every declared extension is present in org.gnome.shell enabled-extensions"
enabledCsv=""
if ! enabledCsv="$(probe_marker ACC-ENABLED)"; then
    unknown "the enabled-extensions list could not be read" \
        "$(probe_marker ACC-ENABLED-UNREADABLE || printf 'no reason reported by the probe')"
else
    report_line "- enabled-extensions now holds: ${enabledCsv}"
    absent=()
    for uuid in "${DECLARED_UUIDS[@]}"; do
        case ",${enabledCsv}," in
            *",${uuid},"*) ;;
            *) absent+=("${uuid}") ;;
        esac
    done
    if [[ "${#absent[@]}" -eq 0 ]]; then
        ok "all ${DECLARED_COUNT} declared UUID(s) are in the list"
    else
        bad "${#absent[@]} declared UUID(s) missing from enabled-extensions: $(join_csv "${absent[@]}")" \
            "the play did not declare them — run deploy.bash and read the 'Declare Deployed Extensions Enabled' task"
    fi
fi

# --- 4. the master switch is not defeating all of it --------------------------------------
# disable-user-extensions true turns every user extension off whatever the list holds, so a
# green check [3] over a true here would be a pass on a session running none of them.
check 4 "disable-user-extensions is false"
disableValue=""
if ! disableValue="$(probe_marker ACC-DISABLE)"; then
    unknown "disable-user-extensions could not be read" \
        "$(probe_marker ACC-DISABLE-UNREADABLE || printf 'no reason reported by the probe')"
elif [[ "${disableValue}" == "false" ]]; then
    ok "disable-user-extensions is false, so the list above is honoured"
else
    bad "disable-user-extensions is ${disableValue}" \
        "GNOME runs NO user extension while this is true, whatever enabled-extensions holds: gsettings set org.gnome.shell disable-user-extensions false"
fi

# --- 5. the deploy removed nothing --------------------------------------------------------
# Additivity is a claim about a TRANSITION, so it needs two recorded states with the deploy
# between them — Task 2.1's before/after triage pair. Asserting it from the current list alone
# would be asserting it from no evidence at all.
check 5 "the deploy removed nothing from the enabled list (before/after triage evidence)"
deployStamps=()
triageStamps=()
if [[ -d "${RUNS_ROOT}/deploy" ]]; then
    mapfile -t deployStamps < <(find "${RUNS_ROOT}/deploy" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
fi
if [[ -d "${RUNS_ROOT}/triage" ]]; then
    mapfile -t triageStamps < <(find "${RUNS_ROOT}/triage" -mindepth 1 -maxdepth 1 -type d -printf '%f\n' | sort)
fi
runSequenceHint="run, in order: ./triage.bash, ./deploy.bash, ./deploy.bash, ./triage.bash — then this gate. Runs are looked for under ${RUNS_ROOT}"
if [[ "${#deployStamps[@]}" -eq 0 ]] || [[ "${#triageStamps[@]}" -lt 2 ]]; then
    unknown "no before/after triage pair bracketing a deploy run" "${runSequenceHint}"
else
    deployStamp="${deployStamps[${#deployStamps[@]} - 1]}"
    beforeStamp=""
    afterStamp=""
    for stamp in "${triageStamps[@]}"; do
        if [[ "${stamp}" < "${deployStamp}" ]]; then
            beforeStamp="${stamp}"
        elif [[ -z "${afterStamp}" ]]; then
            afterStamp="${stamp}"
        fi
    done
    if [[ -z "${beforeStamp}" ]] || [[ -z "${afterStamp}" ]]; then
        unknown "the triage runs do not bracket the deploy run ${deployStamp}" "${runSequenceHint}"
    else
        beforeReport=""
        afterReport=""
        beforeReport="$(find "${RUNS_ROOT}/triage/${beforeStamp}" -maxdepth 1 -type f -name '*report*' | sort | head -n 1)"
        afterReport="$(find "${RUNS_ROOT}/triage/${afterStamp}" -maxdepth 1 -type f -name '*report*' | sort | head -n 1)"
        beforeLine=""
        afterLine=""
        if [[ -z "${beforeReport}" ]] || [[ -z "${afterReport}" ]]; then
            unknown "a triage run directory holds no report file" \
                "before=${beforeStamp} after=${afterStamp} under ${RUNS_ROOT}/triage"
        elif ! beforeLine="$(grep -F -m 1 'org.gnome.shell enabled-extensions: ' "${beforeReport}")" ||
            ! afterLine="$(grep -F -m 1 'org.gnome.shell enabled-extensions: ' "${afterReport}")"; then
            unknown "a triage report does not record the enabled-extensions list" \
                "read ${beforeReport} and ${afterReport}"
        else
            beforeValue="${beforeLine#*enabled-extensions: }"
            afterValue="${afterLine#*enabled-extensions: }"
            if [[ "${beforeValue}" == UNREADABLE* ]] || [[ "${afterValue}" == UNREADABLE* ]]; then
                unknown "a triage run could not read the list itself" \
                    "before: ${beforeValue} / after: ${afterValue}"
            else
                # Both sides are parsed by the same GVariant parser the applier uses, so a
                # value this gate cannot read is reported rather than silently compared as
                # text — a text comparison would call a reordered list a removal.
                diffOut=""
                diffStatus=0
                diffOut="$(python3 -c '
import sys

sys.path.insert(0, sys.argv[1])

from helpers.gnome import enabled_extensions as ee

before = ee.parse_string_list(sys.argv[2])
after = ee.parse_string_list(sys.argv[3])
declared = set(sys.argv[4].split(",")) if sys.argv[4] else set()

after_set = set(after)
print("REMOVED " + ",".join(uuid for uuid in before if uuid not in after_set))
print("BEFORE-COUNT " + str(len(before)))
print("BEFORE-NOT-DECLARED " + str(len([uuid for uuid in before if uuid not in declared])))
' "${PLAN_REPO_ROOT}" "${beforeValue}" "${afterValue}" "${DECLARED_CSV}" 2>&1)" || diffStatus=$?
                if [[ "${diffStatus}" -ne 0 ]]; then
                    unknown "the two recorded lists could not be compared (exit ${diffStatus})" "${diffOut}"
                else
                    removed="$(grep -E '^REMOVED ' <<<"${diffOut}")"
                    removed="${removed#REMOVED }"
                    beforeCount="$(grep -E '^BEFORE-COUNT ' <<<"${diffOut}")"
                    beforeCount="${beforeCount#BEFORE-COUNT }"
                    notDeclared="$(grep -E '^BEFORE-NOT-DECLARED ' <<<"${diffOut}")"
                    notDeclared="${notDeclared#BEFORE-NOT-DECLARED }"
                    report_line "- before (${beforeStamp}): ${beforeValue}"
                    report_line "- after  (${afterStamp}): ${afterValue}"
                    if [[ -n "${removed}" ]]; then
                        bad "the deploy REMOVED ${removed} from enabled-extensions" \
                            "the enable path must be additive; compare ${beforeReport} with ${afterReport}"
                    else
                        ok "all ${beforeCount} pre-existing UUID(s) survived the deploy (${notDeclared} of them not declared by this repo)"
                    fi
                fi
            fi
        fi
    fi
fi

# --- 6. re-running the play changes nothing -----------------------------------------------
# Proven WITHOUT running the play, by asking the applier's own merge what it would do to the
# live value: it prints GNOME-EXT-ENABLED-UNCHANGED exactly when merge reports no change, and
# that is what makes the task report ok rather than changed. Running the play here would make
# an acceptance gate mutate the machine it is judging.
check 6 "re-running the play would report no change (idempotent declared state)"
mergeChanged=""
if ! mergeChanged="$(probe_marker ACC-MERGE-CHANGED)"; then
    unknown "the merge could not be computed" \
        "$(probe_marker ACC-ENABLED-UNREADABLE || printf 'the enabled-extensions list was not readable')"
elif [[ "${mergeChanged}" == "no" ]]; then
    ok "the applier's merge over the live list reports no change"
else
    bad "a re-run would still change the list, adding: $(probe_marker ACC-MERGE-WOULD-ADD || printf 'unreported')" \
        "the declared state has not landed — run deploy.bash and check [3] above"
fi

# --- 7 and 8. the live state, judged by the play's own verifier ---------------------------
# helpers.gnome.verify_extension is the gate the play itself runs, so [7] is that gate's
# verdict on this host. [8] is the stronger question it deliberately does NOT answer: its OK
# verdict covers any state that is not ERROR or a genuine version mismatch, so INITIALIZED and
# INACTIVE both pass it — and "installed, loaded, not enabled" is precisely the defect this
# plan exists to remove. The state is read out of the verifier's own message rather than by a
# second query, so the two cannot disagree about which session they looked at.
check 7 "the play's own verifier fails no declared extension"
verifyFailed=()
verifyActive=()
verifyNotActive=()
verifyPending=()
verifyNoSession=()
for uuid in "${DECLARED_UUIDS[@]}"; do
    verifyOut=""
    verifyStatus=0
    verifyOut="$(cd "${PLAN_REPO_ROOT}" && python3 -m helpers.gnome.verify_extension \
        --uuid "${uuid}" \
        --extensions-dir "${USER_EXTENSIONS_DIR}" \
        --extensions-dir "${SYSTEM_EXTENSIONS_DIR}" 2>&1)" || verifyStatus=$?
    report_line "- verify ${uuid}: ${verifyOut}"
    if [[ "${verifyStatus}" -ne 0 ]]; then
        verifyFailed+=("${uuid} (exit ${verifyStatus}: ${verifyOut})")
    elif [[ "${verifyOut}" != EXT-OK\ \[* ]]; then
        verifyFailed+=("${uuid} (unreadable verdict: ${verifyOut})")
    elif [[ "${verifyOut}" == *"[skip_no_session]"* ]]; then
        verifyNoSession+=("${uuid}")
    elif [[ "${verifyOut}" == *"[pending_scan]"* ]] || [[ "${verifyOut}" == *"[pending_reload]"* ]]; then
        verifyPending+=("${uuid}")
    elif [[ "${verifyOut}" == *"State ACTIVE"* ]]; then
        verifyActive+=("${uuid}")
    else
        verifyNotActive+=("${uuid}: ${verifyOut#EXT-OK }")
    fi
done

if [[ "${#verifyFailed[@]}" -ne 0 ]]; then
    bad "${#verifyFailed[@]} extension(s) failed verification" "$(
        IFS='; '
        printf '%s' "${verifyFailed[*]}"
    )"
elif [[ "${#verifyNoSession[@]}" -ne 0 ]]; then
    # "Nothing failed" is not a verdict when the verifier reached no session: skip_no_session
    # exits 0, so a pass here would be a pass over extensions nobody looked at. The play
    # tolerates that verdict by design; an acceptance gate must not count it.
    unknown "the verifier reached no session for ${#verifyNoSession[@]} of ${DECLARED_COUNT} extension(s), so it judged them not at all" \
        "run this gate from a terminal INSIDE your GNOME session"
else
    ok "all ${DECLARED_COUNT} declared extension(s) produced a readable, non-failing verdict"
fi

check 8 "every declared extension is ACTIVE in the live session"
if [[ "${#verifyNotActive[@]}" -ne 0 ]]; then
    bad "${#verifyNotActive[@]} extension(s) are known to the shell but not ACTIVE" "$(
        IFS='; '
        printf '%s' "${verifyNotActive[*]}"
    )"
elif [[ "${#verifyActive[@]}" -eq "${DECLARED_COUNT}" ]]; then
    ok "all ${DECLARED_COUNT} declared extension(s) report State ACTIVE"
elif [[ "${#verifyNoSession[@]}" -ne 0 ]]; then
    unknown "no GNOME session was reachable, so no live state was judged" \
        "run this gate from a terminal INSIDE your GNOME session (${#verifyNoSession[@]} of ${DECLARED_COUNT} extension(s) unjudged)"
else
    unknown "${#verifyPending[@]} of ${DECLARED_COUNT} extension(s) await a shell rescan or a Wayland reload" \
        "declared on disk and in the list, but this session has not loaded them: log out and back in (or reboot, as run.bash recommends) and re-run this gate — $(join_csv "${verifyPending[@]}")"
fi

# --- 9. the host's deployed VM-lab checker is current -------------------------------------
# Task 2.1's second half, and the host-side cause of the blocker recorded in Task 2.2: vmtest
# copies the guest checker from the HOST's DEPLOYED copy, not from the checkout, so a stale
# deployed copy makes a VM run's verdict a verdict about an older checker. That much IS
# establishable here, even though the VM run itself is not.
check 9 "the deployed VM-lab guest checker matches this repo's copy"
repoChecker="${PLAN_REPO_ROOT}/${GUEST_CHECKER_RELATIVE}"
deployedChecker="${HOME}/.local/share/vmtest/$(basename "${GUEST_CHECKER_RELATIVE}")"
if [[ ! -f "${repoChecker}" ]]; then
    bad "the repo copy is missing: ${repoChecker}" \
        "nothing can be compared against it — the checkout is incomplete or the file moved"
elif [[ ! -f "${deployedChecker}" ]]; then
    unknown "no deployed guest checker at ${deployedChecker}" \
        "either this machine does not host the VM lab, or the lab has never been deployed here: playbooks/imports/optional/common/play-vm-test-lab.yml"
elif cmp -s "${repoChecker}" "${deployedChecker}"; then
    ok "the deployed checker is byte-identical to ${GUEST_CHECKER_RELATIVE}"
else
    bad "the deployed guest checker has drifted from the repo copy" \
        "a vmtest run would judge with the OLD checker (Task 2.2's recorded blocker). Re-run playbooks/imports/optional/common/play-vm-test-lab.yml"
fi

render_verdict
