#!/usr/bin/env bash
# Plan 00124 — acceptance.bash
#
# THE VERDICT GATE. Unlike triage.bash, which gathers facts and renders none, this
# decides whether the Chrome signing-key fix is ACCEPTED on this host.
#
# It asserts OUTCOMES, not that files were edited:
#   1 Chrome and gnupg2 are installed
#   2 the rpm keyring holds Google's key, carrying the subkey the package is signed by
#   3 the deployed key file IS Google's published key, byte-for-byte
#   4 the repo definition is the one the play declares — read OUT OF the play
#   5 the play's own staleness verdict is `none`, so nothing is erased on a current host
#   6 the package the repo ships now verifies against this host's keyring
#   7 a check-mode run of the play reports no change for the key tasks
#
# IDEMPOTENCY IS SPLIT ON PURPOSE and check 7 is only half of it. Check mode cannot judge
# two of the tasks Task 4.2 is about: get_url issues a HEAD and compares the sha1 of that
# empty body against the file on disk, so it reports `changed` for a file that is already
# identical unless the server answers 304; and the erase is a `command:`, which check mode
# SKIPS, so a dry run cannot tell "the loop was empty" from "it did not run". Checks 3 and
# 5 judge those two from state — the same predicates the real tasks use — and deploy.bash
# runs the play a second time for real, which is the observation itself.
#
# WHAT IT DOES TO THIS HOST: nothing. It installs, removes and reconfigures nothing, and
# only check 7 needs root (the play runs with become even in check mode — and the play-run
# ledger deliberately records nothing for a --check run). It writes only into the run
# directory, and reaches the network for Google's published key and for the Chrome package
# check 6 verifies, which it deletes afterwards.
#
# WHERE TO RUN: on the HOST, in a terminal, from this checkout. Enforced by
# plan_require_host (R2) — the CCY container has no rpm keyring and no dnf, so every check
# would answer about the wrong machine, confidently.
#
# Usage: ./CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/acceptance.bash [-h|--help]
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

The Plan 00124 acceptance gate. Run deploy.bash first; this renders the verdict on
what that left behind, against the HOST.

It reads the host and the play, runs helpers.rpm_keys.subkeys, downloads the Chrome
package the repo currently ships (into the run directory, deleted once its signature
is checked) and runs the play once in --check mode, which needs sudo and changes
nothing. It changes nothing itself.

The verdict carries a COVERAGE line counting the declared checks against the ones
that actually ran, so a check that stops executing is visible rather than absorbed
into a lower pass count.

EXIT STATUS
  0  ACCEPTED — every declared check ran and every assertion passed
  1  REJECTED — an assertion failed, or a declared check never ran (an incomplete
     gate has not established what it claims to, even with zero failures)
  2  COULD NOT ESTABLISH — no assertion failed, but a check could not answer: a
     tool, the network or ansible itself did not let it. Not a verdict either way
 64  usage error"

plan_mode gather
plan_parse_common_flags "$@"

if [[ "${#PLAN_REMAINING_ARGS[@]}" -gt 0 ]]; then
    printf '[FATAL] unknown argument(s): %s\n' "${PLAN_REMAINING_ARGS[*]}" >&2
    printf '%s\n' "${PLAN_USAGE}" >&2
    exit 64
fi

plan_require_host "every check reads this host's rpm keyring, /etc/pki/rpm-gpg, /etc/yum.repos.d and dnf"

# Only check 7 needs root — the play runs with become even under --check. Primed here
# because R3 requires it BEFORE the run log opens; in gather mode an unprimed sudo is a
# warning, and check 7 then reports that it could not establish its fact.
plan_prime_sudo
plan_start_log auto

# ── what the play declares, and what this plan is about ──────────────────────────────────

readonly PLAY_REL="playbooks/imports/play-browsers.yml"
readonly PLAY_PATH="${PLAN_REPO_ROOT}/${PLAY_REL}"
readonly LOCAL_KEY="/etc/pki/rpm-gpg/RPM-GPG-KEY-google-chrome"
readonly PUBLISHED_URL="https://dl.google.com/linux/linux_signing_key.pub"
readonly REPO_FILE="/etc/yum.repos.d/google-chrome.repo"
readonly DEFAULTS_FILE="/etc/default/google-chrome"
readonly REPO_ID="google-chrome"
readonly CHROME_PACKAGE="google-chrome-stable"

# The two ids PLAN.md establishes (Tasks 1.2 and 1.3): Google's published primary, and the
# subkey the current Chrome package is signed by. No fingerprint is invented here.
readonly PRIMARY_KEY_ID="7721F63BD38B4796"
readonly SIGNING_SUBKEY_ID="FD533C07C264648F"
# rpm names a gpg-pubkey package after the last 8 hex digits of the primary, lowercased.
# Derived rather than spelled out again so the two cannot drift — the same derivation
# helpers/rpm_keys/subkeys.py makes.
_shortId="${PRIMARY_KEY_ID: -8}"
readonly ENVELOPE_PREFIX="gpg-pubkey-${_shortId,,}-"

readonly REPORT="${PLAN_RUN_DIR}/plan-00124-acceptance-report.md"

# The tasks check 7 reads out of the check-mode run. SOUND_TASKS are the ones whose
# check-mode verdict means what it says; the other three are reported as information and
# judged by checks 3 and 5 instead (see the header).
CHECK_RUN_TASKS=(
    "Ensure gnupg2 is installed"
    "Fetch Google's Published Signing Key"
    "Decide Whether The Imported Google Key Is Stale"
    "Remove The Stale Google Signing Key"
    "Import Google Chrome Signing Key"
    "Verify The Imported Google Key Is Now Current"
    "Stop Chrome's Scriptlet Re-Adding Its Own Repository"
    "Add Google Chrome Repository"
    "Install Google Chrome"
)
SOUND_TASKS=(
    "Ensure gnupg2 is installed"
    "Import Google Chrome Signing Key"
    "Stop Chrome's Scriptlet Re-Adding Its Own Repository"
    "Add Google Chrome Repository"
    "Install Google Chrome"
)
UNSOUND_TASKS=(
    "Fetch Google's Published Signing Key"
    "Remove The Stale Google Signing Key"
    "Verify The Imported Google Key Is Now Current"
)

# ── the verdict machinery ────────────────────────────────────────────────────────────────

# COVERAGE is stated, not inferred. The PASS count cannot carry it — checks emit different
# numbers of assertions and several are conditional — so "8 passed" reads identically
# whether 8 of 8 checks ran or 8 of 12. A check that is deleted, renumbered or skipped by
# an early exit disappears from RAN_CHECKS and is NAMED in the verdict.
EXPECTED_CHECKS=(0 1 2 3 4 5 6 7)
RAN_CHECKS=()
PASS=0
FAIL=0
UNKNOWN=0
# Every line the report file gets, in order.
RESULTS=()

# check <id> <title> — announce a check AND record that it ran. Every numbered section
# starts here; one that prints its own header instead is invisible to COVERAGE.
check() {
    RAN_CHECKS+=("$1")
    printf '\n[%s] %s\n' "$1" "$2"
    RESULTS+=("" "## [$1] $2" "")
    return 0
}

ok() {
    printf '  PASS  %s\n' "$1"
    RESULTS+=("- PASS  $1")
    PASS=$((PASS + 1))
    return 0
}

# bad <what> [detail] — a definitive failure: this host does not hold what the plan says
# it should. Drives REJECTED.
bad() {
    printf '  FAIL  %s\n' "$1" >&2
    RESULTS+=("- FAIL  $1")
    if [[ -n "${2:-}" ]]; then
        printf '        %s\n' "$2" >&2
        RESULTS+=("  - ${2}")
    fi
    FAIL=$((FAIL + 1))
    return 0
}

# cannot <what> [detail] — the check did not answer: a missing tool, a refused network, an
# ansible that would not start. NOT a failure and NOT a pass — it drives exit 2, so a gate
# that could not look is never mistaken for one that looked and was satisfied.
cannot() {
    printf '  UNKN  %s\n' "$1" >&2
    RESULTS+=("- UNKN  $1")
    if [[ -n "${2:-}" ]]; then
        printf '        %s\n' "$2" >&2
        RESULTS+=("  - ${2}")
    fi
    UNKNOWN=$((UNKNOWN + 1))
    return 0
}

# note <text> — context that carries no verdict weight.
note() {
    printf '  note  %s\n' "$1"
    RESULTS+=("- note  $1")
    return 0
}

# finish_verdict — the single exit. Called from the early stop in check 0 and from the end
# of the run, so every way out states what was and was not established. Defined before its
# first call, because a function bash has not yet executed the definition of does not exist.
#
# STANDARD-EXCEPTION(R9): it does not end with plan_finish, which can only exit 0 or 1. An
# acceptance gate has a THIRD answer — "could not establish" — and folding that into either
# of the other two is the confident-wrong-answer failure these standards exist to prevent.
# plan_list_reports is still called, so R10 holds.
finish_verdict() {
    local expected="" line="" missing=()

    for expected in "${EXPECTED_CHECKS[@]}"; do
        case " ${RAN_CHECKS[*]} " in
            *" ${expected} "*) ;;
            *) missing+=("${expected}") ;;
        esac
    done

    {
        printf '# Plan 00124 — acceptance verdict\n\n'
        printf 'Generated on the HOST by acceptance.bash. Run directory: %s\n\n' "${PLAN_RUN_DIR}"
        printf -- '- checks executed: %s of %s\n' "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}"
        printf -- '- assertions: %s passed, %s failed, %s unestablished\n' "${PASS}" "${FAIL}" "${UNKNOWN}"
        if [[ "${#missing[@]}" -ne 0 ]]; then
            printf -- '- NOT RUN: %s\n' "${missing[*]}"
        fi
        for line in "${RESULTS[@]}"; do
            printf '%s\n' "${line}"
        done
    } >"${REPORT}"

    plan_list_reports

    printf '\n==============================================================\n'
    printf 'COVERAGE: %s of %s checks executed (%s assertion(s) passed, %s failed, %s unestablished)\n' \
        "${#RAN_CHECKS[@]}" "${#EXPECTED_CHECKS[@]}" "${PASS}" "${FAIL}" "${UNKNOWN}"
    if [[ "${#missing[@]}" -ne 0 ]]; then
        printf '  NOT RUN: %s\n' "${missing[*]}" >&2
    fi

    if [[ "${FAIL}" -ne 0 ]]; then
        printf 'REJECTED — %s assertion(s) failed, %s passed.\n' "${FAIL}" "${PASS}" >&2
        printf '  Fix the play, run deploy.bash, and re-run this gate.\n' >&2
        printf '==============================================================\n'
        exit 1
    fi
    if [[ "${UNKNOWN}" -ne 0 ]]; then
        printf 'COULD NOT ESTABLISH — no assertion failed, but %s question(s) went unanswered.\n' "${UNKNOWN}" >&2
        printf '  This is NOT an acceptance. Resolve what the UNKN lines name, then re-run.\n' >&2
        printf '==============================================================\n'
        exit 2
    fi
    if [[ "${#missing[@]}" -ne 0 ]]; then
        printf 'REJECTED — no assertion failed, but %s declared check(s) never ran.\n' "${#missing[@]}" >&2
        printf '  A gate that did not run all of its checks has not established what it claims.\n' >&2
        printf '==============================================================\n'
        exit 1
    fi
    printf 'ACCEPTED — every declared check ran and every assertion passed.\n'
    printf '  Plan 00124 Task 4.2 is evidenced on this host; issue #45 can be closed.\n'
    printf '==============================================================\n'
    exit 0
}

# ── reading keys, the play, and the repo file ────────────────────────────────────────────

# read_key_file <path> — set KEY_PRIMARY and KEY_SUBKEYS from an OpenPGP file. Globals
# rather than stdout because both come from ONE gpg parse and the caller needs both; the
# failure reason goes to stderr, which is where a diagnostic belongs.
#
# Parsing STOPS at a second certificate, exactly as helpers/rpm_keys/subkeys.py does: a
# two-certificate bundle — what a vendor rotating a primary ships — would otherwise pair
# the first primary with every certificate's subkeys, and Plan 00124 Task 3b.1 records what
# that cost: a refresh, erase and re-import for ever.
KEY_PRIMARY=""
KEY_SUBKEYS=""
read_key_file() {
    local path="$1" colons="" parsed="" fields=()
    KEY_PRIMARY=""
    KEY_SUBKEYS=""
    if [[ ! -r "${path}" ]]; then
        printf 'not readable: %s\n' "${path}" >&2
        return 1
    fi
    if ! colons="$(gpg --show-keys --with-colons "${path}" 2>&1)"; then
        printf 'gpg could not read %s: %s\n' "${path}" "${colons}" >&2
        return 1
    fi
    parsed="$(printf '%s\n' "${colons}" | awk -F: '
        $1 == "pub" { seen++ }
        seen > 1 { exit }
        $1 == "pub" { primary = $5 }
        $1 == "sub" { subs = subs " " $5 }
        END { print primary subs }')"
    read -r -a fields <<<"${parsed}"
    KEY_PRIMARY="${fields[0]:-}"
    KEY_SUBKEYS="${fields[*]:1}"
    if [[ -z "${KEY_PRIMARY}" ]]; then
        printf 'gpg read %s but reported no primary key, so it holds no key this can identify\n' "${path}" >&2
        return 1
    fi
    return 0
}

# carries_signing_subkey — does the key last read hold the subkey the package is signed by?
# The list is delimited on both sides so a substring cannot pass for an id.
carries_signing_subkey() {
    case " ${KEY_SUBKEYS} " in
        *" ${SIGNING_SUBKEY_ID} "*) return 0 ;;
    esac
    return 1
}

# envelope_armour <gpg-pubkey-package> <dest> — the armoured key rpm keeps as a gpg-pubkey
# package's description, written where gpg can read it.
envelope_armour() {
    local envelope="$1" dest="$2" armour=""
    if ! armour="$(rpm -q "${envelope}" --qf '%{description}' 2>&1)"; then
        printf 'rpm could not read the description of %s: %s\n' "${envelope}" "${armour}" >&2
        return 1
    fi
    if [[ -z "${armour//[[:space:]]/}" ]]; then
        printf '%s has an EMPTY description, so there is no key armour to read\n' "${envelope}" >&2
        return 1
    fi
    printf '%s\n' "${armour}" >"${dest}"
    return 0
}

# fetch_url <url> <dest> — one HTTPS GET through the same library ansible's get_url uses,
# streamed, because check 6 fetches a package of some size.
fetch_url() {
    python3 - "$1" "$2" <<'PYTHON'
import shutil
import sys
import urllib.request

url, dest = sys.argv[1], sys.argv[2]
with urllib.request.urlopen(url, timeout=120) as response, open(dest, "wb") as handle:
    shutil.copyfileobj(response, handle)
PYTHON
}

# remove_downloaded_package — delete the package check 6 fetched. Registered as a cleanup
# below as well as called on the normal path, so a run interrupted mid-download does not
# leave a whole Chrome package sitting in the run directory. Safe to call twice.
DOWNLOADED_PACKAGE=""
remove_downloaded_package() {
    if [[ -n "${DOWNLOADED_PACKAGE}" ]] && [[ -e "${DOWNLOADED_PACKAGE}" ]]; then
        printf '  ....  deleting the package check 6 downloaded: %s\n' "${DOWNLOADED_PACKAGE}"
        rm -f "${DOWNLOADED_PACKAGE}"
    fi
    return 0
}
plan_on_cleanup remove_downloaded_package

# play_lines — the play with its COMMENT lines removed. A comment mentioning a setting is
# not a declaration of it: this play's prose names repo_add_once="true" while the task
# writes "false", and a gate that read the prose would compare the host with the wrong one.
play_lines() {
    awk '!/^[[:space:]]*#/' "${PLAY_PATH}"
}

# play_declares <key> — the single scalar the play declares for `<key>:`, on stdout. Read
# out of the play rather than restated here, so this gate cannot assert a repo definition
# the play has stopped declaring. More than one match makes the question ambiguous, which
# is reported rather than guessed at.
play_declares() {
    local key="$1" matches="" count=0
    if ! matches="$(play_lines | grep -E "^[[:space:]]*${key}:[[:space:]]+[^[:space:]]")"; then
        printf '%s declares no "%s:"\n' "${PLAY_REL}" "${key}" >&2
        return 1
    fi
    count="$(printf '%s\n' "${matches}" | awk 'END { print NR }')"
    if [[ "${count}" -ne 1 ]]; then
        printf '%s declares "%s:" %s times (%s), so which governs the Chrome repo cannot be read from here\n' \
            "${PLAY_REL}" "${key}" "${count}" "$(printf '%s' "${matches}" | tr '\n' ' ')" >&2
        return 1
    fi
    printf '%s' "${matches}" | awk '{ sub(/^[[:space:]]*[^[:space:]]+:[[:space:]]*/, ""); print }'
}

# play_repo_add_once — the repo_add_once assignment the play WRITES, as a whole token.
play_repo_add_once() {
    local matches="" count=0
    if ! matches="$(play_lines | grep -oE 'repo_add_once="[a-z]+"')"; then
        printf '%s declares no repo_add_once value\n' "${PLAY_REL}" >&2
        return 1
    fi
    count="$(printf '%s\n' "${matches}" | awk 'END { print NR }')"
    if [[ "${count}" -ne 1 ]]; then
        printf '%s declares %s repo_add_once values (%s)\n' \
            "${PLAY_REL}" "${count}" "$(printf '%s' "${matches}" | tr '\n' ' ')" >&2
        return 1
    fi
    printf '%s' "${matches}"
}

# repo_value <file> <key> — the value of an ini key, with the spaces ansible's
# yum_repository writes around the `=` ignored.
repo_value() {
    awk -F= -v wanted="$2" '
        /^[[:space:]]*[#;]/ { next }
        index($0, "=") == 0 { next }
        {
            key = substr($0, 1, index($0, "=") - 1)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
            if (key != wanted) next
            value = substr($0, index($0, "=") + 1)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
            print value
        }' "$1"
}

# parse_check_run <json> <task>... — the per-task verdicts out of a json-callback run.
# Emits STATE|<task>|<ok|changed|skipped|failed|absent> for each task named, DECIDE|<line>
# for the decision task's own marker lines, and LATER|<task>|<host> for any OTHER task that
# failed: a later unrelated failure is context, not this plan's verdict.
parse_check_run() {
    python3 - "$@" <<'PYTHON'
import json
import sys

path, *wanted = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    document = json.load(handle)

RANK = {"ok": 0, "skipped": 1, "changed": 2, "failed": 3}


def state_of(result):
    """The one word for a task result, taking the worst item of a looped one."""
    items = result.get("results")
    if isinstance(items, list) and items:
        return max((state_of(item) for item in items), key=lambda state: RANK[state])
    if result.get("failed"):
        return "failed"
    if result.get("skipped"):
        return "skipped"
    if result.get("changed"):
        return "changed"
    return "ok"


seen = {}
for play in document.get("plays", []):
    for task in play.get("tasks", []):
        name = task.get("task", {}).get("name", "")
        for host, result in (task.get("hosts") or {}).items():
            state = state_of(result)
            if name not in wanted:
                if state == "failed":
                    print(f"LATER|{name}|{host}")
                continue
            previous = seen.get(name)
            if previous is None or RANK[state] > RANK[previous]:
                seen[name] = state
            for line in result.get("stdout_lines") or []:
                if line.startswith("RPM-KEY-"):
                    print(f"DECIDE|{line}")

for name in wanted:
    print(f"STATE|{name}|{seen.get(name, 'absent')}")
PYTHON
}

# state_of_task <task> <summary> — one task's verdict out of parse_check_run's output.
state_of_task() {
    printf '%s\n' "$2" | awk -F'|' -v wanted="$1" '$1 == "STATE" && $2 == wanted { print $3 }'
}

# ── 0. preconditions: what this gate reads the host and the play with ────────────────────

check 0 "preconditions: the tools this gate reads the host with, and the play it reads"
missingTools=()
for tool in rpm gpg dnf python3 sha256sum ansible-playbook; do
    if ! command -v "${tool}" >/dev/null; then
        missingTools+=("${tool}")
    fi
done
if [[ "${#missingTools[@]}" -ne 0 ]]; then
    cannot "these tools are absent, so this gate can establish nothing: ${missingTools[*]}" \
        "A missing tool is an IaC gap, not something to work around (CLAUDE.md, Missing Dependencies). gpg comes from gnupg2, which ${PLAY_REL} declares. Do NOT install any of them by hand."
else
    ok "rpm, gpg, dnf, python3, sha256sum and ansible-playbook are all present"
fi
if [[ ! -r "${PLAY_PATH}" ]]; then
    cannot "${PLAY_REL} is not readable, so what the play declares cannot be compared with the host"
else
    ok "${PLAY_REL} is readable, so checks 4 and 7 can read what it declares"
fi

# Nothing below can answer anything without those, and a gate that runs on regardless
# produces a page of failures that all say "no rpm". Stopping here leaves checks 1-7 out of
# RAN_CHECKS, and the verdict names them rather than hiding the incompleteness.
if [[ "${UNKNOWN}" -ne 0 ]]; then
    printf '\n  Stopping: every check below reads the host through what is missing above.\n' >&2
    finish_verdict
fi

# ── 1. the end state this plan exists to reach ───────────────────────────────────────────

check 1 "Chrome is installed, and so is the gnupg2 both the key check and rpm_key need"
for package in "${CHROME_PACKAGE}" gnupg2; do
    queried=""
    if queried="$(rpm -q "${package}" 2>&1)"; then
        ok "${queried} is installed"
    else
        bad "${package} is NOT installed" "${queried}"
    fi
done

# ── 2. the rpm keyring holds Google's key, carrying the signer ───────────────────────────

check 2 "rpm -qa gpg-pubkey holds Google's primary ${PRIMARY_KEY_ID}, carrying subkey ${SIGNING_SUBKEY_ID}"
keyring=""
if ! keyring="$(rpm -qa gpg-pubkey --qf '%{name}-%{version}-%{release}\n' 2>&1)"; then
    cannot "the rpm keyring could not be listed" "${keyring}"
else
    envelopes=()
    while IFS= read -r line; do
        # Anchored on the trailing separator, as the helper is: a prefix match would claim
        # a key nobody asked about.
        case "${line}" in
            "${ENVELOPE_PREFIX}"*) envelopes+=("${line}") ;;
        esac
    done <<<"${keyring}"

    if [[ "${#envelopes[@]}" -eq 0 ]]; then
        bad "no gpg-pubkey package named ${ENVELOPE_PREFIX}* is installed" \
            "This keyring holds no copy of Google primary ${PRIMARY_KEY_ID}, so dnf cannot verify a Chrome package at all. Run deploy.bash."
    else
        note "installed key packages at this short id: ${envelopes[*]}"
        oursTotal=0
        unreadable=0
        for envelope in "${envelopes[@]}"; do
            armourPath="${PLAN_RUN_DIR}/installed-${envelope}.asc"
            if ! envelope_armour "${envelope}" "${armourPath}"; then
                cannot "the armour of ${envelope} could not be read, so its identity is unknown"
                unreadable=$((unreadable + 1))
                continue
            fi
            if ! read_key_file "${armourPath}"; then
                cannot "gpg could not identify ${envelope}"
                unreadable=$((unreadable + 1))
                continue
            fi
            if [[ "${KEY_PRIMARY}" != "${PRIMARY_KEY_ID}" ]]; then
                # Short ids are 8 hex digits and are not unique. A package sitting here
                # with a different primary belongs to somebody else — it is not evidence
                # for this plan, and it is not ours to remove either.
                note "${envelope} carries primary ${KEY_PRIMARY}, which is NOT Google's — somebody else's key, left alone"
                continue
            fi
            oursTotal=$((oursTotal + 1))
            if carries_signing_subkey; then
                ok "${envelope} carries Google's primary and signing subkey ${SIGNING_SUBKEY_ID}"
            else
                bad "${envelope} carries Google's primary but NOT subkey ${SIGNING_SUBKEY_ID}" \
                    "This is the original defect: rpm and dnf call the key present by its primary id while the subkey the package is signed by is absent. Subkeys found: ${KEY_SUBKEYS:-(none)}"
            fi
        done
        if [[ "${oursTotal}" -eq 0 ]] && [[ "${unreadable}" -eq 0 ]]; then
            bad "nothing installed at this short id carries Google's primary ${PRIMARY_KEY_ID}" \
                "The packages found belong to other vendors, so this host holds no Google key."
        fi
    fi
fi

# ── 3. the deployed key file IS what Google publishes ────────────────────────────────────

check 3 "${LOCAL_KEY} is Google's published key, byte-for-byte (so the fetch task reports ok)"
statOut=""
if statOut="$(stat -c '%n mode %a owner %U:%G size %s' "${LOCAL_KEY}" 2>&1)"; then
    note "${statOut}"
    case "${statOut}" in
        *"mode 644 owner root:root"*)
            ok "the key file is 0644 root:root, as the fetch task declares"
            ;;
        *)
            bad "the key file is not 0644 root:root, so the fetch task will report changed" "${statOut}"
            ;;
    esac
else
    bad "${LOCAL_KEY} is absent — the play has not fetched Google's key" "${statOut}"
fi

if ! read_key_file "${LOCAL_KEY}"; then
    bad "${LOCAL_KEY} holds no key gpg can identify"
elif [[ "${KEY_PRIMARY}" != "${PRIMARY_KEY_ID}" ]]; then
    bad "${LOCAL_KEY} carries primary ${KEY_PRIMARY}, not Google's ${PRIMARY_KEY_ID}"
elif ! carries_signing_subkey; then
    bad "${LOCAL_KEY} does not carry signing subkey ${SIGNING_SUBKEY_ID}" \
        "dnf validates the repo against this file, so it could not verify the package. Subkeys: ${KEY_SUBKEYS:-(none)}"
else
    ok "the key file carries Google's primary and signing subkey ${SIGNING_SUBKEY_ID}"
fi

publishedPath="${PLAN_RUN_DIR}/published-linux_signing_key.pub"
fetchOut=""
localSum=""
publishedSum=""
if ! fetchOut="$(fetch_url "${PUBLISHED_URL}" "${publishedPath}" 2>&1)"; then
    cannot "Google's published key could not be fetched, so the on-host copy was not compared with it" \
        "${fetchOut}"
elif ! localSum="$(sha256sum "${LOCAL_KEY}" 2>&1)"; then
    cannot "the on-host key file could not be checksummed" "${localSum}"
elif ! publishedSum="$(sha256sum "${publishedPath}" 2>&1)"; then
    cannot "the fetched published key could not be checksummed" "${publishedSum}"
elif [[ "${localSum%% *}" == "${publishedSum%% *}" ]]; then
    ok "the key file is byte-identical to what Google publishes now (${localSum%% *})"
else
    # Not a failure of the fix. get_url has no checksum: to short-circuit on, so it issues a
    # conditional GET; a 200 replaces the file and reports changed, which is the mechanism
    # working. It does mean the next run is not the no-change run Task 4.2 asks about, so
    # idempotency cannot be called established from here.
    cannot "the key file DIFFERS from what Google publishes now, so the fetch task will report changed on the next run" \
        "on host ${localSum%% *} / published ${publishedSum%% *}. Google has added subkeys before and will again. Re-run deploy.bash, then re-run this gate."
fi

# ── 4. the repo definition is the one the play declares ──────────────────────────────────

check 4 "the Chrome repo definition on this host is the one ${PLAY_REL} declares"
expectedBaseUrl=""
expectedGpgKey=""
declareErr=""
if ! expectedBaseUrl="$(play_declares baseurl 2>&1)"; then
    declareErr="${expectedBaseUrl}"
    expectedBaseUrl=""
elif ! expectedGpgKey="$(play_declares gpgkey 2>&1)"; then
    declareErr="${expectedGpgKey}"
    expectedGpgKey=""
fi
if [[ -n "${declareErr}" ]]; then
    cannot "what the play declares for the repo could not be read, so the host was not compared with it" \
        "${declareErr}"
elif [[ ! -r "${REPO_FILE}" ]]; then
    bad "${REPO_FILE} does not exist — dnf has no ${REPO_ID} repo to install from" \
        "That is cause 1 of issue #45: without a repo, a package handed to dnf by URL lands in @commandline, which has no keys configured."
else
    note "the play declares baseurl ${expectedBaseUrl} and gpgkey ${expectedGpgKey}"
    sections=""
    sections="$(awk '/^\[/ { n++ } END { print n + 0 }' "${REPO_FILE}")"
    if [[ "${sections}" != "1" ]]; then
        note "${REPO_FILE} declares ${sections} repo sections; the values below are read from the whole file"
    fi
    if grep -qE "^\[${REPO_ID}\]" "${REPO_FILE}"; then
        ok "${REPO_FILE} declares [${REPO_ID}]"
    else
        bad "${REPO_FILE} declares no [${REPO_ID}] section"
    fi
    for pair in "baseurl|${expectedBaseUrl}" "gpgkey|${expectedGpgKey}" "gpgcheck|1" "enabled|1"; do
        field="${pair%%|*}"
        expected="${pair#*|}"
        actual=""
        actual="$(repo_value "${REPO_FILE}" "${field}")"
        if [[ "${actual}" == "${expected}" ]]; then
            ok "${field} = ${actual}"
        elif [[ -z "${actual}" ]]; then
            bad "${REPO_FILE} declares no ${field}" "expected ${expected}"
        else
            bad "${field} is ${actual}, not ${expected}" \
                "An https:// gpgkey is Chrome's own scriptlet rewriting this file, which makes the repo task report changed on every run and after every Chrome upgrade."
        fi
    done
fi

expectedDefault=""
if ! expectedDefault="$(play_repo_add_once 2>&1)"; then
    cannot "what the play writes to ${DEFAULTS_FILE} could not be read, so the host was not compared with it" \
        "${expectedDefault}"
elif [[ ! -r "${DEFAULTS_FILE}" ]]; then
    bad "${DEFAULTS_FILE} is absent, so Chrome's %post creates it reading true and rewrites the repo file" \
        "expected the play's ${expectedDefault}"
else
    actualDefault=""
    if ! actualDefault="$(grep -oE 'repo_add_once="[a-z]+"' "${DEFAULTS_FILE}")"; then
        bad "${DEFAULTS_FILE} holds no repo_add_once setting" "expected the play's ${expectedDefault}"
    elif [[ "${actualDefault}" == "${expectedDefault}" ]]; then
        ok "${DEFAULTS_FILE} holds ${actualDefault}, so Chrome's scriptlet leaves the repo file alone"
    else
        bad "${DEFAULTS_FILE} holds ${actualDefault}, not the play's ${expectedDefault}" \
            "While it reads true, Chrome's %post rewrites ${REPO_FILE} with a network gpgkey."
    fi
fi

# ── 5. the play's own staleness verdict ──────────────────────────────────────────────────

check 5 "the play's own decision is 'none' — nothing to erase, nothing to import"
decision=""
if ! decision="$(cd "${PLAN_REPO_ROOT}" && python3 -m helpers.rpm_keys.subkeys --published "${LOCAL_KEY}" 2>&1)"; then
    cannot "helpers.rpm_keys.subkeys could not reach a verdict" "${decision}"
else
    note "$(printf '%s' "${decision}" | tr '\n' ' ')"
    action=""
    action="$(printf '%s\n' "${decision}" | awk '$1 == "RPM-KEY-ACTION" { print $2 }')"
    envelopeLines=""
    envelopeLines="$(printf '%s\n' "${decision}" | awk '$1 == "RPM-KEY-ENVELOPE" { printf "%s ", $2 }')"
    case "${action}" in
        none)
            ok "RPM-KEY-ACTION none — the Remove task's loop is empty and rpm_key finds the key present, so both report ok"
            ;;
        refresh)
            bad "RPM-KEY-ACTION refresh — the installed key is still deficient" \
                "The next run would erase ${envelopeLines}and re-import. On a converged host this must read none; reading refresh after a deploy IS the erase-and-reimport-for-ever loop the play's post-condition exists to catch."
            ;;
        import)
            bad "RPM-KEY-ACTION import — no copy of Google's key is installed at all" "Run deploy.bash."
            ;;
        *)
            cannot "the helper printed no RPM-KEY-ACTION this gate understands" "${decision}"
            ;;
    esac
    if [[ -z "${envelopeLines}" ]]; then
        ok "the helper names no envelope to erase, so nothing is removed from a host whose key is already current"
    else
        bad "the helper names envelopes to erase: ${envelopeLines}"
    fi
fi

# ── 6. the package the repo ships now verifies against this host's keyring ───────────────

check 6 "the Chrome package the repo ships now passes rpm's signature check on this host"
repoQuery=""
if ! repoQuery="$(dnf -q repoquery --queryformat '%{repoid} %{location}\n' "${CHROME_PACKAGE}" 2>&1)"; then
    cannot "dnf repoquery could not describe ${CHROME_PACKAGE}" "${repoQuery}"
elif ! printf '%s\n' "${repoQuery}" | grep -q "${REPO_ID}"; then
    bad "dnf does not offer ${CHROME_PACKAGE} from repo ${REPO_ID}" \
        "dnf said: ${repoQuery}. A package dnf only knows from @commandline is cause 1 of issue #45 — dnf5 validates against the keys configured on the package's repo, and @commandline has none."
else
    location=""
    location="$(printf '%s\n' "${repoQuery}" | awk -v repo="${REPO_ID}" '$1 == repo { print $2; exit }')"
    packageUrl="${location}"
    if [[ -n "${location}" ]] && [[ "${location}" != http* ]]; then
        packageUrl="${expectedBaseUrl%/}/${location#/}"
    fi
    if [[ -z "${location}" ]]; then
        cannot "dnf named repo ${REPO_ID} but no package location could be read out of its answer" \
            "dnf said: ${repoQuery}"
    elif [[ "${packageUrl}" != http* ]]; then
        cannot "the package location ${location} is relative and check 4 could not read the play's baseurl to join it to"
    else
        ok "dnf offers ${CHROME_PACKAGE} from repo ${REPO_ID}"
        packagePath="${PLAN_RUN_DIR}/$(basename "${packageUrl}")"
        # Named BEFORE the fetch, so the registered cleanup can delete a partial download
        # if this run is interrupted.
        DOWNLOADED_PACKAGE="${packagePath}"
        note "fetching ${packageUrl} — the whole package, deleted again below"
        downloadOut=""
        if ! downloadOut="$(fetch_url "${packageUrl}" "${packagePath}" 2>&1)"; then
            cannot "the package could not be downloaded, so its signature was not checked" \
                "${packageUrl}: ${downloadOut}"
        else
            checksig=""
            if ! checksig="$(rpm --checksig --verbose "${packagePath}" 2>&1)"; then
                bad "rpm REFUSES the package the repo ships — this is the failure issue #45 reported" \
                    "$(printf '%s' "${checksig}" | tr '\n' ' ')"
            else
                case "${checksig}" in
                    *NOKEY* | *"NOT OK"*)
                        bad "rpm exited 0 but its report is not a clean verification" \
                            "$(printf '%s' "${checksig}" | tr '\n' ' ')"
                        ;;
                    *)
                        ok "rpm verifies the signatures on the package the repo ships, against this host's keyring"
                        note "$(printf '%s' "${checksig}" | tr '\n' ' ')"
                        ;;
                esac
            fi
            remove_downloaded_package
            note "the package file is gone again — the transcript above is the evidence, not the package"
        fi
    fi
fi

# ── 7. a check-mode run of the play reports no change for the key tasks ──────────────────

check 7 "a --check run of ${PLAY_REL} reports no change for the key tasks it can judge"
checkRunJson="${PLAN_RUN_DIR}/idempotency-check-run.json"
checkRunErr="${PLAN_RUN_DIR}/idempotency-check-run.stderr"
note "running ansible-playbook --check; this takes a few minutes and changes nothing"
checkRunRc=0
# The json callback is what makes per-task verdicts readable instead of grepped out of
# human output. It is a stdout callback, so ansible loads it INSTEAD of the default one
# ansible.cfg configures; play_ledger still loads and deliberately records no --check run.
if (
    export ANSIBLE_STDOUT_CALLBACK=ansible.posix.json
    plan_ansible_playbook "${PLAY_REL}" --check
) >"${checkRunJson}" 2>"${checkRunErr}"; then
    checkRunRc=0
else
    checkRunRc=$?
fi

summary=""
if ! summary="$(parse_check_run "${checkRunJson}" "${CHECK_RUN_TASKS[@]}" 2>&1)"; then
    cannot "the check-mode run produced nothing this gate could read (ansible exited ${checkRunRc})" \
        "parser said: ${summary} — ansible stderr: $(tr '\n' ' ' <"${checkRunErr}"). If the json callback is missing, install the declared collections: ansible-galaxy install -r requirements.yml"
else
    if [[ "${checkRunRc}" -ne 0 ]]; then
        # A later task failing under --check — a repo a dry run never added, say — is not a
        # verdict on the Chrome key tasks, which run first and are read one by one below.
        note "the check-mode run exited ${checkRunRc}; tasks that failed: $(printf '%s\n' "${summary}" | awk -F'|' '$1 == "LATER" { printf "%s; ", $2 }')"
    fi
    while IFS= read -r decideLine; do
        if [[ -n "${decideLine}" ]]; then
            note "the run's own decision: ${decideLine}"
        fi
    done < <(printf '%s\n' "${summary}" | awk -F'|' '$1 == "DECIDE" { print $2 }')
    for task in "${SOUND_TASKS[@]}"; do
        state=""
        state="$(state_of_task "${task}" "${summary}")"
        case "${state}" in
            ok)
                ok "${task}: ok"
                ;;
            changed)
                bad "${task}: changed — the play is NOT idempotent here" \
                    "A converged host must report ok. The run is in ${checkRunJson}."
                ;;
            absent)
                cannot "${task} did not run in the check-mode run, so its idempotency is unestablished" \
                    "See ${checkRunJson} and ${checkRunErr}."
                ;;
            *)
                cannot "${task}: ${state:-no verdict} in the check-mode run" "See ${checkRunJson}."
                ;;
        esac
    done
    for task in "${UNSOUND_TASKS[@]}"; do
        note "${task}: $(state_of_task "${task}" "${summary}") under --check, which is NOT a verdict — get_url compares a HEAD body, the erase is a command: check mode skips, and the verify task is gated on not ansible_check_mode. Checks 3 and 5 judge these."
    done
fi

finish_verdict
