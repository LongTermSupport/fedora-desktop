# Defence Before Fix report: regex-backref-escape

Branch `fix/regex-group-block-scalar`, made from F44 at `f2c59399a`. The defence was committed
red in `87bc54bbb` and the fix in `5b78e94fb`.

## Specification followed

- Method specification 1.1.0, fetched by the dbf skill's refresh script.
- Detector specification 1.0.0 and toolchain specification 0.2.0, both fetched.

## Toolchain

The project has no `defence-before-fix` manifest declaration. `.claude/settings.json` only
enables the plugin, and I changed nothing there. The project's detectors are its own Go QA
gates under `src-go/qa`, plus semgrep rules in `.semgrep/`. Neither is a reference toolchain
in the register, and neither declares a spec version.

- **Listing.** No listing command exists. The defences are enumerated as the gate rows in
  `scripts/qa-gates.tsv` and the rule sections in `CLAUDE/QA.md`.
- **Resolution.** An identifier resolves to the `CLAUDE/QA.md` heading of the same name.
  Here that is `CLAUDE/QA.md#regex-backref-escape`, and the docs gate checks that the anchor
  exists.
- **Single-rule harness.** `go test ./ansible -run RegexBackref` (from `src-go/qa`) runs the
  rule against its fixtures. `scripts/qa-ansible.bash [--root DIR]` runs the gate over a tree.
- **Calibration.** The project records no calibration. I assumed blocking, with no recorded
  exceptions.

## Defect and class

- **Originating instance.** `playbooks/imports/play-claude-yolo.yml:585` (line 586 after the
  fix's added comment line), in the task
  "Read CCY_VERSION for the Go Front's Build". It had `regex_search(..., '\\1', multiline=True)`
  inside a `>-` scalar's `{{ }}`. On ansible-core 2.21.5 it failed with
  `'NoneType' object has no attribute 'group'`.
- **Class.** A backreference (`\N`, `\g<name>`) in a string argument of `regex_search`,
  `regex_replace` or `regex_findall` whose backslash count is wrong for its YAML scalar style
  and its Jinja evaluation context.
- **Hazard.** A `regex_search` group argument must reach the filter as exactly one backslash
  and the group:
  - Two backslashes fail the task with "'NoneType' object has no attribute 'group'".
  - A control character fails it with "Unknown argument".
  - In a pattern or a replacement, an even count is a literal backslash where a backreference
    was meant. `regex_replace` then writes `\1` and nothing fails.

**The mechanism**, which corrects the host's account. I proved it with Ansible's DataLoader and
Templar on 2.19.14, and read the same code in the 2.21.5 wheel (`_jinja_bits.AnsibleLexer`,
`_engine.evaluate_conditional`).

- **YAML** unescapes double-quoted scalars only.
- **Inside `{{ }}`** in task data, `AnsibleLexer` doubles every backslash of a string
  constant, which cancels Jinja's unescape. The filter therefore receives the YAML-decoded text
  as written.
- **Bare conditionals** are evaluated with `escape_backslashes=False`, so Jinja does unescape
  them. The same holds for `{% %}` statements and `.j2` templates.

So line 692 (the `claude_channel_sync` `changed_when`, `'\\1'` in a `>-` scalar) is correct.
On real input it gives True and False, and `'\1'` there would fail with "Unknown argument".
The probes are `untracked/scratch/regex_group_context_probe.py`,
`regex_group_play_probe.py` and `regex_group_fixture_probe.py`.

## Rule

**Detector.** A seventh check in the project's qa-ansible gate, in
`src-go/qa/ansible/regexbackref.go`. It parses each file with `go.yaml.in/yaml/v3`, so the
scalar style is already decoded.

**Identifier.** `regex-backref-escape`.

**Message.** One line, then one advice line:

```
ERROR (regex-backref-escape): <path>:<line>: <filter> [group ]argument <literal> reaches the filter as <what>; in a <style> scalar's <context> write <fix>
    Why and how to fix: CLAUDE/QA.md#regex-backref-escape
```

**Documentation.** `CLAUDE/QA.md#regex-backref-escape` explains the rule, with a table of the
correct spellings. The expectation is E16a in
`CLAUDE/Plan/00192-.../SPEC-qa-ansible.md`.

**Width.** The rule covers:

- every string scalar in every YAML document under `playbooks/ tasks/ vars/ environment/ roles/`;
- bare expressions, which Jinja unescapes because `AnsibleLexer` doubles backslashes only
  between `{{` and `}}`. These are the conditional keys (`when`, `changed_when`,
  `failed_when`, `until`, `that`, `loop_control.break_when`, as a value or a list item) and the
  debug action's `var`;
- `{{ }}` and `{% %}` bodies;
- filter calls in any namespace, the by-name form (`map('regex_replace', ...)`), and
  concatenated arguments;
- patterns as well as group arguments and replacements.

**What it deliberately excludes**, none of which carries the hazard:

- `{# #}` comments, raw blocks and `!unsafe` scalars, because none of them is templated;
- text outside Jinja delimiters;
- non-regex filters;
- a replacement with an odd count, which is an escaped backslash followed by the group.

A YAML file the parser rejects is itself a finding, so its backreferences are never skipped
silently. go-yaml and Ansible's loader are different parsers, so this does not show that Ansible
would reject the file too.

**Next wider rule considered, not built.** The same judgement over `.j2` templates under
`files/` and `templates/`, where Jinja unescapes. It is not built because no `.j2` uses a
regex filter today, which both searches established. The hazard can still arise there, so
leaving it unbuilt is the owner's decision (section 4). I also considered judging pattern
escapes such as `\d` and `\.`. I did not build that, because Jinja keeps unknown escapes in
unescaping contexts, so there is no single wrong count to detect. The independent search marked three pattern
escapes "unsure": `play-lxc-install-config.yml:455` and `play-self-update.yml:85, 87`. Each
has a single backslash (`\+`, `\s`) in a bare conditional. Jinja keeps an unknown escape
unchanged there, so each regex receives what it was written to receive, and none is a defect.
They are the cases that rule would have to rule on, and they show it would flag correct code.

## Proof

**Fixtures.** `tests/fixtures/ansible-regex-backref/flagged.yml` has 17 cases, each marked
`# EXPECT-FINDING`. They cover every scalar style, every context, both named and numbered
groups, the replacement, the map form, statements, conditionals, `break_when` and debug `var`.
`clean.yml` has 21 correct or untemplated cases.

**Fixture labels proven by real Ansible.**
`CLAUDE/Plan/00191-go-cli-tooling/regex-backref-proof/fixture_labels.py` is tracked, which
answers conformance finding 1. It renders every case through Templar and prints
`labels hold`, exit 0. Every flagged case errors or gives the wrong value, every clean case
gives `b`, True or False, and the number of flagged values matches the number of marks.

**Go tests.** `go test ./ansible` from `src-go/qa` passes with 96.8% coverage. The tests
include:

- the fixture-line equality test;
- message and fix cases for each style and context;
- the cases a naive reading would miss: a quoted `}}`, a concatenation, a pattern
  backreference, and a raw block;
- the cases that must stay silent;
- parse-error reporting.

**Red on the repository.** `./scripts/qa-ansible.bash` exited 1 with:

```
  ERROR (regex-backref-escape): playbooks/imports/play-claude-yolo.yml:583: regex_search group argument '\\1' reaches the filter as 2 backslashes, not 1; in a block scalar's {{ }} write '\1'
```

Line 583 is the scalar's first line, the `ccy_front_version: >-` key. The red state survives
on its own in commit `87bc54bbb`. `qa-changed` was also red there, on `ansible-patterns` only.

## Independent search

`defence-before-fix:independent-searcher` was dispatched before the rule existed. It was kept
out of `src-go/qa/ansible/` and `tests/fixtures/`. It searched every first-party tree,
including `files/`, `.j2` templates and shell blocks, and read each hit in context. Its report
is `subagent-reports/261022-regex-group-independent-search.md`.

It found 7 backreference sites and 1 defect (line 585). It judged line 692 correct, along with
`play-basic-configs.yml:604`, `play-vm-test-lab.yml:357, 364, 365` and
`play-self-update.yml:602`. Nothing outside `playbooks/` uses a regex filter, and no
replacement and no `\g<` form exists.

**What each technique could reach that the other could not:**

- **Text search** covered trees the rule does not read: `.j2` files, `files/` and shell text.
  It found no instance there.
- **Reading** was needed to tell 585 from 692, since the two have the same text shape and
  differ only in context.
- **The rule** judges every scalar mechanically, including `vars/`, `environment/` and
  `roles/`, and the map and concatenated forms.

The search found nothing the rule missed. After the conformance review, my own reading of
Ansible's source widened the rule in commit 3 of this branch. I added
`loop_control.break_when`, which `_resolve_conditional` evaluates as a conditional, and the
debug action's `var`, a bare expression. Real Ansible proved the `var` direction: one
backslash there fails, and two give `b`. That overturned my first assumption, which was the
opposite. The widened rule finds no further instance in the tree.

## Sweep and count

- **Scope**, recorded as this remediation's decision because the project had none: the YAML
  that git would publish under `playbooks/ tasks/ vars/ environment/ roles/`, with
  `roles/vendor` excluded. That is the qa-ansible scope, and every file in it parsed.
- **Outside the rule's reach.** `.j2` files and other languages were swept by the independent
  search only, and it found no regex filter in them.
- **Count.** 1 instance in 7 backreference sites.
- **Narrowing.** Only the exclusions under "Rule", each of which is untemplated text, a
  non-regex call, or a correct odd count.

## Fixes

There was one instance: `play-claude-yolo.yml:585`, where `'\\1'` became `'\1'`. I examined it
individually, and a comment above the task gives the reason.

I also examined the other 6 sites individually and left them correct. In particular, I did not
change line 692; changing it would introduce the defect. No suppression was used, and the
hazard does not remain anywhere.

## Conformance review

`subagent-reports/261022-regex-group-conformance-review.md` reproduced the red and green runs
and found the work conformant. It raised three findings:

1. **(medium)** The reproducing probe was untracked. The proofs are now tracked under
   `regex-backref-proof/`.
2. **(low)** The unsure pattern escapes were not named. They are now named under "Rule".
3. **(low)** The owner decision must reach the hand-off. It is in the worker's final message
   as well as in PLAN.md Task 4.5.

The reviewer had to run as a general-purpose agent, read-only in this checkout. The plugin
agent's own worktree could not be created: the hooks daemon refused to seed a worktree nested
in this one.

## Adversarial review

Round 1 (`subagent-reports/261022-regex-group-review-round1.md`) returned FIX-FIRST.

- **F1, fixed.** The rule treated an odd run before `g<`, `8` or `9` in an unescaping context
  as a control character. Python's `unicode-escape` keeps unknown escapes, so `'\g<v>'` in a
  `when:` is correct, and `'\\\g<v>'` arrives as 2 backslashes. Fixture cases and message tests
  now cover both, real Ansible confirms both labels, and E16a and QA.md are corrected.
- **F6 and F7, fixed.** These were wording: the claim that Ansible cannot load the file, the
  `{% %}` mechanism in the journal (a correction entry was added), and the line number.
- **F2 to F5, recorded as known limits.** These are left unfixed by the coordinator's decision,
  and are recorded in PLAN.md Task 4.5, E16a and QA.md:
  - F2: the free-form and `args:` forms of debug `var`;
  - F3: the tests `match`, `search` and `regex`;
  - F4: YAML aliases and templated keys;
  - F5: an even count before a digit in a pattern is always flagged, and QA.md gives the
    workaround.
- **F8, not this branch's.** `scripts/test-wsi-stop-grace.bash` failed intermittently under
  qa-changed: speech-to-text unit tests, with a different set failing on each run. Nothing in
  this diff reaches them. The coordinator is handling that flaky test separately.

## Decisions referred to the owner

There is one: whether to extend the rule to `.j2` templates, which is the next wider rule.

- **Count.** 0 instances fixed there, and 0 known to remain.
- **What it would take.** Walking the published `.j2` files under `files/` and `templates/`,
  treating every region as unescaping, and adding a `qa-routing.tsv` row so a `.j2` change runs
  qa-ansible.
- **The rule meanwhile.** It merges at full width for YAML. It is not weakened, and nothing
  known is left unfixed.

## Permanence

qa-ansible is the `ansible` stage in `scripts/qa-gates.tsv`, so it is blocking in qa-all and
CI. qa-changed runs it whole for any playbook, task or vars change, or a change to its Go
(`ansible-patterns`). A `tests/fixtures/ansible-regex-backref/*` change routes to the go check.

At `5b78e94fb`, the green runs through the project's entry points were:

- `./scripts/qa-changed.bash`: passed.
- `./scripts/qa-ansible.bash`: `✓ ansible: ... regex backreferences escaped right ...`.
- `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml`: rc 0.

qa-all was not run, because the coordinator runs it.

## Toolchain and detector gaps

- The project has no DBF manifest declaration and no listing command that derives defences from
  live configuration. The gate table plus QA.md stand in for the listing.
- Identifier resolution is a documentation anchor, not a resolver command.
- The project forbids inline suppression in general (`R-QA-SUPPRESSION`). qa-ansible offers no
  suppression route for this rule.

## The original defect

The conventional fix is `'\1'` at line 585.

**The reproducing test**, now passing:

- `CLAUDE/Plan/00191-go-cli-tooling/regex-backref-proof/play_task.py` is tracked. It loads the
  real play with Ansible's DataLoader and renders the task through Templar, together with the
  `claude_channel_sync` `changed_when`.
- On the play as of `87bc54bbb` it exits 1, raising the host's exact error
  (`'NoneType' object has no attribute 'group'`).
- On the branch it exits 0, with `'3.99.14.11'`, True and False.
- The kept regression tests are the `folded` case of the flagged fixture and
  `TestRegexBackrefNamesTheReceivedCountAndTheFix` ("the original defect").
- The qa-ansible gate now holds the tree.
