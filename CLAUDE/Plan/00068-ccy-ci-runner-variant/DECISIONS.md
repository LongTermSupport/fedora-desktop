# Plan 00068 — Decisions, requirements and evidence

Supporting document for [PLAN.md](PLAN.md). It holds the durable material that used to live in
the plan body: the architecture the plan serves, the requirements CI places on `ccy`, the nine
technical decisions with their rationale, the unattended-launch defects, the open owner
decisions, the proof-obligation ledger and the risk table. The full prose as it stood before the
plan was slimmed is in [PLAN_archive.md](PLAN_archive.md); the dated narrative is in
[JOURNAL/](JOURNAL/).

Line references (`claude-yolo:2767`, `ssh-handling.bash:496`, …) are to the deployed ccy tree
as it stood when the plan was written. Treat them as pointers, not as current line numbers.

## 1. The architecture this serves

Owned by **lts-infra Plan 00030**:

```
GitHub fires a job for a repo
  → runner VM: is there a checkout of this repo on disk?
      no  → FAIL FAST: this repo is not set up for the runner
      yes → fetch, checkout the job's SHA
          → launch `ccy` IN THE REPO ROOT with the instruction for this event
          → return the result to GitHub
```

`ccy` is launched at job time, in the repo root, and behaves normally, including rebuilding the
project image when `.claude/ccy/Dockerfile` has changed. That is the product, not a hazard to
design around. This plan is only about what `ccy` must gain to be launchable unattended.

**Threat model**: private runner infrastructure for private, self-owned repositories. Not
multi-tenant CI running untrusted pull requests. Do not import constraints from that world
unless the owner asks.

## 2. The framing correction that governs everything

Owner, 2026-08-01: *"i suggested a long time ago that we engineer a CI flavour for ccy. ccy
functionality is up for grabs, we can change it, we can have a special ci mode that is special
and different to normal mode."*

The plan had conflated two constraints and enforced the wrong one:

- **"Do not build a parallel CI product beside ccy"** is binding. That is what Decision 1
  retracted, and the reason holds: the abandoned design removed the project's ability to supply
  its own `Dockerfile`, which is the whole seam.
- **"Do not change ccy's behaviour"** was never the instruction. It was invented in the plan and
  turned every CI need into a conditional bolted onto the desktop path (46 prompt guards being the
  clearest symptom).

**A CI mode is a different flow through the launcher, sharing its libraries, not the desktop flow
with guards on it.** The two constraints that are real:

1. **Reuse the seams, do not duplicate them**: image resolution and build, the project
   `Dockerfile`, the token store.
2. **Do not regress the desktop.** A safety property satisfied by testing the desktop path, not
   by refusing to add a second path.

### Working rule, adopted after six instances

Before specifying a mechanism, name the existing thing it replaces and state why that thing
cannot do the job. If that statement cannot be written truthfully, the mechanism is not needed.

### Already solved — do not rebuild

| Concern                     | Existing mechanism                                                                     |
| --------------------------- | -------------------------------------------------------------------------------------- |
| Per-project tooling         | `.claude/ccy/Dockerfile`, the seam `ccy` already has                                   |
| Image staleness             | `podman build` is the staleness check                                                  |
| Token persistence           | `~/.claude-tokens/ccy/tokens/`, `ccy --create-token`, `select_token`                   |
| Running the container       | `ccy` itself: `--headless --prompt`, plus `CLAUDE_ARGS` passthrough                    |
| Base-image version mismatch | `validate_container_version` (`common.bash:456`) at `claude-yolo:1436`, which rebuilds |
| Project image build         | `claude-yolo:1457-1528`, builds and caches on Dockerfile hash + base version           |

## 3. What CI needs from `ccy`

1. **A CI flow, not 46 guarded prompts.** `reports/ci-required-config.md` specified a fail-fast
   guard at each of 46 prompt sites. That count is an artefact of insisting CI walk the desktop's
   interactive discovery path. The census stays valuable as the inventory of what the desktop path
   does. `reports/ci-flow.md` derives that the flow reaches about 6 of the 46, all credential
   resolution; the guarded primitive is still the right mechanism, with six callers.

2. **Desktop assumptions a CI flow never makes**, each an explicit decision:

   - `--device /dev/dri` (`claude-yolo:2767`): measured, `exit 125`. Unconditionally fatal on a
     headless runner, unaffected by any egress decision.
   - The internet preflight (`:2518-2593`): `podman run --rm --network … alpine wget http://google.com`, then a bare `exit 1`. Needs an unpinned `alpine` pull and egress to
     `google.com`. Decision 8 defuses it (unfettered egress), so it drops from fatal to wasteful,
     but it is the tripwire that fires the moment anyone re-restricts egress.
   - GUI and SSH mounts need the same treatment; the fix shape exists as `GUI_MOUNTS` at
     `:2697-2721`.

3. **MCP injection**: required (Decision 7). Specified in `reports/mcp-and-egress.md`.

4. **A restricted tool surface for CI**: the agent must be unable to modify the checkout or
   push while still able to run the suite and read (Decision 9).

5. **Accept a pre-set `GH_TOKEN` instead of always deriving one.** Today `ccy` has exactly two
   ways to obtain it and neither takes one from the caller:

   | Path       | Source                                    |
   | ---------- | ----------------------------------------- |
   | SSH keys   | `gh-token-<alias>` host shell function    |
   | `--no-ssh` | `gh auth token` (`ssh-handling.bash:496`) |

   `build_ssh_mounts_and_validate` runs unconditionally (`claude-yolo:870`; only discovery is
   gated on `--no-ssh`), so a runner that minted a per-repo, one-hour, `contents: read` token
   cannot hand it over. `GH_TOKEN` is already exported at `:2751` and passed with `-e GH_TOKEN`;
   what is missing is honouring an already-set value. Blocks lts-infra Plan 00030 Task 2.11.

   **Owner's shape: a token-first mode, opt-in and CI-default.** The container already tolerates
   it: `entrypoint.sh:39` guards the identity cross-check on `[ -n "$GITHUB_USERNAME" ]` and `:59`
   guards SSH setup on `[ -n "$SSH_KEY_PATHS" ]`, both skip when empty. `GITHUB_USERNAME` can come
   from `gh api user --jq .login`, which ccy already calls at `ssh-handling.bash:475` as a
   cross-check; token-first promotes it from verification to source. The runner pre-seeds the
   checkout, so ccy never clones; a push would use HTTPS with the token. **No image change, no
   entrypoint change**: the whole blocker is launcher-side at `claude-yolo:870`. It is also the
   better desktop story for anyone without the `play-github-cli-multi.yml` setup, so it is a
   general capability that CI defaults to.

6. **Unattended-launch hygiene**: four cited defects (section 6) plus the concurrency question.

**Not required: `--egress`** (Decision 8).

## 4. Accepted, stated risk — the Claude OAuth token is readable inside every container

Raised by the owner, 2026-08-01. A global `ccy` property, not a CI one. The token is exported and
passed as an environment variable (`claude-yolo:2750`, `-e CLAUDE_CODE_OAUTH_TOKEN` at `:2771`),
so any process inside the container can read it: a `postinstall` script, a test helper, anything
reachable from `$PWD`. The desktop is exposed identically.

It is not fixable by hiding it: Claude Code must hold the credential. Passing by env rather than
argv (`:2745-2749`) closes the other-user hole via `/proc`, not the inside-the-container one.

The available control is blast radius, and ccy already has it: the token store is a pool of named,
dated tokens (`~/.claude-tokens/ccy/tokens/NAME.YYYY-MM-DD.token`, `--token`, `select_token`)
with expiry surfaced by `colorize_expiry`. A dedicated CI token means a compromise is revoked
without killing the human's desktop sessions. That is a provisioning decision, not a code change.
CI raises the likelihood (unattended, scheduled, unwatched) without changing the mechanism.

## 5. Technical decisions

### Decision 1 — Ansible provides the VM and its config; the project provides the image; CI fires `ccy`

Supersedes the earlier "ccy-owned CI base image built by Ansible" direction, which removed the
project's ability to add its own tooling.

### Decision 2 — the `LABEL` identity convention is retired

`podman build` is the staleness check. The convention only looked necessary while the image was
assumed to be built out of band.

### Decision 3 — `--non-interactive` is the CI enabler

Retracts the earlier classification of it as "desktop-only hardening", which followed from
assuming the launcher was not on the CI path. CI invokes `ccy`, so the launcher is the CI path
and its prompt sites are the blocker.

### Decision 4 — no permission surface on the desktop (reversed for CI by Decision 9)

`ccy` runs `claude --dangerously-skip-permissions` unconditionally (`claude-yolo:2792`). Its
posture is a trust model premised on the operator owning the workspace: trusted automation only.
That remains the desktop posture and is unchanged.

### Decision 5 — egress restriction is independent of CI

A runtime property, useful on the desktop too.

### Decision 6 — the fail-fast contract reuses ccy's shapes; new exit codes only on new branches

1. No new message format. `claude-yolo:728-740` (absent value) and `common.bash:503-516` (wrong
   value) become mandatory. Every message names a remediation actionable by exactly one owner.
2. Exit codes 64 (`EX_USAGE`) and 78 (`EX_CONFIG`) are emitted only from branches that exist only
   under `--non-interactive`. No existing `exit` is renumbered. The workflow maps 78 to an
   annotation itself, keeping GitHub-specific knowledge out of `ccy`.
3. Credential resolution is guarded, not removed. CI is a fedora-desktop VM, so ccy's token store
   is already present.

Note: item 2 was designed under the since-retired "keep the desktop byte-identical" constraint
and needs re-reading before implementation (see `reports/ci-flow.md`, "What this does not
settle").

### Decision 7 — MCP injection is REQUIRED

An earlier version retired it, arguing the repo's own committed `.mcp.json` is the existing
mechanism. Retracted on the owner's correction:

1. The estate already provisions for it. `runner.yml:210-211` names "the MCP GitHub server" as
   something the runtime allowlist exists to serve, and `:251-253` keeps `registry.npmjs.org`
   allowlisted specifically so that server can be fetched.
2. `.mcp.json` is the wrong seam for CI. It makes the capability something the repo-under-test
   grants itself, and the server needs a token ccy has no way to deliver: the `-e` list at
   `:2771-2783` is fixed and `CCY_EXTRA_MOUNTS` (`:1780`) has no env counterpart.

Interface, config location, `--strict-mcp-config` and the flag-existence assertion are in
`reports/mcp-and-egress.md`. The rule that matters most: the config must not be written under
`/root/.claude`, which `entrypoint.sh:183-195` symlinks into the checkout.

Still unverified: whether Claude Code prompts to approve a project-scoped MCP server. Since
Decision 9's mechanism composes with `--dangerously-skip-permissions`, CI keeps
`bypassPermissions` and any such prompt is bypassed; it no longer threatens a TTY-less job.

What does need asserting for MCP: that every `mcp__*` tool named in a class's list actually
exists in the server that will run. An unconfigured or misnamed MCP tool is silently inert.
Assert against the server's captured vocabulary, not a hand-kept list.

### Decision 8 — `--egress` is DROPPED; ccy gets unfettered egress at launch

Owner's decision, 2026-08-01. An earlier version argued `--egress` was redundant because the
runner's layers already owned egress. That was factually wrong and is retracted: squid binds
`127.0.0.1:3128` and the fence drops podman's subuid range (`60-runner-egress-fence.nft.j2:54`),
so a ccy container reaches neither the proxy nor the internet.

The owner's reason is cost/benefit:

- ccy needs unfettered access at launch structurally: the daily Claude Code auto-update fetches
  from npm (`claude-yolo:1254`, `:1343`); the preflight pulls `alpine` and requires `google.com`
  (`:2518-2593`); rebuilds need the wide build tier. An allowlist would have to track ccy's
  internals.
- What the allowlist buys is narrower than it looks. It stops commodity supply-chain malware
  phoning home to a random host; it does not stop a deliberate adversary, since `github.com` and
  `api.anthropic.com` must be allowlisted and either is a fine exfiltration channel.
- The operational cost is measured: 660 denied `.actions.githubusercontent.com` CONNECTs left
  runners registered-but-offline; 212 refusals across five Azure shards made every job's logs
  unretrievable (`runner.yml:218-248`).

The safety story moves to Decision 9. The runner's own egress posture becomes an open question
for lts-infra; nothing in fedora-desktop should assume either answer. C3 is retained in
`reports/mcp-and-egress.md` for whoever revisits it.

### Decision 9 — CI runs with a restricted tool surface

Reverses Decision 4 for CI and retires the "tool-level restriction stays OUT" position in
`reports/mcp-and-egress.md`. Intent: for CI, triage and review the agent is read-only with
respect to the repository. It may run the suite and read, but not modify the checkout, commit,
or push.

**Attribution** (corrected 2026-08-10): the intent is the owner's (governing steer: *"CI need
safety and MCP etc"*). The mechanism was this plan's derivation from it, and is revisable on
evidence. The earlier mechanism, "drop `--dangerously-skip-permissions`", was wrong.

**The mechanism: `--disallowedTools`, composed with `--dangerously-skip-permissions`.** Measured
2026-08-10 against the real CLI (see the journal of that day for the full method):

| flags                                                                  | `permissionMode`    | tools | `Bash`/`Edit`/`Write` |
| ---------------------------------------------------------------------- | ------------------- | ----- | --------------------- |
| `--dangerously-skip-permissions`                                       | `bypassPermissions` | 29    | present               |
| `--dangerously-skip-permissions` + `--disallowedTools Bash,Edit,Write` | `bypassPermissions` | 28    | **ABSENT**            |

`--allowedTools` governs permission (what proceeds without a prompt) and removes nothing; asking
for two tools yields 31, more than the default 29. `--disallowedTools` governs availability and
takes the tool out of the session entirely while skip-permissions is passed. That dissolves E8:
keeping `--dangerously-skip-permissions` means there is no prompt for a TTY-less job to hang on.
The flag-existence assertion in `reports/mcp-and-egress.md` stays load-bearing, because ccy
auto-updates the CLI daily.

**Sizing a restriction by counting tools gives a wrong answer.** 29 − 3 = 26, but the measured
count is 28: removing `Bash` adds `Glob` and `Grep`. Assert on the tool names absent from the
session, never on a count.

**"Read-only" cannot be literal for `push`/`pull_request`**, because Plan 00030 has the agent run
`./.claude/ccy/ci.bash`, which needs Bash. The coherent line: execute the suite and read the
tree; never write to it, commit, or push. For `issues`/`issue_comment`, where `ci.bash` does not
run, a genuinely narrower surface is available.

**Prefer an allowlist wherever the vocabulary is not ours.** A denylist over a large server-side
tool surface fails open on a typo, measured rather than theoretical: earlier downstream revisions
denied several tool names that do not exist while the real write primitive was not listed at all.

## 6. Task 3.5 — the CI flow, and compose/networking

Full flow in `reports/ci-flow.md`. Summary:

| Step        | Reuses                                                    | Notes                               |
| ----------- | --------------------------------------------------------- | ----------------------------------- |
| Image       | `build_container_with_hash`, `validate_container_version` | the product; unchanged              |
| Credentials | the token store; token-first (requirement 5)              | no SSH probe, no `gh-token-<alias>` |
| Run         | a fixed flag set                                          | no `/dev/dri`, no GUI, no preflight |
| Exit        | the container's status                                    | not the compose block's (defect 4)  |

**Compose and networking are CI requirements** (owner, 2026-08-01: *"i would not assume that CI
doesn't need compose or podman network stuff"*). A project whose `ci.bash` needs postgres or redis
needs the services up and the ccy container attached to their network. What CI drops is the
negotiation, not the capability, and `lib/network-management.bash` already splits on that line:

| Keep — mechanism, no prompts                                               | Drop — discovery and negotiation                  |
| -------------------------------------------------------------------------- | ------------------------------------------------- |
| `get_expected_network_name` `:10`, `has_compose_files` `:448`              | the project-name-matching heuristic               |
| `_compose_already_running` `:613`, `network_has_running_containers` `:427` | the cross-engine mismatch wizard (`:1973-2243`)   |
| `ensure_network_dns` `:703`, `connect_to_network` `:98`                    | the "select network [0-N]" menus (`:271`, `:274`) |
| `_do_compose_start` `:505`, one confirmation at `:546`                     | that confirmation; `offer_compose_start` `:667`   |

Teardown already tracks `CCY_COMPOSE_WAS_STARTED`, the right shape: tear down what CI started,
leave pre-existing services alone. Two things this forces into the design:

1. **The network must be known before `podman run`**, since `--network` is a create-time
   argument. Either ccy resolves and starts compose before launching, or it uses
   `connect_to_network` (`:98`) to attach the running container afterwards. Pick one deliberately.
2. **CI declares rather than discovers.** The natural home is the project's `.claude/ccy/`. Unlike
   egress rules this is not a security boundary: a project describing its own test dependencies is
   the same category as its `Dockerfile`.

Still not on this path: `save_launch_config` (`:2607`), the leftover-container `rm -f` (`:2741`),
and the `stty` handling (`:2726-2729`).

## 7. Task 3.3 — unattended-launch defects

1. **Container naming races, and the loser is killed.** `get_next_container_name`
   (`common.bash:653-686`) is check-then-act over `podman ps -a` with no lock, and `:2741` runs
   `container_cmd rm -f "$CONTAINER_NAME"` as leftover cleanup. Two jobs for one repo can both
   choose `<repo>_yolo`, and the second one's `rm -f` destroys the first job's running container.
   Fix: serialise the jobs on the runner (lts-infra Plan 00030 Task 2.8), which also fixes the
   worse problem of two jobs checking out different SHAs into one working tree
   (`runner_instances: 4` today). On ccy's side: accept a caller-supplied `--container-name` and
   never `rm -f` it.
2. **ccy dirties the job checkout before the agent starts.** `save_launch_config` (`:2607`, body
   at `:368-392`) writes `.claude/ccy/.last-launch.conf` with a timestamp into the tree the job is
   about to test. Fix: skip the write under non-interactive. Same class: `entrypoint.sh:183-195`
   symlinks `/root/.claude` into the checkout (open decision 3).
3. **Compose teardown prompts after the container exits** (`:2789` onwards, gated on
   `CCY_COMPOSE_WAS_STARTED`). Fix: under non-interactive, act on an announced default.
4. **ccy's exit status is not the container's.** `set -e` is on (`:41`) and the compose block
   follows `container_cmd run` (`:2764`), so a failing container aborts ccy before teardown and a
   passing one lets the compose block set the final status. CI survives this only because Plan
   00030 puts the verdict in `$CI_EXIT` rather than in ccy's exit code.

## 8. Open decisions — owner

1. ~~Does "more locked down" reopen Decision 4?~~ Closed 2026-08-01: yes; Decision 9.
2. **`ccy.env` sourcing** (`entrypoint.sh:269-274`) executes shell from the checkout. Acceptable
   under trusted-automation-only, or gated off in non-interactive mode? A CI agent that may not
   write the repo can still be handed arbitrary shell from the repo.
3. **The `/root/.claude` → `/workspace` symlink** (`:183-195`) puts session state in the job
   checkout. Same question.

## 9. Proof obligations

| ID    | Claim                                                | Status                                       |
| ----- | ---------------------------------------------------- | -------------------------------------------- |
| E1    | Task 1.1's host facts                                | ✅ settled — run `20260731-225344`           |
| E6    | `--device /dev/dri` fatal when the node is absent    | ✅ settled — `exit 125`, confirmed blocker   |
| C3    | `--network pasta:…` / `--network <name>` exclusivity | ✅ settled — first direct measurement        |
| B1–B4 | Spin-vs-abort behaviour of the launcher              | ⬜ interactive; needs real quota             |
| C1/C2 | pasta port-forwarding and loopback exposure          | ⬜ needs a host listener; borrowed, unproven |
| E7    | The internet preflight is fatal on the runner        | ⬜ moot under Decision 8                     |
| E8    | An ungranted tool REFUSES rather than prompting      | ✅ moot — dissolved 2026-08-10               |
| E9    | `--disallowedTools` composes with skip-permissions   | ✅ settled — measured 2026-08-10             |

**E8 is dissolved, not answered.** It asked what an ungranted tool does without
`--dangerously-skip-permissions`. E9 measured that `--disallowedTools` composes with that flag,
so CI never has to drop it and there is no prompt to hang on. Lesson: when a plan is gated on a
behaviour, measure the behaviour before designing around either branch.

**E7 is defused, not answered.** `triage.bash` could never settle it: the host has open egress and
a warm image cache. Logged against lts-infra Plan 00030, whose open question 1 asks the adjacent
question about build-time egress.

Verdicts from the host runs: `reports/host-run-verdicts.md`. Re-run the probes any time with
`./triage.bash` on the HOST.

## 10. Risks and mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                                                                               |
| --------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------------------------------------------------------------ |
| A mechanism is specified that already exists                    | H      | H           | Materialised 6×. Apply the working rule (section 2) before designing                                                     |
| A design defect survives because reviews audit self-consistency | H      | H           | Materialised 3×. Audit against the owner's steer                                                                         |
| A CI mode regresses the desktop path                            | H      | M           | Test the desktop path. Do not mitigate by refusing to add a second path                                                  |
| This plan invents a constraint the owner never set              | H      | H           | Materialised 5×, once wearing the owner's name (Decision 9). Before accepting a constraint, quote where the owner set it |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Section 7, defect 1: caller-supplied name, no `rm -f` on it, jobs serialised on the runner                               |
| A desktop assumption is fatal on the runner and nobody looked   | H      | M           | Materialised 2× (`/dev/dri`, the preflight). Audit every unconditional host assumption before implementing               |
