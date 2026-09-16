# QA Review — Plan 00125, Task 5.3, round 10 (pinned worktree at `72d21ffc`)

> **Provenance**: `qa-reviewer` subagent (Opus 5), 2026-09-16, written via a Bash heredoc
> because `Write`/`Edit` are withheld from this reviewer session. Reviewed inside the
> dedicated linked worktree on branch `worktree-plan-00125-review`. No repository file was
> modified. Two gitignored scratch files were created under `untracked/` to hold a command
> capture and removed in the same command; `git status --porcelain` was empty at the first
> action and at the last.

**Pin**: `git log --oneline -1` = `72d21ffc Plan 00125: we do not QA another repo's files —
Task 2.1 decided, 2.2 done`; `git status --porcelain` empty. Checked as the first action and
again as the last. **The tree held still for the whole review.**

**Verdict**: FIX-BEFORE-MERGE

The vendored-repository boundary is sound and I confirmed its central invariant by
enumeration rather than by reading the commit message: across all **326** file-links in the
71 in-scope documents, 8 reach the vendored branch, 0 are ignored-but-unvendored, 0 escape
the root, and no vendored outcome can reach `findings`. All four of round 9's required items
are closed and I re-verified each. What stops 5.3 discharging is that the same two defect
classes this plan exists to remove are live in the artefacts that close it: a guard named in
a comment that does not exist, a coverage claim that covers one spelling out of five, a
missing-key guard applied to two counters out of three, and an enumeration table that does
not enumerate.

## Blocking

None. Nothing here breaks another user, loses data, leaks an identifier, or violates a HARD
RULE. The gate's exit code is genuinely independent of what is installed — see *Checked and
clean*, item 1.

## Fix before merge

### 1. "Every `qa_gate_detail` pattern" is one spelling out of five, and only the ZERO case is guarded

`scripts/test-qa-helper-summary.bash`, anchor `mapfile -t detail_sites`.

The extraction is `qa_gate_detail "\$[a-z_]+" '[^']+'`. I ran that exact expression against
five call-site spellings; four of them are **silently not extracted**, so the gate they read
is never run and nothing says so:

    EXTRACTED   qa_gate_detail "$foo_out" '[0-9]+ things'
    NOT MATCHED qa_gate_detail "${foo_out}" '[0-9]+ things'     <- braces
    NOT MATCHED qa_gate_detail "$fooOut" '[0-9]+ things'        <- any uppercase
    NOT MATCHED qa_gate_detail "$foo_out2" '[0-9]+ things'      <- any digit
    NOT MATCHED qa_gate_detail "$foo_out" "$FOO_PATTERN"        <- pattern in a variable
    NOT MATCHED qa_gate_detail "$foo_out" "[0-9]+ things"       <- double-quoted pattern

The suite guards `${#detail_sites[@]} -eq 0` with a comment about discovery breaking, and
says **nothing about the partial case**. That is verbatim
`CLAUDE/AgentNotes.md` → *"A partial result read as a complete one — guard the empty case,
miss the partial"*. Measured: 8 extracted of 8 call lines today, so it is latent — but there
is no `COVERAGE: n of m`, so an under-match will never appear as a failure.

Three documents state the property wider than the code delivers it:

- `scripts/lib/qa-helper-summary.bash`, anchor *"reads every pattern below OUT of
  `qa-all.bash` ... so the next one cannot be added uncoupled"*
- `CLAUDE/QA.md`, anchor *"extracts every `qa_gate_detail` pattern from `qa-all.bash`"*
- `scripts/test-qa-helper-summary.bash`, anchor *"It is also self-extending"*

The sharpest part: **the same lesson is applied correctly 250 lines away in this same
commit.** `helpers/docs/link_check.py`'s `_QA_SCRIPT_GATE` was deliberately widened to accept
`$SCRIPT_DIR/x`, `${SCRIPT_DIR}/x` and `$REPO_ROOT/scripts/x`, with the comment *"keying on
one spelling would exempt the other two from the inventory without saying so"*, and a test
named `test_the_same_gate_written_three_ways_is_one_gate`. The new extraction keys on one
spelling. `AgentNotes.md` → *"Generalise a fix past the file you were reading"*.

**Fix**: widen the variable part to `\$\{?[A-Za-z_][A-Za-z0-9_]*\}?`, accept either quote
style for the pattern, and — the part that matters more — count the `qa_gate_detail`
invocation lines in `qa-all.bash` independently, compare, fail on a mismatch, and print
`COVERAGE: n of m` on the passing path.

### 2. `_VENDORED_ROOTS` cites a guard that does not exist anywhere in the tree

`helpers/docs/link_check.py`, anchor *"What keeps the declaration honest is
`check_vendored_declaration`, which runs wherever those repos DO exist: clone one in and QA
fails on the machine you cloned it on."*

I searched every tracked `*.py`, `*.bash` and `*.md` in the repository. `check_vendored_declaration`
appears exactly once — in that comment. It is not defined in `link_check.py` (I listed every
top-level `def`), it is not called by `main()`, and no test references it.

This matters more than a stale name. The comment is the answer to the obvious objection to a
DECLARED exemption list — *what stops someone declaring a root that is not a vendored repo,
and exempting every link under it?* — and the answer is a citation to nothing. Nothing in the
tree checks that a declared root is a repository, or that it is gitignored, or that it
exists anywhere. The behaviour the sentence half-describes (clone an **undeclared** repo in
and a link into it becomes a finding) is delivered by `check_links` plus
`nested_repository_for`, and it is the opposite direction from "keeps the declaration
honest".

**Fix**: delete the sentence, or write the check it names. `AgentNotes.md` → *"A claim
printed where a measurement belongs"*.

### 3. The one vendored counter with no missing-key guard is the one whose zero silences a stage line

`scripts/qa-docs.bash`, anchors `V_OK=$(jq -r '.vendored.ok // "?"'` and
`V_BROKEN=$(jq -r '.vendored.broken | length'`.

The comment three lines above argues the rule explicitly: *"`// "?"` rather than `// 0` — a
missing key means the checker stopped emitting it, and that must not read as a clean zero."*
`V_BROKEN` has no `//` at all, and jq answers `0` for a missing key. Measured:

    $ echo '{"scanned":3,"findings":[]}' | jq -r '.vendored.ok // "?"'        -> ?
    $ echo '{"scanned":3,"findings":[]}' | jq -r '.vendored.broken | length'  -> 0
    $ echo '{"vendored":{"ok":1,"unverifiable":0}}' | jq -r '.vendored.broken | length' -> 0

So if the checker ever stops emitting `broken`, `V_BROKEN` reads 0, the
`if [[ "$V_BROKEN" -gt 0 ]]` branch never fires, the whole `⚠` stage line and its list of
links disappear, and the `✓` line asserts `0 broken`. A blind read whose output is
byte-identical to a clean one — this plan's subject — in the guard written to prevent it.
The two counters that *cannot* silence anything got the guard; the one that can did not.

**Fix**: `V_BROKEN=$(jq -r 'if has("vendored") and (.vendored|has("broken")) then
(.vendored.broken|length) else "?" end' ...)` (or an explicit `jq -e` precondition on the
three keys), and treat `?` as a hard failure rather than as text, since `[[ "?" -gt 0 ]]`
would otherwise be a bash arithmetic error rather than a stated one.

### 4. The sweep-three classification table does not enumerate 29, and contradicts the code it classifies

`CLAUDE/Plan/.../FINDINGS.md`, anchor *"How the stage line is derived"*. Read verbatim from
the file:

    | `qa_gate_case_count`                   | 21    | fixed, sweep one                 |
    | `qa_gate_detail`                       | 4     | 2 in sweep two, 2 in sweep three |
    | `helper_counts_summary` (from a file)  | 1     | Task 4.4                         |
    | prints its own line (`deployed-drift`) | 1     | never had the defect             |
    | the two soft-degrading \`              |       | summary="OK"\` gates             |

Three defects in the one table the commit offers as proof that the population was finally
enumerated:

- **`qa_gate_detail` is 4; it is 6.** Counted in `scripts/qa-all.bash`: 8 call sites over 6
  captures producing 6 stage lines (`nokill`, `planlib`, `extension-compat`,
  `panel-contract`, `vmtest-manifest`, `version-pins`). `qa-all.bash`'s own header says 6 and
  `CLAUDE/QA.md`'s reader table says *"6 gates whose summary is not a count"*. Three
  documents in one commit, two numbers.
- **The column sums to 27, not 29.** The prose immediately above it says *"Sweep three
  enumerated all 29 hard gates and classified how each derives its stage line"*.
- **The last row is not a table row.** A literal `|` inside `` `|| summary="OK"` `` broke the
  cell split; the backtick was escaped and the pipe was not. It carries no count, and the two
  gates it means (`extension-compat`, `panel-contract`) are already inside the
  `qa_gate_detail` bucket now, so reading it as "+2" double-counts them.

I verified the real figures independently: 29 hard gates (29 distinct `exit 1` gates
enumerated in `qa-all.bash`), 28 composed stage lines (28 `printf '✓ …'` lines counted),
21 + 6 + 1 = 28, plus `deployed-drift` printing its own. The code's numbers are right; the
table recording them is not.

## Should fix

### 5. A target that is present, untracked and not ignored still makes the gate machine-dependent

`helpers/docs/link_check.py`, anchor *"`git check-ignore` answers for paths that DO NOT
EXIST"*; the same sentence in `PLAN.md` Task 2.2, `FINDINGS.md` and `CLAUDE/QA.md`.

The question the mechanism answers is *"does `.gitignore` claim this path"*. The finding it
raises says *"target is not tracked by this repository"*. Those differ for a file that
exists locally, was never `git add`ed, and matches no ignore rule. Measured in a throwaway
git repo, same checker, same commit:

    LOCAL (file present, untracked, not ignored) -> findings: []
    CI    (same repo, file absent)               -> findings: [{'problem': 'target does not exist'}]

Green here, red in CI — Cause A's exact shape, in the classification that replaced it. It is
not a regression (the old two-way check behaved identically) and no such link exists today,
but the plan's Goals say *"`qa-all.bash` reaches the same verdict ... or names precisely and
by design which stages cannot run where"*, and this case is neither reached nor named.

**Fix**: either ask the tracked-set question directly (`git ls-files --error-unmatch`, or
membership in `git ls-files`, with care for directory targets — which is presumably why
`check-ignore` was chosen), or narrow the four copies of the sentence to what was measured:
the *ignore* question is machine-independent, and an untracked, unignored target is a
remaining divergence.

### 6. A link to a vendored root ITSELF is exempted by nothing and fails in a clean checkout

`helpers/docs/link_check.py`, anchor `def vendored_root_for`.

The roots carry trailing slashes and the test is `rel_target.startswith(root)`, so a target
that *is* the root — `[daemon](../hooks-daemon)`, or any directory-valued link — matches no
root. It then misses the ignore branch too, because `.gitignore`'s `.claude/hooks-daemon/`
rule is directory-only and git cannot tell a non-existent path is a directory. Measured in
this worktree:

    NOT IGNORED .claude/hooks-daemon        vendored_root_for -> None
    IGNORED     .claude/hooks-daemon/CLAUDE vendored_root_for -> .claude/hooks-daemon/
    NOT IGNORED roles/vendor                vendored_root_for -> None
    IGNORED     roles/vendor/x              vendored_root_for -> roles/vendor/

and end to end on a fixture tree, a link to the root resolves to
`{'problem': 'target does not exist'}` — a hard failure in CI, a pass wherever the tree is
installed. Latent: 0 of the 326 enumerated link targets have this shape. The declaration is
sold as *"Parents, not leaves"*, and the parent itself is the one path the parent does not
cover.

**Fix**: `rel_target == root.rstrip("/") or rel_target.startswith(root)`, with a case.

### 7. `PLAN.md` describes a stage line the gate does not print

`PLAN.md`, Task 2.2, anchor *"The stage line carries `VENDORED: N link(s)`"*. What it carries,
measured here:

    ✓ docs: 71 files OK (links, anchors, playbook catalogue, topic index) — VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken

Three counts, no `link(s)`. The plan is describing the one-count version that the 07:20
journal entry records the owner replacing. Plan/code drift, in the sentence that documents
the deliverable.

### 8. "All three changed because they were broken" — one of the three was working

`FINDINGS.md`, anchor *"three other stage lines changed, and all three changed because they
were **broken**"*; the same word in `PLAN.md` (*"3 changed because they were broken"*) and in
the commit message.

I re-measured old expression against new, on the same live captures:

    OLD compat: [All 5 extension(s) cover the GNOME Shell that Fedora 44 ships.]
    NEW compat: [All 5 extension(s) cover the GNOME Shell that Fedora 44 ships]
    panel-contract: SAME
    version-pins:   SAME as whole-capture interpolation

`extension-compat`'s old reader was `grep -E '^All [0-9]+ extension'`, which matched exactly
one line of a seven-line capture; its `||` fallback never fired, and its stage line lost a
full stop. That is not "broken" in the sense `nokill` (zero matches for its whole life) and
`vmtest-manifest` (a three-line stage line every run) were. The count of three is right and
`FINDINGS.md`'s own before/after block states the real change — only the word generalising
over all three is wrong. Worth correcting *because* it is the paragraph retracting an
over-claim, and round 9's finding 2 was that the previous retraction contained another one.

### 9. `qa-js.bash`'s missing dependency was documented rather than closed

`CLAUDE/QA.md`, new table row anchor *"its own message says no playbook installs it"*.

Round 9 asked for this row and it is correctly here. But what the row records is a tool the
repo's own QA needs and no playbook installs: `.github/workflows/qa.yml` runs `npm ci` in
`extensions` (line 66), and nothing in `playbooks/` or `tasks/` does. So on any host that has
not run `npm install` by hand, `qa-all.bash` exits 2 before a single hard gate runs.
`CLAUDE.md` → *"Missing Dependencies — Fail Fast, Fix in IaC"* is explicit that this is an
IaC gap to close in the playbook, not a fact to write down (points 3 and 5).

`PLAN.md`'s Non-Goals now claim it as a worktree gap with *"its own decision"*, which is the
owner's call to make — so I am flagging rather than pressing it. But it is not only a
worktree gap: it is every undeployed host, and it is the Task 4.1 mechanism (one unpassable
gate disabling every gate behind it) still live and now documented as expected behaviour.
The row should say which playbook will own it, or the plan should say the owner declined to.

## Nits

- **Nothing couples `_VENDORED_ROOTS` to `_EXCLUDE_PREFIX`.** Both tuples list
  `.claude/hooks-daemon/` and `roles/vendor/`, and they must agree: declaring a vendored root
  without adding the scan exclusion would make the gate sweep another repository's markdown
  as if it were ours. No test asserts the containment (searched every tracked file). One
  `assertTrue(all(r in _EXCLUDE_PREFIX for r in _VENDORED_ROOTS))` closes it.
- **The reader suite now embeds six other gates' stdout in its own**, which widens the input
  domain of the reader that reads *it*. `qa_gate_case_count` takes the last matching line;
  measured on the real capture, exactly **1 of 1** lines match `passed:[[:space:]]+[0-9]+`
  and the reader answers `passed: 59`. Sound today, and now dependent on six gates' wording
  with nothing asserting it — the library header's justification (*"no gate emits a second
  `passed: <digits>` line"*) was measured over a population that just grew.
- **`qa-docs.bash` cannot be driven against a fixture tree.** `link_check.main` takes a root
  argument; the bash gate computes `REPO_ROOT` from its own location and passes no override.
  That is why the `broken` rendering control had to be a crafted payload — see the adequacy
  note below.
- **A botched rewrap in the `qa_gate_detail` header**: *"and it exists because the
  alternative was"* / *"# measured and had failed silently"* now breaks mid-clause across the
  comment block.
- **Two consecutive blank lines** after the `_VENDORED_ROOTS` tuple (ruff does not flag them
  at module level under this config; cosmetic).
- **The vendored counts never reach the published JSON.** `qa-docs.bash`'s reshape drops
  `.vendored`, so `jq '.checks.docs'` in `/tmp/qa-results.json` cannot see them; they exist
  only in the stage line. Fine for `verdicts.py`, which compares stage lines — noted because
  the file header advertises the JSON as the machine-readable surface.

## On the partial control — my judgement

The journal names it honestly: the `broken` rendering was proved with a crafted JSON payload
through the real `jq`, not a real broken tree. **I improved on it and it now holds.** I built
a real broken tree in a throwaway git repo, ran the real classifier over it, and took its
real output:

    "broken": [{"file": ".claude/rules/agent-docs.md", "line": 1,
                "target": "../hooks-daemon/CLAUDE/DirectoryRoles.md",
                "problem": "broken link into the vendored repository at .claude/hooks-daemon,
                            which IS present here — it has probably moved the file"}, ...]

fed that through the gate's own `jq -r '.vendored.broken[] | ...'` and `echo` statements, and
parsed the result with the real `verdicts.parse`:

    stages: {'docs': [('⚠', '2 link(s) into a PRESENT vendored repo are broken — …'),
                      ('✓', '71 files OK … VENDORED: 0 verified, 0 unverifiable, 2 broken')]}
    symbol_lines: 2  matched_lines: 2  unrecognised: 0

So the rendering is now proven against classifier output rather than a hand-written payload,
and finding 3 above is the one thing that path still gets wrong. **The remaining gap is
`qa-docs.bash` itself**: no invocation of the script has ever taken the `V_BROKEN -gt 0`
branch, because the script has no root seam (nit above). Adequate, given that every piece
either side of it has been exercised — but say so rather than calling it end-to-end.

## Checked and clean — what I verified, and how

1. **The exit-code invariant holds; I tried to break it.** `check_links` appends to
   `vendored["broken"]` and never to `findings`; `main()` derives `status` and its return
   code from `findings` alone; `qa-docs.bash` derives `NFINDINGS` from `.findings` and the
   `⚠` branch has no `exit`. Real measurement on a tree with two genuinely broken vendored
   links: `findings: []`, exit path 0, 2 broken. The only way a vendored outcome reaches the
   exit code is the crash path, which is correct (below).
2. **`git check-ignore` does answer for paths that do not exist** — measured in this
   worktree against four paths, none of which are on disk here:
   `.claude/hooks-daemon/CLAUDE/X.md`, `roles/vendor/foo/README.md`, `untracked/x.md` all
   reported ignored; `docs/architecture.md` not. `check=False` is correct and annotated, and
   the `returncode not in (0, 1)` branch raises with a message rather than returning an empty
   set. I traced the raise all the way out: an uncaught `RuntimeError` exits Python **1**,
   `qa-docs.bash` treats 1 as "findings", and the `jq -e 'has("findings") and has("scanned")'`
   validation then refuses the traceback and exits **2**. Confirmed both steps
   (`python3 -c "raise RuntimeError"` → exit 1; a traceback through that jq → parse error).
   The docstring's *"the gate reports exit 2"* is true by two hops, neither asserted — the
   Python test asserts only the raise.
3. **The three-way classification against the whole population.** I enumerated every link in
   all 71 in-scope documents: **326** file-links. 8 reach the vendored branch (all
   `.claude/rules/*.md` → `../hooks-daemon/CLAUDE/DirectoryRoles.md`, i.e. exactly Cause A's
   set), 0 are ignored-but-unvendored, 0 escape the repo root. So the exemption's blast
   radius today is eight links to one file. Nothing the old two-way check caught is let
   through except by design (a broken link into a *present* vendored repo, which now warns),
   and finding 6 is the one shape the new branch does not reach.
4. **`_VENDORED_ROOTS` is reachable, correct and not speculative.** `.claude/hooks-daemon/`
   covers the 8 live links. `roles/vendor/` is `ansible.cfg`'s `roles_path = ./roles/vendor`
   (line 7) and `.gitignore`'s `roles/vendor/*` (line 6), so it is a real vendoring location
   with nothing linked into it yet — a declaration, not YAGNI. **Sibling prefixes cannot
   match**: the trailing slashes mean `roles/vendor-extra/x` does not match `roles/vendor/`.
   **Traversal cannot claim vendored status**: `os.path.normpath` collapses `..` *before*
   `repo_relative` rejects an escape, and `repo_relative(None)` short-circuits
   `vendored_root_for`; measured, a `../../elsewhere/README.md` target is a finding, not an
   exemption. **Symlinks**: `normpath` is not `realpath`, so classification is by literal
   repo-relative path — a symlink into a vendored tree would be checked (strict direction)
   and a symlink out of one would be exempted (lax). The repo has exactly one tracked
   symlink, `scripts/vault`, nowhere near either root, so there is no live exposure.
5. **`verdicts.py` keeps both lines of a `⚠`-then-`✓` pair.** `parse()` does
   `stages.setdefault(name, []).append(...)` and `compare()` compares the lists, so nothing is
   half-dropped; verified with the real payload above (2 stages lines, 0 unrecognised, and the
   indented `file:line target` lines correctly excluded from both numerator and denominator
   by `SYMBOL_BEARING`). The cited precedent is real: `qa-patterns.bash` emits
   `⚠ patterns: …` at two anchors and then `✓ patterns: …` — and I saw the shape live in this
   worktree's own run (`⚠ shellcheck: 171 issues` then `✓ bash: 262 files OK`).
6. **`check_links`'s return shape is consistent everywhere.** One production caller
   (`link_check.py`, `findings, vendored = check_links(...)`), two test call sites, both
   tuple-aware; no other file in the tree references it. The `broken` dicts are
   finding-shaped (`file`/`line`/`target`/`problem`) and genuinely excluded — that shape is
   deliberate so the `⚠` line can print them like findings, and it is the only place the two
   could have been confused.
7. **Round 9's four required items, each re-verified against the code:**
   - `vmtest-manifest`, `extension-compat`, `panel-contract`, `version-pins` all now call
     `qa_gate_detail`; no `|| …_summary="OK"` fallback survives anywhere in `qa-all.bash`.
     `vmtest-manifest`'s three-line capture is now one joined line.
   - "Pure refactor" is retracted in `PLAN.md`, `FINDINGS.md` and the journal (finding 8 is
     the one word still over-claiming).
   - The three readers are documented in `qa-all.bash`'s header and in a new `CLAUDE/QA.md`
     section with a function table and an "adding a gate" rule; the catalogue row no longer
     calls the suite a single reader.
   - The `qa-js.bash` row is in the machine-dependence table, and `qa-docs.bash` is correctly
     removed from it with the reason (finding 9 is about the gap, not the row).
   - **The enumeration itself**: 29 hard gates (29 `exit 1` gates, enumerated), 28
     `printf '✓ …'` stage lines (counted), 21 `qa_gate_case_count` + 8 `qa_gate_detail` calls
     over 6 stage lines + 1 `helper_counts_summary` = 28, plus `deployed-drift` composing its
     own. The code's claim is exact. `deployed-drift`'s extra `template (…)` lines are
     indented, so they are not stage lines and it really did not have the defect — checked,
     not assumed. Finding 4 is the write-up, not the count.
8. **The coupling section reaches the exit status.** `set -uo pipefail` (no `-e`, documented
   and correct), the new block precedes `echo "passed: …"` and `[ "$failed" -eq 0 ]`, which
   remain the last two statements, and every failure path increments `failed`. All 8 patterns
   PASS against their real gates, each registered command matching `qa-all.bash`'s own
   invocation including the `.` argument for `check_panel_contract`. The dead-pattern
   mutation reproduces at the function level: the real `nokill` capture answers
   `3 container-watch file(s) clean` for the live pattern and `summary unreadable` for
   `[0-9]+ call site[s]? checked`. Finding 1 is the extraction, not the execution.
9. **Fail-fast**: no `failed_when`, `ignore_errors`, `|| true` or `2>/dev/null` in any
   changed file. `subprocess.run(..., check=False)` in `git_ignored` is the sanctioned
   probe-then-fail form and the returncode is inspected on the next line.
10. **Stderr hygiene**: `link_check.py` puts only its JSON payload on stdout; `qa-docs.bash`'s
    `✗`/`⚠` findings text is stdout, which is correct — it is this gate's payload and the
    stream `verdicts.py` parses — while its crash diagnostics go to `>&2`.
11. **Public-repo safety**: scanned all **1,003** added lines of `72d21ffc` for emails, home
    paths, `/root/` paths, RFC1918 addresses, `/workspace` paths, hex ids of 12–64 chars and
    `.local`/`.lan`/`.home` hostnames. **Zero hits on every pattern.** The only
    install-shaped strings anywhere near the change are `fedora-desktop` (the permitted
    self-reference, and it appears in gate *output*, not in the diff) and `.claude/hooks-daemon`
    (a public project name).
12. **IaC placement**: no playbook, no `files/`, no `files/var/local/claude-yolo/` change, so
    **no CCY version bump is required and correctly none is present**. No Ansible was run.
13. **Plan hygiene**: plan and code committed together; journal **132 added / 0 deleted**
    (append-only); `CLAUDE/Plan/README.md:37` row present; no untracked plan directory;
    Task 2.1 and 2.2 marked ✅ with the evidence behind them, and Tasks 4.3, 5.2, 5.3 left
    open, which is correct. `PLAN.md` Task 2.2's "14 cases" matches
    `TestVendoredLinkTargets` exactly (14 of the 19 new methods; the 5 in
    `TestTheUndeclaredRepositoryHint` are uncounted but belong to the same work).
14. **Lint and modes**: `shellcheck -x` clean on all four changed bash files; `ruff check`
    clean on both Python files; git-recorded modes correct (`qa-all`, `qa-docs`,
    `test-qa-helper-summary` 100755; the library and both Python files 100644).
15. **Naming**: `vendored_root_for`, `git_ignored`, `nested_repository_for`, `repo_relative`
    all say what they do and return what they say. `verified`/`unverifiable`/`broken` name
    three states a reader can act on differently, which is the test. No jargon.

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `./scripts/qa-all.bash` | **exit 2 — could not complete in this worktree.** `ansible-syntax` fails 82/82 on the missing vault password file, then `qa-js.bash` exits 2 on an absent `extensions/node_modules` and aborts the run before any of the 29 hard gates. Both are the declared linked-worktree gaps, now both in `CLAUDE/QA.md`'s table; neither is caused by this change. Stages that did run: `✓ bash: 262 files OK`, `✓ python: 150 files OK`, `✓ patterns: 262 files OK`, `✓ ansible: …` |
| `scripts/qa-docs.bash` (individually) | **exit 0** — `✓ docs: 71 files OK (links, anchors, playbook catalogue, topic index) — VENDORED: 0 verified, 8 unverifiable (repo absent), 0 broken`. This is the CI condition reproduced: the three-week-old failure is gone |
| `scripts/test-qa-helper-summary.bash` | exit 0, `passed: 59 failed: 0`, all 8 coupling cases PASS against their real gates |
| `python3 -m unittest tests.helpers.docs.test_link_check` | exit 0, `Ran 72 tests … OK` |
| `python3 -m unittest tests.helpers.qa_environment.test_verdicts` | exit 0, `Ran 40 tests … OK` |
| `python3 -m unittest tests.helpers.qa_environment.test_unittest_counts` | exit 0, `Ran 21 tests … OK` |
| the six `qa_gate_detail` gates, run individually | all exit 0; captures measured at 7, 1, 1, 3, 1 and 1 lines |
| `scripts/qa-deployed-drift.bash` | exit 0, one line, self-skipped with its reason printed |
| `hooks-daemon plan-qa --sweep` | **NOT RUN.** The daemon tree is gitignored and absent from this worktree, and the review was confined to it. Saying so rather than omitting it |
| `ansible-playbook --syntax-check` | **not triggered** — no playbook changed. It could not run here regardless (no vault password file) |
| `scripts/qa-helper-tests.bash` conditional gate | **triggered** (`helpers/` and `tests/helpers/` both changed) — the two affected modules run above; the full runner was not re-run, the four suites covering the changed code were |
| `helpers.gnome.check_extension_compat` | **not triggered** (no `extensions/` metadata change); run anyway as a probe for findings 1 and 8 — exit 0 |
| extension ESLint | **not triggered** (no extension JS change); not runnable here (no `node_modules`) |
| `shellcheck -x`, `ruff check` | clean on every changed file |

## Does Task 5.3 discharge?

**Not yet, and for the first time the remaining list contains nothing about the design.** The
boundary the owner drew is the right one and it is implemented correctly: I enumerated the
whole link population rather than sampling it, tried four ways to smuggle a target into a
vendored root, traced the exit code through every branch including the crash path, and
produced a real `broken` payload to close the one control the journal had flagged as
partial. The invariant holds. The docs gate now reaches the same verdict here as in CI, which
is the thing that was broken for three weeks.

What is left is the same class this plan keeps finding in its own deliverables, four more
times:

1. Widen the `qa_gate_detail` extraction and add a `COVERAGE: n of m`; the guard refuses zero
   and is silent about partial (finding 1).
2. Delete or write `check_vendored_declaration` (finding 2).
3. Guard `V_BROKEN` the way its two neighbours are guarded (finding 3).
4. Correct the `FINDINGS.md` classification table — 6 not 4, 29 not 27, and repair the
   broken row (finding 4).

Then the should-fixes: narrow the "same verdict" sentence or ask the tracked-set question
(5), cover the root itself (6), correct `PLAN.md`'s stage-line text (7), and the one word in
the retraction (8). Finding 9 and the nits are the owner's calls.

The worktree did its job again: the pin held at both checks, so every citation above was
measured against the same bytes it names.

## Review conditions

- Target: `72d21ffc`, in the dedicated linked worktree on `worktree-plan-00125-review`.
- `git status --porcelain` empty at the first action and at the last; `HEAD` unmoved.
- Read-only on repository files. Fixture trees were built in throwaway temp directories by
  `tempfile`, never in the repository; two gitignored capture files under `untracked/` were
  created and removed within the commands that made them.
- Container: CCY — no Ansible run, no deploy.
