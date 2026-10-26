#!/usr/bin/env bash
# Drive run.bash's consumer hand-off end to end (Plan 00228): `./run.bash --consumer`, and a
# box whose host_vars a consumer install wrote, hand over to scripts/consumer-install.bash;
# every other run takes the developer path exactly as before.
#
# The REAL run.bash runs under a throwaway HOME against stubs for everything that would touch
# the machine: sudo and git (record their arguments), whoami (run.bash refuses root, and the
# CCY container is root), and the checkout's consumer-install.bash (records its arguments). A
# developer run is stopped at its first root command by a sudo that refuses, so it never
# provisions anything. Then the real wrapper builds the real Go command and runs its --help.
#
# `set -e` is deliberately NOT used: every case must run so the summary shows the whole
# picture, and each result is checked explicitly.

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

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

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/consumer-test.XXXXXX")"
trap 'rm -rf "$SCRATCH"' EXIT
BIN="$SCRATCH/bin"
LOG="$SCRATCH/calls.log"
mkdir -p "$BIN"

# The stub checkout git clones: only the wrapper the hand-off execs, which records its argv.
cat >"$SCRATCH/consumer-install.bash" <<'EOF'
#!/usr/bin/env bash
printf 'consumer-install:%s\n' "$*" >>"$STUB_LOG"
EOF
cat >"$BIN/git" <<'EOF'
#!/usr/bin/env bash
printf 'git:%s\n' "$*" >>"$STUB_LOG"
if [ "$1" = clone ]; then
    mkdir -p "$3/scripts"
    cp "$STUB_WRAPPER" "$3/scripts/consumer-install.bash"
fi
EOF
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
printf 'sudo:%s\n' "$*" >>"$STUB_LOG"
[ "${STUB_SUDO_REFUSES:-0}" = 1 ] && exit 97
exit 0
EOF
printf '#!/usr/bin/env bash\necho u\n' >"$BIN/whoami"
chmod +x "$BIN/git" "$BIN/sudo" "$BIN/whoami" "$SCRATCH/consumer-install.bash"

# run_case <home> <args...>: run.bash under the stubs; its output in $SCRATCH/out.
run_case() {
    local home="$1"
    shift
    : >"$LOG"
    HOME="$home" PATH="$BIN:$PATH" STUB_LOG="$LOG" STUB_WRAPPER="$SCRATCH/consumer-install.bash" \
        timeout 120 bash "$REPO_ROOT/run.bash" "$@" </dev/null >"$SCRATCH/out" 2>&1
}
calls() { grep -c "^$1" "$LOG"; }

echo "=== --consumer on a fresh box: git and Go, the clone, the hand-off ==="
home="$SCRATCH/home-fresh"
mkdir -p "$home"
run_case "$home" --consumer
check "exit is the Go tool's" "0" "$?"
check "git and Go installed" "1" "$(grep -cx 'sudo:dnf -y install git golang' "$LOG")"
check "the checkout cloned over HTTPS" "1" "$(grep -cx "git:clone https://github.com/LongTermSupport/fedora-desktop.git $home/Projects/fedora-desktop" "$LOG")"
check "the checkout pulled" "1" "$(grep -cx "git:-C $home/Projects/fedora-desktop pull --ff-only" "$LOG")"
check "handed over, main install included" "1" "$(grep -cx 'consumer-install:--optional-only=false' "$LOG")"
check "nothing else run as root" "1" "$(calls sudo:)"

echo "=== a consumer box: no flag needed, --optional-only passed on, no second clone ==="
home="$SCRATCH/home-consumer"
mkdir -p "$home/Projects/fedora-desktop/scripts" "$home/Projects/fedora-desktop/environment/localhost/host_vars"
cp "$SCRATCH/consumer-install.bash" "$home/Projects/fedora-desktop/scripts/"
printf 'user_login: "u"\ninstall_audience: consumer\n' >"$home/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml"
run_case "$home" --optional-only
check "exit is the Go tool's" "0" "$?"
check "handed over with --optional-only" "1" "$(grep -cx 'consumer-install:--optional-only=true' "$LOG")"
check "no clone over an existing checkout" "0" "$(calls git:clone)"

echo "=== a developer box: the developer path, unchanged ==="
home="$SCRATCH/home-dev"
mkdir -p "$home/Projects/fedora-desktop/scripts" "$home/Projects/fedora-desktop/environment/localhost/host_vars"
cp "$SCRATCH/consumer-install.bash" "$home/Projects/fedora-desktop/scripts/"
printf 'user_login: "u"\ngithub_accounts:\n  personal: "u"\n' >"$home/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml"
STUB_SUDO_REFUSES=1 run_case "$home"
check "not handed over" "0" "$(calls consumer-install:)"
check "the developer installer ran" "1" "$(grep -c 'FEDORA DESKTOP CONFIGURATION INSTALLER' "$SCRATCH/out")"
check "git and Go were not installed alone" "0" "$(grep -cx 'sudo:dnf -y install git golang' "$LOG")"

echo "=== --consumer on a developer box is refused before anything runs ==="
home="$SCRATCH/home-dev"
run_case "$home" --consumer
check "exit 1" "1" "$?"
check "the refusal says why" "1" "$(grep -c 'this is a developer install. Run ./run.bash without --consumer' "$SCRATCH/out")"
check "no sudo, no git, no hand-off" "0" "$(wc -l <"$LOG")"
run_case "$home" --consumer --changed
check "with --changed too: exit 1" "1" "$?"
check "with --changed too: nothing run" "0" "$(wc -l <"$LOG")"

# A single play and --changed stay with run.bash's own play runner. They run from a
# throwaway checkout (the real run.bash and lock helper, a stand-in judge for --changed
# that names one play) so a play can fail: ansible-playbook exits 4. stdin answers "y" to
# every question, so an offer to file a GitHub issue would be accepted and reach gh.
CHECKOUT="$SCRATCH/checkout"
mkdir -p "$CHECKOUT/helpers/play_lock" "$CHECKOUT/helpers/play_ledger" "$CHECKOUT/playbooks/imports"
cp "$REPO_ROOT/run.bash" "$CHECKOUT/run.bash"
cp "$REPO_ROOT/helpers/play_lock/lock.py" "$CHECKOUT/helpers/play_lock/lock.py"
: >"$CHECKOUT/helpers/__init__.py"
: >"$CHECKOUT/helpers/play_ledger/__init__.py"
printf 'import sys\nsys.stdout.write("RUN playbooks/imports/play-a.yml\\n")\n' >"$CHECKOUT/helpers/play_ledger/changed_plays.py"
printf -- '- hosts: localhost\n  tasks: []\n' >"$CHECKOUT/playbooks/imports/play-a.yml"
RUNTIME="$SCRATCH/runtime"
mkdir -m 700 "$RUNTIME"
cat >"$BIN/ansible-playbook" <<'EOF'
#!/usr/bin/env bash
printf 'ansible-playbook:%s\n' "$*" >>"$STUB_LOG"
exit 4
EOF
cat >"$BIN/gh" <<'EOF'
#!/usr/bin/env bash
printf 'gh:%s\n' "$*" >>"$STUB_LOG"
exit 1
EOF
chmod +x "$BIN/ansible-playbook" "$BIN/gh"

# run_play <home> <args...>: the throwaway checkout's run.bash, "y" on every line of stdin.
run_play() {
    local home="$1"
    shift
    : >"$LOG"
    env -u RUN_BASH_HEADLESS -u FEDORA_DESKTOP_PLAY_LOCK_FD \
        HOME="$home" PATH="$BIN:$PATH" XDG_RUNTIME_DIR="$RUNTIME" STUB_LOG="$LOG" \
        timeout 120 bash "$CHECKOUT/run.bash" "$@" <"$SCRATCH/answers" >"$SCRATCH/out" 2>&1
}
printf 'y\n%.0s' 1 2 3 4 5 6 7 8 >"$SCRATCH/answers"
log_line='The log of this run is the output above'

for args in "playbooks/imports/play-a.yml" "--changed"; do
    echo "=== a failed play on a consumer box ($args): the log, never GitHub ==="
    run_play "$SCRATCH/home-consumer" "$args"
    check "exit is the play's" "4" "$?"
    check "the play ran" "1" "$(calls ansible-playbook:)"
    check "not handed over" "0" "$(calls consumer-install:)"
    check "says where the log is" "1" "$(grep -c "$log_line" "$SCRATCH/out")"
    check "no GitHub issue offered" "0" "$(grep -c 'create a GitHub issue' "$SCRATCH/out")"
    check "gh never called" "0" "$(calls gh:)"

    echo "=== the same failure on a developer box ($args) still offers the issue ==="
    run_play "$SCRATCH/home-dev" "$args"
    check "exit is the play's" "4" "$?"
    check "the issue is offered" "1" "$(grep -c 'create a GitHub issue' "$SCRATCH/out")"
    check "no consumer log line" "0" "$(grep -c "$log_line" "$SCRATCH/out")"
done

echo "=== --headless --consumer is refused ==="
home="$SCRATCH/home-headless"
mkdir -p "$home"
run_case "$home" --headless --consumer
check "exit 1" "1" "$?"
check "the refusal says why" "1" "$(grep -c 'a consumer install is attended' "$SCRATCH/out")"
check "nothing run" "0" "$(wc -l <"$LOG")"

echo "=== the real wrapper builds and runs the Go command ==="
if out="$("$REPO_ROOT/scripts/consumer-install.bash" --help 2>&1)"; then status=0; else status=$?; fi
check "consumer-install --help exits 0" "0" "$status"
check "its usage is printed" "1" "$(grep -c '^Usage: consumer-install' <<<"$out")"

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
