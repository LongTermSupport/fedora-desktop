# QA Review — Plan 00125, Task 5.3, round 11 (pinned worktree at `e6ec507a`)

> **Provenance**: `qa-reviewer` subagent (Opus 5), 2026-09-16, written via a Bash heredoc
> because `Write`/`Edit` are withheld from this reviewer session. Reviewed inside the
> dedicated linked worktree on branch `worktree-plan-00125-review`. **No repository file was
> read, run or modified outside that worktree, and no repository file inside it was
> modified.** Every fixture was built in a `tempfile` temp directory.

**Pin**: `git log --oneline -1` = `e6ec507a Plan 00125: round 10 — Cause A's own shape was
inside the fix for Cause A`; `git status --porcelain` empty. Checked as the first action and
again as the last. **The tree held still for the whole review.**

**Verdict**: FIX-BEFORE-MERGE

**The mechanism swap is right, and for the first time I can prove the central invariant on
two machines at this exact commit rather than argue it.** CI run `35079565283` on
`e6ec507a` succeeded and printed `✓ docs: 71 files OK (links, anchors, playbook catalogue,
topic index) — VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken`; this worktree,
also without the daemon, prints that line byte-for-byte; the commit message records the host,
with the daemon present, printing `8 verified, 0 unverifiable, 0 broken`. Same exit code,
different sentence — measured, not asserted. Round 10's findings 2, 3, 4, 6, 7 and 8 are
closed and I re-derived each independently.

What stops 5.3 discharging is the same thing that stopped it last round, and it is once more
inside the fix: **the guard added to close round 10's finding 1 reports a PASS when it is
blind.** Around it sit a half-applied widening, a denominator that is not independent of the
numerator, and five claims that were true when measured and are not true now.

## Blocking

None. Nothing here breaks another user, loses data, leaks an identifier, or violates a HARD
RULE. I scanned all 401 added lines of `e6ec507a` for emails, `/home/<user>` paths, `/root/`,
RFC1918 and loopback addresses, checkout paths, 12–64-char hex ids, `.local`/`.lan`/`.home`
hostnames and the author's name: **zero hits on every pattern**.

## Fix before merge

### 1. The COVERAGE guard reports `PASS` when it cannot measure — the eighth instance, inside the fix for the seventh

`scripts/test-qa-helper-summary.bash`, anchor `detail_calls=$(grep -cE`.

Before this commit the guard was `if [ "${#detail_sites[@]}" -eq 0 ]`, which fired on zero
unconditionally. It is now reached only *after* a mismatch comparison against
`$detail_calls`, and `$detail_calls` is an unvalidated command substitution. When
`qa-all.bash` cannot be read, `grep -c` exits 2 with empty stdout, so `detail_calls` is the
empty string, **both** `[` tests abort with `integer expression expected` (exit 2, so both
conditions read false), and control falls through to the `else` branch.

Measured by extracting the file's own extraction-and-guard block verbatim into a temp
directory and running it against a non-existent path:

    CASE A: QA_ALL missing
      grep: ...: No such file or directory
      [: : integer expression expected
      [: : integer expression expected
      PASS  COVERAGE: 0 of  qa_gate_detail call site(s) extracted
      RESULT passed=1 failed=0 sites=0 calls=[]   exit=0

    CASE B: QA_ALL real
      PASS  COVERAGE: 8 of 8 qa_gate_detail call site(s) extracted
      RESULT passed=8... calls=[8]                exit=0

`failed` stays 0, the `for site in` loop iterates zero times, and the suite exits 0. The two
diagnostics go to stderr, and `qa-all.bash` merges this suite's stderr into a capture it
discards on a successful run (anchor `if ! helper_summary_out="$(bash "$SCRIPT_DIR/test-qa-helper-summary.bash" 2>&1)"`),
so nothing reaches a human. **A clean result indistinguishable from a blind one, in the
guard written to make them distinguishable** — and the case that is now blind is the exact
case the deleted guard covered.

**Fix**: validate the denominator before comparing — `[[ "$detail_calls" =~ ^[0-9]+$ ]]` or
`grep -c … || fail` — and test the zero case first, so an unmeasurable denominator is a
failure rather than the fall-through.

### 2. The extraction regex was widened; the parser that consumes it was not

Same file, anchors `detail_re='qa_gate_detail "\$\{?…` and `site_var="${site#qa_gate_detail`.

The header states the property as delivered: *"THE EXTRACTION MUST ACCEPT EVERY SPELLING, or
a call site is silently not checked and the coupling this whole section exists for does not
apply to it."* The regex accepts five spellings. The four lines that turn a match into
`(var, pattern)` still assume `"$lower_snake"` and a **single-quoted** pattern. Measured, one
call site per fixture file, running the real regex and the real parameter expansions:

    "$nokill_out"   + 'pattern'   num=1 den=1  var=[nokill_out]   pattern=[[0-9]+ clean]   OK
    "${nokill_out}" + 'pattern'   num=1 den=1  var=[{nokill_out}] <- braces kept
    "$nokillOut"    + 'pattern'   num=1 den=1  var=[nokillOut]                             OK
    "$nokill_out2"  + 'pattern'   num=1 den=1  var=[nokill_out2]                           OK
    "$nokill_out"   + "pattern"   num=1 den=1  pattern=[qa_gate_detail "$nokill_out" "[0-9]+ clean"]

The brace form reaches `gate_command_for "{nokill_out}"`, which returns 1, so the case FAILS
with *"no gate command is registered for it"* — a true sentence about a variable that does
not exist. The double-quoted form hands the **entire call-site text** to `qa_gate_detail` as
the pattern and FAILS with *"the pattern qa-all.bash applies to … no longer matches that
gate's output"* — which is a false diagnosis: the pattern was never extracted.

So two of the five newly-accepted spellings are extracted and then misdiagnosed rather than
checked. **No committed case covers any of the four new spellings.** The mutation the journal
cites (a line-continuation call site, `extracted 7 … but it has 8`) exercises the
*denominator*, not the widening — so the widening itself is unverified, in the suite whose
subject is verification that does not exercise what it vouches for.

**Fix**: strip an optional `{`/`}` from `site_var`, and select the pattern by quote character
rather than assuming `'`. Add one case per accepted spelling.

### 3. "a deliberately loose pattern that cannot miss what the strict one catches" is false, and one spelling is missed by both

Same file, anchor *"the denominator is counted independently, from a deliberately loose
pattern that cannot miss what the strict one catches"*.

The denominator is `grep -cE '[^_]qa_gate_detail '`. It requires a character before the name;
the numerator does not. Measured:

    call site at column 0          numerator=1  denominator=0  -> MISMATCH (a false FAIL)

So it *can* miss what the strict one catches, and the sentence justifying the guard is a
claim where a measurement belongs.

Worse, the two patterns are **not independent**: both key on the literal `qa_gate_detail `
including the single space. A spelling that separates the name from its first argument by
anything other than exactly one space is missed by both, and they agree:

    tab between name and "$var"    numerator=0  denominator=0  -> silently under-counts, PASS

That is the "two agree while both under-count" shape, and it is the only shape this design
cannot see. It is also line-counting versus match-counting: `grep -c` counts lines, so two
calls on one line disagree with the numerator.

**Fix**: derive the denominator from something structurally different — e.g. count lines
matching `qa_gate_detail` with no trailing-space or leading-character constraint, or parse
the call sites in Python the way `link_check.qa_gates` parses gate invocations.

### 4. `COVERAGE: n of m` is produced and never delivered

Same file, anchor `printf '  PASS  COVERAGE: %s of %s`.

The line goes to the suite's stdout, and `qa-all.bash` captures that stream and reduces it to
`qa_gate_case_count`'s single number. CI run `35079565283` at this commit prints exactly one
line from this gate:

    ✓ helper-counts-reader: passed: 60

`CLAUDE/QA.md` states the rule against this itself, anchor *"The second number goes in the
stage line specifically… a coverage figure reported only there is produced and never
delivered — which is one step short of the class this page is about."* The `n of m` should
ride in the stage line the way `version-pins` carries `COVERAGE: 9 of 9` — i.e. become part
of what `qa_gate_case_count` returns, or be printed by `qa-all.bash` beside it.

### 5. Two numbers in the plan were true when measured and are not true now

- **`FINDINGS.md`, anchor *"The suite has 1,482 tests"***. Measured at this commit:
  `scripts/qa-helper-tests.bash` → `Ran 1511 tests`, and CI run `35079565283` →
  `✓ helper-tests: Ran 1511 tests in 65 modules (65 tracked), 2 skipped`. This commit added 7
  of the difference itself (`link_check` 72 → 79). **This is the surviving copy of the
  sentence `CLAUDE/QA.md` deleted for this exact reason** — QA.md's anchor reads *"This
  sentence carried the test count until it had rotted twice and the two copies of it
  disagreed. A number that must be re-measured to stay true does not belong in prose."* It
  has now rotted a third time, in the other copy.
- **`PLAN.md` Task 4.4, anchor *"`scripts/test-qa-helper-summary.bash` (51 cases)"***. The
  suite prints `passed: 60` here and in CI. Counting PASS/FAIL lines per section: the four
  sections Task 4.4 describes account for **34**; Task 4.5's sections account for **26**,
  which is the figure Task 4.5 *was* updated to in this commit (25 → 26) while its neighbour
  was not. `51` was the whole-suite total when the sentence landed (`8a5354a7`; the journal
  records `helper-counts-reader: passed: 51` at that point).

### 6. The two headline classification tables still ask the question this commit removed

The commit's whole point is that "ignored" and "not tracked" are different populations. Both
summary tables still name the old one, and each is followed 25 lines later by a paragraph
explaining why it is wrong:

- `CLAUDE/QA.md`, table row **`| ignored but vendored by nobody |`**
- `CLAUDE/Plan/.../FINDINGS.md`, table row **`| ignored, but vendored by nobody |`**

A third copy is in the production docstring: `helpers/docs/link_check.py`, `check_links`,
anchor *"A target this repo ignores but nobody vendored is a FINDING, not an exemption."*

The bucket is now *not tracked, and vendored by nobody*. A tracked file that happens to match
an ignore rule moved **out** of this bucket and a present-but-unignored untracked file moved
**in** — which is the finding. Three documents asserting the old divergence was gone is how
this survived a round; three documents still describing the old question is the same shape.

### 7. `qa-docs.bash`'s comment explains a `// "?"` idiom this commit deleted three lines below it

`scripts/qa-docs.bash`, anchor *"`// \"?\"` rather than `// 0` — a missing key means the
checker stopped emitting it, and that must not read as a clean zero."*

`V_OK` and `V_UNVERIFIABLE` no longer carry `// "?"`; they are plain `jq -r '.vendored.ok'`
behind the new precondition, and the same comment block ends *"A missing key is now a hard
failure, not a count."* The paragraph argues for a mechanism that is no longer in the file it
annotates — a citation to code that does not exist, which is round 10's finding 2 reappearing
seven lines from the guard that closed it.

## Should fix

### 8. The exit code still depends on what is on the disk — one level up, in the scan population

`helpers/docs/link_check.py`, anchor `def collect_scope`.

The *targets* now ask trackedness. The *documents* are still whatever `os.walk` finds. An
untracked in-scope markdown file is scanned and its broken link is a finding. Measured in a
fixture git repo — a never-`git add`ed `helpers/CLAUDE.md` produced
`[{'file': 'helpers/CLAUDE.md', 'problem': 'target does not exist'}]`. That fails here and
passes in CI, which is Cause A's direction reversed and is neither reached nor named.

Enumerated, not sampled: **71 of 71** in-scope documents are tracked in this checkout, so it
is latent. But the gate says `71 files OK` with no denominator, while
`qa-helper-tests.bash` — for exactly this reason, and documented in `CLAUDE/QA.md` under
*"discovered but not tracked"* — prints `COVERAGE: 65 of 65 tracked helper test modules` and
`65 modules (65 tracked)` in its stage line. The lesson is written down beside the thing it
fixed and not generalised to the gate two rows below it.

**Fix**: report `N files (M tracked)` in the docs stage line, the same shape as the
helper-tests line.

### 9. The exit-2-on-no-checkout claim is still unasserted, and its only test describes the mechanism that was replaced

`tests/helpers/docs/test_link_check.py`, `test_a_tree_git_cannot_answer_for_fails_loudly`.

The docstring reads *"No git checkout means the **ignore question** has no answer… Returning
**\"nothing is ignored\"** would hand back a confident verdict…"*. The code it covers raises
about `git ls-files` and trackedness. The commit rewrote the `_GitTree` class docstring and
`tracked_paths`'s docstring and left this one — the single test guarding the raise.

The chain still works; I measured it end to end this time rather than inferring it. Against a
non-git temp tree:

    link_check exit=1
    RuntimeError: git ls-files failed in … (exit 128): fatal: not a git repository … —
      cannot tell which link targets this repository tracks, so no link verdict here would
      mean anything
    jq validation REFUSES the payload -> qa-docs.bash would exit 2

**Should it be asserted?** Yes — the docstring claims *"the gate reports exit 2"* and the test
asserts only the raise, so the two hops that make it 2 are covered by nothing. The cheapest
honest version is to give `qa-docs.bash` the root seam it lacks (round 10's open nit) and
assert the gate's exit code; that same seam is what would let the `V_BROKEN -gt 0` branch be
exercised by the script rather than by a hand-assembled payload.

### 10. Two target shapes the swap reclassifies, both latent here

Measured in throwaway git repos with the real `check_links`:

- **A link to the repository root itself** is now a finding:
  `[{'target': '../', 'problem': 'target is not tracked by this repository'}]`. The derived
  directory set comes from `os.path.dirname`, whose walk terminates at `""`, so `"."` is never
  in it; under `check-ignore` the same link passed. **0 of the 326** file-links in the 71
  in-scope documents have this shape today (enumerated). One line fixes it: seed `directories`
  with `"."`.
- **A link into a git submodule** is now a finding —
  `'target is not tracked by this repository — a repository is nested at vendorsub; if it is
  vendored, declare it in _VENDORED_ROOTS'` — because `ls-files` lists the gitlink path but
  not its contents. Under `check-ignore` it passed. This repo has no submodules (`.gitmodules`
  absent, 0 index entries with mode `160000`), so it is latent; it matters because the plan's
  own framing is *"whatever is vendored next"*, and the offered remedy — declaring the
  submodule in `_VENDORED_ROOTS` — would exempt a tree that CI can in fact verify.

### 11. The `qa-js.bash` Non-Goal is narrower than the document it cites

`PLAN.md`, anchor *"The linked-worktree gaps"*. `CLAUDE/QA.md`'s own row says the missing
input is absent in *"a linked worktree, **and any checkout where `npm install` has not been
run**"*. Round 10 raised this; the response framed it as a worktree gap with "its own
decision". Two sentences apart, the plan and the reference disagree about the population.

This is the owner's call and I am flagging rather than pressing it, but it is not free: in
this worktree `qa-js.bash` exits 2 at `qa-all.bash`'s line before `qa-docs.bash` is invoked at
all, so **the gate this plan exists to fix is never executed by the mandated command here** —
`qa-all.bash` produced `✗ js: eslint dev tooling for extensions/ is not set up`,
`ERROR: Missing required tools (node / extensions node_modules)`, exit 2, and no `docs:` line
at all. That is Task 4.1's mechanism, live, in front of this plan's own deliverable.

## Nits

- **The denominator over-counts on a prose mention.** A comment in `qa-all.bash` reading
  `call qa_gate_detail with a pattern` matches `[^_]qa_gate_detail ` — measured,
  `numerator=1 denominator=2`, a FAIL whose message blames the extraction regex. The header
  comment at `qa-all.bash`'s anchor *"`qa_gate_case_count` 21, `qa_gate_detail` 6"* survives
  only because it writes the name in backticks with no trailing space.
- **`has()` is key presence, not a usable value.** Measured:
  `{"vendored":{"ok":null,"unverifiable":null,"broken":null}}` passes the new precondition,
  `V_BROKEN` reads `0`, the `⚠` branch never fires, and the `✓` line reads
  `VENDORED: null verified, null unverifiable, 0 broken`. `"broken": {}` would be silent
  rather than visibly null. `(.vendored.broken | type == "array")` closes it.
- **The new `jq -e` guard discards jq's own complaint** (`>/dev/null`) where the validation 20
  lines above captures it and prints `jq said: …`. Two adjacent guards, two conventions.
- **Three blank lines** now follow the `_VENDORED_ROOTS` tuple (round 10 reported two); ruff
  does not flag them at module level under this config.
- **The `qa_gate_detail` header rewrap still breaks mid-clause**: `# printed` now sits alone
  on its own line above `` # `N container-watch file(s) clean` ``. The break moved rather than
  closed.
- **`test_a_target_above_the_repo_root_is_not_read_as_inside_it`** asserts only
  `len(findings) == 1`. Its fixture target does not exist, so the new `rel_target is None`
  branch is not distinguished from the existence branch; an escaping link that *exists* is
  untested (it is now a finding, where it previously passed).
- **Success criterion *"The docs gate passes in a checkout with no hooks daemon installed"* is
  unticked** although it is demonstrated twice over at this commit — here, and in CI run
  `35079565283`.
- **Success criterion 1 names a specific run as "the most recent"** (`35076071578`,
  `7e85ff49`). `F44` has moved past it — runs `35079565283` (this commit), `35079918786` and
  `35080409646` all succeeded since. The criterion should name the condition, not freeze a
  measurement as a law; this is the same class the plan documents at length.

## Checked and clean — what I verified, and how

1. **The exit-code invariant, on two machines at this commit.** CI run `35079565283`
   (`headSha e6ec507a`, conclusion `success`, jobs `qa-all.bash` and `gitleaks secret scan`
   both success) printed `✓ docs: 71 files OK … VENDORED: 0 verified, 8 unverifiable (repo
   absent), 0 broken` and `✓ QA passed: 920 files checked`. This worktree, daemon also absent,
   prints the identical docs line at exit 0. The commit message records the host, daemon
   present, printing `8 verified, 0 unverifiable, 0 broken`. Same verdict, different sentence
   — the invariant demonstrated rather than argued.
2. **No vendored outcome can reach `findings`.** All three arms of the vendored branch end in
   `continue`; measured on fixtures: a link to the root with the tree absent → `unverifiable`
   1, findings `[]`; with the tree present → `ok` 1, findings `[]`.
3. **The whole population, enumerated rather than sampled.** Across all **71** in-scope
   documents: **326** file-links and 64 anchor-only links; **8** reach the vendored branch,
   **0** escape the repo root, **0** resolve to a non-existent target, **0** are untracked,
   and exactly **1** is a directory target (`README.md`, anchor `docs/`) — so the derived
   directory set is load-bearing for one link today.
4. **Round 10 finding 6 (the root itself), both directions.** `vendored_root_for("roles/vendor")`
   → `roles/vendor/`; `vendored_root_for("roles/vendor/x")` → `roles/vendor/`;
   `vendored_root_for("roles/vendor-extra/x.md")` → `None`; `vendored_root_for("roles/vendorx")`
   → `None`. End to end, a sibling under `roles/vendor-extra/` is a finding with vendored
   counts `{ok: 0, unverifiable: 0, broken: []}`. **Ordering**: the vendored branch runs before
   the existence check, so a link to the root is `unverifiable` in CI and `ok` on a machine
   with the tree — never `target does not exist`.
5. **Round 10 finding 3 (the vendored precondition) fires.** Measured on four payloads: no
   `vendored` key → REFUSE; `broken` key missing → REFUSE; all three present → PASS. It runs
   before the reshape and before any stage line is composed, and it cannot be reached by the
   crash paths above it.
6. **Round 10 finding 2 (the dead citation) is closed and accurate everywhere.** The deleted
   function is named only in prose that narrates its deletion (`link_check.py`, `FINDINGS.md`)
   and in the review/journal artefacts. The replacement statement — *nothing checks that a
   declared root really is a vendored repository* — is present in `helpers/docs/link_check.py`,
   `CLAUDE/QA.md` and `FINDINGS.md`, and the opposite direction it claims *is* delivered:
   measured, a link into an undeclared nested repository produces
   `'… a repository is nested at vendorsub; if it is vendored, declare it in _VENDORED_ROOTS'`.
   The new `test_every_vendored_root_is_also_excluded_from_the_scan` runs and passes, and the
   comment citing it names it correctly.
7. **Round 10 findings 4 and 8 (the numbers), re-derived from the code, not read from the
   table.** `qa_gate_case_count` call sites in `qa-all.bash`: **21**. `qa_gate_detail` call
   sites: **8**, over **6** distinct capture variables. `helper_counts_summary`: **1**.
   `printf '✓ …'` lines in `qa-all.bash`: **29**, of which one is the final `✓ QA passed`
   summary, leaving **28** composed stage lines = 21 + 6 + 1. `deployed-drift` composes its
   own inside its own script. `CLAUDE/QA.md`'s two tables document **36** gates and
   `link_check.qa_gates` parses **36** from `qa-all.bash`, with **0** in either direction
   unmatched; 36 − 7 merged = **29** hard gates. The FINDINGS table now parses into five
   clean three-cell rows summing to 29 with `**total**` stated. Every figure checks out.
   The retraction's precision also checks out: `check_extension_compat`'s capture is **7**
   lines, the old `^All [0-9]+ extension` reader matched exactly **1** of them, and the only
   difference between old and new output is the trailing full stop — so "two were broken, one
   lost a full stop" is exact.
8. **`tracked_paths` raises rather than returning an empty set** — confirmed, and the message
   now says "tracks" rather than "owns". `check=False` is annotated and the returncode is
   inspected on the next line. `-z` is the right output mode: the previous `splitlines()` read
   would have mangled a path needing quoting.
9. **`.vendored` now survives into the published JSON.** `qa-docs.bash`'s reshape carries
   `"vendored": .vendored` and `qa-all.bash`'s merge maps the whole object to `.checks.docs`,
   so `jq '.checks.docs.vendored'` answers. Round 10's nit closed.
10. **`qa_gate_case_count` still sees exactly one candidate line in the grown capture.**
    Measured on the live suite output: **1 of 1** lines match `passed:[[:space:]]+[0-9]+`, and
    the reader answers `passed: 60`, matching CI's stage line byte for byte. The COVERAGE and
    `version-pins` lines the suite now embeds carry no second `passed: <digits>`.
11. **Fail-fast**: no `failed_when`, `ignore_errors`, `|| true` or `2>/dev/null` in any changed
    file. `qa-docs.bash` keeps `set -euo pipefail`; the two `subprocess.run(..., check=False)`
    call sites inspect the returncode on the next line.
12. **Stderr hygiene**: `link_check.py` puts only its JSON payload on stdout. `qa-helper-tests.bash`
    writes **0 bytes** to stdout and 2,655 to stderr — the emptiness precondition
    `qa-all.bash` requires still holds with the grown suite.
13. **IaC placement**: no playbook, no `files/`, no `files/var/local/claude-yolo/` change, so
    **no CCY version bump is required and none is present**. No Ansible was run.
14. **Plan hygiene**: plan and code in one commit; the journal is **77 added / 0 deleted**
    (append-only); the `CLAUDE/Plan/README.md` index row is present; no untracked plan
    directory; `Status: In Progress` with Task 5.3 correctly left `⬜`. `PLAN.md` Task 2.2's
    "26 cases" now matches `TestVendoredLinkTargets` (21) + `TestTheUndeclaredRepositoryHint`
    (5) exactly, closing round 10's uncounted five.
15. **Lint and modes**: `ruff check` clean on both Python files against the pinned `0.16.4`;
    `shellcheck -x` clean on all four bash files; git-recorded modes correct (`qa-all`,
    `qa-docs`, `test-qa-helper-summary` 100755, the library and both Python files 100644).
16. **Naming**: `tracked_paths`, `vendored_root_for`, `nested_repository_for`, `repo_relative`
    each say what they do and return what they say. `verified`/`unverifiable`/`broken` name
    three states a reader acts on differently. No jargon.

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `./scripts/qa-all.bash` | **exit 2 — cannot complete in a linked worktree.** `ansible-syntax` fails on the absent vault password file, then `qa-js.bash` exits 2 on an absent `extensions/node_modules` and aborts **before `qa-docs.bash` is invoked** and before all 29 hard gates. Both gaps are declared in `CLAUDE/QA.md`'s machine-dependence table; neither is caused by this change. Stages that ran: `✓ bash: 262 files OK`, `✓ python: 150 files OK`, `✓ patterns: 262 files OK`, `✓ ansible: … 80 playbook(s) …` |
| CI at this commit | **run `35079565283`, `headSha e6ec507a`, conclusion `success`** — `✓ docs: 71 files OK … VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken`; `✓ helper-tests: Ran 1511 tests in 65 modules (65 tracked), 2 skipped`; `✓ helper-counts-reader: passed: 60`; `✓ QA passed: 920 files checked`. This is the authority the plan is about, green on the commit under review |
| `scripts/qa-docs.bash` (individually) | **exit 0** — docs line identical to CI's, byte for byte |
| `scripts/test-qa-helper-summary.bash` | exit 0, `passed: 60 failed: 0`, `PASS COVERAGE: 8 of 8`, all 8 coupling cases PASS against their real gates |
| `scripts/qa-helper-tests.bash` (conditional — `helpers/` and `tests/helpers/` both changed, so **triggered**) | exit 0, `COVERAGE: 65 of 65 tracked helper test modules`, `Ran 1511 tests … OK (skipped=1)`, stdout 0 bytes |
| `python3 -m unittest tests.helpers.docs.test_link_check` | exit 0, `Ran 79 tests … OK` |
| `ruff check` (pin `0.16.4`, asserted) | clean on both changed Python files |
| `shellcheck -x` | clean on all four changed bash files |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook changed. It could not run here regardless (no vault password file) |
| `python3 -m helpers.gnome.check_extension_compat` | **not triggered** (no `extensions/` metadata change); run anyway as a probe for the retraction measurement — exit 0 |
| `cd extensions && node_modules/.bin/eslint .` | **not triggered** (no extension JS change); not runnable here (no `node_modules`) |
| `hooks-daemon plan-qa --sweep` | **NOT RUN.** The daemon tree is gitignored and absent from this worktree, and this review was confined to the worktree. Saying so rather than omitting it |

## Does Task 5.3 discharge?

**Not yet — but the remaining list is shorter than last round and contains nothing about the
design.** The mechanism swap is the right question finally asked, the existence-first ordering
is correct and correctly reasoned, and for the first time the invariant is demonstrated on two
machines at the reviewed commit instead of argued from the code. Round 10's findings 2, 3, 4,
6, 7 and 8 are genuinely closed and I re-derived every number rather than reading the table.

What is left is, again, this plan's own classes inside the artefacts that close it:

1. The COVERAGE guard reports PASS when it is blind (finding 1) — this is the one that
   matters, because it is a regression this commit introduced into a guard it was adding.
2. The widening is half-applied and untested, and the denominator is neither independent nor
   as loose as its comment claims (findings 2 and 3).
3. `COVERAGE: n of m` never reaches a human (finding 4).
4. Two stale counts (finding 5) and three stale statements of the old ignore question
   (findings 6 and 7).

Then the should-fixes: name or measure the scan population (8), assert the exit-2 chain and
fix the test docstring that still describes `check-ignore` (9), and decide on the two
reclassified target shapes (10). Finding 11 and the nits are the owner's calls.

The worktree did its job for a third round: the pin held at both checks, so every citation
above was measured against the same bytes it names.

## Review conditions

- Target: `e6ec507a`, in the dedicated linked worktree on `worktree-plan-00125-review`.
- `git status --porcelain` empty at the first action and at the last; `HEAD` unmoved.
- Read-only on repository files. Every fixture — non-git tree, submodule, repo-root link,
  present-untracked target, untracked in-scope document, five call-site spellings, four jq
  payloads — was built in a `tempfile` temp directory and removed. Nothing was written inside
  the repository except this report.
- Container: CCY — no Ansible run, no deploy.

---

# Addendum — re-checked against `f3a4f301`, `5d38cc83` and `01850f04`

Three commits landed after the snapshot above. I materialised `01850f04` into a temp
directory (`git archive` + `git init` + `git add -A` + commit, so every file is tracked —
the clean-checkout condition) and re-ran every probe there. **Nothing below moved the pinned
worktree**; the snapshot this report reviews is still `e6ec507a`.

At the tip: `python3 -m unittest tests.helpers.docs.test_link_check` → `Ran 86 tests … OK`,
and `scripts/qa-docs.bash` in the synthesised clean checkout → `✓ docs: 71 files OK … —
VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken`, exit 0.

## Closed by the later commits — withdraw these from the list above

- **Finding 1 (the COVERAGE guard reports PASS when blind)** — closed by `f3a4f301`.
  Re-run against the tip's own guard block with a non-existent `qa-all.bash`:
  `FAIL  no qa_gate_detail call sites found …`, `RESULT passed=0 failed=1 calls=[0]`. The
  `detail_calls=0` seed plus the explicit `if !` fallback makes the denominator an integer on
  every path, so the fall-through is gone.
- **Finding 3 (the denominator is not loose enough, and one spelling is silent)** — closed by
  `f3a4f301`. Measured at the tip: a **column-1** call site now gives `PASS COVERAGE: 1 of 1`
  (was `numerator=1 denominator=0`), and the **tab** spelling now gives
  `FAIL extracted 0 … but it has 1` (was `0 and 0`, agreeing silently). `[[:space:]]` in place
  of the literal space is what closes the tab case, and `(^|[^_[:alnum:]])` the column-1 case.
- **Finding 10, first half (a link to the repository root)** — closed by `5d38cc83`.
  Measured at the tip: `[the repo](../)` → `findings == []`. `directories = {"."}` is the
  right seed, and the RED-first test is real.
- **Round 10's open nit, the unreachable `V_BROKEN -gt 0` branch** — closed by `01850f04`.

## On removing the `V_BROKEN` branch — the right call, and for a better reason than "it never ran"

Two things make it right rather than merely tidier. The composition now executes on **every**
run against an empty list, so a shape error — a renamed key, a changed entry field — surfaces
on the next QA run instead of on the day a vendored pointer goes stale, which is the one day
nobody wants to meet a formatter for the first time. And the precondition was extended to
`has("vendored_warning")`, so a checker that stopped emitting the key is a hard failure rather
than a quiet skip; measured, a payload without the key is REFUSED, and even if it slipped past,
`jq -r '.vendored_warning[]'` exits 5 under `set -e` rather than printing nothing.

I drove the real `vendored_warning_lines()` output through the gate's real
`jq -r '.vendored_warning[]'` and the real `verdicts.parse`:

    ⚠ docs: 2 link(s) into a PRESENT vendored repo are broken — it has probably moved the file:
        .claude/rules/agent-docs.md:1  ../hooks-daemon/CLAUDE/DirectoryRoles.md
        .claude/rules/plan-dir.md:4  ../hooks-daemon/CLAUDE/PlanDir.md
    ✓ docs: 71 files OK … — VENDORED: 0 verified, 0 unverifiable (repo absent), 2 broken

    stages: {'docs': [Verdict(symbol='⚠', …), Verdict(symbol='✓', …)]}

Both stages survive and the indented detail lines are correctly not read as stages.

**One thing to fix in it**, and it is this plan's own class: the test that guards that
invariant cites the authority and then copies it. `TestTheVendoredWarningBlock`'s docstring
for `test_the_detail_lines_are_indented_so_they_are_not_read_as_stages` says *"`verdicts.STAGE`
anchors its symbol at column 0"*, but the assertion is a hand-written `r"^\s*[✓✗⚠] "` and the
test module imports only `link_check` — it never imports `verdicts`. A pattern and the thing
it describes that nothing compares will drift, which is exactly the `nokill` argument. Parsing
the composed block with the real `verdicts.parse` and asserting it yields **one** stage closes
it in one line, and would also cover `SYMBOL_BEARING` — the census denominator, which the copy
does not describe at all.

## Still open at `01850f04`

Re-measured at the tip, not carried forward on trust:

- **Finding 2 (the extraction was widened, the parser was not)** — open, and now slightly
  worse. Both newly-accepted spellings raise the numerator and reach the loop:
  `"${nokill_out}"` → `PASS COVERAGE: 1 of 1`, then `site_var=[{nokill_out}]` and a FAIL saying
  *no gate command is registered for it*; `"$nokill_out" "pattern"` → `PASS COVERAGE: 1 of 1`,
  then the whole call-site text is used as the pattern and it FAILs as a drifted pattern. So
  **`COVERAGE: n of n` now counts call sites as extracted that the checker cannot use** — it
  measures extraction, not checking, and the header's claim is about checking.
- **Finding 4** — `COVERAGE: n of m` still goes only to the suite's captured stdout.
- **Finding 5** — `FINDINGS.md`'s *"The suite has 1,482 tests"* and `PLAN.md` Task 4.4's
  *"(51 cases)"* both still present. The first has moved further: this addendum's tip adds 7
  more `link_check` tests (79 → 86) on top of the 1,511 I measured.
- **Finding 6** — all three statements of the removed ignore question still present: the table
  rows in `CLAUDE/QA.md` and `FINDINGS.md`, and `check_links`'s own docstring anchor *"A target
  this repo ignores but nobody vendored is a FINDING"*.
- **Finding 7** — `qa-docs.bash`'s *"`// \"?\"` rather than `// 0`"* comment still annotates
  code that no longer uses it, now above a precondition that has grown a fourth key.
- **Finding 8** — `collect_scope` still walks the filesystem; the docs stage line still has no
  tracked denominator.
- **Finding 9** — the raise test's docstring still opens *"No git checkout means the ignore
  question has no answer"*, and the exit-2 chain is still unasserted.
- **Finding 10, second half** — a link into a git submodule is still a finding at the tip
  (measured with a real submodule). Latent: no submodules in this repo.
- **Finding 11** and the nits are unchanged.

## Two residues in the repaired denominator (new, minor, both loud)

- A **trailing** comment mentioning the function on a code line still over-counts: measured,
  `extracted 1 … but it has 2`. `grep -vE '^[[:space:]]*#'` drops only whole-line comments.
- **Two calls on one line** still disagree: `extracted 2 … but it has 1`, because `grep -c`
  counts lines and `grep -o` counts matches.

Neither is silent, so neither is the class this plan is about — but both FAIL with the message
*"the extraction regex does not cover every spelling"*, which in these two cases is false. A
reader would go and widen a regex that is already correct.

**Verdict unchanged: FIX-BEFORE-MERGE**, on a shorter list. Findings 2 and 5–9 are what remain.

---

# Addendum 2 — re-checked against `f500f2d2`

Same method as addendum 1: `f500f2d2` materialised into a temp directory (`git archive` +
`git init` + `git add -A` + commit, so every file is tracked — the clean-checkout condition),
every probe re-run there, mutations applied to the extraction and never to the repository.
**The pinned worktree did not move.**

`f500f2d2` is green in CI — run `35084809430`, and its lines confirm every number quoted to
me: `✓ docs: 71 files (71 tracked) OK … — VENDORED: 0 verified, 8 unverifiable (repo absent),
0 broken`; `✓ helper-tests: Ran 1545 tests in 66 modules (66 tracked), 2 skipped`;
`✓ helper-counts-reader: passed: 66`; `✓ QA passed: 922 files checked`.

## The parser is the right answer, and I could not break the part that matters

`helpers/qa_environment/gate_call_sites.py` replaces two counts that had to agree with one
that reports what it could not read. I ran fifteen shapes through the real `call_sites()`:

    plain / braces / tab / column-0 / double-quoted pattern   all 1 site, correct var AND pattern
    two calls on one line                                     2 sites, 0 unparsed
    trailing comment mention / whole-line comment mention     0 sites, 0 unparsed
    bare-word arg / line continuation                         0 sites, 1 unparsed, line named
    `_qa_gate_detail` / `qa_gate_detail_v2`                    0 sites, 0 unparsed (correct)
    `#` inside the pattern                                    1 site, pattern intact

Findings 2 and 3 and both denominator residues from addendum 1 are genuinely closed, and the
widening is verified per spelling for the first time — the thing the regex version never did.
Parsing rather than counting is the structural fix, not a fourth grep.

## Fix before merge

### A. The reverse-direction check enumerates a hand-written array beside the thing it checks

`scripts/test-qa-helper-summary.bash`, anchors `GATE_COMMAND_VARS=(` and `gate_command_for()`.

The forward direction is derived: the parser finds the call sites. The reverse direction is
**a second, hand-maintained copy** of `gate_command_for`'s case arms, and it can only report
what somebody remembered to write twice. Measured — one case arm added to the extracted copy,
the array untouched:

    added an unreferenced case arm 'orphan_out'
    suite exit=0
    passed: 66 failed: 0

An orphaned registration is exactly the state the reverse check was added to catch — *"a
registration no call site reads"* — and it is invisible. This is the pattern this plan has
removed four times already, and `CLAUDE/AgentNotes.md` states the rule: replacing a stale
enumeration with a fresher enumeration is not the fix; deriving the set is.

**Fix**: one source of truth. `declare -A GATE_COMMAND=([nokill_out]="bash scripts/…" …)`,
look up with `${GATE_COMMAND[$var]+set}`, and iterate `"${!GATE_COMMAND[@]}"` for the reverse
pass. The `case` and the array then cannot disagree because there is only one of them.

## Should fix

### B. The COVERAGE branch still falls through to PASS when the counts are not numbers

Same file, anchor `site_count="$(printf '%s' "$sites_json" | jq '.sites | length')"`.

If the parser cannot run, `sites_json` is empty, both `jq` calls produce nothing, and both
`[` tests abort on a non-integer — the same fall-through addendum 1 found in the grep
version. Measured by making the module unavailable:

    /usr/bin/python3: No module named helpers.qa_environment.gate_call_sites
      PASS  COVERAGE:  qa_gate_detail call site(s) parsed, 0 unparsed
      FAIL  gate_command_for registers $nokill_out, but no qa_gate_detail call site … reads it
      … (six of these)
    passed: 52 failed: 6   suite exit=1

So the **run** fails, which is the important part — but it fails by the reverse check, and
this branch prints a PASS with an empty number. The rescue is incidental: it depends on
`GATE_COMMAND_VARS` being non-empty, and finding A shows that array is hand-maintained.
`jq -e '(.sites | type == "array") and (.unparsed | type == "array")'` on `sites_json` before
reading the two lengths would make this branch answer for itself.

### C. `.tracked` is the one number in the docs stage line with no guard

`scripts/qa-docs.bash`, anchor `TRACKED=$(jq -r '.tracked' "$TMP_RAW")`.

The payload validation checks `has("findings") and has("scanned")`; the new typed guard checks
`.vendored.ok`, `.vendored.unverifiable`, `.vendored.broken` and `.vendored_warning`. Neither
covers `.tracked`. Measured — `"tracked"` removed from `link_check`'s payload:

    ✓ docs: 71 files (null tracked) OK (links, anchors, playbook catalogue, topic index) — …
    qa-docs exit=0

It degrades visibly rather than silently, so it is not the blind class — but it is the number
added *because* an unstated denominator reads as clean, and it is now the only field in that
line nothing checks. The guard immediately above it says so itself: *"Two adjacent guards with
two conventions is how one of them ends up the weaker one."* One clause:
`and (.tracked | type == "number")`.

### D. A wrongly-parsed call site is a third state the parser's docstring says cannot exist

`helpers/qa_environment/gate_call_sites.py`, anchor `_ARGS = re.compile(`.

The pattern group is `(?P<pattern>.*?)(?P=quote)`, non-greedy, with no escape handling.
Measured:

    qa_gate_detail "$a_out" "say \"hi\" now"   ->  sites=1  unparsed=0  pattern='say \'

The module header states the invariant as *"A silently dropped call site is not a state this
function can reach: it either parses an occurrence or reports it."* This is a third state —
parsed, wrongly, reported as clean. Downstream it becomes *"the pattern qa-all.bash applies to
`$a_out` no longer matches that gate's output"*, a true failure with a false diagnosis, which
is one of the four defects the commit message lists as its reason for existing. And if a
truncated pattern happened to still match, the site would PASS while the pattern checked is
not the pattern `qa-all.bash` applies.

Latent — none of the six live patterns contains a quote. **Fix**: handle `\"` inside a
double-quoted pattern, or refuse a pattern whose closing quote is preceded by a backslash and
return it as `unparsed`, which is the honest answer for something the parser cannot read.

### E. The name inside a string or a heredoc is reported as an unparsed call site

Same file, anchor `def strip_comment`.

`strip_comment` is quote-aware, which is right and closes the comment case in both directions.
But a string is not a comment, and the occurrence scan runs on what survives. Measured:

    echo "use qa_gate_detail here"                 -> sites=0 unparsed=1
    a two-line string mentioning it on line 2      -> sites=0 unparsed=1 (line 2)
    a heredoc body mentioning it                   -> sites=0 unparsed=1

Each fails the suite with *"N qa_gate_detail call site(s) in qa-all.bash did not parse —
unchecked, not absent"*. `qa-all.bash` has none today (0 unparsed, measured), so this is
latent; it is worth naming because it is the same misdiagnosis class the comment stripping was
added to remove, one construct over. The cheap mitigation is a sentence in the header saying
the parser reads comments but not string context, so the next reader meeting the message knows
where to look.

## Nits

- **The control cited for the ⚠-block assertion is not the mutation that breaks it.** Measured
  against `test_the_block_is_one_stage_to_the_real_verdict_parser`:

      as composed                  stages=['docs'] symbols=['⚠','✓']          ASSERTION PASSES
      MUTATION: indent stripped    stages=['docs'] symbols=['⚠','✓']          ASSERTION PASSES
      MUTATION: detail leads ⚠     stages=['docs'] symbols=['⚠','⚠','⚠','✓']  ASSERTION FAILS

  The assertion is sound and does discriminate — against a detail line that *begins with a
  stage symbol*, which is the real hazard. Removing the indent does not break it, because a
  detail line has no symbol either way. Two consequences worth a line each: the reported
  control demonstrates something else, and the previous version's direct
  `assertTrue(detail.startswith("    "))` was dropped, so the indentation the docstring names
  is now asserted by nothing. It is still load-bearing — `SYMBOL_BEARING` requires the symbol
  at line start — so a one-line `startswith` alongside the parser assertion covers both.
- **`PLAN.md`'s "Delivery & Milestones" is a frozen list.** Anchor *"Remaining: Task 2.1
  (decision), 2.2 (its implementation), 4.3 (decision), 5.2 (follows 2.2), 5.3"*. Tasks 2.1,
  2.2 and 5.2 are all `[x] ✅` thirty lines above it. 4.3 and 5.3 are correctly still open.

## The two questions you asked me

**Finding 4 — is the reasoning right?** The recursion is real: registering this suite's own
capture variable in `gate_command_for` would make `capture_for` run
`test-qa-helper-summary.bash` from inside itself, so routing it through `qa_gate_detail` is
genuinely impossible. Carrying the property on an exit code instead is the right substitute in
principle, and the reverse direction is the right shape for it.

Two corrections. First, *"cannot ride in the stage line"* is stronger than what was shown:
what cannot be done is routing it through `qa_gate_detail`. `qa-all.bash` already captures this
suite's whole output, so it could read a second marker line out of that capture and append it
to the stage line with no registration and no recursion. I am not asking for that — the exit
code is the better mechanism and the marker would be one more thing to keep in step — but the
sentence should say "not through `qa_gate_detail`" rather than "cannot". Second, as built the
exit-code substitute is weaker than the argument needs: findings A and B are both in it. Fix
those two and the reasoning holds.

**Finding 9 — does the exit-2 chain need the seam?** Yes, and I see you have already started:
the working tree's `scripts/qa-docs.bash` has grown `REPO_ROOT="${1:-…}"` and names
`scripts/test-qa-docs-exit-codes.bash`, neither of which is in `f500f2d2`. The reason is not
tidiness. That script documents three exit codes; two of them are reachable today only by
argument, because each needs a tree this repository is not, and the `2` in particular arrives
through two hops — Python exits 1, then the `jq -e` validation refuses the traceback — that no
committed test touches. I measured the chain end to end twice and it works; measuring it in a
review is not the same as a gate that would notice it stopping. The same seam is what makes
the zero-file guard and the crash path testable, and those are the three branches whose whole
job is to refuse a pass.

## Closed since addendum 1 — re-verified, not taken on trust

- **2, 3 and both denominator residues** — the fifteen-shape table above.
- **5** — `1,482 tests` and `(51 cases)` both gone from `FINDINGS.md` and `PLAN.md`.
- **6** — both headline tables now read `| untracked, and vendored by nobody |`, and
  `check_links`' docstring no longer asks the ignore question.
- **7** — the `// "?"` paragraph is gone; the block now argues for the typed guard it sits on.
- **8** — control reproduced: an uncommitted in-scope document gives
  `✓ docs: 72 files (71 tracked) OK`, against `71 files (71 tracked)` clean. Not excluding
  untracked documents is the right call — a broken link in an uncommitted file is a true
  finding, and the denominator is what makes the two machines comparable.
- **9, docstring half** — the raise test no longer describes `check-ignore`.
- **10, second half** — the submodule distinction is written down where the hint would send
  someone wrong, with the measurement (`.gitmodules` absent, no mode-160000 entries) beside it.
- **11** — the Non-Goal now names the same population as `CLAUDE/QA.md`: a worktree *and any
  checkout where `npm install` has not been run*.
- **My finding inside `01850f04`** — the test imports `verdicts` and parses with
  `verdicts.parse`; see the nit above for what its control actually proves.
- **The nits** — three decoy payloads (`null`, `{}`, missing key) all REFUSED and the real one
  accepted; jq's complaint captured and printed; blank lines back to two; the mid-clause
  comment break closed; `test_an_escaping_target_that_EXISTS_is_still_a_finding` added beside
  the absent case; criterion 1 restated as a condition; criterion 4 ticked against CI run
  `35081847136`, which I confirmed is real, green and on `338fa35b`.

## Task 4.3's finding — checked independently, and it holds

`qa-all.bash` contains **32** `✗ QA FAILED:` lines; excluding the final run summary
(`✗ QA FAILED: $NERRORS errors in $TOTAL files`) leaves **31** gate aborts. I parsed all 32
with the real `verdicts.parse`: **32 of 32 produce no stage at all**, because `RUN_SUMMARY`
matches `^[✓✗⚠] QA (?:passed|FAILED):` before `STAGE` can. So a failing gate contributes no
stage line and is indistinguishable from one that never ran. `FINDINGS.md` states the
reconciliation itself — *"32 `exit 1` sites, less the final summary; `helper-tests` owns three
of them, which is how 31 lines cover 29 gates"* — and it is exact. Option (2) is correctly
ruled out.

## Mechanical gates at `f500f2d2`

| Gate | Result |
| ---- | ------ |
| CI | **run `35084809430`, `headSha f500f2d2`, success** — the four stage lines quoted above |
| `python3 -m unittest tests.helpers.docs.test_link_check` | exit 0, `Ran 90 tests … OK` |
| `python3 -m unittest tests.helpers.qa_environment.test_gate_call_sites` | exit 0, `Ran 23 tests … OK` |
| `scripts/qa-docs.bash` in a synthesised clean checkout | exit 0, `✓ docs: 71 files (71 tracked) OK … 0 verified, 8 unverifiable, 0 broken` |
| `scripts/test-qa-helper-summary.bash` | exit 0, `passed: 66 failed: 0`, `PASS COVERAGE: 8 qa_gate_detail call site(s) parsed, 0 unparsed` |
| `ruff check` (pin `0.16.4`) | clean on all four Python files |
| `shellcheck -x` | clean on both changed bash files |
| git modes | `gate_call_sites.py` and its test 100644; `qa-docs.bash` and `test-qa-helper-summary.bash` 100755 — correct |
| public-repo scan | **0 hits** across all 727 added lines, on eleven patterns |

**Verdict: FIX-BEFORE-MERGE, and A is the only one I would hold for.** B through E and the two
nits are real and measured, but none of them breaks a user, and every one is latent today. A is
a hand-written enumeration standing in for a derived one inside the check written to catch a
stale registration, and it costs one `declare -A` to remove.

---

# Addendum 3 — final, `2b84ff8f`, scoped to blocking correctness only

Scope as instructed: only a finding that makes a gate return a wrong verdict, hide a failure,
or claim something false about what it checked. `2b84ff8f` materialised into a temp clean
checkout; the pinned worktree did not move. CI run `35085997269` on this commit was still in
progress while I worked; `f500f2d2` before it is green.

**One finding meets that bar.** It is one word, in the new gate, and I would not hold the plan
for it.

## The finding

### Case 1 of `test-qa-docs-exit-codes.bash` can pass for the wrong reason

`scripts/test-qa-docs-exit-codes.bash`, anchor
`check_exit "a tree git cannot answer for" 2 "$GATE_RC" "link_check"`.

`check_exit`'s own comment states the property: *"Two different faults share exit 2, and a gate
that returned the right number for the wrong reason would pass a check that only read it."*
The needle for case 1 is the bare string `link_check`, and **three** of `qa-docs.bash`'s exit-2
branches contain it — the crash branch, the payload-validation branch, and
`✗ docs: helpers/docs/link_check.py is missing — broken checkout`.

Measured: case 1's fixture built exactly as the script builds it, but with the `helpers`
symlink pointing at a path that does not exist —

    exit=2
    output: ✗ docs: helpers/docs/link_check.py is missing — broken checkout
    >>> case 1's needle 'link_check' MATCHES -> the case would PASS

So the case can report `PASS  a tree git cannot answer for -> exit 2` while the raise-and-refuse
chain — the two hops the whole file says are the reason it exists — was never reached. That is
the case claiming something false about what it exercised, which is the one thing the needle
mechanism was added to prevent.

**Fix, one word**: use a needle unique to the branch under test —
`did not emit the expected JSON`. Nothing else prints it.

## Why I am confident about the rest of the new gate

- **It discriminates against the mutation it exists for.** I deleted `qa-docs.bash`'s
  `has("findings") and has("scanned")` validation and re-ran: `FAIL a tree git cannot answer
  for — expected exit 2, got 4`. The two-hop chain is now genuinely asserted rather than
  reasoned about, which closes finding 9.
- **It discriminates against a gate that returns 2 for everything.** With `exit 2` inserted at
  the top of `qa-docs.bash`: `passed: 0 failed: 4` — all four cases fail, including the two
  that *expect* 2, because the needles find no message. The needle mechanism does real work;
  case 1's needle is simply the wrong string.
- The fixtures are outside the repository (`mktemp -d`) with a cleanup trap, symlink the real
  `helpers/` rather than copying it, and `os.walk` does not follow that symlink, so the real
  `helpers/CLAUDE.md` is not dragged into a fixture's scan. `run_gate` uses globals with the
  subshell hazard called out. Mode `100755`, shellcheck clean.
- Gate inventory, derived and checked both ways at this commit: **37 run, 37 documented, 0
  run-not-documented, 0 documented-not-run.** The new row is real.
- Suites at this commit: `link_check` 90 tests OK, `gate_call_sites` 23 tests OK, coupling
  suite `passed: 66 failed: 0`, `test-qa-docs-exit-codes.bash` 4/4,
  `✓ docs: 71 files (71 tracked) OK … 0 verified, 8 unverifiable, 0 broken` in a synthesised
  clean checkout.

## Everything else I have open is outside the cap, and I am not re-raising it

Stated once so the record is complete, not as findings:

- The `GATE_COMMAND_VARS` array (addendum 2, finding A) is unchanged at `2b84ff8f`. It is a
  latent gap: all six registrations are listed today, so no verdict is wrong and no output
  claims otherwise. Outside the cap.
- The COVERAGE branch printing `PASS` with an empty count when the parser cannot run
  (addendum 2, finding B) — the run still fails, loudly, by the reverse check. Outside the cap.
- `.tracked` being untyped (C) and the escaped-quote truncation (D) are **already fixed in the
  working tree** — I can see `(.tracked | type == "number")` in `qa-docs.bash`'s guard and
  `(?<!\\)(?P=quote)` in `gate_call_sites.py`, with a test asserting the escaped quote is
  reported rather than truncated. Neither is in `2b84ff8f`; both will land with the next commit.
- Prose, comment wording and counts: I checked `CLAUDE/QA.md`'s "thirty-six gates" /
  "twenty-nine" / "37 stage names" prose against the 37 gates now in the inventory and it has
  not moved. Excluded by the cap, recorded here in one line so that the check I ran is visible.

## Verdict

**PASS**, with the one-word correction above recorded rather than held for.

Nothing in the production path returns a wrong verdict, hides a failure, or claims something
it did not check. The docs gate reaches the same exit code with and without the daemon and
says which; its three documented exit codes are now produced by running it; the call-site
population is parsed rather than counted twice; every number in its stage line is checked
before it is printed. The plan's two causes are fixed and demonstrated on both machines.

---

# Confirmation — addendum 2's list at `089a7127`

Confirmation pass, not a review: each claimed fix driven at `089a7127` in a temp clean
checkout, nothing else looked for. The pinned worktree did not move.

| Item | Claim | Measured | Verdict |
| ---- | ----- | -------- | ------- |
| **A** | one `declare -A GATE_COMMAND`, iterated for the reverse pass | one list only — `gate_command_for` reads `${GATE_COMMAND[$1]+set}`, the reverse loop iterates `"${!GATE_COMMAND[@]}"` sorted; no second array anywhere. My exact mutation (`[orphan_out]="true"`, read by no call site) now gives `FAIL gate_command_for registers $orphan_out, but no qa_gate_detail call site in qa-all.bash reads it`, `passed: 66 failed: 1`, exit 1 | **PASS** |
| **B** | `sites_json` type-checked before either length is read | module moved aside → `FAIL the call-site parser produced no usable output — nothing below checked anything`, `passed: 51 failed: 8`, exit 1, and **zero** `PASS COVERAGE` lines. The branch answers for itself; the reverse check is no longer the rescue | **PASS** |
| **C** | `.tracked` in the typed guard | present as the first clause. `"tracked"` removed from the payload → `✗ docs: link_check did not emit usable vendored counts … jq said: false`, **exit 2**, where it was `(null tracked)` at exit 0 | **PASS** |
| **D** | escaped quote refused, not truncated | `"say \"hi\" now"` → 0 sites, 1 unparsed; `'it\'s here'` → 0 sites, 1 unparsed. Three controls unaffected, including `"[0-9]+\s+clean"` — a backslash not before a quote still parses, so the refusal is not over-broad | **PASS** |
| **E** | limit stated in the module header | header says it tracks quoting well enough to tell a comment from a `#` in a string but does **not** track string or heredoc context, so an occurrence inside one is reported as unparsed | **PASS** |
| **Nit 1** | direct indent assertion restored, docstring corrected | `assertTrue(detail.startswith("    "))` back alongside the `verdicts.parse` assertion, and the docstring records that stripping the indent leaves the parse unchanged while a detail line that BEGINS with a symbol gives `['⚠','⚠','⚠','✓']` — which is what I measured | **PASS** |
| **Nit 2** | "Remaining:" names only 4.3 and 5.3 | `Remaining: Task 4.3 (owner's decision) and Task 5.3 (qa-reviewer).` | **PASS** |

**Addendum 2's list is closed.** Health at this commit, for the record: coupling suite
`passed: 66 failed: 0` with `COVERAGE: 8 … parsed, 0 unparsed`; `link_check` 90 tests OK;
`gate_call_sites` 25 tests OK (23 + D's two); `docs-exit-codes` 4/4;
`✓ docs: 71 files (71 tracked) OK … 0 verified, 8 unverifiable, 0 broken`; ruff and
shellcheck clean. CI: `2b84ff8f` has since gone green; `089a7127` was in progress.

One status note on an item already reported rather than a new finding: addendum 3's needle
(`check_exit "a tree git cannot answer for" … "link_check"`) is unchanged at `089a7127` and
is already corrected to `did not emit the expected JSON` in the working tree, so it lands
with the next commit.
