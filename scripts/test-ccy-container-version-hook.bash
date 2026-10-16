#!/usr/bin/env bash
# Unit-test the pre-commit hook's container version gate, and its CCY_VERSION gate over ccy's
# one-key agent, whose source lives under helpers/ rather than beside the launcher.
#
# WHY THIS TEST EXISTS. The entrypoint and the supervisor plugins are baked into the ccy image,
# and a running container only picks up a change to them through a container version bump. The
# gate rejects a commit that changes them without one, and a commit whose Dockerfile LABEL and
# launcher REQUIRED_CONTAINER_VERSION disagree. It runs the real hook against a throwaway
# repository holding a minimal copy of the ccy tree.
#
# `set -e` is deliberately NOT used: every case must run so the summary reports the full
# picture, and each result is checked explicitly.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOOK_DIR="$SCRIPT_DIR/git-hooks"

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

repo="$work/repo"
ccy="$repo/files/var/local/claude-yolo"
mkdir -p "$ccy/supervisor-plugins" "$repo/hooks"
cp -R "$HOOK_DIR/." "$repo/hooks/"
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.com

# write_launcher <ccy-version> <container-version>
write_launcher() {
    printf '#!/usr/bin/env bash\nCCY_VERSION="%s"\nREQUIRED_CONTAINER_VERSION="%s"\n' "$1" "$2" >"$ccy/claude-yolo"
}
# write_dockerfile <container-version> — the image copies the entrypoint, the plugin directory
# and one more file with a flag; a --from copy names a build-stage path, not a repo file.
write_dockerfile() {
    printf '%s\n' 'FROM scratch' "LABEL claude-yolo-version=\"$1\"" \
        'COPY entrypoint.sh /usr/local/bin/entrypoint.sh' \
        'COPY supervisor-plugins/ /opt/claude-yolo/supervisor-plugins/' \
        'COPY --chown=root:root guard-tool /opt/claude-yolo/guard-tool' \
        'COPY docs/ /opt/claude-yolo/docs/' \
        'COPY optional/ /opt/claude-yolo/optional/' \
        'COPY --from=builder /build/not-in-repo /usr/local/bin/not-in-repo' >"$ccy/Dockerfile"
}
# The build context is filled from a second repo root, files/opt/claude-yolo, and the pingbus
# zipapp in optional/ is built from helpers/pingbus and helpers/agent_bus.
opt="$repo/files/opt/claude-yolo"
write_second_root() {
    mkdir -p "$opt/docs" "$repo/helpers/pingbus" "$repo/helpers/agent_bus"
    printf 'the guide\n' >"$opt/docs/GUIDE.txt"
    printf 'host only\n' >"$opt/not-copied.txt"
    printf 'SEATS = 1\n' >"$repo/helpers/pingbus/cli.py"
    printf 'SEATS = 1\n' >"$repo/helpers/agent_bus/cli.py"
}

write_launcher 1.0.0 2.0
write_dockerfile 2.0
printf '#!/usr/bin/env bash\necho start\n' >"$ccy/entrypoint.sh"
printf 'PLUGIN_API = 1\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
printf '#!/usr/bin/env bash\necho guard\n' >"$ccy/guard-tool"
printf '#!/usr/bin/env bash\necho host\n' >"$ccy/host-only-tool"
filter_src="$repo/helpers/ssh_agent_filter/ssh_agent_filter.py"
mkdir -p "$(dirname "$filter_src")"
printf 'MAX = 1\n' >"$filter_src"
write_second_root
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m baseline

# hook_status — stage everything, run the real hook, print its exit status; reset the index.
hook_status() {
    local rc=0
    git -C "$repo" add -A
    (cd "$repo" && bash hooks/pre-commit) >"$work/hook.out" 2>&1 || rc=$?
    echo "$rc"
}
# baseline — put the tree back to the committed state.
baseline() {
    write_launcher 1.0.0 2.0
    write_dockerfile 2.0
    printf '#!/usr/bin/env bash\necho start\n' >"$ccy/entrypoint.sh"
    printf 'PLUGIN_API = 1\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
    printf '#!/usr/bin/env bash\necho guard\n' >"$ccy/guard-tool"
    printf '#!/usr/bin/env bash\necho host\n' >"$ccy/host-only-tool"
    printf 'MAX = 1\n' >"$filter_src"
    write_second_root
    git -C "$repo" add -A
}
has() { if grep -q -F -- "$1" "$work/hook.out"; then echo yes; else echo no; fi; }

echo "=== the container version gate ==="

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
check "a plugin change without a container bump is rejected" "1" "$(hook_status)"
check "…and says to bump the container version" "yes" "$(has 'container version bump required')"
baseline

printf '#!/usr/bin/env bash\necho started\n' >"$ccy/entrypoint.sh"
check "an entrypoint change without a container bump is rejected" "1" "$(hook_status)"
baseline

printf '#!/usr/bin/env bash\n# why it starts\necho start\n' >"$ccy/entrypoint.sh"
check "a comment-only entrypoint change needs no bump" "0" "$(hook_status)"
baseline

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
write_dockerfile 2.1
write_launcher 1.0.1 2.1
check "a plugin change with both versions bumped together passes" "0" "$(hook_status)"
check "…and reports the bump" "yes" "$(has '2.0 → 2.1')"
baseline

printf 'PLUGIN_API = 1\nVALUE = 2\n' >"$ccy/supervisor-plugins/ccy_lifecycle.py"
write_dockerfile 2.1
check "a LABEL bump the launcher does not match is rejected" "1" "$(hook_status)"
check "…and names both values" "yes" "$(has '(2.1) and the launcher')"
baseline

printf '#!/usr/bin/env bash\necho guarded\n' >"$ccy/guard-tool"
check "any file the Dockerfile COPYs is guarded, not just a listed few" "1" "$(hook_status)"
baseline

printf '#!/usr/bin/env bash\necho host side\n' >"$ccy/host-only-tool"
check "a file in the ccy directory the image does not copy needs no container bump" "0" "$(hook_status)"
check "…and prints no container check" "no" "$(has 'container version bump requirement')"
baseline

echo "=== the second context root and the pingbus zipapp sources (Plan 00202) ==="

printf 'the new guide\n' >"$opt/docs/GUIDE.txt"
check "a change under files/opt/claude-yolo the image copies, without a bump, is rejected" "1" "$(hook_status)"
check "…and says to bump the container version" "yes" "$(has 'container version bump required')"
baseline

printf 'the new guide\n' >"$opt/docs/GUIDE.txt"
write_dockerfile 2.1
write_launcher 1.0.1 2.1
check "…and passes with both versions bumped together" "0" "$(hook_status)"
baseline

printf 'host only, changed\n' >"$opt/not-copied.txt"
check "a file under files/opt/claude-yolo the image does not copy needs no container bump" "0" "$(hook_status)"
baseline

for pkg in pingbus agent_bus; do
    printf 'SEATS = 2\n' >"$repo/helpers/$pkg/cli.py"
    check "a helpers/$pkg change without a container bump is rejected" "1" "$(hook_status)"
    baseline
    printf 'SEATS = 2\n' >"$repo/helpers/$pkg/cli.py"
    write_dockerfile 2.1
    write_launcher 1.0.1 2.1
    check "…and passes with both versions bumped together" "0" "$(hook_status)"
    baseline
done

echo "=== deletions, headings and shebangs are changes too ==="

rm "$repo/helpers/pingbus/cli.py"
check "deleting a helpers/pingbus file without a container bump is rejected" "1" "$(hook_status)"
check "…and names the deleted file" "yes" "$(has 'helpers/pingbus/cli.py')"
baseline

rm "$ccy/supervisor-plugins/ccy_lifecycle.py"
check "deleting a supervisor plugin without a container bump is rejected" "1" "$(hook_status)"
baseline

rm "$repo/helpers/pingbus/cli.py"
write_dockerfile 2.1
write_launcher 1.0.1 2.1
check "…and the deletion passes with both versions bumped together" "0" "$(hook_status)"
baseline

printf '# The guide\n' >"$opt/docs/SKILL.md"
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m 'add a skill'
printf '# The renamed guide\n' >"$opt/docs/SKILL.md"
check "a markdown heading change in an image file without a container bump is rejected" "1" "$(hook_status)"
git -C "$repo" rm -q -f "$opt/docs/SKILL.md"
git -C "$repo" commit -q --no-verify -m 'drop the skill'

printf '#!/bin/sh\necho start\n' >"$ccy/entrypoint.sh"
check "a shebang change in an image script without a container bump is rejected" "1" "$(hook_status)"
baseline

echo "=== the CCY version gate: ccy's one-key agent (Plan 00163) ==="

# Its source is a helper, deployed into lib/ beside the libraries the gate already covers.
printf 'MAX = 2\n' >"$filter_src"
check "a one-key agent change without a CCY_VERSION bump is rejected" "1" "$(hook_status)"
check "…and says to bump CCY_VERSION" "yes" "$(has 'CCY version bump required')"
baseline

printf 'MAX = 2\n' >"$filter_src"
write_launcher 1.0.1 2.0
check "a one-key agent change with CCY_VERSION bumped passes" "0" "$(hook_status)"
baseline

echo "=== a diff larger than a pipe buffer is still judged ==="

# Both gates read `git diff` through a pipe into a reader that stops at the first changed code
# line. With a diff bigger than the pipe buffer, git is still writing when the reader exits, so
# git dies of SIGPIPE; under `set -o pipefail` the check then read false and the change passed.
# big_py <file> <value> — 20000 code lines, so rewriting every one is a diff of about a megabyte.
big_py() { awk -v v="$2" 'BEGIN { for (i = 0; i < 20000; i++) printf "VALUE_%05d = \"%s\"\n", i, v }' >"$1"; }
for big in helpers/pingbus/big.py helpers/ssh_agent_filter/big.py; do
    big_py "$repo/$big" old
done
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m 'add large image files'
big_py "$repo/helpers/pingbus/big.py" new
check "a large helpers/pingbus change without a container bump is rejected" "1" "$(hook_status)"
check "…and says to bump the container version" "yes" "$(has 'container version bump required')"
git -C "$repo" show HEAD:helpers/pingbus/big.py >"$repo/helpers/pingbus/big.py"
big_py "$repo/helpers/ssh_agent_filter/big.py" new
check "a large one-key agent change without a CCY_VERSION bump is rejected" "1" "$(hook_status)"
check "…and says to bump CCY_VERSION" "yes" "$(has 'CCY version bump required')"
git -C "$repo" show HEAD:helpers/ssh_agent_filter/big.py >"$repo/helpers/ssh_agent_filter/big.py"
baseline

printf 'unrelated\n' >"$repo/README.txt"
check "a commit touching no image file is not judged" "0" "$(hook_status)"
check "…and prints no container check" "no" "$(has 'container version bump requirement')"

echo "=== the gate covers exactly the repo files the real play puts into the image ==="

# The gate's paths must not drift from what the image actually copies. Read the real play's
# tasks (and the task files it includes) that write into the build context: its copy tasks and
# its pingbus zipapp build. Keep the repo files landing under a real Dockerfile COPY source, and
# run the real hook against a change to each one; every other tracked file under
# files/opt/claude-yolo/ must not be judged. Any other task naming the build context must be one
# of the known readers below, or the test fails: a new way of writing into the image is then
# taught here deliberately, never missed.
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if ! python3 -B -s - "$REPO_ROOT" >"$work/image-files" 2>"$work/image-files.err" <<'PY'; then
import json
import pathlib
import subprocess
import sys

import yaml

root = pathlib.Path(sys.argv[1])
sys.path.insert(0, str(root))
from helpers.pingbus import bundle

CONTEXT = "/opt/claude-yolo/"
READERS = {"Calculate Dockerfile Hash for Build", "Build Claude YOLO Container Image with Hash"}
INCLUDES = ("ansible.builtin.include_tasks", "ansible.builtin.import_tasks")
sources = []
for line in (root / "files/var/local/claude-yolo/Dockerfile").read_text().splitlines():
    words = line.split()
    if words[:1] == ["COPY"] and not any(w.startswith("--from=") for w in words):
        args = [w for w in words[1:] if not w.startswith("--")]
        sources += [a.rstrip("/") for a in args[:-1]]
tracked = subprocess.run(["git", "-C", str(root), "ls-files"], check=True,
                         capture_output=True, text=True).stdout.splitlines()


def copied(rel):
    rel = rel.rstrip("/")
    return any(rel == s or rel.startswith(s + "/") for s in sources)


def tracked_under(path):
    hits = [t for t in tracked if t == path or (path.endswith("/") and t.startswith(path))]
    if not hits:
        sys.exit(f"the play copies {path}, which git does not track")
    return hits


def render(text, item):
    if isinstance(item, dict):
        for key, value in item.items():
            text = text.replace("{{ item.%s }}" % key, value)
    elif item is not None:
        text = text.replace("{{ item | basename }}", item.rsplit("/", 1)[-1])
        text = text.replace("{{ item }}", item)
    return text.replace("{{ root_dir }}/", "")


def walk(tasks):
    for task in tasks or []:
        for key in ("block", "rescue", "always"):
            if isinstance(task.get(key), list):
                yield from walk(task[key])
        for key in INCLUDES:
            if key in task:
                ref = task[key] if isinstance(task[key], str) else task[key]["file"]
                yield from walk(yaml.safe_load((root / render(ref, None)).read_text()))
        yield task


def teach(task, why):
    sys.exit(f"task '{task.get('name')}' {why}: teach this test, so the gate's paths stay checked")


found = []
play = yaml.safe_load((root / "playbooks/imports/play-claude-yolo.yml").read_text())
for task in walk([t for p in play for t in (p.get("pre_tasks") or []) + (p.get("tasks") or [])]):
    if CONTEXT.rstrip("/") not in json.dumps(task) or any(k in task for k in INCLUDES):
        continue
    copy, command = task.get("ansible.builtin.copy"), task.get("ansible.builtin.command")
    argv = command.get("argv") or [] if isinstance(command, dict) else []
    if copy:
        for item in task.get("loop") or [None]:
            dest = render(copy["dest"], item)
            if not dest.startswith(CONTEXT):
                continue
            if "src" not in copy:
                teach(task, "writes content into the build context")
            src, rel = render(copy["src"], item), dest[len(CONTEXT):]
            if dest.endswith("/") and not src.endswith("/"):
                rel += src.rsplit("/", 1)[-1]
            directory = src.endswith("/")
            found += [f for f in tracked_under(src) if copied(rel + f[len(src):] if directory else rel)]
    elif "helpers.pingbus.bundle" in argv:
        out = argv[argv.index("--out") + 1]
        if out.startswith(CONTEXT) and copied(out[len(CONTEXT):]):
            found += [f for package in bundle.PACKAGES for f in tracked_under(package + "/")]
    elif "ansible.builtin.file" in task:
        if task["ansible.builtin.file"].get("state") not in ("directory", "absent"):
            teach(task, "makes something other than a directory or a removal in the build context")
    elif task.get("name") not in READERS and "ansible.builtin.debug" not in task:
        teach(task, "names the build context in a way this test cannot read")
print("\n".join(dict.fromkeys(found)))
PY
    cat "$work/image-files.err"
    check "the play's image files can be listed" "0" "1"
fi
check "the play copies files from files/opt/claude-yolo into the image" "yes" \
    "$(if grep -q '^files/opt/claude-yolo/' "$work/image-files"; then echo yes; else echo no; fi)"
check "the pingbus zipapp's sources are found (helpers/pingbus and helpers/agent_bus)" "2" \
    "$(grep -o -E '^helpers/(pingbus|agent_bus)/' "$work/image-files" | sort -u | wc -l)"

# Every tracked file under files/opt/claude-yolo/ the play does NOT put into the image.
(cd "$REPO_ROOT" && git ls-files files/opt/claude-yolo) | grep -v -x -F -f "$work/image-files" >"$work/host-files"
check "the host-only kit files under files/opt/claude-yolo are found" "yes" \
    "$(if grep -q '^files/opt/claude-yolo/optional/agent-bus/README.host$' "$work/host-files"; then echo yes; else echo no; fi)"

repo="$work/real"
mkdir -p "$repo/hooks"
cp -R "$HOOK_DIR/." "$repo/hooks/"
for path in files/var/local/claude-yolo/Dockerfile files/var/local/claude-yolo/claude-yolo \
    $(cat "$work/image-files" "$work/host-files"); do
    mkdir -p "$(dirname "$repo/$path")"
    cp "$REPO_ROOT/$path" "$repo/$path"
done
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.com
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m baseline
# probe <file> <wanted> — change the file, run the real hook, put the file back.
probe() {
    printf 'drift probe\n' >>"$repo/$1"
    check "$3" "$2" "$(hook_status) $(has 'container version bump required')"
    git -C "$repo" show "HEAD:$1" >"$repo/$1"
    git -C "$repo" add -A
}
while IFS= read -r path; do
    probe "$path" "1 yes" "a change to $path without a container bump is rejected"
done <"$work/image-files"
while IFS= read -r path; do
    probe "$path" "0 no" "a change to host-only $path needs no container bump"
done <"$work/host-files"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
