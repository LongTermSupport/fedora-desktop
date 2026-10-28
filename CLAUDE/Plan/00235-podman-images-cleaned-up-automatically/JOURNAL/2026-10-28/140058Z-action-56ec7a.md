## 14:00 · action · —   — Task 4.10: regex-class-escape, red before 0d1a61321, green after

Task 4.10, Defence Before Fix for the doubled-backslash regex escape of 0d1a61321.

The independent searcher ran first, from the class alone, and found no further instance: every
other doubled backslash in the Ansible tree is a backreference (regex-backref-escape's), sits in
a double-quoted scalar, is bash, or is a conditional's hex escape that Jinja decodes
(play-speech-to-text.yml, correct).

The rule, regex-class-escape, is in src-go/qa/ansible beside regex-backref-escape and counts
backslashes the same way (Plan 00191's Templar-proven model: Ansible's lexer keeps backslashes
only between {{ }}; Jinja unescapes bare conditionals, debug var and {% %}, keeps unknown
escapes, and turns \\b into a backspace). It reads the pattern of regex_search, regex_replace,
regex_findall and of the tests match, search and regex, by name too.

Red on the tree at 0d1a61321^ (git archive into a scratch repo, qa-ansible.bash --root): 2
findings, the subuid regex (podman-store-ownership-probe.yml:27) and the held-lock regex
(podman-store-repair.yml:130). Green at 0d1a61321 and on the live tree (0).

The brief's third line, the store-path guard at podman-store-repair.yml:52, is an assert that:
a bare conditional, where '\\s' reached the regex as \\s. Under the proven model it was correct
before its change and is correct after, so the rule is silent on it and the clean fixture holds
both forms. regex-escape-proof/render_three.py renders all three before and after with the
Templar, read-only, for the host to confirm; if the guard renders wrong before the fix, the
conditional model of both regex rules needs revisiting.

The conformance reviewer agent could not be dispatched: its worktree could not be created (the
daemon's worktree seeding refused three entries outside the repository root).
