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
