# Plan 00079: Podman container control — freeze/thaw by container, network, and CCY group

**Status**: In Progress. Task 3.3b is done: the 2026-09-23 host batch ran acceptance against
the reviewed code, `VERDICT: PASS`. Two success criteria remain before Task 3.4, both in
Task 3.5. One is the interactive picker, with and without fzf, which is a human check at a
HOST terminal. The other is a confirming `qa-reviewer` PASS.
**Created**: 2026-08-19
**Owner**: joseph
**Priority**: Medium

> The full pre-slimming document, with every fact and decision inline, is kept
> verbatim in [PLAN_archive.md](PLAN_archive.md). Facts, findings and risks now
> live in [RESEARCH-facts.md](RESEARCH-facts.md) (F-numbers) and decisions in
> [DECISIONS.md](DECISIONS.md) (D-numbers). The activity stream is in `JOURNAL/`.

## Overview

The user wants an easy way to freeze/unfreeze ("suspend/unsuspend") Podman
containers — individually, all CCY containers as a group, and by network — with
a simple interactive UI. The four verbs map to **one mechanism** that works on
this repo's rootless Podman: `podman pause`/`unpause` (the cgroup freezer). CRIU
`checkpoint`/`restore` requires root and is out of scope (F1/F2, D1).

No surveyed existing tool offers network-scoped bulk freeze, CCY-group
operations, or self-freeze protection (D2), so the plan is a small purpose-built
interactive bash tool, `podfreeze`, deployed to `~/.local/bin/` by a new optional
play (D3). CCY sessions are identified by the run-time `ccy=true` label the
launcher sets from CCY 3.40.0, plus the `<project>_yolo[_N]` name pattern for
sessions started by an older CCY; the inherited image label is deliberately
unused (D4, D6). Sessions also carry identity labels so a group can be "every
session on GitHub account X" (D8).

## Goals

- One command to freeze/thaw: a named container, all CCY containers, all
  containers on a named network, all sessions on one identity, or an
  interactively picked set.
- Simple TUI: a menu of **groups** (fzf, numbered-menu fallback) that opens a
  group to show its members with "all" as the first row (D9), acts, refreshes
  and re-offers itself until you quit (D7).
- **No verb to choose and nothing to confirm**: the state decides the verb, so
  the same choice twice toggles (D7).
- Prints the exact set it touched, with CCY rows marked; `-n` to look without
  acting; refuses to run inside a container.
- CCY containers carry explicit labels so "all CCY containers" and the identity
  groups are filters, not name regexes.
- Deployed via Ansible (`files/home/.local/bin/podfreeze` + new play).

## Non-Goals

- **Checkpoint/restore (CRIU)** — root-only, and CCY containers run `--rm`
  which checkpoint rejects without `--export`. A reboot-surviving suspend would
  be a separate root-mode plan.
- A GUI — the user accepts a TUI.
- Docker/LXC support — Podman-first per `CLAUDE/ContainerEngines.md`; the play
  declares the tool Podman-only.
- Replacing podman-tui/Cockpit for general container *browsing*.

## Context & Background

- Facts F1–F23, the safety case (F15/F17) and the risk table:
  [RESEARCH-facts.md](RESEARCH-facts.md)
- Decisions D1–D9 (mechanism, build-vs-adopt, bash+fzf, CCY identification,
  naming, dropping the image label, the group/derived-verb UX, identity axes,
  drill-down): [DECISIONS.md](DECISIONS.md)
- Plan-local scripts: `triage.bash` (Phase 0 probes), `deploy.bash` (HOST entry
  point, chains into `acceptance.bash`). `acceptance.bash` runs
  `scripts/test-podfreeze.bash` first as its unit check
- The recurring defect class this plan kept meeting — a partial result read as a
  complete one (F18–F23) — is recorded in `CLAUDE/AgentNotes.md`

## Tasks

### Phase 0: Host triage + decision gate

- [x] ✅ **Task 0.1**: Plan-local `triage.bash` written (read-only, HOST-only; its run log
  goes under `untracked/plan-runs/`)
- [x] ✅ **Task 0.2**: Run on the HOST. H1–H3 confirmed, H4 partly; facts
  recorded (F1–F15) and the outcome journalled
- [x] ✅ **Task 0.3**: **Decision gate PASSED.** Rootless pause is viable
  (cgroups v2 + systemd + crun), so the plan proceeds as designed

### Phase 1: CCY container labelling — the fix for the F16 over-match

Not a prerequisite (the tool works without it) but no longer optional either:
F16 showed `--ccy` selecting a CCY-*derived* container that was not a session.
See D4 and D6.

- [x] ✅ **Task 1.1**: Add `--label ccy=true --label ccy-project=<project>` to
  the `container_cmd run` invocation in `files/var/local/claude-yolo/claude-yolo`
- [x] ✅ **Task 1.2**: Bump `CCY_VERSION` (minor) with a comment describing the
  change; update `docs/ccy-changelog.md` — 3.39.0 → 3.40.0
- [x] ✅ **Task 1.3**: Point `podfreeze` at `ccy=true`, drop the image label
  (D6), and re-aim the acceptance gate's check 9 at the new contract
- [x] ✅ **Task 1.4**: Label the session's **identity** too —
  `ccy-github` / `ccy-token` / `ccy-ssh-keys` — and give `podfreeze` matching
  `--github` / `--token` / `--ssh-key` targets plus menu rows (D8)
- [x] ✅ **Task 1.5**: Run `./scripts/qa-all.bash`; commit

### Phase 2: The `podfreeze` tool

- [x] ✅ **Task 2.1**: Implement `files/home/.local/bin/podfreeze`:
  - optional verbs `freeze`/`thaw` with targets `<name>…`, `--network <net>`,
    `--ccy`, `--all`; `list` report; no target = interactive **group** menu
    (fzf, numbered-menu fallback) that loops until quit — see D7
  - verb derived from state when not given; `--dry-run` prints the affected set
    and exits; no confirmation prompt (D7)
  - refuses to run inside a container (`/run/.containerenv`)
  - freeze skips already-paused containers; thaw targets only paused ones
    (`--filter status=`); empty selection is a clear message, not a silent pass
  - prompts/progress/errors → stderr; `list` output is the human payload
    (stdout) per `CLAUDE/StderrHygiene.md`
- [x] ✅ **Task 2.2**: New play
  `playbooks/imports/optional/common/play-podfreeze.yml` (`scope: general`):
  deploy the script `0755`, install `fzf` (optional dependency — tool degrades
  without it). Documented in `docs/playbooks.md`
- [x] ✅ **Task 2.3**: Run `./scripts/qa-all.bash` (includes ansible syntax +
  shebang/exec-bit checks); commit
- [x] ✅ **Task 2.4**: Plan-local `unit-test-selection.bash` — sources the
  tool's function region verbatim and drives it against a synthetic inventory
  (multi-network container, half-frozen group, empty CCY group, explicit verb
  overriding the derived one).
  **Since superseded**: deleted in favour of `scripts/test-podfreeze.bash` (Plan 00122),
  which covers every function it did, runs in `qa-all.bash`, and is what
  `acceptance.bash` now runs (Plan 00130's review, decided 2026-09-23)

### Phase 3: Deploy, acceptance, review

- [x] ✅ **Task 3.1**: Write plan-local `deploy.bash` + `acceptance.bash`
  (HOST): deploy chains straight into acceptance and exits with its verdict;
  acceptance freezes/thaws a **throwaway** container on a **throwaway** network,
  checks `--ccy --dry-run` against the live CCY set as a contract, and checks
  the in-container refusal, the unknown-network and unknown-name failures, and
  mutually-exclusive targets. `--all` is never run for real. Both log through
  `plan_start_log auto`, to `untracked/plan-runs/<plan>/<script>/<timestamp>/` — Plan
  00130 converted them and relocated the old plan-local `logs/` tree, which no longer
  exists here

- [x] ✅ **Task 3.2**: User runs `CLAUDE/Plan/00079-podman-container-control/deploy.bash`
  on the HOST (it runs `play-claude-yolo.yml` **then** `play-podfreeze.yml`,
  then acceptance itself); journal the verdict. Run 3: PASS, 21 checks,
  0 skipped (runs 1–2 and the refused `acceptance.bash`-alone attempt that
  produced F18 are in `JOURNAL/`)

- [x] ✅ **Task 3.2b**: Re-run `deploy.bash` to exercise **check 9b** (the F19
  fix). PASS, 22 checks, 0 skipped — `--ccy` provably covers the whole fleet,
  not only the labelled part

- [x] ✅ **Task 3.2c**: Re-run `deploy.bash` to exercise **check 13b** and the
  `select_identity` disclosure it tests (F20). PASS, 24 checks, 0 skipped

- [x] ✅ **Task 3.2d**: Record the recurring defect class rather than only its
  instances — `CLAUDE/AgentNotes.md` gains *"A partial result read as a complete
  one"*, `CLAUDE/QA.md` points at it, and the `qa-reviewer` agent gains the
  three things to look for

- [x] ✅ **Task 3.3**: Run the `qa-reviewer` agent over the plan's full diff;
  resolve all BLOCK/FIX-BEFORE-MERGE findings. Verdict FIX-BEFORE-MERGE,
  9 findings, all resolved (F21–F23 among them; unit test grew to 49
  assertions). Full account in `JOURNAL/`

- [x] ✅ **Task 3.3c**: Confirming re-review run 2026-09-10, since Task 3.3's
  nine fixes had never themselves been reviewed. **Eight of the nine are verified
  genuinely fixed, line by line.** Report and an editor's note on its two
  inaccuracies in
  [subagent-reports/260910-qa-review-00079-opus-5.md](subagent-reports/260910-qa-review-00079-opus-5.md)

- [x] ✅ **Task 3.3d**: The re-review's findings addressed. Six of the seven
  should-fix items are closed here; the seventh is Task 3.3b, which is a host run.

  - **The gating one.** The axis-offer decision is now the named function
    `identity_axis_discriminates`, called by both `pick_target` and the unit test,
    instead of a predicate inlined in a function no test can call. **Proved by
    breaking it**: reverting the function body to the `-lt 2` cardinality proxy now
    fails 2 assertions, where the previous version left all 49 green. Restored, and
    the suite is 50 assertions PASS. A second case was added for the *other* side
    of the predicate — an axis whose one value covers every CCY session must be
    suppressed — so a predicate hardcoded to `return 0` cannot pass either.
  - `docs/playbooks.md` no longer describes the rule F21 removed.
  - The tool's header block lists all seven targets, matching `usage()`.
  - Acceptance check 13b's two unexercised branches are `skip()`, not `ok()`, so
    the closing count stops over-reporting.
  - The pre-rename `podman-freeze` binary is removed by
    `play-podfreeze.yml` (`state: absent`), not by an `rm` in `deploy.bash` — that
    was a manual system change the IaC HARD RULE prohibits, and it only reached
    someone who ran the play through the wrapper.
  - `deploy.bash` **reads** `CCY_VERSION` instead of printing a stale literal.
  - Nits: two stale comments removed from the tool; `do_action` now reports a
    selected name missing from the inventory instead of silently dropping it.

  **Recorded past this plan**: `CLAUDE/AgentNotes.md` gains rows 12–17 of the
  partial-result table — the four from this plan and three from 00092 — plus the
  note that row 14 (a test that does not execute the production path) was written
  by someone who had just read that page. The habit it prescribes: break the fix on
  purpose and watch the new test go red.

  **Not fixed, deliberately, and not this plan's to fix**: 48 of the 69 tracked
  plan scripts do not source `_planlib.inc.bash` and resolve the repo root with
  `git -C … rev-parse`, which `PlanScriptStandards.md` R1 forbids — including four
  created on 2026-09-10. Counted, not estimated. This plan's four scripts are among
  them. It is a repo-wide migration and wants its own plan; raising it rather than
  quietly widening this one.

- [x] ✅ **Task 3.3b**: **HOST ACTION, done by the 2026-09-23 batch.** It ran `deploy.bash`,
  which chains into acceptance, and a separate `acceptance.bash`. Both returned
  `VERDICT: PASS — 22 check(s) passed, 1 skipped` against the byte-checked deployed copy.
  Restated: 21 passed and 2 skipped, because check 9b's no-unlabelled branch counted a
  pass without running the tool (now a skip; journal 26-09-23, 20:30).
  Check 13 ran against 6 of 6 labelled sessions. Check 13b SKIPs by design when no
  unlabelled session is running. Journal 26-09-23. This closes the re-review's one
  outstanding finding: runs 3, 4 and 5 predated Task 3.3d's fix commit, so until now no
  acceptance run had executed against the reviewed code.

- [ ] ⬜ **Task 3.5**: The two open success criteria. (a) **Human, at a HOST terminal**:
  run `podfreeze` with no target, once with `fzf` on `PATH` and once without, and in each
  open a group, act on it and quit. (b) A confirming `qa-reviewer` run over the plan's
  diff; the last verdict (Task 3.3c) was FIX-BEFORE-MERGE, and Task 3.3d's fixes have not
  been reviewed.

- [ ] ⬜ **Task 3.6**: Prove `freeze --ccy` and `thaw --ccy` for real, which acceptance
  deliberately never does to live sessions. **Human, at a HOST terminal**, in a quiet
  moment: `podfreeze freeze --ccy`, confirm every session is paused (`podfreeze list`),
  then `podfreeze thaw --ccy` and confirm they all resume. It interrupts every running
  agent, which is why no script does it unattended.

- [ ] ⬜ **Task 3.7**: The triage probe `deployed ccy version` called the `ccy` alias,
  which a non-interactive script cannot see (`rc=127`). It now calls
  `/var/local/claude-yolo/claude-yolo --version`. Confirm at the next HOST triage run.

- [ ] ⬜ **Task 3.4**: Mark plan Complete, move to `Completed/`, update README
  index + statistics in the same commit

## Dependencies

- Depends on: nothing
- Blocks: nothing

## Success Criteria

- [x] `podfreeze freeze --network <net>` selects exactly the containers on that network,
  pauses them, and prints the set it touched. *(Checks 4–6 on the HOST, 2026-09-23, show
  the preview, the pause and the printed set, but on a one-member throwaway network.
  Exclusivity, meaning other networks' containers are left alone, is shown by the
  selection tests in `scripts/test-podfreeze.bash` (check 0), not by a live run.)*
- [ ] `podfreeze freeze --ccy` / `thaw --ccy` resolves every CCY container, labelled and
  legacy-named. *(Resolution is shown: check 9 dry-runs `--ccy` against the live labelled
  fleet, and check 0's suite covers the name-pattern path. What is not shown: a real
  `freeze --ccy` or any `thaw --ccy` has never run in acceptance, because the gate
  dry-runs `--ccy` so it never pauses live sessions. Task 3.6.)*
- [ ] Interactive picker works with and without fzf — **not exercised by any run**
  (`pick_target` needs a TTY). Human check at a HOST terminal
- [x] Refuses to run inside a container; `--dry-run` changes nothing *(checks 2, 4, 9)*
- [x] `acceptance.bash` renders `VERDICT: PASS` on the HOST against the
  deployed copy (it refuses to vouch for a binary that differs from the repo)
  *(2026-09-23, twice; those runs compared the entry script only. The gate now also
  compares the deployed freeze library, so the next run covers both files.)*
- [x] New CCY containers carry `ccy`, `ccy-project`, `ccy-github`, `ccy-token`
  and `ccy-ssh-keys` labels, and `podfreeze --github <id>` resolves exactly the
  sessions on that account (D4, D8) *(triage H6: 6 of 6 sessions carry all five; check 13)*
- [ ] `./scripts/qa-all.bash` passes; `qa-reviewer` verdict is PASS (or PASS
  WITH NITS, nits addressed or accepted) — the last verdict (Task 3.3c) was
  FIX-BEFORE-MERGE; Task 3.3d's fixes await a confirming review

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00079-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan created + research logged
- Phase 0 host triage run; decision gate passed, Phase 1 demoted
- Phase 2 built: `podfreeze`, `play-podfreeze.yml`, docs; plus the
  plan's `deploy.bash` + `acceptance.bash` (Task 3.1)
- Phase 1 landed: CCY 3.40.0 run-time labels, identity axes (D8), image label
  dropped (D6)
- Host acceptance runs 3–5 PASS; qa-reviewer findings resolved (Task 3.3),
  CCY 3.40.1
