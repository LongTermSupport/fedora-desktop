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

Verified in this container before planning:

- The token reaches PID 1 via `-e CLAUDE_CODE_OAUTH_TOKEN` at
  `files/var/local/claude-yolo/claude-yolo:3021`, passed by name so it never
  appears in the `podman run` argv.
- Container mode always has a token. The "Desktop" fallback that uses the host's
  own OAuth is host-mode only — `lib/token-management.bash:1024` states there is
  no Desktop fallback in container mode. So `/proc/1/environ` is a reliable source.
- A bare `claude -p` from the Bash tool fails with `Not logged in`. The same
  command with the token recovered from `/proc/1/environ` returns a real completion.
  A prototype wrapper worked on first run and left no token in the calling shell.
- `ccy.env` is already sourced **as shell** inside the container at
  `entrypoint.sh:336-341`, so the checked-out tree already controls the command
  that runs. Plan 00068 recorded this as `E10`. Putting the opt-in flag there adds
  no trust that the file does not already hold.
- Skills are staged in the image under `/opt/claude-yolo/skills/` and copied
  **unconditionally** to `/root/.claude/skills/` at `entrypoint.sh:295-304`, which
  runs *before* `ccy.env` is sourced. Both facts constrain the wiring.
- `/root/.claude` symlinks to `/workspace/.claude/ccy`, so the skills directory is
  **host-persisted** across sessions and gitignored at `.claude/ccy/.gitignore:17`.
  A skill installed by an enabled session therefore survives into a later disabled
  session unless it is actively removed.
- Deployment chain for any image asset: repo `files/opt/claude-yolo/...` →
  `play-claude-yolo.yml` copies to the host build context → `Dockerfile` COPYs into
  the image → `entrypoint.sh` installs into the session.

The dedupe scout checked 58 live plans and found none covering this. Nearest
neighbours are 00068 (CCY env config for CI), 00089 and 00048 (token injection),
and 00080 (network isolation); each touches token handling or CCY config, none
touches in-container child processes.

## Tasks

### Phase 1: Pin down what "no security degradation" means

- [x] ✅ **Task 1.1**: Write [SECURITY-MODEL.md](SECURITY-MODEL.md): the seven
  invariants, the threat model, and the explicit statement that the scrub is
  not a boundary under root.
- [x] ✅ **Task 1.2**: Write `acceptance.bash` on `_planlib.inc.bash` per
  PlanScriptStandards R1–R14, one probe per invariant, verdict at the end.
  - [x] ✅ Added `plan_require_container` to the library — the mirror of
    `plan_require_host`, because this gate judges the CONTAINER and would go
    vacuously green on the host. Both branches tested; library at 1.1.0.
  - [x] ✅ `selftest-probes.bash` proves each probe CAN fail. Its first run found
    two real defects: I1 reported a failed `grep` as a clean pass, and the I2
    negative case planted no violation because `bash -c` execs a lone command.
- [x] ✅ **Task 1.3**: Baseline captured. I2, I3 and I6 pass; **I1 correctly
  reports a real leak this session caused** by tracing the probe with `bash -x`,
  which put the token into the host-mounted session transcript. See `JOURNAL/`.
  The remedy is token rotation and belongs to the owner.

### Phase 2: The wrapper

- [ ] ⬜ **Task 2.1**: Write `files/opt/claude-yolo/optional/bin/ccy-claude`.
  Behaviour: honour an already-set `CLAUDE_CODE_OAUTH_TOKEN`; otherwise recover
  it from `/proc/1/environ`; fail fast with a named cause when absent; enforce
  `CCY_CLAUDE_DEPTH` against `CCY_CHILD_CLAUDE_MAX_DEPTH`; `exec claude "$@"`.
- [ ] ⬜ **Task 2.2**: Diagnostics to stderr, per StderrHygiene — the child's own
  stdout is the payload and must stay clean for `$(capture)` and `jq`.
- [ ] ⬜ **Task 2.3**: Confirm the token never reaches stdout, stderr, argv, or a
  file, by probe rather than by reading the script.

### Phase 3: The skill

- [ ] ⬜ **Task 3.1**: Write `files/opt/claude-yolo/optional-skills/child-claude/SKILL.md`
  modelled on the `browsing` skill: frontmatter `name`/`description`/`allowed-tools`,
  an announce line, and when **not** to use it.
- [ ] ⬜ **Task 3.2**: Document the real traps: the child needs `< /dev/null` or it
  waits on stdin; the child spends the same subscription quota as the parent;
  the child's transcripts land in the host-mounted project directory; the
  `Agent` tool is the right choice for most in-session work.

### Phase 4: Entrypoint wiring

- [ ] ⬜ **Task 4.1**: Move the conditional install **after** the `ccy.env` source
  in `entrypoint.sh`. The unconditional `/opt/claude-yolo/skills/` copy stays
  where it is; only the optional tree is gated.
- [ ] ⬜ **Task 4.2**: When enabled: symlink the wrapper onto `PATH` and copy the
  optional skill into `/root/.claude/skills/`. Announce it on stderr.
- [ ] ⬜ **Task 4.3**: When **not** enabled: actively remove a previously installed
  optional skill from the host-persisted skills directory. This is the
  idempotency requirement, not a nicety — without it the mode cannot be turned
  off. Never remove `/root/.claude/skills/browsing` or unrelated user content.
- [ ] ⬜ **Task 4.4**: Fail fast if the flag is set but the image lacks the optional
  tree, naming the version mismatch. Do not silently run without the tooling.

### Phase 5: Ship it through IaC

- [ ] ⬜ **Task 5.1**: Add copy tasks to `playbooks/imports/play-claude-yolo.yml`
  for the new build-context files, following the existing skills pattern
  including the stale-file removal task.
- [ ] ⬜ **Task 5.2**: Add the `COPY` lines to `files/var/local/claude-yolo/Dockerfile`.
- [ ] ⬜ **Task 5.3**: Bump `LABEL claude-yolo-version` in the Dockerfile **and**
  `REQUIRED_CONTAINER_VERSION` in `claude-yolo` to the same new value; the
  image changed, so a running container must be rebuilt.
- [ ] ⬜ **Task 5.4**: Bump `CCY_VERSION` only if the launcher itself changes.
  Confirm whether it does; if not, record why no bump was needed.
- [ ] ⬜ **Task 5.5**: Add a commented, disabled example line to this project's own
  `.claude/ccy/ccy.env` so the option is discoverable where it is set.

### Phase 6: Verify and review

- [ ] ⬜ **Task 6.1**: Run `./scripts/qa-all.bash` and fix every finding.
- [ ] ⬜ **Task 6.2**: Update `docs/` and `CLAUDE/ContainerRules.md` for the new
  option. One owner for the fact, links from elsewhere.
- [ ] ⬜ **Task 6.3**: Run the `qa-reviewer` agent over the full plan diff and
  resolve every BLOCK and FIX-BEFORE-MERGE finding. Required, not optional.
- [ ] ⬜ **Task 6.4**: (On HOST, not in CCY) run `play-claude-yolo.yml`, rebuild the
  image, then run `acceptance.bash` in a container **with** the flag and again
  **without** it. Both runs must pass.

## Dependencies

- Interacts with Plan 00080 (network isolation): a child shares the parent's
  network namespace, so it inherits any isolation rather than escaping it.
  Confirm, do not assume.
- Interacts with Plan 00068 (CI runner variant): a CI entrypoint may not be `tini`
  at PID 1. The wrapper's fail-fast path must be legible there.

## Technical Decisions

### Decision 1: Recover the token from `/proc/1/environ`, not from a file or an env alias

**Context**: the child needs the token; three mechanisms could supply it.
**Options considered**:

- *`env` key in `settings.json`* — documented to reach subprocesses, but
  `/root/.claude` is the host-mounted project directory, so this writes a live
  credential into the project tree. Rejected outright.
- *A second, non-scrubbed variable name* — works, because only the exact name is
  stripped. It makes the credential readable by every command in the session and
  therefore by every transcript. A real widening. Rejected.
- *Read `/proc/1/environ` inside the wrapper* — the value never enters a variable
  the agent composes, never appears in argv, never reaches a transcript.
  **Decision**: `/proc/1/environ`, because it is the only option that adds no new
  exposure surface over what root in this container already has.
  **Date**: 2026-09-02

### Decision 2: The opt-in lives in `ccy.env`, and is a capability declaration, not a security control

**Context**: an in-container gate cannot bind an agent running as root.
**Decision**: state this plainly rather than implying the flag contains anything.
`ccy.env` is already sourced as shell in-container, so the flag grants nothing the
file could not already do. Its job is to declare intent and to keep the tooling and
the skill out of sessions that did not ask for them.
**Date**: 2026-09-02

### Decision 3: Pass arguments through verbatim

**Context**: the wrapper could inject `--dangerously-skip-permissions` for convenience.
**Decision**: it does not. Injecting it would silently widen what a child may do,
which is precisely the degradation this plan forbids. The caller passes what it needs.
**Date**: 2026-09-02

## Success Criteria

- [ ] With the flag set, a child `claude -p` returns a real completion.
- [ ] Without the flag, no wrapper on `PATH` and no child-claude skill in
  `/root/.claude/skills/`, including in a session that follows an enabled one.
- [ ] `acceptance.bash` passes with the flag on and with it off.
- [ ] The token appears in no file under `/workspace`, no argv, and no variable in
  the agent's Bash-tool environment, verified by probe.
- [ ] Depth limit refuses a grandchild spawn.
- [ ] QA passes (`./scripts/qa-all.bash`).
- [ ] `qa-reviewer` returns no BLOCK or FIX-BEFORE-MERGE finding.

## Risks & Mitigations

| Risk                                                                         | Impact | Probability | Mitigation                                                       |
| ---------------------------------------------------------------------------- | ------ | ----------- | ---------------------------------------------------------------- |
| Stale skill persists into a disabled session via the host-mounted skills dir | H      | H           | Task 4.3 removes it actively; probed by `acceptance.bash`        |
| Token leaks into a transcript through a diagnostic                           | H      | M           | Wrapper never echoes the value; probe asserts it                 |
| PID 1 is not `tini` in a CI variant, so recovery fails obscurely             | M      | M           | Fail fast naming the cause; noted against Plan 00068             |
| Runaway recursive spawning exhausts quota                                    | M      | M           | `CCY_CLAUDE_DEPTH` bounded, default max depth 1                  |
| Image and launcher versions drift, so the flag is set but tooling is absent  | M      | M           | Task 4.4 fails fast; Task 5.3 bumps both version values together |
| Agent uses child processes where the `Agent` tool is correct                 | L      | M           | The skill says when not to use it                                |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00092-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Plan filed
