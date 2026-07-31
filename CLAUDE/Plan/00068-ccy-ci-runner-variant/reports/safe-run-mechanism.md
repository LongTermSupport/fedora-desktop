# Plan 00068 — the safe run mechanism (Phase 3, redone under D33)

Phase 3 as written specified a ccy-owned CI **image**. Under the corrected ownership model — Ansible
provides the VM, the **project** drives the image, we provide a safe mechanism for running project
podman containers as CI workloads — that is not what we owe. This is Phase 3 redone against the
steer instead of against itself.

Everything below is read from source in this checkout.

## The question asked first, because D33 exists

**Is there existing code that already does this?** `claude-yolo` constructs a hardened
`podman run` every time it launches a desktop session. The reuse instinct says extract it.

Read it before deciding — `claude-yolo:2770-2792`:

```bash
container_cmd run $DOCKER_FLAGS --rm \
    --name "$CONTAINER_NAME" \
    $NETWORK_FLAG \
    --device /dev/dri:/dev/dri \
    "${DOCKER_MOUNTS[@]}" "${SSH_MOUNTS[@]}" "${GUI_MOUNTS[@]}" \
    -e CLAUDE_CODE_OAUTH_TOKEN -e GH_TOKEN \
    -e "GITHUB_USERNAME=$GITHUB_USERNAME" \
    …
    -w /workspace \
    "$IMAGE_NAME" \
    claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"
```

**Extraction is the wrong call, and the reason is specific rather than aesthetic.** This is not a
factored-out function — it is the tail of a 141 KB script, and every part of it that CI would want is
interleaved with parts CI must not have:

| What it does                                               | Why CI cannot take it                                                                                                                                                                  |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `--device /dev/dri:/dev/dri`, **unconditional**            | **E6, confirmed by the owner's host run**: a missing `--device` path is fatal to podman (`EXIT 125`). A CI runner VM is headless. This single flag makes the launcher unrunnable there |
| `GUI_MOUNTS`, `SSH_MOUNTS`                                 | X11/Wayland sockets and an ssh-agent — desktop session concerns with no CI meaning                                                                                                     |
| `claude --dangerously-skip-permissions` as the **command** | Hard-coded. CI must run **arbitrary** commands — the owner's CI runs deterministic QA tools *as well as* claude-powered tasks                                                          |
| credential resolution preceding every run path             | ~16 interactive prompt sites (`select_token`/`create_token`, ~`:900-1150`) that a CI job cannot answer                                                                                 |

And refactoring `claude-yolo` to extract it would edit the launcher that runs every desktop session —
forbidden by the no-desktop-degradation constraint, and gated behind a `CCY_VERSION` bump and host
testing besides.

**So: written fresh.** That is also the simpler answer, which is worth stating plainly because
"reuse" usually is. Once the desktop-only concerns are gone the CI run line is roughly eight flags.
Duplicating eight flags is cheaper and far safer than extracting them from a script whose other
133 KB is exactly what must not run.

## What the mechanism is

A small, purpose-built runner, deployed to the CI VM by Ansible, which takes a project-built image
and a command, and runs it hardened. It is the *only* thing this estate puts on the CI path.

```
podman run --rm \
    --name "ccy-ci-${PROJECT}-${RUN_ID}-${ATTEMPT}"        # C7: salted, never get_next_container_name
    <egress flags per Task 5.1/5.2>                        # already designed
    -v "${CHECKOUT}:/workspace"                            # the job checkout
    -v "${CI_ENTRYPOINT}:/usr/local/bin/ccy-ci-entrypoint.sh:ro"   # from the VM, never from the image
    -w /workspace \
    -e CLAUDE_CODE_OAUTH_TOKEN -e GH_TOKEN                 # by NAME — value never enters argv
    --entrypoint /usr/bin/tini \
    "${PROJECT_IMAGE}" \
    -- /usr/local/bin/ccy-ci-entrypoint.sh "$@"            # arbitrary command, not hard-coded claude
```

### Why each departure from the desktop line

**`--entrypoint` is mandatory here, not optional.** The project image is `FROM claude-yolo:latest`,
so it **inherits the desktop `ENTRYPOINT`** (`Dockerfile:215`). Running a project image in CI without
overriding it runs `entrypoint.sh` — `GH_TOKEN`-or-die (`:14-17`), `gh auth login` (`:33-36`), the
ssh-agent block, and `ccy.env` **sourced from the checkout** (`:269-274`). That last one executes
shell from the branch under test. This is the "mis-selected entrypoint" residual the plan already
identified in production at a consumer; here it is the default outcome unless overridden.

**Overriding brings `tini` back as our problem — G1 is live, not retired.** `Dockerfile:215` is
`["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]`, and `--entrypoint` replaces the whole
vector. Desktop keeps `tini` precisely because it never overrides. The mechanism therefore sets
`--entrypoint /usr/bin/tini` and passes the CI entrypoint as an argument — so `tini` is PID 1 by
construction, and zombie reaping and signal forwarding (a cancelled CI job) survive.

**The CI entrypoint is bind-mounted from the VM, never baked into an image.** It is Ansible-provided,
sits on the runner, and is mounted read-only. Consequences: the **desktop image stays byte-identical**
(the constraint is satisfied structurally, not argued), projects do not have to `COPY` anything, and
the script is updated by re-provisioning the VM rather than by rebuilding every project image.

**The command is arbitrary.** `podman run … <image> npm test` and `… claude -p "triage this"` are the
same code path. Desktop hard-codes `claude`; CI must not, because the owner's CI is deterministic QA
tooling *and* AI tasks.

**Container naming is salted.** `claude-yolo:2619` uses `get_next_container_name` (`lib/common.bash:583`),
which computes a free name from `ps -a` + increment with **no lock**, from a `PROJECT_NAME` derived
from `basename $PWD` — and `:2747` then unconditionally `rm -f`s that name, which force-removes a
**running** container. Two concurrent jobs for one repo would have the second kill the first. That is
C7, already confirmed; the mechanism must not reuse this function.

**Tokens pass by NAME from the caller's environment.** `-e CLAUDE_CODE_OAUTH_TOKEN` with no `=value`
(`:2777`) — podman reads the value from the ambient environment, so the secret never enters argv or
the process table. `lib/token-management.bash:107` records this as deliberate (BSH-09). It matters
more in CI than on the desktop, because `ps` output reaches logs. What is dropped is everything that
*resolves* a credential — the CI caller supplies it; the mechanism never prompts, reads a keyring, or
touches `~/.claude-tokens/`.

**Preconditions are asserted before anything starts** — see
[ci-required-config.md](ci-required-config.md) for the full required-configuration set, the
collect-all-then-abort preflight contract, and the failure output format.

## What the CI entrypoint does — and why it is not "nothing"

Specified in [ci-entrypoint-spec.md](ci-entrypoint-spec.md); D29 corrected it from *"prepares
nothing"* to *no network I/O and no authentication, plus the minimum local state*. That minimum is
`/root/.claude.json` — `hasCompletedOnboarding`, `bypassPermissionsModeAccepted`, and
`projects["/workspace"].hasTrustDialogAccepted` — without which a TTY-less `claude` blocks on
prompts.

Under D33 this becomes cheap to reason about: **it always writes them and then `exec "$@"`.** A
deterministic QA run (`npm test`) pays two pointless file writes; a claude-powered run needs them.
Fewest code paths beats a conditional that has to detect which kind of workload it is.

It must still **not**: create the `/root/.claude` → `/workspace/.claude/ccy` symlink (`:185-195`),
source `ccy.env` (`:269-274`), authenticate, or touch SSH.

## What this design does not need

Recorded because these were all specified, and their absence should read as a decision:

- **No `LABEL` identity convention** — `podman build` answers staleness (D33).
- **No ccy-owned CI base image**, no `claude-yolo:ci`, no `Dockerfile.ci` shipped by this repo.
- **No overlay on the project image** — Task 3.3's decision stands (D33).
- **No registry, no image distribution** — the image is built and consumed on the same VM.
- **Nothing added to `claude-yolo:latest` or to any project image.**

The project adds CI tooling and MCP servers to **its own** Dockerfile, which is the founding steer
and needs no mechanism from us at all.

## Unproven, and honestly so

| ID     | Claim                                                                     | Status                                                                           |
| ------ | ------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| **G1** | `--entrypoint` replaces the entire vector, dropping `tini`                | **Live again** under this design. Cheap and read-only to measure                 |
| G2/G3  | `claude` blocks without the onboarding/trust flags                        | Group-B-shaped: needs captured output and real quota. Interactive with the owner |
| —      | Whether a bind-mounted entrypoint survives the image's own `WORKDIR`/user | Not yet checked                                                                  |
| —      | Egress flags (Task 5.1/5.2) under this argv shape                         | Designed, never executed; C1/C2 remain borrowed measurements                     |

Nothing here has been run. This is a specification, and the plan's Non-Goals forbid implementing it.
