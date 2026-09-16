#!/usr/bin/env bash
# Plan 00124 — probe-chrome.bash
#
# The probe bodies for triage.bash (CLAUDE/PlanTriage.md: the probes live IN the
# script, never in a transcript pasted into chat). It appends ONE section to the
# report file named as $1; which section is chosen by $2, so the orchestrator can
# run each as its own gather leg and a section that cannot answer fails only
# itself.
#
# Read-only with respect to this host: it installs nothing, removes nothing,
# reconfigures nothing, and needs no root. It writes only into the run directory
# (copies of the key files it compares), and reaches the network only for the one
# HTTPS GET of the published Google key that section "key" compares against.
#
# Usage: probe-chrome.bash <report-file> <checkout|key|host>
#
# EXIT STATUS
#   0  the section answered every decisive question
#   1  a decisive question went unanswered. The FACT-FINDING is incomplete — it
#      does not mean the host is broken. The report names what was not established.
#  64  usage error
set -euo pipefail

readonly report="${1:?usage: probe-chrome.bash <report-file> <checkout|key|host>}"
readonly section="${2:?usage: probe-chrome.bash <report-file> <checkout|key|host>}"
readonly repoRoot="${PLAN_REPO_ROOT:?probe-chrome.bash must be run from triage.bash}"
readonly runDir="${PLAN_RUN_DIR:?probe-chrome.bash must be run from triage.bash}"

readonly play="${repoRoot}/playbooks/imports/play-browsers.yml"
readonly helperModule="helpers/rpm_keys/subkeys.py"
readonly localKey="/etc/pki/rpm-gpg/RPM-GPG-KEY-google-chrome"
readonly publishedUrl="https://dl.google.com/linux/linux_signing_key.pub"

# Google's Linux signing key is ONE primary carrying eight signing subkeys, five
# of them expired, and the current Chrome package is signed by the subkey below.
# rpm and dnf both judge a key "present" by its PRIMARY id, which is why a key
# imported under F41 is never refreshed and the newer subkey never arrives.
readonly primaryKeyId="7721F63BD38B4796"
readonly signingSubkeyId="FD533C07C264648F"
# rpm names a gpg-pubkey package after the last 8 hex digits of the primary,
# lowercased. Derived rather than spelled out a second time, so the two cannot
# drift apart — it is the same derivation helpers/rpm_keys/subkeys.py makes.
_shortId="${primaryKeyId: -8}"
readonly envelopePrefix="gpg-pubkey-${_shortId,,}-"

# PROBE_RC — the exit status of the LAST probe, so a caller can record a DECISIVE
# question that went unanswered. Probes themselves always return 0: one
# unanswerable question must not stop the rest of the section being collected.
PROBE_RC=0

# _probe <label> <no-match-rc> <command>... — report all THREE outcomes distinctly:
# the command FAILED, the command ran and found NOTHING, the command found
# something. Those are three different facts, and a triage script that collapses
# them reports a host it never actually asked about.
#
# <no-match-rc> is what makes that true for a grep-terminated probe. grep exits 1
# when it ran perfectly and matched nothing, so treating every non-zero exit as a
# failure prints "COMMAND FAILED" over the exact finding the probe exists for — an
# absent key reported as a broken command. Pass 1 for grep and for `rpm -q`, whose
# exit 1 likewise means "no such package"; pass "" for anything whose non-zero exit
# really is a failure. Exit >= 2 stays a failure either way, which is also grep's
# own documented contract.
_probe() {
    local label="$1" noMatchRc="$2"
    shift 2
    local out="" rc=0
    # `if cmd; then rc=0; else rc=$?; fi`, NOT `if ! cmd; then rc=$?; fi`: `!`
    # replaces the status with its own negation, so the second form records 0 for
    # every failure and the whole three-outcome contract below silently collapses.
    if out="$("$@" 2>&1)"; then
        rc=0
    else
        rc=$?
    fi
    PROBE_RC="${rc}"
    if [[ "${rc}" -eq 0 ]]; then
        if [[ -z "${out//[[:space:]]/}" ]]; then
            printf -- '- %s: ran cleanly and produced no output\n' "${label}"
        else
            printf -- '- %s:\n%s\n' "${label}" "${out}"
        fi
        return 0
    fi
    if [[ -n "${noMatchRc}" ]] && [[ "${rc}" -eq "${noMatchRc}" ]]; then
        if [[ -z "${out//[[:space:]]/}" ]]; then
            printf -- '- %s: ran cleanly and MATCHED NOTHING (exit %d)\n' "${label}" "${rc}"
        else
            printf -- '- %s: ran cleanly and MATCHED NOTHING (exit %d):\n%s\n' "${label}" "${rc}" "${out}"
        fi
        return 0
    fi
    printf -- '- %s: COMMAND FAILED (exit %d): %s\n' "${label}" "${rc}" "${out:-(no output)}"
    return 0
}

# probe — for a command whose every non-zero exit is a failure.
probe() {
    _probe "$1" "" "${@:2}"
}

# probe_match — for a command whose exit 1 means "ran fine, matched nothing".
probe_match() {
    _probe "$1" 1 "${@:2}"
}

# note_unanswered <what> — record a decisive question this run could not answer.
# The section reports these and exits non-zero, so plan_finish says the
# fact-finding was incomplete rather than reporting a clean run (R7/R9).
unanswered=""
note_unanswered() {
    unanswered="${unanswered}${unanswered:+; }$1"
}

# key_facts <path> — the identity of an OpenPGP key file: sha256, primary id,
# subkey ids, and whether it carries the subkey the current Chrome package is
# signed by. Returns non-zero, with the reason printed, when the file cannot be
# read or gpg cannot parse it: an unreadable key is not a key with no subkeys, and
# reporting the first as the second is how a host gets declared fine by a probe
# that failed.
key_facts() {
    local path="$1" sum="" colons="" primary="" subs=""
    if [[ ! -r "${path}" ]]; then
        printf 'NOT READABLE: %s\n' "${path}"
        return 1
    fi
    if ! sum="$(sha256sum "${path}" 2>&1)"; then
        printf 'sha256sum failed for %s: %s\n' "${path}" "${sum}"
        return 1
    fi
    # gpg chatter goes to stderr and is captured with the colon listing; awk keeps
    # only the record types it was asked for, so the noise cannot be parsed as a key.
    if ! colons="$(gpg --show-keys --with-colons "${path}" 2>&1)"; then
        printf 'gpg could not read %s: %s\n' "${path}" "${colons}"
        return 1
    fi
    primary="$(printf '%s\n' "${colons}" | awk -F: '$1 == "pub" { print $5; exit }')"
    subs="$(printf '%s\n' "${colons}" | awk -F: '$1 == "sub" { printf "%s ", $5 }')"
    if [[ -z "${primary}" ]]; then
        printf 'gpg read %s but reported no primary key, so it holds no key this can identify\n' "${path}"
        return 1
    fi
    local primaryNote="NOT the published Google primary ${primaryKeyId}"
    if [[ "${primary}" == "${primaryKeyId}" ]]; then
        primaryNote="the published Google primary"
    fi
    local carries="no"
    # awk leaves one trailing separator, and the membership test below needs the
    # list delimited on both sides so a substring cannot pass for an id.
    case " ${subs}" in
        *" ${signingSubkeyId} "*) carries="yes" ;;
    esac
    subs="${subs% }"
    printf '    path:     %s\n' "${path}"
    printf '    sha256:   %s\n' "${sum%% *}"
    printf '    primary:  %s  (%s)\n' "${primary}" "${primaryNote}"
    printf '    subkeys:  %s\n' "${subs:-(none)}"
    printf '    carries %s: %s\n' "${signingSubkeyId}" "${carries}"
}

# dump_installed_armour <envelope> <dest> — the armoured key rpm keeps as a
# gpg-pubkey package's description, written where gpg can read it.
dump_installed_armour() {
    local envelope="$1" dest="$2" armour=""
    if ! armour="$(rpm -q "${envelope}" --qf '%{description}' 2>&1)"; then
        printf 'rpm could not read the description of %s: %s\n' "${envelope}" "${armour}"
        return 1
    fi
    if [[ -z "${armour//[[:space:]]/}" ]]; then
        printf '%s has an EMPTY description, so there is no key armour to read\n' "${envelope}"
        return 1
    fi
    printf '%s\n' "${armour}" >"${dest}"
    printf 'wrote %s\n' "${dest}"
}

# fetch_published_key <dest> — one HTTPS GET of Google's published key, through the
# same library ansible's get_url uses. Nothing on the host is touched; the bytes
# land in the run directory so the on-host copy can be compared with them.
fetch_published_key() {
    local dest="$1"
    python3 - "${publishedUrl}" "${dest}" <<'PYTHON'
import sys
import urllib.request

url, dest = sys.argv[1], sys.argv[2]
with urllib.request.urlopen(url, timeout=30) as response:
    payload = response.read()
with open(dest, "wb") as handle:
    handle.write(payload)
print(f"{len(payload)} bytes fetched from {url}")
PYTHON
}

# key_decision <published-key-path> — the play's OWN decision, made by the module
# the play runs. Not a re-implementation: a check that re-implements the predicate
# instead of calling it stays green when the predicate breaks.
key_decision() {
    local published="$1"
    (
        cd "${repoRoot}"
        python3 -m helpers.rpm_keys.subkeys --published "${published}"
    )
}

# chrome_repo_files — every chrome/google repo file under /etc/yum.repos.d, with
# its contents. A `dnf remove` of Chrome does not delete these: the file is written
# by the package's post-install scriptlet and is owned by no package, so it outlives
# the thing that created it.
chrome_repo_files() {
    local file found=0
    for file in /etc/yum.repos.d/*.repo; do
        [[ -e "${file}" ]] || continue
        case "${file,,}" in
            *chrome* | *google*) ;;
            *) continue ;;
        esac
        found=1
        printf '### %s\n' "${file}"
        cat "${file}"
    done
    if [[ "${found}" -eq 0 ]]; then
        printf '(no chrome or google repo file under /etc/yum.repos.d)\n'
    fi
}

# dnf_repos — the repo list, filtered to the ones this plan is about. The dnf call
# and the filter are separated so their exit statuses stay distinguishable: a
# failing dnf returns 2 (a real failure), while grep's 1 keeps its own meaning of
# "ran fine, matched nothing".
dnf_repos() {
    local listing=""
    if ! listing="$(dnf -q repolist --all 2>&1)"; then
        printf 'dnf repolist failed: %s\n' "${listing}"
        return 2
    fi
    printf '%s\n' "${listing}" | grep -iE 'repo id|chrome|google'
}

# require_tools <tool>... — a missing tool is an IaC gap, not something to skip
# around (CLAUDE.md, "Missing Dependencies"). Absence of a check is not a passing
# check, so this fails the section.
require_tools() {
    local tool
    for tool in "$@"; do
        if command -v "${tool}" >/dev/null; then
            continue
        fi
        printf -- '- %s IS NOT INSTALLED, so this section can establish nothing.\n' "${tool}"
        case "${tool}" in
            gpg)
                printf '  gpg comes from gnupg2, which playbooks/imports/play-browsers.yml declares\n'
                printf '  precisely because the key check reads keys with it — deploy that play.\n'
                ;;
            *)
                printf '  %s is a base package on this distribution; its absence is an IaC gap in\n' "${tool}"
                printf '  whatever play owns this host, not something for a probe to work around.\n'
                ;;
        esac
        printf '  Do NOT install it by hand (CLAUDE.md, Missing Dependencies — Fail Fast, Fix in IaC).\n'
        return 1
    done
    return 0
}

# ── section 1: what this checkout carries ────────────────────────────────────────

section_checkout() {
    cat <<'NOTE'

## 1. What THIS checkout carries

Issue #45 was two stacked causes. Cause 1: handing dnf a package URL puts it in
the synthetic `@commandline` repo, and dnf5 validates against the keys configured
on the package's repo — `@commandline` has none. Cause 2: the Google primary key
imported under F41 never received the signing subkey the current package is signed
by. The fix for both is in play-browsers.yml, driven by the helper below.

The greps report which shape is present. A grep that MATCHED NOTHING ran perfectly
and found that shape absent — that is a finding, not a failure.

NOTE
    printf -- '- checkout: %s\n' "${repoRoot}"
    probe 'HEAD' git -C "${repoRoot}" log --oneline -1
    probe 'branch and tracking' git -C "${repoRoot}" status --short --branch

    if [[ ! -r "${play}" ]]; then
        printf -- '- play-browsers.yml is NOT READABLE at %s, so what this checkout\n' "${play}"
        printf '  installs cannot be established.\n'
        note_unanswered "the shape of the Chrome tasks in play-browsers.yml"
        return 1
    fi

    probe_match 'the package the Install Google Chrome task names (the fixed shape)' \
        grep -nE '^[[:space:]]*name: google-chrome-stable[[:space:]]*$' "${play}"
    probe_match 'a direct .rpm URL (the pre-fix shape, which reproduces cause 1)' \
        grep -nE 'name: https://dl\.google\.com/.*\.rpm' "${play}"
    probe_match 'the google-chrome repository declaration and the key it validates against' \
        grep -nE 'yum_repository|gpgkey:|baseurl:' "${play}"
    probe_match 'the key-staleness decision and the module that makes it' \
        grep -n 'helpers.rpm_keys.subkeys' "${play}"
    probe_match 'the removal loop, which acts only on what the decision names' \
        grep -n 'RPM-KEY-ENVELOPE' "${play}"
    probe 'the helper module this checkout carries' \
        ls -l "${repoRoot}/${helperModule}"
}

# ── section 2: the Google signing key on this host (PLAN.md Task 4.2) ────────────

section_key() {
    cat <<'NOTE'

## 2. The Google signing key on this host  (READ THIS FOR: Task 4.2)

Task 4.2 asks whether a SECOND run reports the key tasks as ok rather than
changed. Each fact below predicts one of the play's tasks:

| fact                                      | the task it predicts                      |
| ----------------------------------------- | ----------------------------------------- |
| gnupg2 is installed                       | Ensure gnupg2 Is Available                |
| the key file is present, 0644 root:root   | Fetch Google's Published Signing Key      |
| the key file matches what Google publishes | Fetch Google's Published Signing Key      |
| the RPM-KEY-ACTION line                   | Remove The Stale Google Signing Key       |
| the keyring holds the published primary   | Import Google Chrome Signing Key          |
| repo_add_once is "false"                  | Stop Chrome's Scriptlet Re-Adding …       |
| the repo file's gpgkey is the file:// one | Add Google Chrome Repository              |

The last two are one fact in two places. Chrome's %post rewrites the repo file
with a network gpgkey whenever /etc/default/google-chrome is absent or reads
"true", so a host in that state reports `changed` for Add Google Chrome
Repository on EVERY run and after every Chrome upgrade. Section 2.4 reads both.

RPM-KEY-ACTION none means the removal loop receives no envelopes, so that task
has nothing to do and rpm_key finds the key already present. refresh means the
loop names envelopes to erase, so that task and the re-import both act. import
means no copy of this key is installed at all.

The decision is made by running the module the play runs, not by re-implementing
it here — a check that re-implements the predicate stays green when the predicate
breaks.

NOTE
    printf 'The key file is %s. The published primary is %s, and the current package\n' \
        "${localKey}" "${primaryKeyId}"
    printf 'is signed by its subkey %s.\n' "${signingSubkeyId}"
    if ! require_tools rpm gpg python3 sha256sum; then
        note_unanswered "every key fact (a required tool is missing)"
        return 1
    fi

    printf '\n### 2.1 What the keyring holds\n\n'
    probe_match 'the OpenPGP tool the key check reads keys with' rpm -q gnupg2

    local listing="" line="" envelope="" armourPath=""
    local envelopes=()
    if ! listing="$(rpm -qa gpg-pubkey --qf '%{name}-%{version}-%{release}\n' 2>&1)"; then
        printf -- '- the rpm keyring could not be listed: %s\n' "${listing}"
        note_unanswered "the installed Google key"
    else
        while IFS= read -r line; do
            # Anchored on the trailing separator, exactly as the helper does: these
            # names are what the play would hand `rpm --erase`, and a prefix match
            # would claim a key nobody asked about.
            case "${line}" in
                "${envelopePrefix}"*) envelopes+=("${line}") ;;
            esac
        done <<<"${listing}"

        if [[ "${#envelopes[@]}" -eq 0 ]]; then
            printf -- '- NO gpg-pubkey package named %s* is installed, so this keyring holds\n' "${envelopePrefix}"
            printf '  no copy of Google primary %s. That is the import case.\n' "${primaryKeyId}"
        else
            printf -- '- installed key packages for this primary: %s\n' "${envelopes[*]}"
            for envelope in "${envelopes[@]}"; do
                armourPath="${runDir}/installed-${envelope}.asc"
                probe "armour of ${envelope}" dump_installed_armour "${envelope}" "${armourPath}"
                if [[ "${PROBE_RC}" -ne 0 ]]; then
                    note_unanswered "the identity of the installed key ${envelope}"
                    continue
                fi
                probe "identity of ${envelope}" key_facts "${armourPath}"
                if [[ "${PROBE_RC}" -ne 0 ]]; then
                    note_unanswered "the identity of the installed key ${envelope}"
                fi
            done
        fi
    fi

    printf '\n### 2.2 The key file the play fetched, and what Google publishes now\n\n'
    # probe_match, not probe: stat exits 1 for "no such file", and in this script's
    # taxonomy an absent key file is a FINDING — the play has not fetched it yet —
    # not a broken command. Reporting it as COMMAND FAILED is the thing this script
    # exists not to do.
    probe_match 'permissions and ownership of the fetched key file' 1 \
        stat -c '%n  mode %a  owner %U:%G  size %s' "${localKey}"
    probe 'identity of the key file on this host' key_facts "${localKey}"
    if [[ "${PROBE_RC}" -ne 0 ]]; then
        note_unanswered "the identity of ${localKey}"
    fi

    local publishedPath="${runDir}/published-linux_signing_key.pub"
    probe 'fetching the published key for comparison' fetch_published_key "${publishedPath}"
    if [[ "${PROBE_RC}" -ne 0 ]]; then
        printf -- '- the published key could not be fetched, so the on-host copy was not compared\n'
        printf '  with it. That says nothing about the on-host copy either way.\n'
        note_unanswered "whether the on-host key file matches the published one"
    else
        probe 'identity of the published key' key_facts "${publishedPath}"
        if [[ "${PROBE_RC}" -ne 0 ]]; then
            note_unanswered "the identity of the published key"
        fi
        local localSum="" publishedSum="" compareError=""
        if ! localSum="$(sha256sum "${localKey}" 2>&1)"; then
            compareError="${localSum}"
        elif ! publishedSum="$(sha256sum "${publishedPath}" 2>&1)"; then
            compareError="${publishedSum}"
        fi
        if [[ -n "${compareError}" ]]; then
            printf -- '- the two key files were NOT compared, because one could not be read: %s\n' \
                "${compareError}"
            note_unanswered "whether the on-host key file matches the published one"
        elif [[ "${localSum%% *}" == "${publishedSum%% *}" ]]; then
            printf -- '- the key file on this host is BYTE-IDENTICAL to what Google publishes now.\n'
        else
            printf -- '- the key file on this host DIFFERS from what Google publishes now:\n'
            printf '    on host:   %s\n' "${localSum%% *}"
            printf '    published: %s\n' "${publishedSum%% *}"
            printf '  EXPECT THE FETCH TASK TO REPORT changed ON THE NEXT RUN, and that is the\n'
            printf '  mechanism working rather than a regression. get_url only takes its\n'
            printf '  already-present early exit when a checksum: is set, and this task sets\n'
            printf '  none — so it issues a conditional GET with If-Modified-Since derived from\n'
            printf '  the file mtime. A 200 replaces the file and reports changed; a 304 reports\n'
            printf '  ok. Google has added subkeys before and will again.\n'
        fi
    fi

    printf '\n### 2.3 The decision the play itself will make\n\n'
    if [[ -r "${localKey}" ]]; then
        probe 'helpers.rpm_keys.subkeys against the key file the play manages' \
            key_decision "${localKey}"
        if [[ "${PROBE_RC}" -ne 0 ]]; then
            note_unanswered "the key-staleness decision the play will make"
        fi
    else
        printf -- '- %s is not readable, so the play would fetch it before deciding;\n' "${localKey}"
        printf '  the decision is made below against the copy fetched into the run directory\n'
        printf '  instead, which is the same question asked of the same bytes.\n'
        if [[ -r "${publishedPath}" ]]; then
            probe 'helpers.rpm_keys.subkeys against the freshly published key' \
                key_decision "${publishedPath}"
            if [[ "${PROBE_RC}" -ne 0 ]]; then
                note_unanswered "the key-staleness decision the play will make"
            fi
        else
            note_unanswered "the key-staleness decision the play will make"
        fi
    fi

    printf '\n### 2.4 Who owns the repo file — the play, or Chrome'"'"'s scriptlet\n\n'
    cat <<'NOTE'
Chrome's %post creates /etc/default/google-chrome with repo_add_once="true" when
it is ABSENT, and re-writes /etc/yum.repos.d/google-chrome.repo with a NETWORK
gpgkey whenever it reads true. A host in that state reports `changed` for "Add
Google Chrome Repository" on every run, which is the Task 4.2 answer.

NOTE
    probe_match 'the repo_add_once setting Chrome'"'"'s scriptlet reads' 1 \
        grep -H 'repo_add_once' /etc/default/google-chrome
    if [[ "${PROBE_RC}" -eq 1 ]]; then
        printf -- '- /etc/default/google-chrome has no repo_add_once line (or does not exist).\n'
        printf '  The play now writes it as "false" BEFORE installing Chrome, so the first\n'
        printf '  run after this change reports that task changed and subsequent runs ok.\n'
    fi
    probe_match 'the gpgkey the repo file actually carries' 1 \
        grep -H 'gpgkey' /etc/yum.repos.d/google-chrome.repo
    printf -- '- A gpgkey of file:///etc/pki/rpm-gpg/... is the play'"'"'s. An https:// one is the\n'
    printf '  scriptlet'"'"'s, and means the repo task will report changed putting it back.\n'

    if [[ -n "${unanswered}" ]]; then
        printf '\n- FACT-FINDING INCOMPLETE. Not established by this run: %s\n' "${unanswered}"
        return 1
    fi
}

# ── section 3: the rest of the host picture, kept for a regression ───────────────

section_host() {
    cat <<'NOTE'

## 3. Repo files, dnf and the installed package

Kept from the original diagnostic. None of it decides Task 4.2; all of it is what
a regression would need, because it is the state that made the original failure
message ambiguous.

NOTE
    if ! require_tools rpm dnf; then
        note_unanswered "every dnf and rpm fact (a required tool is missing)"
        return 1
    fi

    probe 'kernel' uname -r
    probe_match 'the release this host reports' grep -E '^(NAME|VERSION_ID)=' /etc/os-release
    probe 'repo files left behind under /etc/yum.repos.d' chrome_repo_files
    probe_match 'is Chrome installed right now' rpm -q google-chrome-stable
    probe_match 'the repos dnf knows about, filtered' dnf_repos
    probe_match "dnf's own view of the package" dnf -q info google-chrome-stable
    probe 'dnf version (the behaviour that changed is dnf5s)' dnf --version
    probe_match 'the dnf packages installed' rpm -q dnf dnf5 libdnf5
}

# ── dispatch ─────────────────────────────────────────────────────────────────────

# stdout is the report; progress goes to stderr (CLAUDE/StderrHygiene.md).
printf '==> probing section: %s\n' "${section}" >&2

sectionStatus=0
case "${section}" in
    checkout)
        if ! section_checkout >>"${report}"; then sectionStatus=1; fi
        ;;
    key)
        if ! section_key >>"${report}"; then sectionStatus=1; fi
        ;;
    host)
        if ! section_host >>"${report}"; then sectionStatus=1; fi
        ;;
    *)
        printf '[FATAL] unknown section %s — expected checkout, key or host\n' "${section}" >&2
        exit 64
        ;;
esac

if [[ "${sectionStatus}" -ne 0 ]]; then
    printf '[WARN] section %s could not answer a decisive question, so the FACT-FINDING is\n' "${section}" >&2
    printf '       incomplete. That is not a statement about the host. See %s\n' "${report}" >&2
    exit 1
fi

printf '==> section %s appended to %s\n' "${section}" "${report}" >&2
