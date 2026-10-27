# Plan 00080: Task 2.2 decision options

**Status: DECIDED by the owner, 2026-09-24: Option 2, a network per session.**

- **Naming:** the networks carry the prefix `ccy-isolate-`, and `podfreeze` ignores that
  prefix, so its menu does not fragment (D1).
- **Priority:** Low. Sessions on one machine reaching each other is not a significant
  attack vector, so the plan is parked until the owner schedules Phase 3.
- **Before Phase 3:** the measured caveats under Option 2 below still apply, since
  isolation is not the default on this host and H4 is unmeasured.

The options, the evidence for each, and what would reverse each are laid out below. The threat model behind them is
`THREAT-MODEL.md`. F-numbers are in `PLAN.md`. F29 to F38 are detailed in
`subagent-reports/260924-triage-findings-opus-5.5.md`. Option numbers follow
`research/findings.md` §5.

## The two inputs, as measured

- **(a) Is anything exposed?** Not now. Three snapshots found zero listeners a neighbour can
  reach (F22, F26, F30). The path is open (F32). One real service, a database, is bound to
  loopback only.
- **(b) What does the bridge buy?** Mid-session `ccy --connect`. Preferences are written
  *only* by `--connect` (F31), and 4 projects have one. After the first connect, though,
  those projects join their network **at launch**, which needs no shared bridge. The
  snapshot agrees: no live session is on a project network (F31). So the bridge buys the
  **first, mid-session attach** of a session launched without a network. Nothing in the repo
  measures how often that happens. **The owner knows; the repo does not.**

## Ruled out on evidence (not options)

- **Option 3** (`--network none`) and **Option 8** (`--internal`) have no egress, so Claude
  Code cannot reach its API.
- **Option 6** (`isolate=strict` on `podman`) is cross-network only. It does nothing inside
  one network (research F7).
- **Option 7** (`--network host`) is strictly worse.

## Option 1: keep the shared default, and document it

- **Change**: none to the launcher. Task 4.0 documents that `--no-network` is the isolating
  mode (F2b). The docs state the bind-address rule (loopback for anything a session serves).
  The `podfreeze` row is relabelled (D1, one line).
- **For**: (a) is empty three times running. The most valuable assets have no network
  surface. It costs nothing and leaks nothing. The `--connect` workflow is untouched.
- **Against**: the exposure is one ordinary action away (a dev server with `--host`). That
  action is invisible, and the bridge crosses project and identity boundaries (F29).
  Documentation depends on each session's agent reading it and following it.
- **Reverses if**: a P4 run finds a session listening on `0.0.0.0`/`::`. Or a non-CCY
  container is found on `podman`. Or sessions come to hold more sensitive served data.
  A P4 re-run *while a dev server is running* is the test that has not been done yet.

## Option 4: delete the override, and revert to pasta

- **Change**: remove `NETWORK_FLAG="--network podman"` (research F2). Default sessions get
  pasta and are isolated by design (F1). Nothing is created, so nothing can leak.
- **For**: this is a deletion. It gives zero-maintenance isolation, with no lifecycle, no
  H4, no DNS side-effect and no pre-2.0 isolate trap. Projects with a recorded preference
  **keep** joining their network at launch (F31). A session launched *on* a project network
  can still `--connect` elsewhere (a bridge-to-bridge attach, as in P8 / F34). In `podfreeze`
  it is the cleanest outcome: no change needed (D1).
- **Against**: `ccy --connect` on a session launched without a network fails with the
  original `"pasta" is not supported` error (research F3). That is the bug the override was
  added to fix. The launch preflight is also skipped when no network is selected (research
  F12/F13), so it would need re-homing.
- **Variant 4b** (new; it follows from F31): pasta by default, and `--connect` against a pasta
  session fails fast with a clear message. The message says "relaunch; the next launch will
  join `<net>`", and the preference is saved **before** failing, so the relaunch picks it up.
  This turns the lost capability into a restart, not an error. It does not keep the running
  session.
- **Reverses if**: the owner does mid-session attaches often enough that a restart is not
  acceptable. Or pasta turns out to differ from the bridge in something CCY relies on (not
  probed in this plan).

## Option 2: a network per session

- **Change**: create `ccy-<project>-<n>` at launch, attach, and remove it on exit, plus a
  reaper sweep at launch.
- **For**: it keeps mid-session `--connect`. P8 / F34 shows `network connect` works from a
  user-created bridge. Egress and the host alias work from a fresh network (F35).
- **Against, measured**:
  - **isolation is not the default here.** The host is below netavark 2.0 (F24), and P7's
    empirical check was confounded (F33). So `--opt isolate=` must be passed explicitly and
    **proven** by a sound acceptance probe. Otherwise it is the appearance of a fix;
  - **DNS changes silently.** A fresh network is DNS-enabled (F36), so `--disable-dns` is
    needed, or the `ensure_network_dns` skip must be extended;
  - **H4 is unknown.** P11 measured nothing (F37). A fixed P11 is a precondition of
    Task 3.1. A memberless leaked network holds no interface (F38), but a stranded one (its
    container also survived) was not observed;
  - **cost on every launch.** A network create, plus the preflight against a cold network
    (U12, not measured);
  - `podfreeze` **fragments** into one row per session (D1). That is more work than the
    status quo;
  - published-port reach under `isolate` (U7) is untested.
- **Reverses if**: a fixed P11 shows networks stranding in normal use. Or `isolate` breaks a
  workflow (U7). Or mid-session `--connect` turns out to be rare, which makes 4 or 4b cheaper
  for the same isolation.

## Option 5: a network per project

- Same machinery and caveats as Option 2 (F24/F33, F36, F37). Sessions of one project share
  a network, which fits the fact that they already share tokens, SSH key and tree. The
  lifecycle is harder: the network must be removed only when the last session leaves.
- In today's fleet every session is a different project (F29). So its isolation equals
  Option 2's in practice, with fewer objects when a project runs several sessions.
- **Reverses if**: several sessions per project becomes rare. Then it is Option 2 with a harder
  lifecycle.

## The question for the owner

**How often do you attach a *running* session to a project network, as opposed to launching
into it?**

- **Rarely, or a restart is fine**: Option 4 or 4b gives isolation for the cost of a deletion.
- **Often, and it must not restart**: Option 2 or 5, gated on a fixed P7 and P11.
- **Accept the latent exposure**: Option 1, with a P4 re-run while a dev server is up, to test
  the one assumption it rests on.

Whichever is chosen, record it as D3 in `PLAN.md` with the reason and the reversal
condition above. Then apply the D1 `podfreeze` consequence in Task 4.1.
