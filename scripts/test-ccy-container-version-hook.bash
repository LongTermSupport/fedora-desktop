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

printf 'unrelated\n' >"$repo/README.txt"
check "a commit touching no image file is not judged" "0" "$(hook_status)"
check "…and prints no container check" "no" "$(has 'container version bump requirement')"

echo "=== every repo file the real play puts into the image is guarded by the gate ==="

# The gate's paths must not drift from what the image actually copies. Read the real play's
# copy tasks and its pingbus zipapp build into the build context, keep those landing under a
# real Dockerfile COPY source, and run the real hook against a change to each repo file found.
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
if ! python3 -B -s - "$REPO_ROOT" >"$work/image-files" 2>"$work/image-files.err" <<'PY'; then
import pathlib
import subprocess
import sys

import yaml

root = pathlib.Path(sys.argv[1])
sys.path.insert(0, str(root))
from helpers.pingbus import bundle

CONTEXT = "/opt/claude-yolo/"
KNOWN = {"ansible.builtin.copy", "ansible.builtin.file", "ansible.builtin.command"}
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


def tracked_file(path):
    hits = [t for t in tracked if t == path or (path.endswith("/") and t.startswith(path))]
    if not hits:
        sys.exit(f"the play copies {path}, which git does not track")
    return hits[0]


def render(text, item):
    if isinstance(item, dict):
        for key, value in item.items():
            text = text.replace("{{ item.%s }}" % key, value)
    elif item is not None:
        text = text.replace("{{ item | basename }}", item.rsplit("/", 1)[-1])
        text = text.replace("{{ item }}", item)
    return text.replace("{{ root_dir }}/", "")


found = []
play = yaml.safe_load((root / "playbooks/imports/play-claude-yolo.yml").read_text())
for task in (t for p in play for t in (p.get("pre_tasks") or []) + (p.get("tasks") or [])):
    for module, args in task.items():
        if not isinstance(args, dict) or not str(args.get("dest", "")).startswith(CONTEXT):
            continue
        if module not in KNOWN:
            sys.exit(f"task '{task.get('name')}' writes into the build context with {module}, "
                     "which this test cannot read: teach it, so the gate's paths stay checked")
    copy = task.get("ansible.builtin.copy")
    for item in (task.get("loop") or [None]) if copy else []:
        dest = render(copy["dest"], item)
        if not dest.startswith(CONTEXT):
            continue
        if "src" not in copy:
            sys.exit(f"task '{task.get('name')}' writes content into the build context: teach this test")
        src, rel = render(copy["src"], item), dest[len(CONTEXT):]
        if dest.endswith("/") and not src.endswith("/"):
            rel += src.rsplit("/", 1)[-1]
        # A directory copy: keep its first file that lands under a COPY source.
        files = [t for t in tracked if t.startswith(src)] if src.endswith("/") else [tracked_file(src)]
        found += [f for f in files if copied(rel + f[len(src):] if src.endswith("/") else rel)][:1]
    command = task.get("ansible.builtin.command")
    argv = command.get("argv") or [] if isinstance(command, dict) else []
    if "helpers.pingbus.bundle" in argv:
        out = argv[argv.index("--out") + 1]
        if out.startswith(CONTEXT) and copied(out[len(CONTEXT):]):
            found += [tracked_file(package + "/") for package in bundle.PACKAGES]
print("\n".join(dict.fromkeys(found)))
PY
    cat "$work/image-files.err"
    check "the play's image files can be listed" "0" "1"
fi
check "the play copies files from files/opt/claude-yolo into the image" "yes" \
    "$(if grep -q '^files/opt/claude-yolo/' "$work/image-files"; then echo yes; else echo no; fi)"
check "the pingbus zipapp's sources are found (helpers/pingbus and helpers/agent_bus)" "2" \
    "$(grep -c -E '^helpers/(pingbus|agent_bus)/' "$work/image-files")"

repo="$work/real"
mkdir -p "$repo/hooks"
cp -R "$HOOK_DIR/." "$repo/hooks/"
for path in files/var/local/claude-yolo/Dockerfile files/var/local/claude-yolo/claude-yolo $(cat "$work/image-files"); do
    mkdir -p "$(dirname "$repo/$path")"
    cp "$REPO_ROOT/$path" "$repo/$path"
done
git -C "$repo" init -q
git -C "$repo" config user.name test
git -C "$repo" config user.email test@example.com
git -C "$repo" add -A
git -C "$repo" commit -q --no-verify -m baseline
while IFS= read -r path; do
    printf 'drift probe\n' >>"$repo/$path"
    check "a change to $path without a container bump is rejected" "1 yes" "$(hook_status) $(has 'container version bump required')"
    git -C "$repo" show "HEAD:$path" >"$repo/$path"
    git -C "$repo" add -A
done <"$work/image-files"

echo
printf 'passed: %s  failed: %s\n' "$passed" "$failed"
[ "$failed" -eq 0 ]
