# Plan 00068 — the CI entrypoint, specified (D29)

The plan states that `ccy` owes CI **exactly two things**. Deliverable 1 has
[label-convention-spec.md](label-convention-spec.md) — keys, algorithm, writer, reader,
comparison. Deliverable 2 had no specification document. This is it.

## Why this was not noticed for seven rounds

What existed for deliverable 2 was a **mechanism**, and it is genuinely well specified:

| Already settled                                                | Where                           |
| -------------------------------------------------------------- | ------------------------------- |
| `Dockerfile.ci` *ships* the CI entrypoint as a file            | `phase3-image-layering.md` §3.1 |
| it does **not** become the default `ENTRYPOINT`                | same, and Task 3.1's D-block    |
| selection is the caller's, explicitly, via `--entrypoint`      | same                            |
| it carries `LABEL claude-yolo-ci-version`, gated independently | same                            |
| a project selects the base with `FROM claude-yolo:ci`          | `phase3-image-layering.md` §3.2 |

None of that says **what the file contains**, or what it is called. Round 2 had to write
`--entrypoint /opt/claude-yolo/<ci-entrypoint>` with a literal placeholder
(`fable-review-2.md:38`) — the review could not name the file because the name did not exist.
It also guessed the wrong directory: the desktop entrypoint is at `/usr/local/bin/entrypoint.sh`
(`Dockerfile:184`, `:215`), not under `/opt/claude-yolo/`.

Round 4 then asserted the opposite, in bold: *"Decision 6 / the CI entrypoint … is **well
specified**"*, *"the one half of 'the two things' that is **not hand-waved**"*
(`fable-review-4.md:180-190`). Its evidence covers shipping and selection only.

This is the plan's signature failure mode one level up — **a true statement about the mechanism
presented as a stronger statement about the deliverable**. Structurally it is D27 again: two
siblings named as a pair throughout, work done on one of them.

## The "prepares nothing" conflation — corrected

Task 5.3 says: *"Under the Decision 6 CI entrypoint the minimum boot allowlist is EMPTY — it
prepares nothing and authenticates nothing."*

The **allowlist** half is correct and remains correct: the CI entrypoint makes no network call, so
a caller's egress allowlist needs nothing in it merely to boot. That was the finding's purpose and
it survives.

The **behaviour** half does not follow. Preparing local state requires no network. Read from
source, `entrypoint.sh` writes three assertions into `/root/.claude.json` (`:240-263`):

- `hasCompletedOnboarding: true` (`:243`)
- `bypassPermissionsModeAccepted: true` (`:245`)
- `projects["/workspace"].hasTrustDialogAccepted = true` (`:257`)

An entrypoint that genuinely prepares nothing leaves all three unset, and a CI job then meets
onboarding and trust prompts with no TTY to answer them — **it spins**. That is precisely the
group-B failure this plan exists to prevent, reintroduced by its own specification.

**Corrected statement**: the CI entrypoint performs **no network I/O and no authentication**, and
performs the **minimum local state preparation** required for a non-interactive start. Those are
compatible; the original sentence collapsed them.

## Name and path

```
/usr/local/bin/ccy-ci-entrypoint.sh
```

Alongside the desktop entrypoint (`Dockerfile:184` puts `entrypoint.sh` in `/usr/local/bin/`), not
under `/opt/claude-yolo/` — that tree holds data and docs (`ccy-startup-info.txt`, `docs/`,
`plugins/`, the ctrl+z sentinel), not executables on `PATH`. Prefixed `ccy-` so it is unambiguous
in a `podman run` line read months later.

## The `tini` problem — a defect in the selection mechanism

`Dockerfile:215` is:

```
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]
```

`tini` is PID 1. `--entrypoint` replaces the **entire** vector, not its last element — so the
endorsed selection mechanism (*"selection is the caller's, explicitly"*) **silently drops `tini`**.
The CI job's entrypoint becomes PID 1 itself, losing:

- **zombie reaping** — every subprocess `claude` spawns and abandons accumulates as a zombie for
  the life of the job;
- **signal forwarding** — a CI cancellation (`SIGTERM` to PID 1) is not forwarded to children,
  so cancelled jobs hang until the runner's hard kill.

Nothing in the plan mentions this. It is a consequence of the mechanism the plan endorses, and it
is invisible in a short job — which is exactly why it must be specified rather than discovered.

**Required caller form:**

```
podman run --entrypoint /usr/bin/tini <image> -- /usr/local/bin/ccy-ci-entrypoint.sh <cmd>...
```

**Rejected alternative**: making `ccy-ci-entrypoint.sh` re-`exec` itself under `tini`. It hides a
process-model change inside a script, and a caller who already did the right thing gets two `tini`s.
The caller is already writing an explicit `--entrypoint`; the contract belongs there, documented.

> **Unproven.** That `--entrypoint` replaces the whole vector is OCI/podman documented behaviour,
> not something measured here. It is recorded as **G1** in
> [hardware-proof-checklist.md](hardware-proof-checklist.md) and must be demonstrated before the
> caller form above is published as guidance.

## Behaviour: every desktop step, and its CI disposition

Read from `files/var/local/claude-yolo/entrypoint.sh` in this checkout.

| #   | Desktop behaviour                                                    | Line       | CI  | Why                                                                                         |
| --- | -------------------------------------------------------------------- | ---------- | --- | ------------------------------------------------------------------------------------------- |
| 1   | `set -e`                                                             | `:6`       | ✎   | **Strengthen** to `set -euo pipefail` — repo standard; `set -e` alone misses unset vars     |
| 2   | `GH_TOKEN` unset ⇒ fatal                                             | `:14-17`   | ✗   | A CI job may legitimately have no token (read-only build). Requiring it fails valid jobs    |
| 3   | copy `gitconfig` from `/tmp/claude-config-import`                    | `:24-26`   | ✗   | Host-session artifact; the mount does not exist in CI                                       |
| 4   | `gh auth login --with-token` ⇒ fatal on failure                      | `:33-36`   | ✗   | Authentication is the caller's business. This is one of the three `api.github.com` touches  |
| 5   | `GITHUB_USERNAME` identity assertion                                 | `:39-51`   | ✗   | Consequence of 4                                                                            |
| 6   | `gh auth status` ⇒ fatal                                             | `:53-56`   | ✗   | Consequence of 4                                                                            |
| 7   | `ssh-agent` + `ssh-add` each `SSH_KEY_PATHS`, else a 20-line warning | `:59-89`   | ✗   | CI does not push over SSH from inside the container; the warning is desktop UX              |
| 8   | `curl api.github.com/meta` → pin host keys, else `accept-new`        | `:111`     | ✗   | Consequence of 7. This is the soft network touch; dropping it is what empties the allowlist |
| 9   | `export IS_SANDBOX=1`                                                | `:146`     | ✓   | Root-detection bypass. Same need in CI — the container runs as UID 0                        |
| 10  | `export CCY_DISABLE_SUSPEND=1`                                       | `:150`     | ✓   | Harmless and cheap. No TTY in CI so ctrl+z cannot arrive, but the env var costs nothing     |
| 11  | ctrl+z patch sentinel warning                                        | `:155-158` | ✗   | Diagnostic for an interactive freeze that cannot occur without a TTY                        |
| 12  | `mkdir -p /workspace/.claude/ccy` + symlink `/root/.claude` → it     | `:185-195` | ✗   | **See below — the most consequential drop**                                                 |
| 13  | write/merge `settings.json` (LSP, PHPantom)                          | `:204-226` | ✗   | Lands inside the symlink of 12; and LSP is a desktop authoring affordance                   |
| 14  | install the PHPantom plugin into `/root/.claude/plugins/`            | `:230-237` | ✗   | Same; consequence of 12                                                                     |
| 15  | create `/root/.claude.json` (onboarding + bypass-permissions)        | `:240-252` | ✓   | **Required.** Without it the job hits onboarding and spins                                  |
| 16  | set `projects["/workspace"].hasTrustDialogAccepted`                  | `:257-263` | ✓   | **Required.** Without it the job hits the trust dialog and spins                            |
| 17  | source `/workspace/.claude/ccy/ccy.env`                              | `:269-274` | ✗   | **Executes code from the checkout — see below**                                             |
| 18  | optional `CCY_CLAUDE_WRAPPER` wrap, then `exec "$@"`                 | `:280-284` | ✎   | Keep the bare `exec "$@"`; **drop** the wrapper hook (desktop supervisor concern)           |

Legend: ✓ reproduce · ✗ drop · ✎ change

### 12 — why the symlink must not be reproduced

`:195` does `ln -sf /workspace/.claude/ccy /root/.claude`. In CI, `/workspace` **is the checkout**.
Reproducing this writes Claude Code session state, settings and plugins into the build tree, where
it can be picked up by a `git status` check, committed by an automated job, or cached between runs.
`phase3-image-layering.md:257` already flagged that `entrypoint.sh:195`'s symlink lands somewhere
undesirable; this states the consequence and the decision.

The distinction that makes item 15 safe while 13 is not: `/root/.claude.json` is a **file at
`/root/`**, outside the symlink, so writing it never touches the checkout. `/root/.claude/` is the
symlinked directory. Same-looking paths, opposite blast radii.

### 17 — why `ccy.env` must not be sourced

`:269-274` sources `/workspace/.claude/ccy/ccy.env` if present — arbitrary shell from the checked-out
tree, executed before the workload. On the desktop that is a deliberate, defensible design: the
operator owns the workspace, and the plan's Decision 4 rests on exactly that premise. In CI the
checkout may be a contributor's branch, so the same line becomes "run the branch author's shell
code". The plan's stated posture — *`ccy` in CI is for trusted automation only* — is a reason this
is not a vulnerability today; it is not a reason to reproduce the behaviour in an entrypoint whose
whole purpose is the untrusted-adjacent case.

## What it must NOT do

- **No network I/O of any kind.** This is what makes the empty boot allowlist true. Any future
  addition that calls out invalidates Task 5.3's surviving half.
- **No credential handling** — no token read, no `gh auth`, no `ssh-add`.
- **No writes anywhere under `/workspace`.** The checkout is the caller's, not ours.
- **No `ENTRYPOINT` directive in `Dockerfile.ci`.** Unchanged from Task 3.1 — shipping is not
  selecting.
- **No policy.** No egress allowlist, no proxy address, no permission surface. Runtime properties,
  per restatement §3.

## Proof obligations before this may be implemented

Recorded as **group G** in [hardware-proof-checklist.md](hardware-proof-checklist.md):

| ID  | Claim                                                                                             | Why it must be measured                               |
| --- | ------------------------------------------------------------------------------------------------- | ----------------------------------------------------- |
| G1  | `--entrypoint` replaces the entire `ENTRYPOINT` vector, so `tini` is dropped                      | The caller form above is wrong if it does not         |
| G2  | `claude` with no `/root/.claude.json` blocks on onboarding rather than proceeding                 | Items 15/16 are unnecessary if it does not block      |
| G3  | `claude` with `hasCompletedOnboarding` but no `hasTrustDialogAccepted` blocks on the trust prompt | Distinguishes which of the two writes is load-bearing |

G2 and G3 are **group-B-shaped** — telling "blocked on a prompt" from "ran and was killed by
`timeout`" needs captured output, a real session and real quota, so they inherit group B's
conclusion: an interactive investigation with the owner, not a hand-over script. They are recorded
rather than automated, and the specification above states plainly what changes if either comes back
the other way.

## Status of this document

**Design only.** No `Dockerfile.ci` and no CI entrypoint script exist in this repo — confirmed by
search in Round 7 (`fable-review-7.md:38-40`) and unchanged. Writing either is implementation, which
this plan's Non-Goals forbid; it belongs to the implementation plan. What changes with this document
is that deliverable 2 is now specified to the same standard as deliverable 1, so the implementation
plan has something to implement.
