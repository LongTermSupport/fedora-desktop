"""Prove the regex-backref-escape fixture labels with Ansible's own YAML loader and Templar.

The rule (CLAUDE/QA.md, regex-backref-escape) is proven by its Go tests against
tests/fixtures/ansible-regex-backref; this script proves the fixtures themselves tell the
truth, by rendering every value the way Ansible does, on whatever ansible-core runs it.

Usage, from the repository root, with the Python that has ansible-core:
    python3 -I CLAUDE/Plan/00191-go-cli-tooling/regex-backref-proof/fixture_labels.py \
        tests/fixtures/ansible-regex-backref/flagged.yml tests/fixtures/ansible-regex-backref/clean.yml

A conditional (when, changed_when, failed_when, until, that, loop_control.break_when) goes
through Templar.evaluate_conditional, the debug action's var through evaluate_expression, and
anything else through Templar.template. With s = "ab" a correct value gives "b" (True for a
conditional, False for failed_when). Every flagged value must not, every clean one must, and
the flagged values rendered must number the file's EXPECT-FINDING marks.
Exit 0 when every label holds, 1 otherwise.
"""

import sys
from pathlib import Path

from ansible.errors import AnsibleError
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

CONDITIONAL = {"when": True, "changed_when": True, "until": True, "that": True, "failed_when": False}
VERBATIM = {"shell_text", "unsafe", "raw"}


def values(task):
    """Yield (key, text, wanted) for every value of one fixture task."""
    for key, wanted in CONDITIONAL.items():
        if key in task:
            items = task[key] if isinstance(task[key], list) else [task[key]]
            for item in items:
                yield key, item, wanted
    if "break_when" in (task.get("loop_control") or {}):
        yield "break_when", task["loop_control"]["break_when"], True
    debug = task.get("ansible.builtin.debug") or {}
    if "var" in debug:
        yield "var", debug["var"], "b"
    for args in (task.get("ansible.builtin.set_fact") or {}, task.get("ansible.builtin.assert") or {}):
        for key, item in args.items():
            if key == "that":
                for cond in item:
                    yield "that", cond, True
            else:
                yield key, item, "b"


def outcome(templar, key, text):
    """Render one value as Ansible would; a failure of the filter is the outcome, as text."""
    try:
        if key in CONDITIONAL or key == "break_when":
            return templar.evaluate_conditional(trust_as_template(text))
        if key == "var":
            return templar.evaluate_expression(trust_as_template(text))
        if key == "unsafe":  # the loader leaves !unsafe untrusted, and Ansible never templates it
            return templar.template(text)
        return templar.template(trust_as_template(text))
    except AnsibleError as exc:  # the filter's failure is the result under test
        return "ERROR " + str(exc).splitlines()[0]


def label_holds(key, got, wanted, expect_correct):
    """Whether one rendered value matches its fixture's label."""
    if key in VERBATIM:
        return not str(got).startswith("ERROR")
    if key == "plain":  # a plain scalar cannot open with {, so it carries an x prefix
        return (got == "xb") == expect_correct
    return (got == wanted) == expect_correct


def main(flagged, clean):
    """Check both fixtures; return the exit status."""
    loader = DataLoader()
    templar = Templar(loader=loader, variables={"s": "ab"})
    bad = 0
    for path, expect_correct in ((flagged, False), (clean, True)):
        print(f"== {path} (each must be {'correct' if expect_correct else 'wrong'})")
        rendered = 0
        for task in loader.load_from_file(path):
            for key, text, wanted in values(task):
                got = outcome(templar, key, text)
                ok = label_holds(key, got, wanted, expect_correct)
                rendered += 1
                bad += not ok
                print(f"  {'ok  ' if ok else 'FAIL'} {key:13} {got!r:60.60} <- {text!r}")
        if not expect_correct:
            marks = sum(line.rstrip().endswith("# EXPECT-FINDING") for line in Path(path).read_text().splitlines())
            if marks != rendered:
                print(f"  FAIL {rendered} flagged value(s) rendered, {marks} EXPECT-FINDING mark(s)")
                bad += 1
    print("labels hold" if bad == 0 else f"{bad} label(s) do not hold", file=sys.stderr)
    return 1 if bad else 0


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: fixture_labels.py <flagged.yml> <clean.yml>")
    sys.exit(main(sys.argv[1], sys.argv[2]))
