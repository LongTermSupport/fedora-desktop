# Plan 00068: Make ccy fully non-interactive so CI can invoke it

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

> **This folder was truncated on 2026-07-31 at commit `0dde4f0`.** Twenty-two reports and one
> probe were deleted, not archived — they specified mechanisms that were retracted, or rested
> on premises since disproved. Git has them; nothing here should be reconstructed from them.
> `JOURNAL/` is kept in full: it is append-only by rule and is self-evidently a log of what
> happened, including the wrong turns. The danger being removed is a *report* that reads like
> a current specification when it is not.

## The architecture this serves

Owned by **lts-infra Plan 00030**. Stated by the owner, and it is short:

```
GitHub fires a job for a repo
  → runner VM: is there a checkout of this repo on disk?
      no  → FAIL FAST: this repo is not set up for the runner
      yes → fetch, checkout the job's SHA
          → launch `ccy` IN THE REPO ROOT with the instruction for this event
          → return the result to GitHub
```

**`ccy` is launched at job time, in the repo root, and behaves normally** — including
rebuilding the project image when `.claude/ccy/Dockerfile` has changed. That is the product,
not a hazard to design around.

This plan is only about **what `ccy` must gain to be launchable unattended**.

## What CI needs from `ccy`

1. **A fully non-interactive mode.** Every prompt site either takes an announced default or
   **fails fast and loud**, naming the flag that answers it. No spinning, no silent defaults.
   Specified site by site in `reports/ci-required-config.md`. One guarded primitive covers all
   46 sites — a guard that `exit`s cannot spin whatever the caller's errexit state.

2. **Conditional desktop assumptions.** Two of them, both unconditional and both fatal on a
   headless, egress-restricted runner:

   - `--device /dev/dri` (`claude-yolo:2767`) — measured, `exit 125`. **Unconditionally fatal**
     on a headless runner; unaffected by any egress decision.
   - **the internet preflight** (`:2518-2593`) — `podman run --rm --network … alpine wget http://google.com`, then a bare `exit 1`. It needs an unpinned `alpine` pull *and* egress
     to `google.com`. **Decision 8 defuses this**: with unfettered egress both succeed, so it
     drops from fatal to merely wasteful (an image pull and a round trip per launch). It is
     recorded because it is the tripwire that fires the moment anyone re-restricts egress, and
     because a bare `exit 1` on a connectivity check is the wrong shape regardless.

   GUI and SSH mounts need the same treatment. Fix shape already exists: `GUI_MOUNTS` at
   `:2697-2721`.

3. **MCP injection** — required. The runner's allowlist already provisions for a GitHub MCP
   server (`runner.yml:210-211`, `:251-253`); ccy has no MCP code at all. Specified in
   `reports/mcp-and-egress.md`.

4. **A restricted tool surface for CI** — the agent must be **unable to modify the checkout or
   push**, while still being able to run the suite and read. This reverses Decision 4 on the
   owner's instruction; see Decision 9, which is where the sharp edge is.

5. **Accept a pre-set `GH_TOKEN` instead of always deriving one.** Today `ccy` has exactly two
   ways to obtain it, and **neither takes one from the caller**:

   | Path       | Source                                    |
   | ---------- | ----------------------------------------- |
   | SSH keys   | `gh-token-<alias>` host shell function    |
   | `--no-ssh` | `gh auth token` (`ssh-handling.bash:496`) |

   `build_ssh_mounts_and_validate` runs **unconditionally** (`claude-yolo:870`; only *discovery*
   is gated on `--no-ssh`), so a runner that has minted a per-repo, one-hour, `contents: read`
   token has no way to hand it over. The plumbing already exists — `GH_TOKEN` is exported at
   `:2751` and passed with `-e GH_TOKEN`; what is missing is honouring a value that is already
   set. Blocks lts-infra Plan 00030 Task 2.11, which is the end state for per-project credentials.

   **This does not remove the SSH key.** ccy is SSH-first by design: the key is how it learns
   *which account it is* (`ssh-handling.bash:304`, `:341`, failing at `:382-394` if the key
   authenticates to nothing), and key 0 defines the container identity. A CI mode that supplies
   a token still has to say what happens to that identity probe.

6. **Unattended-launch hygiene** — four cited defects, in Task 3.3, plus the concurrency
   question the owner raised (Task 3.3.1).

**Not required: `--egress`** — dropped on the owner's decision (Decision 8).

## Already solved — do not rebuild

Recorded because this plan specified replacements for all of these before checking:

| Concern                     | Existing mechanism                                                                    |
| --------------------------- | ------------------------------------------------------------------------------------- |
| Per-project tooling         | `.claude/ccy/Dockerfile` — the seam `ccy` already has                                 |
| Image staleness             | `podman build` — it *is* the staleness check                                          |
| Token persistence           | `~/.claude-tokens/ccy/tokens/`, `ccy --create-token`, `select_token`                  |
| Running the container       | `ccy` itself — `--headless --prompt`, plus `CLAUDE_ARGS` passthrough                  |
| Base-image version mismatch | `validate_container_version` (`common.bash:456`) at `claude-yolo:1436` — **rebuilds** |
| Project image build         | `claude-yolo:1457-1528` — builds and caches on Dockerfile hash + base version         |

**Working rule, adopted after six instances**: before specifying a mechanism, name the existing
thing it replaces and state why that thing cannot do the job. If that statement cannot be
written truthfully, the mechanism is not needed.

## Standing principle — the design IS ccy. Do not build beside it.

Project Dockerfile customisation, the rebuild when it changes, the daily update, the token
flow — these are the product. The only permitted changes are the two above: make the prompts
non-interactive, and make the desktop-only assumptions conditional.

**Threat model: private runner infrastructure for private, self-owned repositories.** Not
multi-tenant CI running untrusted pull requests. Do not import constraints from that world
unless the owner asks.

## Goals

- Specify a fully non-interactive mode that fails fast and loud.
- Make every unconditional desktop assumption conditional — the two known fatal ones, and any
  found by the audit the risk table now demands.
- Give CI an MCP interface (Decision 7) and a restricted tool surface (Decision 9).
- Keep desktop `ccy` byte-identical when the new flags are absent.

## Non-Goals

- **No implementation.** No file outside this plan folder is modified.
- No separate CI runner, CI image, CI entrypoint, or CI credential path.
- No `LABEL` identity convention (Decision 2).
- **No `--egress`** (Decision 8) — and no assumption, either way, about what the runner's own
  egress posture becomes as a result. That is lts-infra's to decide.
- No permission surface **on the desktop** — Decision 4 stands there, and is reversed only for
  CI by Decision 9.

## Technical Decisions

### Decision 1 — Ansible provides the VM and its config; the project provides the image; CI fires `ccy`

Supersedes the earlier "ccy-owned CI base image built by Ansible" direction, which removed the
project's ability to add its own tooling.

### Decision 2 — the `LABEL` identity convention is retired

`podman build` is the staleness check. The convention only looked necessary while the image was
assumed to be built out of band.

### Decision 3 — `--non-interactive` is the CI enabler

Retracts the earlier classification of it as "desktop-only hardening", which followed from
assuming the launcher was not on the CI path. **CI invokes `ccy`, so the launcher IS the CI
path** and its prompt sites are the blocker.

### Decision 4 — no permission surface — **REVERSED for CI by Decision 9**

`ccy` runs `claude --dangerously-skip-permissions` unconditionally (`claude-yolo:2792`). Its
posture is a trust model premised on the operator owning the workspace. Price stated: **trusted
automation only.**

That remains the **desktop** posture and is unchanged. The "CI should be more locked down" steer
did reopen it, and the owner closed it: see Decision 9.

### Decision 5 — egress restriction is independent of CI

A runtime property, useful on the desktop too.

### Decision 7 — MCP injection is REQUIRED

An earlier version of this decision retired it, arguing the repo's own committed `.mcp.json` is
the existing mechanism. **Retracted on the owner's correction**, for two reasons:

1. **The estate already provisions for it.** `runner.yml:210-211` names *"the MCP GitHub
   server"* as something the runtime allowlist exists to serve, and `:251-253` keeps
   `registry.npmjs.org` allowlisted specifically so that server can be fetched — with a note to
   drop it once the server is pre-bundled into the ccy baseline image.
2. **`.mcp.json` is the wrong seam for CI.** It makes the capability something the
   repo-under-test grants *itself*, and the server needs a token ccy has no way to deliver —
   the `-e` list at `:2771-2783` is fixed and `CCY_EXTRA_MOUNTS` (`:1780`) has no env
   counterpart. That gap was found and then filed as a non-load-bearing residual; it is the
   crux.

Interface, config location, `--strict-mcp-config`, and the flag-existence assertion:
`reports/mcp-and-egress.md`. The rule that matters most: **the config must not be written under
`/root/.claude`**, which `entrypoint.sh:183-195` symlinks into the checkout.

**Still unverified**: whether Claude Code prompts to approve a project-scoped MCP server. Under
Decision 9 this stops being hypothetical, because CI no longer passes
`--dangerously-skip-permissions`.

### Decision 8 — `--egress` is DROPPED; ccy gets unfettered egress at launch

**Owner's decision, 2026-08-01**, and not for the reason this plan reached on its own.

An earlier version argued `--egress` was redundant because the runner's layers already owned
egress. **That was factually wrong** and is retracted: squid binds `127.0.0.1:3128` and the
fence drops podman's subuid range (`60-runner-egress-fence.nft.j2:54`), so a ccy container
reaches neither the proxy nor the internet. `--egress` would have been the container's *only*
route in, not a duplicate of it.

The owner's reason is cost/benefit, and it holds:

- **ccy needs unfettered access at launch — structurally, not incidentally.** The daily Claude
  Code auto-update fetches from npm (`claude-yolo:1254`, `:1343`); the preflight pulls `alpine`
  from Docker Hub and requires `google.com` (`:2518-2593`); rebuilds need the wide build tier.
  Squeezing that through a CONNECT allowlist means an allowlist that tracks ccy's internals.
- **What the allowlist buys is narrower than it looks.** It genuinely stops commodity
  supply-chain malware phoning home to a random host — real, since npm/pip/go packages execute
  at install time. It does **not** stop a deliberate adversary: `github.com` and
  `api.anthropic.com` must be allowlisted, and either is a fine exfiltration channel. It
  protects against the opportunistic case only.
- **The operational cost is measured.** 660 denied `.actions.githubusercontent.com` CONNECTs
  left runners registered-but-offline; 212 refusals across five Azure shards made every job's
  logs unretrievable (`runner.yml:218-248`).

**The safety story moves to Decision 9** — egress control traded for tool control, which is the
tighter lever here: an agent that cannot write or push cannot turn a compromised dependency into
a repo change, whatever it can reach.

**Consequence for lts-infra**: the runner's egress posture becomes an open question *there*.
Not this plan's to close, and nothing in fedora-desktop should assume either answer. C3 is
retained in `reports/mcp-and-egress.md` for whoever revisits it.

### Decision 9 — CI runs with a restricted tool surface, not `--dangerously-skip-permissions`

**Owner's decision, 2026-08-01. Reverses Decision 4 for CI** and retires the "tool-level
restriction stays OUT" position in `reports/mcp-and-egress.md`, which was reasoned *from*
Decision 4.

Intent: for CI, triage and review the agent is **read-only with respect to the repository** — it
may run the suite and read, but not modify the checkout, commit, or push.

**The sharp edge, to be settled before this is built.** `--dangerously-skip-permissions`
(`claude-yolo:2792`) *bypasses* an allowlist rather than composing with it, so CI must not pass
it. The question is then what an ungranted tool does:

- if it is **refused**, the design works;
- if it **prompts**, a TTY-less job **hangs** — the exact non-interactive failure this plan
  exists to prevent, reintroduced by the safety feature.

That is Claude CLI behaviour, it is **unverified**, and ccy auto-updates the CLI daily so it can
change underneath us. The flag-existence-and-behaviour assertion in `reports/mcp-and-egress.md`
is therefore load-bearing, not defensive.

**"Read-only" cannot be literal for `push`/`pull_request`**, because Plan 00030 has the agent
run `./.claude/ccy/ci.bash`, which needs Bash. The coherent line: *execute the suite and read
the tree; never write to it, commit, or push.* For `issues`/`issue_comment`, where `ci.bash`
does not run, a genuinely narrower surface is available.

### Decision 6 — the fail-fast contract reuses ccy's shapes; new exit codes only on new branches

1. **No new message format.** `claude-yolo:728-740` (absent value) and `common.bash:503-516`
   (wrong value) become mandatory. Every message names a remediation actionable by exactly one
   owner.
2. **Exit codes 64 (`EX_USAGE`) and 78 (`EX_CONFIG`) are emitted only from branches that exist
   only under `--non-interactive`.** No existing `exit` is renumbered, so desktop stays
   byte-identical. The workflow maps 78 to an annotation itself, keeping GitHub-specific
   knowledge out of `ccy`.
3. **Credential resolution is guarded, not removed.** CI is a fedora-desktop VM, so ccy's token
   store is already present.

## Tasks

### Phase 1 — Ground the unverified claims

- [x] ✅ **Task 1.1**: Host facts collected and verdicts recorded → `reports/host-run-verdicts.md`.
  Two runs; `20260731-225344` is authoritative (`all legs OK`). **E6 confirmed a blocker**;
  **C3 confirmed by first direct measurement**.
- [x] ✅ **Task 1.2**: Enumerate all prompt sites — 46, carried into `ci-required-config.md`.
- [x] ✅ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and how the image is built.

### Phase 2 — `--non-interactive`

- [x] ✅ **Task 2.1**: The fail-fast contract, site by site → `reports/ci-required-config.md`.
  46 sites classified; adds no new message format. Of 15 preconditions originally specified,
  **4 survive** — 7 are already asserted by `ccy`, 4 are moot, 1 is YAGNI.

### Phase 3 — Re-specify the remaining scope against the current architecture

The earlier specifications were written when the launcher was believed to be off the CI path
and the image built out of band. Both premises are gone. A first pass then retired MCP and
`--egress` on the strength of this plan's working rule; **the owner reversed both**, correctly —
the working rule was applied without also auditing against the owner's steer, which is this
plan's most-repeated failure. The scope below is the owner's, settled 2026-08-01.

- [x] ✅ **Task 3.1**: MCP injection — **required** (Decision 7). Design restored to
  `reports/mcp-and-egress.md` with its three dead premises marked; PLAN.md is authoritative
  where they differ. Outstanding sub-question: the missing env passthrough for the MCP server's
  token, which is what makes `.mcp.json` insufficient rather than merely awkward.

- [x] ✅ **Task 3.2**: `--egress` — **dropped by the owner** (Decision 8), and the earlier
  "the runner already owns egress" reasoning retracted as factually wrong. ccy gets unfettered
  egress at launch. C3 retained for any future revisit.

- [ ] ⬜ **Task 3.4**: The CI tool surface (Decision 9). Two things, and the first gates the
  second:

  1. **Verify what an ungranted tool does** without `--dangerously-skip-permissions` — refuse,
     or prompt. If it prompts, a TTY-less job hangs and the whole approach needs a different
     mechanism. This is a behaviour test against the real CLI, not a documentation read.
  2. Specify the per-event surfaces: `push`/`pull_request` (may run `ci.bash` and read; no
     write, commit or push) and `issues`/`issue_comment` (narrower — no `ci.bash`).

- [x] ✅ **Task 3.3**: Unattended-launch hygiene — four defects, each cited, each with a fix
  that is a guard rather than a new subsystem:

  1. **Container naming races, and the loser is killed.** `get_next_container_name`
     (`common.bash:653-686`) is check-then-act over `podman ps -a` with no lock, and `:2741`
     then runs `container_cmd rm -f "$CONTAINER_NAME"` as a leftover-cleanup safety net. Two
     jobs for one repo can both read `ps -a` before either container exists, both choose
     `<repo>_yolo`, and **the second one's `rm -f` destroys the first job's running
     container** — a green job and a mysteriously dead one.

     **The owner's fix is better than mine, and fixes more**: serialise the jobs. `ccy`'s side
     still takes a caller-supplied `--container-name` and never `rm -f`s it (cheap, and it makes
     the race unreachable rather than merely unlikely), but the primary control belongs on the
     runner — see lts-infra Plan 00030 Task 2.8. The reason it fixes more: Plan 00030 gives each
     repo **one** checkout, so two concurrent jobs for that repo `git checkout` different SHAs
     **in the same working tree**. That is two jobs corrupting each other's source, and no
     container-naming fix touches it. `runner_instances: 4` today, so this is live, not
     theoretical.

  2. **ccy dirties the job checkout before the agent starts.** `save_launch_config` (`:2607`,
     body at `:368-392`) writes `.claude/ccy/.last-launch.conf` — with a timestamp, so it
     differs every run — into the **working tree the job is about to test**. Fix: skip the
     write under non-interactive. Same class: `entrypoint.sh:183-195` symlinks
     `/root/.claude` → `/workspace/.claude/ccy`, putting session state in the checkout too;
     that one is open question 3, because it is load-bearing for session persistence.

  3. **Compose teardown prompts after the container exits** (`:2789` onwards, gated on
     `CCY_COMPOSE_WAS_STARTED`). A prompt after the work is done still hangs the job. Fix:
     under non-interactive, act on an announced default; never prompt.

  4. **ccy's exit status is not the container's.** `set -e` is on (`:41`) and the compose
     block follows `container_cmd run` (`:2764`), so a failing container aborts ccy before
     teardown and a passing one lets the compose block set the final status. CI survives this
     **only because** Plan 00030 put the verdict in `$CI_EXIT` rather than in ccy's exit code.
     Recorded as a reason that decision is load-bearing, not as luck.

### Phase 4 — Hand off to implementation

**Gated on Task 3.4's measurement, not on a decision.** The owner has now settled the scope
(Decisions 7, 8, 9). What is not settled is whether an ungranted tool refuses or prompts — and
that single fact decides whether Decision 9's approach works at all or needs replacing. An
implementation plan written before it is answered would be wrong in a way that stays invisible
until it is half built.

- [ ] ⬜ **Task 4.1**: Create the implementation plan. This plan specifies; it does not build.

## Open decisions — owner

1. ~~Does "more locked down" reopen Decision 4?~~ **Closed 2026-08-01 — yes; Decision 9.**
2. **`ccy.env` sourcing** (`entrypoint.sh:269-274`) executes shell from the checkout.
   Acceptable under trusted-automation-only, or gated off in non-interactive mode? Decision 9
   sharpens this: a CI agent that may not write the repo can still be handed arbitrary shell
   *from* the repo, which is a wider hole than the one being closed.
3. **The `/root/.claude` → `/workspace` symlink** (`:183-195`) puts session state in the job
   checkout. Same question.

## Proof obligations

| ID    | Claim                                                | Status                                       |
| ----- | ---------------------------------------------------- | -------------------------------------------- |
| E1    | Task 1.1's host facts                                | ✅ settled — run `20260731-225344`           |
| E6    | `--device /dev/dri` fatal when the node is absent    | ✅ settled — `exit 125`, confirmed blocker   |
| C3    | `--network pasta:…` / `--network <name>` exclusivity | ✅ settled — first direct measurement        |
| B1–B4 | Spin-vs-abort behaviour of the launcher              | ⬜ interactive; needs real quota             |
| C1/C2 | pasta port-forwarding and loopback exposure          | ⬜ needs a host listener; borrowed, unproven |
| E7    | The internet preflight is fatal on the runner        | ⬜ moot under Decision 8 — see below         |
| E8    | An ungranted tool REFUSES rather than prompting      | ⬜ **gates Decision 9 and Task 4.1**         |

**E8 is now the load-bearing unknown.** If an ungranted tool prompts instead of refusing, a
TTY-less CI job hangs and Decision 9 needs a different mechanism entirely. It is measurable
cheaply and directly — run the real CLI without `--dangerously-skip-permissions`, with a
deliberately ungranted tool, and observe. Do not settle it from documentation; ccy auto-updates
the CLI daily, so the answer must come from the binary that will actually run.

**E7 is defused, not answered.** Decision 8 gives ccy unfettered egress, so the preflight's
`alpine` pull and `google.com` fetch both succeed and it stops being fatal. It stays on this
list because it is the tripwire that fires the moment anyone re-restricts egress. Note also that
`triage.bash` could never have settled it: the host has open egress and a warm image cache, so
the preflight passes there regardless of what the runner would do. Logged
against lts-infra Plan 00030, whose open question 1 already asks the adjacent question about
build-time egress.

Re-run the probes any time: `./triage.bash` on the HOST.

## Dependencies

- **Blocks**: the ccy CI implementation plan, not yet created.
- **Consumed by**: lts-infra Plan 00030, which owns the runner-side dispatch.

## Success Criteria

- [x] ✅ Every claim is cited to a `file:line` or explicitly marked unverified.
- [x] ✅ A reader can say what happens to an existing `.claude/ccy/Dockerfile`: **nothing**.
- [x] ✅ Desktop ccy is provably unaffected when the new flags are absent.
- [x] ✅ The fully non-interactive contract is specified site by site.
- [x] ✅ Task 1.1's host facts are answered by a run, not by inference.
- [x] ✅ Every surviving report describes a live mechanism.
- [x] ✅ MCP, `--egress` and the unattended-launch capabilities are resolved against the
  current architecture, and by the owner rather than by this plan's internal reasoning.
- [ ] ⬜ E8 is measured: an ungranted tool refuses rather than prompting (Task 3.4).
- [ ] ⬜ The CI tool surface is specified per event.

## Risks & Mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                                                                     |
| --------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------- |
| A mechanism is specified that already exists                    | H      | H           | **Materialised 6×.** Apply the working rule above before designing                                             |
| A design defect survives because reviews audit self-consistency | H      | H           | **Materialised 3×.** Audit against the owner's steer                                                           |
| Non-interactive mode changes desktop behaviour                  | H      | M           | Flags absent ⇒ byte-identical                                                                                  |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Task 3.3.1 — caller-supplied name, and no `rm -f` on it                                                        |
| A desktop assumption is fatal on the runner and nobody looked   | H      | M           | **Materialised 2×** (`/dev/dri`, the preflight). Audit every unconditional host assumption before implementing |
