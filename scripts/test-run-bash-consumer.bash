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

echo "=== a single play and --changed on a consumer box stay with run.bash ==="
home="$SCRATCH/home-consumer"
STUB_SUDO_REFUSES=1 run_case "$home" --changed
check "--changed not handed over" "0" "$(calls consumer-install:)"

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
