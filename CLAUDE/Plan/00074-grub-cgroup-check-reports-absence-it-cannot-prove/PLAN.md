# Plan 00074: the legacy-grub check reports an absence it cannot prove — and continues past a failure it did prove

**Status**: In Progress
**Created**: 2026-08-03
**Owner**: joseph
**Priority**: High

## Overview

`run.bash`'s "Checking for Legacy Grub Configurations" step contains **two** defects, found while
implementing Plan 00073 and deliberately not bundled into it. They are opposites of each other,
which is why one block manages to contain both:

1. It reports **absence it cannot prove** — a failing `grubby` is reported as "no legacy config".
2. It reports **a failure it did prove**, and then carries on regardless.

The second is the worse of the two, and it was not the one flagged at the time.

## Ground truth — the block as it stands

`run.bash:1617-1634`, measured at `86ba6ae`:

```bash
if _sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
  warning "Found legacy cgroup configuration, removing..."
  _sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=0"
  _sudo grubby --update-kernel=ALL --remove-args="systemd.unified_cgroup_hierarchy=1"
  if _sudo grubby --info=ALL 2>/dev/null | grep -q "systemd.unified_cgroup_hierarchy"; then
    error "Failed to remove cgroup configuration - may need manual intervention"
    echo -e "${YELLOW}${INFO} To manually remove, run:${NC}"
    echo -e "   sudo grubby --update-kernel=ALL --remove-args='...'"
  else
    success "Legacy cgroup configuration removed successfully"
  fi
else
  success "No legacy cgroup configuration found"
fi
```

### Defect 1 — a check that could not look reports what a check that looked reports

When `grubby` **fails**, `2>/dev/null` discards the reason, stdout is empty, the `grep` matches
nothing, the `if` takes the `else`, and the run prints:

> ✓ No legacy cgroup configuration found

Nothing in the output distinguishes that from a real, successful, negative answer. `set -o pipefail`
does not save it: `grubby(rc=1) | grep -q(rc=1)` is a non-zero pipeline either way, and non-zero is
exactly what routes to the `else`.

This is the repo's signature defect — *a true statement about a check presented as a stronger
statement about the world* — sitting inside the check whose entire job is to answer that question.

### Defect 2 — a proven failure prints an error and continues

`error()` (`run.bash:829-831`) is `echo -e` and nothing else. **It does not exit.** So when the
removal *verifiably fails*, the block prints "Failed to remove cgroup configuration", prints two
lines of manual instructions, falls out of the `if`, and the installer proceeds to the next step and
ultimately exits **0**.

That is the "skip and warn" pattern `CLAUDE.md` bans by name, on the one path where the script has
**positively established** that the box is misconfigured. It is strictly worse than defect 1:
defect 1 is ignorance mistaken for knowledge; defect 2 is knowledge discarded.

It was not part of the original finding. Opening the block to write the fix for defect 1 is what
surfaced it — which is the argument for fixing a thing rather than only noting it.

## Why this was not bundled into Plan 00073

Correctly, and worth restating. Plan 00073 was a sudo-credential change. Fixing this alters control
flow on the **interactive** path (aborting where it currently continues), which is a behaviour
change with its own regression surface. Burying it in a diff nobody is reviewing for that is how
unrelated breakage ships. It got an open question and a journal entry there, and now its own plan.

## Design

### D1. Four states, four distinct outcomes

The whole fix is refusing to collapse four states into two:

| State                             | Today                      | After                                     |
| --------------------------------- | -------------------------- | ----------------------------------------- |
| `grubby` ran, no legacy args      | ✓ "No legacy config found" | unchanged — ✓ "No legacy config found"    |
| `grubby` ran, legacy args removed | ✓ "removed successfully"   | unchanged — ✓ "removed successfully"      |
| `grubby` ran, removal FAILED      | ✗ printed, run continues   | **ABORT** non-zero, naming what was left  |
| `grubby` FAILED — could not look  | ✓ "No legacy config found" | **ABORT** non-zero, quoting grubby's text |

The two unchanged rows are the point: this must be inert on every path that works today.

### D2. `grubby` exiting non-zero is fatal; `grubby` exiting 0 with no output is not

This distinction keeps the regression risk small, and it is precisely the distinction the current
code destroys by piping through `grep` — after which both look like "no match".

- **Exit 0, no legacy args in the output** = "I looked; they are not there." Not fatal. A box with
  no boot entries that still exits 0 behaves exactly as it does today.
- **Non-zero exit** = "I could not look." Fatal.

So only a genuine `grubby` error aborts. That is a much narrower blast radius than "no boot entries
aborts the install", which is what a cruder fix would have produced.

### D3. `fatal` — the both-modes abort this block needs twice

Headless has `hl_abort` (loud banner, exit 1); interactive has `error`, which does not exit. Neither
alone covers a fatal condition reachable from both paths. `fatal <step> <what> <debug>` branches
once: `hl_abort` when headless, `error` + pointer + `exit 1` otherwise.

Two call sites in this block justify it. It is deliberately **not** retrofitted to pre-existing
`error; exit 1` sites — that is the scope creep this plan exists to avoid.

### D4. Extract to a function so the fix can be TESTED

The block is inline top-level code, so nothing can exercise it. Moved into
`check_legacy_grub_cgroup()` at top level it can be extracted from `run.bash` and run against a
**stub `grubby` first on `PATH`** — the pattern Plan 00073 proved for `_sudo`. That drives all four
states deterministically and needs no real `grubby`: there is none in a CCY container, and any one
real box would only ever exhibit one state anyway.

Bash resolves function names at call time, so a top-level function may call `info`/`warning`/
`success`/`error`/`_sudo` even though those are defined inside `main()` — they exist by the time it
runs, and the test stubs them.

## Goals

- The two working paths behave **identically** — proven, not assumed.
- A `grubby` that cannot run aborts loud, quoting what it actually said.
- A removal that verifiably failed aborts loud instead of printing and continuing.
- The outcome is decided by a function a test can drive through every state.

## Non-Goals

- No change to what counts as a legacy configuration, or to the removal commands themselves.
- No sweep of other `error`-without-`exit` sites. There may well be more; this plan fixes the block
  it is about and does not go looking. Worth a later audit — noted, not started, not implied fixed.

## Tasks

### Phase 1: test first

- [x] ✅ **Task 1.1**: `acceptance.bash` written **before** the fix and RED — all four legs failed.
  Honestly RED, and honestly *uninformative*: all four failed with the **same** message
  ("`check_legacy_grub_cgroup()` not found"), which is exactly the uniform-failure shape that
  proves nothing about any individual leg. Correct at that moment, but not evidence.

- [x] ✅ **Task 1.2**: Discrimination proven by **perturbation**, which is what turns the green run
  into evidence. Each defect was reintroduced separately in a fixture copy of `run.bash`:

  ```
  as shipped (expect all PASS)           clean=PASS  legacy=PASS  stuck=PASS  broken=PASS
  defect 1 back (expect broken FAIL)     clean=PASS  legacy=PASS  stuck=PASS  broken=FAIL
  defect 2 back (expect stuck FAIL)      clean=PASS  legacy=PASS  stuck=FAIL  broken=PASS
  control: msg text (expect clean FAIL)  clean=FAIL  legacy=PASS  stuck=PASS  broken=PASS
  ```

  A perfect diagonal: one perturbation, one failing leg, the right one every time. The
  perturber also **verifies its own rewrite took** — a replacement matching nothing yields a
  perfect copy and would report "all legs still pass", which is precisely the false result the
  script exists to rule out (`bash-standards` §5).

### Phase 2: implementation

- [x] ✅ **Task 2.1**: `fatal` (D3) — `hl_abort` when headless, `error` + pointer + `exit 1`
  otherwise, output blocked to stderr to match `hl_abort`.
- [x] ✅ **Task 2.2**: `check_legacy_grub_cgroup()` (D1/D2/D4) replacing the inline block. The
  removal commands are now captured too, so a failing `grubby --update-kernel` reports what it
  said instead of dying under a bare `set -e` with no message. The `title` call stays at the call
  site — `STEP_TOTAL` is derived by counting `title` invocations, so moving it would silently
  shift every step number.
- [x] ✅ **Task 2.3**: `RUN_BASH_VERSION` → 1.12.0. A **minor**, not a patch: this aborts where it
  previously continued, which is a behaviour change even though it is a fix.
- [x] ✅ **Task 2.4** (NEW, owner-raised mid-plan): the `RUN_BASH_VERSION` comment had grown to
  **4,791 characters on one line** — a changelog wearing a comment's clothes, unreadable in an
  editor and unreviewable in a diff. Moved to `docs/run-bash-changelog.md` (indexed in
  `docs/README.md`), with a 3-line pointer left behind. The version history is preserved and
  properly structured per release, not discarded. The function comments added by this plan and
  Plan 00073 were trimmed to WHY-only in the same pass; the load-bearing ones are kept
  (`_sudo`'s `[@]:-` warning has a test behind it, and `hl_sudo_askpass_start`'s "do not fix this
  to match the ssh one" prevents a silent-failure regression).

### Phase 3: prove it

- [x] ✅ **Task 3.1**: `acceptance.bash` GREEN — 4/4 states, in-container, no real `grubby`.
- [x] ✅ **Task 3.2**: `./scripts/qa-all.bash` green — 493 files, exit 0, shellcheck issue count
  **unchanged at 105**. Plan 00073's harness re-run too: still 3/3 on group A, group B still
  refusing, so the comment edits did not break its `_sudo` extraction.
- [ ] ⬜ **Task 3.3**: Host verification — **owner confirmed (2026-08-03) this rides along with the
  lts-infra box install**, so it needs no separate exercise: the next real `run.bash` there is the
  regression check. A real `run.bash` on a real box still passes this step.
  Unlike Plan 00073's Task 3.3 this is a **regression** check, not the proof of the feature: the
  four states are exhaustive and the stub drives all of them, so the logic is fully proven
  in-container. What a host adds is confidence that real `grubby` output lands in the state the
  fixture models — a question about the fixture's realism, not about the code.

## Open questions — owner

1. **Is a `grubby` that cannot run really fatal?** D2 says yes, and narrows it so only a genuine
   error trips it, not an empty result. The honest caveat: `grubby` is not installed in this
   container, so I could **not** measure what it returns on a box with zero boot entries. If it
   exits non-zero there, such a box would now abort where it previously (wrongly) reported success.
   The abort message names exactly what happened, which is why I still think fatal is right — but it
   is a real behaviour change on an unmeasured case, so it is your call.

## Dependencies

- Found during **Plan 00073** (open question 2 there). No code dependency in either direction.

## Success Criteria

- [x] ✅ Four states, four distinct outcomes, each asserted by a test **shown** to discriminate
  (Task 1.2's diagonal), not merely observed to be green.
- [x] ✅ Neither working path changes — `clean` and `legacy` pass identically before and after,
  and stay passing under every perturbation that breaks a different leg.
- [x] ✅ No path prints a failure and then exits 0.

## Risks & Mitigations

| Risk                                                    | Impact | Probability | Mitigation                                                                          |
| ------------------------------------------------------- | ------ | ----------- | ----------------------------------------------------------------------------------- |
| A box where `grubby` legitimately errors now aborts     | H      | L           | D2 — only a non-zero exit is fatal, not an empty result; the message quotes grubby  |
| The refactor changes a working path                     | H      | L           | Two of four states asserted unchanged; qa-all + the shellcheck count                |
| Test passes for the wrong reason (the recurring defect) | H      | M           | Task 1.2 — perturb and require a surgical failure, per `bash-standards` §9          |
| Other `error`-without-`exit` sites remain               | M      | H           | Explicit Non-Goal; recorded for a later audit rather than silently implied as fixed |

## Notes & Updates

No recovery cron — the owner asked for crons to be stopped.

## Delivery & Milestones

- Blow-by-blow: `JOURNAL/`
