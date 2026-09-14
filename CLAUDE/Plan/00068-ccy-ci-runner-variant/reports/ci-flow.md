# Plan 00068 Task 3.5 — the CI flow

The deliverable the invented "only conditionals" constraint was preventing (see PLAN.md, the
framing correction). A CI mode is **a different flow through the launcher**, sharing `lib/*.bash`
and the image/token seams. It is not the desktop flow with guards bolted on, and it is not a
second product.

Every line reference is `files/var/local/claude-yolo/claude-yolo` unless prefixed.

## The flow

| #   | Step               | Reuses                                                           | Skips                                                        |
| --- | ------------------ | ---------------------------------------------------------------- | ------------------------------------------------------------ |
| 1   | Parse and validate | existing flags (`:135-169`)                                      | —                                                            |
| 2   | Project name       | `get_project_name` (`common.bash`)                               | —                                                            |
| 3   | Credentials        | `select_token` by explicit name; `:1033` reads the token file    | SSH discovery `:864`, `build_ssh_mounts_and_validate` `:870` |
| 4   | Image              | `validate_container_version` `:1430`, project build `:1451-1528` | the daily auto-update `:1254-1360`                           |
| 5   | Container name     | caller-supplied, **required**                                    | `get_next_container_name`, the `rm -f` net `:2741`           |
| 6   | Mounts             | `$PWD:/workspace` + config temp `:1770-1773`                     | `GUI_MOUNTS` `:2697-2721`, `SSH_MOUNTS`                      |
| 7   | Run                | `container_cmd run` with a fixed flag set                        | `--device /dev/dri` `:2767`, `-it` (use `-i`)                |
| 8   | Exit               | the container's status, propagated                               | `save_launch_config` `:2607`, `stty` `:2726-2729`            |

~~Networking and compose are **deferred entirely** (lts-infra Plan 00030: the case study does not
need them), so steps 5–7 pass no `--network` and the flow never enters `:1789-2498` or the
preflight at `:2518-2593`.~~

> **SUPERSEDED by [DECISIONS.md](../DECISIONS.md) §6** (owner, 2026-08-01, `9f514222`): *"i would
> not assume that CI doesn't need compose or podman network stuff"*. **CI keeps the capability and
> drops only the negotiation** — §6 gives the keep/drop split function by function, and Plan 00113
> Phase 2b implements it. This paragraph was written at `e67abde2`, 07:24 the same day, *after*
> the decision was already in the branch: written contradicted, not overtaken by it.
>
> It is load-bearing, not decorative — see the note on the derivation table below.

### Step 3 — credentials, token-first

`GH_TOKEN` is taken from the environment (requirement 5). `GITHUB_USERNAME` is derived from it
with `gh api user --jq .login` — the call `ssh-handling.bash:475` already makes as a cross-check,
promoted to source. The Claude token comes from `--token <name>`; a name that does not resolve is
a **fail-fast**, never a prompt.

Both container-side guards already tolerate this: `entrypoint.sh:39` gates the identity check on
`[ -n "$GITHUB_USERNAME" ]` and `:59` gates SSH setup on `[ -n "$SSH_KEY_PATHS" ]`.

### Step 4 — the image is the product, untouched

`.claude/ccy/Dockerfile` → `claude-yolo:${PROJECT_NAME}` (`:1447`, `:1627`), rebuilt on a
Dockerfile-hash or base-version change (`:1464-1482`). **No CI-specific image, no `Dockerfile.ci`,
no Ansible-side staleness logic** — that is Decision 1, and it is the one thing this plan must not
touch.

The daily auto-update is off via the existing `CCY_AUTO_UPDATE=0`: on a runner the image is a
pinned artefact refreshed out of band, not a thing that mutates mid-job.

### Step 7 — the run

Differences from `:2764-2786`, each already argued elsewhere:

- **no `--device /dev/dri`** — measured `exit 125` on a headless host (E6)
- **`-i`, never `-it`** — no TTY
- **`--disallowedTools <class list>` is ADDED** — the one addition here, and the only
  security-carrying flag the desktop path does not already pass. Decision 9's restricted tool
  surface rides *alongside* `--dangerously-skip-permissions`, which CI **keeps** unchanged
  from `:2764-2786` (so it is not itself a difference): E9 measured on 2026-08-10 that the two
  compose. The per-class list is [ci-tool-surface.md](ci-tool-surface.md)
- **`--mcp-config <container-local path>`** — Decision 7; never under `/root/.claude`, which
  `entrypoint.sh:183-195` symlinks into the checkout

## Requirement 1, re-derived: ~6 sites, not 46 — SUPERSEDED, see the note below the table

`ci-required-config.md` §4.3 groups the 46 census sites by owner. Mapping each group onto the flow
above — a site is only reachable if the flow enters the code path that contains it:

| Census group                    | Sites | On the CI path?                                                        |
| ------------------------------- | ----- | ---------------------------------------------------------------------- |
| (b) source default, right       | 4     | **No** — all in the network/compose block or teardown                  |
| (c) source default, wrong       | 4     | **No** — `:822` is config restore; the rest are network detection      |
| (a) already correct             | 1     | **No** — inside `build_ssh_mounts_and_validate`                        |
| (e) network selection           | 4     | **No** — network detection                                             |
| (e) SSH key selection           | 2     | **No** — token-first skips it                                          |
| (e) engine/network recovery     | 2     | **No** — the cross-engine wizard                                       |
| (e) `create_token`              | 7     | **No** — CI selects a named token, never creates one                   |
| (e) token export                | 1     | **No** — `--export-token` is its own mode                              |
| (d) guided Dockerfile authoring | 1     | **No** — interactive authoring tool                                    |
| (e) token *resolution*          | 5     | **YES** — `:968 :992 :1013 :1104 :1121`                                |
| (e) `select_token` interactive  | 1     | **YES** — `token-management.bash:611`, when `--token` does not resolve |
| (d) migration                   | 1     | **Probably** — `:78` runs early, before any mode branch                |

**≈6 reachable sites, all of them credential resolution**, and all answered by one rule: an
unresolvable `--token` fails fast naming the flag. The guarded primitive in §4.2 is still the right
mechanism — it just has six callers, not forty-six.

> **SUPERSEDED — four rows and therefore the ≈6 figure**, by [DECISIONS.md](../DECISIONS.md) §6,
> the same reversal noted above. Rows **(b) 4**, **(c) 4**, **(e) network selection 4** and
> **(e) engine/network recovery 2** are marked "No" *because networking and compose were deferred*.
> §6 restores the kept half of that block to the CI path, so **13 of those sites must be re-judged**
> — some stay excluded because §6 drops them as negotiation, but that is a different reason and has
> to be established rather than inherited.
>
> The `≈6` was derived under the deferral and is superseded with it. **Plan 00113 Task 0.2 measures
> the real figure**; nothing may forward `≈6` as a fact in the meantime. Every other row is
> untouched by §6 and stands — the complete list, so that this enumeration is not itself a
> partial claim sitting next to a superseded set: `:822` (config restore, carved out of (c)),
> **(a) already correct**, **(e) SSH-key selection**, **(e) `create_token`**, **(e) token
> export**, **(d) guided Dockerfile authoring**, **(e) token resolution**, **(e) `select_token`
> interactive** and **(d) migration**. The last two of those are the rows that made up the `≈6`,
> so they survive the supersession — what does not survive is the total, and the claim that all
> of it was credential resolution.

> **This is a derivation, not a measurement.** It maps the census's own grouping onto the flow
> above; it does not re-walk each of the 46 sites in the source. Confirm before implementing, by
> instrumenting the CI path and asserting which `read` calls it can reach. Recording the
> distinction because this plan's recurring defect is exactly a derivation reported as a fact —
> and the note above is that defect landing anyway, by a route the caveat did not cover.

## What this does not settle

- ~~**E8** — whether an ungranted tool refuses or prompts.~~ **Dissolved 2026-08-10**
  ([DECISIONS.md §9](../DECISIONS.md)): E9 measured that `--disallowedTools` composes with
  `--dangerously-skip-permissions`, so CI never drops that flag and there is no prompt to hang
  on. Step 7's tool surface is therefore implementable, and is specified in
  [ci-tool-surface.md](ci-tool-surface.md). What survives is narrower: ccy auto-updates the CLI
  daily, so the flags must be confirmed present against the binary about to run — Plan 00113
  Task 0.3.
- **The entrypoint** — this flow assumes the desktop `entrypoint.sh` is reused unchanged, which
  its two `-n` guards make plausible but which is untested with no SSH key and no
  `GITHUB_USERNAME`.
- **Exit-code propagation** — step 8 requires the container's status to survive `set -e` (`:41`)
  and the absence of the compose block. Decision 6's numbering was designed under the retired
  "no existing `exit` is renumbered" constraint and should be re-read now that constraint is gone.
