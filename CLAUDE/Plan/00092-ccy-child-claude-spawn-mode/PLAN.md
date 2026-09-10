# Plan 00092: CCY child-claude spawn mode

**Status**: In Progress
**Created**: 2026-09-02
**Owner**: joseph
**Priority**: Medium

## Overview

A CCY session cannot currently spawn a working child `claude` process. Claude Code
strips its own credential from the environment it hands to Bash subprocesses, so a
child launched from the agent's shell answers `Not logged in · Please run /login`.
Measured in this container: comparing PID 1's environment against the Bash-tool
shell, exactly two names are missing — `CLAUDE_CODE_OAUTH_TOKEN` and `GH_TOKEN`.
The second is CCY's own deliberate `unset` in `entrypoint.sh:48`; the first is
Claude Code scrubbing by name. Every other variable CCY passes survives.

This plan adds an **opt-in** mode, declared in the project's tracked
`/workspace/.claude/ccy/ccy.env`, that installs two things into the container: a
small wrapper that re-attaches the session's own token to a child process, and a
skill that tells the agent the capability exists and when to use it. When the flag
is absent, neither artefact is present and behaviour is exactly as today.

The binding constraint is **no security degradation**. That phrase is given a
testable meaning in [SECURITY-MODEL.md](SECURITY-MODEL.md) as seven invariants,
each with a probe in `acceptance.bash`. The short version: inside a CCY container
the agent is already root and `/proc/1/environ` is already readable, so the scrub
is an accident-prevention measure and not a boundary. The feature must therefore
add **no new exposure surface** — no on-disk copy of the token, nothing in argv,
nothing in an inheritable variable, nothing in a transcript — rather than claim to
restore a boundary that does not exist.

## Goals

- A project opts in with one line in its tracked `ccy.env`, and out by removing it.
- When enabled, the agent can run a child `claude` that authenticates successfully.
- When enabled, the agent is told the capability exists without being told to use it.
- When disabled, neither the wrapper nor the skill is present in the container.
- Every invariant in [SECURITY-MODEL.md](SECURITY-MODEL.md) is enforced by a probe
  in `acceptance.bash` that fails loudly when violated.
- Child depth is bounded, so a child cannot recursively spawn an unbounded tree.

## Non-Goals

- **Restoring the credential scrub as a security boundary.** Impossible while the
  agent runs as root in the same namespace as the token. Not attempted.
- **Running `claude` as a non-root user in CCY.** That is the only change that
  would make a real boundary, and it is a much larger change to the whole image.
  Recorded in the security model as the honest alternative; out of scope here.
- **A host launcher flag** (`ccy --child-claude`). The ask is a `.claude/ccy`
  config option. A launcher flag is a second precedence layer nobody asked for.
- **Orchestration.** No queueing, no fan-out helper, no agent-teams integration.
  The wrapper launches one process; the agent decides what to do with it.
- **Changing what the child is allowed to do.** Arguments are passed through
  verbatim. The wrapper never injects `--dangerously-skip-permissions` or a model.

## Context & Background

Everything measured before planning — where the token actually is, why `ccy.env` adds
no trust it does not already hold, why the skills directory is host-persisted, and the
additive-only deployment chain from repo to running session — is in
[RESEARCH-facts.md](RESEARCH-facts.md), each fact at a `file:line`.

Supporting documents: [SECURITY-MODEL.md](SECURITY-MODEL.md) (threat model, the seven
invariants), [DECISIONS.md](DECISIONS.md), `subagent-reports/`, `JOURNAL/`.

## Tasks

### Phases 1–5: complete — the feature is built and shipped through IaC

Compressed once Phase 6 became the only live work. The full task-by-task record is in
git history and `JOURNAL/`; the durable output is the four files below plus
[SECURITY-MODEL.md](SECURITY-MODEL.md) and [DECISIONS.md](DECISIONS.md).

- [x] ✅ **Phase 1 — the standard.** `SECURITY-MODEL.md` states the seven invariants,
  the threat model, and plainly that the credential scrub is not a boundary under root.
  `acceptance.bash` implements one probe per invariant on `_planlib.inc.bash`
  (PlanScriptStandards R1–R14); `plan_require_container` was added to the library so the
  gate cannot go vacuously green on the host. `selftest-probes.bash` proves each probe
  CAN fail, and found two real defects on its first run. The baseline also produced a
  **true** I1 red: tracing the probe with `bash -x` put the token into the host-mounted
  transcript. Remedy is token rotation, and it is the owner's.
- [x] ✅ **Phase 2 — the wrapper.** `ccy-claude` honours an already-set
  `CLAUDE_CODE_OAUTH_TOKEN`, otherwise recovers it from `/proc/1/environ`, fails fast
  with a named cause, enforces the depth bound, and `exec claude "$@"` with arguments
  verbatim. Diagnostics to stderr only. Verified against a stub `claude` reporting its
  own argv.
- [x] ✅ **Phase 3 — the skill.** `SKILL.md` leads with when **not** to use the feature,
  because a child process is a worse subagent than the `Agent` tool, and documents the
  traps — stdin needs `< /dev/null`, quota is shared, transcripts land in the host-mounted
  project directory, never trace a command that touches the token. Includes the trap the
  functional probe actually found: a child started in `/workspace` inherits this project's
  whole harness and answers about it.
- [x] ✅ **Phase 4 — entrypoint wiring.** The conditional install sits after the `ccy.env`
  source and gates only the separate `optional/` tree. Enabled: symlink, skill replaced
  wholesale, both flags exported past the `exec`. Disabled: the host-persisted skill is
  removed. A flag set against an image that lacks the tree refuses to start, naming
  `ccy --rebuild`.
- [x] ✅ **Phase 5 — IaC.** Copy tasks in `play-claude-yolo.yml`, `COPY optional/` in the
  Dockerfile, container 2.28 → 2.29 and `CCY_VERSION` 3.45.1 → 3.46.0 together, changelog
  entry, and a commented example in this project's own `.claude/ccy/ccy.env`.

### Phase 6: Verify and review

- [x] ✅ **Task 6.1**: `./scripts/qa-all.bash` passes, 640 files.

  - [x] ✅ Fixed a QA gate defect found on the way: `qa-all.bash` was **not
    idempotent**. Its own ansible-syntax stage installs Galaxy collections into
    the gitignored `.ansible/`, and only `.ansible/roles` was excluded from
    discovery, so the second consecutive run failed on vendored upstream
    fixtures. Committed separately as 9ab5d6b.
  - [x] ✅ Fixed a second gate defect: the pre-commit secret scanner matched
    private tokens by bare substring, so a 5-character identity token matched
    inside a company name this repo already documents, and every commit touching
    two tracked files was rejected with no way to comply. Now word-boundary
    matching with a 15-case test suite. Committed separately as 3956584.

- [x] ✅ **Task 6.2**: `docs/ccy.md` gains a `CCY_CHILD_CLAUDE` subsection under
  per-project configuration, which is where `ccy.env` options already live, plus
  a `docs/ccy-changelog.md` entry for 3.46.0.

- [x] ✅ **Task 6.3**: `qa-reviewer` run over the full diff. Verdict
  FIX-BEFORE-MERGE, no outstanding BLOCK. All four FIX-BEFORE-MERGE findings
  resolved: I1 now walks all of `/workspace` and `/root`; a `PRESENT` leg mirrors
  I6 in the enabled state; `docs/` no longer links into this folder; the
  version-gate gap is Plan 00093. Seven of eight nits taken, the eighth declined
  with a reason. Finding-by-finding detail: `JOURNAL/` 12:30.

- [x] ✅ **Task 6.5**: Confirming `qa-reviewer` re-review, 2026-09-10 — Task 6.3's
  fixes had never themselves been reviewed. **FIX-BEFORE-MERGE**: 0 blocking,
  **6** fix-before-merge, 8 nits. Extensive clean list, including the live credential
  absent from all 898 tracked files and all 227 host-mounted state files. Report and
  its authoritative `file:line` appendix:
  [subagent-reports/260910-qa-review-00092-opus-5.md](subagent-reports/260910-qa-review-00092-opus-5.md)

- [x] ✅ **Task 6.6**: I1's binary blind spot fixed. `--binary-files=without-match`
  meant `grep` never opened a file it judged binary while the pass line reported the
  whole enumeration as searched — the plan's own top-threat invariant reporting a
  partial result as a complete one. Now `--binary-files=text`, which also removes a
  dependence on which `grep` is on `PATH` (ugrep in the container, GNU grep on the
  host, differing defaults). Reasoning and the options weighed: [DECISIONS.md](DECISIONS.md)
  Decision 4. **Run in this container after the change: `COVERAGE: searched 85054 of 85054 enumerated regular file(s) under 7 path(s), 0 skipped`, exit 0.**

- [x] ✅ **Task 6.7**: The other five fix-before-merge findings, all fixed:

  - `probe-host.bash` H3 guarded both sentinels on both values, as H2 does, so two
    unreadable versions can no longer compare equal and print
    "current at NOT-FOUND".
  - `play-claude-yolo.yml` and `.claude/ccy/ccy.env` now reference Plan 00092 **by
    number**, so neither breaks when this folder moves to `Completed/`. Swept the
    tracked tree: the only remaining path reference is `CLAUDE/Plan/README.md`'s
    index row, which is correct and moves with the plan.
  - `deploy.bash` **reads** `REQUIRED_CONTAINER_VERSION` from the launcher instead of
    quoting a literal, so its closing note cannot go stale again.
  - `selftest-probes.bash` gained `PRESENT` cases: one green, plus two reds covering
    both halves of the check — a wrapper that is not the image's, and an installed
    skill that has drifted from the shipped one. **These are written but NOT yet
    exercised**: this container has the mode off, so the run reports them
    `UNVERIFIED: PRESENT cases — the mode is off, so both artefacts are absent by design`. Task 6.4 step 5 is what proves them.

  **Selftest run in this container: passed 14, failed 0, unverified 1, VERDICT PASS.**

- [x] ✅ **Task 6.8**: The eight nits, addressed rather than accepted:
  `probe-invariant.bash` PRESENT now fails when the image-side reference `SKILL.md`
  is absent instead of skipping the `cmp` and falling through to a pass;
  `entrypoint.sh` validates `CCY_CHILD_CLAUDE_MAX_DEPTH` at session start on the same
  terms as `CCY_CHILD_CLAUDE`, so the banner can no longer announce an unusable bound
  (CCY 3.49.1, container 2.36); the risk table's stale `CCY_CLAUDE_DEPTH` renamed;
  Delivery & Milestones filled in; Task 6.4 corrected below; the additive-only
  build-context staging documented against the `state: absent` pattern that exists
  because it was missed once; the name-collision warn-and-continue stated in
  [SECURITY-MODEL.md](SECURITY-MODEL.md) under I6 and as Decision 5; and
  `test-planlib.bash` wired into `qa-all.bash` — the nit that reached past this plan,
  since `_planlib.inc.bash` backs every plan script in the repo and nothing ran its
  regression suite automatically.

- [ ] 🚫 **Task 6.4**: **Blocked — HOST ACTION, cannot run in the container.**
  Every step is a script in this folder, not a command to retype from chat, per
  [PlanTriage.md](../../PlanTriage.md). All three refuse to run in the wrong place.

  1. `./triage.bash` on the HOST — what is stale before changing anything.
  2. `./deploy.bash` on the HOST — runs `play-claude-yolo.yml`. It deliberately
     does **not** rebuild the image, and says so. **Attempted once already**: it
     failed on the host, which is what produced the `_planlib.inc.bash` 1.1.1 fix
     (`JOURNAL/`). The rerun has not happened.
  3. `ccy --rebuild` — required; the launcher now asks for container **2.36**.
  4. `./triage.bash` again — its H4 leg confirms the rebuild landed.
  5. `./acceptance.bash` INSIDE a container with `CCY_CHILD_CLAUDE=1` set. This is
     also the only run that can exercise Task 6.7's new `PRESENT` cases.
  6. `./acceptance.bash` INSIDE a **later** container with the flag removed.
     This is the step that matters: it proves the mode can be turned off.

  - Before running it: I1 will report the session transcript from 2026-09-02
    unless the OAuth token is rotated first. That red is a true finding,
    recorded in `JOURNAL/`, not a defect in the gate. (I1 is green in *this*
    container, which holds a different token — that says nothing about the host.)

## Dependencies

- Interacts with Plan 00080 (network isolation): a child shares the parent's
  network namespace, so it inherits any isolation rather than escaping it.
  Confirm, do not assume.
- Interacts with Plan 00068 (CI runner variant): a CI entrypoint may not be `tini`
  at PID 1. The wrapper's fail-fast path must be legible there.

## Technical Decisions

Five decisions, with their options and reasoning, are in
[DECISIONS.md](DECISIONS.md): the token source (`/proc/1/environ` over `settings.json`
or an env alias), the opt-in as a capability declaration rather than a control,
verbatim argument pass-through, `--binary-files=text` in the I1 search, and leaving a
name-colliding skill in place rather than deleting a user's work.

## Success Criteria

- [x] ✅ A child `claude -p` returns a real completion with the credential
  attached. Proved before the wrapper existed, and the wrapper reproduces it.
- [x] ✅ Without the flag, no wrapper on `PATH` and no child-claude skill on disk
  (probe I6, and its negative case plants one and watches I6 go red).
- [x] ✅ The token appears in no file, no argv and no variable in the agent's
  Bash-tool environment — probes I1, I2 and I3, each falsifiable.
- [x] ✅ Depth limit refuses. Verified both ways: the real wrapper refuses at the
  limit, and a stub with no guard makes probe I7 go red.
- [x] ✅ QA passes (`./scripts/qa-all.bash`), twice in a row, 640 files.
- [ ] 🚫 `acceptance.bash` green with the flag on and with it off. **Blocked
  twice over.** Enabling needs a host image rebuild, which cannot happen in the
  container. And I1 is currently a TRUE red: this session leaked the token into
  the host-mounted transcript (see `JOURNAL/` 08:55), so the gate correctly fails
  until the token is rotated. Both are owner actions.
- [ ] 🔄 `qa-reviewer` returns no BLOCK or FIX-BEFORE-MERGE finding.

## Risks & Mitigations

| Risk                                                                         | Impact | Probability | Mitigation                                                      |
| ---------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------- |
| Stale skill persists into a disabled session via the host-mounted skills dir | H      | H           | Phase 4 removes it actively; probed by `acceptance.bash` (I6)   |
| Token leaks into a transcript through a diagnostic                           | H      | M           | Wrapper never echoes the value; probe asserts it                |
| PID 1 is not `tini` in a CI variant, so recovery fails obscurely             | M      | M           | Fail fast naming the cause; noted against Plan 00068            |
| Runaway recursive spawning exhausts quota                                    | M      | M           | `CCY_CHILD_CLAUDE_DEPTH` bounded, default max depth 1           |
| Image and launcher versions drift, so the flag is set but tooling is absent  | M      | M           | Phase 4 fails fast; Phase 5 bumps both version values together  |
| Agent uses child processes where the `Agent` tool is correct                 | L      | M           | The skill says when not to use it                               |
| A child inherits the project harness and answers something unrelated         | M      | H           | Measured, not predicted; the skill shows neutral vs project cwd |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00092-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed; the seven invariants defined and gated before any code — `5c02138`
- Feature landed end to end: wrapper, skill, entrypoint wiring, IaC, CCY 3.46.0 /
  container 2.29 — `8e4480a`
- Probes made falsifiable rather than merely green; `selftest-probes.bash` found two
  real defects in them — `1ff0a70`
- The wrapper's error path was dead code under `set -e`; measured, not theorised —
  `2823cdb`
- The cwd-inheritance trap found by the functional probe and written into the skill —
  `6d36539`, `16a3137`
- Host steps put into scripts rather than a chat message, per PlanTriage — `4d0e5c4`
- First `qa-reviewer` round: all four FIX-BEFORE-MERGE findings resolved — `6ddef5b`
- `planlib` 1.1.1, extracted from this plan's failed host deploy: a relative playbook
  path now resolves against the repo root, not the cwd — `f4a2799`
- Confirming re-review: the I1 probe was skipping 38% of what it reported as searched —
  `2a9a000`, findings recorded at `file:line` in `7cf4932`
