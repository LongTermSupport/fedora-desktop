# DBF conformance review: ansible-umask-pinned (Plan 00185)

Reviewer: Defence Before Fix conformance reviewer, against method specification 1.1.0
(sections 3, 4, 7, 8). Under review: defence commit `3a9eee25f`, final commit `10118f437`,
draft report `261023-umask-dbf-report.md`, independent search
`261023-umask-independent-search.md`.

**Scope note.** While this review ran, the working tree of the branch was being changed by
someone else (staged, uncommitted): the two test modules moved from `tests/helpers/umask_pin/`
to `tests/umask_pin/`, a new `scripts/test-umask-pin.bash` with its own `qa-gates.tsv` row,
edits to `callback_plugins/umask_pin.py`, `CLAUDE/QA.md`, both plays, `meta-deploy.list` and
the plan's triage scripts, and a review `261023-umask-review-r1.md`. This review judges the
**committed** state `10118f437`. Every reproduction below ran in clean `git archive` trees of
the two commits, not in the moving working tree. The in-flight change needs its own green run
(finding 6).

## Reproduction (section 7)

Both trees were extracted with `git archive <commit>` into `untracked/scratch/`, then
`git init` and `git add -A` so the rule's `git ls-files` scan sees exactly the commit's files
(6465 files). That makes red reproducible from an archive: nothing the proof depends on lies
outside version control (clause 3.3 Part A).

| Run                  | Tree                                    | Command                                                                                                                          | Result                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| -------------------- | --------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Red, entry point     | `3a9eee25f`                             | `./scripts/qa-changed.bash --files <the two test modules>`                                                                       | exit 1, `✗ unit-tests: 2 module(s)`, `qa-changed FAILED: unit-tests`                                                                                                                                                                                                                                                                                                                                                                    |
| Red, harness         | `3a9eee25f`                             | `python3 -m unittest tests.helpers.umask_pin.test_umask_pinned_rule tests.helpers.umask_pin.test_umask_pin_against_real_ansible` | 14 run, 3 failures: `ansible.cfg:24: ansible-umask-pinned: callbacks_enabled does not name umask_pin ...; see CLAUDE/QA.md "ansible-umask-pinned"`, and the probe's `by-module is 0600` and `by-command is 0600`. All fixture tests and the probe's control (0600) pass, so the rule was loaded and the probe sees the inherited umask                                                                                                  |
| Green, entry point   | `10118f437`                             | `./scripts/qa-changed.bash --files <the two test modules> ansible.cfg callback_plugins/umask_pin.py`                             | exit 0, `✓ unit-tests: 2 module(s)`, plus the four `ansible.cfg`-naming suites passing                                                                                                                                                                                                                                                                                                                                                  |
| Green, harness       | `10118f437` (worktree, before the move) | the same unittest command, `-v`                                                                                                  | 14 tests OK, probe 0644, control 0600                                                                                                                                                                                                                                                                                                                                                                                                   |
| Green, blocking gate | `10118f437`                             | `./scripts/qa-helper-tests.bash`                                                                                                 | **Not completed.** Coverage line `155 of 155 tracked helper test modules`, then the run was killed by a 30-minute `timeout` (exit 124) on a loaded host before unittest printed its summary. One `F` appears in the dot stream, in a module that cannot be identified without the summary. This review therefore does not vouch for the full gate's green; the rule's green rests on the `qa-changed` entry-point run above (finding 4) |

The red proof survives as a commit of its own: `3a9eee25f` adds only `CLAUDE/QA.md`, the rule
and the probe. `ansible.cfg` and the plugin arrive in `10118f437`, and the rule's two test
files are byte-identical between the commits (`git diff 3a9eee25f 10118f437 -- tests/` is
empty), so green came from the fix and not from editing the rule.

**Originating symptom.** The host's 0600 system font cache cannot be reproduced in this
container (it needs a root `fc-cache` on the host under a 077 session). It was not rerun; the
verdict rests on the red and green runs plus the unprivileged probe, which reproduces the
mechanism (a task with no mode under an inherited 077 writes 0600, and 0644 with the pin).
Acceptance check 18 is the host-side check and is pending the meta-deploy run.

## Findings, by severity

### 1. Medium: the independent search finished after the rule was committed and run (3.1 step 2)

Clause 3.1 requires the two techniques to be "chosen and run **before** the [Rule] is run
against the codebase". The search report's file was last written at 15:31; the red commit,
whose message reports the rule run against `ansible.cfg:24`, is 15:22. The searcher's own
caveat says `ansible.cfg` and `play-fonts.yml` changed and "an untracked callback plugin
appeared" while it searched, and it read a `play-fonts.yml` comment naming the pin. So the
rule was built and red-run, and the fix was being written, while the search was in progress.
The report's "dispatched before the rule existed" may be true of the dispatch; it is not true
of the search's completion, and the width decision could not have rested on it.

Mitigation found: the searcher did not open the rule's files, did not run the detector, used
three techniques (text search, reading, a YAML parse) with a stated saturation test, and
recorded what each found that the others could not. Its findings were reconciled afterwards
(A14/A15 reasoned out, H1 to H7 acted on). The substance is sound; the order is not as the
clause requires. **Resolve:** record in the report that the search completed after the red
commit, and that the reconciliation, not the rule's original design, is where the search
bore on the width. No rework is owed if the reconciliation holds, which it appears to.

### 2. Medium: gaps in the rule's width are recorded as "Not caught" without an owner referral (3.1 lower bound, section 4)

The rule does not see: an `ANSIBLE_CONFIG` naming a configuration outside the repository; a
value built on another line; and spellings such as `os.environ.setdefault("ANSIBLE_CALLBACKS_ENABLED", ...)`
or `putenv`, which have no `=`/`:` after the name and are not in the fixtures. Today no
first-party code does any of these with real tasks (this review's grep over tracked files:
the `ANSIBLE_CONFIG=` sites pin the repository's own config or a throwaway one for an
unprivileged temp-dir test). So the reason these are not covered is "no instance exists
today", not "the hazard cannot arise there". Section 4 makes leaving that wider rule unbuilt
the owner's decision. The report names only the task-level rule as "next wider rule" (with a
sound hazard-absent reason under the pin) and does not refer the `ANSIBLE_CONFIG` extension.
**Resolve:** either extend clause 2 to report an `ANSIBLE_CONFIG` assignment that does not
name the repository's `ansible.cfg` (and add the `setdefault`/`putenv` spellings), or list
the extension under "Decisions referred to the owner" as a rule left narrower for want of an
instance.

### 3. Medium: narrowings are not recorded with their sentences in the remediation docs (3.3 Part B, 3.6)

The rule excludes vendored and daemon-owned trees, `untracked/`, `CLAUDE/Plan/Completed/` and
`Cancelled/`, files ending `.md .json .txt .golden .tsv`, and both of its own test files whole.
The sentences live only in code comments and the draft report; the `CLAUDE/QA.md` section
lists none of them, and its "Not caught" omits the no-`ansible.cfg` case the module docstring
gives (Plan 00062 A14/A15). Two sentences do not hold as written:

- `.json` is "prose and data, where a name is never an assignment a process runs with":
  false in general, since a JSON settings file's `env` block (an editor's or a session
  harness's) is exactly an environment a launched process runs with. No tracked JSON sets
  `ANSIBLE_CALLBACK*` today (checked), so this is a doubtful narrowing, which section 4 routes
  as a suppression, not a live instance.
- The probe module is excluded as a whole file; clause 3.3 Part C asks for the self-match
  exclusion to be as precise as possible with a test that it reaches nothing else. Only the
  control run's one line carries the pattern.

Archived plans: no sentence says why their scripts cannot run (one completed plan's script
sets `ANSIBLE_CONFIG`). **Resolve:** add the narrowings and their sentences to the QA.md
section; drop `.json` from the skipped suffixes (or justify it per file); narrow the probe
exclusion to its control-run line.

### 4. Low: the green blocking gate and `qa-changed` routing (3.5)

The draft report claims green through `./scripts/qa-helper-tests.bash`. This review's run of
that gate on a clean `10118f437` tree did not finish within 30 minutes and showed one
unattributed `F`. The rule's own green is reproduced through `qa-changed` (an entry point the
project uses to accept changes). The full-gate green is unverified here and should be shown by
the coordinator's `qa-all.bash` run, with the failing module named if the `F` recurs.

At `10118f437` the rule runs in `qa-helper-tests.bash` (blocking, in `qa-all`), and
`qa-changed` runs it only when a test module itself changes. This review confirmed that a
change to `ansible.cfg` or `callback_plugins/umask_pin.py` alone does not route to the rule
(`--base dcb76af56 --plan` for the whole branch lists no `unit-tests` check). The report
records this as a toolchain gap, which is the right handling. Note that the runtime failure
mode is fail-open (a callback that cannot load only warns), so the probe is the only thing
that notices a broken plugin; the in-flight `scripts/test-umask-pin.bash` naming both paths
addresses this.

### 5. Low: the reproducing test is the mechanism, not the reported behaviour (3.1 last paragraph, 7)

The probe pins "a task writing with no mode under an inherited 077" for `copy` and a command,
unprivileged with `-c local`. It does not exercise `become`/sudo; the claim that sudo yields
the union of the caller's umask and 022 is reasoning, not tested. The reported behaviour (the
system font cache readable after a run from a 077 session) is pinned only by host acceptance
check 18. That is an honest split and the report names both; it should say explicitly that
the sudo leg is unverified by a runner check.

### 6. Info: the in-flight change must be re-proven before it is committed (3.5)

The staged move of the modules out of `tests/helpers/` takes them out of
`qa-helper-tests.bash`'s walk; the new `qa-gates.tsv` row for `scripts/test-umask-pin.bash`
replaces it. Before that commit lands, the green run must be repeated through the entry point
on the new paths, the QA.md "Run by" text and the report's "Permanence" section updated, and
the identifier must still resolve. Not reviewed here because it is not committed.

### 7. Info: process items outside the specification

`meta-deploy.list` at `10118f437` queues only `00185-terminal-emoji-glyphs`, so the H2/H7
repairs in `play-basic-configs.yml` would not reach the host (raised in review r1; the staged
list adds a `changed-plays` entry). This bears on "every instance fixed" in practice only once
the host run happens.

## Checked, no finding

- **Class and hazard (3.1).** Named as a pattern with a hazard sentence; the class is defended
  at the one choke point every run passes, and the next wider rule (task-level `mode:`) is
  named and rejected for hazard absence under the pin, which is the practitioner's call. The
  behaviour-level question is answered (probe plus acceptance check 18).
- **Detector reads code (3.2).** The rule parses `ansible.cfg` and scans tracked text; it does
  not execute code. Housed in a unittest module, following the project's existing practice for
  Python-checked rules; the probe is the separate behaviour pin, not the detector.
- **Fixtures (3.3 Part C).** Each clause has firing fixtures and quiet fixtures, including a
  look-alike name and reads/comparisons; a control proves the scan reaches `ansible.cfg`.
- **Sweep and count (3.4).** Scope recorded (all tracked first-party files, every `ansible.cfg`),
  count 1 (`ansible.cfg:24`), corroborated by the search's launch-point table (20 launchers,
  all loading the repository's `ansible.cfg` except A14/A15, reasoned out as matching no host).
  No baseline, no suppression. H1 to H7 and the 13 unsure rows receive the pin by pattern; H1,
  H2, H7 examined individually for past damage; H3 to H6 past host state referred with count
  fixed (3 of 7 explicit repairs), what stopped it and what to try next.
- **Blocking and identifier (3.5, 3.6, 8.1 to 8.5).** The rule fails, never warns; the message
  is terse, names file and line, and carries `ansible-umask-pinned` and the doc pointer; the
  QA.md heading exists in the same commit as the rule, states what, why and the correct
  construction; QA.md `###` headings enumerate the project's rules.
- **Original defect last (3.4, 3.5).** Fixed in the final commit with the repair path in
  `play-fonts.yml` (rebuild when any system cache file is not other-readable) and a
  reproducing test (the probe, red 0600, green 0644).
- **Report template.** All sections present and consistent with the commits, except the
  timing claim in finding 1 and the omissions in findings 2 and 3.

## Verdict

The **defence conforms in substance** for 3.2, 3.3 Part A, 3.5 and 3.6: red and green both
reproduce through the project's entry point from clean trees, and the rule is blocking with a
resolving identifier. The **remediation does not yet conform**: finding 1 (search ordering,
3.1), finding 2 (owner referral of the unbuilt `ANSIBLE_CONFIG` extension, section 4) and
finding 3 (narrowings unrecorded in the remediation docs, one doubtful) are open. All three are
resolvable by record and small rule edits; none requires reworking the fix.
