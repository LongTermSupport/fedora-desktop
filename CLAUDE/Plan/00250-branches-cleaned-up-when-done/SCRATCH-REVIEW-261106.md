# Worktree scratch review before a deliberate clear (2026-11-06)

Read-only review of the ignored content under `untracked/` in every linked worktree, so that
the coordinator can delete it on purpose (TRIAGE cause 3) without losing work. Measured against
`origin/F44` at 8d3c76486. Paths are relative to `<repo>`.

**Scope:** 50 worktrees: every one in `git worktree list` except the main checkout,
`.claude/worktrees/f44-arm` and the nested `agent-a5e07c91129acb55c-99bbdc03` (kept for its
arm64 run logs). `untracked/.gitignore`, `__pycache__` and `.ruff_cache` were skipped. No vault
or `*.secret*` file was opened; files inside repository copies were judged by path only.

## Method

- **HEAD reachability:** `git merge-base --is-ancestor <head> origin/F44`, else
  `git branch -a --contains <head>`. `archive/<name>` below is `refs/remotes/archive/<name>`,
  the fetched copy of origin's `refs/archive/<name>`.
- **Notes:** every `*.md`, brief, handoff, journal draft, design and notes file was tested by
  sampling up to eight of its long lines and looking each up with `git grep -F` in
  `origin/F44` and in the worktree's own `HEAD`. Every 0-hit file was then read.
- **Groups:**
  - **DEBRIS**: as the brief defines it, plus *superseded* working briefs and handoffs: those
    for work that F44 dropped or replaced, or whose outcome is a report committed on a ref.
  - **IN-F44**: the text is in F44.
  - **IN-BRANCH**: not in F44, but committed on the worktree's own branch, which origin or
    `refs/archive` holds. Safe to clear while that ref is kept. The brief has no such group;
    it is listed separately so that nothing depends on an unstated assumption.
  - **UNIQUE**: in no ref, and of value.

## Per worktree

### `.claude/worktrees/` (25)

**agent-a08350fb…**: detached at 2ddf10069, in F44. `untracked/scratch/` is empty.
**SAFE-TO-CLEAR.**

**agent-a272344d…** (`review/teams-wait-play-lock`, d227099ff, held by its archive ref).
`scratch/fable-review/`: a QA runner script, its logs and a done marker. DEBRIS.
**SAFE-TO-CLEAR.**

**agent-a2c0593f…**: detached at d92b58eb0, held by
`archive/wip-rescue/agent-a2c0593f…`. Review logs, a suite-runner script, and two review
diffs that can be regenerated. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a30eec90…** (`review/teams-full-handles-rereview`, a76793a55, held by its archive
ref). `scratch/` is empty. **SAFE-TO-CLEAR.**

**agent-a374a700…** (`review/teams-full-handles`, d309ed70f, held by its archive ref). Five
`review-suite-N.log` files. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a38fa8d7…**: detached at 6c4d14423, held by `archive/wip-rescue/…`. `scratch/rev/*.txt`
test and lint output. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a5b2a7eb…**: detached at d81618505, in F44. `scratch/` is empty. **SAFE-TO-CLEAR.**

**agent-a5c8b56d…**: detached at 72c7dbca6, held by `archive/wip-rescue/…`. `scratch/rev3/`
QA output (`*.out`, `*.err`). DEBRIS. **SAFE-TO-CLEAR.**

**agent-a5ee80f7…** (`agent-a5ee80f767118a918-a448e021`, c9dd59437, on origin; WANTED,
ccy pids limit).

- DEBRIS: the `qc*.out`, `t/pat/gt/gold/bl/th.out` and `msg*.txt` commit drafts; the
  `pids-probe.bash`, `tmuxenv-probe.bash`, `pidsprobe2/`, `pids3/` and `r3/` probes and logs
  (including the copied files in `pids3/f44/`, `r3/f44/` and `r3/head/`); and
  `scratch/red/files/…/claude-yolo`, a launcher copy kept for the red run.

- UNIQUE: the four files below. Neither F44 nor the branch holds them, and the branch commits no
  report at all:

  - `agent-reports/261023-ccy-pids-limit.md`: the DBF report. It is stale (written for
    3.100.16), but it is the only record of the specification and toolchain used.
  - `agent-reports/261023-ccy-pids-limit-review-1.md`: review round 1, CHANGES, with the red
    and green proof on the rule.
  - `agent-reports/261023-ccy-pids-limit-review-2.md`: review round 2, CHANGES, with the
    finding list that c9dd59437 addressed.
  - `scratch/261023-a5ee80f767118a918-handoff.md`: the open findings, plus three assessments
    found nowhere else: a health finding on a fast pids climb (proposed, not built), the design
    of a near-limit process-tree snapshot, and the meta-deploy entry.

  **HAS-UNIQUE.**

**agent-a761340e…**: detached at 42d3174fc, in F44. `plan-runs/00194…/acceptance/`, from two
finished runs on 10-19, and `scratch/fake194.out`. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a81014c6…** (`agent-a81014c63f66ee6f8-c4d3bbe2`, c04bb307f, on origin; WANTED, umask
pin).

- IN-BRANCH: `scratch/umask-journal.md`, `umask-journal-r1.md` and
  `261023-umask-dbf-handoff.md`. All their sampled lines are in the branch's 00185 JOURNAL and
  reports.
- DEBRIS:
  - `scratch/umask-dbf-review-red/` and `-green/`: two full repository copies, `.git` included,
    about 13,000 files;
  - the QA and test logs;
  - the `umask-*-msg.txt` commit drafts;
  - `mdl-head.txt`, a copy of the meta-deploy.list header;
  - `plan-runs/00194…`, finished runs.

**SAFE-TO-CLEAR** while the branch ref is kept.

**agent-a83a9b8f…** (`review/pingbus-compact-before-wake`, 4405d2ee4, on origin).
`scratch/fable-cw-commit-msg.txt` is a commit draft. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a85b0ccf…**: detached at ec39effee, held by `archive/wip-rescue/…`. An empty `facts/`
directory. **SAFE-TO-CLEAR.**

**agent-a8c69aa6…** (`agent-a8c69aa67110156ce-c48195fd`, 2250df65a, on origin; WANTED, 00177
Task 7.6).

- IN-BRANCH: `agent-reports/261023-dbf-search-exitcode-kind.md`, committed in the branch's
  00177 `subagent-reports/`, and the six journal drafts (`261023-journal-7.6.md` and
  `journal-177-261023-*.md`), which are in the branch's 00177 JOURNAL.

- DEBRIS (superseded):

  - `agent-reports/261023-ccy-teams-unknown-team-and-kind.md`, a PARTIAL status at 37cbe9ae7;
  - `scratch/261023-a8c69aa67110156ce-handoff.md`.

  Both describe work that the branch later finished: its PLAN has Task 7.6 ✅, and it commits
  `261023-dbf-report-exitcode-kind.md`, the conformance review and the round 1 and 2 reviews.

- DEBRIS: the review diff patch, the QA logs, the `msg-*.txt` drafts, `edit_rules.py`,
  `teams-help-new.txt`, the `driver` binary, and `plan-runs/00194…` (finished).

**SAFE-TO-CLEAR** while the branch ref is kept.

**agent-a9351bb9…**: detached at 87ef7c397, in F44. `plan-runs/00194…` from two runs on
10-19, `scratch/ut.log` and `fake.log`. DEBRIS. **SAFE-TO-CLEAR.**

**agent-a9df74a9…**: detached at fe19c98f2, in F44. `scratch/rereview/sweep_real.py`, a
one-shot sweep probe, and a stamp file. DEBRIS. **SAFE-TO-CLEAR.**

**agent-abe52abf…** (`review/lxc-checkout-typed-rereview`, ef0d2ac29, held by its archive
ref). QA and test logs; `scratch/mergetree/src-go/` and `scratch/red/src-go/`, tree copies
made for red/green (about 3,200 files). DEBRIS. **SAFE-TO-CLEAR.**

**agent-abf35077…**: detached at a86557067, held by `agent-a8c69aa…` (local and origin).

- IN-BRANCH: `scratch/review.md`, the DBF conformance review. It is byte-for-byte the text of
  `CLAUDE/Plan/00177-…/subagent-reports/261023-dbf-conformance-exitcode-kind.md` on
  `agent-a8c69aa…`.
- DEBRIS: `red.out` and `green.out`.

**SAFE-TO-CLEAR** while that branch is kept.

**agent-ac5d0762…** (`feat/agent-bus-v2-design-2`, ba521348f, held by many refs, its archive
ref included).

- IN-F44: `scratch/00209-design-new.txt`, `00209-design-tail.txt` and
  `00209-journal-rework.txt`; every sampled line is in F44.
- DEBRIS: `00209-commit-msg.txt`.

**SAFE-TO-CLEAR.**

**agent-aca5e949…** (`go-qa-ci/adopt`, 4b60da906, held by its archive ref).

- IN-BRANCH: `scratch/journal-body.md`, `journal2.md` and `journal3.md`, which are in the
  branch's 00191 JOURNAL.
- DEBRIS:
  - about 25 top-level `*.txt` files of go-qa, test and coverage output (`t.txt` is 2.6 MB);
  - the one-shot edit scripts in `scratch/*.py`;
  - the `msg*.txt` drafts;
  - `repos/go-qa-ci/`, a clone of the upstream repository.

**SAFE-TO-CLEAR** while the archive ref is kept.

**agent-ad49d488…** (`agent-ad49d4886a70d25db-01b66214`, a18f28610, on origin; WANTED, ccy
go-cache).

- DEBRIS:

  - the QA logs, `gc.cov` and `commit-msg.txt`;
  - `scratch/gocache-r3/`: a Go build cache copy of about 1,360 files, a 10 MB `ccy-teams`
    binary and a swap script;
  - `scratch/gocache-r4/` and `gocache-symlink/`: probe programs;
  - `plan-runs/00194…`, finished runs.

- UNIQUE: the six files below. Neither F44 nor the branch holds them:

  - `agent-reports/261023-ccy-go-cache-opus-5-5.md`: the build report, covering the design,
    the measurements and the problem. The problem itself is F44's
    `00213-…/subagent-reports/261023-ccy-shutdown-hang.md`.
  - `agent-reports/261023-ccy-go-cache-review-r1.md`, `-r2.md`, `-r3.md` and `-r4.md`: four
    adversarial review rounds. r4 found blocker B1 (the docker volume).
  - `agent-reports/261023-go-cache-handoff.md`: the state at wind-down and the non-blocking
    round 4 items. `scratch/261023-go-cache-handoff.md` is byte-identical, so only one copy needs
    keeping.

  **HAS-UNIQUE.**

**agent-ae87230b…** (`agent-ae87230b6e5b05508-967f0e77`, 959a79b70, on origin; WANTED, 00173).

- IN-BRANCH: `scratch/plan173-journal-owner-test.md` and `plan173-journal-cc-finding.md`,
  which are in the branch's 00173 JOURNAL.

- DEBRIS: `plan173-test.log` and the two commit-message drafts.

- UNIQUE: `scratch/plan-173-followup.md`, the follow-up report to the lead. It explains why no
  host cc session can be shown the shutdown warning (read from the code), names the two routes
  that could show it, and lists the decisions still open for the owner. It is not in the
  branch: the journal entries cover only part of it, and the open-decision list is not in them.

  **HAS-UNIQUE.**

**agent-aee97f4c…**: detached at e6b7ba207, held by `archive/wip-rescue/…`. `scratch-*.log`
and `*.list` files, and a test-runner script. DEBRIS. **SAFE-TO-CLEAR.**

**agent-afecb652…** (`feat/pingbus-compact-before-wake`, 6d3d61abe, held by its archive ref
and by the origin review branch).

- IN-BRANCH: `scratch/journal-compact-wake.md`, which is in the branch's 00194 JOURNAL.
- DEBRIS: `usage_probe.py`, `compact_probe.py` and `binary_probe.py`, read-only probes whose
  facts are in the branch's `SPEC-compact-wake.md`; the test runner and its output; and the
  commit draft.

**SAFE-TO-CLEAR** while the archive ref is kept.

**dbf-cfd-2** (`dbf/cfd-2`, 35e854658, held by its archive ref). No `untracked/` content.
**SAFE-TO-CLEAR.**

**plan-team-liaison-role** (`plan-team-liaison-role`, fbbc19094, on origin; WANTED, 00225).
`scratch/261023-liaison-owner-input.md` is IN-BRANCH: the tip commit records it in the 00225
JOURNAL. **SAFE-TO-CLEAR** while the branch ref is kept.

### `untracked/worktrees/` (25)

**bus-v2-m1-matrix** (`feat/bus-v2-m1-matrix`, 58c7b6e1a, on origin).

- IN-F44: `scratch/261021-m0-plan-review.md`. F44 has
  `00209-…/subagent-reports/261021-m0-plan-review.md`, and 7 of its 8 sampled lines match.
- DEBRIS: the commit-message drafts, `m1-qa-changed.log`, and `scratch/v1/`, copies of v1
  Python and SPEC files.

**SAFE-TO-CLEAR.**

**bus-v2-m1-proto** (`feat/bus-v2-m1-proto`, a04c0745d, held by the proto-\* branches and
their archive refs).

- DEBRIS (superseded): `scratch/261021-m1-proto-handoff.md` and `m1proto/WORKER-BRIEF.txt`.
  F44 dropped `bus/proto` for `teamchat/proto`. The Unicode facts in the handoff are coded in
  `unicode15*.go` on the branch and discussed in F44's 00209 DESIGN.
- DEBRIS: the `cats.go`, `cmp.py` and `nfkc.py` probes, `gocats.txt` (3.3 MB), and
  `gen/grammar.py`, the generator of a superseded corpus.

**SAFE-TO-CLEAR.**

**bus-v2-m1-proto-envelope**, **-limits**, **-ping** and **-rooms**: each on its
`feat/bus-v2-m1-proto-*` branch and held by its archive ref. Their contents are corpus
generators for the superseded `bus/proto` (`gen_envelope.py`, `gen.py`, `gen/ping.py`), red
and green test output, lint logs and commit drafts. DEBRIS. **SAFE-TO-CLEAR**, all four.

**bus-v2-m1-state** (`feat/bus-v2-m1-state`, held by its archive ref). One-shot edit scripts
(`fix*.py`, `trim*.py`, `ren.py`, `copyip.py`), a draft test for the superseded `bus/state`,
a QA log and a message draft. DEBRIS. **SAFE-TO-CLEAR.**

**bus-v2-m1-state-locks** (`feat/bus-v2-m1-state-locks`, held by its archive ref). Mutation
and test logs, `cover.out`, and `scratch/mut/` and `mut2/`, tree copies made for mutation runs
(about 6,100 files). DEBRIS. **SAFE-TO-CLEAR.**

**bus-v2-m2-hooks** (`feat/bus-v2-m2-hooks`, bd6dcc8fa, held by its archive ref).

- DEBRIS (superseded): `261021-m2-hooks-brief.md`, `-golden-task.md` and `-impl-task.md`, the
  working briefs for a port that F44's 00209 PLAN calls superseded. The outcome is in the
  archived branch's `261021-m2-hooks-golden.md` and `-impl-notes.md`.
- DEBRIS: `261021-m2-hooks-cost/`, a timing run whose result is the branch's
  `261021-m2-hooks-python-cost.md`, together with a test world and `pingbus.pyz`; and `m2/`
  splice and lint-fix scripts and code fragments.

**SAFE-TO-CLEAR.**

**bus-v2-m2-hooks-golden** (`feat/bus-v2-m2-hooks-golden`, held by its archive ref and by
`feat/bus-v2-m2-hooks`).

- DEBRIS (superseded): the brief, byte-identical to the m2-hooks copy, and
  `261021-m2-hooks-golden-handoff.md`. The branch's committed golden report records the
  outcome, oddities included.
- DEBRIS: the scenario generators, the QA logs and the commit draft.

**SAFE-TO-CLEAR.**

**bus-v2-n3b-qar**, **n3c-qar**, **n3d-qarev** and **n3f-rev**: detached, every HEAD in F44.
No `untracked/` content. **SAFE-TO-CLEAR.**

**bus-v2-n3b-rv2**, **-rv3** and **-rva**; **n3c-rv2** and **-rv3**; **n3d-rva** and
**-rvb**: detached, each held by `origin/wip-rescue/bus-v2-<name>`. Review test output, diffs,
`stat` and `dlist` dumps, and the probe tests and overlays in `n3b-rva`. DEBRIS.
**SAFE-TO-CLEAR**, all seven.

**bus-v2-n3b-rvb**: detached at 0f2f54a31, in F44. Test output, a lease probe and two `tmp*`
directories. DEBRIS. **SAFE-TO-CLEAR.**

**bus-v2-n3e-seam** (`feat/bus-v2-n3e-seam`, 2691c4e72, held by its archive ref).

- IN-F44: `scratch/n3e-seam-journal.md`; every sampled line is in F44.
- DEBRIS: the commit drafts; the `rev/` run logs and `driver` binary; and `rev/t-8d2496eba/`
  and `t-de71902e4/`, tree copies made for red/green (about 8,100 files).

**SAFE-TO-CLEAR.**

**bus-v2-s1-declare** (`feat/bus-v2-s1-declare`, 11d722039, held by its archive ref).

- DEBRIS:

  - the working briefs (`261021-s1-declare-brief.md`, `-task2-brief.md`, `-task34-brief.md`,
    `-fix1-brief.md` and `-merge-brief.md`). Their work landed in F44 and is reported in
    `00209-…/subagent-reports/261021-s1-*.md`.
  - about 40 QA, suite and timing logs, and the commit and message drafts;
  - the one-shot edit scripts and the `.bak` copies;
  - `keytime/`, two `ccy-teams` binaries;
  - `timing/`, three tree copies (about 10,800 files).

- UNIQUE: the four files below:

  - `scratch/261021-s1-declare-reader-notes.md`: the builder notes for F44's live
    `src-go/internal/bus/declare`. They give the exported API, the red and green record, and
    the mutation proof that the parity test can fail; that proof found a fixture gap
    (`comment-whitespace.yml`). F44's coordinator report summarises them but does not hold
    them.
  - `scratch/261021-s1-pycompat-notes.md`: the same kind of notes for F44's
    `src-go/internal/bus/pycompat`. 1 of 8 sampled lines is in F44.
  - `scratch/261021-s1-seat-reader-notes.md`: the same for F44's `src-go/internal/bus/seat`
    and `agentbus.SeatList`. 1 of 8 sampled lines is in F44.
  - `scratch/261021-s1-declare-gen-fixtures.py`: the generator of the 138 byte-exact parity
    fixtures F44 ships under `src-go/internal/bus/declare/testdata/parity/` (CRLF, BOM, NEL,
    invalid UTF-8). No ref holds it, so the fixtures cannot be regenerated without it.

  **HAS-UNIQUE.**

  Also: F44's `261021-s1-declare.md` cites `untracked/scratch/261021-s1-timing.bash`, which
  exists only here. It is a timing script (DEBRIS), but once it is cleared the citation points
  at nothing.

## Summary table

| Worktree                                                 | HEAD                        | Reachable from        | Verdict                     |
| -------------------------------------------------------- | --------------------------- | --------------------- | --------------------------- |
| agent-a08350fb…                                          | detached 2ddf10069          | origin/F44            | SAFE-TO-CLEAR (empty)       |
| agent-a272344d…                                          | review/teams-wait-play-lock | archive               | SAFE-TO-CLEAR               |
| agent-a2c0593f…                                          | detached d92b58eb0          | archive/wip-rescue    | SAFE-TO-CLEAR               |
| agent-a30eec90…                                          | review/…-rereview           | archive               | SAFE-TO-CLEAR (empty)       |
| agent-a374a700…                                          | review/teams-full-handles   | archive               | SAFE-TO-CLEAR               |
| agent-a38fa8d7…                                          | detached 6c4d14423          | archive/wip-rescue    | SAFE-TO-CLEAR               |
| agent-a5b2a7eb…                                          | detached d81618505          | origin/F44            | SAFE-TO-CLEAR (empty)       |
| agent-a5c8b56d…                                          | detached 72c7dbca6          | archive/wip-rescue    | SAFE-TO-CLEAR               |
| agent-a5ee80f7…                                          | own branch c9dd59437        | origin                | **HAS-UNIQUE** (4)          |
| agent-a761340e…                                          | detached 42d3174fc          | origin/F44            | SAFE-TO-CLEAR               |
| agent-a81014c6…                                          | own branch c04bb307f        | origin                | SAFE-TO-CLEAR (IN-BRANCH)   |
| agent-a83a9b8f…                                          | review/pingbus-compact-…    | origin                | SAFE-TO-CLEAR               |
| agent-a85b0ccf…                                          | detached ec39effee          | archive/wip-rescue    | SAFE-TO-CLEAR (empty)       |
| agent-a8c69aa6…                                          | own branch 2250df65a        | origin                | SAFE-TO-CLEAR (IN-BRANCH)   |
| agent-a9351bb9…                                          | detached 87ef7c397          | origin/F44            | SAFE-TO-CLEAR               |
| agent-a9df74a9…                                          | detached fe19c98f2          | origin/F44            | SAFE-TO-CLEAR               |
| agent-abe52abf…                                          | review/lxc-…-rereview       | archive               | SAFE-TO-CLEAR               |
| agent-abf35077…                                          | detached a86557067          | agent-a8c69aa… origin | SAFE-TO-CLEAR (IN-BRANCH)   |
| agent-ac5d0762…                                          | feat/agent-bus-v2-design-2  | archive and others    | SAFE-TO-CLEAR (IN-F44)      |
| agent-aca5e949…                                          | go-qa-ci/adopt              | archive               | SAFE-TO-CLEAR (IN-BRANCH)   |
| agent-ad49d488…                                          | own branch a18f28610        | origin                | **HAS-UNIQUE** (6, one dup) |
| agent-ae87230b…                                          | own branch 959a79b70        | origin                | **HAS-UNIQUE** (1)          |
| agent-aee97f4c…                                          | detached e6b7ba207          | archive/wip-rescue    | SAFE-TO-CLEAR               |
| agent-afecb652…                                          | feat/pingbus-compact-…      | archive, origin       | SAFE-TO-CLEAR (IN-BRANCH)   |
| dbf-cfd-2                                                | dbf/cfd-2                   | archive               | SAFE-TO-CLEAR (empty)       |
| plan-team-liaison-role                                   | own branch fbbc19094        | origin                | SAFE-TO-CLEAR (IN-BRANCH)   |
| bus-v2-m1-matrix                                         | feat/bus-v2-m1-matrix       | origin                | SAFE-TO-CLEAR (IN-F44)      |
| bus-v2-m1-proto                                          | feat/bus-v2-m1-proto        | archive (descendants) | SAFE-TO-CLEAR               |
| bus-v2-m1-proto-envelope, -limits, -ping, -rooms         | own feat/ branch            | archive               | SAFE-TO-CLEAR               |
| bus-v2-m1-state, -state-locks                            | own feat/ branch            | archive               | SAFE-TO-CLEAR               |
| bus-v2-m2-hooks, -hooks-golden                           | own feat/ branch            | archive               | SAFE-TO-CLEAR               |
| bus-v2-n3b-qar, n3c-qar, n3d-qarev, n3f-rev              | detached                    | origin/F44            | SAFE-TO-CLEAR (empty)       |
| bus-v2-n3b-rvb                                           | detached 0f2f54a31          | origin/F44            | SAFE-TO-CLEAR               |
| bus-v2-n3b-rv2, -rv3, -rva; n3c-rv2, -rv3; n3d-rva, -rvb | detached                    | origin/wip-rescue/\*  | SAFE-TO-CLEAR               |
| bus-v2-n3e-seam                                          | feat/bus-v2-n3e-seam        | archive               | SAFE-TO-CLEAR (IN-F44)      |
| bus-v2-s1-declare                                        | feat/bus-v2-s1-declare      | archive               | **HAS-UNIQUE** (4)          |

Every HEAD is reachable from origin/F44 or from a ref on origin; no HEAD would be lost with its
worktree. "SAFE-TO-CLEAR (IN-BRANCH)" holds only while that branch's ref (origin head or
`refs/archive`) is kept. If the owner deletes one of those refs, its notes go with it.

## Every UNIQUE file, and where it belongs

Each target is a plan's `subagent-reports/`. Before committing, scrub absolute paths, which
the handoffs contain, to `<repo>`, and replace cached-spec and plugin paths with placeholders.

| File                                                                      | Belongs in                                                                                                   | Name                                |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| agent-a5ee80f7…/untracked/agent-reports/261023-ccy-pids-limit.md          | 00181-ccy-session-limits (related, Not Started; no plan owns the branch, so the owner may pick one)          | `261023-ccy-pids-limit.md`          |
| agent-a5ee80f7…/untracked/agent-reports/261023-ccy-pids-limit-review-1.md | 00181                                                                                                        | `261023-ccy-pids-limit-review-1.md` |
| agent-a5ee80f7…/untracked/agent-reports/261023-ccy-pids-limit-review-2.md | 00181                                                                                                        | `261023-ccy-pids-limit-review-2.md` |
| agent-a5ee80f7…/untracked/scratch/261023-a5ee80f767118a918-handoff.md     | 00181                                                                                                        | `261023-ccy-pids-limit-handoff.md`  |
| agent-ad49d488…/untracked/agent-reports/261023-ccy-go-cache-opus-5-5.md   | no owning plan; nearest is 00213-agent-bus-performance-go-carve-out, which holds the problem report it cites | `261023-ccy-go-cache.md`            |
| agent-ad49d488…/untracked/agent-reports/261023-ccy-go-cache-review-r1.md  | 00213 (or the plan that takes the branch)                                                                    | `261023-ccy-go-cache-review-r1.md`  |
| agent-ad49d488…/untracked/agent-reports/261023-ccy-go-cache-review-r2.md  | same                                                                                                         | `261023-ccy-go-cache-review-r2.md`  |
| agent-ad49d488…/untracked/agent-reports/261023-ccy-go-cache-review-r3.md  | same                                                                                                         | `261023-ccy-go-cache-review-r3.md`  |
| agent-ad49d488…/untracked/agent-reports/261023-ccy-go-cache-review-r4.md  | same                                                                                                         | `261023-ccy-go-cache-review-r4.md`  |
| agent-ad49d488…/untracked/agent-reports/261023-go-cache-handoff.md        | same (the `scratch/` copy is identical; keep one)                                                            | `261023-ccy-go-cache-handoff.md`    |
| agent-ae87230b…/untracked/scratch/plan-173-followup.md                    | 00173-shutdown-with-update-always-goes-down                                                                  | `261013-evening-run-cc-warning.md`  |
| bus-v2-s1-declare/untracked/scratch/261021-s1-declare-reader-notes.md     | 00209-agent-bus-v2-in-go                                                                                     | `261021-s1-declare-builder.md`      |
| bus-v2-s1-declare/untracked/scratch/261021-s1-pycompat-notes.md           | 00209                                                                                                        | `261021-s1-pycompat-builder.md`     |
| bus-v2-s1-declare/untracked/scratch/261021-s1-seat-reader-notes.md        | 00209                                                                                                        | `261021-s1-seat-builder.md`         |
| bus-v2-s1-declare/untracked/scratch/261021-s1-declare-gen-fixtures.py     | 00209 plan folder (plan-local scaffolding; it then falls under the ruff gate)                                | `s1-declare-gen-fixtures.py`        |

The first three groups follow the WANTED branches in BRANCH-OUTCOMES. If the owner drops a
branch, its reports can go into that branch before it is archived instead of into F44. Either
way, they must be committed somewhere before the worktree is cleared.

## Notes for the coordinator

- `untracked/scratch/evacuated-worktrees/` in the main checkout already holds copies, by name, for
  eleven of these worktrees: `agent-a272344d…`, `agent-a30eec90…`, `agent-a374a700…`,
  `agent-abe52abf…`, `agent-aca5e949…`, and `bus-v2-m1-proto`, `-m1-state`,
  `-m1-state-locks`, `-m2-hooks`, `-m2-hooks-golden` and `-s1-declare`. Whether those copies
  are complete was not checked. Those copies are themselves
  untracked. Nothing in them changes the verdicts above.
- The repository copies (`umask-dbf-review-*`, `mergetree`, `mut*`, `t-*`, `timing/`) were
  judged by path only. Their `vault.bash` files are the repository's script, not secrets, and
  none was opened.
- A deviation from read-only: this review wrote one file outside this report. It is
  `untracked/scratch/wtlist-review.txt`, the worktree list, in the main checkout's ignored
  scratch. It can be deleted.
