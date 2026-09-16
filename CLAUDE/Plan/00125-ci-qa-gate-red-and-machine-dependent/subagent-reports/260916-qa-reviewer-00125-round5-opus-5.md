# QA Review — Plan 00125 round 5, `318fe424` on `F44`

> **Provenance.** Written by the round-5 reviewer itself. The `Write` tool was unavailable
> to that session (tools: `Read`, `Bash`, `SendMessage` only), so this file was created with
> a quoted heredoc after the report-size hook required persistence. Content is the
> reviewer's full long-form report, unabridged.

**Verdict**: FIX-BEFORE-MERGE

Tree clean, `HEAD == origin/F44`, nothing unpushed.

## Blocking

None. Nothing here breaks another user, loses data, leaks private information, or violates a
HARD RULE.

## Should fix

### 1. `helper_test_summary`'s last-match rule is backwards for the capture it actually reads

`scripts/lib/qa-helper-summary.bash:24-26` (the rule's stated justification) and `:29-31`
(the awk).

The two-line defect **is** genuinely fixed — `-o` is gone, the output is one line, and
`"the summary is exactly one line"` is a real control that would have failed before. But the
tie-break is justified by a claim that is false for the capture shape this function consumes.

The comment says unittest's count line "sits immediately above its result line, so where a
test has printed its own `Ran ...` at column 0 the authoritative one is the final one."
unittest writes its summary to **stderr**; a test's `print()` goes to **stdout**, which
Python block-buffers when it is a pipe — and `qa-all.bash:157` captures through a pipe. So
the test's output is flushed at interpreter exit, i.e. *after* unittest's summary:

```
$ python3 -m unittest test_decoy 2>&1      # test_a does print("Ran 3 tests in 9.99s")
.
----------------------------------------------------------------------
Ran 1 test in 0.000s

OK
Ran 3 tests in 9.99s        <-- the decoy lands LAST
```

Against that real capture:

```
summary=[Ran 3 tests]      # for a run that ran 1 test
skipped=[0]
```

The three new cases (`:179`, `:182`, `:187`) all place the decoy **before** unittest's line —
the ordering that cannot occur for a stdout print. `:187`'s comment ("a decoy anchored at
column 0 does not win") asserts precisely the property the function does not have.

No live decoy exists today (`grep -rn "Ran [0-9]* test" tests/ helpers/` finds none outside
this suite's own fixtures), so this is latent, not broken-now. But it is the same class as
round 3's: a defence that reads as proven and is not.

**Fix**: anchor on the count line that immediately precedes the terminal `^OK`/`^FAILED`
rather than on "last", and add a fixture with the decoy *after* the real line — that fixture
is the one that fails today.

Related, same file: `:44` says "unittest does not buffer stdout, so a test printing one of
this repo's own transcript fixtures is enough to do it [win an earlier match]". The buffering
claim is false for a piped capture and the consequence is inverted — a stray `skipped=` lands
*later*, not earlier. `helper_skip_count`'s behaviour is unaffected (grep returns both lines,
bash `=~` takes the first, and the real result line comes first), so this is a wrong reason
attached to a correct outcome — which is what the next reviser will read.

### 2. `25 gates` is attributed to a commit where nothing was masked

`PLAN.md:169-174`, `FINDINGS.md:180-185`, `FINDINGS.md:236-239`.

`29ceee97` is *this plan's own round-2 commit*. `390a9290` (the three machine-reading test
fixes) is its ancestor, and `PLAN.md:212` itself records that its CI run `35041998528`
reached `QA FAILED: 8 errors in 918 files` — reaching the final summary means every hard gate
ran. Nothing was masked at `29ceee97`.

Counted at the commits where the masking actually happened (`git show <c>:scripts/qa-all.bash`,
gate invocations after `helper-tests`):

| commit | date | hard gates behind `helper-tests` |
| --- | --- | --- |
| `9a79dd77` | 2026-09-11 (DisplayLink pair lands) | **5** |
| `b3f6e909` | 2026-09-14 (last Cause-B commit) | **11** |
| `29ceee97` | round 2, nothing masked | 25 |

So `PLAN.md:171-173` — *"So from the moment the DisplayLink pair began failing, CI stopped
executing the last 25 gates entirely"* — is false by a factor of five. `25` is a correct
measurement at `29ceee97` and a wrong description of 2026-09-11 to 09-14.

This is the plan's own lesson ("numbers describing what CI *was* masking are the counts at
the masked commit", `FINDINGS.md:223-224`) applied to the wrong commit. Round 4 confirmed
`25` as arithmetic and did not check what it was attached to; the recount in `cfe8afbe`
re-derived it "the same way" and inherited the attribution.

**Fix**: relabel `29ceee97` as "counted at", and state the masking figures at the commits
where it occurred (5 growing to 11).

### 3. Two of the recounted numbers landed in PLAN.md and not in FINDINGS.md

Both sentences were *edited* in `cfe8afbe` with the number left standing:

- `FINDINGS.md:165` — "stops **28** hard gates short" vs `PLAN.md:163` "**27**". Derived:
  29 hard gates; `nokill-containerwatch` and `deployed-drift` both run before the abort at
  `qa-all.bash:137`, so 27 are behind it. **27 is right.**
- `FINDINGS.md:244` — "extend it to the other **28**" vs `PLAN.md:192-193` "those **29**".
  36 minus 7 is 29. **29 is right.**

### 4. `FINDINGS.md:235` — "`docs` followed by 25 passing gates"

Unattributed to any run, and it cannot be right for the run the plan cites. At `29ceee97`
there were 28 hard gates (27 `printf` stage sites plus `deployed-drift`), all declared after
`docs`, and run `35041998528` reached the final summary — so 28 passed after `docs`. Name the
run and re-derive, or delete the number.

### 5. `PLAN.md:200` still says the reader gate has 16 cases

It has 20. `bash scripts/test-qa-helper-summary.bash` prints `passed: 20`; `qa-all.bash`
prints `helper-summary-readers: passed: 20`. `cfe8afbe` added the four cases *and* edited
`PLAN.md` in the same commit without moving this number. (`JOURNAL/00125-Journal-26-09-16.md:262`
also says 16 — correct, the journal is append-only history.)

### 6. `PLAN.md:76` says `verdicts.py` has 35 tests

`python3 -m unittest tests.helpers.qa_environment.test_verdicts` prints `Ran 40 tests ... OK`.

### 7. The (d) decision table contradicts its own prose

`FINDINGS.md:78` — row *"Can pass a broken link | no — these links were never ours to audit"* —
against `FINDINGS.md:92`: *"(d) means a genuinely broken daemon-authored link would go
unreported here"*. The table answers "no" by reframing the question; the prose answers it
honestly nine lines later. The table is the artefact the owner will decide from. Make the
cell read "yes, but only for links this repo cannot fix", and keep the scope argument in the
row below it.

### 8. If (d) is chosen, the implementation must state its coverage

Conditional on the owner's decision, but it belongs in the plan now, because the exclusion
mechanism is where it will be forgotten.

`.claude/rules/` is **deliberately in scope** (`helpers/docs/link_check.py:243`). Every
existing entry in `_EXCLUDE_PREFIX` (`:214-223`) is a whole tree keyed by **path**. (d) would
be this gate's first **content-keyed, intra-directory** exclusion: whether a file is checked
becomes a property of what the daemon stamps into it. A path prefix is readable off the path
and cannot drift; a marker moves whenever the daemon changes what it generates — and an
under-match is silent, which is AgentNotes' *A partial result read as a complete one* exactly.

So the ownership *rationale* transfers; the *mechanism* does not, and `FINDINGS.md:83-87`'s
"not a new kind of judgement" elides that. Require the gate to print a number:
`docs: 71 files OK (7 of 15 .claude/rules checked, 8 excluded as daemon-generated)`. Also
state whether the exclusion is link-only or drops the file from `in_scope` wholesale (the
latter also removes anchor checking — costless today, since each file has one link and no
anchors).

## Answers to the six questions

**1 — `helper_test_summary`.** Fix real; rule wrong. See finding 1. On the awk under
`set -euo pipefail`: **safe**. `awk '/.../{last=$0} END{print last}'` exits 0 even with no
match and prints an empty line, so the command substitution succeeds and the `[[ =~ ]]` falls
through to `printf 'passed'`. Verified for both the no-match and the empty-capture paths
(`rc=0 out=[passed]` in each). If `awk` were missing the assignment fails and `set -e` aborts
— correct fail-fast.

**2 — The counts, derived independently.** I did not check the arithmetic; I re-derived every
figure from a fresh `qa-all.bash` run using the repo's own parser
(`helpers/qa_environment/verdicts.py`), and separately from `git show <commit>:scripts/qa-all.bash`
for the historical one.

```
total stage names: 37
hard: 29
behind helper-tests: 26
behind deployed-drift: 27
```

- **29 hard gates** — correct (28 `printf` stage sites plus `deployed-drift`, which echoes
  its sub-script's line at `qa-all.bash:142`)
- **27 behind the drift abort** — correct (abort is `exit 1` at `qa-all.bash:137`; `nokill`
  and `deployed-drift` precede it)
- **26 behind helper-tests** — correct
- **36 gates / 37 names** — correct (7 accumulating produce 8 names; 39 symbol lines,
  38 matched plus 1 summary equals full coverage)
- **25 at `29ceee97`** — correct as a measurement (27 `printf` sites plus `deployed-drift`
  gives 28 hard; 25 after `helper-tests`)

All five are right. The remaining problems are findings 2, 3 and 4 — propagation and
attribution, not arithmetic.

**3 — `CLAUDE/QA.md`.** The new sentence is correct and the rest of the file agrees.
`:19-22` (thirty-six / twenty-nine / eight names / 37-for-36) matches the derivation exactly;
`:43` says twenty-nine and the table at `:47-77` has exactly 29 rows. A grep for every other
count claim in the file returns only those two sites, so there is no residual. Cosmetic only:
the new sentence leaves `A missing` orphaned at the end of `:22`.

**4 — The three comments are now true as written.**

- `SYMBOL_BEARING` (`verdicts.py:70-77`) — verified by probe. `2026-09-16 01:02:03 [tick] docs: ...`
  gives `symbol_lines=0 matched=0`: out of denominator *and* numerator, a silent 100%, exactly
  as the docstring now states. And "a timestamp whose FORMAT changed still lands here" holds:
  `1789525225 [tick] docs: ...` gives `symbol_lines=1 matched=0`, a visible alarm.
- `podfreeze:879-888` — `while [ "$#" -gt 0 ]` at `:813`; `-h|--help` exits 0 at `:817`;
  `-*` and the two missing-value paths call `die`, which is `exit 1` at
  `freeze-common.bash:154-157`; every other branch shifts. No `break`. True.
- `test-qa-helper-summary.bash:60-64` — `refuses()` asserts non-zero exit **and** empty stdout
  (`:68-77`), and the channel is asserted separately at `:145-159`. True.

**5 — Task 2.1 option (d): admissible, and not over-corrected.**

It is genuinely not (b). (b) is environment-conditional; (d) is unconditional and needs no
network. That is the axis `CLAUDE.md`'s "Missing Dependencies" rule actually turns on, and (d)
does not sit on the prohibited side of it.

The supporting facts check out exactly:

- The correspondence is exact: `git ls-files .claude/rules/` gives 15; files carrying
  `hooks-daemon-rule-version` gives 8; files linking into the daemon tree gives the **same** 8.
  The 7 repo-authored files link nowhere near it.
- **No real link rot is lost today.** Each of the 8 contains exactly one markdown link, and
  all eight are `../hooks-daemon/CLAUDE/DirectoryRoles.md`. Zero repo-owned targets.
- **The `docs_qa` mitigation is real, not asserted.** `.claude/rules` is in the daemon's own
  corpus (`corpus.py:70` `_SATELLITE_DIR_NAMES`, consumed at `:418` inside `is_in_scope`,
  `:395-420`), and `docs_qa/checks/pointer_resolves.py` does file-existence link resolution
  over that corpus at SWEEP. So a genuinely broken daemon-authored link is reported by the
  daemon on any installed machine.

Where the claim is **not** sound: `link_check.py:214-223` is a path-prefix mechanism and
`.claude/rules/` is an explicit positive inclusion at `:243`. Same rationale, materially
different mechanism, with a silent-under-match failure mode the prefix list does not have.
See finding 8 — this does not make (d) inadmissible; it makes a coverage line a condition of
implementing it.

**6 — The provenance header on round 4's report.** Honest about what it is and does not
overstate completeness: it says plainly "It is not the reviewer's own file" and "the detail it
says was dropped is genuinely gone". One phrase overstates — "transcribed **verbatim in
substance**" combines two different and incompatible claims (word-for-word versus faithfully
paraphrased) and reads stronger than either. "Transcribed from that message" would be
accurate. Nit, not a should-fix.

## Nits

- `318fe424`'s own observation — *"a reviewer that cannot persist its own output is a tooling
  gap, not a one-off"* — exists only in the commit message. That is the exact failure mode the
  commit exists to fix, and the next round will not see it. It belongs in the plan or the
  journal. This round is a second instance: the round-5 reviewer's `Write` was withheld too,
  and this file exists only because a stop hook forced the issue.
- No journal entry covers `318fe424`.
- `JOURNAL/00125-Journal-26-09-16.md:396` records ":602 to :597" as fixed; `FINDINGS.md` now
  contains neither — the citation was deleted rather than corrected. Journals are append-only,
  so nothing to change; noting it so a future reader does not go looking for `:597`.
- `CLAUDE/QA.md:22` — orphaned "A missing" at line end after the inserted sentence.

## Checked and clean

- **Fail-fast** — no new `failed_when: false` / `ignore_errors: true`, no `|| true`, no
  skip-and-warn. `helper_skip_count` still hard-fails an unreadable capture rather than
  answering `0`. The awk substitution aborts under `set -e` if awk is absent, which is the
  right direction.
- **Stderr hygiene** — `helper_test_summary` writes only its payload to stdout;
  `helper_skip_count`'s diagnostic goes to stderr and that is now *asserted* rather than
  described.
- **Version bumps** — none owed. Across every file the whole plan has touched there is no
  `files/var/local/claude-yolo/**`, no Dockerfile/entrypoint/patch script/deployed skill, and
  the freeze tools carry no version constant.
- **Public-repo safety** — scanned both commits' added lines for home paths, non-example
  emails, RFC 1918 addresses, `.local` hostnames and key blocks: no matches. The `Owner`
  field's value is a pre-existing repo-wide convention in 84 plan files, not introduced here.
- **Plan Commit Rule** — tree clean, `HEAD == origin/F44`, journal append-only (74 added,
  0 removed), `CLAUDE/Plan/README.md:37` index row present. `318fe424` is a plan-only commit,
  which the rule explicitly encourages.
- **Placement in the IaC graph** — no new play, no new script, no new abstraction; every
  change is an edit to the file that already owns the concern. Nothing to relocate.
- **Modes and lint** — git records `100644` for the library and `verdicts.py`, `100755` for
  the test script and `podfreeze`. `shellcheck -x` clean on both changed bash files.

## Mechanical gates

- **`./scripts/qa-all.bash`**: **PASS** — exit 0, `QA passed: 918 files checked`, 37 stage
  names, `helper-tests: Ran 1464 tests, 1 skipped`, `helper-summary-readers: passed: 20`.
- **`hooks-daemon plan-qa --sweep`**: exit 1, **0 block / 2 advise**, both pre-existing and
  unrelated to 00125 — `path-existence` on `00046-localhost-yml-leak-guard`, and
  `journal-freshness` naming 13 other plans.
- **`ansible-playbook --syntax-check`**: **not triggered** — neither commit touches a
  playbook. `qa-all`'s `ansible-syntax` stage ran repo-wide anyway: 82 playbooks OK.
- **`scripts/qa-helper-tests.bash`** standalone: **PASS** (`Ran 1464 tests ... OK (skipped=1)`)
  — triggered by `helpers/qa_environment/verdicts.py`.
- **`helpers.gnome.check_extension_compat`** and **`extensions/` eslint**: **not triggered** —
  no `extensions/` metadata or extension JS in either commit. compat ran inside `qa-all`
  regardless: 5 extensions OK.

## Task 5.3 — not dischargeable

What stands between it and done, precisely:

1. Finding 1 — the last-match rule and its two false premises (`lib:24-26`, `lib:44`), plus a
   fixture with the decoy *after*.
2. Finding 2 — the `25 gates` attribution in `PLAN.md:169-174` and `FINDINGS.md:180-185` /
   `:236-239`.
3. Finding 3 — `FINDINGS.md:165` (28 to 27) and `:244` (28 to 29).
4. Finding 4 — `FINDINGS.md:235`'s unattributed "25 passing gates".
5. Finding 5 — `PLAN.md:200` 16 to 20.
6. Finding 6 — `PLAN.md:76` 35 to 40.
7. Finding 7 — the (d) table cell at `FINDINGS.md:78`.

Finding 8 is conditional on the owner choosing (d) and does not block 5.3; it should be
written into Task 2.2's acceptance now so it is not discovered afterwards. Tasks 2.1, 2.2, 4.3
and 5.2 are open by design, and nothing in this review changes that.
