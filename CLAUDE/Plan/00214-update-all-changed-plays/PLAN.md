# Plan 00214: update all changed plays

**Status**: In Progress
**Created**: 2026-10-22
**Owner**: joseph
**Priority**: Medium
**Issue**: #110

## Overview

One command should bring a host up to date: every play that has to run again here, run
without anyone listing them. `./run.bash --changed` already does that. Issue #110 names
three gaps around it: the health report and its startup notice never say so, the report and
the runner counted different plays (10 against 25 on one host), and the meta-deploy queue
had no way to say "and every changed play", so agents hand-copied stale plays into it.

The root cause of the 10 against 25 is that the two counted with different rules. The
report (`helpers/play_ledger/check_freshness.py`) judged a play only on commits touching
its own file between the commit it ran from and HEAD, plus a hash check on a dirty run.
`run.bash --changed` (`helpers/play_ledger/changed_plays.py`) judges it on its own file and
every checkout path it deploys, diffed against the working tree, and also runs a play whose
last run failed and every play that ran from a dirty checkout. So the report missed every
play whose template, lib or helper changed, every failed play, every dirty run whose play
file was unchanged, and every uncommitted edit. This plan makes `changed_plays.judge` the
one definition, read by both.

## Goals

- The health report's play-freshness section names exactly the plays `run.bash --changed`
  runs, from one shared judgement, and its coverage says what it leaves out.
- The full report (`fedora-desktop-health`, the once-a-day login report) and the one-line
  startup reminder print the copy-paste command that clears them: this checkout's own
  path, resolved at runtime, plus `run.bash --changed`.
- `meta-deploy.list` has a `changed-plays` entry that runs `run.bash --changed --yes` at
  its place in the list.

## Non-Goals

- **Gap 4, a panel action "Update everything"**: declined (YAGNI). The startup notice and
  the report now name the one command, and the panel's existing "Re-run a play…" row opens
  `run.bash --rerun`, where `a` picks every changed play.
- Converting the current queue's hand-listed playbook entries, which belong to other plans
  and carry their own ordering.

## Tasks

### Phase 1: one definition of a stale play (gap 2)

- [x] ✅ **Task 1.1**: Red test first: `TestAgreesWithRunBashChanged` in
  `tests/helpers/play_ledger/test_check_freshness.py` builds a real repository and ledger
  with a play whose deployed file changed, one whose last run failed, one that ran dirty,
  one with an uncommitted edit and one current, and asserts the report names exactly the
  plays `changed_plays.run` prints. It failed with the report naming none of the four.
- [x] ✅ **Task 1.2**: `changed_plays.judge` returns a `Judgement` (states, gone, unresolved,
  changed inputs, dirty runs, successors and the plays it does not judge) and raises
  `NoAnswer`; `changed_plays.run` prints from it. `check_freshness.run` reads the same
  judgement; its own play-file history (`git_history.changes_since`,
  `play_sha256_at_head`, `freshness.classify`, `freshness.retire`, the UNEXPLAINED state)
  is deleted. A finding's detail is the changed paths, the dirty run or the play a
  successor takes over. A failed last run is its own finding.
- [x] ✅ **Task 1.3**: The report's coverage sentence (`check_freshness.coverage_sentence`)
  reaches the status document's play-freshness section from `login_report`, naming the
  plays that hold a reference nobody can follow and the ledgered plays outside
  `playbooks/imports/`. Only a run that judged states one, as with the pins.
- [x] ✅ **Task 1.4**: The panel's "Re-run a play…" count counts the plays `--changed` runs:
  every state but `fresh` and `unresolved`, so a state the panel has no words for still
  counts.

### Phase 2: name the command (gap 1)

- [x] ✅ **Task 2.1**: Test first, then `login_message`: when the document's plays include
  a `stale` or `failed` one, the full report prints, below the faults, the command that
  runs them, this checkout's path (from the helper's own location, quoted) plus
  `run.bash --changed`, and the one-line reminder names the same command. No real path is
  written into the repo. The desktop notification is left alone: it cannot be copied
  from, and it already sends the reader to `fedora-desktop-health`.

### Phase 3: meta-deploy can say "every changed play" (gap 3)

- [x] ✅ **Task 3.1**: `scripts/test-meta-deploy.bash` first (it failed on the new cases),
  then `meta-deploy.bash`: a `changed-plays` entry runs the checkout's
  `run.bash --changed --yes` as one unit at its place in the list, refuses words after it,
  and is named `run.bash --changed --yes` in the summary. The script shrank (161 code
  lines, 180 statements; the ratchet record tightened).
- [x] ✅ **Task 3.2**: Ordering semantics documented in `meta-deploy.list`'s header: the
  entry runs where it stands and decides which plays when it starts, so it goes above the
  plan entries that expect those plays to have run. AgentNotes, PlanWorkflow and QA.md
  point at it.

### Phase 4: host check and review

- [ ] ⬜ **Task 4.1**: A read-only `acceptance.bash` that runs the report's check and the
  runner's helper on the host and requires they name the same plays, and that the report
  prints this checkout's `run.bash --changed`; queued in `meta-deploy.list`.
- [ ] ⬜ **Task 4.2**: Docs (`docs/playbooks.md`) say the report and `--changed` share one
  judgement, and what the startup notice now prints.
- [ ] ⬜ **Task 4.3**: Adversarial Opus review to MERGE, then the qa-reviewer agent.

## Success Criteria

- [ ] On the same checkout and ledger, the report's changed and failed plays are the plays
  `run.bash --changed` lists, proved by a test on a real repository.
- [ ] The report and the startup reminder print `<this checkout>/run.bash --changed`.
- [ ] A `changed-plays` line in `meta-deploy.list` runs every changed play at its place.

## Delivery & Milestones

- Branch `feat/update-all-changed-plays`.
