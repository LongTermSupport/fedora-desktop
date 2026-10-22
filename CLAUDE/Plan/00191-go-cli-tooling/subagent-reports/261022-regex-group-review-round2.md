# Adversarial review, round 2: fix/regex-group-block-scalar

Scope: commit af102cb9d (the response to round 1) and the whole branch,
`git diff F44...fix/regex-group-block-scalar`. I worked read-only. My probes are under
`untracked/scratch/rb2/` (untracked).

## Verdict: MERGE

F1 is fixed, and the fix is correct. I checked it against the real Templar in every
evaluation context. F6 and F7 are fixed. F2 to F5 are recorded as known limits, and nothing
claims otherwise. The fix adds no defect. The findings below are low or informational, none
blocks the merge, and they can be folded into a later docs pass.

## What I verified

### F1: a matrix run against the real Templar

`untracked/scratch/rb2/matrix.py` runs on ansible-core 2.19.14 and Python 3.11.2. It generated
352 cases:

- **Contexts:** `when:`, debug `var`, a `{% set %}` statement and `{{ }}` in a `>-` set_fact.
- **Runs:** k = 1 to 4 backslashes, before each of `0` to `9` and before `g<v>`.
- **Arguments:** a `regex_search` group argument and a `regex_replace` replacement, for each
  of the above.

For each case the script did three things:

- It measured with the Templar the exact string the filter receives.
- It cross-checked group arguments against the real `regex_search` result. The pattern has
  9 groups and a named group `v`.
- It ran the rule (the branch's `qa-ansible` binary) over the generated file. It compared
  whether the rule flagged the case, and the count or "control character" in the message,
  with that truth.

Result: **352 cases, 0 mismatches**. Where the real filter's outcome was compared, it agreed
with the measured string in every case.

- **In the unescaping contexts (conditional, `{% %}`, debug `var`):**

  - k odd before `0` to `7` gives a control character;
  - k odd before `8`, `9` or `g<` gives (k+1)/2 backslashes;
  - k even gives k/2.

  The rule reports each of these exactly. For example, `'\\\g<v>'` in a `when:` is reported as
  "2 backslashes, not 1", and `'\g<v>'`, `'\8'` and `'\9'` in a `when:` are clean.

- **In `{{ }}`:** the received count is k in all 132 cases, the same as before the fix. The
  change is inside `if ctx != ctxTemplate` (`regexbackref.go:329`).

- **Index safety:** `next := arg.body[loc[0]+run]` (`regexbackref.go:332`) cannot go out of
  range, because `backrefRun` (`:50`) always matches a character after the run.

- **Suggested spellings:** every suggested fix in the unescaping contexts is the 2-backslash
  form, and that form receives 1.

### Gates and proofs

- `go test -count=1 ./ansible` passed with 96.8% coverage, and `go vet` is clean.
- `./scripts/qa-ansible.bash` exits 0 on the tree.
- `regex-backref-proof/fixture_labels.py` reports "labels hold" on the updated fixtures. The new
  clean cases are `'\g<v>'` and `'\\g<v>'` in a `when:`, which evaluate to True. The new
  flagged case is `'\\\g<v>'`, where the filter fails.

### F6 and F7

- **F6, fixed** in all three places: `regexbackref.go:96-98`, `CLAUDE/QA.md:918-919` and the
  DBF report "Rule" section.
- **F7, fixed:**
  - a journal correction entry for the `{% %}` mechanism (10:50), whose content I checked is
    correct;
  - the DBF report gives the 585 to 586 line note.

### F2 to F5

- **PLAN.md Task 4.5** (`PLAN.md:630-637`) lists all four.
- **E16a** (`SPEC-qa-ansible.md:115-117`) lists F2 to F4 as "Not read".
- **QA.md** (`:919-920`) lists F2 to F4, and gives the F5 workaround at `:950-951`.
- **No overclaim:** no file claims full coverage or "no false positives".

### The rest of the branch

- **Play fix:** `play-claude-yolo.yml:586` (`'\1'` in a `>-` set_fact `{{ }}`) is unchanged
  since round 1 and still correct.
- **Version check:** the result is asserted against `^[0-9][0-9.]*$` at `:595`, so a missed
  match fails fast.
- **Deploy queue:** the play is still queued in `meta-deploy.list:124`.

## Findings

### R2-1 (low, docs): F5 is not named as a known limit in E16a

Location: `SPEC-qa-ansible.md:115`. The "Not read (known limits)" sentence lists F2 to F4
only. The spec does imply F5: "any other argument an odd number" means an even count is
always flagged. But the brief asked that F5 be recorded there.

Fix: add one clause, for example "an even count before a digit in a pattern (a literal
backslash) is always flagged; write it `[\\]` or `\x5c`."

### R2-2 (info, docs): QA.md says "templated mapping keys", but the rule reads no mapping key

Location: `CLAUDE/QA.md:920`. The rule reads no mapping key at all, templated or not (round 1
F4). E16a says "mapping keys", which is accurate. Suggested fix: drop "templated".

### R2-3 (info): unknown escapes rely on Python's DeprecationWarning behaviour

`codecs.decode(..., 'unicode_escape')` keeps `\g`, `\8` and `\9`, and emits a
DeprecationWarning for each. Python could one day reject these escapes. If it did, the
1-backslash spelling `'\g<v>'` in a conditional, which the rule accepts, would stop working.
The 2-backslash spelling the rule suggests works either way.

Nothing needs changing now. This could be a sentence in QA.md recommending `'\\g<v>'` in
unescaping contexts.

### R2-4 (info): the journal's 10:14 entry still cites `play-claude-yolo.yml:585`

Location: `JOURNAL/00191-Journal-26-10-22.md:51`. The journal is append-only, so this is
correct. The DBF report carries the 586 note, but the 10:50 correction entry does not mention
the line number.
