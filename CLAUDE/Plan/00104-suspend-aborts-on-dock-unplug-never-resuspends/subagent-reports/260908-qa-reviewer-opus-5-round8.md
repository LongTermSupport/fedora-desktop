# QA Review — Plan 00104, round 8, `b0bcb09..3d487ba` (single commit `3d487ba`)

**Reviewer**: qa-reviewer (opus-5) · **Date**: 2026-09-08 · **Branch**: F44
**HEAD**: `3d487ba` · `git status --short` empty — this reviews what would deploy.
**Rounds**: [1](260908-qa-reviewer-opus-5.md) BLOCK · [2](260908-qa-reviewer-opus-5-round2.md) FIX-BEFORE-MERGE · [3](260908-qa-reviewer-opus-5-round3.md) BLOCK · [4](260908-qa-reviewer-opus-5-round4.md) FIX-BEFORE-MERGE · [5](260908-qa-reviewer-opus-5-round5.md) FIX-BEFORE-MERGE · [6](260908-qa-reviewer-opus-5-round6.md) PASS WITH NITS · [7](260908-qa-reviewer-opus-5-round7.md) PASS WITH NITS

**Verdict**: **PASS WITH NITS** — **DEPLOY**

The runtime deliverable is correct, verified, and unchanged. The sweep is real and it
landed on the sites that mattered — including the one that was user-visible. It is **not
quite total**: two residues of the same sentence family survive in the test files, and the
journal's four-item exception list mis-describes one of them. Neither can affect a run.
Nothing here gates deployment.

## Blocking

**None.**

## Should fix

**None.** Both remaining items are test-file wording; see Nits.

## Nits

### N1. `tests/helpers/suspend_wakeup/test_core.py:80-81` — the mirror of the round-7 S2 sentence, unswept

```python
def test_unreadable_target_is_not_counted_as_disarmed(self):
    """An attribute we could not read is not evidence the policy applied."""
    result = core.evaluate({"AC": None})
    ...
    self.assertEqual(result.unverifiable, ["AC"])
```

This is the `None`-contract sentence for `core.evaluate()` — the exact thing round 7's S2
flagged at `core.py:72` and this commit rewrote. The test that documents the same contract
for the same function still carries the narrow framing (`None` = "an attribute we could not
read"), and its method name still uses the retired bucket word `unreadable` while asserting
`result.unverifiable` two lines below.

The journal says the sweep covered "the exit contract, the `None` contract and the
`UNREADABLE` label across **helpers, tests**, the play and the docs". `test_core.py` is
tests, this is the `None` contract, and it appears in neither the six-corrected list nor
the four-left list. That is the recurrence the dispatch asked me to look for — smaller than
before, but real.

**Fix**: `test_none_target_is_not_counted_as_disarmed` /
`"""A device we could not vouch for is not evidence the policy applied."""`

### N2. `tests/helpers/suspend_wakeup/test_cli.py:83` — the third `unreadab*` hit in a file the journal says had two

```python
def test_a_dangling_device_symlink_is_unreadable_not_dropped(self):
```

The journal's exception list reads: *"the two `test_cli.py` uses describe the
`IsADirectoryError` fixture specifically."* Grepped: there are **three** `unreadab*` hits in
`test_cli.py` — `:67`, `:68` (both the `IsADirectoryError` fixture, accurate, correctly
left) and `:83`, which is the **dangling-symlink** fixture. That device is never read at
all: `cli.py:70`'s `exists()` returns False and the function short-circuits at `:74` without
touching `read_text`. Under the vocabulary this commit deliberately fixed, that is the
*unverifiable* case, not the *unreadable* one.

Defensible in plain English (a dangling symlink's target is indeed unreadable), and the
docstring immediately below is fully accurate — hence a nit, not a should-fix. But the
exception list is factually wrong about it, and this is the second time the same name has
been waved through (round 6 mischaracterised it, round 7 repeated the characterisation).

**Fix**: `test_a_dangling_device_symlink_is_unverifiable_not_dropped`.

## The sweep — independently enumerated

I built the population myself rather than checking the commit's list. Every site in the
repo that describes this helper's **exit contract**, its **`None` contract**, the
**COVERAGE labels**, or **when the play aborts**:

| Site | Family | Verdict |
| --- | --- | --- |
| `helpers/suspend_wakeup/cli.py:12-17` (module docstring = `--help`) | exit contract | **fixed, accurate** — verified against the runtime matrix below |
| `helpers/suspend_wakeup/cli.py:37-46` | `None` contract | **fixed, accurate** |
| `helpers/suspend_wakeup/cli.py:71-73` (`exists()` branch comment) | `None` contract | accurate — "cannot be vouched for … unverifiable" |
| `helpers/suspend_wakeup/cli.py:81` (`except OSError` comment) | `None` contract | **left, correctly** — at that point `exists()` was True and `FileNotFoundError` is already caught, so "exists but unreadable" is exactly the branch |
| `helpers/suspend_wakeup/core.py:70-74` (`evaluate`) | `None` contract | **fixed, accurate** |
| `helpers/suspend_wakeup/core.py:49-54` (`ok`) | `None` contract | accurate — the load-bearing sentence uses "cannot vouch for"; the detail sentence is shorthand, not a claim of enumeration |
| `helpers/suspend_wakeup/core.py:5-17` (module) | why-not-grep + target set | accurate; target set matches the udev rule exactly (`AC`, `ucsi-source-psy-*`) |
| `tests/…/test_cli.py:53` (`UNVERIFIABLE`) | COVERAGE label | **fixed** |
| `tests/…/test_cli.py:67-68` | `None` contract | **left, correctly** — genuinely the `IsADirectoryError` fixture |
| `tests/…/test_cli.py:83` | `None` contract | **left, mis-justified** → N2 |
| `tests/…/test_core.py:80-81` | `None` contract | **missed** → N1 |
| `docs/playbooks.md:130-142` ("three cases") | play aborts | accurate — three cases, three real abort paths |
| `docs/playbooks.md:126` | COVERAGE | accurate |
| `playbooks/imports/play-suspend-and-lid-policy.yml:237-252` | verification placement | accurate |
| `CLAUDE/Plan/…/PLAN.md:160-168` | COVERAGE + placement | accurate |
| `CLAUDE/Plan/…/deploy.bash:20-22` | COVERAGE | accurate |
| `files/etc/udev/rules.d/99-suspend-wakeup-policy.rules` | target set | in step with `core.py` |

**No `UNREADABLE` label survives in live source.** The only uppercase hits repo-wide are
Plan 00092's unrelated `probe-host.bash` and this plan's `JOURNAL/` (append-only history —
correct that those stand).

**"Three cases" in `docs/playbooks.md:131` verified against the play**, not assumed:
`fail:` at `:93` (sleep-hook directory), `fail:` at `:225` (GNOME schema), and the helper's
rc at `:253`. The `end_host` at `:86` and the upower skip are correctly described as *not*
aborts. Exactly three abort paths; the doc's enumeration is complete.

## `--help` — run, and it is now accurate

`python3 -m helpers.suspend_wakeup.cli --help` (stdout; stderr empty — clean stream split)
now reads:

> Exit 1 = at least one target is still `enabled`, or one could not be vouched for: its
> attribute was unreadable, held a value that is neither `enabled` nor `disabled`, or its
> device entry did not resolve.

Measured against every exit path in the code, using ephemeral fixtures:

```
rc=0  all targets disabled                COVERAGE: 1 of 1 power-delivery devices disarmed
rc=0  empty host                          COVERAGE: 0 of 0 — no power-delivery wakeup devices on this host
rc=0  non-target, no attribute            COVERAGE: 0 of 0 — …
rc=0  target, attribute absent            COVERAGE: 0 of 0 — …
rc=1  target still enabled                … — STILL ARMED: AC
rc=1  unrecognised value                  … — UNVERIFIABLE: AC
rc=1  empty read                          … — UNVERIFIABLE: AC
rc=1  attribute is a directory (OSError)  … — UNVERIFIABLE: AC
rc=1  dangling device symlink             … — UNVERIFIABLE: AC
rc=1  attribute mode 000 (EACCES)         … — UNVERIFIABLE: AC
```

Three distinct exit-1 causes, and the help sentence names all three and nothing that cannot
happen. `docs/playbooks.md:138-140` says the same thing in the same order. Round 7's S1 is
closed properly.

## Behaviour unchanged — confirmed independently of the AST check

I compared at the **bytecode** level, not the AST: compiled both revisions of all four
Python files, walked every nested code object, and diffed `co_code`, `co_names`,
`co_varnames` and `co_consts` with only the leading docstring constant elided.

```
--- docstrings ELIDED ---
   helpers/suspend_wakeup/cli.py             IDENTICAL
   helpers/suspend_wakeup/core.py            IDENTICAL
   tests/helpers/suspend_wakeup/test_cli.py  IDENTICAL
   tests/helpers/suspend_wakeup/test_core.py IDENTICAL
```

With a negative control to prove the method can see a change at all:

```
--- docstrings INCLUDED ---
   cli.py DIFFERS · core.py DIFFERS · test_cli.py DIFFERS · test_core.py IDENTICAL
```

`test_core.py` — the one file not in the diff — is the only one identical under both. The
three touched files differ in exactly one thing: docstring constants. Zero executable
change.

## `CLAUDE/Plan/README.md:37` — checked against `DECISIONS.md` and the delivered work

New row: *"…make the suspend request durable (udev wakeup policy, a re-suspend hook,
battery idle-suspend as backstop)"*.

`DECISIONS.md:22-32` (Decision 4, which `:9` marks as superseding Decision 1's framing)
states the three layers in priority order: remove the trigger (udev), make the request
durable (`system-sleep` hook), defence in depth (GNOME idle-suspend, *"demoted from primary
to backstop"*). The row now names all three in that order, uses Decision 4's own words for
the goal, and correctly labels layer 3 as the backstop. All three artefacts exist:
`files/etc/udev/rules.d/99-suspend-wakeup-policy.rules`,
`files/usr/lib/systemd/system-sleep/resuspend-aborted-suspend` (0700, executable), and the
`sleep-inactive-battery-type` tasks at `play:180-206`. **Accurate.**

The `PLAN.md:162-163` rewrite ("No count recorded here — counts go stale, the gate is the
source of truth") also lands: the decision stays in `PLAN.md`, the review history stays in
the journal, per `.claude/rules/plan-dir.md`.

## Checked and clean

- **IaC placement**: unchanged this round — one play, no bureaucratic split, imported at
  `playbook-main.yml:12`, verification last with the reason stated at `play:237-242`.
- **Fail-fast**: one `failed_when: false` at `play:195`, annotated, trailing-form, rc
  consumed at `:211`/`:235`. No new suppressions.
- **Version bumps**: `git diff --name-only` shows no `files/var/local/claude-yolo/`, no
  Dockerfile, no `entrypoint.sh`, no `files/opt/claude-yolo/`. **No CCY bump owed.**
- **Plan Commit Rule**: `PLAN.md`, `JOURNAL/`, the round-7 report and the code all landed in
  `3d487ba`; working tree empty; index row present. `**Status**: In Progress` is honest —
  Phase 4 (host deploy + incident reproduction) and Task 4.3 remain correctly unticked.
- **Public-repo safety**: added lines scanned for home paths, emails, RFC1918 addresses and
  UUIDs → no hits.
- **Stderr hygiene**: `--help` and the COVERAGE line both to stdout (help and the report
  *are* the payload); stderr empty.
- **Naming**: `suspend_wakeup`, `core`/`cli`, `is_policy_target`, `evaluate`,
  `unverifiable` — all say what they do. No jargon.

## Mechanical gates

| gate | rc | note |
| --- | --- | --- |
| `qa-all.bash` | 2 | short-circuits at the two known out-of-scope failures; gates run individually |
| `qa-ansible.bash` | **0** | |
| `qa-ansible-syntax.bash` | **0** | |
| `qa-docs.bash` | **0** | |
| `qa-patterns.bash` | **0** | |
| `qa-helper-tests.bash` | **0** | **Ran 255 tests … OK**; no stray COVERAGE lines after `OK` |
| `ruff check helpers/suspend_wakeup tests/helpers/suspend_wakeup` | **0** | All checks passed |
| `qa-bash.bash` | 1 | **only** `CLAUDE/Plan/00079-…/unit-test-selection.bash:111` SC2154 ×8 — pre-existing, outside the range |
| `qa-python.bash` | 2 | ruff pin 0.16.0 vs installed 0.16.3 — pre-existing, own plan |
| `plan-qa --sweep` | 1 | **0 block**, 2 advise (repo-wide staleness/journal nags). **00104 in neither list.** |
| `--syntax-check` | **0** | `play-suspend-and-lid-policy.yml` and `playbook-main.yml` |

**Conditional gates, stated explicitly**: `qa-helper-tests.bash` **was required**
(`helpers/` and `tests/helpers/` in the diff) → run, green. `check_extension_compat` and
`eslint` **not required** — no `extensions/` path in the diff.

## Final assessment of the whole deliverable — eight rounds in

**The deliverable is clean, safe to run, and free of functional defects.** Stated plainly,
without hedging.

- Behaviour has not changed since round 6, proven three different ways now (the author's by
  AST, mine by bytecode).
- Every exit path of the verification helper is exercised and matches its documentation
  exactly, including the user-visible `--help` text.
- The play states its population on every real run, distinguishes clean from blind at
  `n=0`, cannot abort before the fix it verifies is installed, and its three abort cases are
  documented accurately.
- No fail-fast violation, no missing dependency, no runtime probing for known state, no
  version bump owed, no secrets, no doc drift in `docs/`.

The two nits are wording inside test files. They cannot reach an operator, cannot change a
result, and do not warrant another review round on their own — fold them into whatever
commit records the Phase 4 host verification.

What remains to close this plan is unchanged and is host work, not code work:

1. `TASK [Report wakeup policy coverage]` must flip to
   `COVERAGE: 3 of 3 power-delivery devices disarmed`.
2. Out-of-band confirmation: `cat /sys/class/power_supply/{AC,ucsi-source-psy-*}/power/wakeup`
   → three `disabled`.
3. Task 4.2's reproduction (suspend → unplug within ~3 s → lid closed) has still never been
   executed on the host.

**DEPLOY.**
