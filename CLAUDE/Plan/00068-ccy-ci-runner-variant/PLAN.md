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

   - `--device /dev/dri` (`claude-yolo:2767`) — measured, `exit 125`.
   - **the internet preflight** (`:2518-2593`) — `podman run --rm --network … alpine wget http://google.com`, and `exit 1` on failure. It needs an unpinned `alpine` pull *and*
     egress to `google.com`, neither of which a squid-allowlisted runner grants. Found while
     designing Phase 3; it is the same defect class as `/dev/dri` and just as fatal.

   GUI and SSH mounts need the same treatment. Fix shape already exists: `GUI_MOUNTS` at
   `:2697-2721`.

3. **Nothing else.** MCP injection and `--egress` were requirements until Phase 3 applied the
   working rule below to them; both are retired (Decisions 7 and 8). What remains is
   **unattended-launch hygiene** — four cited defects, specified in Task 3.3.

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
- Keep desktop `ccy` byte-identical when the new flags are absent.

## Non-Goals

- **No implementation.** No file outside this plan folder is modified.
- No separate CI runner, CI image, CI entrypoint, or CI credential path.
- No `LABEL` identity convention (Decision 2).
- No permission surface (Decision 4).

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

### Decision 4 — no permission surface

`ccy` runs `claude --dangerously-skip-permissions` unconditionally (`claude-yolo:2792`). Its
posture is a trust model premised on the operator owning the workspace. Price stated: **trusted
automation only.**
**Open**: the "CI should be more locked down" steer may reopen this for CI specifically.

### Decision 5 — egress restriction is independent of CI

A runtime property, useful on the desktop too.

### Decision 7 — MCP needs no ccy mechanism; the requirement is retired

`grep -rn -i mcp` over the whole deployed tree returns **nothing**. There is no mechanism to
extend — and none is needed. `$PWD` is mounted at `/workspace` (`:1771`) and is the working
directory (`:2784`), so a repo's committed `.mcp.json` is already in front of Claude Code.
**The project drives its MCP config exactly as it drives its `Dockerfile`.** The working rule
asks for a truthful statement of why the existing thing cannot do the job; it cannot be
written, so the requirement goes. Two residuals, neither load-bearing today:

- **No env passthrough for an MCP server's secret.** The `-e` list at `:2771-2783` is fixed;
  `CCY_EXTRA_MOUNTS` (`:1780`) covers mounts, and has no env counterpart. Build it when a repo
  actually needs one — that is a one-line array, not a design.
- **Unverified**: whether Claude Code prompts to approve a project-scoped MCP server despite
  `--dangerously-skip-permissions` and the pre-accepted trust dialog (`entrypoint.sh:257-262`).
  If it does, it is a **prompt site** and belongs in the `--non-interactive` census, not here.

### Decision 8 — `--egress` is not built for CI; the runner already owns egress

The existing thing is the runner VM's egress layers (squid allowlist + nftables, lts-infra).
They are **outside the job's control**; a launcher flag is **inside** it — anything that can
launch `ccy` can drop the flag. Adding a weaker control in the weaker place, duplicating one
that already works, is not a gain. Decision 5 said egress restriction is independent of CI;
the conclusion that follows is that it is **out of this plan's scope**.

C3 is not wasted: it is the binding constraint for whoever does build `--egress` later —
`--network pasta:…` and `--network <name>` are mutually exclusive, and ccy's `--network`
already means "join this named network" (`:534`, `:1794-1884`), defaulting to `--network podman` under podman (`:2508-2510`). The flag must hard-error, never silently drop one.

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
and the image built out of band. Both premises are gone, so the specs went with them. Redone
against the current architecture, **two of the three requirements do not survive contact with
the working rule** — and the third grew a blocker nobody had looked for.

- [x] ✅ **Task 3.1**: MCP injection — **retired, no ccy change** (Decision 7).

- [x] ✅ **Task 3.2**: `--egress` — **out of scope; the runner VM already owns egress**
  (Decision 8). C3 is retained as the constraint on any future build.

- [x] ✅ **Task 3.3**: Unattended-launch hygiene — four defects, each cited, each with a fix
  that is a guard rather than a new subsystem:

  1. **Container naming races, and the loser is killed.** `get_next_container_name`
     (`common.bash:653-686`) is check-then-act over `podman ps -a` with no lock, and `:2741`
     then runs `container_cmd rm -f "$CONTAINER_NAME"` as a leftover-cleanup safety net. Two
     jobs for one repo can both read `ps -a` before either container exists, both choose
     `<repo>_yolo`, and **the second one's `rm -f` destroys the first job's running
     container** — a green job and a mysteriously dead one. Fix: non-interactive mode
     **requires** a caller-supplied `--container-name` and never calls the `rm -f` net on it.
     A unique name cannot have leftovers; if it exists anyway that is a collision to fail on,
     not to bulldoze.
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

**The design is complete; the handoff is not, and deliberately so.** Task 4.1 is gated on the
three owner decisions below. Two of them change what gets built: if Decision 4 reopens, CI
grows a permission surface that does not exist today; if `ccy.env` sourcing is gated off, the
entrypoint changes. Writing an implementation plan before those are answered means writing one
that is wrong in a way that will not be visible until it is half built.

- [ ] ⬜ **Task 4.1**: Create the implementation plan. This plan specifies; it does not build.

## Open decisions — owner

1. **Does "more locked down" reopen Decision 4** for CI specifically?
2. **`ccy.env` sourcing** (`entrypoint.sh:269-274`) executes shell from the checkout.
   Acceptable under trusted-automation-only, or gated off in non-interactive mode?
3. **The `/root/.claude` → `/workspace` symlink** (`:185-195`) puts session state in the job
   checkout. Same question.

## Proof obligations

| ID    | Claim                                                | Status                                       |
| ----- | ---------------------------------------------------- | -------------------------------------------- |
| E1    | Task 1.1's host facts                                | ✅ settled — run `20260731-225344`           |
| E6    | `--device /dev/dri` fatal when the node is absent    | ✅ settled — `exit 125`, confirmed blocker   |
| C3    | `--network pasta:…` / `--network <name>` exclusivity | ✅ settled — first direct measurement        |
| B1–B4 | Spin-vs-abort behaviour of the launcher              | ⬜ interactive; needs real quota             |
| C1/C2 | pasta port-forwarding and loopback exposure          | ⬜ needs a host listener; borrowed, unproven |
| E7    | The internet preflight is fatal on the runner        | ⬜ **read, not measured — see below**        |

**E7 cannot be settled by `triage.bash`.** The host has unrestricted egress and a warm image
cache, so the preflight passes there and that result says nothing about the runner. What is
established is only what the code says: `:2523` needs an `alpine` pull and a 200 from
`http://google.com`, and `:2591` is a bare `exit 1`. Whether the runner's squid allowlist
denies either is a **fact about the runner**, and it has to be measured on the runner. Logged
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
  current architecture — two retired with a stated reason, one specified defect by defect.

## Risks & Mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                                                                     |
| --------------------------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------- |
| A mechanism is specified that already exists                    | H      | H           | **Materialised 6×.** Apply the working rule above before designing                                             |
| A design defect survives because reviews audit self-consistency | H      | H           | **Materialised 3×.** Audit against the owner's steer                                                           |
| Non-interactive mode changes desktop behaviour                  | H      | M           | Flags absent ⇒ byte-identical                                                                                  |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Task 3.3.1 — caller-supplied name, and no `rm -f` on it                                                        |
| A desktop assumption is fatal on the runner and nobody looked   | H      | M           | **Materialised 2×** (`/dev/dri`, the preflight). Audit every unconditional host assumption before implementing |
