# Plan 00128: a missing dev dependency silences thirty QA gates

**Status**: In Progress (Phase 1 measurement done — the cost is 31 gates; Phase 2 is the owner's decision gate)
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

**Phase 1 measured it at 31 gates, and found the sharpest version of the problem in the
same run.** On a fresh clone `ansible-syntax` fails **83 times** — every failure printed,
the gate named in the census, and the run carries on, which is Task 4.3 working. Then `js`
cannot find a tool, prints two lines, and ends the run. So a comprehensively broken gate
costs its own verdict and nothing else, while a gate whose optional dev tool is absent
costs thirty-one other gates theirs. The suite punishes the milder condition far harder
than the severe one. [FINDINGS.md §2](FINDINGS.md)

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

- [x] ✅ **Task 1.1**: Measured on a real `git clone` into `untracked/scratch/` rather
  than by moving the directory aside — a clone IS the population, since
  `extensions/node_modules` is gitignored, and it leaves the working tree untouched.
  **7 gates report on a fresh clone against 38 here: 31 lose their verdict entirely.**
  [FINDINGS.md §1](FINDINGS.md)
- [x] ✅ **Task 1.2**: Seven `exit 2` aborts, all the same shape.
  [FINDINGS.md §4](FINDINGS.md). Two things the census settles: `qa-docs`'s abort is NOT
  a missing tool — it means the checker produced no usable result, a branch 00125 added
  so a crash could not read as a clean run, and its meaning must survive Phase 3. And the
  js gate is the **only** one whose missing input is dev-only; the other five name tools a
  provisioned host has anyway, which is why the conflict lands here and nowhere else
- [ ] ⬜ **Task 1.3**: Establish who actually hits this — a fresh clone, a linked
  worktree, CI (green only because `.github/workflows/qa.yml` runs `npm ci`), and the CCY
  container. 00125 found `CLAUDE/QA.md` and its own Non-Goal disagreeing about that
  population, so read the current text rather than either summary
- [ ] ⬜ **Task 1.4**: A plan-local `triage.bash` wrapping Task 1.1's measurement, so the
  7-versus-38 number is re-derivable after Phase 3 rather than being a figure in a
  document. Built on `_planlib.inc.bash` per `CLAUDE/PlanScriptStandards.md`

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
  verdict — pass, fail, or not-run-and-why — for **every** gate. Measured against Phase
  1's baseline: the fresh-clone count rises from **7** toward the **38** a complete
  checkout names, with any remaining shortfall explained per gate rather than silent
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
