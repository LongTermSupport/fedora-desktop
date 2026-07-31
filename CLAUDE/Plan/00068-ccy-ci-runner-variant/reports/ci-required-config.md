# Plan 00068 — required CI configuration, and the preflight that asserts it

Every precondition below is **asserted**, never summarised. No `<thing>_armed` boolean is derived
from whether a credential happens to exist; no `| default('')` is applied to a credential, because
that converts UNDEFINED into EMPTY and blinds the check. Where behaviour genuinely differs, the
**operator declares intent** with an explicit mode — which is `_enabled`, not `_armed`.
See [.claude/rules/no-armed-flags.md](../../../.claude/rules/no-armed-flags.md).

## The single most useful diagnostic: which layer is broken

A CI failure is only quick to fix if the reader knows **whose problem it is**. Three layers, three
different people, three different fixes:

| Layer       | Owner               | A failure here means                                 | Fixed by                             |
| ----------- | ------------------- | ---------------------------------------------------- | ------------------------------------ |
| **VM**      | Ansible / infra     | the runner was provisioned wrong or has drifted      | re-run the play; file an infra issue |
| **JOB**     | the workflow author | the workflow YAML or repo secrets are wrong          | edit `.github/workflows/*.yml`       |
| **PROJECT** | the project repo    | `.claude/ccy/Dockerfile` or the built image is wrong | edit the project Dockerfile          |

**Every assertion below is tagged with its layer, and the tag is printed in the failure.** This is
the difference between "CI is broken" and "add `CLAUDE_CODE_OAUTH_TOKEN` to repo secrets".

## Required configuration

### Layer VM — provisioned by Ansible

| #   | Requirement                                                     | Assertion                                                 | On failure, tell the operator                                       |
| --- | --------------------------------------------------------------- | --------------------------------------------------------- | ------------------------------------------------------------------- |
| V1  | `podman` present and usable rootless                            | `podman info` exits 0                                     | re-run the podman play; check subuid/subgid for the runner user     |
| V2  | Base image `claude-yolo:latest` present                         | `podman image exists claude-yolo:latest`                  | the runner was never provisioned with ccy; run the ccy play         |
| V3  | Base image carries `claude-yolo-version`                        | the label is **non-empty**                                | the image is not a ccy base, or predates the label; rebuild it      |
| V4  | Base version **matches** what this runner expects               | label == the runner's pinned `REQUIRED_CONTAINER_VERSION` | print both values; re-provision to rebuild the base                 |
| V5  | CI entrypoint present on the VM                                 | file exists, non-empty, mode `0555`                       | re-run the ccy play; it is Ansible-owned, never built into an image |
| V6  | Egress infrastructure present **when egress mode is requested** | proxy reachable / ruleset loaded                          | name the mode that was requested and what was missing               |

V4 exists because `Dockerfile:36` (`LABEL claude-yolo-version="2.22"`) carries an explicit
`⚠️ THIS VALUE MUST MATCH REQUIRED_CONTAINER_VERSION` comment. An invariant a comment asks a human to
maintain is one a machine should assert.

V6 is the only conditional item, and it is conditional on a **declared mode**, not on whether some
config file happens to be present.

### Layer JOB — supplied by the workflow

| #   | Requirement                                                      | Assertion                                               | On failure, tell the operator                                         |
| --- | ---------------------------------------------------------------- | ------------------------------------------------------- | --------------------------------------------------------------------- |
| J1  | Checkout path given and is a directory                           | non-empty, `-d`                                         | name the variable and the value received                              |
| J2  | Project image name given and the image exists locally            | non-empty, `podman image exists`                        | the build phase did not run or did not tag what the run phase expects |
| J3  | Run identity for container naming                                | `GITHUB_RUN_ID` and `GITHUB_RUN_ATTEMPT` both non-empty | required so concurrent jobs cannot collide — see C7                   |
| J4  | Mode declared explicitly                                         | exactly one of `--claude` / `--exec`                    | the mechanism must not infer intent from the command                  |
| J5  | **`--claude` only**: `CLAUDE_CODE_OAUTH_TOKEN` set and non-empty | present in the environment, non-empty                   | add it to repo secrets and map it in the workflow `env:`              |
| J6  | Any caller-declared required env vars are set                    | each named var non-empty                                | list every missing name at once                                       |
| J7  | A command to run                                                 | argv after `--` is non-empty                            | nothing to do is a configuration error, not a no-op                   |

**J3 is not optional.** `claude-yolo:2619` names containers with `get_next_container_name`
(`lib/common.bash:583`), which computes a free name from `ps -a` + increment with **no lock**, and
`:2747` then unconditionally `rm -f`s that name — which force-removes a *running* container. Two
concurrent jobs on one repo would have the second kill the first. The CI mechanism must never use
that function, and must salt with run id + attempt.

**J5 is gated on the declared mode, never on the token's presence.** "Token absent ⇒ skip the claude
work" is precisely the `_armed` pattern that is banned: it converts a misconfiguration into a silent
behaviour change. A `--claude` job with no token is an error, loudly.

**J6 is how a claude-powered triage task declares it needs `GH_TOKEN`.** The mechanism does not guess
which workloads need GitHub — the caller states it. Passing an unset variable through and letting the
workload fail deep inside `gh` is exactly the diagnosis problem this document exists to prevent.

### Layer PROJECT — from the project repository

| #   | Requirement                                 | Assertion                                                | On failure, tell the operator                                             |
| --- | ------------------------------------------- | -------------------------------------------------------- | ------------------------------------------------------------------------- |
| P1  | The project image descends from a ccy base  | `claude-yolo-version` label present on the project image | the image was not built `FROM claude-yolo:latest`                         |
| P2  | `/usr/bin/tini` exists in the project image | `test -x /usr/bin/tini` inside it                        | the project Dockerfile removed it; the run mechanism requires it as PID 1 |

**P1 is provenance, not staleness — the distinction matters.** Decision 2 retired the `LABEL`
convention for *staleness* because `podman build` already answers that. P1 asks a different question:
*is this image ccy-derived at all?* Label **inheritance** is what makes it work — a project image
inherits `claude-yolo-version` from its base, so its presence proves descent. That is the same
inheritance property that made it useless for staleness.

**P2 exists because the mechanism sets `--entrypoint /usr/bin/tini`.** `tini` is installed at
`Dockerfile:71` and used at `:215`, so it is present in every unmodified ccy image — but a project
Dockerfile *could* remove it, and the resulting podman error names a path, not a cause.

## The preflight contract

**Run every assertion, collect every failure, print them together, then abort.** Nothing proceeds —
no image is pulled, no container starts, no credential is read.

This is deliberate and worth defending, because a naive reading of *fail fast* says exit on the first
failure. The project's rule is that a failure must never be **skipped and continued past**, and
nothing here continues past anything: the preflight is a validation phase, not an operation. Exiting
on the first failure would turn a three-mistake workflow into **three CI round-trips**, each costing
minutes, and each revealing exactly one more problem. Collecting is fail-*complete on diagnosis* and
still fail-fast on execution.

**Exit codes**, so a wrapper can distinguish causes without parsing text:

| Code | Meaning                                                   |
| ---- | --------------------------------------------------------- |
| 0    | every precondition satisfied                              |
| 78   | configuration error (`EX_CONFIG`) — any preflight failure |
| 64   | usage error (`EX_USAGE`) — bad invocation                 |

## Output: giving the runner enough context

The reader is looking at a GitHub Actions log with no TTY, no colour, and possibly folded output.

**Every failure states four things** — expected, found, layer, remediation:

```
[FATAL] CI preflight failed: 2 problem(s).

  [JOB] CLAUDE_CODE_OAUTH_TOKEN is not set
        expected : a non-empty value, because mode --claude was declared
        found    : unset
        fix      : add CLAUDE_CODE_OAUTH_TOKEN to the repository secrets, then map it
                   in the workflow:   env:
                                        CLAUDE_CODE_OAUTH_TOKEN: ${{ secrets.CLAUDE_CODE_OAUTH_TOKEN }}

  [VM]  base image version mismatch
        expected : claude-yolo-version = 2.22  (this runner's REQUIRED_CONTAINER_VERSION)
        found    : claude-yolo-version = 2.19
        fix      : the runner VM is provisioned with a stale ccy base image.
                   Re-run the ccy play on the runner; this is an infra fix, not a workflow fix.

Context (no secrets):
  mode            : --claude
  project image   : claude-yolo:myproject  (sha256:1a2b3c…)
  base image      : claude-yolo:latest      (sha256:4d5e6f…)
  checkout        : /home/runner/work/myproject/myproject
  container name  : ccy-ci-myproject-1234567890-1
  podman          : 5.2.1
  egress mode     : proxy
```

**On GitHub Actions specifically** (detected by `GITHUB_ACTIONS=true`, never assumed), each failure is
*additionally* emitted as a workflow command so it surfaces as an annotation at the top of the run
rather than only inside a folded log:

```
::error title=CI preflight [JOB]::CLAUDE_CODE_OAUTH_TOKEN is not set — add it to repository secrets
```

The plain-text block is always printed. The annotation is an addition, never a replacement — if the
detection is wrong, no diagnostic is lost.

### Secrets

- **Never print a credential value, prefix, suffix, or length.** Report only `set` / `unset` /
  `set but empty`. GitHub masks *registered* secrets, but masking is not a guarantee to design
  against, and a length is a real if weak leak.
- **Pass credentials by name, never by value** — `-e CLAUDE_CODE_OAUTH_TOKEN`, not
  `-e CLAUDE_CODE_OAUTH_TOKEN="$value"`. This is already ccy's deliberate pattern
  (`lib/token-management.bash:107`, BSH-09) and it keeps the secret out of argv and the process
  table. It matters more in CI than on the desktop, because `ps` output reaches logs.
- The context block prints image **digests**, paths, versions and the resolved container name. None
  of those are secrets, and all of them are what an engineer needs to reproduce the run.

## Banned in this mechanism

Each of these converts a misconfiguration into a silent behaviour change:

- `<thing>_armed` derived from a credential's presence, and any `when:`-style gate consuming it.
- `<thing>_required: false` opt-outs.
- `| default('')` on a credential — turns UNDEFINED into EMPTY, and the guard stops seeing it.
- `2>/dev/null` and `|| true` to quiet a probe. Capture into a variable and report the reason.
- Warning and continuing. There is no advisory tier: a precondition either holds or the run stops.
- Inferring the mode from the command (`case "$1" in claude*)`). The caller declares it.

## Unproven

Nothing here has been executed; the plan implements nothing.

| Claim                                                                       | Status                                               |
| --------------------------------------------------------------------------- | ---------------------------------------------------- |
| `podman image exists` is the right presence test and exits non-zero cleanly | not measured                                         |
| `::error title=…::` renders as a job annotation from a composite step       | not measured                                         |
| P2's in-image `test -x` costs one fast container start                      | not measured; may be replaceable by an image inspect |
| G1 — `--entrypoint` replaces the whole vector, dropping `tini`              | still outstanding; P2 exists because of it           |
