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
2. **Conditional desktop assumptions.** `--device /dev/dri` is unconditional
   (`claude-yolo:2773`) and **fatal on a headless runner** — measured, `exit 125`. GUI and SSH
   mounts need the same treatment. Fix shape already exists: `GUI_MOUNTS` at `:2703-2727`.
3. **MCP injection** — required, **not yet specified** against this architecture.
4. **Egress restriction** — an `--egress` allowlist. The `--network` naming collision is real
   and C3 is measured (`reports/host-run-verdicts.md` §2): `--network pasta:…` and
   `--network <name>` are mutually exclusive, so the flag must hard-error rather than silently
   lose one. **Not yet specified** against this architecture.
5. **Unattended-launch capabilities** — concurrency-safe container naming (two jobs for one
   repo must not kill each other's containers), the egress preflight, compose teardown on
   failure, and **CI must not write `.claude/ccy/` into the checkout** — that path is read
   *and executed* (`entrypoint.sh:269-274`). **Not yet specified** against this architecture.

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

The earlier specifications for these were written when the launcher was believed to be off the
CI path and the image built out of band. Both premises are gone, so the specs went with them.
The **requirements** are listed under "What CI needs from `ccy`" above; what is missing is a
current design for each.

- [ ] ⬜ **Task 3.1**: MCP injection — interface and where config is written (**not** the
  symlinked location).
- [ ] ⬜ **Task 3.2**: `--egress` — mechanism, and the hard error on the `--network` collision
  that C3 now grounds.
- [ ] ⬜ **Task 3.3**: Unattended-launch capabilities (item 5 above).

### Phase 4 — Hand off to implementation

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
- [ ] ⬜ MCP, `--egress` and the unattended-launch capabilities are specified against the
  current architecture.

## Risks & Mitigations

| Risk                                                            | Impact | Probability | Mitigation                                                         |
| --------------------------------------------------------------- | ------ | ----------- | ------------------------------------------------------------------ |
| A mechanism is specified that already exists                    | H      | H           | **Materialised 6×.** Apply the working rule above before designing |
| A design defect survives because reviews audit self-consistency | H      | H           | **Materialised 3×.** Audit against the owner's steer               |
| Non-interactive mode changes desktop behaviour                  | H      | M           | Flags absent ⇒ byte-identical                                      |
| Concurrent jobs for one repo kill each other's containers       | H      | M           | Run-ID-salted names; never `get_next_container_name`               |
