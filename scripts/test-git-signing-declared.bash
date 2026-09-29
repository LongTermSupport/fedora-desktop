#!/usr/bin/env bash
# Commit signing is configured only where the machine DECLARES something to sign with
# (Plan 00139 Task 5.5).
#
# A headless box provisioned with RUN_BASH_GITHUB_ACCOUNTS=none has no login key: run.bash
# skips ~/.ssh/id on that path and writes `github_accounts: {}`. The signing tasks in
# play-git-configure-and-tools.yml assert that key, so they must not run there, and must
# still run, and still fail loudly on a missing key, everywhere else. The gate is a declared
# state, never "the key file exists": that would turn a desktop's lost key into silent
# unsigned commits.
#
# Three parts, all read from the real files:
#   1. vars/git-signing.yml's git_signing_declared, evaluated by ansible under each declared
#      state, through a `when:` exactly as the play uses it.
#   2. the play: every task touching the signing key, the recorded public key or
#      commit.gpgsign/tag.gpgsign sits in the one block gated on git_signing_declared.
#   3. that block, lifted out of the play unchanged and run: a none box gets no signing
#      config and no error, a desktop gets signing, and a desktop missing its key fails.
#
# Nothing here contacts a host or changes one: ansible runs against localhost with a
# throwaway config and inventory, git's HOME and the recorded host_vars are temp files, and
# every write lands in a temp directory. The repo's ansible.cfg is deliberately NOT used:
# its play_ledger callback would record these runs in the machine's play ledger, and it
# needs the vault password file to start. Part 3 needs community.general
# (`ansible-galaxy install -r requirements.yml`), as the ansible-syntax gate already does.
#
# `set -e` is deliberately NOT used: every case runs so the summary is complete, and each
# result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
VARS_FILE="$REPO_ROOT/vars/git-signing.yml"
PLAY_FILE="$REPO_ROOT/playbooks/imports/play-git-configure-and-tools.yml"

for tool in ansible-playbook ansible-doc python3 ssh-keygen git; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found — this suite runs the real play's signing tasks with it" >&2
        exit 2
    fi
done
for file in "$VARS_FILE" "$PLAY_FILE"; do
    if [ ! -f "$file" ]; then
        echo "FAIL: $file not found" >&2
        exit 1
    fi
done

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/tmp"
printf '[defaults]\nretry_files_enabled = False\nstdout_callback = ansible.builtin.default\n' \
    > "$work/ansible.cfg"

# run_ansible <command> <args...> — a sealed ansible run. The caller's ANSIBLE_* settings are
# dropped, bar where collections live, so none of them can route this through the repo's
# config or its callbacks.
run_ansible() {
    env -i PATH="$PATH" HOME="$HOME" LANG=C.UTF-8 \
        ${ANSIBLE_COLLECTIONS_PATH:+ANSIBLE_COLLECTIONS_PATH="$ANSIBLE_COLLECTIONS_PATH"} \
        ANSIBLE_CONFIG="$work/ansible.cfg" \
        ANSIBLE_LOCAL_TEMP="$work/tmp" ANSIBLE_REMOTE_TEMP="$work/tmp" \
        "$@" </dev/null 2>&1
}

if ! doc_out=$(run_ansible ansible-doc -t module community.general.git_config); then
    echo "ERROR: community.general is not installed — ansible-galaxy install -r requirements.yml" >&2
    printf '%s\n' "$doc_out" >&2
    exit 2
fi

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

# inventory <file> <host-vars YAML, indented for the inventory> — localhost as the desktop.
inventory() {
    cat > "$1" <<EOF
all:
  children:
    desktop:
      hosts:
        localhost:
          ansible_connection: local
          ansible_python_interpreter: "{{ ansible_playbook_python }}"
          user_login: "<user>"
$2
EOF
}

echo "=== git_signing_declared, from vars/git-signing.yml ==="

cat > "$work/verdict.yml" <<EOF
- hosts: desktop
  gather_facts: false
  vars_files:
    - "$VARS_FILE"
  tasks:
    - name: Signing Is Configured
      when: git_signing_declared
      ansible.builtin.copy:
        dest: "{{ verdict_file }}"
        content: "sign {{ git_signing_key_path }}\n"
        mode: "0600"

    - name: Signing Is Not Configured
      when: not git_signing_declared
      ansible.builtin.copy:
        dest: "{{ verdict_file }}"
        content: "skip\n"
        mode: "0600"
EOF

# verdict <case> <host-vars> — what the gate decides for that declared state.
verdict() {
    local name="$1" out rc
    inventory "$work/$name.inventory.yml" "          verdict_file: \"$work/$name.verdict\"
$2"
    out=$(run_ansible ansible-playbook -i "$work/$name.inventory.yml" "$work/verdict.yml")
    rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "  ansible-playbook exited $rc for case $name:" >&2
        printf '%s\n' "$out" >&2
        printf 'ansible-failed'
        return
    fi
    if [ ! -f "$work/$name.verdict" ]; then
        printf 'no-verdict'
        return
    fi
    tr -d '\n' < "$work/$name.verdict"
}

check "github_accounts: {} and no git_signing_key (headless none): signing skipped" \
    "skip" \
    "$(verdict none '          github_accounts: {}')"

check "github_accounts: {} with git_signing_key declared: signs with that key" \
    "sign /home/<user>/.ssh/machine" \
    "$(verdict none-key '          github_accounts: {}
          git_signing_key: /home/<user>/.ssh/machine')"

check "one GitHub account (desktop): signs with ~/.ssh/id" \
    "sign /home/<user>/.ssh/id" \
    "$(verdict desktop '          github_accounts:
            personal: example-user')"

check "two GitHub accounts: signs with ~/.ssh/id" \
    "sign /home/<user>/.ssh/id" \
    "$(verdict desktop-two '          github_accounts:
            personal: example-user
            work: example-work')"

# Undeclared is not "none": only the explicit empty map opts out, so a machine that declares
# nothing keeps the key assert and fails loudly when the key is missing.
check "github_accounts undeclared: signs with ~/.ssh/id" \
    "sign /home/<user>/.ssh/id" \
    "$(verdict undeclared '')"

echo "=== play-git-configure-and-tools.yml gates every signing task ==="

# Ansible parses the play (the same parser that will run it) and hands it over as JSON.
cat > "$work/dump.yml" <<EOF
- hosts: desktop
  gather_facts: false
  tasks:
    - name: Dump the Play as JSON
      ansible.builtin.copy:
        dest: "$work/play.json"
        content: "{{ lookup('ansible.builtin.file', '$PLAY_FILE') | from_yaml | to_json }}"
        mode: "0600"
EOF
inventory "$work/dump.inventory.yml" ""
if ! dump_out=$(run_ansible ansible-playbook -i "$work/dump.inventory.yml" "$work/dump.yml"); then
    echo "FAIL: ansible could not parse $PLAY_FILE:" >&2
    printf '%s\n' "$dump_out" >&2
    exit 1
fi

# Prints `gates <n>` (blocks gated on git_signing_declared), `gated <n>` (signing tasks
# inside them) and `ungated <task name>` per signing task outside, and writes the gated
# block into a playbook of its own for part 3.
if ! findings=$(python3 - "$work/play.json" "$work/block-play.yml" "$VARS_FILE" <<'PY'
import json
import sys

GATE = "git_signing_declared"
SIGNING = ("git_signing_key_path", "git_signing_public_key", "gpgsign")
# The retire block compares the two paths so it cannot delete the key in use. It reads no
# file and configures nothing, so it is the one reference allowed outside the gate.
RETIRE_GUARD = "git_signing_key_path != git_signing_retired_machine_key"


def conditions(task):
    when = task.get("when", [])
    return [when] if isinstance(when, str) else list(when)


def touches_signing(task):
    body = {key: value for key, value in task.items() if key not in ("block", "rescue", "always")}
    text = json.dumps(body)
    return any(token in text for token in SIGNING)


def is_retire_guard(task):
    return task.get("ansible.builtin.assert", {}).get("that") == RETIRE_GUARD


def walk(tasks, gated, out):
    for task in tasks:
        gate_here = "block" in task and GATE in conditions(task)
        if gate_here:
            out["blocks"].append(task)
        if "block" in task:
            for key in ("block", "rescue", "always"):
                walk(task.get(key, []), gated or gate_here, out)
            continue
        if not touches_signing(task) or is_retire_guard(task):
            continue
        if gated:
            out["gated"] += 1
        else:
            out["ungated"].append(task.get("name", "<unnamed task>"))


plays_file, block_play_file, vars_file = sys.argv[1:4]
with open(plays_file, encoding="utf-8") as handle:
    plays = json.load(handle)
found = {"blocks": [], "gated": 0, "ungated": []}
for play in plays:
    walk(play.get("tasks", []), False, found)

# JSON is YAML, so the block goes into a playbook verbatim, `when:` and all.
with open(block_play_file, "w", encoding="utf-8") as handle:
    json.dump([{
        "hosts": "desktop",
        "gather_facts": False,
        "vars_files": [vars_file],
        "environment": {"HOME": "{{ git_home }}"},
        "tasks": found["blocks"][:1],
    }], handle)

print(f"gates {len(found['blocks'])}")
print(f"gated {found['gated']}")
for name in found["ungated"]:
    print(f"ungated {name}")
PY
); then
    echo "FAIL: the play walker crashed" >&2
    exit 1
fi

gates=$(awk '$1 == "gates" {print $2}' <<< "$findings")
gated=$(awk '$1 == "gated" {print $2}' <<< "$findings")
ungated=$(awk '$1 == "ungated" {sub(/^ungated /, ""); print}' <<< "$findings")

check "exactly one block is gated on git_signing_declared" "1" "$gates"
# stat, assert, probe, assert, slurp, record, git_config: fewer means one moved out or the
# walker stopped seeing them, and either must be looked at.
check "the gate holds all seven signing tasks" "7" "$gated"
check "no signing task runs outside the gate" "" "$ungated"

echo "=== the gated block, run ==="

# run_block <case> <host-vars> — runs the play's own signing block with git's HOME and the
# recorded host_vars in the case's directory. Prints ansible's output; returns its status.
run_block() {
    local name="$1" dir="$work/case-$1"
    mkdir -p "$dir/home" "$dir/root/environment/localhost/host_vars"
    printf 'user_login: "<user>"\n' >"$dir/root/environment/localhost/host_vars/localhost.yml"
    inventory "$dir/inventory.yml" "          root_dir: \"$dir/root\"
          git_home: \"$dir/home\"
$2"
    run_ansible ansible-playbook -i "$dir/inventory.yml" "$work/block-play.yml"
}

# git_value <case> <key> — the value in the case's global git config, or `unset`.
git_value() {
    local config="$work/case-$1/home/.gitconfig" value
    if [ ! -f "$config" ]; then
        printf 'unset'
        return
    fi
    if value=$(git config --file "$config" --get "$2"); then
        printf '%s' "$value"
    else
        printf 'unset'
    fi
}

# recorded <case> — how many git_signing_public_key lines the play wrote to host_vars.
recorded() {
    grep -c '^git_signing_public_key: ' "$work/case-$1/root/environment/localhost/host_vars/localhost.yml"
}

if [ "$gates" != "1" ]; then
    echo "  (skipped: part 3 needs exactly one gated block, and the checks above already failed)"
else
    # The headless none box: no key anywhere, and nothing to sign with.
    if none_out=$(run_block none '          github_accounts: {}'); then none_rc=0; else none_rc=$?; fi
    check "none: the block runs clean with no key on disk" "0" "$none_rc"
    [ "$none_rc" -eq 0 ] || printf '%s\n' "$none_out" >&2
    check "none: commit.gpgsign is not set" "unset" "$(git_value none commit.gpgsign)"
    check "none: user.signingkey is not set" "unset" "$(git_value none user.signingkey)"
    check "none: no public key is recorded" "0" "$(recorded none)"

    # A desktop: a passphrase-protected key, as run.bash makes, named by git_signing_key.
    mkdir -p "$work/keys"
    if ! keygen_out=$(ssh-keygen -q -t ed25519 -N 'test-passphrase' -C test-key -f "$work/keys/id" 2>&1); then
        echo "FAIL: ssh-keygen could not make the test key: $keygen_out" >&2
        exit 1
    fi
    if desktop_out=$(run_block desktop "          github_accounts:
            personal: example-user
          git_signing_key: \"$work/keys/id\""); then desktop_rc=0; else desktop_rc=$?; fi
    check "desktop: the block runs clean" "0" "$desktop_rc"
    [ "$desktop_rc" -eq 0 ] || printf '%s\n' "$desktop_out" >&2
    check "desktop: commit.gpgsign is true" "true" "$(git_value desktop commit.gpgsign)"
    check "desktop: tag.gpgsign is true" "true" "$(git_value desktop tag.gpgsign)"
    check "desktop: gpg.format is ssh" "ssh" "$(git_value desktop gpg.format)"
    check "desktop: user.signingkey is the declared key" "$work/keys/id" "$(git_value desktop user.signingkey)"
    check "desktop: the public key is recorded once" "1" "$(recorded desktop)"

    # A desktop whose key is gone still fails, loudly, at the assert.
    if missing_out=$(run_block missing "          github_accounts:
            personal: example-user
          git_signing_key: \"$work/keys/absent\""); then missing_rc=0; else missing_rc=$?; fi
    check "desktop, key missing: the play fails" "failed" "$([ "$missing_rc" -ne 0 ] && echo failed || echo passed)"
    missing_said=0
    if grep -q 'must be a regular file at mode 0600' <<< "$missing_out"; then missing_said=1; fi
    check "desktop, key missing: it fails at the key assert" "1" "$missing_said"
    check "desktop, key missing: commit.gpgsign is not set" "unset" "$(git_value missing commit.gpgsign)"
fi

echo
echo "passed: $passed  failed: $failed"
[ "$failed" -eq 0 ]
