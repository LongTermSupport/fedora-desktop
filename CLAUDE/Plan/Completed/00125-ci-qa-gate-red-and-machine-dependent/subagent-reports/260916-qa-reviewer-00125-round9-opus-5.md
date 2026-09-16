# QA Review — Plan 00125, Task 5.3, round 9 (pinned worktree at `8a5354a7`)

> **Provenance**: `qa-reviewer` subagent (Opus 5), 2026-09-16, written via a Bash heredoc
> because `Write`/`Edit` are withheld from this reviewer session. Reviewed inside the
> dedicated linked worktree on branch `worktree-plan-00125-review`. No repository file was
> modified; the one mutation test below was performed in memory, never on disk.

**Pin**: `git log --oneline -1` = `8a5354a7 Plan 00125: two more copies, and the sentence
that hid them` and `git status --porcelain` = empty, checked as the first action **and**
again as the last. **The tree held still for the whole review.**

**Verdict**: FIX-BEFORE-MERGE

All eight dispatch claims check out; I verified each against the code rather than the
commit message, and four of them by enumerating the population rather than sampling. What
stops 5.3 discharging is not the code: it is that Task 4.5's sweep, already corrected once
for exactly this, still under-counts its population — and the one gate that is *actively*
emitting the defect today was not in either sweep.

## Blocking

None. Nothing here breaks a user, loses data, leaks an identifier, or violates a HARD RULE.

## Fix before merge

### 1. The sweep is still scoped to what was looked at, and the live instance was missed

`FINDINGS.md`, anchor *"it was 21 of the 29 non-merged gates, and 2 of the remaining 8 had
the same defect"*.

I enumerated all 29 hard gates in `scripts/qa-all.bash` and classified how each derives its
stage-line text. 21 now use `qa_gate_case_count`, 2 use `qa_gate_detail`, 1 uses
`helper_counts_summary`, 1 (`deployed-drift`) prints its own line. **Four of the remaining
eight still derive their stage line by an unscoped read of the child's whole capture**, and
one of them is exhibiting the defect on every run:

- **`vmtest-manifest` emits a THREE-LINE stage line today.** Measured:

      ✓ vmtest-manifest: vars/vm-test-scenarios.yml: scenarios=8 runnable=8 bridge=7 host_only=1 bases=3
      vars/vm-test-scenarios.yml: 8 scenario(s) agree with their guest checker
      vars/vm-test-scenarios.yml: 1 scenario fixture(s) run before a declared reboot

  Not observational — `scripts/qa-vmtest-manifest.bash:59`, `:135` and `:191` are three
  unconditional `echo`s to stdout on the success path, and `qa-all.bash` (anchor
  `printf '✓ vmtest-manifest: %s\n'`) interpolates the whole capture. Fed through the real
  parser, `verdicts.parse()` keeps **only the first line**; the other two — both coverage
  measurements — are dropped:

      stages: {'vmtest-manifest': ['vars/vm-test-scenarios.yml: scenarios=8 runnable=8 bridge=7 host_only=1 bases=3']}
      symbol_lines: 1  matched_lines: 1

  That is the defect Task 4.5 exists to remove, quoted verbatim in the new library header
  (*"the stage line became TWO lines — which `verdicts.py` half-drops, reading the first as
  the stage and losing the rest"*), live, in `qa-all.bash`, on every run. It was missed
  because it is not caused by `grep -o` — which is precisely the reason the first sweep's
  scoping was declared wrong.

- **`extension-compat` and `panel-contract` keep an assertive `||` fallback**, anchors
  `|| compat_summary="OK"` and `panel_contract_summary="OK"`. Both read the whole capture
  with an unscoped `grep -E` and substitute the word `OK` when the pattern stops matching.
  `OK` is indistinguishable from a real terse answer — which is the argument the new
  `qa_gate_detail` header makes in this same commit (*"A fallback that ASSERTS something is
  worse than no fallback ... it reads exactly like a measurement"*), 25 lines above them in
  the same file. Measured today: each pattern matches exactly once, so both are latent
  rather than blind — exactly the state `planlib-tests` was in when it *was* repaired.

- `version-pins` interpolates its whole capture too; one line today, latent.

Both surviving `||` readers are the only ones left: `grep -nE '\|\| [a-z_]+_summary='` over
`qa-all.bash` returns those two lines and nothing else.

**Fix**: route the four through `qa_gate_detail` with the wording each gate actually prints
(`vmtest-manifest` needs a chosen line, not the capture), and correct the FINDINGS sentence
to the enumerated figure. The number to write down is *4 of the remaining 8 had the shape;
2 were repaired*, or the sweep is written up as total for the third time.

### 2. "Pure refactor: every stage line byte-identical" is false, in the bullet that falsifies it

`PLAN.md`, Task 4.5, anchor *"Pure refactor: every stage line byte-identical bar the reader
gate's own count"*; the same sentence in `FINDINGS.md`, anchor *"Verified as a pure
refactor: every stage line in a full run is byte-identical to the run before it"*.

Measured against the real captures, old expression vs new:

    OLD ✓ nokill-containerwatch: no forbidden kill call sites
    NEW ✓ nokill-containerwatch: 3 container-watch file(s) clean

So there are two exceptions now, not one, and the second is described three clauses earlier
in the same bullet. Round 8 had already named it: *"The nokill one is a genuine repair, not
a refactor."* `planlib-tests` is byte-identical both ways, so the exception is exactly one
stage line plus the reader gate's own count.

This is the plan's own class — a measurement taken correctly before the population changed,
then carried forward as a law — in the commit that retracts two others.

### 3. Round 8's finding 1 was closed in the header and not in the two places that point at it

The library header is fixed and correct (*"it defines three functions"*, and the
distinction between the one that parses no stream and the two that do). Its two references
were not:

- `scripts/qa-all.bash`, anchor *"The reader for the helper-tests stage line"* — still
  singular. It now sources three functions serving 24 stage lines.
- `CLAUDE/QA.md`, catalogue row anchor *"the reader behind this suite's own `helper-tests`
  line"* — still singular. That suite now has 51 cases, 17 of which cover the two readers
  shared by 23 other gates.

Wider, and the part worth the owner's attention: `qa_gate_case_count` and `qa_gate_detail`
appear **nowhere** in `CLAUDE/`, `docs/` or `.claude/rules/` outside this plan's own
PLAN.md and FINDINGS.md. `CLAUDE/QA.md` is the single source of truth for the gates, and 23
of the 29 hard gates now take their stage line from a shared library it does not mention.

### 4. `qa-js.bash` aborts the whole run in an environment the Task 4.2 table does not list

`CLAUDE/QA.md`, table anchor *"The same command does not reach the same verdict
everywhere"*, lists four stages. In this linked worktree `./scripts/qa-all.bash` exits **2**
before reaching any of the 29 hard gates, because `qa-js.bash` exits 2 on
`extensions/node_modules` being absent — deps its own message says *"no playbook
installs"*. `ansible-syntax` also fails 82/82 for the missing vault password file, which the
table *does* declare for a linked worktree.

That is Task 4.1's mechanism, live, in the environment this review was told to use: one
stage that cannot pass stopped every gate behind it from running. The table built by Task
4.2 (marked ✅) is the instrument for exactly this and is missing the row. Same partial-
population shape as finding 1 — the table covers the stages the author had in mind.

## Should fix

### 5. The coupling that broke is still not tested against the real gate

`scripts/test-qa-helper-summary.bash`, anchor *"the nokill gate's real wording is read"*,
asserts `qa_gate_detail` against a **hardcoded copy** of the gate's output string. The thing
that failed for the whole life of the old reader was the pattern drifting from the gate's
wording; nothing in the tree couples the two. If `qa-nokill-containerwatch.bash` changes its
wording, the case stays green, the gate still exits 0, and the stage line silently becomes
`summary unreadable` — visible to a human reading it, invisible to every gate.

The suite already knows the answer and states it one screen up (anchor *"the real runner
writes a file this reader understands"*: *"the only case here that would survive the format
being changed on one side only"*). The same discipline was not applied to the two patterns
whose drift is the defect being repaired. One case that runs the real gate and asserts the
answer is not `summary unreadable` closes it; the gate is a grep over 3 files.

## Nits

- **The test count has drifted again, in two directions.** `CLAUDE/QA.md` anchor *"one
  `print()` among 1,482 tests"*; `scripts/qa-all.bash` anchor *"one `print()` in any of
  1,483 tests"*. Measured here: `Ran 1485 tests`. Two files, two stale numbers, disagreeing
  with each other — and the delta is exactly the two runner tests this commit added.
- **Both bash headers cite the wrong task.** `scripts/lib/qa-helper-summary.bash` and
  `scripts/test-qa-helper-summary.bash` both open *"Plan 00125, Task 4.2"*. PLAN.md
  attributes this file to Task 4.4 and 4.5; Task 4.2 is the `CLAUDE/QA.md` write-up. The
  library's one-line purpose statement (*"Read the `helper-tests` stage line out of the
  counts file"*) also still describes one of its three functions.
- **`PLAN.md`'s Non-Goals cite a document that does not exist on this branch** — anchor
  *"`CLAUDE/Plan/00123-…/WORKTREE-QA-GAP.md`"*. `git ls-files | grep 00123` is empty, there
  is no README row, and the plan lives on an unmerged branch (`worktree-ccy-reboot-restore`,
  PR 47). `qa-docs.bash` excludes `CLAUDE/Plan/**`, so nothing catches it. It is the gap
  that stopped `qa-all.bash` completing here (finding 4).
- **The two fallbacks in one file answer the same question differently.**
  `qa_gate_case_count` degrades to `passed`, `qa_gate_detail` to `summary unreadable`; only
  the second's shape is argued. Since all 21 callers emit a count (measured), degrading
  there to `summary unreadable` as well would cost nothing and would stop the fallback from
  sharing its first word with the real answer. A considered decision as it stands, not an
  oversight — noted rather than pressed.

## Checked and clean — what I verified, and how

Each dispatch claim, checked against the code:

1. **`--tracked-modules` required, default deleted, docs moved with it** — `required=True`
   with no `default=`; the module docstring's usage example carries the flag; the help text
   has no "defaults to" clause. All four call sites pass it (`qa-helper-tests.bash`,
   `test-qa-helper-summary.bash`, and both test helpers). No spelled-out invocation
   survives anywhere else — `grep -rn 'tracked.modules'` over the tree returns only those.
2. **The `tracked=7` case is real, and I re-ran the mutation myself.** In memory, I
   replaced `counts_text` so `main()` records the module count instead of the flag, and ran
   `TestRunEndToEnd`: `RAN: 9 FAILURES: 1`, the single failure being
   `test_main_records_the_tracked_total_it_was_given_not_the_module_count`. Nothing else in
   that class notices. No file was touched; `git status --porcelain` empty afterwards.
3. **Library header** — correct, and it distinguishes `helper_counts_summary` from the two
   readers that do parse a stream.
4. **`qa_gate_detail`** — exists, 5 cases of its own, and both routed gates render one line:
   `✓ nokill-containerwatch: 3 container-watch file(s) clean` and
   `✓ planlib-tests: PASSED (library version 1.2.0)`. Negative control with a pattern that
   cannot match returns `summary unreadable`. The *"zero matches in its entire life"* claim
   holds: the gate has one commit, and `checked` appears nowhere in it.
5. **`qa_gate_case_count`'s justification is now a measured property** — and it reproduces.
   Enumerated across all 21 gates, not sampled: every one exits 0, every one emits
   **exactly one** line matching `passed:[[:space:]]+[0-9]+`, and all 21 of those lines are
   on **stdout** (0 on stderr), so the `2>&1` merge cannot reorder them. The retracted
   claim reproduces too: 8 of 21 print after their summary (`secret-scan`,
   `ccy-rootless-guard`, `ccy-token-mode`, `ccy-ssh-handling`, `freezelib`, `lxcfreeze`,
   `podfreeze`, `qa-ansible-failfast`). No gate backgrounds anything; all 15 `EXIT` traps
   are `rm -rf`, including the two indirected through a `cleanup` function, which print
   nothing. The word-fallback is correctly described as defensive.
6. **Citations** — `grep -n 'qa-all\.bash:[0-9]'` over PLAN.md and FINDINGS.md returns
   nothing. Each of the four anchors matches **exactly once** in `scripts/qa-all.bash`. I
   also resolved the seven surviving line citations into files this plan does not edit
   (`qa-deployed-drift.bash:192` and `:219`, `play-podfreeze.yml:75`,
   `play-lxcfreeze.yml:91`, `link_check.py:214-223` in two files, `triage.bash:18-23`) —
   **all seven land on what they claim**.
7. **The corrected numbers** — 21 `qa_gate_case_count` call sites and 2 `qa_gate_detail`
   call sites, counted in the file; 29 hard gates, enumerated. See finding 1 for where the
   corrected sentence is still short.
8. **PLAN.md counts and the Task 1.3 move** — `Ran 21 tests` from the runner suite;
   `passed: 51` from the reader suite; 17 cases across the two new sections (12 + 5),
   matching Task 4.5's figure. PLAN.md is 17,356 bytes, under the 18,000 advisory. I diffed
   the moved Task 1.3 text clause by clause against `FINDINGS.md`'s *"The two `host_health`
   tests, one of which was a dated bomb"*: the injection-seam argument, the 2026-09-28 bomb,
   the `test_handoff` container/runner asymmetry and the emulated-runner verification are
   all present. **Nothing was lost in the move.**

Beyond the claims:

- **Pure refactor, verified independently of the commit message.** I sourced the library,
  ran each of the 21 gates, and compared the *exact old expression* against
  `qa_gate_case_count` on the same capture: **21 of 21 SAME**, one match line each. The
  only stage line that changed is `nokill`'s — which is finding 2.
- **End to end**: `qa-helper-tests.bash --counts-file --counts-token` writes a complete
  counts file, the gate's stdout is **0 bytes**, `helper_counts_summary` renders
  `Ran 1485 tests in 65 modules (65 tracked), 1 skipped`, and `COVERAGE: 65 of 65` reaches
  stderr with the file names. The empty-stdout gate in `qa-all.bash` therefore still holds.
- **Fail-fast**: no `failed_when`/`ignore_errors`/`|| true`/`2>/dev/null` in any changed
  file. Both soft degrades sit under an `if ! ...; then exit 1` that already carries the
  gate's verdict, so no failure signal is discarded.
- **Stderr hygiene**: both new functions put only their payload on stdout; every diagnostic
  in `helper_counts_summary` is `>&2`; the coverage line and file names stay on stderr.
- **`qa_tracked_helper_tests`**'s `[[ -f ]]` filter drops a tracked-but-deleted file from
  the denominator — I checked before flagging it, and it is the established idiom shared by
  all five tracked-set helpers in `qa-discovery.bash`. Consistent, not a defect.
- **IaC placement**: no playbook, no `files/`, no `files/var/local/claude-yolo/` change, so
  **no CCY version bump is required and correctly none is present**. No Ansible was run.
- **Public-repo safety**: scanned all 1,629 added lines across `551a65d7..8a5354a7` for
  emails, home paths, private IPs, hostnames, container names and checkout paths. The only
  hits are `files/home/.local/...` repo paths and one `/workspace` — the CCY mount point
  `CLAUDE.md` itself documents. **Clean.**
- **Plan hygiene**: journal append-only in the last commit (**68 added / 0 deleted**), plan
  and code committed together, `CLAUDE/Plan/README.md:37` row present, no untracked plan
  directory. Task statuses match what I could verify.
- **Lint and modes**: `shellcheck -x` clean on all five changed bash files; `ruff check`
  clean on both Python files; modes correct (`qa-all`, `qa-helper-tests`,
  `test-qa-helper-summary` 100755; libraries and Python 100644).
- **Naming**: `qa_gate_case_count` and `qa_gate_detail` both say what they do. The only
  structural naming point is that a reader for 23 gates lives in a file called
  `qa-helper-summary.bash` — cosmetic, and the owner's call.

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `./scripts/qa-all.bash` | **exit 2 — could not complete in this worktree.** `qa-js.bash` exits 2 (`extensions/node_modules` absent) before any hard gate runs; `ansible-syntax` fails 82/82 on the missing vault password file. Both are linked-worktree environment gaps, neither caused by this change. See finding 4 |
| every gate under review, run individually | **all green**: 21 case-count gates exit 0, `nokill` and `planlib` exit 0, `helper-tests` exits 0 |
| `scripts/test-qa-helper-summary.bash` | exit 0, `passed: 51 failed: 0` |
| `python3 -m unittest tests.helpers.qa_environment.test_unittest_counts` | exit 0, `Ran 21 tests ... OK` |
| `scripts/qa-helper-tests.bash` (both ways) | exit 0, `COVERAGE: 65 of 65`, `Ran 1485 tests`, gate stdout 0 bytes |
| `scripts/qa-docs.bash` | exit 1, **8 findings, all Cause A** (`.claude/rules/*.md -> ../hooks-daemon/CLAUDE/DirectoryRoles.md target does not exist`) — the one open decision, Task 2.1. Reproduced here for the same reason CI reproduces it |
| `hooks-daemon plan-qa --sweep` | **NOT RUN.** The daemon tree is gitignored and absent from the linked worktree, and this review is confined to the worktree. Saying so rather than omitting it: round 8 ran it at `ac706483` — exit 1, 0 block / 2 advise, neither finding touching Plan 00125 |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook changed. It could not run here regardless (no vault password file) |
| `qa-helper-tests.bash` conditional gate | **triggered** (`helpers/` + `tests/helpers/` changed) — run, above |
| `check_extension_compat` | **not triggered** (no `extensions/` metadata change); run anyway as a probe for finding 1 — exit 0 |
| extension ESLint | **not triggered** (no extension JS change); not runnable here (no `node_modules`) |
| `shellcheck -x`, `ruff check` | clean |

## Does Task 5.3 discharge?

**Not yet, and the remaining list is short.** The engineering is sound and I confirmed it
rather than trusting it: the refactor is byte-identical across 21 of 21 gates, every
population claim in the new comments reproduces under enumeration, the `tracked=7` case
genuinely pins the wiring under mutation, the anchors are unique, and the plan documents are
tidy and lossless. Six rounds ago that would have been the whole question.

It does not discharge because two of this plan's own recurring defects are live in its own
deliverable: a sweep whose corrected population is still smaller than the defect (and which
misses the one gate emitting a multi-line stage line on every run), and a "pure refactor,
every stage line byte-identical" claim falsified by a change described in the same bullet.

Required before 5.3:

1. Route `vmtest-manifest` — and, on the same argument, `extension-compat`,
   `panel-contract` and `version-pins` — through `qa_gate_detail`, and write the enumerated
   figure into `FINDINGS.md` (finding 1).
2. Correct "pure refactor / every stage line byte-identical" in `PLAN.md` and `FINDINGS.md`
   to name the `nokill` repair as the second exception (finding 2).
3. Close round 8's finding 1 in `qa-all.bash` and `CLAUDE/QA.md`, and give the two shared
   readers a line in `CLAUDE/QA.md` (finding 3).
4. Add the `qa-js.bash` row to the machine-dependence table (finding 4).

Follow-up, not 5.3's business: the real-gate coupling case (finding 5), the test-count
drift, the task-number headers, the dangling 00123 citation, and the fallback asymmetry.
Tasks 2.1, 2.2, 4.3 and 5.2 remain the owner's decisions and are correctly out of scope.

The worktree did its job: the pin held at both checks, so every citation above was measured
against the same bytes it names.

## Review conditions

- Target: `8a5354a7`, in a dedicated linked worktree on `worktree-plan-00125-review`.
- `git status --porcelain` empty at the first action and at the last; `HEAD` unmoved.
- Read-only on repository files. The single mutation test ran in memory.
- Container: CCY — no Ansible run, no deploy.
