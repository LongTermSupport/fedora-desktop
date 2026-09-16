# Plan 00128: a missing dev dependency silences thirty QA gates

**Status**: Not Started
**Created**: 2026-09-16
**Owner**: joseph
**Priority**: Medium

## Overview

`./scripts/qa-all.bash` is mandatory before every commit. On a fresh clone it exits 2
before roughly thirty of its gates have run, and reports nothing about any of them.

The mechanism is one gate: `scripts/qa-js.bash` needs
`extensions/node_modules/.bin/eslint`, and when that is absent it prints setup guidance
and exits 2. `scripts/qa-all.bash:140-146` turns that into a whole-suite abort. The js
gate sits ahead of `qa-docs.bash` and every gate after it, so a single missing dev
dependency costs the verdicts of all of them.

**This is not an oversight, and that is what makes it a plan rather than a patch.** Two
positions are written down in this repository and they conflict. `CLAUDE.md`'s
*"Missing Dependencies — Fail Fast, Fix in IaC"* rule says never accept a missing
dependency and add it to the relevant playbook. `scripts/qa-js.bash:107-112` says these
deps are dev-only, that no playbook installs them, that most users never edit the
GNOME-extension JavaScript, and that auto-installing them *"would force dev tooling on
every install."* Both are deliberate. Something has to give, and which thing gives is the
owner's decision — which is exactly why
[Plan 00125](../Completed/00125-ci-qa-gate-red-and-machine-dependent/PLAN.md) measured
this, costed it, and left it to its own plan rather than closing it.

The defect class is Plan 00125's own subject one level up. That plan was about a check
whose clean result is indistinguishable from a blind one; its Task 4.3 made the suite
report every hard gate instead of aborting at the first failure, and **deliberately kept
the seven `exit 2` missing-tool aborts**. So this gap survives 00125 by design, at the
suite's exit path rather than inside a gate: a run that says nothing about thirty gates
looks the same as a run that had nothing to say about them.

## Goals

- **Decide, in writing, where extension dev tooling belongs** — the desktop provision, a
  separate bootstrap, or nowhere — and record the reasoning where the next reader of
  `qa-js.bash` will find it.
- **A fresh clone's `qa-all.bash` reports a verdict for every gate it did not run**, or
  runs them. A missing dev dependency must not be able to cost an unrelated gate its
  verdict silently.
- **Resolve the contradiction rather than pick a side by accident.** Whichever way it
  goes, `CLAUDE.md`'s missing-dependency rule and `qa-js.bash`'s comment must agree
  afterwards — including, if that is the answer, the rule gaining a stated exception for
  dev-only tooling.

## Non-Goals

- **Auto-installing node deps from a QA script.** `qa-js.bash` already refuses to, and
  the reason it gives is sound: a QA gate that mutates the checkout to make itself pass
  is not a gate.
- **The vault password file**, the other missing-input gap 00125 named. It is human-only
  — the path is `secret_file_guard`-protected, so an agent cannot name it in a command, a
  script or a playbook task. Tracked as Plan 00109 Task 0.3.
- **Changing what ESLint checks**, or the extension JavaScript itself.
- **Removing the `exit 2` convention.** A missing tool genuinely is a different result
  from a failing check, and 00125 kept that distinction on purpose. The question is what
  it costs, not whether it exists.

## Tasks

### Phase 1: Establish the cost, and who pays it

- [ ] ⬜ **Task 1.1**: A plan-local `triage.bash` that measures, on this checkout, how
  many gates `qa-all.bash` skips when `extensions/node_modules` is moved aside, and which
  ones — the probe goes IN the script per `CLAUDE/PlanTriage.md`. 00125's FINDINGS gives
  the shape of the answer but not the number; a count asserted from reading is the thing
  this repo keeps getting wrong
- [ ] ⬜ **Task 1.2**: Enumerate every `exit 2` tool-abort in `qa-all.bash` and what each
  one guards, so the decision is taken against the whole population and not just the js
  one. 00125's census put it at seven
- [ ] ⬜ **Task 1.3**: Establish who actually hits this — a fresh clone, a linked
  worktree, CI (green only because `.github/workflows/qa.yml` runs `npm ci`), and the CCY
  container. 00125 found `CLAUDE/QA.md` and its own Non-Goal disagreeing about that
  population, so read the current text rather than either summary

### Phase 2: The decision gate

- [ ] 🚫 **Task 2.1**: **OWNER'S DECISION — where extension dev tooling belongs.** Three
  options, and Phase 1 is what makes them comparable rather than a matter of taste:

  **A. A playbook installs it.** Closes the IaC gap as `CLAUDE.md` literally requires.
  Costs node deps on every desktop provision for tooling most users never invoke, which
  is the objection `qa-js.bash` already records.

  **B. A separate dev bootstrap** — an optional play, or a documented one-time
  `cd extensions && npm ci`. Keeps the provision lean and keeps the rule's spirit (the
  dependency is owned by IaC, just not by the default path). Costs a second setup step
  that a fresh clone must know about, which is how the current state arose.

  **C. Neither — the rule gains a stated exception**, and the abort stops being total.
  Cheapest, and it concedes that a dev-only tool's absence is not the same kind of fact
  as a missing runtime dependency. Requires editing `CLAUDE.md`, which is the owner's.

  **Not blocked on being answered before Phase 3**: Task 3.1 is worth doing under any of
  the three, because none of them makes it correct for one gate's missing tool to erase
  another gate's verdict.

### Phase 3: A tool abort stops costing unrelated gates their verdict

- [ ] ⬜ **Task 3.1**: `qa-all.bash` continues past a missing-tool abort and reports the
  gate as `⚠ <name>: not run — <tool> absent`, distinct from both a pass and a failure,
  and the run's exit status still reflects that something was not established. The three
  states must be distinct in the OUTPUT, not merely in the exit code — a reader looking
  at the summary is the one who has to tell them apart
- [ ] ⬜ **Task 3.2**: A gate for it. The suite must fail if a tool abort can again
  silence a gate that would otherwise have run — measured by mutating one gate's tool
  precondition and asserting the census still names every other gate, the same technique
  00125's Task 4.3 used
- [ ] ⬜ **Task 3.3**: Reconcile `CLAUDE/QA.md` and `scripts/qa-js.bash`'s comment with
  whatever Task 2.1 decided. Both currently describe the present behaviour correctly, so
  both go stale the moment Phase 3 lands
- [ ] ⬜ **Task 3.4**: `qa-reviewer` over the whole diff before this plan is Complete

## Success Criteria

- [ ] On a checkout with no `extensions/node_modules`, `./scripts/qa-all.bash` reports a
  verdict — pass, fail, or not-run-and-why — for **every** gate, and the count of
  gates it names matches the count on a checkout that has the deps
- [ ] The js gate's absent tooling is still reported loudly, and is still distinguishable
  from a passing ESLint run
- [ ] Task 2.1's decision is recorded with its reasoning, and `CLAUDE.md`,
  `CLAUDE/QA.md` and `scripts/qa-js.bash` agree with it and with each other
- [ ] Mutating a gate's tool precondition fails the new gate — falsified, not assumed
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` findings resolved

## Delivery & Milestones

- Origin: Plan 00125's Non-Goal and its `FINDINGS.md` section *"The `qa-js.bash` gap is
  not worktree-only, and it sits in front of this plan's deliverable"* — measured there,
  and left for this plan with the remedy named as the owner's decision
