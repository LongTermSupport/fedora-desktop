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

Networking and compose are **deferred entirely** (lts-infra Plan 00030: the case study does not
need them), so steps 5–7 pass no `--network` and the flow never enters `:1789-2498` or the
preflight at `:2518-2593`.

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
- **no `--dangerously-skip-permissions`** — Decision 9's restricted tool surface, **gated on E8**
- **`--mcp-config <container-local path>`** — Decision 7; never under `/root/.claude`, which
  `entrypoint.sh:183-195` symlinks into the checkout

## Requirement 1, re-derived: ~6 sites, not 46

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

> **This is a derivation, not a measurement.** It maps the census's own grouping onto the flow
> above; it does not re-walk each of the 46 sites in the source. Confirm before implementing, by
> instrumenting the CI path and asserting which `read` calls it can reach. Recording the
> distinction because this plan's recurring defect is exactly a derivation reported as a fact.

## What this does not settle

- **E8** — whether an ungranted tool refuses or prompts. Step 7's tool surface is unimplementable
  until that is measured against the real CLI, and ccy auto-updates that CLI daily.
- **The entrypoint** — this flow assumes the desktop `entrypoint.sh` is reused unchanged, which
  its two `-n` guards make plausible but which is untested with no SSH key and no
  `GITHUB_USERNAME`.
- **Exit-code propagation** — step 8 requires the container's status to survive `set -e` (`:41`)
  and the absence of the compose block. Decision 6's numbering was designed under the retired
  "no existing `exit` is renumbered" constraint and should be re-read now that constraint is gone.
