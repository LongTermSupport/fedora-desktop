#!/usr/bin/env bash
# Drive run.bash's single-play mode end to end (Plan 00137 T1.4 + T2.1): the unattended
# `--headless <play>.yml` path, and the play lock both modes now take.
#
# The REAL run.bash and the REAL lock helper run in a throwaway checkout, against stubs
# for everything that would touch the machine: ansible-playbook (records what it was
# given, what its stdin was, and whether the play lock was held while it ran), sudo (a
# NOPASSWD or password-sudo box, on request) and whoami (run.bash refuses root, and the
# CCY container is root). Nothing here runs a play or needs a TTY.
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
yes_if() { if "$@"; then echo yes; else echo no; fi; }

for tool in flock python3; do
    if ! command -v "$tool" >/dev/null; then
        echo "FAIL: $tool is required by the code under test and is not installed" >&2
        exit 1
    fi
done

mkdir -p "$REPO_ROOT/untracked/scratch"
SCRATCH="$(mktemp -d "$REPO_ROOT/untracked/scratch/single-play-test.XXXXXX")"
cleanup() { rm -rf "$SCRATCH"; }
trap cleanup EXIT

# ── a throwaway checkout: the real run.bash and lock helper, one fixture play ─────────
CHECKOUT="$SCRATCH/checkout"
mkdir -p "$CHECKOUT/helpers/play_lock" "$CHECKOUT/playbooks/imports"
cp "$REPO_ROOT/run.bash" "$CHECKOUT/run.bash"
cp "$REPO_ROOT/helpers/play_lock/lock.py" "$CHECKOUT/helpers/play_lock/lock.py"
printf -- '- hosts: localhost\n  tasks: []\n' >"$CHECKOUT/playbooks/imports/play-fixture.yml"
PLAY="playbooks/imports/play-fixture.yml"

RUNTIME="$SCRATCH/runtime"
mkdir -m 700 "$RUNTIME"
LOCK="$RUNTIME/fedora-desktop-plays.lock"
LOG="$SCRATCH/calls.log"
FAKE_HOME="$SCRATCH/home"
mkdir -p "$FAKE_HOME/.local/bin"
# run.bash's own temporary files land here, so the fake play can look for a password copy.
TEST_TMP="$SCRATCH/tmp"
mkdir -p "$TEST_TMP"

# ── the fakes ────────────────────────────────────────────────────────────────────────
BIN="$SCRATCH/bin"
mkdir -p "$BIN"
# ansible-playbook lives ONLY in ~/.local/bin, as pipx puts it, so a headless run that did
# not add that directory to PATH cannot find it.
cat >"$FAKE_HOME/.local/bin/ansible-playbook" <<'EOF'
#!/usr/bin/env bash
{
    printf 'argv:'
    printf ' %s' "$@"
    printf '\n'
    printf 'binary: %s\n' "$0"
    printf 'stdin: %s\n' "$(readlink /proc/self/fd/0)"
    prev=""
    for a in "$@"; do
        if [ "$prev" = "--become-password-file" ]; then
            printf 'become-file: %s\n' "$a"
            # As the real CLI does: `-` is read from stdin to EOF; any other value is
            # realpath'd first, which turns a pipe's /dev/fd/N into a path that does not exist.
            if [ "$a" = "-" ]; then
                printf 'become-kind: %s\n' "$(stat -L -c %F /proc/self/fd/0)"
                secret="$(cat)"
            elif [ -e "$(realpath -m -- "$a")" ]; then
                printf 'become-kind: %s\n' "$(stat -L -c %F -- "$a")"
                secret="$(cat -- "$a")"
            else
                echo "ERROR! The password file $a was not found" >&2
                exit 5
            fi
            printf 'become: %s\n' "$secret"
            # Any copy of the password on disk while the play runs, where run.bash's
            # temporary files would be: a same-uid process could read it from there.
            printf 'tmp-copies: %s\n' "$(grep -rlF -- "$secret" "$TMPDIR" | wc -l)"
        fi
        prev="$a"
    done
    if flock -n "$TEST_LOCK" true; then echo "lock: free"; else echo "lock: held"; fi
} >>"$TEST_LOG"
exit "${FAKE_PLAY_RC:-0}"
EOF
# sudo: `-k -n true` is the NOPASSWD probe; `-k -S -p '' true` reads the password on stdin,
# which is how an unattended single play proves it without writing it to a file.
cat >"$BIN/sudo" <<'EOF'
#!/usr/bin/env bash
case "$*" in
"-k -n true")
    if [ "${FAKE_NOPASSWD:-0}" = 1 ]; then exit 0; fi
    echo "sudo: a password is required" >&2
    exit 1
    ;;
"-k -S -p  true")
    given=""
    IFS= read -r given
    if [ "$given" = "${FAKE_SUDO_PASSWORD:-}" ]; then exit 0; fi
    echo "sudo: 1 incorrect password attempt" >&2
    exit 1
    ;;
*)
    echo "fake sudo: unexpected call: $*" >&2
    exit 97
    ;;
esac
EOF
printf '#!/usr/bin/env bash\necho tester\n' >"$BIN/whoami"
chmod 755 "$BIN/sudo" "$BIN/whoami" "$FAKE_HOME/.local/bin/ansible-playbook"

# run_play [env assignments...] -- <run.bash args...>. stdin is a PIPE, not /dev/null, so
# the "stdin is closed" check proves run.bash's own redirect rather than this harness's.
run_play() {
    local -a assigns=()
    while [ "$1" != "--" ]; do
        assigns+=("$1")
        shift
    done
    shift
    : >"$LOG"
    out="$(printf '' | env -u RUN_BASH_HEADLESS -u FEDORA_DESKTOP_PLAY_LOCK_FD \
        HOME="$FAKE_HOME" PATH="$BIN:/usr/bin:/bin" XDG_RUNTIME_DIR="$RUNTIME" TMPDIR="$TEST_TMP" \
        TEST_LOG="$LOG" TEST_LOCK="$LOCK" "${assigns[@]}" \
        bash "$CHECKOUT/run.bash" "$@" 2>&1)"
    rc=$?
}
calls() { if [ -f "$LOG" ]; then cat "$LOG"; fi; }
ran() { calls | grep -c '^argv:'; }

echo "=== --headless <play>: an unattended play on a NOPASSWD box ==="
run_play FAKE_NOPASSWD=1 -- --headless "$PLAY" -e some=var
check "exits 0" "0" "$rc"
check "runs the play once" "1" "$(ran)"
check "with no become flag (NOPASSWD)" "0" "$(calls | grep -c -- '--become-password-file')"
check "forwards the ansible-playbook arguments" "yes" "$(yes_if grep -q -- 'argv: .*play-fixture.yml -e some=var$' "$LOG")"
check "stdin is closed (/dev/null)" "stdin: /dev/null" "$(calls | grep '^stdin:')"
check "finds ansible-playbook in ~/.local/bin although PATH lacked it" "1" "$(ran)"
check "holds the play lock while the play runs" "lock: held" "$(calls | grep '^lock:')"
check "says the preflight was sudo-only" "yes" "$(yes_if grep -q 'Unattended play preflight OK.*sudo=NOPASSWD:ALL' <<<"$out")"
check "the lock is free again afterwards" "yes" "$(yes_if flock -n "$LOCK" true)"
check "and names the last holder" "yes" "$(yes_if grep -q "what=run.bash $PLAY" "$LOCK")"

echo "=== RUN_BASH_HEADLESS=1 through the same path (what a play's shebang does) ==="
run_play FAKE_NOPASSWD=1 RUN_BASH_HEADLESS=1 -- "$CHECKOUT/$PLAY"
check "exits 0" "0" "$rc"
check "runs the play" "1" "$(ran)"

echo "=== password sudo, the password handed over as an inherited descriptor ==="
PW="$SCRATCH/sudo-pass"
printf 'correct horse' >"$PW"
chmod 600 "$PW"
: >"$LOG"
out="$(env -u FEDORA_DESKTOP_PLAY_LOCK_FD HOME="$FAKE_HOME" PATH="$BIN:/usr/bin:/bin" \
    XDG_RUNTIME_DIR="$RUNTIME" TMPDIR="$TEST_TMP" TEST_LOG="$LOG" TEST_LOCK="$LOCK" \
    FAKE_NOPASSWD=0 FAKE_SUDO_PASSWORD='correct horse' RUN_BASH_SUDO_PASSWORD_FILE=/dev/fd/3 \
    bash "$CHECKOUT/run.bash" --headless "$PLAY" </dev/null 3<"$PW" 2>&1)"
rc=$?
check "exits 0" "0" "$rc"
check "Ansible gets the password through --become-password-file" "become: correct horse" "$(calls | grep '^become:')"
check "through a pipe, not a file" "become-kind: fifo" "$(calls | grep '^become-kind:')"
check "and no copy of it is on disk while the play runs" "tmp-copies: 0" "$(calls | grep '^tmp-copies:')"
check "says the password route was proven" "yes" "$(yes_if grep -q 'sudo=password' <<<"$out")"

# The descriptor must be READ, never reopened by its /dev/fd path. On the host the caller
# is root and the password file is root-only 0600: a user opening /dev/fd/3 re-checks the
# file's permissions and gets EACCES, even though the descriptor it inherited is readable.
# A socket reproduces that for any user, root included: its /dev/fd path cannot be opened
# at all, while reading the descriptor works.
: >"$LOG"
out="$(env -u FEDORA_DESKTOP_PLAY_LOCK_FD HOME="$FAKE_HOME" PATH="$BIN:/usr/bin:/bin" \
    XDG_RUNTIME_DIR="$RUNTIME" TMPDIR="$TEST_TMP" TEST_LOG="$LOG" TEST_LOCK="$LOCK" \
    FAKE_NOPASSWD=0 FAKE_SUDO_PASSWORD='correct horse' RUN_BASH_SUDO_PASSWORD_FILE=/dev/fd/3 \
    python3 -c '
import os, socket, sys
ours, theirs = socket.socketpair()
ours.sendall(b"correct horse")
ours.close()
os.dup2(theirs.fileno(), 3)
os.execvp("bash", ["bash", *sys.argv[1:]])
' "$CHECKOUT/run.bash" --headless "$PLAY" </dev/null 2>&1)"
rc=$?
check "a descriptor whose path cannot be reopened: exits 0" "0" "$rc"
check "and Ansible still gets the password" "become: correct horse" "$(calls | grep '^become:')"

run_play FAKE_NOPASSWD=0 FAKE_SUDO_PASSWORD='right' RUN_BASH_SUDO_PASSWORD_FILE="$PW" -- --headless "$PLAY"
check "a wrong password refuses the run" "1" "$rc"
check "before the play runs" "0" "$(ran)"

run_play FAKE_NOPASSWD=0 -- --headless "$PLAY"
check "no NOPASSWD and no password refuses the run" "1" "$rc"
check "and runs nothing" "0" "$(ran)"
check "naming the two ways to fix it" "yes" "$(yes_if grep -q 'RUN_BASH_SUDO_PASSWORD_FILE' <<<"$out")"

echo "=== a failing play ==="
run_play FAKE_NOPASSWD=1 FAKE_PLAY_RC=4 -- --headless "$PLAY"
check "exits with the play's own status" "4" "$rc"
check "and asks nothing (no issue-filing prompt)" "no" "$(yes_if grep -q 'create a GitHub issue' <<<"$out")"

echo "=== the play lock: one play run per host ==="
exec 8<>"$LOCK"
flock -n 8
printf 'pid=999 what=cycle since=now\n' >"$LOCK"
run_play FAKE_NOPASSWD=1 -- --headless "$PLAY"
check "a run while another holds the lock exits 75" "75" "$rc"
check "and runs nothing" "0" "$(ran)"
check "and names the holder" "yes" "$(yes_if grep -q 'what=cycle' <<<"$out")"
run_play FAKE_NOPASSWD=1 PATH="$FAKE_HOME/.local/bin:$BIN:/usr/bin:/bin" -- --interactive "$PLAY"
check "the interactive single play is refused the same way" "75" "$rc"

run_play FAKE_NOPASSWD=1 FEDORA_DESKTOP_PLAY_LOCK_FD=8 -- --headless "$PLAY"
check "a holder that delegates its descriptor lets the child run" "0" "$rc"
check "and the lock stays held throughout" "lock: held" "$(calls | grep '^lock:')"
exec 8>&-

run_play FAKE_NOPASSWD=1 FEDORA_DESKTOP_PLAY_LOCK_FD=9 -- --headless "$PLAY"
check "a delegation claim that is not a held lock is an error" "1" "$rc"
check "and runs nothing" "0" "$(ran)"

echo "=== the interactive single play, lock free, is unchanged ==="
run_play FAKE_NOPASSWD=1 PATH="$FAKE_HOME/.local/bin:$BIN:/usr/bin:/bin" -- --interactive "$PLAY"
check "exits 0" "0" "$rc"
check "runs the play bare on NOPASSWD" "0" "$(calls | grep -c -- '--become-password-file')"
check "prints no unattended banner" "no" "$(yes_if grep -q 'Unattended play preflight' <<<"$out")"
check "holds the lock while it runs" "lock: held" "$(calls | grep '^lock:')"

echo "=== RUN_BASH_ANSIBLE_PLAYBOOK: a caller pins the ansible it trusts ==="
SYSTEM_BIN="$SCRATCH/system-bin"
mkdir -p "$SYSTEM_BIN"
cp "$FAKE_HOME/.local/bin/ansible-playbook" "$SYSTEM_BIN/ansible-playbook"
chmod 755 "$SYSTEM_BIN/ansible-playbook"
run_play FAKE_NOPASSWD=1 PATH="$SYSTEM_BIN:$BIN:/usr/bin:/bin" \
    RUN_BASH_ANSIBLE_PLAYBOOK="$SYSTEM_BIN/ansible-playbook" -- --headless "$PLAY"
check "a pinned ansible-playbook that PATH resolves to runs" "0" "$rc"
check "and it is the pinned one, not the pipx one in ~/.local/bin" \
    "binary: $SYSTEM_BIN/ansible-playbook" "$(calls | grep '^binary:')"
run_play FAKE_NOPASSWD=1 RUN_BASH_ANSIBLE_PLAYBOOK="$SYSTEM_BIN/ansible-playbook" -- --headless "$PLAY"
check "a pinned ansible-playbook that PATH does not resolve to is refused" "1" "$rc"
check "and runs nothing" "0" "$(ran)"
check "and says which one it found" "yes" "$(yes_if grep -q 'RUN_BASH_ANSIBLE_PLAYBOOK' <<<"$out")"
run_play FAKE_NOPASSWD=1 PATH="$SYSTEM_BIN:$BIN:/usr/bin:/bin" \
    RUN_BASH_ANSIBLE_PLAYBOOK="system-bin/ansible-playbook" -- --headless "$PLAY"
check "a relative pin is refused" "1" "$rc"
check "and runs nothing" "0" "$(ran)"
run_play FAKE_NOPASSWD=1 PATH="$FAKE_HOME/.local/bin:$BIN:/usr/bin:/bin" \
    RUN_BASH_ANSIBLE_PLAYBOOK="$SYSTEM_BIN/ansible-playbook" -- --interactive "$PLAY"
check "a pin outside an unattended single play is refused, not ignored" "1" "$rc"
check "and runs nothing" "0" "$(ran)"

echo "=== refusals ==="
run_play FAKE_NOPASSWD=1 -- --headless /etc/passwd.yml
check "a path outside the checkout's playbooks/ is refused" "1" "$rc"
check "and runs nothing" "0" "$(ran)"
chmod 777 "$RUNTIME"
run_play FAKE_NOPASSWD=1 -- --headless "$PLAY"
check "an unsafe runtime directory refuses the run" "1" "$rc"
check "and runs nothing" "0" "$(ran)"
chmod 700 "$RUNTIME"

printf '\npassed: %s failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
