"""Render play-claude-yolo.yml's two regex backreference expressions as Ansible does.

This reproduces the defect fixed under the rule regex-backref-escape (CLAUDE/QA.md): on the
play before the fix, "Read CCY_VERSION for the Go Front's Build" fails with
"'NoneType' object has no attribute 'group'"; after it, it gives the launcher's CCY_VERSION.
The claude_channel_sync changed_when must give True for a changed version and False for an
unchanged one.

Usage, from the repository root, with the Python that has ansible-core:
    python3 -I CLAUDE/Plan/00191-go-cli-tooling/regex-backref-proof/play_task.py \
        playbooks/imports/play-claude-yolo.yml files/var/local/claude-yolo/claude-yolo
Exit 0 when both expressions give the expected values, 1 otherwise.
"""

import re
import sys
from pathlib import Path

from ansible.errors import AnsibleError
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

LOOKUP = "lookup('ansible.builtin.file', root_dir ~ '/files/var/local/claude-yolo/claude-yolo')"


def tasks(plays):
    """Every task of every play, block bodies included."""
    stack = [t for p in plays for s in ("pre_tasks", "tasks", "post_tasks") for t in p.get(s) or []]
    while stack:
        task = stack.pop(0)
        yield task
        for section in ("block", "rescue", "always"):
            stack.extend(task.get(section) or [])


def render(label, func):
    """Print one rendering; return its value, or None when Ansible failed it."""
    try:
        got = func()
    except AnsibleError as exc:  # the filter's failure is the defect being reproduced
        print(f"{label}: ERROR {str(exc).splitlines()[0]}")
        return None
    print(f"{label}: {got!r}")
    return got


def main(play_path, launcher):
    """Check both expressions; return the exit status."""
    loader = DataLoader()
    all_tasks = list(tasks(loader.load_from_file(play_path)))
    launcher_text = Path(launcher).read_text()
    want_version = re.search(r'^CCY_VERSION="([0-9][0-9.]*)"', launcher_text, re.M).group(1)

    read = next(t for t in all_tasks if t.get("name") == "Read CCY_VERSION for the Go Front's Build")
    expr = read["ansible.builtin.set_fact"]["ccy_front_version"]
    if LOOKUP not in expr:
        sys.exit("the task no longer reads the launcher through the expected lookup; update this proof")
    templar = Templar(loader=loader, variables={"launcher_text": launcher_text})
    got = render("ccy_front_version", lambda: templar.template(trust_as_template(expr.replace(LOOKUP, "launcher_text"))))
    ok = got == want_version

    cond = next(t for t in all_tasks if t.get("register") == "claude_channel_sync")["changed_when"]
    for stdout, want in (("x\nCCVER before=1.0.1 after=1.0.2", True), ("x\nCCVER before=1.0.1 after=1.0.1", False)):
        ctemplar = Templar(loader=loader, variables={"claude_channel_sync": {"stdout": stdout}})
        verdict = render(f"changed_when on {stdout.splitlines()[-1]!r}", lambda: ctemplar.evaluate_conditional(trust_as_template(cond)))
        ok = ok and verdict is want
    print("both expressions are right" if ok else "an expression is wrong", file=sys.stderr)
    return 0 if ok else 1


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: play_task.py <play-claude-yolo.yml> <claude-yolo launcher>")
    sys.exit(main(sys.argv[1], sys.argv[2]))
