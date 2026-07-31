# Plan 00072: ccy must ASSERT the engine is rootless, not infer it

**Status**: In Progress
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: High

## Overview

`ccy` runs `claude --dangerously-skip-permissions` (`claude-yolo:2792`) and bind-mounts the
project at `/workspace`. The entire safety case for that rests on **the container engine being
rootless**, so that container uid 0 maps to an unprivileged host user through a user namespace.

`ccy` checks two things today, and neither of them asks the engine.

| Where              | What it does                                                   | Why it is not enough                                                    |
| ------------------ | -------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `claude-yolo:1178` | Refuses to run when the **invoking user** is uid 0             | Correct, and kept — but it constrains the *client*, not the engine      |
| `claude-yolo:1188` | For docker only, compares the **context name** to `rootless`   | A context can be named anything; `DOCKER_HOST` overrides context wholly |
| `claude-yolo:1208` | Comment: *"Podman … is inherently rootless - no check needed"* | An inference presented as a check. Nothing verifies the antecedent      |

The engine can answer directly, and Plan 00068's host run already proved the call works:
`podman info --format '{{.Host.Security.Rootless}}'` returned `rootless: true`
(`00068 …/triage-runs/20260731-225344`, engine section).

This is the project's own rule applied to `ccy`: **a precondition is asserted, never summarised**
([.claude/rules/no-armed-flags.md](../../../.claude/rules/no-armed-flags.md)).

## Goals

- Replace the inference at `claude-yolo:1185-1208` with an **assertion against the engine's own
  report**, for podman and docker alike.
- **Unknown is not safe.** A failed, empty or unparseable report is a hard stop, not a pass.
- Keep the uid-0 guard exactly as it is — correct and cheaper.
- Make the decision **testable without a container engine**.

## Non-Goals

- No change to what `ccy` does once the assertion passes.
- No new flag and **no override**. An escape hatch here would be the `_armed` shape the project
  bans — the point is that this precondition cannot be opted out of.
- No image content change (`Dockerfile`/`entrypoint.sh` logic untouched ⇒ no
  `REQUIRED_CONTAINER_VERSION` bump).

## Design

### Why "unknown ⇒ stop" is the load-bearing decision

Plan 00068's group-F probe measured the exact failure this guard must avoid. Asking podman for a
label that does not exist returns **exit 0 and zero bytes** — silent empty. A naive comparison of
two unknowns therefore comes back *equal*, and the check reports the safe-sounding answer while
having measured nothing (`00068 reports/host-run-verdicts.md` §3).

A rootlessness check has the same shape: `info` can fail, print nothing, or print a field that has
moved. If empty were treated as "fine", the guard would pass hardest exactly when it is least able
to see — loudest when blindest.

So the verdict is three-valued (`rootless` / `rootful` / `unknown`) and **only `rootless`
proceeds**.

### Split for testability

The decision is a pure function of `(engine, raw report)`. It runs no command, so it is testable
with no engine present:

```
engine_rootless_verdict <engine> <raw-report>   # -> rootless | rootful | unknown
```

The wrapper in `claude-yolo` runs `info`, captures stdout+stderr and the exit code, hands them to
the pure function, and fails loud with a remediation naming what to do.

## Tasks

### Phase 1 — Implement

- [x] ✅ **Task 1.1**: Add the pure `engine_rootless_verdict()` to
  `files/var/local/claude-yolo/lib/common-pure.bash` — **not** `common.bash` as first written
  here. `common.bash` calls `exit 1` at file scope when the engine is not on `PATH` (`:36-40`),
  so a test sourcing it would need a container engine installed to test a function whose whole
  point is that it needs none. `common-pure.bash` exists for exactly this ("safe to source from
  any host shell without triggering a podman-check exit"). The querying wrapper
  `engine_assert_rootless()` does live in `common.bash`, where `container_cmd` is.
- [x] ✅ **Task 1.2**: Replace `claude-yolo:1185-1208` with the assertion; delete the
  context-name check and the "no check needed" comment.
- [x] ✅ **Task 1.3**: Bump `CCY_VERSION` (minor — new guard) in the same commit, per
  [.claude/rules/ccy-version-bump.md](../../../.claude/rules/ccy-version-bump.md).

### Phase 2 — Prove it discriminates

- [x] ✅ **Task 2.1**: `scripts/test-ccy-rootless-guard.bash` — table-driven cases over real
  recorded engine output: podman rootless/rootful, docker rootless/rootful, empty, `info`
  failure, and a moved field.
- [x] ✅ **Task 2.2**: **Wire it into CI** (`.github/workflows/qa.yml`). An unrun test is
  decoration — `scripts/test-ccy-ssh-probe.bash` is the precedent for a ccy bash test that runs
  nowhere.
- [x] ✅ **Task 2.3**: Confirm each negative control fails for the *stated* reason, not
  incidentally.

### Phase 3 — Correct the stale comment found on the way

- [x] ✅ **Task 3.1**: `Dockerfile:211` says *"USER directive is NOT set here - we use --user flag
  at runtime"*. **No `--user` flag is ever passed** — `DOCKER_FLAGS` is only `-i`/`-it`
  (`claude-yolo:2696`/`:2698`) and the run at `:2770-2792` has none. Harmless under a rootless
  engine, but it documents a control that does not exist. Correct it to state what actually
  provides the mapping.

## Proof obligations

| ID  | Claim                                                      | How it gets settled                       |
| --- | ---------------------------------------------------------- | ----------------------------------------- |
| P1  | `podman info` exposes `.Host.Security.Rootless`            | ✅ Plan 00068 host run, engine section    |
| P2  | The guard stops a rootful engine                           | Task 2.1 negative control                 |
| P3  | The guard stops on unknown/empty                           | Task 2.1 negative control                 |
| P4  | The guard still passes on this host's real rootless podman | ⬜ owner runs the test script on the HOST |

## Risks & Mitigations

| Risk                                                               | Impact | Probability | Mitigation                                                           |
| ------------------------------------------------------------------ | ------ | ----------- | -------------------------------------------------------------------- |
| The guard blocks a legitimate rootless setup it fails to recognise | H      | M           | P4 on the real host; the refusal prints the raw report it rejected   |
| A future engine moves the field and every run becomes `unknown`    | M      | M           | That is the intended failure direction — loud, never silent-pass     |
| Treating `unknown` as safe creeps back in later                    | H      | L           | Task 2.1 pins it as a negative control with the Plan 00068 rationale |

## Dependencies

- **Depends on**: Plan 00068's host run for P1.
- **Blocks**: nothing. Independent hardening, valuable on the desktop today.

## Success Criteria

- [x] ✅ `ccy` refuses to start unless the engine itself reports rootless.
- [x] ✅ Empty / failed / unparseable reports are refusals, proven by test.
- [x] ✅ The test runs in CI.
- [ ] ⬜ Desktop behaviour is unchanged on a rootless engine.

## Notes & Updates

Found while answering the owner's challenge to a Plan 00068 line citing `/root/.claude`. The
answer was that `ccy` already refuses to run as root (`claude-yolo:1178`) and podman here is
rootless, so `/root` is the in-container HOME of a userns-mapped unprivileged identity — **not**
host root. The owner then asked for a fail-fast if a rootful run is ever attempted; this plan is
that. No recovery cron — the owner asked for crons to be stopped.

## Delivery & Milestones

- Blow-by-blow: `JOURNAL/`
