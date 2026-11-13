#!/usr/bin/env bash
# Plan 00246 Task 8.8 — x30-lab.bash: X30 on the VM lab, agent bus v2 across three networks
# (INSTALL-PATH.md section 3; design subagent-reports/261109-vmlab-multinet-design-opus.md).
#
#   N1  bus .10, member .20     declared        N2  bus .10, router .2    declared
#   N3  router .2, prober .20   undeclared; the prober reaches N2 through the router
#
# Checks: the three networks (N*); the bus's password sudo user issuer (P2, D7) and its outbound
# ssh blocked, with a control (P3C, P3); stage A's play with team x30 on the bus (networks N1
# and N2, url https://bus.x30.test:<port>, a DNS name) and no team on member and router; the
# unit files equal `units print` (C1); TLS pinned to the team CA (C6, C6N); no plain http (C7);
# the prober refused at the firewall, fast (C8); the relay-form issue (sudo -S on ssh's stdin,
# member add piped through this controller into place-issued); worker to worker both ways with
# the coordinator declared, issued and never run (W1, W2); the host move with the DNS url in
# D6's order (M1-M4).
#
# Not run, and the report says so: the /32 check and the permissive-zone refusal (D8), C11, C12.
# Bridged: no production code takes a placed pool seat into a session (Task 4.2), so
# x30-guest.bash stage-seat copies it. Met: restore is refused in the production build until
# host checks H-C5 and H-C15 pass (DC8, decided as option (a)), so M1-M4 end as COULD NOT
# ESTABLISH until those checks have run (if either fails, DC8 returns to the owner).
# Every statement of a leg ends in `|| return 1`: plan_deploy_leg runs a leg as an `if`
# condition, where bash ignores errexit (issue #233).
#
# INPUT: host_vars vm_test_lab_networks n1, n2, n3 (no default; the lab play writes them into
# lab.env). The lab play must have run with this commit; the checkout must be clean.
# RUN ON THE HOST as the desktop user, through CLAUDE/Plan/meta-deploy.bash or directly, with
# --keep to leave the guests up. Never prompts; no sudo on the host.
# EFFECT: guests x30-bus, x30-member, x30-router, x30-prober and links x30-n1 to x30-n3, removed
# on every exit path (and a previous run's first) unless --keep; 6.75 GiB of guest RAM, 28 GiB
# of disk asked first (D4). The sudo password, inventory and backup (it holds the CA key) sit
# in a 0700 directory under $XDG_RUNTIME_DIR, removed on exit. The play ledger is off for the
# guest plays (D5, issue #234).
# EXIT: 0 every check passed; 1 a check failed or a leg aborted; 2 COULD NOT ESTABLISH; 64 usage.
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

PLAN_USAGE="usage: x30-lab.bash [--keep] [-h|--help]

X30 on the VM lab: four rootless guests on three point-to-point networks, stage A's teamchat
play, the relay-form issue with a password sudo and the bus's outbound ssh blocked, worker to
worker with the coordinator absent, and the host move with a DNS url. Host-only, no sudo.
--keep leaves the guests and links up for diagnosis."

plan_mode deploy
plan_parse_common_flags "$@"
KEEP=0
for arg in "${PLAN_REMAINING_ARGS[@]+"${PLAN_REMAINING_ARGS[@]}"}"; do
    case "${arg}" in
        --keep) KEEP=1 ;;
        *)
            printf '[FATAL] unknown argument: %s\n%s\n' "${arg}" "${PLAN_USAGE}" >&2
            exit 64
            ;;
    esac
done
if [[ "${PLAN_CHECK:-0}" -eq 1 ]]; then
    printf '[FATAL] --check has no dry run here: the scenario boots guests to have anything to check\n' >&2
    exit 64
fi

plan_require_host "it boots lab guests in this user's libvirt session and runs plays against them"
if [[ "${EUID}" -eq 0 ]]; then
    printf '[FATAL] run this as the desktop user, not root: the lab is rootless\n' >&2
    exit 1
fi
plan_start_log auto

# ── the scenario's constants ──────────────────────────────────────────────────────────────
GUEST_FIXTURE="${PLAN_SCRIPT_DIR}/x30-guest.bash"
BASE=server-fast
TEAM=x30
PORT=8448
DNS_NAME=bus.x30.test
PREFIX="${TEAM}+lab.host"
COORDINATOR="${TEAM}.1+lab.host"
MEMBER_WORKER="${TEAM}.2+lab.host"
ROUTER_WORKER="${TEAM}.3+lab.host"
GUESTS=(x30-bus x30-member x30-router x30-prober)
LINKS=(x30-n1 x30-n2 x30-n3)
DISK_GIB=$((22 + 2 * (${#GUESTS[@]} - 1)))
REPORT="${PLAN_RUN_DIR}/x30-lab-report.md"
RUN_TAG="$(date -u +%Y%m%dT%H%M%SZ)-$$"
SECRETS=""
N1=""
N2=""
N3=""
BUS_N1=""
BUS_N2=""
MEMBER_N1=""
ROUTER_N3=""
PASSED=0
FAILED=0
NOT_RUN=()

# ── helpers ───────────────────────────────────────────────────────────────────────────────

note() {
    printf '%s\n' "$1" | tee -a "${REPORT}"
}

# check <id> <what> <cmd...>: run a check; its verdict is a line of the report.
check() {
    local id="$1" what="$2"
    shift 2
    if "$@"; then
        PASSED=$((PASSED + 1))
        note "[PASS] ${id} ${what}"
    else
        FAILED=$((FAILED + 1))
        note "[FAIL] ${id} ${what}"
    fi
}

# refused <cmd...>: true when the command fails (the check is that something is refused).
refused() {
    if "$@"; then
        return 1
    fi
    return 0
}

# ip_at <cidr> <n>: the n'th address of the network.
ip_at() {
    local a b c d v
    IFS=. read -r a b c d <<<"${1%/*}"
    v=$(((a << 24) + (b << 16) + (c << 8) + d + $2))
    printf '%d.%d.%d.%d\n' $(((v >> 24) & 255)) $(((v >> 16) & 255)) $(((v >> 8) & 255)) $((v & 255))
}

gssh() {
    vmtest member-ssh "$@"
}

# fixture <guest> <step> [args...]: one step of x30-guest.bash in the guest, as root.
fixture() {
    local guest="$1" step="$2" out
    shift 2
    out="$(gssh "${guest}" "sudo bash -s -- ${step} $(printf '%q ' "$@")" <"${GUEST_FIXTURE}")" || return 1
    printf '%s\n' "${out}"
    grep -qx "VMTEST-GUEST-PREPARE-DONE ${step}" <<<"${out}"
}

# fixture_user <guest> <step> [args...]: a step as the login user (the seat store is the user's).
# An argument is expanded by the guest's shell, so '$HOME/...' names the guest user's home.
fixture_user() {
    local guest="$1" step="$2" out
    shift 2
    out="$(gssh "${guest}" "bash -s -- ${step} $*" <"${GUEST_FIXTURE}")" || return 1
    printf '%s\n' "${out}"
    grep -qx "VMTEST-GUEST-PREPARE-DONE ${step}" <<<"${out}"
}

# as_issuer <guest> <command>: the command under the issuer's PASSWORD sudo, the password on
# ssh's stdin and never on a command line.
as_issuer() {
    local pw
    pw="$(<"${SECRETS}/pw")" || return 1
    printf '%s\n' "${pw}" | gssh --as issuer "$1" "sudo -S -p '' $2"
}

# mac_of <guest> <link>: the MAC of the guest's NIC on the link, as member-address prints it.
mac_of() {
    local mac
    mac="$(vmtest member-address "$1" | awk -v l="link=$2" '$1 == "VMTEST-MEMBER-NIC" && $3 == l { sub("^mac=", "", $5); print $5 }')" || return 1
    if [[ ! "${mac}" =~ ^52:54:00:[0-9a-f]{2}:[0-9a-f]{2}:[0-9a-f]{2}$ ]]; then
        printf '[FATAL] %s has no NIC on %s\n' "$1" "$2" >&2
        return 1
    fi
    printf '%s\n' "${mac}"
}

# address_field <guest> <field>: one field of the guest's VMTEST-MEMBER-ADDRESS line.
address_field() {
    vmtest member-address "$1" |
        awk -v f="$2=" '$1 == "VMTEST-MEMBER-ADDRESS" { for (i = 3; i <= NF; i++) if (index($i, f) == 1) print substr($i, length(f) + 1) }'
}

# tcp_open <guest> <address> <port>: the guest opens a TCP connection, bounded at 10 s.
tcp_open() {
    gssh "$1" "timeout 10 bash -c 'exec 3<>/dev/tcp/$2/$3'"
}

teardown() {
    local g l status=0
    if [[ "${KEEP}" -eq 1 ]]; then
        printf '==> --keep: guests and links left up; remove them with vmtest member-down / link-down\n' >&2
    else
        for g in "${GUESTS[@]}"; do
            vmtest member-down "${g}" || status=1
        done
        for l in "${LINKS[@]}"; do
            vmtest link-down "${l}" || status=1
        done
    fi
    if [[ -n "${SECRETS}" ]]; then
        rm -rf "${SECRETS}" || status=1
    fi
    return "${status}"
}

# ── setup legs ────────────────────────────────────────────────────────────────────────────

leg_preflight() {
    local tool free
    for tool in vmtest ansible-playbook awk curl; do
        if ! command -v "${tool}" >/dev/null; then
            printf '[FATAL] %s is not on PATH; vmtest comes from play-vm-test-lab.yml\n' "${tool}" >&2
            return 1
        fi
    done
    N1="$(vmtest lab-network n1)" || return 1
    N2="$(vmtest lab-network n2)" || return 1
    N3="$(vmtest lab-network n3)" || return 1
    free="$(df --output=avail -B1G "${VMTEST_HOME:-${HOME}/.local/share/vmtest}" | awk 'NR == 2 { print $1 }')" || return 1
    if [[ ! "${free}" =~ ^[0-9]+$ ]] || [[ "${free}" -lt "${DISK_GIB}" ]]; then
        printf '[FATAL] the lab has %s GiB free; %s guests need %s GiB (22 for the first, 2 per further guest); free some with vmtest prune-runs\n' \
            "${free}" "${#GUESTS[@]}" "${DISK_GIB}" >&2
        return 1
    fi
    if [[ -z "${XDG_RUNTIME_DIR:-}" || ! -d "${XDG_RUNTIME_DIR}" ]]; then
        printf '[FATAL] no XDG_RUNTIME_DIR for the run'"'"'s secrets\n' >&2
        return 1
    fi
    SECRETS="$(mktemp -d "${XDG_RUNTIME_DIR}/x30-lab.XXXXXX")" || return 1
    chmod 0700 "${SECRETS}" || return 1
    (umask 077 && head -c 48 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' >"${SECRETS}/pw") || return 1
    if [[ "$(wc -c <"${SECRETS}/pw")" -lt 24 ]]; then
        printf '[FATAL] the throwaway password came out short\n' >&2
        return 1
    fi
    note "networks: n1 ${N1}, n2 ${N2}, n3 ${N3}; run ${RUN_TAG}"
}

leg_clear_leftovers() {
    local g l
    for g in "${GUESTS[@]}"; do
        vmtest member-down "${g}" || return 1
    done
    for l in "${LINKS[@]}"; do
        vmtest link-down "${l}" || return 1
    done
}

leg_links() {
    local l
    for l in "${LINKS[@]}"; do
        vmtest link-up "${l}" || return 1
    done
}

boot_bus() {
    vmtest member-up x30-bus "${BASE}" --ram-mib 3072 --link x30-n1:a --link x30-n2:a
}

leg_guests() {
    boot_bus || return 1
    vmtest member-up x30-member "${BASE}" --ram-mib 1536 --link x30-n1:b || return 1
    vmtest member-up x30-router "${BASE}" --ram-mib 1536 --link x30-n2:b --link x30-n3:a || return 1
    vmtest member-up x30-prober "${BASE}" --ram-mib 768 --link x30-n3:b || return 1
}

# bus_net <host number>: the bus's two link NICs at that host number, its route to N3, and
# its own DNS name.
bus_net() {
    local n="$1" m1 m2
    m1="$(mac_of x30-bus x30-n1)" || return 1
    m2="$(mac_of x30-bus x30-n2)" || return 1
    BUS_N1="$(ip_at "${N1}" "${n}")" || return 1
    BUS_N2="$(ip_at "${N2}" "${n}")" || return 1
    fixture x30-bus net "${m1}=${BUS_N1}/${N1#*/}" "${m2}=${BUS_N2}/${N2#*/}" \
        "route=${N3}@$(ip_at "${N2}" 2)@${m2}" || return 1
    fixture x30-bus hosts "${DNS_NAME}" "${BUS_N1}" || return 1
}

leg_networks() {
    local router_n2 prober_n3 mac
    MEMBER_N1="$(ip_at "${N1}" 20)" || return 1
    router_n2="$(ip_at "${N2}" 2)" || return 1
    ROUTER_N3="$(ip_at "${N3}" 2)" || return 1
    prober_n3="$(ip_at "${N3}" 20)" || return 1
    bus_net 10 || return 1
    mac="$(mac_of x30-member x30-n1)" || return 1
    fixture x30-member net "${mac}=${MEMBER_N1}/${N1#*/}" || return 1
    fixture x30-router net "$(mac_of x30-router x30-n2)=${router_n2}/${N2#*/}" \
        "$(mac_of x30-router x30-n3)=${ROUTER_N3}/${N3#*/}" forward || return 1
    mac="$(mac_of x30-prober x30-n3)" || return 1
    fixture x30-prober net "${mac}=${prober_n3}/${N3#*/}" "route=${N2}@${ROUTER_N3}@${mac}" || return 1
}

# bus_host_setup: firewalld, the rejecting zone on both link NICs, and the password sudo user.
bus_host_setup() {
    local m1 m2
    m1="$(mac_of x30-bus x30-n1)" || return 1
    m2="$(mac_of x30-bus x30-n2)" || return 1
    fixture x30-bus firewalld || return 1
    fixture x30-bus zone "${m1}" "${m2}" || return 1
    gssh x30-bus 'sudo sh -c "umask 077 && cat >/root/x30-issuer-pw"' <"${SECRETS}/pw" || return 1
    fixture x30-bus issuer || return 1
}

leg_hosts() {
    bus_host_setup || return 1
    fixture x30-member firewalld || return 1
    fixture x30-router firewalld || return 1
    fixture x30-member hosts "${DNS_NAME}" "${BUS_N1}" || return 1
    fixture x30-router hosts "${DNS_NAME}" "${BUS_N2}" || return 1
}

leg_block_ssh_out() {
    fixture x30-bus block-ssh-out
}

# write_inventory <file>: the guests as ssh hosts, from member-address; never the controller.
write_inventory() {
    local file="$1" g port key user
    printf 'x30:\n  hosts:\n' >"${file}" || return 1
    for g in x30-bus x30-member x30-router; do
        port="$(address_field "${g}" port)" || return 1
        key="$(address_field "${g}" key)" || return 1
        user=fedora
        if [[ "${g}" == x30-bus ]]; then
            user=issuer
        fi
        if [[ ! "${port}" =~ ^[0-9]+$ || -z "${key}" ]]; then
            printf '[FATAL] member-address %s gave no port or key\n' "${g}" >&2
            return 1
        fi
        printf '    %s: {ansible_host: 127.0.0.1, ansible_port: %s, ansible_user: %s, ansible_ssh_private_key_file: "%s"}\n' \
            "${g}" "${port}" "${user}" "${key}" >>"${file}" || return 1
    done
    printf '  vars:\n    ansible_connection: ssh\n    ansible_ssh_common_args: "%s"\n' \
        "-o IdentitiesOnly=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" >>"${file}"
}

# team_vars <file> <only-teams-json-or-empty>: the play's input for the bus. A scoped run
# approves nothing: the play refuses an approval of a team outside teamchat_only_teams.
team_vars() {
    local file="$1" only="$2"
    {
        if [[ -n "${only}" ]]; then
            printf '{"teamchat_owner_group": "x30owners", "teamchat_approve_teams": [],\n'
            printf ' "teamchat_only_teams": %s,\n' "${only}"
        else
            printf '{"teamchat_owner_group": "x30owners", "teamchat_approve_teams": ["%s"],\n' "${TEAM}"
        fi
        printf ' "teamchat_teams": [{"team": "%s", "state": "present", "port": %d,\n' "${TEAM}" "${PORT}"
        printf '   "description": "X30 lab team across three networks", "networks": ["%s", "%s"],\n' "${N1}" "${N2}"
        printf '   "url": "https://%s:%d", "humans": ["labowner"], "coordinator": "%s",\n' "${DNS_NAME}" "${PORT}" "${COORDINATOR}"
        printf '   "trust": {"kind": "github", "org": "example-org"}}]}\n'
    } >"${file}"
}

play() {
    (
        export ANSIBLE_CALLBACKS_ENABLED=ansible.builtin.default
        plan_ansible_playbook playbooks/imports/play-teamchat.yml -i "${SECRETS}/inventory.yml" --flush-cache "$@"
    )
}

play_bus() {
    team_vars "${SECRETS}/bus-vars.json" "$1" || return 1
    play -e teamchat_hosts=x30-bus -e "@${SECRETS}/bus-vars.json" --become-password-file "${SECRETS}/pw"
}

leg_plays() {
    write_inventory "${SECRETS}/inventory.yml" || return 1
    play_bus "" || return 1
    printf '{"teamchat_owner_group": "x30owners", "teamchat_teams": []}\n' >"${SECRETS}/worker-vars.json" || return 1
    play -e teamchat_hosts=x30-member,x30-router -e "@${SECRETS}/worker-vars.json" || return 1
}

# issue <seat> <human-text> <guest|-> : member add on the bus under the password sudo, its answer
# relayed through this controller into place-issued on the guest, then staged for a session.
# '-' issues and places nowhere: the coordinator, which no session ever runs.
issue() {
    local seat="$1" human="$2" guest="$3" out
    local add="teamchat-admin member add --team ${TEAM} --prefix ${PREFIX} --seat ${seat} --kind host --human-text ${human} --credential password"
    if [[ "${guest}" == - ]]; then
        out="$(as_issuer x30-bus "${add}")" || return 1
        if [[ "${out}" != *'"handle":"'"${COORDINATOR}"'"'* ]]; then
            printf '[FATAL] member add did not answer with the coordinator handle\n' >&2
            return 1
        fi
        return 0
    fi
    out="$(as_issuer x30-bus "${add}" |
        gssh "${guest}" 'mkdir -p "$HOME/x30-checkout" && teamchat seat place-issued --checkout "$HOME/x30-checkout"')" || return 1
    printf '%s\n' "${out}"
    if [[ ! "${out}" =~ placed\ seat\ ([^ ]+)\ of\ team\ ${TEAM} ]]; then
        printf '[FATAL] place-issued on %s did not say it placed a seat\n' "${guest}" >&2
        return 1
    fi
    printf '%s\n' "${BASH_REMATCH[1]}" >"${SECRETS}/seat-${guest}" || return 1
    fixture_user "${guest}" stage-seat "${TEAM}" "$(<"${SECRETS}/seat-${guest}")" '$HOME/x30-state' || return 1
}

leg_issue() {
    issue 1 true - || return 1
    issue 2 false x30-member || return 1
    issue 3 false x30-router || return 1
}

# ── checks ────────────────────────────────────────────────────────────────────────────────

# session <guest> <command>: the command inside a plain teamchat session on the staged seat.
session() {
    local seat
    seat="$(<"${SECRETS}/seat-$1")" || return 1
    gssh "$1" "teamchat run --plain --kind host --state \"\$HOME/x30-state\" --seat ${TEAM}/${seat} -- $2"
}

# w2w <from> <to-guest> <to-handle> <tag>: a receiver waits on <to-guest>, then <from> sends;
# the receiver's records must hold a peer message with the text.
w2w() {
    local from="$1" to="$2" handle="$3" tag="$4" text recv pid sent=0
    text="x30-${RUN_TAG}-${tag}"
    recv="${PLAN_RUN_DIR}/recv-${tag}.jsonl"
    session "${to}" "teamchat wait --timeout 240" >"${recv}" &
    pid=$!
    sleep 30
    if session "${from}" "sh -c 'printf \"%s\\n\" ${text} | teamchat say --to ${handle} --wait-contact'"; then
        sent=1
    fi
    if ! wait "${pid}"; then
        printf '[FAIL] the receiver on %s ended non-zero\n' "${to}" >&2
        return 1
    fi
    if [[ "${sent}" -ne 1 ]]; then
        printf '[FAIL] the send from %s ended non-zero\n' "${from}" >&2
        return 1
    fi
    grep -F '"kind":"peer"' "${recv}" | grep -qF "${text}"
}

# fingerprint <guest> <cert path>: sha256 of the certificate's DER.
fingerprint() {
    gssh "$1" "sudo openssl x509 -in $2 -outform DER | sha256sum" | awk '{ print $1 }'
}

seat_sum() {
    gssh x30-member "sha256sum \"\$HOME/x30-state/${TEAM}/seat.json\"" | awk '{ print $1 }'
}

# compare_units: every file units list names equals units print of it, and a copy with one
# byte added is told apart (the comparator is not vacuous).
compare_units() {
    gssh x30-bus 'bash -s' <<'EOF'
set -euo pipefail
n=0
while IFS=$'\t' read -r name path; do
    [[ -n "${name}" ]] || continue
    cmp <(sudo cat "${path}") <(teamchat-admin units print "${name}")
    if cmp -s <(sudo cat "${path}"; printf x) <(teamchat-admin units print "${name}"); then
        echo "the comparator did not see an added byte in ${path}" >&2
        exit 1
    fi
    n=$((n + 1))
done < <(teamchat-admin units list)
[[ "${n}" -gt 0 ]]
echo "units compared: ${n}"
EOF
}

tls_pinned() {
    gssh x30-bus "sudo cat /var/lib/teamchat-admin/${TEAM}/tls/ca.crt" >"${PLAN_RUN_DIR}/team-ca.crt" || return 1
    gssh x30-member 'cat >"$HOME/x30-ca.crt"' <"${PLAN_RUN_DIR}/team-ca.crt" || return 1
    gssh x30-member "curl -sS -m 15 --cacert \"\$HOME/x30-ca.crt\" https://${DNS_NAME}:${PORT}/_matrix/client/versions | grep -q versions"
}

tls_unpinned_refused() {
    refused gssh x30-member "curl -sS -m 15 https://${DNS_NAME}:${PORT}/_matrix/client/versions"
}

plain_http_refused() {
    refused gssh x30-member "curl -sS -m 15 http://${DNS_NAME}:${PORT}/_matrix/client/versions | grep -q versions"
}

prober_refused_fast() {
    local out
    out="$(gssh x30-prober "s=\$(date +%s%N); rc=0; curl -sS --connect-timeout 10 https://${BUS_N2}:${PORT}/ || rc=\$?; echo \"rc=\$rc ms=\$(( (\$(date +%s%N) - s) / 1000000 ))\"")" || return 1
    printf '%s\n' "${out}"
    [[ "${out}" =~ rc=([0-9]+)\ ms=([0-9]+) ]] || return 1
    [[ "${BASH_REMATCH[1]}" -eq 7 && "${BASH_REMATCH[2]}" -lt 3000 ]]
}

no_nopasswd_for_issuer() {
    refused gssh --as issuer x30-bus 'sudo -n true'
}

checks_networks() {
    check N1 "the member reaches the bus over N1 (tcp 22)" tcp_open x30-member "${BUS_N1}" 22
    check N2 "the router reaches the bus over N2 (tcp 22)" tcp_open x30-router "${BUS_N2}" 22
    check N3 "the prober reaches the router over N3 (tcp 22)" tcp_open x30-prober "${ROUTER_N3}" 22
    check N3R "the prober reaches the bus on N2 through the router, and back (tcp 22)" tcp_open x30-prober "${BUS_N2}" 22
    check P2 "issuer's sudo on the bus asks a password (no NOPASSWD)" no_nopasswd_for_issuer
    check P3C "control: before the block, the bus opens ssh to the member over N1" tcp_open x30-bus "${MEMBER_N1}" 22
}

checks_install() {
    check C1 "every installed unit file equals units print" compare_units
    check C6 "the member reaches the team over TLS by its DNS name, the team CA pinned" tls_pinned
    check C6N "without the team CA the member's TLS is refused" tls_unpinned_refused
    check C7 "plain http to the team port gets no Matrix answer" plain_http_refused
    check C8 "the prober (undeclared N3) is refused at the firewall in under 3 s" prober_refused_fast
}

checks_workers() {
    check W1 "member (N1) to router (N2), coordinator absent" w2w x30-member x30-router "${ROUTER_WORKER}" m2r
    check W2 "router (N2) to member (N1), coordinator absent" w2w x30-router x30-member "${MEMBER_WORKER}" r2m
}

# host_move: the host move with the DNS url (D6 order). A restore the build refuses (DC8) is
# recorded as not run, and the verdict becomes COULD NOT ESTABLISH.
host_move() {
    local out n ca_before leaf_before seat_before
    ca_before="$(fingerprint x30-bus "/var/lib/teamchat-admin/${TEAM}/tls/ca.crt")" || return 1
    leaf_before="$(fingerprint x30-bus "/var/lib/teamchat-tls/${TEAM}/leaf.crt")" || return 1
    seat_before="$(seat_sum)" || return 1
    out="$(as_issuer x30-bus "teamchat-admin backup --team ${TEAM}")" || return 1
    if [[ ! "${out}" =~ \"backup\":([0-9]+) ]]; then
        printf '[FATAL] backup gave no number: %s\n' "${out}" >&2
        return 1
    fi
    n="${BASH_REMATCH[1]}"
    (umask 077 && as_issuer x30-bus "cat /var/lib/teamchat-admin/${TEAM}/backups/${n}.tar" >"${SECRETS}/backup.tar") || return 1
    [[ -s "${SECRETS}/backup.tar" ]] || return 1
    vmtest member-down x30-bus || return 1
    boot_bus || return 1
    bus_net 11 || return 1
    bus_host_setup || return 1
    fixture x30-bus block-ssh-out || return 1
    fixture x30-member hosts "${DNS_NAME}" "${BUS_N1}" || return 1
    fixture x30-router hosts "${DNS_NAME}" "${BUS_N2}" || return 1
    write_inventory "${SECRETS}/inventory.yml" || return 1
    play_bus '[]' || return 1
    gssh x30-bus "sudo install -D -m 0600 /dev/stdin /var/lib/teamchat-admin/${TEAM}/backups/${n}.tar" <"${SECRETS}/backup.tar" || return 1
    if ! out="$(as_issuer x30-bus "teamchat-admin restore --team ${TEAM} --backup ${n}" 2>&1)"; then
        printf '%s\n' "${out}"
        if [[ "${out}" == *DC8* ]]; then
            NOT_RUN+=("M1-M4: teamchat-admin restore is refused in the production build until DC8 (${out})")
            return 0
        fi
        return 1
    fi
    play_bus "" || return 1
    check M1 "the team CA is the same after the move" test "${ca_before}" = "$(fingerprint x30-bus "/var/lib/teamchat-admin/${TEAM}/tls/ca.crt")"
    check M2 "the leaf is new after the move" test "${leaf_before}" != "$(fingerprint x30-bus "/var/lib/teamchat-tls/${TEAM}/leaf.crt")"
    check M3 "the member's seat is untouched" test "${seat_before}" = "$(seat_sum)"
    check M4 "the workers talk after the move with no reissue" w2w x30-member x30-router "${ROUTER_WORKER}" moved
}

# ── run ───────────────────────────────────────────────────────────────────────────────────
printf '# Plan 00246 X30 on the VM lab\n\nRun directory: %s\n\n' "${PLAN_RUN_DIR}" >"${REPORT}"
plan_on_cleanup teardown

plan_deploy_leg "preflight: tools, the lab's subnets, disk, the run's secrets" leg_preflight
plan_deploy_leg "remove a previous run's guests and links" leg_clear_leftovers
plan_deploy_leg "the three links" leg_links
plan_deploy_leg "the four guests" leg_guests
plan_deploy_leg "addresses and routes on the links" leg_networks
plan_deploy_leg "firewalld, the bus's rejecting zone, the password sudo user, the DNS name" leg_hosts
checks_networks
plan_deploy_leg "the bus's outbound ssh blocked" leg_block_ssh_out
check P3 "the bus's outbound ssh is blocked" refused tcp_open x30-bus "${MEMBER_N1}" 22
plan_deploy_leg "stage A's play: the bus with team x30, the member and router with none" leg_plays
checks_install
plan_deploy_leg "the relay-form issue of the coordinator and two workers" leg_issue
checks_workers
plan_deploy_leg "the host move with the DNS url" host_move

{
    printf '\nNot run (not built, D8; not in this scenario): the /32 check, the permissive-zone refusal, C11, C12.\n'
    printf 'Bridged by the scenario: the pool seat taken into a session by x30-guest.bash stage-seat (no production take, Task 4.2).\n'
    printf 'Coordinator %s: declared and issued; no session of it ran.\n' "${COORDINATOR}"
} >>"${REPORT}"
for item in "${NOT_RUN[@]+"${NOT_RUN[@]}"}"; do
    note "[NOT RUN] ${item}"
done
verdict=pass
if [[ "${FAILED}" -gt 0 ]]; then
    verdict=fail
elif [[ "${#NOT_RUN[@]}" -gt 0 ]]; then
    verdict=could-not-establish
fi
note "X30-LAB passed=${PASSED} failed=${FAILED} not_run=${#NOT_RUN[@]} verdict=${verdict}"
case "${verdict}" in
    fail)
        plan_list_reports
        exit 1
        ;;
    could-not-establish)
        plan_list_reports
        exit 2
        ;;
    *) plan_finish ;;
esac
