# Plan 00068 — Phases 4 and 5: MCP injection and egress restriction

Designed together because they share one constraint — both are **runtime** properties
(restatement §3), so neither is reachable from the mechanism the owner's steer endorses, and both
must therefore be launcher/entrypoint surface or nothing.

Where the consumer has **measured** something, this document reuses the measurement and cites it
rather than re-deriving it. That was Task 5.2's explicit instruction and it applies equally to
Phase 4.

---

# Phase 4 — MCP injection

## Task 4.1 — the interface, and where the config is written

**E4 re-confirmed for this document**: case-insensitive search for `mcp` across `claude-yolo`, all
seven `lib/*.bash`, `entrypoint.sh` and all four Dockerfiles returns **zero** matches. Everything
below is net-new.

### The interface — both routes, because the owner asked for both

| Route           | Surface                                       | Serves                                  |
| --------------- | --------------------------------------------- | --------------------------------------- |
| **Ad-hoc**      | `--mcp <name>` on the launcher                | a desktop one-off; Task 4.3             |
| **Declarative** | an `MCP` declaration in `.claude/ccy/ccy.env` | a project that always wants it, tracked |
| **Full**        | the server binary baked into a project image  | the owner's "full blown customisation"  |

These are not three mechanisms — the first two select *which servers to wire*, the third supplies
*the binary to wire*. A project can bake a binary and never wire it (useless but harmless), or
declare a server whose binary is absent (which must fail loudly — see the assertion below).

`ccy.env` is the right home for the declarative route because it already exists as the tracked
per-project config seam (`entrypoint.sh:269-274`) and is sourced in-container immediately before
the exec. No new file, no new precedence rule.

### Where the config is written — and it must NOT be the symlinked location

`entrypoint.sh:195` symlinks `/root/.claude` → `/workspace/.claude/ccy`, so **anything written
under `/root/.claude` lands in the checkout.** Writing the MCP config there would:

- mutate the job's checkout (restatement §5), and
- make the config an *input* the checkout controls, which is E10 row 4 in a second costume.

**Specification: write the MCP config to a container-local path outside `/root/.claude`** — a
tmpfs or `/run` path — and pass it explicitly with `--mcp-config <path>`. It is regenerated per
launch from the declarations; it is not state.

### `--strict-mcp-config`, and the decision it forces

The consumer's `entrypoint.sh:213-214` records what the flag does:

> `--strict-mcp-config` : use ONLY the MCP servers in `--mcp-config`. Any `.mcp.json` in the
> checkout is ignored.

So **without it, a `.mcp.json` in the checkout adds MCP servers to the session.** For ccy's trust
model (E10: the operator owns the workspace) honouring the checkout's `.mcp.json` is defensible
and arguably the desktop-correct behaviour. But it is another instance of "the checked-out tree
decides what runs", and it must be *stated* rather than inherited by accident.

**Specification:** ccy passes `--strict-mcp-config` **whenever it passes `--mcp-config`**. Rationale:
the moment ccy takes responsibility for wiring servers, silently merging an unreviewed second
source makes ccy's own declaration untrustworthy — a project reading `ccy.env` could not tell
what its session actually has. A project that wants `.mcp.json` honoured simply does not use the
ccy interface; that is a coherent, explicit choice rather than a silent merge.

### The flag-existence assertion — copied deliberately from the consumer

The consumer verifies the CLI actually supports each flag before relying on it
(`entrypoint.sh:133-137`, via a `require_flag` helper), and states why at `:114-115`:

> "A CLI that silently lacks `--permission-mode` or `--strict-mcp-config` would run with a weaker
> boundary than the design states"

**ccy needs this more than the consumer does**, and this is the argument for adopting it here:
ccy *auto-updates Claude Code daily, in place* (E8, `claude-yolo:1254`/`:1343`). The CLI under ccy
is a moving target by design. A `--mcp-config` that silently stops being recognised would degrade
to a session with no MCP servers and no error — this estate's signature failure, arriving through
a mechanism ccy built for itself.

**Specification:** before invoking with `--mcp-config`/`--strict-mcp-config`, assert the running
`claude` advertises them; abort naming the flag if not. Likewise assert the declared server's
binary exists and is executable before writing it into the config — a config naming a missing
binary is the same silent-degradation shape.

## Task 4.2 — tool-level restriction stays OUT

**Decision: out**, which the task told me to default to and which Decision 4 now makes structural
rather than a preference.

The consumer's `tool-matrix.sh` exists to serve `--permission-mode default` + `--allowedTools`
(`entrypoint.sh:210`, `:231-236`) — a **fail-closed** posture where an ungranted tool is refused.
Decision 4 declined that posture for ccy. A tool allowlist without fail-closed permissions is
decoration: it would narrow the MCP server's advertised tools while `--dangerously-skip-permissions`
(`claude-yolo:2792`) leaves everything else ungated. That is a control that *fires* without
*discriminating* — worse than absent, because its presence invites reliance.

`ccy`'s job is to **wire a server**. Owning one consumer's authorisation matrix is not it.

## Task 4.3 — the desktop ad-hoc case

The owner's original question was about injecting MCP into a *standard* ccy session, not only CI.
The interface above serves that identically and by construction: `--mcp <name>` is a launcher
flag with no CI dependency, and `ccy.env` is a desktop feature that predates this plan.

This is Decision 3's principle applied to Phase 4 — a capability that is generally useful must not
be gated behind the CI variant. The only CI-specific part is *where the binary comes from*
(`Dockerfile.ci` bakes it, per Task 3.1); a desktop user whose project image provides a server
uses the same flag.

---

# Phase 5 — egress restriction

## Task 5.1 — `--egress`, and a naming problem that turns out to be a capability conflict

Task 5.1 was scoped to reconcile `--egress` against `--network`. Two findings widen it.

**First, it is three-way** (restatement R11). `--network` *widens* reach — it attaches an
additional podman network (`claude-yolo:1801-1812`). `--no-network` does **not** narrow it: at
`:2514-2517` it merely leaves `NETWORK_FLAG` empty, so no `--network` argument is passed and
podman's default applies. There is no `--network none` in the codebase. `--no-network` is the more
dangerous of the two, because its name is an explicit safety promise it does not keep.

**Second — and this is the real problem — `--egress` and `--network` cannot both be honoured.**
The consumer measured it (`scripts/tests/pasta-loopback-forward-probe.sh:42`):

> "`--network pasta:…` and `--network ai-egress` are mutually exclusive"

Both occupy podman's single `--network` argument. So this is not a naming confusion to be tidied
with better labels — **a session cannot have both compose-service attachment and proxy-forwarded
egress.** Any design that renames the flags and stops there produces a launcher that silently
drops one of the two.

**Specification:**

1. `--egress` and `--network` together is a **hard error** naming the conflict — never a silent
   precedence rule. A precedence rule here is precisely the "one decision, two paths, one of them
   quietly wrong" shape.
2. Rename for honesty, with deprecation: `--network` → `--attach-network` (what it does), and
   `--no-network` → `--no-network-detect` (what it does — skips auto-detection and the
   connectivity preflight, and nothing else). Keep the old spellings working with a warning; they
   are in `ccy.env` files and muscle memory.
3. If genuine isolation is ever wanted, that is a *third*, separate flag mapping to
   `--network none`. It does not exist today and should not be conjured by renaming something.

## Task 5.2 — the mechanism, reusing the consumer's measurement

`policy/egress.sh:46-47`:

```
export SANDBOX_PROXY_PORT=3128
export SANDBOX_NETWORK="pasta:-T,${SANDBOX_PROXY_PORT}"
```

`-T <port>` makes podman's rootless network helper forward the **container's own** loopback port to
the **host's** loopback port (`egress.sh:13`; `pasta-loopback-forward-probe.sh:29-33`). One port,
one direction. Combined with `--http-proxy=true`, `http_proxy=http://127.0.0.1:3128` is injected
into the container, so every proxy-aware client routes through the host's allowlisting proxy.

**Why `--map-host-loopback` is rejected — measured, not argued.** `egress.sh:18-22` records that
podman's defaults are `--no-map-gw`, no `--map-host-loopback`, `-T none` (read out of the *live
pasta argv* in probe V8), so by default nothing in the container reaches the host at all;
`--map-host-loopback` is the other option that works and **it is wider** — V9 measured it exposing
the host's entire loopback (`pasta-loopback-forward-probe.sh:22-28`). Exposing every host loopback
service to obtain one proxy port is a trade nobody would make deliberately, which is why it is
recorded here rather than left for someone to rediscover.

Note the probes read the *actual* argv rather than trusting documentation — the same discipline
this plan applies elsewhere, and the reason these numbers are reusable.

## Task 5.3 — the minimum allowlist for a container that merely boots

Under the **desktop** entrypoint, booting requires GitHub:

| Touchpoint                   | Citation              | Fatal?                                     |
| ---------------------------- | --------------------- | ------------------------------------------ |
| `GH_TOKEN` present           | `entrypoint.sh:14-17` | **yes** — `exit 1`, no network needed      |
| `gh auth login --with-token` | `entrypoint.sh:33-36` | **yes** — `exit 1`; needs `api.github.com` |
| `gh auth status`             | `entrypoint.sh:53-56` | **yes** — `exit 1`; needs `api.github.com` |
| `api.github.com/meta`        | `entrypoint.sh:111`   | no — falls back at `:130-133` (fable §7)   |

So the minimum boot allowlist under the desktop entrypoint is **`api.github.com`**, plus a valid
token. E8's npm fetch is *not* in the boot set — it is launcher-side and, per Phase 3, never runs
at job time.

**Under the Decision 6 CI entrypoint the minimum boot allowlist is EMPTY**, because none of rows
1–4 exist: it prepares nothing, authenticates nothing, and asserts nothing about trust. That is a
concrete, previously-unstated payoff of Decision 6 — it removes GitHub from the *boot* path
entirely, so egress policy is decided purely by what the workload needs rather than by what
session-prep demands. A job that never talks to GitHub can have an allowlist that never mentions it.

The **runtime** allowlist is a different set and belongs to the workload, not to ccy. The estate's
version is in `lts-infra`'s `RUNNER-VM-DESIGN.md` §5.6.

## Task 5.4 — the proof

An egress control asserted but not measured is worth nothing. Three probes, because there are
three distinct things that can be true or false independently — the third is the one that proves a
**boundary** rather than a convention:

1. **Allowed host reaches through.** A real HTTP status from an allowlisted host through the
   proxy. A `401`/`404` counts — it proves TLS reached the origin.
2. **Denied host is refused BY THE PROXY.** Expect `403` *from squid*, not a timeout. A timeout
   would prove only that something failed, which is the weaker claim that gets mistaken for the
   stronger one.
3. **The bypass attempt is DROPPED.** Direct `:443` from the workload uid, expecting a timeout
   from the uid-fenced nftables policy. Without this, 1 and 2 together prove only that the proxy
   works *when used*.

`lts-infra`'s `RUNNER-VM-DESIGN.md` §9 T1/T2 already carries this exact battery with the
distinction between a proxy `403` and an L1 timeout spelled out; the ccy-side `triage.bash` should
reuse its shape rather than invent a parallel one.

**Two things the proof must not do**, both instances of this plan's recurring failure mode:

- It must not run from inside a nested container. Task 1.1's preamble already establishes that a
  nested result is not evidence about the host.
- It must not treat "the command failed" as "the control worked". Each probe asserts a *specific*
  outcome — a proxy `403`, a real status code, a timeout — not merely non-zero.

---

## What Phases 4 and 5 do not settle

- **No mechanism has been run.** The pasta numbers are the consumer's measurements on its own
  runner, reused as Task 5.2 instructed. They have **not** been re-measured under `ccy`, and ccy's
  container shape differs (writable `/workspace` bind mount, desktop entrypoint). Task 5.4's
  battery is specified, not executed.
- **The `--strict-mcp-config` decision changes desktop behaviour** for any project that currently
  relies on a checkout `.mcp.json` being picked up. Since ccy has no MCP support at all today
  (E4), nothing can be relying on it *through ccy* — but a user invoking `claude` inside a ccy
  session is a different path, and that interaction is unexamined here.
- **Task 5.1's rename has a blast radius not measured here**: `--network` may appear in projects'
  `ccy.env` files and in saved launch configs (`claude-yolo:2613`'s `save_launch_config`). The
  deprecation path is specified; the population is not surveyed.

---

## ⚠ CORRECTIONS APPLIED AFTER THIS DOCUMENT WAS WRITTEN

This report is preserved as written (line numbers are cited by later review rounds). The
correction blocks at the head of [../PLAN.md](../PLAN.md) are AUTHORITATIVE where they differ.
Appended per **D9**, which found that none of the six reports carried any correction note.

- **D1 and D3 rescoped Tasks 4.1, 5.1 and 5.3**; the pointer notes are in PLAN.md at those tasks.
- **`--no-network` does not isolate the container** (R11) — it skips the egress preflight only.
  Any text here treating it as an isolation control is superseded.
- The pasta measurements (`pasta:-T,3128`, `--map-host-loopback`, the mutual-exclusion finding)
  are the **consumer's**, taken on its runner, and are **not re-measured under `ccy`**. See
  `hardware-proof-checklist.md` group C — C3 is the load-bearing one.
