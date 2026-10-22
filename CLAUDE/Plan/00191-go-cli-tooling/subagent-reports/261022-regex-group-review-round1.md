# Adversarial review, round 1: fix/regex-group-block-scalar

Scope: `git diff F44...fix/regex-group-block-scalar` (87bc54bbb, 5b78e94fb, 4977c0fc5), in the
worktree for this review. I worked read-only. My probes are under `untracked/scratch/rbreview/`
(untracked).

## Verdict: FIX-FIRST (one defect in the rule, small and contained)

The play fix is right, and the root cause is right. The gate wiring is right. One claim is
built into the rule's code, its spec (E16a) and its docs, and it is false: in an unescaping
context, `\g<name>` with an odd backslash count becomes "a control character". Because of it,
the rule flags correct code and prints a wrong diagnosis. Fix it, add a fixture for it, and
then merge. Everything else below is low severity and can be recorded as known limits.

## What I verified

- **Root cause (correct).** I read the ansible-core 2.19.14 source and checked that the 2.21.5
  wheel is identical in this respect. `AnsibleLexer.tokeniter`
  (`_internal/_templating/_jinja_bits.py`) doubles the backslashes of a `TOKEN_STRING` only
  while `in_variable` is set, that is, between `TOKEN_VARIABLE_BEGIN` and `TOKEN_VARIABLE_END`.
  It does this only when `_TemplateCompileContext.escape_backslashes` is set.

  - Conditionals pass `escape_backslashes=False` (`_engine.py`
    `_normalize_and_evaluate_conditional`).
  - `debug var` calls `evaluate_expression` with the default `True`. But
    `compile_expression` lexes in the `variable` state and emits no `VARIABLE_BEGIN`, so no
    doubling happens there either. The rule's reasoning ("a bare expression has no {{ }}")
    is the right mechanism.
  - `regex_search` (`plugins/filter/core.py`) does `re.match(r'\\(\d+)', arg).group(1)`. Two
    backslashes give `None.group`, which is the host's exact error.

- **Reproduction.** `regex-backref-proof/play_task.py` on the F44 play gives the host's error.
  On the branch it gives `'3.99.14.11'` and the `changed_when` gives True and False. I ran
  `fixture_labels.py` and every label holds.

- **Gate.** The following pass on the branch: `./scripts/qa-ansible.bash` (exit 0, the new
  clause in the tick line), `go test ./ansible` (96.8% coverage) and
  `scripts/test-qa-ansible-failfast.bash`. `./scripts/qa-changed.bash --base F44` passed
  every check, including go-qa in diff mode, docs, ansible-syntax and the 18 other suites, but
  exited 1 on one suite, `test-wsi-stop-grace.bash`. That suite ran only because the branch
  changes `src-go/`. Its failures are in the speech-to-text Python tests (`wsi-stream unit tests`):

  - first run: `SafetyStopTest.test_keepalives_keep_it_recording` and
    `FileJobTest.test_a_running_job_is_marked_for_the_panel_and_unmarked_when_it_ends`;
  - re-run alone: only the second of those.

  The branch touches nothing those tests read. A failure set that changes between two runs
  points to timing on a loaded machine, not to this diff. I could not check out F44 to
  confirm this, because I worked read-only. The coordinator should note it rather than call
  qa-changed green (finding F8).

- **Routing.** `qa-changed --plan --files tests/fixtures/ansible-regex-backref/clean.yml`
  gives `check go`. `runGoGate` runs the full go-qa for a path outside `src-go/`, so the
  fixture tests do run.

- **Plumbing.** The existing `stage ansible` row in `qa-gates.tsv` carries the rule. The row
  parses no summary text, so the reworded tick and cross lines break nothing. No golden test
  or other parser reads those lines: they appear only in `report.go`, its tests, the spec and
  the script comment.

- **Fix.** The fix leaves the `changed_when` at `play-claude-yolo.yml:693` unchanged. It is a
  bare conditional in a `>-` scalar, so `'\\1'` is correct there (proven by Templar).

- **Deploy queue.** `playbooks/imports/play-claude-yolo.yml` is already queued in
  `meta-deploy.list` and has not run yet, so no queue edit was owed.

- **Go code.** There is no `_ =`, no nolint and no discarded error. The code is idiomatic for
  the package.

## Findings

### F1 (medium, must fix): `\g<name>` in an unescaping context is misjudged

Locations: `src-go/qa/ansible/regexbackref.go:26`, `:327-336` (`judge`), the spec text
`CLAUDE/Plan/00192-.../SPEC-qa-ansible.md` E16a ("a control character (`k` odd)"), and
`CLAUDE/QA.md` "Why it exists" (`'\1'` becomes 0x01, stated for every backreference).

Jinja decodes a string constant with Python's `unicode-escape`, which **keeps unknown
escapes**. `\g` is one (as are `\8` and `\9`). Measured with jinja2:

- `'\g<v>'` gives `\g<v>` (1 backslash);
- `'\\\g<v>'` gives `\\g<v>` (2 backslashes).

So for `g<`, an odd k reaches the filter as (k+1)/2 backslashes, not as a control character.

- **Proof.** Templar `evaluate_conditional("(s | regex_search('a(?P<v>b)', '\g<v>') | first) == 'b'")`
  gives **True**.
- **The rule's result.** On that line it reports
  `regex_search group argument '\g<v>' reaches the filter as a control character, not a backslash; in a plain scalar's conditional write '\\g<v>'`.
- **The cost.** Correct code is flagged, with a false diagnosis. The suggested spelling also
  works, so the cost is a forced, needless edit plus a false teaching in the docs. k=3 is
  diagnosed wrongly too: the rule says "control character", but the filter really receives
  2 backslashes, which is still wrong.
- **The fix.**
  - In `judge`, when the run ends in `g<` (or `8`/`9`) in an unescaping context, compute
    `received = run/2 + run%2` with no control flag.
  - Add a clean-fixture case (`'\g<v>'` in a `when:`) and a flagged one (`'\\\g<v>'`).
  - Correct E16a and the QA.md paragraph.

No tracked file has this today, so this is not a live false positive. It is a false statement
in the rule and its spec, so it should be fixed before the rule becomes the reference.

### F2 (low): free-form and `args:` debug `var` are not read as bare expressions

Location: `regexbackref.go:135`. `ctxBare` applies only to the key `var` directly under a
`debug` mapping. The rule is silent on these forms, all of which Ansible evaluates through
`evaluate_expression`:

- `ansible.builtin.debug: var=s|regex_search('a(b)','\1')` (verified silent);
- `debug:` with an `args: {var: ...}` mapping;
- `action: debug var=...`.

A one-backslash group there fails with "Unknown argument". The repo uses none of these forms.
Either record them in E16a as unread, or treat a `var=` key/value in a debug scalar as bare.

### F3 (low): tests `match`/`search`/`regex` are not read

Location: `regexbackref.go:36`. A backreference in the pattern of a test, such as
`when: s is search('(b)\1')` or `select('match', '(b)\\\\1')`, has the same hazard. With one
backslash in a conditional, Templar gives False silently, because `\1` becomes 0x01. The rule
reads only the three filters. There is no instance in the repo. Name this in QA.md as not
read, or add the three tests with `pattern` 0. The brief asked about this case specifically.

### F4 (low): YAML aliases and templated keys

Location: `regexbackref.go:148`. An `AliasNode` is skipped. The anchored scalar is judged only
in its definition's context. An anchor defined under `vars:` and aliased into `when:` is
evaluated as a conditional but judged as `{{ }}` text, where it has no regions, so the rule is
silent. Mapping keys are never visited. Both are exotic and absent from the repo. Note them as
unread in E16a.

### F5 (low): an even backslash count before a digit in a pattern is always flagged

Location: `regexbackref.go:330`. Example: `regex_replace('C:\\\\1', 'D')` in `{{ }}`, meant to
match a literal `C:\\1`. The flag is intended, and QA.md documents the workaround (`[\\]` or
`\x5c`), so this is acceptable. I list it only so the docs claim "no false positives" is not
made anywhere. I did not find that claim made.

### F6 (low, docs accuracy): "Ansible cannot load it either"

Locations: `regexbackref.go:95`, `CLAUDE/QA.md` ("because Ansible cannot load it either"), and
the DBF report "Rule" section. go-yaml v3 and Ansible's loader (PyYAML with Ansible's
constructors) are different YAML parsers. Neither one rejecting a file proves the other
does. I checked one case: a duplicate key is accepted by both, because a `yaml.Node` decode
does not check duplicates, so it is not a false positive. The sentence states something as
fact that has not been shown. Soften it to "a file the YAML library cannot parse is reported,
so its backreferences are not silently skipped".

### F7 (informational): plan and journal claims

- **True and reproduced:** "proved with Ansible's own Templar", "7 backreference sites,
  1 instance", "introduced in 0390d976", "`'\1'` in the changed_when would fail with
  'Unknown argument'", and that no `.j2` uses a regex filter.
- **Slightly loose wording:**
  - The journal says `{% %}` statements are "evaluated with `escape_backslashes=False`".
    They are not: they are templated with it True, and the lexer simply does not escape
    outside `{{ }}`. The conclusion is still right.
  - The DBF report says the original line is "line 585". It is line 586 after the added
    comment line, and the PLAN does not cite a line. Harmless.
- **Scratch probes.** The journal's scratch probes `untracked/scratch/regex_group_*.py` exist
  in the main checkout, not in this worktree. They are untracked, as described.

### F8 (low, not this diff): test-wsi-stop-grace.bash fails intermittently under qa-changed

Two speech-to-text unit tests fail under `qa-changed --base F44`, and a different set fails
on each run (see "What I verified"). Nothing in this diff reaches them. A push gate cannot
call this suite green. Run it on F44 to settle whether the failure was there before this
branch, and if so, treat the flaky tests as their own defect.

## Not defects (checked)

- **Contexts the rule handles correctly** (verified with my own scalars): `{{- -}}`
  whitespace control, `ansible.builtin.` and short `debug` names, the keyword form of
  `regex_replace` (`replacement='\\1'`), `regex_findall`, an all-template conditional
  (`when: "{{ ... }}"`, which Ansible templates with escaping, so it is judged as `{{ }}`),
  `!unsafe`, `{% raw %}`, multi-document files and a set_fact key named `that` holding a
  template.
- **The tree's own sites:** `play-vm-test-lab.yml:357,364,365` (double-quoted `'\\1'`),
  `play-basic-configs.yml:604` (double-quoted), `play-self-update.yml:602` (`>-` with `'\1'`)
  and `play-claude-yolo.yml:693` (conditional with `'\\1'`). All are correct, and the rule is
  silent on all of them.
- **Not covered by the rule:** a backreference assembled across constants (`'\\' ~ '1'`) or
  held in a variable (`grp: '\\1'` in a single-quoted var, used as the group). Templar fails
  both. Static analysis cannot reasonably see either, and neither is in the repo.
