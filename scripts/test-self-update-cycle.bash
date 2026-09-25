#!/usr/bin/env bash
# Drive the real files/usr/local/sbin/fedora-desktop-self-update end to end (Plan 00137).
#
# WHY THIS EXISTS. The cycle's decisions are unit-tested against a fake host in
# tests/helpers/self_update/test_cycle.py. What those tests cannot see is the real
# wiring: the root wrapper's argument and root checks, the helpers imported from the
# deploy clone, a real signed fetch and fast-forward, the play lock actually held while a
# play runs, the become password reaching the play as a readable inherited descriptor, and
# `systemctl reboot` coming last. So the wrapper runs for real under
# FEDORA_DESKTOP_SELF_UPDATE_TEST_PREFIX, which roots every path under a scratch
# directory and puts stub `runuser` and `systemctl` first on PATH. The stubs log what they
# were asked; nothing is rebooted and nothing runs as another user.
#
# The clone is a real git repository holding a copy of the working tree's helpers, with
# a bare origin reached through an https URL rewritten by insteadOf, and commits signed by
# a throwaway SSH key. Its Fedora pin is this machine's os-release VERSION_ID, so the
# updater's version gate passes wherever the test runs.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.

set -uo pipefail

# Every git call here, the wrapper's included, sees only the repositories' own config. A
# machine whose global config signs every commit would otherwise sign the "unsigned"
# fixtures with the trusted key, and the refusal cases would test nothing.
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
unset GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TOOL="$REPO_ROOT/files/usr/local/sbin/fedora-desktop-self-update"
PLAY="playbooks/imports/play-claude-yolo.yml"
REMOTE_URL="https://example.invalid/fedora-desktop.git"
PRINCIPAL="owner@example.com"

if [ ! -x "$TOOL" ]; then
    echo "FAIL: $TOOL is missing or not executable" >&2
    exit 1
fi
for tool in git ssh-keygen flock python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is not on PATH; this test needs it" >&2
        exit 1
    fi
done

passed=0
failed=0
check() {
    local label="$1" want="$2" got="$3"
    if [ "$got" = "$want" ]; then
        passed=$((passed + 1))
        printf '  PASS  %s\n' "$label"
    else
        failed=$((failed + 1))
        printf '  FAIL  %s\n        want: %q\n        got:  %q\n' "$label" "$want" "$got" >&2
    fi
}

# The fixture's deploy clone stands in for /var/lib/fedora-desktop/deploy, which is outside
# every home, and the cycle refuses code search paths under the user's home. So the scratch
# tree must be outside the home too. A checkout under ~/ (a desktop, a CI runner) would put
# it there, so it lives in the system temp directory instead.
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/self-update-cycle-test.XXXXXX")"
case "$SCRATCH/" in
    "$(getent passwd "$(id -un)" | cut -d: -f6)"/*)
        echo "FAIL: the scratch tree $SCRATCH is under this user's home; set TMPDIR outside it" >&2
        rm -rf "$SCRATCH"
        exit 1
        ;;
esac
OUTSIDE=""
cleanup() {
    rm -rf "$SCRATCH"
    if [ -n "$OUTSIDE" ]; then rm -rf "$OUTSIDE"; fi
}
trap cleanup EXIT

# The isolation above is what the refusal cases stand on, so it is proven before any case
# runs: a commit made without -S must come out unsigned.
PROBE="$SCRATCH/isolation-probe"
git init -q "$PROBE"
# A probe that made no commit proves nothing, so that is a failure too, not a pass: git
# config naming a missing signing key fails the commit, and an absent HEAD has no gpgsig.
if ! git -C "$PROBE" -c user.name=Probe -c user.email="$PRINCIPAL" commit -q --allow-empty -m probe ||
    ! git -C "$PROBE" rev-parse -q --verify HEAD >/dev/null; then
    echo "FAIL: the isolation probe could not make a commit, so whether this machine's git" >&2
    echo "      config reaches the fixtures is unknown (see git's error above)" >&2
    exit 1
fi
if git -C "$PROBE" cat-file commit HEAD | grep -q '^gpgsig'; then
    echo "FAIL: a commit made without -S came out signed; this machine's git config reaches" >&2
    echo "      the fixtures, so the unsigned-commit cases cannot be trusted" >&2
    exit 1
fi
rm -rf "$PROBE"

PREFIX="$SCRATCH/root"
BIN="$PREFIX/bin"
ETC="$PREFIX/etc/fedora-desktop"
CLONE="$PREFIX/var/lib/fedora-desktop/deploy"
STATE="$PREFIX/var/lib/fedora-desktop/self-update"
PUBLISHED="$PREFIX/var/lib/fedora-desktop/self-update-status"
LOG="$SCRATCH/calls.log"
RUNTIME="$SCRATCH/runtime"
ORIGIN="$SCRATCH/origin.git"
WORK="$SCRATCH/work"
mkdir -p "$BIN" "$PREFIX/etc" "$RUNTIME"
chmod 700 "$RUNTIME"

# ── the fakes ─────────────────────────────────────────────────────────────────────────
# runuser: `runuser -u USER -- env -i K=V... cmd args`. It logs the command as the user
# would run it. For a play it also logs the become password read from the inherited
# descriptor the way run.bash reads it, the vault password read by REOPENING /dev/fd/N the
# way ansible opens its vault password file, the pinned ansible and fact cache the play is
# handed, and whether the play lock is held by someone else — the cycle — at that moment.
# `sh -c` (the cycle asking what ansible-playbook resolves to) really runs, in the given
# environment. FAKE_PLAY_RC / FAKE_NOTIFY_RC / FAKE_VERIFY_RC set the other answers.
cat >"$BIN/runuser" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "$1" != "-u" ] || [ "$3" != "--" ] || [ "$4" != "env" ] || [ "$5" != "-i" ]; then
    echo "runuser stub: unexpected argv: $*" >&2
    exit 99
fi
shift 5
declare -a pairs=()
declare -A given=()
while [ $# -gt 0 ] && [[ "$1" == *=* ]]; do
    pairs+=("$1")
    given["${1%%=*}"]="${1#*=}"
    shift
done
command="$(basename "$1")"
case "$command" in
sh | ansible-config) exec env -i "${pairs[@]}" "$@" ;;
esac
shift
case "$command" in
run.bash)
    password="$(cat <&"${given[RUN_BASH_SUDO_PASSWORD_FILE]#/dev/fd/}")"
    vault="$(cat "${given[ANSIBLE_VAULT_PASSWORD_FILE]}")"
    lock_open=no
    if [ -e "/dev/fd/${given[FEDORA_DESKTOP_PLAY_LOCK_FD]}" ]; then lock_open=yes; fi
    held=yes
    if flock -n "${given[XDG_RUNTIME_DIR]}/fedora-desktop-plays.lock" true; then held=no; fi
    echo "play $* become=$password vault=$vault lock-fd-open=$lock_open lock-held=$held" >>"$FAKE_LOG"
    echo "handed ansible=${given[RUN_BASH_ANSIBLE_PLAYBOOK]} path=${given[PATH]%%:*} cache=${given[ANSIBLE_CACHE_PLUGIN]} collections=${given[ANSIBLE_COLLECTIONS_PATH]}" >>"$FAKE_ENV_LOG"
    home_searched=""
    for name in "${!given[@]}"; do
        [[ "$name" == ANSIBLE_* ]] || continue
        IFS=: read -r -a searched <<<"${given[$name]}"
        for directory in "${searched[@]}"; do
            if [[ "$directory" == "${given[HOME]}" || "$directory" == "${given[HOME]}"/* ]]; then home_searched+=" $name"; fi
        done
    done
    echo "searched callbacks=${given[ANSIBLE_CALLBACK_PLUGINS]} roles=${given[ANSIBLE_ROLES_PATH]} modules=${given[ANSIBLE_LIBRARY]} user-site-off=${given[PYTHONNOUSERSITE]} home:${home_searched:- none}" >>"$FAKE_ENV_LOG"
    exit "${FAKE_PLAY_RC:-0}"
    ;;
ccy-sessions)
    echo "ccy-sessions $*" >>"$FAKE_LOG"
    case "$1" in
    verify-restore) exit "${FAKE_VERIFY_RC:-0}" ;;
    *) exit "${FAKE_NOTIFY_RC:-0}" ;;
    esac
    ;;
esac
echo "runuser stub: unexpected command $command" >&2
exit 99
EOF
cat >"$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "systemctl $*" >>"$FAKE_LOG"
exit "${FAKE_REBOOT_RC:-0}"
EOF
chmod 755 "$BIN/runuser" "$BIN/systemctl"

# The pinned system ansible-core and its collections: only their ownership and what PATH
# resolves to matter, because the play itself is the runuser stub above.
SYSTEM_ANSIBLE="$PREFIX/usr/bin/ansible-playbook"
COLLECTIONS="$SCRATCH/collections"
mkdir -p "$PREFIX/usr/bin" "$COLLECTIONS"
printf '#!/bin/sh\nexit 0\n' >"$SYSTEM_ANSIBLE"
# ansible-config: the effective settings, as `dump --format json` prints them. It reports
# the module search path the cycle handed it, so the cycle's own pinning is what it judges.
# While $HOME_SETTING_FLAG exists it also reports a search path a later ansible-core added,
# defaulted under the user's home, which no pinned list names.
SYSTEM_ANSIBLE_CONFIG="$PREFIX/usr/bin/ansible-config"
HOME_SETTING_FLAG="$SCRATCH/config-dump-adds-a-home-search-path"
cat >"$SYSTEM_ANSIBLE_CONFIG" <<EOF
#!/usr/bin/env bash
set -euo pipefail
[ "\$*" = "dump --format json" ] || { echo "ansible-config stub: unexpected argv: \$*" >&2; exit 99; }
printf '[{"name": "DEFAULT_MODULE_PATH", "origin": "env", "value": ["%s"]}' "\${ANSIBLE_LIBRARY}"
if [ -e "$HOME_SETTING_FLAG" ]; then
    printf ', {"name": "FUTURE_WIDGET_PLUGIN_PATH", "origin": "default", "value": ["%s/.ansible/plugins/widget"]}' "\${HOME}"
fi
printf ', {"name": "DEFAULT_LOCAL_TMP", "origin": "default", "value": "%s/.ansible/tmp"}]\n' "\${HOME}"
EOF
chmod 755 "$SYSTEM_ANSIBLE" "$SYSTEM_ANSIBLE_CONFIG" "$COLLECTIONS"

# ── the signed history ─────────────────────────────────────────────────────────────────
ssh-keygen -q -t ed25519 -N "" -C "$PRINCIPAL" -f "$SCRATCH/signing-key"
mkdir -p "$ETC"
printf '%s namespaces="git" %s\n' "$PRINCIPAL" "$(cut -d' ' -f1,2 "$SCRATCH/signing-key.pub")" \
    >"$ETC/self-update.allowed_signers"

git_work() {
    git -C "$WORK" -c user.name=Owner -c user.email="$PRINCIPAL" -c gpg.format=ssh \
        -c user.signingkey="$SCRATCH/signing-key" "$@"
}
# commit_signed MESSAGE: commit whatever the work tree holds, signed, and push it.
commit_signed() {
    git_work add -A
    git_work commit -q -S -m "$1"
    git_work push -q origin HEAD:main
}

os_version="$(awk -F= '$1 == "VERSION_ID" { gsub(/["'\'']/, "", $2); print $2 }' /etc/os-release)"
if [[ ! "$os_version" =~ ^[0-9]+ ]]; then
    echo "FAIL: /etc/os-release has no numeric VERSION_ID; the updater's version gate cannot pass here" >&2
    exit 1
fi

git init -q --bare -b main "$ORIGIN"
git init -q -b main "$WORK"
git_work remote add origin "$ORIGIN"
mkdir -p "$WORK/helpers/self_update" "$WORK/helpers/play_lock" "$WORK/playbooks/imports" "$WORK/vars"
cp "$REPO_ROOT"/helpers/self_update/*.py "$REPO_ROOT/helpers/self_update/unattended-plays.json" \
    "$WORK/helpers/self_update/"
cp "$REPO_ROOT/helpers/play_lock/lock.py" "$WORK/helpers/play_lock/"
cp "$REPO_ROOT/.gitignore" "$WORK/"
printf 'fedora_version: %s\n' "${os_version%%.*}" >"$WORK/vars/fedora-version.yml"
printf -- '- hosts: localhost\n  tasks: []\n' >"$WORK/$PLAY"
commit_signed "base"

git clone -q -b main "$ORIGIN" "$CLONE"
git -C "$CLONE" config remote.origin.url "$REMOTE_URL"
git -C "$CLONE" config "url.$ORIGIN.insteadOf" "$REMOTE_URL"
BASE="$(git -C "$CLONE" rev-parse HEAD)"

echo "change one" >"$WORK/README"
commit_signed "first signed change"
FIRST="$(git -C "$WORK" rev-parse HEAD)"

# ── helpers ────────────────────────────────────────────────────────────────────────────
OUT="$SCRATCH/out"
ERR="$SCRATCH/err"
RC=0
# cycle ARGS...: run the real wrapper under the test prefix; sets RC, OUT and ERR.
ENV_LOG="$SCRATCH/env.log"
cycle() {
    : >"$LOG"
    : >"$ENV_LOG"
    env FEDORA_DESKTOP_SELF_UPDATE_TEST_PREFIX="$PREFIX" FEDORA_DESKTOP_SELF_UPDATE_MINUTE_SECONDS=0 \
        FAKE_LOG="$LOG" FAKE_ENV_LOG="$ENV_LOG" XDG_RUNTIME_DIR="$RUNTIME" "$TOOL" "$@" >"$OUT" 2>"$ERR"
    RC=$?
}
calls() { cat "$LOG"; }
PLAYED="play --headless $PLAY become=correct horse vault=vault words lock-fd-open=yes lock-held=yes"
has() { if [ -e "$1" ]; then echo yes; else echo no; fi; }
says() { if grep -q -- "$1" "$2"; then echo yes; else echo no; fi; }
result_key() { awk -F= -v key="$1" '$1 == key { print substr($0, length(key) + 2) }' "$STATE/last-result"; }
published_key() { awk -F= -v key="$1" '$1 == key { print substr($0, length(key) + 2) }' "$PUBLISHED/result"; }
state_key() { awk -F= -v key="$2" '$1 == key { print substr($0, length(key) + 2) }' "$STATE/$1"; }
write_owed() { printf 'boot=%s\nnew=%s\nplays=%s\n' "$1" "$FIRST" "$PLAY" >"$STATE/owed-verify"; }
BOOT_ID="$(cat /proc/sys/kernel/random/boot_id)"

# ── usage and refusals ─────────────────────────────────────────────────────────────────
echo "usage and refusals"
cycle --help
check "--help exits 0" "0" "$RC"
check "--help prints the usage" "yes" "$(says '^Usage:' "$OUT")"
cycle frobnicate
check "an unknown subcommand is a usage error (64)" "64" "$RC"
cycle
check "no subcommand is a usage error (64)" "64" "$RC"

if [ "$EUID" -eq 0 ]; then
    # The refusal is only reachable as a non-root user; run a copy as nobody.
    OUTSIDE="$(mktemp -d)"
    chmod 755 "$OUTSIDE"
    install -m 755 "$TOOL" "$OUTSIDE/fedora-desktop-self-update"
    runuser -u nobody -- "$OUTSIDE/fedora-desktop-self-update" status >"$OUT" 2>"$ERR"
    RC=$?
else
    env -u FEDORA_DESKTOP_SELF_UPDATE_TEST_PREFIX "$TOOL" status >"$OUT" 2>"$ERR"
    RC=$?
fi
check "a non-root caller is refused (77)" "77" "$RC"
check "the refusal says how to run it" "yes" "$(says 'must run as root' "$ERR")"

cycle status
check "a missing state directory is a config error (70)" "70" "$RC"
mkdir -p "$STATE"
chmod 700 "$STATE"

cycle status
check "a missing published-result directory is a config error (70)" "70" "$RC"
check "the refusal names the play to deploy" "yes" "$(says 'deploy the self-update play first' "$ERR")"
mkdir -p "$PUBLISHED"
chmod 2750 "$PUBLISHED"

cycle run
check "run with no config file is a config error (70)" "70" "$RC"
cycle status
check "so is status: nothing is imported from the clone before its HEAD is judged" "70" "$RC"

# write_config SINKS: the config the play would render, with ALERT_SINKS=SINKS.
write_config() {
    printf 'USER=%s\nBRANCH=main\nREMOTE_URL=%s\nPRINCIPAL=%s\nWARN_MINUTES=1\nALERT_SINKS=%s\nANSIBLE_COLLECTIONS_DIR=%s\n' \
        "$(id -un)" "$REMOTE_URL" "$PRINCIPAL" "$1" "$COLLECTIONS" >"$ETC/self-update.conf"
    chmod 600 "$ETC/self-update.conf"
}
write_config ""

cycle status
check "status with no history exits 0" "0" "$RC"
check "status with no history says so" "no cycle has run yet" "$(awk 'NR == 1' "$OUT")"

# ── arguments cannot override the fixed paths ─────────────────────────────────────────
# sudoers allows only four exact argument lists; this proves the helper refuses the rest
# too, so a switch to a lenient argument parser would fail here before it reached a host.
echo "arguments cannot override the fixed paths"
for extra in "run --ansible-playbook /bin/true" "run -- --config /dev/null" "status --config=/dev/null" \
    "run --dry-run --clone /tmp" "verify --state-dir /tmp"; do
    read -r -a extra_args <<<"$extra"
    cycle "${extra_args[@]}"
    check "'$extra' is a usage error (64)" "64" "$RC"
    check "'$extra' calls nothing" "" "$(calls)"
done

cycle run
check "run with no become password file is a config error (70)" "70" "$RC"
check "nothing moved without the become password file" "$BASE" "$(git -C "$CLONE" rev-parse HEAD)"
printf 'correct horse\n' >"$ETC/self-update.become"
chmod 600 "$ETC/self-update.become"
cycle run
check "run with no vault password file is a config error (70)" "70" "$RC"
check "nothing moved without the vault password file" "$BASE" "$(git -C "$CLONE" rev-parse HEAD)"
printf 'vault words\n' >"$ETC/self-update.vault"
chmod 640 "$ETC/self-update.vault"
cycle run
check "run with a vault password others can read is a config error (70)" "70" "$RC"
check "nothing was called with a readable password" "" "$(calls)"
chmod 600 "$ETC/self-update.vault"

exec 9>"$RUNTIME/fedora-desktop-plays.lock"
flock -n 9
cycle run
exec 9>&-
check "run while another play run holds the lock exits 75" "75" "$RC"
check "nothing was called while locked out" "" "$(calls)"
check "nothing moved while locked out" "$BASE" "$(git -C "$CLONE" rev-parse HEAD)"

# ── the Slack alert sink ───────────────────────────────────────────────────────────────
# A configured sink whose webhook cannot be used is refused before anything runs; a dry
# run and status never read the secret; and a real delivery attempt is made from the root
# process. That attempt goes to a stub HTTPS proxy on 127.0.0.1, which logs the CONNECT
# and refuses it, so no packet leaves the machine and the refusal is the failure the cycle
# must journal and record.
echo "the Slack alert sink"
WEBHOOK_FILE="$ETC/self-update.slack-webhook"
WEBHOOK="https://hooks.slack.com/services/EXAMPLE/EXAMPLE/EXAMPLE"
write_config slack
cycle run
check "slack configured with no webhook file: run is a config error (70)" "70" "$RC"
check "and nothing was called" "" "$(calls)"
check "and nothing moved" "$BASE" "$(git -C "$CLONE" rev-parse HEAD)"
cycle verify
check "slack configured with no webhook file: verify is a config error (70)" "70" "$RC"
cycle run --dry-run
check "a dry run never reads the webhook, so a missing one does not stop it" "0" "$RC"
check "and the dry run still records nothing" "no" "$(has "$STATE/last-result")"
cycle status
check "nor does status" "0" "$RC"
printf '%s\n' "$WEBHOOK" >"$WEBHOOK_FILE"
chmod 644 "$WEBHOOK_FILE"
cycle run
check "a webhook file others can read is a config error (70)" "70" "$RC"
check "and nothing was called" "" "$(calls)"
printf 'https://example.invalid/not-a-webhook\n' >"$WEBHOOK_FILE"
chmod 600 "$WEBHOOK_FILE"
cycle run
check "a webhook file that holds no Slack webhook is a config error (70)" "70" "$RC"
check "and the refusal does not echo what the file holds" "no" "$(says 'not-a-webhook' "$ERR")"
check "and nothing was called" "" "$(calls)"

printf '%s\n' "$WEBHOOK" >"$WEBHOOK_FILE"
PROXY_LOG="$SCRATCH/proxy.log"
PROXY_PORT_FILE="$SCRATCH/proxy.port"
cat >"$SCRATCH/proxy.py" <<'EOF'
import http.server
import os
import sys


class Refuse(http.server.BaseHTTPRequestHandler):
    def do_CONNECT(self):
        with open(sys.argv[1], "a", encoding="utf-8") as log:
            log.write(f"CONNECT {self.path}\n")
        self.send_response(403)
        self.end_headers()

    def log_message(self, *args):
        pass


server = http.server.HTTPServer(("127.0.0.1", 0), Refuse)
with open(sys.argv[2] + ".tmp", "w", encoding="utf-8") as port:
    port.write(str(server.server_address[1]))
os.replace(sys.argv[2] + ".tmp", sys.argv[2])
server.serve_forever()
EOF
python3 "$SCRATCH/proxy.py" "$PROXY_LOG" "$PROXY_PORT_FILE" &
PROXY_PID=$!
for _ in $(seq 100); do
    if [ -s "$PROXY_PORT_FILE" ]; then break; fi
    sleep 0.1
done
if [ ! -s "$PROXY_PORT_FILE" ]; then
    echo "FAIL: the stub proxy did not start" >&2
    kill "$PROXY_PID"
    exit 1
fi
PROXY="http://127.0.0.1:$(cat "$PROXY_PORT_FILE")"
write_owed "an-earlier-boot"
https_proxy="$PROXY" HTTPS_PROXY="$PROXY" no_proxy="" NO_PROXY="" FAKE_VERIFY_RC=1 cycle verify
kill "$PROXY_PID"
wait "$PROXY_PID" 2>/dev/null
check "an announced result with slack configured exits as the cycle decided (23)" "23" "$RC"
check "the root process tried to deliver it to the webhook's host" "CONNECT hooks.slack.com:443" "$(cat "$PROXY_LOG")"
check "the failed delivery is journalled" "yes" "$(says 'the alert could not be delivered: slack: not delivered' "$ERR")"
check "the failed delivery is recorded" "yes" "$(result_key alert | grep -q '^slack: not delivered' && echo yes || echo no)"
check "and published for the host-health report" "$(result_key alert)" "$(published_key alert)"
check "the outcome is still the cycle's own" "verify-failed" "$(result_key outcome)"
check "the webhook's secret path is in no output or record" "no" \
    "$(cat "$OUT" "$ERR" "$STATE/last-result" "$PUBLISHED/result" | grep -q 'EXAMPLE/EXAMPLE' && echo yes || echo no)"
rm -f "$WEBHOOK_FILE" "$STATE/last-result" "$PUBLISHED/result"
write_config ""

# ── verify ─────────────────────────────────────────────────────────────────────────────
echo "verify"
cycle verify
check "verify with nothing owed exits 0" "0" "$RC"
check "verify with nothing owed calls nothing" "" "$(calls)"

write_owed "$BOOT_ID"
cycle verify
check "verify in the boot that owes it exits 0" "0" "$RC"
check "verify in the boot that owes it calls nothing" "" "$(calls)"
check "and leaves the marker" "yes" "$(has "$STATE/owed-verify")"

write_owed "an-earlier-boot"
FAKE_VERIFY_RC=1 cycle verify
check "a failed restore check exits 23" "23" "$RC"
check "the restore check runs as the user with the contract's wait" "ccy-sessions verify-restore --wait 300" "$(calls)"
check "a failed restore check is recorded" "verify-failed" "$(result_key outcome)"
check "a failed restore check is alerted" "yes" "$(says 'ALERT verify-failed' "$ERR")"
check "the marker is cleared after a failed check" "no" "$(has "$STATE/owed-verify")"

write_owed "an-earlier-boot"
cycle verify
check "a passed restore check exits 0" "0" "$RC"
check "a passed restore check is recorded as deployed" "deployed" "$(result_key outcome)"
check "the record names the commit" "$FIRST" "$(result_key new)"
check "the marker is cleared after a passed check" "no" "$(has "$STATE/owed-verify")"
rm -f "$STATE/last-result"

# ── the first cycle ────────────────────────────────────────────────────────────────────
echo "the first cycle: every allowlisted play, then warn and reboot"
cycle run --dry-run
check "a dry run exits 0" "0" "$RC"
check "a dry run names the play" "yes" "$(says "^RUN $PLAY\$" "$OUT")"
check "a dry run calls nothing" "" "$(calls)"
check "a dry run moves nothing" "$BASE" "$(git -C "$CLONE" rev-parse HEAD)"
check "a dry run records nothing" "no" "$(has "$STATE/last-result")"

cycle run
check "the first cycle exits 0" "0" "$RC"
check "the first cycle runs the play with both passwords and the lock held, warns, then reboots" \
    "$PLAYED
ccy-sessions notify going-down --minutes 1
systemctl reboot" "$(calls)"
check "the play is pinned to the system ansible, with facts in memory and the system collections" \
    "handed ansible=$SYSTEM_ANSIBLE path=$PREFIX/usr/bin cache=memory collections=$COLLECTIONS" "$(awk 'NR == 1' "$ENV_LOG")"
check "and searches for no code under the user's home, while the clone's play ledger and roles still load" \
    "searched callbacks=$CLONE/callback_plugins:/usr/share/ansible/plugins/callback roles=$CLONE/roles/vendor modules=/usr/share/ansible/plugins/modules user-site-off=1 home: none" \
    "$(awk 'NR == 2' "$ENV_LOG")"
check "the clone is on the signed commit" "$FIRST" "$(git -C "$CLONE" rev-parse HEAD)"
check "the deployed record is the signed commit" "$FIRST" "$(state_key deployed sha)"
check "a verify is owed for the signed commit" "$FIRST" "$(state_key owed-verify new)"
check "the owed verify names this boot" "$BOOT_ID" "$(state_key owed-verify boot)"
check "the record says rebooting" "rebooting" "$(result_key outcome)"
check "the record carries no scratch path" "no" "$(says "$SCRATCH" "$STATE/last-result")"
check "the published copy says rebooting" "rebooting" "$(published_key outcome)"
check "the published copy owes a check from this boot" "$BOOT_ID" "$(published_key owed_boot)"
check "the published copy is group-readable, writable by root only" "640" "$(stat -c %a "$PUBLISHED/result")"
check "the published copy carries no scratch path" "no" "$(says "$SCRATCH" "$PUBLISHED/result")"
check "root's imports left no bytecode in the clone" "" "$(find "$CLONE" -name '__pycache__' -print)"
check "and git sees no file in it beyond the signed commit" "" "$(git -C "$CLONE" ls-files --others)"

cycle run
check "a cycle in the boot that still owes its reboot asks again" "0" "$RC"
check "and only warns and reboots" "ccy-sessions notify going-down --minutes 1
systemctl reboot" "$(calls)"

write_owed "an-earlier-boot"
cycle verify
check "the post-boot verify passes" "0" "$RC"
cycle run
check "a cycle with nothing new exits 0" "0" "$RC"
check "a cycle with nothing new calls nothing" "" "$(calls)"
check "a cycle with nothing new is recorded" "nothing" "$(result_key outcome)"

# ── a later change ─────────────────────────────────────────────────────────────────────
echo "a later change: only the plays it touches, and a failed play is retried"
echo "changed" >"$WORK/README"
commit_signed "a change no play reads"
cycle run
check "a change no allowlisted play reads exits 0" "0" "$RC"
check "and runs nothing" "" "$(calls)"
check "and still moves the deployed record" "$(git -C "$WORK" rev-parse HEAD)" "$(state_key deployed sha)"

UNSIGNED_BASE="$(git -C "$WORK" rev-parse HEAD)"
printf -- '- hosts: localhost\n  tasks: [] # changed\n' >"$WORK/$PLAY"
git_work add -A
git_work commit -q -m "an unsigned change to the play"
git_work push -q origin HEAD:main
cycle run
check "an unsigned tip is not taken" "0" "$RC"
check "an unsigned tip runs nothing" "" "$(calls)"
check "an unsigned tip moves nothing" "$UNSIGNED_BASE" "$(git -C "$CLONE" rev-parse HEAD)"

printf -- '- hosts: localhost\n  tasks: [] # signed\n' >"$WORK/$PLAY"
commit_signed "a signed change to the play"
PLAY_CHANGE="$(git -C "$WORK" rev-parse HEAD)"

mv "$SYSTEM_ANSIBLE" "$SCRATCH/ansible-playbook.away"
cycle run
check "no system ansible-playbook is a config error (70)" "70" "$RC"
check "and no play runs" "" "$(calls)"
check "and it is recorded" "config-invalid" "$(result_key outcome)"
check "and alerted" "yes" "$(says 'ALERT config-invalid' "$ERR")"
check "and the play stays owed" "$UNSIGNED_BASE" "$(state_key deployed sha)"
mv "$SCRATCH/ansible-playbook.away" "$SYSTEM_ANSIBLE"
chmod 775 "$SYSTEM_ANSIBLE"
cycle run
check "a group-writable system ansible-playbook is refused (70)" "70" "$RC"
check "and no play runs" "" "$(calls)"
chmod 755 "$SYSTEM_ANSIBLE"
chmod 777 "$COLLECTIONS"
cycle run
check "a world-writable collections directory is refused (70)" "70" "$RC"
check "and no play runs" "" "$(calls)"
chmod 755 "$COLLECTIONS"
touch "$HOME_SETTING_FLAG"
cycle run
check "a search path a later ansible-core defaults under the user's home is refused (70)" "70" "$RC"
check "and no play runs" "" "$(calls)"
check "and it names the setting" "yes" "$(says 'FUTURE_WIDGET_PLUGIN_PATH=' "$ERR")"
check "and it is recorded" "config-invalid" "$(result_key outcome)"
rm -f "$HOME_SETTING_FLAG"

FAKE_PLAY_RC=2 cycle run
check "a failed play exits 21" "21" "$RC"
check "a failed play stops the cycle: no warning, no reboot" "$PLAYED" "$(calls)"
check "a failed play is recorded" "play-failed" "$(result_key outcome)"
check "a failed play is alerted" "yes" "$(says 'ALERT play-failed' "$ERR")"
check "a failed play is published" "play-failed" "$(published_key outcome)"
check "a failed play is published owing nothing" "" "$(published_key owed_boot)"
check "a failed play leaves the deployed record behind" "$UNSIGNED_BASE" "$(state_key deployed sha)"
check "no verify is owed after a failed play" "no" "$(has "$STATE/owed-verify")"

FAKE_NOTIFY_RC=1 cycle run
check "a session that cannot be warned exits 22" "22" "$RC"
check "the failed play is retried with fresh passwords, then the reboot is abandoned" \
    "$PLAYED
ccy-sessions notify going-down --minutes 1" "$(calls)"
check "an unwarnable session is recorded" "unwarnable" "$(result_key outcome)"
check "the reboot stays owed" "$PLAY_CHANGE" "$(state_key owed-verify new)"

FAKE_REBOOT_RC=1 cycle run
check "a refused reboot exits 24" "24" "$RC"
check "a refused reboot withdraws the warning" "ccy-sessions notify going-down --minutes 1
systemctl reboot
ccy-sessions notify reboot-cancelled" "$(calls)"
check "a refused reboot is recorded" "reboot-failed" "$(result_key outcome)"

# ── refusals through the real wrapper ─────────────────────────────────────────────────
echo "refusals: the wrong remote, and a first cycle from a commit nobody signed"
git -C "$CLONE" config remote.origin.url "https://example.invalid/somebody-else.git"
cycle run
check "a clone whose remote is not REMOTE_URL is refused (20)" "20" "$RC"
check "and nothing is called" "" "$(calls)"
check "and it is recorded" "refused" "$(result_key outcome)"
git -C "$CLONE" config remote.origin.url "$REMOTE_URL"

# The BLOCK the review found: a fresh clone sits on the remote's tip, which may be a
# commit nobody signed, and the gate only judges commits ABOVE HEAD. Root must not import
# or run anything from it.
printf -- '- hosts: localhost\n  tasks: [] # nobody signed this\n' >"$WORK/$PLAY"
git_work add -A
git_work commit -q -m "an unsigned tip"
git_work push -q origin HEAD:main
UNSIGNED_TIP="$(git -C "$WORK" rev-parse HEAD)"
mv "$CLONE" "$SCRATCH/clone.before"
find "$STATE" -mindepth 1 -delete
git clone -q -b main "$ORIGIN" "$CLONE"
git -C "$CLONE" config remote.origin.url "$REMOTE_URL"
git -C "$CLONE" config "url.$ORIGIN.insteadOf" "$REMOTE_URL"
check "a fresh clone lands on the unsigned tip" "$UNSIGNED_TIP" "$(git -C "$CLONE" rev-parse HEAD)"
cycle run
check "a first cycle from an unsigned HEAD is refused (20)" "20" "$RC"
check "and nothing is called" "" "$(calls)"
check "and it says why" "yes" "$(says 'is not a commit signed by' "$ERR")"
check "and nothing moved" "$UNSIGNED_TIP" "$(git -C "$CLONE" rev-parse HEAD)"
check "and nothing was deployed" "no" "$(has "$STATE/deployed")"
cycle status
check "status is refused from that clone too (20)" "20" "$RC"

# What the play does after cloning: anchor HEAD on the newest commit the pinned key signed.
(cd "$REPO_ROOT" && python3 -m helpers.self_update.update --anchor --checkout "$CLONE" --branch main \
    --allowed-signers "$ETC/self-update.allowed_signers" --principal "$PRINCIPAL") >"$OUT" 2>"$ERR"
check "the anchor succeeds" "0" "$?"
check "and moves the clone back to the newest signed commit" "$PLAY_CHANGE" "$(git -C "$CLONE" rev-parse HEAD)"
cycle run --dry-run
check "after anchoring, the first cycle can run" "0" "$RC"
check "and names every allowlisted play" "yes" "$(says "^RUN $PLAY\$" "$OUT")"

# Round 2's BLOCK: git status never lists an ignored file, and python imports a pyc beside
# its source without checking it. An unsigned tip can leave one behind (a recursive clone's
# submodule, a planted __pycache__); root must refuse the tree before importing anything.
PLANTED="$CLONE/helpers/self_update/__pycache__/cycle.cpython-311.pyc"
mkdir -p "$(dirname "$PLANTED")"
printf 'planted bytecode' >"$PLANTED"
cycle run --dry-run
check "an ignored bytecode file in the clone is refused before any import (20)" "20" "$RC"
check "and nothing is called" "" "$(calls)"
check "and it names the file" "yes" "$(says 'helpers/self_update/__pycache__/cycle.cpython-311.pyc' "$ERR")"
cycle status
check "status is refused too (20)" "20" "$RC"
(cd "$REPO_ROOT" && python3 -B -m helpers.self_update.update --anchor --checkout "$CLONE" --branch main \
    --allowed-signers "$ETC/self-update.allowed_signers" --principal "$PRINCIPAL") >"$OUT" 2>"$ERR"
check "the anchor refuses it as well (11)" "11" "$?"
rm -rf "$(dirname "$PLANTED")"
mkdir -p "$CLONE/vendor-sub"
git -C "$CLONE/vendor-sub" init -q
printf 'x = 1\n' >"$CLONE/vendor-sub/code.py"
printf 'vendor-sub/\n' >>"$CLONE/.git/info/exclude"
cycle run --dry-run
check "a leftover nested repository at an ignored path is refused (20)" "20" "$RC"
check "and it names the directory" "yes" "$(says 'vendor-sub/' "$ERR")"
# The play never updates an existing clone, so "re-run the play" cannot clear this.
check "and it names the remedy that works" "yes" "$(says 'self_update_enabled: false' "$ERR")"
rm -rf "$CLONE/vendor-sub"
echo "local edit" >>"$CLONE/README"
cycle run --dry-run
check "a local edit to a tracked file is refused (20)" "20" "$RC"
check "and it names the remedy that works" "yes" "$(says 'self_update_enabled: false' "$ERR")"
git -C "$CLONE" checkout -q HEAD -- README

# The one file the play itself puts in the clone: the host_vars copy. It is allowed, and
# nothing else is.
mkdir -p "$CLONE/environment/localhost/host_vars"
printf 'user_login: tester\n' >"$CLONE/environment/localhost/host_vars/localhost.yml"
cycle run --dry-run
check "the host_vars copy the play installs is allowed" "0" "$RC"
(cd "$REPO_ROOT" && python3 -B -m helpers.self_update.update --anchor --checkout "$CLONE" --branch main \
    --allowed-signers "$ETC/self-update.allowed_signers" --principal "$PRINCIPAL" \
    --allow-untracked environment/localhost/host_vars/localhost.yml) >"$OUT" 2>"$ERR"
check "and the anchor allows it when told to" "0" "$?"

# The exception is that exact FILE. A directory at the same path is not it, and hides
# whatever it holds from a pattern-based exclude.
mv "$CLONE/environment/localhost/host_vars/localhost.yml" "$SCRATCH/localhost.yml.saved"
mkdir "$CLONE/environment/localhost/host_vars/localhost.yml"
printf 'x = 1\n' >"$CLONE/environment/localhost/host_vars/localhost.yml/hidden.py"
cycle run --dry-run
check "a directory at the host_vars path is refused (20)" "20" "$RC"
check "and it names what is inside it" "yes" "$(says 'host_vars/localhost.yml/hidden.py' "$ERR")"
rm -rf "$CLONE/environment/localhost/host_vars/localhost.yml"
mv "$SCRATCH/localhost.yml.saved" "$CLONE/environment/localhost/host_vars/localhost.yml"

# The stray listing is read through a process substitution, whose failure `set -e` never
# sees. A git that cannot list must still stop the cycle, not read as "no strays".
REAL_GIT="$(command -v git)"
cat >"$BIN/git" <<EOF
#!/usr/bin/env bash
for arg in "\$@"; do
    [ "\$arg" = "ls-files" ] && { echo "git stub: ls-files failed" >&2; exit 128; }
done
exec "$REAL_GIT" "\$@"
EOF
chmod 755 "$BIN/git"
cycle run --dry-run
check "a git that cannot list the untracked files is refused (20)" "20" "$RC"
check "and nothing is called" "" "$(calls)"
check "and it says why" "yes" "$(says 'could not list the deploy clone' "$ERR")"
rm "$BIN/git"

# The file the wrapper checks HEAD against, before any Python runs, must be one nobody else
# can rewrite; update.py checks it too, but only after the clone's code is imported.
chmod 666 "$ETC/self-update.allowed_signers"
cycle status
check "a world-writable allowed-signers file is refused before any import (70)" "70" "$RC"
check "and it says why" "yes" "$(says 'allowed-signers' "$ERR")"
chmod 644 "$ETC/self-update.allowed_signers"

# A signed commit whose bytes were altered after signing: the gate refuses the whole cycle.
echo "tampered" >"$WORK/README"
git_work add -A
git_work commit -q -S -m "signed, then altered"
TAMPERED="$(git -C "$WORK" cat-file commit HEAD | awk '{ sub(/signed, then altered/, "altered after signing"); print }' |
    git -C "$WORK" hash-object -t commit -w --stdin)"
git_work push -q origin "$TAMPERED:refs/heads/main"
cycle run
check "a tampered signed commit refuses the cycle (20)" "20" "$RC"
check "and nothing is called" "" "$(calls)"
check "and it is recorded as refused by the gate" "update refused" "$(result_key phase) $(result_key outcome)"
check "and nothing moved" "$PLAY_CHANGE" "$(git -C "$CLONE" rev-parse HEAD)"

printf 'passed: %d failed: %d\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
