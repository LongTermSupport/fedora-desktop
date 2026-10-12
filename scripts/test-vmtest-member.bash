#!/usr/bin/env bash
# Unit-test vmtest's member verbs: member-up, member-ssh and member-down.
#
# WHY THIS EXISTS. A member is a lab guest that outlives the vmtest process: another script
# (Plan 00161's acceptance, its U23.vm leg) boots it, drives it over SSH, and removes it. So
# the properties that matter are lifecycle ones, and each would fail silently on a real
# host: a member-up that fails part-way must leave no guest running; a member-down must
# remove the guest and its directory, and be a no-op (not an error) when there is nothing;
# a name must never reach a path or a domain name unvalidated. member-ssh must hand the
# remote command's exit status back unchanged, since the caller judges the guest by it.
#
# The test sources the real vmtest (its main runs only when executed), puts stub virsh,
# virt-install, qemu-img and ssh first on PATH, and replaces only the lab-state functions
# that would read a deployed lab or the network (load_lab, base_facts, probe_upstream,
# freshness_verdict, helper).
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VMTEST="$SCRIPT_DIR/../files/home/.local/bin/vmtest"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        echo "  PASS: $label"
    else
        failed=$((failed + 1))
        echo "  FAIL: $label → '$got' (wanted '$want')"
    fi
}

# ── stubs: state in files under $work/state, every call appended to $work/state/calls ──
stubs="$work/bin"
state="$work/state"
mkdir -p "$stubs" "$state"
export STUB_STATE="$state"

cat >"$stubs/virsh" <<'EOF'
#!/usr/bin/env bash
printf 'virsh %s\n' "$*" >>"$STUB_STATE/calls"
shift 2 # -c <uri>
case "$1" in
    list) cat "$STUB_STATE/domains" ;;
    domstate) cat "$STUB_STATE/domstate" ;;
    undefine)
        grep -vFx -- "${@: -1}" "$STUB_STATE/domains" >"$STUB_STATE/domains.new"
        mv "$STUB_STATE/domains.new" "$STUB_STATE/domains"
        ;;
esac
exit 0
EOF
cat >"$stubs/virt-install" <<'EOF'
#!/usr/bin/env bash
printf 'virt-install %s\n' "$*" >>"$STUB_STATE/calls"
if [ -e "$STUB_STATE/virt-install-fails" ]; then exit 1; fi
while [ $# -gt 0 ]; do
    if [ "$1" = "--name" ]; then echo "$2" >>"$STUB_STATE/domains"; fi
    shift
done
EOF
cat >"$stubs/qemu-img" <<'EOF'
#!/usr/bin/env bash
printf 'qemu-img %s\n' "$*" >>"$STUB_STATE/calls"
: >"${@: -1}"
EOF
cat >"$stubs/ssh" <<'EOF'
#!/usr/bin/env bash
printf 'ssh %s\n' "$*" >>"$STUB_STATE/calls"
exit "$(cat "$STUB_STATE/ssh-rc")"
EOF
chmod +x "$stubs"/*

lab="$work/lab"
mkdir -p "$lab/ssh" "$lab/bases/server-fast-44"
: >"$lab/ssh/lab-key"
: >"$lab/ssh/lab-key.pub"
: >"$lab/bases/server-fast-44/base.qcow2"
: >"$lab/bases/server-fast-44/base.json"

reset_state() {
    rm -rf "${state:?}"/* "$lab/members"
    : >"$state/domains"
    : >"$state/calls"
    echo running >"$state/domstate"
    echo 0 >"$state/ssh-rc"
}

# vmtest_do <gate-rc> <command...> — run one vmtest command in a fresh shell with the stubs.
vmtest_do() {
    local gate_rc="$1"
    shift
    PATH="$stubs:$PATH" VMTEST_HOME="$lab" GATE_STUB_RC="$gate_rc" bash -c '
        . "$1"
        shift
        load_lab() { mkdir -p "${IMAGES_DIR}" "${BASES_DIR}"; }
        base_facts() { BASE_NAME="$1-44"; BASE_KIND=fast; BASE_PROFILE=server; BASE_VCPUS=2; BASE_RAM_MIB=4096; }
        probe_upstream() { PROBE_FILE="$(mktemp)"; }
        freshness_verdict() { GATE_RC="${GATE_STUB_RC}"; GATE_DECISION=stub; GATE_BASE_SIZE=0; GATE_LINE=stub; }
        helper() { return 0; }
        main "$@"
    ' _ "$VMTEST" "$@" >"$work/out" 2>"$work/err"
}
# calls <ERE> — how many recorded stub calls match.
calls() { awk -v p="$1" '$0 ~ p { n++ } END { print n + 0 }' "$state/calls"; }
exists() { if [ -e "$1" ]; then echo yes; else echo no; fi; }

echo "member names"
reset_state
for bad in "Bad" "../x" "-x" "a/b" "" "a_b"; do
    vmtest_do 0 member-down "$bad"
    check "member-down refuses the name '$bad'" "1" "$?"
done
check "no refused name reached virsh" "0" "$(calls '^virsh')"

echo "member-down"
reset_state
vmtest_do 0 member-down agent-bus-acceptance-u23
check "nothing there: exits 0" "0" "$?"
check "nothing there: no destroy or undefine" "0" "$(calls 'destroy|undefine')"
check "nothing there: reports the member down" "VMTEST-MEMBER-DOWN agent-bus-acceptance-u23" "$(cat "$work/out")"

reset_state
echo vmtest-member-agent-bus-acceptance-u23 >"$state/domains"
mkdir -p "$lab/members/agent-bus-acceptance-u23"
: >"$lab/members/agent-bus-acceptance-u23/overlay.qcow2"
vmtest_do 0 member-down agent-bus-acceptance-u23
check "a running member: exits 0" "0" "$?"
check "a running member: destroyed" "1" "$(calls 'destroy vmtest-member-agent-bus-acceptance-u23')"
check "a running member: undefined" "1" "$(calls 'undefine --nvram vmtest-member-agent-bus-acceptance-u23')"
check "a running member: its directory removed" "no" "$(exists "$lab/members/agent-bus-acceptance-u23")"

reset_state
echo vmtest-member-m0 >"$state/domains"
echo "shut off" >"$state/domstate"
vmtest_do 0 member-down m0
check "a stopped member: exits 0" "0" "$?"
check "a stopped member: not destroyed again" "0" "$(calls 'destroy')"
check "a stopped member: undefined" "1" "$(calls 'undefine --nvram vmtest-member-m0')"

echo "member-up"
reset_state
vmtest_do 0 member-up m1 server-fast
check "boots: exits 0" "0" "$?"
check "boots: reports the member up" "VMTEST-MEMBER-UP m1 domain=vmtest-member-m1" "$(cat "$work/out")"
check "boots: the guest is left running" "vmtest-member-m1" "$(cat "$state/domains")"
check "boots: its overlay is in its directory" "yes" "$(exists "$lab/members/m1/overlay.qcow2")"
check "boots: its SSH port is recorded" "yes" "$(if grep -qE '^[0-9]+$' "$lab/members/m1/ssh-port"; then echo yes; else echo no; fi)"
check "boots: on a passt port forward" "1" "$(calls 'backend[.]type=passt')"

vmtest_do 0 member-up m1 server-fast
check "a member of that name exists: refused" "1" "$?"
check "a member of that name exists: no second boot" "1" "$(calls '^virt-install')"

reset_state
vmtest_do 0 member-up m1
check "no base key: refused" "1" "$?"
check "no base key: nothing booted" "0" "$(calls '^virt-install')"

reset_state
vmtest_do 1 member-up m2 server-fast
check "a base the freshness gate refuses: refused" "1" "$?"
check "a base the freshness gate refuses: nothing booted" "0" "$(calls '^virt-install')"
check "a base the freshness gate refuses: names ensure-base" "1" "$(grep -c 'vmtest ensure-base server-fast' "$work/err")"

reset_state
echo "shut off" >"$state/domstate"
vmtest_do 0 member-up m3 server-fast
check "a guest that stops before SSH: fails" "1" "$?"
check "a guest that stops before SSH: undefined" "1" "$(calls 'undefine --nvram vmtest-member-m3')"
check "a guest that stops before SSH: its overlay removed" "no" "$(exists "$lab/members/m3/overlay.qcow2")"
check "a guest that stops before SSH: its directory kept for the console log" "yes" "$(exists "$lab/members/m3")"

reset_state
: >"$state/virt-install-fails"
vmtest_do 0 member-up m4 server-fast
check "virt-install fails: fails" "1" "$?"
check "virt-install fails: its overlay removed" "no" "$(exists "$lab/members/m4/overlay.qcow2")"

echo "member-ssh"
reset_state
vmtest_do 0 member-ssh nobody true
check "no such member: refused" "1" "$?"
check "no such member: no ssh" "0" "$(calls '^ssh')"

reset_state
mkdir -p "$lab/members/m5"
echo 40123 >"$lab/members/m5/ssh-port"
echo 7 >"$state/ssh-rc"
vmtest_do 0 member-ssh m5 'sudo -n true'
check "the remote command's status is returned" "7" "$?"
check "on the member's port, as the cloud user, with the lab key" "1" "$(calls "-i $lab/ssh/lab-key -p 40123 fedora@127[.]0[.]0[.]1 sudo -n true$")"

if [ "$failed" -ne 0 ]; then
    echo "  last stderr:"
    cat "$work/err"
fi
echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
