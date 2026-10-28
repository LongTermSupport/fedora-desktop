"""Render the three regexes commit 0d1a61321 changed, before and after, with Ansible's Templar.

The rule regex-class-escape (CLAUDE/QA.md) is red on two of the three lines as they were before
that commit and silent on the third: the store-path guard is an assert `that`, a bare
conditional, which Jinja unescapes, so its '\\s' reached the regex as \\s and matched. This
script settles that on real Ansible: it renders each expression as play-podman.yml's tasks do
(the two set_fact/vars values through Templar.template, the assert through
evaluate_conditional) over sample data, and prints what each gives.

Usage, from the repository root, with the Python that has ansible-core (read-only; no play runs):
    python3 -I CLAUDE/Plan/00235-podman-images-cleaned-up-automatically/regex-escape-proof/render_three.py
Exit 0 when every rendering is what the rule predicts (the two flagged lines wrong before their
fix, all three right after it, the guard right before it too), 1 otherwise.
"""

import base64
import sys

from ansible.errors import AnsibleError
from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

SUBUID = "(?m)^(?:' ~ (user_login | regex_escape) ~ '|' ~ uid ~ '):"
CASES = [
    # name, kind, expression, wanted when right, whether the rule predicts it right
    ("ownership probe before", "template",
     "{{ subuid | b64decode | regex_findall('" + SUBUID + r"(\\d+):(\\d+)\\s*$') }}", [["524288", "65536"]], False),
    ("ownership probe after", "template",
     "{{ subuid | b64decode | regex_findall('" + SUBUID + r"(\d+):(\d+)\s*$') }}", [["524288", "65536"]], True),
    ("store-path guard before", "conditional",
     r"(conf | b64decode) is not regex('(?m)^\\s*' ~ key ~ '\\s*=')", False, True),
    ("store-path guard after", "conditional",
     r"(conf | b64decode) is not regex('(?m)^\s*' ~ key ~ '\s*=')", False, True),
    ("held-lock inodes before", "template",
     r"{{ locks | b64decode | regex_findall('\\s[0-9a-f]+:[0-9a-f]+:(\\d+)\\s') | map('int') | list }}", [1234], False),
    ("held-lock inodes after", "template",
     r"{{ locks | b64decode | regex_findall('\s[0-9a-f]+:[0-9a-f]+:(\d+)\s') | map('int') | list }}", [1234], True),
]


def b64(text):
    """The text as slurp gives it."""
    return base64.b64encode(text.encode()).decode()


def main():
    """Render every case; return the exit status."""
    templar = Templar(loader=DataLoader(), variables={
        "user_login": "someone", "uid": "1000", "key": "graphroot",
        "subuid": b64("someone:524288:65536\n"),
        "conf": b64("[storage]\n  graphroot = \"/elsewhere\"\n"),
        "locks": b64("1: POSIX  ADVISORY  WRITE 99 fd:01:1234 0 EOF\n"),
    })
    bad = 0
    for name, kind, expr, wanted, predicted_right in CASES:
        try:
            if kind == "conditional":
                got = templar.evaluate_conditional(trust_as_template(expr))
            else:
                got = templar.template(trust_as_template(expr))
        except AnsibleError as exc:  # a failing render is a result, printed, never hidden
            got = "ERROR " + str(exc).splitlines()[0]
        right = got == wanted
        ok = right == predicted_right
        bad += not ok
        print(f"{'ok  ' if ok else 'FAIL'} {name:24} {'right' if right else 'wrong'}: {got!r}")
    print("the rule's predictions hold" if bad == 0 else f"{bad} prediction(s) do not hold", file=sys.stderr)
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
