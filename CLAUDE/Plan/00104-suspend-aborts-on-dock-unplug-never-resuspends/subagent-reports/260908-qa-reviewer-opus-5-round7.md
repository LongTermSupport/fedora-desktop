# QA Review — Plan 00104, round 7, `45f2b9f..b0bcb09` (single commit `b0bcb09`)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `b0bcb09`
**Rounds**: [1](260908-qa-reviewer-opus-5.md) BLOCK · [2](260908-qa-reviewer-opus-5-round2.md) FIX-BEFORE-MERGE · [3](260908-qa-reviewer-opus-5-round3.md) BLOCK · [4](260908-qa-reviewer-opus-5-round4.md) FIX-BEFORE-MERGE · [5](260908-qa-reviewer-opus-5-round5.md) FIX-BEFORE-MERGE · [6](260908-qa-reviewer-opus-5-round6.md) PASS WITH NITS / DEPLOY

**Verdict**: **DEPLOY** — the machine-facing deliverable is unchanged and correct. **But not CLEAN**: round 6's S2 fix was applied to one of the three places carrying the same sentence, which is the exact failure shape this commit's own message names.

`git status --short` is empty. This reviews what would deploy.

## Confirmed: no behaviour changed

AST comparison of every touched Python file at `45f2b9f` vs `b0bcb09`, with docstrings stripped:

```
helpers/suspend_wakeup/cli.py:             AST(sans docstrings) SAME
helpers/suspend_wakeup/core.py:            SAME (byte-identical incl. docstrings)
tests/helpers/suspend_wakeup/test_cli.py:  AST(sans docstrings) SAME
tests/helpers/suspend_wakeup/test_core.py: AST(sans docstrings) SAME
```

`git diff --name-only` touches 7 files: no `*.yml`, no `files/`, no `scripts/`, no
`files/var/local/claude-yolo/` (so no `CCY_VERSION` / Dockerfile LABEL bump is owed).
Comments, docstrings and prose only, as claimed.

## Blocking

**None.**

## Should fix

### S1. `helpers/suspend_wakeup/cli.py:12-14` — the module's own exit-1 contract is the *unfixed* copy of the sentence round-6 S2 fixed in `docs/`. It is also `--help` output

```python
Exit 0 = every power-delivery device the udev policy targets reads `disabled`, OR the
host has none. Exit 1 = at least one is still `enabled`, or its attribute could not be
read.
```

That is the same "exists but cannot be read" framing round 6 flagged at
`docs/playbooks.md:138`, verbatim in shape, in the file that *implements* the behaviour.
Measured against real fixtures (tmpdir, `--power-supply-dir`):

```
all disabled          : rc=0 out='COVERAGE: 1 of 1 power-delivery devices disarmed'
value read but unknown: rc=1 out='COVERAGE: 0 of 1 … — UNVERIFIABLE: AC'
dangling device entry : rc=1 out='COVERAGE: 0 of 1 … — UNVERIFIABLE: AC'
```

Two exit-1 paths the docstring does not name. It matters more than an internal comment
because `argparse` uses `description=__doc__` — I ran
`python3 -m helpers.suspend_wakeup.cli --help` and this sentence is in the user-visible
help text. An operator debugging a `TASK [Verify the power-delivery wakeup policy applied]`
failure on a host where `power/wakeup` returned something odd reads help text saying that
case cannot happen.

**Fix**: mirror the wording now in `docs/playbooks.md:138-140` —
`Exit 1 = at least one target is still enabled, or one could not be vouched for: its
attribute was unreadable, held a value that is neither enabled nor disabled, or its device
entry did not resolve.`

### S2. `helpers/suspend_wakeup/core.py:72-73` — the `evaluate()` docstring's `None` contract now contradicts what `cli.py` deliberately does

```python
`None` means the `power/wakeup` attribute was present but unreadable — distinct from
the device being absent entirely, which simply means it is not in the mapping.
```

`cli.py:67-71` was changed in `45f2b9f` specifically so that a device that is **absent**
(dangling symlink, vanished mid-enumeration) **is** in the mapping, as `None` — that was
the whole point of the `exists()` guard, and `cli.py:55-59` now documents it at length.
The second half of this sentence states the opposite. This is the interface contract
between the two modules, so it is worth more than a comment: the next person to touch
`evaluate()` reads it as licence to treat `None` as "unreadable attribute" only.

**Fix**: `None` means we could not vouch for the device — its attribute was unreadable, or
the entry itself did not resolve. A device with no `power/wakeup` at all is simply not in
the mapping.

### S3. `CLAUDE/Plan/README.md:37` — the index row still describes the plan by the framing `DECISIONS.md` marks superseded

```
- [00104-…](00104-…/) - Unplugging the dock aborts s2idle and nothing re-suspends, so the
  closed laptop runs hot in a bag; restore the battery idle-suspend safety net
```

`PLAN.md:66-71` records Decision 1 ("restore the battery idle-suspend safety net") as
**superseded by Decision 4** ("the goal is a durable suspend request, not a bounded
failure"), and the delivered work is a udev rule plus a `system-sleep` hook plus
idle-suspend as *backstop*. The index summarises the plan as its one demoted layer,
omitting the two that are actually the fix. Same shape as S1/S2 — a description the work
outgrew.

**Fix**: `…; make the suspend request durable (udev wakeup policy, a re-suspend hook,
battery idle-suspend as backstop)`.

## Nits

- **`tests/helpers/suspend_wakeup/test_cli.py:53`** — `Reporting such a device as
  UNREADABLE would hard-fail the run`. `UNREADABLE` is a COVERAGE-line label the code no
  longer emits; `core.py:65` prints `UNVERIFIABLE:`. Round 6 checked the `unreadable` hits
  and judged the survivors accurate, but characterised them as "a test method name that
  still describes a `None` fixture" — this one is the uppercase *output label* inside a
  docstring. The lowercase prose uses at `:68` and `:83` are fine.
- **`PLAN.md:162-164`** — `Counts deliberately not written down here — they went stale
  twice, including on the very commit that corrected them`. The *decision* to omit the
  count belongs in `PLAN.md`; the review history behind it is already in
  `JOURNAL/00104-Journal-26-09-08.md` word for word. Per `.claude/rules/plan-dir.md`,
  narrative goes to the journal. One clause ("counts go stale; the gate is the source of
  truth") would carry it.
- **`helpers/suspend_wakeup/cli.py:41`** — the third bullet still heads the `None` outcome
  as `**Attribute present but unreadable**`, which is now one of two causes. Mitigated by
  the paragraph at `:50-59` that spells out the other, so genuinely minor — but it is the
  same sentence family as S1/S2 and should move with them.

## Judged, and I disagree with raising it as a finding

- **`files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend:43`** — `falls to layer
  3's 900s idle-suspend instead`. `900` is a measured host value
  (`TRIAGE-EVIDENCE.md:245`), not something any playbook sets, so it could differ
  elsewhere. The argument the comment makes (a residual gap covered by layer 3) holds
  regardless of the number, and the number is sourced in the plan. Not a defect.
- **`resuspend-aborted-suspend:184-191`** — `if ! systemd-run …; then log …`. Looks like
  skip-and-warn; it is the round-1/round-2 remediation for the `--collect` wedge, and
  there is no recovery available to a `post` hook after the timer fails to schedule.
  Settled, correctly.

## The claims in this commit, verified individually

| Claim | Verified |
| --- | --- |
| **S1**: count removed rather than corrected a third time | `PLAN.md:162-164`. The removal is the **right call** — the number was never the assertion. Measured now: `test_core` + `test_cli` → `Ran 30 tests … OK`; `qa-helper-tests.bash` → `Ran 255 tests … OK`. The remaining claims ("covering both the pure verdict and the sysfs read"; "`qa-helper-tests.bash` green") are both true and both re-checkable by running the named gate, so the ✅ line asserts everything a reader needs and nothing that rots. **Not under-asserted.** |
| **S2**: `docs/playbooks.md` third abort case widened | `docs/playbooks.md:138-142` now names unreadable / unrecognised value / unresolvable entry. Checked against `core.Result.ok` (`core.py:55`) — false iff `still_armed` (value `enabled`) or `unverifiable`, and `unverifiable` has exactly three producers: `core.py:91` (`None` from an OSError read, `cli.py:78`), `core.py:99` (unrecognised value), `cli.py:71` (entry does not resolve). The bullet is now an exact enumeration. **Accurate.** The "three cases" count also holds — the play has exactly two `fail:` tasks (`:92`, `:224`) plus the helper's rc. |
| **Nit**: `round-3 finding B2` removed from `test_core.py:11` | Grepped `round[- ][0-9]` / `finding [A-Z][0-9]` across `helpers/suspend_wakeup`, `tests/helpers/suspend_wakeup`, the play, the udev rule, the sleep hook, `docs/playbooks.md`, `PLAN.md`, `DECISIONS.md`, `deploy.bash` → **NONE**. The journal's "No round-number references remain in any source file" is true. |
| **Nit**: `TestMain` docstring rewritten | `test_cli.py:111-117`. No longer counts methods; states why capture exists. Good. |
| **Nit**: `cli.py` `exists()` comment widened | `cli.py:68-70` now names dangling symlink + EACCES + ELOOP. Matches round 6's measured table. |
| **Nit**: `iterdir()`/`stat()` race recorded | `cli.py:55-59`. States the trade and why the abort is safe (last task). Consistent with the play's `:237-248` comment. |

## The sweep — what else has the same shape

Looked specifically for stale counts, stale behaviour descriptions, review history in
permanent source, and plan/doc claims the code no longer supports:

- **Stale counts**: none left. `PLAN.md:26` "F1–F17" — all 17 headings present in
  `TRIAGE-EVIDENCE.md` (F16 is a sub-heading). `PLAN.md:63` "Four decisions" — 4 headings
  in `DECISIONS.md`. `docs/playbooks.md:130` "three cases" — 3 bullets, 3 abort paths.
  `PLAN.md:133` "Attempt cap (3)" = `MAX_ATTEMPTS=3`. `PLAN.md:129` / `docs:122`
  "within 10s" = `RESUSPEND_WINDOW=10`.
- **The one place a count *would* have gone stale again is the one done right**:
  `docs/architecture.md` re-sequenced all 31 entries when the play was inserted at
  position 5, and the list matches `playbook-main.yml`'s import order entry for entry.
  That is the whole-population fix.
- **Stale behaviour descriptions**: three instances, all of the round-6-S2 sentence — S1,
  S2 and the `cli.py:41` nit.
- **Review history in permanent source**: zero in code; one residue in `PLAN.md` (nit).
- **Plan/doc claims the code no longer supports**: `PLAN.md`'s Task 3.1–3.6 bullets all
  check out against the play as written (probe at `:175` outside preflight; verification
  at `:253`/`:269` after `flush_handlers` at `:154`; `end_host` at `:86`;
  `check_mode: false` on the probe at `:191`). `PLAN.md:181-184` "Does **not** modify
  `play-prevent-ssh-suspend.yml`" — confirmed across the *whole* plan range
  (`4a37331~1..b0bcb09` touches 27 files; that file is not among them). One stale claim
  found, in `README.md` (S3).
- **The unrelated file in the plan range**: `playbooks/imports/play-podman.yml:127` gained
  a `# FAIL-FAST-OK:` annotation at `40e05b5`. Not dismissed as out of scope — read
  `:123-144`; the annotation is accurate (`rc` and `stdout_lines` are both consumed by the
  `when:` on the next two tasks). Fine.

## Stepping back — seven rounds in, is the shape right?

**Yes, and I looked for accumulated damage rather than assuming.** The plan has been
rewritten repeatedly under review and has not acquired the usual scar tissue:

- **One play, no bureaucratic split.** Layers 1–3 and their verification live in the play
  that owns suspend policy, imported from `playbook-main.yml:12` adjacent to
  `play-prevent-ssh-suspend.yml`, with an explicit comment (`:9`) saying adjacency is for
  readability, *not* ordering. No new play was created to solve a placement problem.
- **The verification states its population.** `COVERAGE: n of m`, named buckets, words
  rather than a bare pass at `n=0`, printed on every real run. The repo's single most
  recurrent defect class, closed properly.
- **The helper split obeys `helpers/CLAUDE.md`**: pure `core` + thin `cli`, stdlib only,
  namespace package, invoked with `command:` + `argv:` + `chdir`.
- **The one `failed_when: false`** (`:195`) is annotated, trailing-form, genuine
  probe-then-fail with rc consumed at `:211` and `:235`. `read_int_or_empty` in the hook
  validates content, not existence — the Plan 00067 trap avoided deliberately.
- **Nothing is over-built.** Three layers, one play, one helper, one rule, one hook.

The recurring failure mode of *this plan* has been documentation lagging behind
repeatedly-revised code — and round 7 is another instance of exactly that, at reduced
amplitude. The fix is three sentences; the shipped behaviour is not in question.

## Mechanical gates

Run individually because `qa-all.bash` short-circuits at `qa-python`'s rc=2 (pre-existing
ruff pin, out of scope per the brief).

| gate | rc | note |
| --- | --- | --- |
| `qa-ansible.bash` | **0** | 75 playbooks; shebang+exec; fail-fast patterns |
| `qa-ansible-syntax.bash` | **0** | 77 playbooks |
| `qa-docs.bash` | **0** | 63 files (links, anchors, playbook catalogue) |
| `qa-patterns.bash` | **0** | 200 files |
| `qa-bash.bash` | 1 | **only** `CLAUDE/Plan/00079-…/unit-test-selection.bash` SC2154 ×5 — pre-existing, outside the range |
| `qa-python.bash` | 2 | ruff pin 0.16.0 vs 0.16.3. `ruff check helpers/suspend_wakeup tests/helpers/suspend_wakeup` → **All checks passed!** |
| `qa-helper-tests.bash` | **0** | **Ran 255 tests, OK**, no stray COVERAGE lines after `OK` |
| `plan-qa --sweep` | 1 | 0 block / 2 advise, both repo-wide nags. **00104 in neither list.** |
| `ansible-playbook --syntax-check` | **0** | the play and `playbook-main.yml` |

**Conditional gates, stated explicitly**: `qa-helper-tests.bash` **was required**
(`helpers/` and `tests/helpers/` both in the diff) → run, green. `check_extension_compat`
and `eslint` **not required** — `git diff --name-only` shows no `extensions/` path.

**Public-repo safety**: added lines scanned for `/home/<user>` paths, emails, RFC1918
addresses, UUIDs and `.local` hostnames → the only hit is round 6's own report text
describing its own scan. Clean.

**Plan Commit Rule**: `PLAN.md`, `JOURNAL/`, the round-6 report and the code all landed in
`b0bcb09`; working tree empty; index row present at `CLAUDE/Plan/README.md:37`. Tasks 1.2,
2.1 (part), 2.2 and 4.1–4.3 correctly unticked; `**Status**: In Progress` is honest.

## DEPLOY call — **DEPLOY**

Nothing executable changed in this commit, and the runtime deliverable was already settled
in round 6. The three should-fix items are sentences in a docstring, a docstring and an
index row; none can affect a run. Fix them in the next commit rather than gating on them.

Still the only things that close this plan, unchanged from rounds 5 and 6:

1. `TASK [Report wakeup policy coverage]` must flip from `COVERAGE: 0 of 3 … STILL ARMED`
   to `COVERAGE: 3 of 3 power-delivery devices disarmed`.
2. Confirm out of band: `cat /sys/class/power_supply/{AC,ucsi-source-psy-*}/power/wakeup`
   → three `disabled`.
3. Task 4.2's reproduction (suspend → unplug within ~3 s → lid closed) has still not been
   executed on the host by anything in this range.
