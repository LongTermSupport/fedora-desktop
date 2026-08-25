# Plan 00067: QA gates silently scan ZERO files in a nested checkout

**Status**: Complete
**Created**: 2026-07-29
**Completed**: 2026-07-29
**Owner**: joseph
**Priority**: High

## Overview

`scripts/qa-bash.bash` and `scripts/qa-js.bash` report **success while scanning nothing** when
this repo is checked out under a path containing a segment they exclude. Measured in a checkout
at `untracked/repos/fedora-desktop`:

```
qa-bash      exit=0 : bash: 0 files OK
qa-js        exit=0 : js: 0 files OK
qa-patterns  exit=0 : patterns: 117 files OK
```

**112 bash files** and every JavaScript file went unchecked, and both gates exited 0.

> **Count correction.** The first measurement said *86*. That was the `*.sh`/`*.bash`
> **name-matched** subset only — `qa-bash.bash` has a *second* `find` block that also picks up
> executables with a bash shebang and no such extension, and that block was equally disabled.
> The true unscanned population, measured after the fix, is **112**. The original figure
> understated the defect; it is corrected here and in the success criteria rather than left to
> look like a shortfall.

`CLAUDE.md` mandates `./scripts/qa-all.bash` before every commit touching Bash or Python. In
such a checkout that command is a no-op which reports a pass — so the mandatory gate provides no
protection while looking like it does.

## Root cause

Each gate resolves its own root and then filters with **absolute-prefix** patterns:

```bash
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"   # qa-bash.bash:14
find "$REPO_ROOT" -type f \( -name "*.sh" -o -name "*.bash" \) \
    ...
    ! -path "*/untracked/*" \                                  # qa-bash.bash:43
```

`find` matches `-path` against the **whole** path it prints. Every path under
`/workspace/untracked/repos/fedora-desktop` contains `/untracked/`, so
`! -path "*/untracked/*"` excludes the entire repository. The exclusion is *intended* to mean
"the repo's own `untracked/` scratch directory" — a **repo-root-relative** concept expressed as
an unanchored glob.

`qa-patterns.bash` is unaffected because it passes `--exclude 'untracked'` to semgrep, which
resolves exclusions relative to the scan root rather than as an absolute-path glob. That is the
behaviour the `find`-based gates should match.

`qa-python.bash` carries the identical defect in both of its `find` blocks. It currently exits 2
here for an unrelated reason (`ruff not installed`), which **masks** the bug rather than avoiding
it — remove the mask and it too would scan zero files and pass.

## Why this matters more than its size

The gate does not fail, and it does not warn. It prints `✓ bash: 0 files OK` — **a true
statement about the check presented as a stronger statement about the world**. A control that
silently degrades to a no-op is worse than one that is absent, because absence is visible and
nobody builds on it. This repo's `CLAUDE.md` bans `|| true` and `failed_when: false` for exactly
this reason; an unanchored exclusion glob achieves the same outcome without using any banned
token.

It was found while rebuilding Plan 00066's `triage.bash` (lts-infra Plan 00023): the QA gate was
run, printed `0 files OK`, and could not honestly be cited as evidence of anything.

## Goals

- Anchor every **repo-root-relative** exclusion to `$REPO_ROOT`, so a nested checkout is scanned
  exactly like a top-level one.
- Keep genuinely **any-depth** exclusions unanchored (`.git`, `node_modules`).
- Make a zero-file scan **fail loudly** instead of passing — so this class of defect can never
  again present as a pass, whatever its cause.
- Prove the fix by measurement: file counts before and after, in this very checkout.

## Non-Goals

- Not changing *what* is excluded. The exclusion set is correct; only the matching is wrong.

- Not touching `qa-patterns.bash` (semgrep already resolves relative to the scan root) or
  `qa-ansible*.bash` (they enumerate explicit directory lists, not a repo-wide `find`).

- Not fixing `ruff not installed`. `qa-python.bash` is right to exit 2 over it — see `CLAUDE.md`
  "Missing Dependencies".

  > **Correction (made while closing this plan).** This Non-Goal originally called that a *real
  > IaC gap in this repo*. It is not. This repo declares `ruff` in **both** places it needs it:
  > `playbooks/imports/play-python.yml:20` (host) and `.claude/ccy/Dockerfile:33` (its own CCY
  > image). The gate was being run from **lts-infra's** CCY container — the repo that vendors this
  > one at `untracked/repos/fedora-desktop` — and *that* image installs only `ansible-lint` and
  > `yamllint`. So the missing dependency belongs to the surrounding container, not to
  > fedora-desktop, and it is fixed in lts-infra's IaC rather than here. Recorded because the
  > original wording would have sent the next reader looking for a gap in this repo that does not
  > exist.

## Tasks

### Phase 1: fix the matching

- [x] ✅ **Task 1.1**: Measure the defect in this checkout: which gates scan zero files, which
  exit 0 while doing so, and how many files *should* be scanned (measured 86 name-matched;
  corrected to 112 once the second `find` block was also working — see the count correction above).
- [x] ✅ **Task 1.2**: Anchor the root-relative exclusions to `$REPO_ROOT` in
  `scripts/qa-bash.bash` (both `find` blocks), `scripts/qa-python.bash` (both) and
  `scripts/qa-js.bash` (one). Leave `*/.git/*` and `*/node_modules/*` unanchored — those
  legitimately occur at any depth (submodules, nested package trees). `*/__pycache__/*`,
  `*/.venv/*` and `*/venv/*` likewise stay unanchored in the Python gate.
- [x] ✅ **Task 1.3**: Add a zero-file guard to each gate: finding no files of its language is a
  **failure**, with a message naming the likely cause. This is the structural half of the fix —
  anchoring stops today's instance, the guard stops the class.

### Phase 2: prove it

- [x] ✅ **Task 2.1**: Re-run each gate in this checkout and record the counts.

  | Gate        | Before            | After             | Note                                        |
  | ----------- | ----------------- | ----------------- | ------------------------------------------- |
  | `qa-bash`   | `exit 0`, **0**   | `exit 0`, **112** | 105 shellcheck findings surfaced, 0 `error` |
  | `qa-js`     | `exit 0`, **0**   | `exit 0`, **6**   | `node --check` clean, eslint clean          |
  | `qa-python` | `exit 2` (masked) | `exit 0`, **38**  | discovery proof only — see below            |

  The 105 shellcheck findings are all `warning`/`info`/`style`; the gate reports them and
  gates only on `error`-level, so exit 0 is correct. They are **results, not regressions** —
  files that had never been analysed. Triaging them is separate work, and they must not be
  re-hidden.

  `qa-python` still exits 2 here for `ruff not installed`, and that check sits *before* file
  discovery (`qa-python.bash:22`), so neither its anchoring fix nor its zero-file guard is
  reachable in this container. Both were proven with a **stub `ruff`** on `PATH` that lints
  nothing and returns `[]`: discovery went 0 → 38, and the guard fired `exit 2` on an empty
  tree. The 38 files did pass a *real* `python3 -m py_compile`; only the lint step was stubbed,
  and an empty stub result set is **not** evidence of clean Python.

- [x] ✅ **Task 2.2**: Verify no over-exclusion regression. Zero files from any excluded tree
  appear in the scanned set, and a negative control confirms the exclusions are doing real work
  rather than being vacuously satisfied — each excluded tree genuinely contains bash:

  | Excluded tree           | bash files present | in scanned set |
  | ----------------------- | ------------------ | -------------- |
  | `untracked/`            | 7                  | 0              |
  | `roles/vendor/`         | 23                 | 0              |
  | `.claude/hooks-daemon/` | 88                 | 0              |
  | `.claude/ccy/`          | 20                 | 0              |
  | `.claude/skills/`       | 6                  | 0              |
  | `.ansible/roles/`       | 23                 | 0              |

- [x] ✅ **Task 2.3**: Confirm the zero-file guard actually fires. Each gate was copied into an
  empty tree **without** a `.sh`/`.bash`/`.js` name and **without** the execute bit — otherwise
  the gate discovers its own copy and reports 1 file, and the test proves nothing. All three
  exit **2** with the diagnostic naming the likely cause.

### Phase 3: record

- [x] ✅ **Task 3.1**: Note in `CLAUDE/QA.md` that a gate reporting `0 files` is a failure, not a
  pass, so the next reader does not have to rediscover it.

## Technical Decisions

### Decision 1: anchor to `$REPO_ROOT` rather than post-filter relative paths

**Context**: the exclusions mean "relative to the repo root", but `find -path` sees absolute
paths. Two fixes are possible: anchor the patterns, or post-filter the relative path in the read
loop.

**Decision**: anchor the patterns (`! -path "$REPO_ROOT/untracked/*"`). It is a one-token change
per line, keeps the pruning inside `find` (so excluded trees are never walked or read), and needs
no new loop logic. Post-filtering would walk vendored trees only to discard them.

**Caveat, stated because it is real**: if `$REPO_ROOT` ever contained a glob metacharacter (`*`,
`?`, `[`) the anchored pattern would misbehave. That is implausible for a checkout path, and the
failure mode would be over-exclusion — caught immediately by the Task 1.3 zero-file guard, which
is precisely why the guard is part of this fix rather than a follow-up.

### Decision 2: a zero-file scan is a failure

**Context**: anchoring fixes the known instance. It does not stop the next mis-scoped exclusion,
a wrong `REPO_ROOT`, or a rename from producing the same silent pass.

**Decision**: each gate fails when it finds no files of its language, naming the likely cause.
This repo always contains bash and JavaScript, so zero is never a legitimate answer here.
Consistent with the `CLAUDE.md` fail-fast rule and with `qa-python.bash` already exiting 2 on a
missing analyser — absence of a check is not a passing check.

## Success Criteria

- [x] `./scripts/qa-bash.bash` in this nested checkout scans **112** files, not 0. *(Criterion
  originally read "86" — corrected to the measured 112; see the count correction in the Overview.)*
- [x] `./scripts/qa-js.bash` scans a non-zero count — **6**.
- [x] The repo's own `untracked/`, `roles/vendor/`, `.claude/hooks-daemon/`, `.claude/ccy/`,
  `.claude/skills/` and `.ansible/roles/` are still excluded — proven with a negative control
  (167 bash files across those trees, 0 in the scanned set).
- [x] A gate pointed at a tree with no matching files exits **non-zero** — all three exit 2.
- [x] `CLAUDE/QA.md` states that a `0 files` report is a failure.
- [x] The three modified gates pass `bash -n` and `shellcheck -x` (exit 0, no new findings).

## Risks & Mitigations

| Risk                                                                        | Impact | Probability | Mitigation                                                                                           |
| --------------------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------------------------------------------- |
| Newly-scanned files surface a backlog of real findings, making the gate red | M      | H           | Those are RESULTS, not regressions. Report the count honestly; triage separately, never re-hide them |
| Anchoring over-excludes and silently shrinks coverage the other way         | H      | L           | Task 2.2 compares the scanned list to an expected set; the Task 1.3 guard catches a total wipe-out   |
| A `$REPO_ROOT` containing glob metacharacters breaks the anchored pattern   | M      | L           | Decision 1 caveat; the zero-file guard converts it from silent to loud                               |
| Touching shared QA gates conflicts with concurrent work on another branch   | M      | M           | Lands on this plan's own branch; the change is ~12 lines across 3 files and trivially reviewable     |

## Notes & Updates

- Found while rebuilding Plan 00066's `triage.bash` under lts-infra Plan 00023, which recorded it
  as a finding rather than fixing it inline. This plan is that fix.
- Failsafe recovery cron: reusing the session's existing hourly cron `ffc583d1` (:23) rather than
  adding a second — a duplicate at the same minute was already removed once this session.

### Follow-up finding (NOT fixed here — deliberately out of this plan's scope)

`qa-bash.bash` treats its analyser as **optional** while `qa-python.bash` treats its analyser as
**required**:

| Gate             | Analyser missing    | Result                                                      |
| ---------------- | ------------------- | ----------------------------------------------------------- |
| `qa-python.bash` | `ruff` absent       | `exit 2` — hard fail (`qa-python.bash:22-25`)               |
| `qa-bash.bash`   | `shellcheck` absent | writes `[]`, reports a pass (`qa-bash.bash:114`, `150-152`) |

`CLAUDE/QA.md` documents this as *"silently skipped if absent"*. That is **the same class of
defect this plan is about** — a control that degrades to a no-op while the run still reports
success — differing only in that it is intentional and documented rather than accidental. The
zero-file guard added here does not catch it: the file count is non-zero, it is the *analysis*
that is missing.

Not fixed here because it is a behaviour change with a wider blast radius than the anchoring fix
(it would make `qa-all.bash` fail on any box without `shellcheck`, which is an IaC question about
what the gate's dependencies are, not a bug in path matching). Recorded so it is tracked rather
than stranded; it wants its own plan, alongside the `ruff` IaC gap.

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00067-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Defect measured and root-caused: `find -path` matches absolute paths, so an unanchored
  `*/untracked/*` excludes an entire repo checked out under such a path. 86 bash files + all JS
  unscanned, both gates exit 0.
