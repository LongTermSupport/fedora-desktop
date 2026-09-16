# Plan 00128 — Findings

Measured evidence for the tasks in [PLAN.md](PLAN.md). Everything here was produced by
running something, not by reading code and inferring.

## 1. The cost is 31 gates, measured on a real fresh clone (Task 1.1)

Not "roughly thirty" from reading the script. A genuine `git clone` of this repository
into `untracked/scratch/`, which is the population by construction — `extensions/node_modules`
is gitignored, so a clone never has it — then `./scripts/qa-all.bash` in that clone.

| Checkout          | Distinct gates that reported a verdict | `qa-all.bash` exit |
| ----------------- | -------------------------------------- | ------------------ |
| fresh clone       | **7**                                  | 2                  |
| this working tree | **38**                                 | 0                  |

The seven that report are `shellcheck`, `bash`, `python`, `patterns`, `ansible`,
`ansible-syntax`, `js`. Everything after the js gate is never invoked: **31 gates produce
no verdict of any kind**, and the run says nothing about them.

Static cross-check on the script, agreeing from the other direction: 29 gate invocations
appear after the js gate's line in `qa-all.bash`, against 6 before it. The two numbers
differ because some gates are invoked more than once and some names appear only in
comments — the empirical 7-versus-38 is the one to quote.

## 2. The same run shows both failure kinds side by side, and they cost wildly different amounts

This is the sharpest statement of the problem, and it was not visible until the fresh
clone was actually run.

In that **one** run:

- `ansible-syntax` **failed 83 times** — the clone has no `roles/` (galaxy dependencies
  are not vendored), so every playbook fails to parse. Every one of those failures is
  printed, the gate is named in the census, and **the run carries on**. That is Plan
  00125's Task 4.3 working exactly as designed.
- `js` **could not find a tool**, printed two lines, and **ended the run**, taking 31
  gates' verdicts with it.

So a gate that is comprehensively broken costs its own verdict and nothing else, while a
gate whose optional dev tool is absent costs thirty-one other gates their verdicts. The
suite treats the milder condition far more harshly than the severe one.

## 3. A raw clone has a second, unrelated prerequisite — do not over-claim the measurement

The `ansible-syntax` failures above are **not** a finding of this plan. A raw `git clone`
is not the same thing as a developer checkout after setup: CI installs galaxy
dependencies and writes a vault placeholder before running QA, and a developer following
the documented setup would too.

Stated here so the 7-versus-38 number is not read as "a fresh clone is 83-ways broken".
The 31 lost verdicts are caused by the js gate's abort alone, and would still be lost on a
clone that had `roles/` — the abort happens after `ansible-syntax` either way. The
ansible-syntax result is evidence for §2's contrast, not a defect this plan owns.

## 4. The seven tool aborts, and what each guards (Task 1.2)

All seven share the shape `if [[ $rc -eq 2 ]]; then echo ERROR >&2; exit 2; fi`:

| Gate                | Guards                                                     |
| ------------------- | ---------------------------------------------------------- |
| `qa-bash`           | shellcheck / bash tooling                                  |
| `qa-python`         | ruff / python tooling                                      |
| `qa-patterns`       | `semgrep` — names `pipx install semgrep`                   |
| `qa-ansible`        | ansible tooling                                            |
| `qa-ansible-syntax` | `ansible-playbook`                                         |
| `qa-js`             | node **and** `extensions/node_modules`                     |
| `qa-docs`           | NOT a missing tool — a zero-file scan or a crashed checker |

Two things follow.

**`qa-docs` is the odd one out and must not be swept up with the rest.** Its `exit 2`
means the checker produced no usable result, which is a different fact from a tool being
absent — Plan 00125 added that branch precisely so a crashed checker could not read as a
clean run. Whatever Phase 3 does to the other six, this one's meaning has to survive.

**The js gate is the only one whose missing input is dev-only.** The other five name
tools that any host running this repo's plays needs anyway, and that a provisioned
machine has. `extensions/node_modules` is the single case where the absent thing is
optional by design — which is why the conflict lands here and not on the others, and why
"install it in a playbook" reads differently for this one.

## 5. Ordering is what decides the cost, and nothing pins it

The js gate's position in the file is the entire reason the number is 31 rather than 1.
Nothing states or tests that ordering, so moving the js gate later would quietly shrink
the blast radius and moving it earlier would grow it — with no gate noticing either way.

Phase 3's fix should not be "move the js gate to the end". That would reduce this
instance to near-zero while leaving the mechanism intact for the next tool that goes
missing, and it would make the suite's correctness depend on an ordering no test asserts.

## 6. Who hits this, verified per population (Task 1.3)

00125 recorded `CLAUDE/QA.md` and its own Non-Goal disagreeing about the population, so
each was checked rather than either summary trusted.

**`CLAUDE/QA.md:108` is correct as written** — *"a linked worktree, and any checkout where
`npm install` has not been run"*. Both halves verified:

| Population           | Has `extensions/node_modules`? | How established                                  |
| -------------------- | ------------------------------ | ------------------------------------------------ |
| fresh `git clone`    | no                             | cloned one; `qa-all.bash` exits 2 (§1)           |
| linked git worktree  | no                             | created one with `git worktree add`, then removed |
| CI                   | yes                            | `.github/workflows/qa.yml:66` runs `npm ci`      |
| this CCY container   | yes                            | `qa-js` passes here; someone ran `npm ci`        |
| a *fresh* CCY container | no                          | 00125 measured why: no node stage, and the path is under the bind mount |

**There is a live instance in this repository right now.** Of the two worktrees under
`untracked/worktrees/`, `worktree-plan-00125-review` has no `extensions/node_modules`, so
`./scripts/qa-all.bash` run there today would abort after seven gates. Both worktrees are
clean — nothing uncommitted, nothing unpushed — so this is not hypothetical exposure on a
population that does not exist.

*(Unrelated housekeeping, noted not acted on: `worktree-plan-00125-review` belongs to a
plan that is Complete and archived, and has no unsaved work. Removing it is the owner's
call, not this plan's.)*

## 7. QA.md already names the fix shape — and the model it names has a residue of the same defect

`CLAUDE/QA.md:112-113`, immediately under the machine-dependence table:

> `qa-deployed-drift.bash` is the shape to copy: it states the dependency, skips only for a
> reason it prints, and the reason is checkable.

That is Task 3.1's requirement, already written down as this repository's own standard,
and `qa-js.bash` does not meet it — it aborts the suite instead of skipping with a reason.
So Phase 3 is not inventing a convention; it is applying one the docs already prescribe.

**But the named model is not quite right either, and Phase 3 should improve on it rather
than copy it.** `qa-deployed-drift.bash:69`, `:85` and `:92` all report their skip as:

```
✓ deployed-drift: skipped (CCY container — no deployed copies to compare); …
```

A tick. The reason is printed, which is the important half — but the SYMBOL says pass, and
`helpers/qa_environment/verdicts.py` parses that symbol. Anything counting ✓ stages counts
a gate that ran nothing as a gate that passed. That is this repo's recurring defect class
surviving inside the gate held up as the example of avoiding it.

So Task 3.1's `⚠ <name>: not run — <tool> absent` is deliberately a third symbol, not a
tick with prose after it. Three states need three symbols if a machine is going to read
them, and one already does.
