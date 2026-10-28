#!/usr/bin/env bash
# The black-box cases of the deleted vmtest suites (test-vmtest-member.bash and
# test-vmtest-host-only-gate.bash at 2c9878497^), re-pointed from the sourced bash at the
# built Go binary. Each case label and assertion is the suite's; what changed is vmtest_do:
# where the bash suite replaced internal functions (load_lab, base_facts, probe_upstream,
# freshness_verdict, helper, cmd_build_base), this run gives the binary a real lab tree and
# fakes only at its process seams: virsh, virt-install, qemu-img, ssh, curl and gpg2 on PATH
# and the Python helper modules through VMTEST_HELPERS_LIB.
#
# Run inside `unshare -rm`: the lab is a tmpfs (the disk guard wants 22 GiB free, which the
# scratch disk has not) and /usr/share is an overlay whose upper layer is a tmpfs, so the
# binary's fixed GPG key directory can exist without anything outside scratch being written.
#
#   (cd src-go && go build -o <scratch>/vmtest ./cmd/vmtest)
#   unshare -rm bash blackbox-vmtest.bash <scratch>/vmtest <scratch>/bb-run
#
# For the build of the cut-over commit (`git archive 2c9878497 src-go`, built the same way), set
# BB_CUTOVER=1: its validator marker had no audience field.
#
# The work directory must not exist yet (under untracked/scratch/). `set -e` is deliberately
# not used, as in the suites: every case runs and each result is checked explicitly.
set -uo pipefail

if [ $# -ne 2 ]; then
    echo "usage: unshare -rm bash $0 <vmtest-binary> <new-work-dir>" >&2
    exit 64
fi
VMTEST_BIN="$1"
work="$2"
mkdir "$work" || exit 2
work="$(cd "$work" && pwd)"
mkdir -p "$work/lab" "$work/ovl"
mount -t tmpfs -o size=64g none "$work/lab" || exit 2
mount -t tmpfs none "$work/ovl" || exit 2
mkdir -p "$work/ovl/upper" "$work/ovl/work"
mount -t overlay overlay -o "lowerdir=/usr/share,upperdir=$work/ovl/upper,workdir=$work/ovl/work" /usr/share || exit 2
mkdir -p /usr/share/distribution-gpg-keys/fedora || exit 2

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

stubs="$work/bin"
state="$work/state"
mkdir -p "$stubs" "$state"
export STUB_STATE="$state"

# The suite's four stubs, unchanged.
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
if [ -e "$STUB_STATE/ssh-hangs" ]; then exec sleep 60; fi
exit "$(cat "$STUB_STATE/ssh-rc")"
EOF
# Nothing may reach the network: a build that gets as far as a download fails here.
cat >"$stubs/curl" <<'EOF'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >>"$STUB_STATE/calls"
exit 1
EOF
cat >"$stubs/gpg2" <<'EOF'
#!/usr/bin/env bash
printf 'gpg2 %s\n' "$*" >>"$STUB_STATE/calls"
exit 1
EOF
chmod +x "$stubs"/*

# The helper seam: the modules the member, ensure-base and run paths call, as fakes that
# answer what the bash suite's replaced functions answered.
hl="$work/helpers"
mkdir -p "$hl/helpers/vmtest"
: >"$hl/helpers/__init__.py"
: >"$hl/helpers/vmtest/__init__.py"
cat >"$hl/helpers/vmtest/validate_manifest.py" <<'EOF'
import os, sys
sys.stdin.read()
a = sys.argv[1:]
with open(os.environ["STUB_STATE"] + "/calls", "a") as f:
    f.write("HELPER validate_manifest " + " ".join(a) + "\n")
if "--base" in a:
    k = a[a.index("--base") + 1]
    print(f"VMTEST-BASE key={k} name={k}-44 kind=fast profile=server tree=t vcpus=2 ram_mib=4096")
elif "--scenario" in a:
    i = a[a.index("--scenario") + 1]
    # The audience field came after the cut-over; BB_CUTOVER=1 drives the cut-over build.
    audience = "" if os.environ.get("BB_CUTOVER") == "1" else " audience=developer"
    print(f"VMTEST-SCENARIO id={i} base=server-fast base_name=server-fast-44 profile=server planned=1 "
          f"max_skipped=0 runnable=true host_only={os.environ.get('HOST_ONLY', 'false')} "
          f"reboot_before_checks=false run_env=-{audience}")
EOF
# The cut-over build asked retention's floor before a member boots; the bash suite's helper
# stand-in answered every helper 0, so this one passes the floor.
cat >"$hl/helpers/vmtest/retention.py" <<'EOF'
import os, sys
with open(os.environ["STUB_STATE"] + "/calls", "a") as f:
    f.write("HELPER retention " + " ".join(sys.argv[1:]) + "\n")
EOF
cat >"$hl/helpers/vmtest/probe_upstream.py" <<'EOF'
import os, sys
with open(os.environ["STUB_STATE"] + "/calls", "a") as f:
    f.write("HELPER probe_upstream\n")
print("VMTEST-FRESHNESS-REVISION 1")
EOF
cat >"$hl/helpers/vmtest/freshness_gate.py" <<'EOF'
import os, sys
a = sys.argv[1:]
with open(os.environ["STUB_STATE"] + "/calls", "a") as f:
    f.write("HELPER freshness_gate " + ("record-only" if "--record-only" in a else "verdict") + "\n")
if "--record-only" in a:
    disk = os.path.join(os.path.dirname(a[a.index("--base-json") + 1]), "base.qcow2")
    st = os.stat(disk)
    print(f"VMTEST-BASE-RECORD name=server-fast-44 kind=fast profile=server base_size={st.st_size} "
          f"base_mtime={int(st.st_mtime)} base_sha256={'0' * 64}")
    sys.exit(0)
print("VMTEST-FRESHNESS-VERDICT decision=stub\tdegraded=no\tdivergences=0")
sys.exit(int(os.environ.get("GATE_STUB_RC", "0")))
EOF

lab="$work/lab"
mkdir -p "$lab/ssh" "$lab/bases/server-fast-44"
: >"$lab/ssh/lab-key"
: >"$lab/ssh/lab-key.pub"
: >"$lab/bases/server-fast-44/base.qcow2"
: >"$lab/bases/server-fast-44/base.json"
: >"$lab/guest-cleanup.bash"
echo '{}' >"$lab/scenarios.json"
printf 'VMTEST_FEDORA_VERSION=44\nVMTEST_REPO_URL=https://example.com/lab.git\nVMTEST_REPO_BRANCH=main\n' >"$lab/lab.env"

reset_state() {
    rm -rf "${state:?}"/* "$lab/members"
    : >"$state/domains"
    : >"$state/calls"
    echo running >"$state/domstate"
    echo 0 >"$state/ssh-rc"
}

# vmtest_do <gate-rc> <command...> — run the Go binary once with the stubs and fakes.
vmtest_do() {
    local gate_rc="$1"
    shift
    PATH="$stubs:$PATH" VMTEST_HOME="$lab" VMTEST_HELPERS_LIB="$hl" GATE_STUB_RC="$gate_rc" \
        "$VMTEST_BIN" "$@" >"$work/out" 2>"$work/err"
}
calls() { awk -v p="$1" '$0 ~ p { n++ } END { print n + 0 }' "$state/calls"; }
exists() { if [ -e "$1" ]; then echo yes; else echo no; fi; }

echo "== test-vmtest-member.bash, against the Go binary =="
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
rc=$?
[ -z "${BB_DEBUG:-}" ] || { cat "$work/err"; cat "$state/calls"; }
check "boots: exits 0" "0" "$rc"
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
# A wrong argument count is a usage error, exit 64, in the Go (SPEC-vmtest E2); the bash's die gave 1.
check "no base key: refused (usage, 64 by E2)" "64" "$?"
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
check "a guest that stops before SSH: its SSH port removed" "no" "$(exists "$lab/members/m3/ssh-port")"
vmtest_do 0 member-ssh m3 true
check "a member that never came up: member-ssh refused" "1" "$?"
check "a member that never came up: no ssh" "0" "$(calls '^ssh .* true$')"
check "a guest that stops before SSH: its directory kept for the console log" "yes" "$(exists "$lab/members/m3")"

reset_state
: >"$state/virt-install-fails"
vmtest_do 0 member-up m4 server-fast
check "virt-install fails: fails" "1" "$?"
check "virt-install fails: its overlay removed" "no" "$(exists "$lab/members/m4/overlay.qcow2")"

echo "member-up interrupted"
reset_state
echo 255 >"$state/ssh-rc"
PATH="$stubs:$PATH" VMTEST_HOME="$lab" VMTEST_HELPERS_LIB="$hl" GATE_STUB_RC=0 \
    "$VMTEST_BIN" member-up m6 server-fast >"$work/out" 2>"$work/err" &
upPid=$!
upWaiting=yes
for ((i = 0; i < 100; i++)); do
    if [ -e "$lab/members/m6/ssh-port" ]; then
        upWaiting=no
        break
    fi
    if ! kill -0 "$upPid" 2>/dev/null; then
        upWaiting=exited
        break
    fi
    sleep 0.1
done
check "member-up reached its SSH wait before the signal" "no" "$upWaiting"
kill -TERM "$upPid"
upRc=0
wait "$upPid" || upRc=$?
check "SIGTERM during member-up: exits 143, never 0" "143" "$upRc"
check "SIGTERM during member-up: the guest destroyed" "1" "$(calls 'destroy vmtest-member-m6')"
check "SIGTERM during member-up: the guest undefined" "1" "$(calls 'undefine --nvram vmtest-member-m6')"
check "SIGTERM during member-up: overlay and SSH port removed" "no no" \
    "$(exists "$lab/members/m6/overlay.qcow2") $(exists "$lab/members/m6/ssh-port")"

echo "ensure-base"
# The bash suite replaced cmd_build_base with a line in the call log, so "rebuilt" and "built"
# were the dispatch alone and exited 0. Against the binary the real build runs; seen from
# outside, it is entered when it says so and asks the probe for the base's media, and it then
# fails (exit 1) because the fakes publish none. The dispatch is what these cases pin.
reset_state
vmtest_do 0 ensure-base server-fast
check "a usable base: exits 0" "0" "$?"
check "a usable base: not rebuilt" "0" "$(grep -c 'probe listed no artefact' "$work/err")"
check "a usable base: reported current" "VMTEST-BASE-CURRENT server-fast-44 decision=stub" "$(cat "$work/out")"

reset_state
vmtest_do 1 ensure-base server-fast
check "a base the gate refuses: build entered (exit 1, no media)" "1" "$?"
check "a base the gate refuses: rebuilt" "1 1" "$(grep -c 'may not be used (freshness: stub): rebuilding it' "$work/err") $(grep -c 'probe listed no artefact for server-fast-44' "$work/err")"

reset_state
vmtest_do 0 ensure-base server-other
check "an absent base: build entered (exit 1, no media)" "1" "$?"
check "an absent base: built" "1 1" "$(grep -c 'base server-other-44 is absent: building it' "$work/err") $(grep -c 'probe listed no artefact for server-other-44' "$work/err")"
check "no build reached a download" "0" "$(calls '^(curl|gpg2)')"

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
check "with SSH keepalives" "1" "$(calls 'ServerAliveInterval=15 -o ServerAliveCountMax=4')"

: >"$state/ssh-hangs"
VMTEST_MEMBER_SSH_TIMEOUT=1 vmtest_do 0 member-ssh m5 'dnf -y install git-core'
check "a command that never finishes: ended by the timeout (124)" "124" "$?"
check "a command that never finishes: says so" "1" "$(grep -c 'did not finish within 1s' "$work/err")"

VMTEST_MEMBER_SSH_TIMEOUT=soon vmtest_do 0 member-ssh m5 true
check "a malformed timeout: refused" "1" "$?"

echo
echo "== test-vmtest-host-only-gate.bash, against the Go binary (vmtest run) =="
# The bash suite called the extracted host_only_preflight with the lists in variables. Here
# the gate is reached as a user reaches it, `vmtest run <host-only scenario> --github-*`,
# with the lists in the lab. A permitted run gets past the gate to the base check, which then
# stops it (the base is absent): "permitted" is that line and no refusal; "refused" is exit 1
# before it.
SCENARIO="server-github-token"
SECRET_VALUE="ghp_UNIQUEtokenVALUE0123456789"
PASSPHRASE_VALUE="correct-horse-battery-staple-UNIQUE"
ALLOWLIST="$lab/scenarios.allowlist"
HOST_ONLY_LIST="$lab/scenarios.host-only"
sec="$work/secrets"
mkdir -p "$sec"
rm -rf "$lab/bases/server-fast-44"

reset() {
    reset_state
    printf 'server-fast-provision\n' >"$ALLOWLIST"
    printf '%s\n' "$SCENARIO" >"$HOST_ONLY_LIST"
    TOKEN_FILE="$sec/gh-token"
    SSH_PP_FILE="$sec/ssh-pass"
    printf '%s\n' "$SECRET_VALUE" >"$TOKEN_FILE"
    printf '%s\n' "$PASSPHRASE_VALUE" >"$SSH_PP_FILE"
    chmod 600 "$TOKEN_FILE" "$SSH_PP_FILE"
    GITHUB_ACCOUNT="throwaway-lab-user"
    unset VMTEST_FROM_BRIDGE
}
run_gate() {
    local rc=0
    # A missing credential is the option left off the command line; an option given an empty
    # value is a usage error (exit 64, SPEC-vmtest E2), checked once below.
    local -a opts=(--commit 0123456789abcdef0123456789abcdef01234567)
    [ -z "$2" ] || opts+=(--github-account "$2")
    [ -z "$3" ] || opts+=(--github-token-file "$3")
    [ -z "$4" ] || opts+=(--github-ssh-passphrase-file "$4")
    HOST_ONLY=true vmtest_do 0 run "$1" "${opts[@]}" || rc=$?
    DIE_MESSAGE="$(cat "$work/err")"
    PAST_GATE="$(grep -c 'is absent (stage: base)' "$work/err")"
    return "$rc"
}
refused() { if [ "$1" = 1 ] && [ "$PAST_GATE" = 0 ]; then echo 1; else echo "rc=$1 past_gate=$PAST_GATE"; fi; }

reset
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
rc=$?
check "a correctly prepared host-only run is permitted (past the gate to the base check)" "1 1" "$rc $PAST_GATE"
check "a permitted run says nothing of the gate" "0" "$(grep -c 'host-only\|--github\|secret file\|bridge' <<<"$DIE_MESSAGE")"

reset
export VMTEST_FROM_BRIDGE=1
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a run launched by the bridge is refused" "1" "$(refused $?)"
case "$DIE_MESSAGE" in
    *bridge*) check "the refusal says it came from the bridge" "yes" "yes" ;;
    *) check "the refusal says it came from the bridge" "yes" "no: ${DIE_MESSAGE}" ;;
esac

reset
printf 'something-else\n' >"$HOST_ONLY_LIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a scenario absent from the host-only list is refused" "1" "$(refused $?)"

reset
rm -f "$HOST_ONLY_LIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a missing host-only list is refused, not treated as permission" "1" "$(refused $?)"

reset
printf 'server-fast-provision\n%s\n' "$SCENARIO" >"$ALLOWLIST"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a scenario on BOTH enumerations is refused" "1" "$(refused $?)"
case "$DIE_MESSAGE" in
    *both*) check "the refusal names the disjointness breach" "yes" "yes" ;;
    *) check "the refusal names the disjointness breach" "yes" "no: ${DIE_MESSAGE}" ;;
esac

reset
GITHUB_ACCOUNT=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a missing --github-account is refused" "1" "$(refused $?)"

reset
HOST_ONLY=true vmtest_do 0 run "$SCENARIO" --github-account "" --github-token-file "$TOKEN_FILE" \
    --github-ssh-passphrase-file "$SSH_PP_FILE"
check "an empty --github-account value is a usage error (64, E2)" "64" "$?"

reset
TOKEN_FILE=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a missing --github-token-file is refused" "1" "$(refused $?)"

reset
SSH_PP_FILE=""
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a missing --github-ssh-passphrase-file is refused" "1" "$(refused $?)"

reset
TOKEN_FILE="$sec/does-not-exist"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "an unreadable token file is refused" "1" "$(refused $?)"

reset
: >"$TOKEN_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "an empty token file is refused" "1" "$(refused $?)"

reset
: >"$SSH_PP_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "an empty passphrase file is refused" "1" "$(refused $?)"

reset
chmod 644 "$TOKEN_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a group/world-readable token file is refused" "1" "$(refused $?)"

reset
chmod 640 "$SSH_PP_FILE"
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "a group-readable passphrase file is refused" "1" "$(refused $?)"

reset
GITHUB_ACCOUNT='user; rm -rf ~'
run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
check "an account name outside the grammar is refused" "1" "$(refused $?)"

leaked=0
did_not_refuse=0
for spoil in empty-token bad-mode from-bridge both-lists; do
    reset
    case "$spoil" in
        empty-token) : >"$TOKEN_FILE" ;;
        bad-mode) chmod 644 "$TOKEN_FILE" ;;
        from-bridge) export VMTEST_FROM_BRIDGE=1 ;;
        both-lists) printf '%s\n' "$SCENARIO" >>"$ALLOWLIST" ;;
    esac
    run_gate "$SCENARIO" "$GITHUB_ACCOUNT" "$TOKEN_FILE" "$SSH_PP_FILE"
    if [ "$(refused $?)" != 1 ]; then
        did_not_refuse=$((did_not_refuse + 1))
    fi
    case "$DIE_MESSAGE$(cat "$work/out")" in
        *"$SECRET_VALUE"* | *"$PASSPHRASE_VALUE"*) leaked=$((leaked + 1)) ;;
    esac
done
check "every spoiled case actually refused" "0" "$did_not_refuse"
check "no refusal echoes a secret value" "0" "$leaked"
check "no host-only case reached ssh, virt-install or the network" "0" "$(calls '^(ssh|virt-install|curl|gpg2)')"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
