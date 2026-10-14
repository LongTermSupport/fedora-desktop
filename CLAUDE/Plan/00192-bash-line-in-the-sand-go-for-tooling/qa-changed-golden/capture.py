"""Golden capture: run the shipped bash qa-changed over a fixed world and record it.

Plan 00192, qa-changed port. Test tooling for a program that has been removed: it runs
scripts/qa-changed.bash (with scripts/qa-discovery.bash and scripts/lib/qa-helper-summary.bash)
as they were at a git revision, so the goldens in src-go/qa/changed/testdata/golden/ can be
re-derived and audited.

    python3 -I capture.py [--rev REV] [--out DIR]

The world is a small repository built from WORLD below (and written to world.json for the Go
replay): one file of each kind the router distinguishes, stub gates and suites, and a src-go
module with two commands. Every scenario in SCENARIOS is run against a fresh copy: the setup
edits the working tree, then the bash runs with stub tools first on PATH (each logs its argv
and exits as the scenario says). A record is the command line, stdout, stderr, the exit status
and the stubs' argv log, with the temporary root written as <ROOT>.

The same world and stub semantics are implemented again in Go (golden_test.go); the two must
agree, and every difference must be a numbered bug of SPEC-qa-changed.md section 10.
"""

import argparse
import json
import os
import re
import shutil
import subprocess
import tempfile
from pathlib import Path

REPO = Path(__file__).resolve().parents[4]
TESTDATA = REPO / "src-go/qa/changed/testdata"
DEFAULT_REV = "f0284848"  # F44 after the freeze merge: the bash as the spec read it
LEGACY = ["scripts/qa-changed.bash", "scripts/qa-discovery.bash", "scripts/lib/qa-helper-summary.bash"]
# System programs the bash needs; everything else on PATH is a stub or absent.
SYSTEM = ["bash", "sh", "git", "go", "awk", "grep", "sort", "mktemp", "cp", "rm", "dirname",
          "basename", "env", "cat", "mkdir", "jq", "find", "head", "tr", "uniq", "wc", "sed"]
STUBBED = ["shellcheck", "semgrep", "ruff", "ansible-playbook", "node", "python3"]
TIMEOUT = 120

GATE = """#!/usr/bin/bash
echo "gate:{name} json=${{QA_JSON_OUT:+1}}" >> "$STUB_LOG"
code=0
[[ -f "$STUB_DIR/exit-{name}" ]] && code="$(cat "$STUB_DIR/exit-{name}")"
[[ "$code" == 0 ]] || echo "{name}: stub failure" >&2
exit "$code"
"""
SUITE = """#!/usr/bin/bash
echo "suite:{name}" >> "$STUB_LOG"
code=0
[[ -f "$STUB_DIR/exit-{name}" ]] && code="$(cat "$STUB_DIR/exit-{name}")"
[[ "$code" == 0 ]] || {{ echo "{name}: stub failure" >&2; exit "$code"; }}
echo "passed: {count} failed: {failed}"
{mentions}
"""


def suite(name, count, mentions="", failed=0):
    """A stub suite whose text names the given files (a suite is picked by naming a file)."""
    text = SUITE.format(name=name, count=count, failed=failed, mentions="\n".join(f"# {m}" for m in mentions.split()))
    return text


WORLD = {
    ".gitignore": "ignored.bash\n",
    "scripts/qa-all.bash": (
        '#!/usr/bin/bash\n# bash "$SCRIPT_DIR/test-commented.bash"\n'
        'run bash "$SCRIPT_DIR/test-alpha.bash"\nrun bash "$SCRIPT_DIR/test-beta.bash"\n'
        'run bash "$SCRIPT_DIR/test-teams.bash"\nrun bash "$SCRIPT_DIR/qa-docs.bash"\n'
        'run bash "$SCRIPT_DIR/test-gone.bash"\n'
    ),
    "scripts/test-alpha.bash": suite("test-alpha.bash", 3, "tools/run.sh main.go lib/util.sh"),
    "scripts/test-beta.bash": suite("test-beta.bash", 5, "vars/names.yml a.txt tools/plain.js"),
    "scripts/test-teams.bash": suite("test-teams.bash", 9),
    "scripts/qa-toolchain.bash": GATE.format(name="qa-toolchain.bash"),
    "scripts/qa-ansible.bash": GATE.format(name="qa-ansible.bash"),
    "scripts/qa-docs.bash": GATE.format(name="qa-docs.bash"),
    "scripts/qa-go.bash": GATE.format(name="qa-go.bash"),
    "scripts/qa-bash-limits.bash": GATE.format(name="qa-bash-limits.bash"),
    "scripts/go-binary-suites.tsv": "teams\tscripts/test-teams.bash\nfrz\tqa-go\n",
    "scripts/bash-limits.allowlist": "# none\nfiles/home/.local/bin/listed-tool\t10\t1\t10\t-\n",
    "tools/run.sh": "#!/bin/sh\n:\n",
    "tools/py-tool": "#!/usr/bin/env python3\nx = 1\n",
    "tools/bash-tool": "#!/usr/bin/bash\n:\n",
    "tools/inline": "#!/usr/bin/bash -c 'echo hi'\n",
    "tools/plain.js": "// js\n",
    "lib/util.sh": "#!/bin/sh\n:\n",
    "helpers/pkg/mod.py": "x = 1\n",
    "helpers/pkg/lonely.py": "x = 2\n",
    "helpers/pkg/__init__.py": "",
    "helpers/agent_bus/declare.py": "x = 3\n",
    "tests/helpers/pkg/test_mod.py": "import unittest\n",
    "playbooks/imports/play-a.yml": "- hosts: all\n  tasks:\n    - include_tasks: ../../tasks/step.yml\n",
    "playbooks/play-main.yml": "- import_playbook: imports/play-a.yml\n",
    "tasks/step.yml": "- name: step\n  debug: msg=hi\n",
    "vars/names.yml": "k: v\n",
    "vars/unnamed.yml": "k: v\n",
    "other/data.yml": "k: v\n",
    "docs/readme.md": "# docs\n",
    "extensions/ext@x/extension.js": "// ext\n",
    "extensions/ext@x/ok.mjs": "// ext\n",
    "extensions/node_modules/dep.js": "// vendored\n",
    "src-go/go.mod": "module example.com/w\n\ngo 1.21\n",
    "src-go/go.sum": "",
    "src-go/.golangci.yml": 'version: "2"\n',
    "src-go/README.md": "# go\n",
    "src-go/cmd/teams/main.go": 'package main\n\nimport "example.com/w/internal/teams"\n\nfunc main() { teams.Run() }\n',
    "src-go/cmd/frz/main.go": 'package main\n\nimport "example.com/w/internal/frz"\n\nfunc main() { frz.Run() }\n',
    "src-go/internal/teams/t.go": "package teams\n\nfunc Run() {}\n",
    "src-go/internal/teams/t_test.go": "package teams\n",
    "src-go/internal/frz/f.go": "package frz\n\nfunc Run() {}\n",
    "src-go/internal/other/o.go": "package other\n",
    "src-go/qa/x/q.go": "package x\n",
    "files/var/local/claude-yolo/lib/common-pure.bash": "#!/usr/bin/bash\n:\n",
    "files/home/.local/bin/tmpl.j2": "#!/usr/bin/bash\n{{ x }}\n",
    "files/home/.local/bin/listed-tool": "#!/usr/bin/env python3\n",
    "untracked/skip.bash": "#!/usr/bin/bash\n:\n",
    "untracked/skip.py": "x = 1\n",
    "pkg1/main.go": "package main\n",
    "pkg2/main.go": "package main\n",
    "ccy": "#!/usr/bin/bash\n:\n",
    "ccy-teams": "#!/usr/bin/bash\n:\n",
    "a.txt": "text\n",
    "misc/other.txt": "text\n",
    ".github/workflows/ci.yml": "name: ci\n",
    ".semgrep/bash-conventions.yml": "rules: []\n",
    "ansible.cfg": "[defaults]\n",
    "kept.bash": "#!/usr/bin/bash\n:\n",
    "gone.bash": "#!/usr/bin/bash\n:\n",
}

ALL = "{ROOT}/tools/run.sh"


def scen(name, args, setup=None, **extra):
    """One scenario: a name, the command line, and edits to the working tree before it runs."""
    return {"name": name, "args": args, "setup": setup or {}, **extra}


def build_scenarios():
    """The corpus: every world file alone, grouped files, git collection and run mode."""
    out = [scen("file-" + re.sub(r"[^A-Za-z0-9]+", "-", f).strip("-"), ["--plan", "--files", f])
           for f in sorted(WORLD)]
    groups = {
        "shell-and-python": ["tools/run.sh", "helpers/pkg/mod.py", "tools/py-tool"],
        "go-closure": ["src-go/internal/teams/t.go", "src-go/internal/other/o.go", "src-go/go.mod"],
        "go-frz-only": ["src-go/internal/frz/f.go"],
        "everything": sorted(f for f in WORLD if not f.startswith("scripts/")),
        "same-basename": ["pkg1/main.go", "pkg2/main.go"],
        "name-in-name": ["ccy", "ccy-teams"],
        "suite-and-file": ["scripts/test-alpha.bash", "tools/run.sh"],
        "yaml-set": ["tasks/step.yml", "vars/names.yml", "vars/unnamed.yml", "other/data.yml"],
        "two-md": ["docs/readme.md", "src-go/README.md"],
    }
    out += [scen("group-" + k, ["--plan", "--files", *v]) for k, v in groups.items()]
    out += [
        scen("explicit-dot-slash", ["--plan", "--files", "./tools/run.sh"]),
        scen("explicit-duplicates", ["--plan", "--files", "tools/run.sh", "tools/run.sh"]),
        scen("explicit-absolute", ["--plan", "--files", ALL]),
        scen("explicit-missing", ["--plan", "--files", "no/such/file.bash"]),
        scen("explicit-empty", ["--plan", "--files"]),
        scen("explicit-then-flag", ["--files", "tools/run.sh", "--plan"]),
        scen("help", ["--help"]),
        scen("unknown-flag", ["--nope"]),
        scen("base-needs-ref", ["--base"]),
        scen("base-swallows-flag", ["--base", "--plan"]),
        scen("bad-base", ["--plan", "--base", "nosuchref"]),
        scen("clean-tree", ["--plan"]),
        scen("clean-tree-run", []),
        scen("git-modified-untracked-deleted", ["--plan"], {
            "write": {"kept.bash": "#!/usr/bin/bash\n: changed\n", "new.py": "x = 1\n", "ignored.bash": ":\n"},
            "delete": ["gone.bash"]}),
        scen("git-deleted-allowlisted", ["--plan"], {"delete": ["files/home/.local/bin/listed-tool"]}),
        scen("git-deleted-go", ["--plan"], {"delete": ["src-go/internal/teams/t.go"]}),
        scen("git-deleted-named-by-suite", ["--plan"], {"delete": ["lib/util.sh"]}),
        scen("git-rename", ["--plan"], {"rename": [["kept.bash", "renamed.bash"]]}),
        scen("git-base-committed", ["--plan", "--base", "{FIRST}"], {
            "write": {"kept.bash": "#!/usr/bin/bash\n: second\n"}, "commit": True}),
        scen("git-staged-only", ["--plan"], {"write": {"kept.bash": "#!/usr/bin/bash\n: staged\n"}, "stage": True}),
        scen("git-submodule-like-dir", ["--plan"], {"write": {"emptydir/.keep": ""}, "delete": []}),
    ]
    out += run_scenarios()
    return out


# Edits that make a diff touching every kind of check.
BIG = {"write": {
    "tools/run.sh": "#!/bin/sh\n: changed\n", "helpers/pkg/mod.py": "x = 9\n", "playbooks/imports/play-a.yml": "- hosts: all\n  tasks: []\n  # edit\n",
    "tasks/step.yml": "- name: step\n  debug: msg=ho\n", "docs/readme.md": "# docs 2\n", "tools/plain.js": "// js 2\n",
    "extensions/ext@x/extension.js": "// ext 2\n", "src-go/internal/teams/t.go": "package teams\n\nfunc Run() { _ = 1 }\n",
    "extensions/node_modules/.bin/eslint": "#!/bin/sh\necho \"eslint $* in $(basename \"$PWD\")\" >> \"$STUB_LOG\"\n",
}, "exec": ["extensions/node_modules/.bin/eslint"]}


def run_scenarios():
    """Run mode under stubs: every check, each failure alone, two together, missing tools."""
    big = dict(BIG)
    out = [scen("run-all-pass", [], big)]
    for tool in ["shellcheck", "semgrep", "ruff", "python3", "ansible-playbook", "node", "qa-ansible.bash",
                 "qa-docs.bash", "test-alpha.bash", "test-teams.bash", "qa-toolchain.bash"]:
        out.append(scen("run-fail-" + tool.replace(".", "-"), [], big, exits={tool: 1}))
    out += [
        scen("run-fail-two", [], big, exits={"shellcheck": 1, "ruff": 1}),
        scen("run-semgrep-finding", [], big, semgrepFindings=[{
            "path": "tools/run.sh.bash", "start": {"line": 3}, "check_id": "rule.x", "extra": {"message": "no"}}]),
        scen("run-gate-exit-2", [], big, exits={"qa-go.bash": 2}),
        scen("run-gate-exit-2-and-fail", [], big, exits={"qa-docs.bash": 2, "ruff": 1}),
        scen("run-bash-syntax-error", [], {"write": {"bad.bash": "if then\n"}}),
        scen("run-missing-shellcheck", [], big, missing=["shellcheck"]),
        scen("run-missing-ruff", [], big, missing=["ruff"]),
        scen("run-missing-node", [], big, missing=["node"]),
        scen("run-missing-ansible", [], big, missing=["ansible-playbook"]),
        scen("run-missing-eslint", [], {"write": {"extensions/ext@x/extension.js": "// ext 3\n"}}),
        scen("run-uncovered-only", [], {"write": {"misc/other.txt": "changed\n"}}),
        scen("run-markdown-only", [], {"write": {"docs/readme.md": "# again\n"}}),
        scen("run-suite-picked-by-name", [], {"write": {"a.txt": "changed\n"}}),
        scen("run-explicit-files", ["--files", "tools/run.sh", "helpers/pkg/mod.py"]),
        scen("run-go-closure", [], {"write": {"src-go/internal/teams/t.go": "package teams\n\nfunc Run() { _ = 2 }\n"}}),
        scen("run-suite-zero-count", [], {"write": {"scripts/test-teams.bash": suite("test-teams.bash", 0)}}),
        scen("run-suite-count-reports-failures", [], {"write": {"scripts/test-teams.bash": suite("test-teams.bash", 9, "", 2)}}),
        scen("run-excluded-only", [], {"write": {"untracked/skip.bash": "#!/usr/bin/bash\n: x\n"}}),
    ]
    return out


def sh(args, cwd, env=None):
    """Run a command, failing loudly; its stdout."""
    res = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True, check=False, timeout=TIMEOUT)
    if res.returncode != 0:
        msg = f"{args} failed ({res.returncode}): {res.stderr}"
        raise SystemExit(msg)
    return res.stdout.strip()


def write_tree(root, files, mode_exec=()):
    """Write files under root (parents created); entries in mode_exec are made executable."""
    for rel, text in files.items():
        path = root / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
        if rel in mode_exec:
            path.chmod(0o755)


def build_world(root, legacy):
    """The world repository at root, committed; returns the first commit's id."""
    git = ["git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"]
    env = {**os.environ, "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1"}
    sh(["git", "init", "-q"], root, env)
    write_tree(root, WORLD)
    for rel, text in legacy.items():
        write_tree(root, {rel: text})
    sh([*git, "add", "-A"], root, env)
    sh([*git, "commit", "-q", "-m", "base"], root, env)
    return sh(["git", "rev-parse", "HEAD"], root, env)


def apply_setup(root, setup, env):
    """Edit the working tree as the scenario says."""
    git = ["git", "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"]
    write_tree(root, setup.get("write", {}), setup.get("exec", ()))
    for rel in setup.get("delete", []):
        (root / rel).unlink()
    for old, new in setup.get("rename", []):
        sh([*git, "mv", old, new], root, env)
    if setup.get("stage") or setup.get("commit"):
        sh([*git, "add", "-A"], root, env)
    if setup.get("commit"):
        sh([*git, "commit", "-q", "-m", "second"], root, env)


def stub_tools(stubdir, bindir):
    """Stub programs that log their argv and exit as the scenario says, plus the system links."""
    plain = '#!/bin/sh\necho "{name} $*{extra}" >> "$STUB_LOG"\nc=0\n[ -f "$STUB_DIR/exit-{name}" ] && c=$(cat "$STUB_DIR/exit-{name}")\n[ "$c" = 0 ] || echo "{name}: stub failure" >&2\nexit "$c"\n'
    extras = {
        "python3": ' | GIT_CONFIG_GLOBAL=$GIT_CONFIG_GLOBAL GIT_CONFIG_NOSYSTEM=$GIT_CONFIG_NOSYSTEM',
        "ansible-playbook": " | ANSIBLE_CONFIG=$ANSIBLE_CONFIG",
    }
    for name in STUBBED:
        text = plain.format(name=name, extra=extras.get(name, ""))
        if name == "semgrep":
            text = SEMGREP
        path = bindir / name
        path.write_text(text)
        path.chmod(0o755)
    for name in SYSTEM:
        found = shutil.which(name)
        if found:
            (bindir / name).symlink_to(found)
    (stubdir).mkdir(exist_ok=True)


SEMGREP = """#!/bin/sh
out=""
prev=""
for a in "$@"; do
  [ "$prev" = "--json-output" ] && out="$a"
  prev="$a"
done
echo "semgrep $* | mirror: $(find . -type f | sort | tr '\\n' ' ')" >> "$STUB_LOG"
if [ -f "$STUB_DIR/semgrep-findings" ]; then cp "$STUB_DIR/semgrep-findings" "$out"; else echo '{"results":[]}' > "$out"; fi
c=0
[ -f "$STUB_DIR/exit-semgrep" ] && c=$(cat "$STUB_DIR/exit-semgrep")
[ "$c" = 0 ] || echo "semgrep: stub failure" >&2
exit "$c"
"""


def normalise(text, root, scripts_root):
    """Temporary paths out, placeholders in."""
    text = text.replace(str(scripts_root), "<ROOT>").replace(str(root), "<ROOT>")
    text = re.sub(r"--json-output \S+", "--json-output <JSON>", text)
    return re.sub(r"/tmp/[A-Za-z0-9._-]+", "<TMP>", text)


def run_one(scen_, rev_files, first_holder):
    """Run the bash for one scenario in a fresh world; the record."""
    with tempfile.TemporaryDirectory() as tmp:
        tmp = Path(tmp)
        root = tmp / "world"
        root.mkdir()
        stub = tmp / "stub"
        stub.mkdir()
        bindir = tmp / "bin"
        bindir.mkdir()
        stub_tools(stub, bindir)
        env = {**os.environ, "GIT_CONFIG_GLOBAL": "/dev/null", "GIT_CONFIG_NOSYSTEM": "1"}
        first = build_world(root, rev_files)
        apply_setup(root, scen_["setup"], env)
        for name in scen_.get("missing", []):
            (bindir / name).unlink()
        for name, code in scen_.get("exits", {}).items():
            (stub / f"exit-{name}").write_text(str(code))
        if "semgrepFindings" in scen_:
            (stub / "semgrep-findings").write_text(json.dumps({"results": scen_["semgrepFindings"]}))
        log = tmp / "log"
        run_env = {**env, "PATH": str(bindir), "STUB_LOG": str(log), "STUB_DIR": str(stub)}
        args = [a.replace("{ROOT}", str(root)).replace("{FIRST}", first) for a in scen_["args"]]
        res = subprocess.run(["bash", str(root / "scripts/qa-changed.bash"), *args], cwd=root, env=run_env,
                             capture_output=True, text=True, check=False, timeout=TIMEOUT)
        lines = log.read_text().splitlines() if log.exists() else []
        first_holder.append(first)
        return {
            "args": scen_["args"],
            "stdout": normalise(res.stdout, root, root).replace(first, "<FIRST>"),
            "stderr": normalise(res.stderr, root, root).replace(first, "<FIRST>"),
            "status": res.returncode,
            "log": [normalise(line, root, root) for line in lines],
        }


def legacy_files(rev):
    """The bash as it was at the revision: the files the golden is recorded from."""
    return {rel: sh(["git", "show", f"{rev}:{rel}"], REPO) + "\n" for rel in LEGACY}


def main():
    """Record every scenario from the revision into the testdata directory."""
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--rev", default=DEFAULT_REV)
    parser.add_argument("--out", default=str(TESTDATA))
    opts = parser.parse_args()
    out = Path(opts.out)
    rev = sh(["git", "rev-parse", opts.rev], REPO)
    legacy = legacy_files(rev)
    scenarios = build_scenarios()
    (out / "golden").mkdir(parents=True, exist_ok=True)
    (out / "world.json").write_text(json.dumps(WORLD, indent=1, sort_keys=True) + "\n")
    (out / "scenarios.json").write_text(json.dumps({"revision": rev, "scenarios": scenarios}, indent=1) + "\n")
    firsts = []
    for scen_ in scenarios:
        record = run_one(scen_, legacy, firsts)
        (out / "golden" / f"{scen_['name']}.json").write_text(json.dumps(record, indent=1) + "\n")
    print(f"recorded {len(scenarios)} scenarios from {rev}")


if __name__ == "__main__":
    main()
