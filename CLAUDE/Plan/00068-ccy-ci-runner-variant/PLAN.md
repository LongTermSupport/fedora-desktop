# Plan 00068: Extend ccy so a CI/GitHub-runner-optimised session launches cleanly

> **RENUMBERED 00066 → 00068 (2026-07-30).** This plan was scaffolded as 00066 on its own
> branch while `F44` independently took 00066 for `00066-ftp-camera-airbnb-wifi-and-hotspot-triage`.
> Both are real plans with real history; the collision would surface the moment the branches meet,
> and the plan-QA `no-new-collisions` / `row-folder-bijection` checks would reject it. This plan
> moved because its owner (this agent) was asked to, and because the ftp-camera plan is already
> merged into the default branch — moving the merged one would rewrite shared history.
> 00067 was unavailable (`Completed/00067-qa-gates-inert-in-nested-checkout`), so 00068 it is.
>
> The git branch is still named `plan-00066-ccy-ci-runner`. That is left alone deliberately:
> renaming a pushed branch churns refs for no benefit, and a branch name is not a plan artefact.
> **`JOURNAL/` bodies also still say 00066** — they are append-only by rule, so they are corrected
> by this note and by a new dated entry, never by rewriting what was written.

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

> ## ROUND 1 CORRECTIONS — read this before the body below
>
> Everything from "## Overview" down is left **exactly as the round-1 reviewers saw
> it**, so `reports/fable-review-1.md` and `reports/sonnet-scan-1.md` keep referring to a
> real document. This block supersedes it where they conflict. Six of my own claims did
> not survive, and one of them was the plan's central thesis.
>
> **C1 — the plan missed the single most consequential axis (new E10).** `ccy` runs
> `claude --dangerously-skip-permissions` **unconditionally** (`claude-yolo:2792`), and
> there is no `--permission-mode`, `--allowedTools` or `--disallowedTools` surface
> anywhere in the launcher, the 7 libs, or the entrypoint. It is stronger than that: the
> entrypoint writes `"bypassPermissionsModeAccepted": true` into `.claude.json`
> (`entrypoint.sh:245`), so the image **pre-accepts** the bypass dialog. The consumer's
> design is the exact inverse — `--permission-mode default` plus an allowlist, so an
> ungranted tool is REFUSED. These are not two tunings of one launcher; they are opposite
> security postures, and this is the axis that decides whether any of this may be pointed
> at a pull request. Verified independently. *(fable §1)*
>
> **C2 — the thesis is wrong: "re-implementation" mischaracterises what the consumer
> built.** I wrote that `actions-hub` re-implements ccy in ~1,737 lines. It does not. It
> **reuses** the image — `ccy-baseline/Dockerfile:61` is literally `FROM claude-yolo:latest` — and **deliberately replaces** the launcher and entrypoint, with
> the reasoning written down at `run-sandbox.sh:375-402` (including the honest note that
> `--entrypoint /bin/bash` drops `tini`, accepted for a `--rm` short-lived container).
> So the shared surface is the **image**; the launcher and entrypoint diverge because the
> **trust models** diverge. Decision 1 rejected "a second launcher" and never considered
> the third option the evidence actually supports: **a second, small, CI-shaped
> entrypoint sharing the same base image** — which is approximately what the consumer
> has. Decision 1 must be rescoped; the `Dockerfile.ci` idea survives, the
> launcher-convergence framing does not. *(fable §2)*
>
> **C3 — E2's census was wrong, by my own method's blind spot.** I searched `read -rp`
> and reported "35 sites across the launcher and libs" as though exhaustive. Actual:
> **37** `read -rp` **plus 9** `read -r -p` = **46**. Every one of the 9 is in
> `lib/token-management.bash` — 878 lines, the entire credential subsystem, which my
> pattern missed completely because that file's house style separates the flags. Among
> them is `select_token` (`token-management.bash:611`), which `claude-yolo:1004` calls on
> the **default launch path**. So the earliest and most certain unattended blocker is
> credential resolution, which E2/E3 never cite. This is the recurring failure mode
> again — a true statement about *what one grep found* presented as a statement about
> *how many places ccy can hang*. Verified by re-running both patterns. *(fable §3)*
>
> **C4 — E3 was wrong: most of those sites do NOT spin, they abort.** `claude-yolo:41`
> sets `set -e`. My replication omitted it, so I proved a property of my own test script
> and not of the launcher. Re-tested with `set -e` present: **exit 1 at the first
> `read`**, no spin. Only prompts reached via an ancestor invoked as an `if`/`while`
> *condition* spin, because bash suspends `errexit` for that whole subtree — which is the
> two `check_*_containers_startup` container TUIs (`docker-health.bash:161`, `485`), not
> the sites I cited (`claude-yolo:1104`, `2011`, `network-management.bash:271`,
> `dockerfile-custom.bash:37/117/157`). The hang is real but **narrower**; the abort is
> still a defect (an undiagnosable `exit 1` mid-banner), so Task 2.1 stands — but
> "unattended ccy hangs" was overstated, and the two genuinely-hanging sites deserve
> priority precisely because a concurrent same-project job is what triggers them.
> *(sonnet §4-5, confirmed by my own re-test)*
>
> > **REFINED by Task 7.3** —
> > [reports/prompt-classification-round3.md](reports/prompt-classification-round3.md). C4's
> > *mechanism* is right and its negative claims all hold. Its **count of 2 should read 5**:
> > it never walked the call path into `select_token` / `create_token`, which every call site
> > guards with `||` — so C3's "earliest and most certain unattended blocker" is *also* a
> > hang, resolving the tension between C3 and C4. The suspending context is an
> > `if`/`while`/`until` **condition**, an `&&`/`||` non-final position, or `!` — and it
> > propagates transitively into bare calls beneath it, which is why a bare call site proves
> > nothing on its own.
>
> **C5 — a fifth capability was unnamed: credential acquisition, not just the prompts
> around it.** There is **no path** that accepts `CLAUDE_CODE_OAUTH_TOKEN` by value;
> `SELECTED_TOKEN` is always resolved from a file glob under `$TOKEN_DIR`, and `--token NAME` selects among *existing files*. `create_token` is an inherent human-in-a-browser
> OAuth flow (`token-management.bash:254`) with a manual-paste retry loop behind it, and
> its own comment concedes the recorded expiry is a 90-day guess. So CI needs (a) an
> out-of-band provisioning story and (b) a genuinely new "take the token from the
> environment and skip the token-file subsystem" mode — new functionality touching ~200
> lines, not a non-interactivity fix. *(sonnet §1, fable §5)*
>
> **C6 — "built by Ansible, never per-job" only answers the persistent-host case.** There
> is **no registry push or pull anywhere** in the launcher, the libs, or any play — every
> `build`/`commit` writes to local storage only. On a genuinely ephemeral runner
> "pre-built by Ansible" has no meaning. Task 3.4 needs an explicit
> self-hosted-only-for-now decision, or registry support as new scope. *(sonnet §2)*
>
> **C7 — a concurrency defect the plan never considered.** `get_next_container_name`
> (`lib/common.bash:583`) computes a free name by `ps -a` + increment with no lock, from a
> `PROJECT_NAME` derived purely from `basename $PWD` + parent — no run ID. Then
> `claude-yolo:2747` unconditionally runs `container_cmd rm -f "$CONTAINER_NAME"`, which
> force-removes a **running** container. Two concurrent jobs for one repo can therefore
> have the second kill the first's live container. The consumer salts with
> `GITHUB_RUN_ID`/`RUN_ATTEMPT` (`run-sandbox.sh:222`) precisely to avoid this.
> *(sonnet §3, fable §6)*
>
> **C8 — an unrelated hard dependency sits outside any egress policy.** With podman and
> without `--no-network`, ccy pulls **`alpine`** and fetches **plain
> `http://google.com`** in a *separate* container, exiting 1 on failure
> (`claude-yolo:2524-2599`). A correctly-scoped allowlist (GitHub + Anthropic + npm) will
> not contain it, so ccy hard-exits before the real container starts, for a reason an
> operator would find bizarre. `--no-network` is therefore **mandatory** for CI — which
> the plan never says. *(sonnet §6)*
>
> **C9 — Decision 2 contradicts shipped code without saying so.** I cited
> `ssh-handling.bash:357` as the one existing `HEADLESS_MODE` guard, without reading how
> it works: `if [ "${HEADLESS_MODE:-false}" = "true" ] || [ ! -t 0 ]`. It uses the very
> `-t 0` inference Decision 2 forbids, and prints which branch it took. So "never infer"
> should soften to **"never *silently* infer"** — the existing precedent is fine and the
> absolute rule was dogma. *(fable §4)*
>
> **C10 — compose services leak on any failing run.** The teardown block at
> `claude-yolo:2794-2846` is unreachable when the run exits non-zero, because `set -e`
> has already ended the script — including the ordinary CI case of a task that
> legitimately fails. Exit-code propagation itself is correct. *(sonnet §7)*
>
> **C11 — the ordering was over-serialised.** Only *proving* things unattended needs
> Phase 2; Phases 3/4 can be **designed** in parallel, and Decision 3 already concedes
> egress can be designed *and proven interactively* on a workstation. *(fable §8)*
>
> ### What this does to the plan's direction
>
> The goal is no longer "extend ccy so `actions-hub` deletes its stack" — C1 and C2 show
> that framing was wrong. The honest split, which Round 2 must restate as the plan's
> actual thesis:
>
> - **Shared and worth consolidating: the IMAGE.** `Dockerfile.ci` survives review and is
>   the one piece both sides already agree on.
> - **Irreducibly separate: the launcher/entrypoint for untrusted input.** A YOLO-by-
>   construction launcher and a fail-closed sandbox cannot be the same artifact. The
>   consumer's entrypoint should probably stay — the open question is whether it belongs
>   *here*, as a second small CI entrypoint beside `entrypoint.sh`, rather than in the
>   consumer.
> - **Worth fixing in ccy on their own merits, for TRUSTED automation:**
>   `--non-interactive` (C4's narrowed form), token-from-environment (C5), the
>   concurrency defect (C7), the `google.com` preflight (C8), the compose leak (C10).
>   None of these need the untrusted-PR threat model to justify them.
>
> Round 2 is required. Tasks 7.1-7.5 below carry the corrections forward.

> ## ROUND 2 — the restated thesis, and the decisions Round 1 demanded
>
> Full document, with every citation re-read from source rather than carried over:
> [reports/round2-restatement.md](reports/round2-restatement.md). Summarised here; the report
> governs where they differ.
>
> **The governing input is an owner steer** (verbatim): \*"allow each project to have its OWN ccy
> runner in the NORMAL WAY - dockerfile customisation, custom tooling etc etc - its perfect / BUT
>
> - CI need safety and MCP etc - it needs either adhoc or full blown customisation"\*.
>
> **R1 — the steer settles Task 3.3.** The per-project `.claude/ccy/Dockerfile` seam is the
> mechanism and is not to be replaced. The "mandatory platform overlay for the general case"
> branch is dead; it survives only for untrusted checkouts, which is a different plan.
>
> **R2 — `ccy` is THREE layers, and the steer is about exactly one.** Image / entrypoint /
> launcher. The steer is an **image** mechanism, so it delivers **tooling** — which is what it
> was built for and is genuinely excellent at. Safety lives entirely in the other two layers.
>
> **R3 — the entrypoint is INSIDE the image, so you cannot take one without the other.**
> `Dockerfile:215` sets `ENTRYPOINT`; none of the three project-facing templates declares one, so
> a project image built the normal way runs the **desktop** entrypoint — `GH_TOKEN`-or-die, `gh auth login`, the checkout symlink, the trust flags. Confirmed three independent ways, including
> two live productions failures in other repos (report §1.1). This is the structural reason half
> the steer cannot be satisfied by the mechanism the steer endorses.
>
> **R4 — E10, and it is stronger than C1 stated.** `ccy` asserts "this workspace is trusted" in
> **four** places, not one: `claude-yolo:2792` (`--dangerously-skip-permissions`, unconditional),
> `entrypoint.sh:245` (`bypassPermissionsModeAccepted`), `entrypoint.sh:257-263`
> (`hasTrustDialogAccepted`, unconditional), and `entrypoint.sh:269-274` — which **sources
> `/workspace/.claude/ccy/ccy.env` as shell** and then `exec`s `CCY_CLAUDE_WRAPPER` from it
> (`:280-282`). The last is new to this plan: **the checked-out tree controls the command that
> runs.** So the posture is not a loose default a flag could tighten; it is a coherent, deliberate
> trust model whose premise is that the operator owns the workspace.
>
> **R5 — Decision 4: `ccy` does NOT grow a permission surface.** C1 required this be answered.
> Two opposite postures in one artifact is the defect C2 already found; a launcher flag would not
> reach the entrypoint half anyway; and the estate already has stronger containment one trust
> boundary up (lts-infra `RUNNER-VM-DESIGN.md` §5.4/§6.4). **The price, stated: `ccy` in CI is for
> TRUSTED automation only, and is not a replacement for the consumer's sandbox.** This plan stops
> using that repo's deletion as its motivating example.
>
> **R6 — Decision 5: the trusted-only scope is asserted, not documented.** A required
> caller-supplied declaration with no default and no inference from `GITHUB_EVENT_NAME`; absence
> is a hard stop. Inference would be the banned `_armed` shape.
>
> **R7 — Decision 6: a second, small CI entrypoint beside `entrypoint.sh` — ADOPT.** The third
> option Decision 1 never considered. It fixes the problem at the layer it lives in (R3), and it
> is the correction for a defect now made three times by three codebases hand-rolling
> `--entrypoint /bin/bash`. Scoped deliberately: *prepare nothing, assert nothing about trust,
> exec what you were told*. It is **not** the consumer's fail-closed sandbox — Decision 4 declined
> that.
>
> **R8 — "ad-hoc or full-blown" resolves per capability, and two of four fit neither.** Tooling:
> done already. MCP: both routes viable, both need net-new wiring (E4 re-confirmed — zero matches).
> Permissions: neither route. Egress: ad-hoc only, being a runtime property.
>
> **R9 — the split is by TIME, not by feature.** Image build belongs at **provision** time
> (Ansible, wide egress armed for that window only); job time runs an already-built image. This
> closes **C6**: the runner is JIT-ephemeral *registration* on a *persistent* VM, so a
> provision-time build is coherent, and **registry support is declared out of scope** — confirmed
> costless, since searching the launcher and all 7 libs for engine `push`/`pull` returns zero.
>
> **R10 — three concrete gaps block lts-infra deleting its duplicate** (its Plan 00026 Task 3.3):
> a non-interactive build-and-exit mode; a build identity readable **from the image** (a `LABEL`,
> not `$HOME/.cache/claude-yolo-*-dockerfile-hash` at `claude-yolo:1454`, which is host-user-local
> and cannot answer CI's question); and a supported way to run a command without the desktop
> entrypoint. The second is a real defect independent of CI.
>
> **R11 — `--no-network` does not isolate, and that is worse than the `--network` trap.** C8's
> "mandatory" conclusion holds (preflight is fatal: `claude-yolo:2529`, `exit 1` at `:2597`), but
> at `:2514-2517` the flag merely leaves `NETWORK_FLAG` empty, so podman's default network still
> applies. There is no `--network none` in the codebase. **Task 5.1's naming problem is therefore
> three-way**, and `--no-network` is the dangerous one because its name is a safety promise it
> does not keep.

> ## ROUND 2 CORRECTIONS — read before the Round-2 block above
>
> [reports/fable-review-2.md](reports/fable-review-2.md): **2 BLOCKER + 2 MAJOR**. The five Round-2
> documents are left exactly as the reviewer saw them, so the review keeps referring to real
> documents (Task 6.3's method). This block supersedes them where they conflict.
>
> Both claimed corrections were independently confirmed (`claude-yolo:full` never existed; Ansible
> never builds `claude-yolo:base`), as were E10's four citations, R11, the OCI-inheritance claim
> and every consumer-repo citation. What did not survive is structural.
>
> **D1 — BLOCKER: I never said who invokes the CI entrypoint, and the two answers are not
> interchangeable.** I identified this fork early and then wrote five documents without ever
> settling it. Task 3.1's own words — *"Selection is the caller's, explicitly"* — imply a raw
> caller-side `podman run --entrypoint`, with **no `claude-yolo` involvement**. The reviewer is
> right that this orphans work.
>
> **The answer is BOTH, split by TIME — which R9 already established and I failed to carry into
> Phases 2/4/5:**
>
> | Phase              | Who invokes                                | `claude-yolo` involved?    |
> | ------------------ | ------------------------------------------ | -------------------------- |
> | **Provision time** | Ansible, to build the project image        | **yes** — build-and-exit   |
> | **Job time**       | the caller's own `podman run --entrypoint` | **no** — never on the argv |
>
> Consequences I must accept rather than argue with:
>
> - **Phase 5's `--egress` as a `claude-yolo` flag is orphaned for the CI path.** A job that never
>   invokes the launcher never passes it. CI egress is the **caller's own podman argv**, exactly as
>   the consumer already does. Phase 5 is *not wasted* — Decision 3 scoped `--egress` as
>   desktop-useful and not CI-gated, and that half stands — but Task 5.3's "under the Decision 6 CI
>   entrypoint" framing implied a ccy-enforced allowlist that does not exist. **Mis-scoped, not
>   wrong.**
> - **Task 7.4's C7 / C8 / C10 are session-launch defects a CI job never reaches.** They remain
>   real *desktop* defects and stay in scope as such; they are no longer CI-motivated.
> - **Phase 2 keeps a narrower justification**: the non-interactive **build-and-exit** mode
>   (R10 item 1), invoked by Ansible. That is materially smaller than "unattended `ccy` can hang",
>   and the plan should stop implying the larger one.
>
> **D2 — BLOCKER: Decision 4's safety citation defends the wrong threat.** I cited `lts-infra`
> `RUNNER-VM-DESIGN.md` §5.4/§6.4. §6.4 proves **hypervisor-escape** protection; §5.4 restricts
> **which destinations** are reachable. Round 1's C1 was neither: it is a **confused-deputy** risk —
> a live write-capable `GH_TOKEN` misused *at a destination the allowlist already permits*, since
> `api.github.com` is on every plausible allowlist. Neither cited section touches it. Worse, the
> citation smuggles an assumption: those controls belong to one purpose-built VM, and **`ccy`
> neither requires nor checks for them** — it runs on a laptop or a plain cloud VM just as well.
>
> **Taking remedy (a): the RUNNER-VM-DESIGN citation is WITHDRAWN from Decision 4.** The decision
> rests solely on the trusted-only scope (R5), which is self-sufficient. **The residual, stated:
> if the trust declaration is ever wrong, there is no defence-in-depth inside `ccy` against a live
> `GH_TOKEN` being misused — on any host.** That is the price of Decision 4, and it is larger than
> the price I first wrote down.
>
> **D3 — MAJOR: Task 4.1's declarative MCP route contradicts Decision 6.** Decision 6 says the CI
> entrypoint omits the §2 trust assertions — of which **row 4 is `ccy.env` sourcing**. Task 4.1
> then puts the declarative MCP route *in `ccy.env`*, citing the desktop entrypoint's sourcing
> code. Both cannot hold.
>
> **Resolved: the CI entrypoint does NOT source `ccy.env`.** Decision 6 stands as written — row 4
> is exactly as strong a trust assertion as rows 2–3, and keeping it would gut the decision. So
> **the `ccy.env` declarative route is DESKTOP-ONLY**, alongside `--mcp`. The CI path gets an
> explicit **environment-variable contract supplied by the caller**, mirroring the consumer's own
> required-environment contract. Phase 4 must be re-read with that split.
>
> **D4 — MAJOR: Decision 5 is an unverified self-attestation, and my own Task 4.2 principle
> convicts it.** Task 4.2 rejects the consumer's tool-matrix because *"a control that fires without
> discriminating is worse than absent, because its presence invites reliance."* Decision 5's flag
> **fires** (it gates startup) and **discriminates nothing** — nothing checks that a caller passing
> it is telling the truth. I applied the principle to someone else's design and not to my own.
>
> **Stated plainly, as it should have been: the trust declaration is an unverified attestation
> whose only enforcement is that the caller typed the word.** The residual is accepted — `ccy`'s
> desktop trust model is built the same way — but it is recorded, not dressed up as closing C1.
>
> **And my "no inference from `GITHUB_EVENT_NAME`" over-reached.** The no-armed-flags rule bans
> *deriving* the decision from environment state. It does not ban *cross-checking* an
> already-explicit declaration against observable state. Those are different, and I collapsed them.
> A `pull_request_target`-from-a-fork event that arrives *with* the trusted flag is a
> declaration/environment **disagreement**, and warning or refusing on it is defence-in-depth, not
> the banned shape. Re-opened as an option.
>
> **The loop is NOT quiet.** Round 2 found material problems, so a Round 3 is required (Task 6.4).

## Overview

`ccy` is this repo's Claude-Code container launcher. It is built for one situation: a
human at a Fedora workstation, at a TTY, with a desktop session. A consuming
repository (`LongTermSupport/actions-hub`, private) needs the same thing on a headless
GitHub Actions runner, unattended, with restricted egress and an MCP server wired in.
Rather than extend `ccy`, that repo built its own: **~1,737 lines** across
`ccy-baseline/Dockerfile` (169), `run-sandbox.sh` (510), `resolve-image.sh` (256),
`entrypoint.sh` (240), and a `policy/` tree (~400). It re-implements image resolution,
the container invocation, credential handling, and the entrypoint — all of which `ccy`
already does.

That is the **fourth** re-implementation of upstream in that estate; the previous three
were deleted for the same reason. The fix is not a better fork. It is to grow the
capabilities `ccy` is missing, **at source, here**, so the consumer deletes its copy and
becomes a caller.

This plan is **design + audit only**. It changes no code. Its output is a reviewed,
hostile-audited design and a task breakdown that a later plan executes.

## Goals

- Establish, with cited evidence, exactly what `ccy` lacks for unattended CI use —
  separating *proven* gaps from *suspected* ones.
- Decide the shape: what becomes a flag, what becomes an image variant, and what must
  NOT be forked. Record the reasoning, not just the choice.
- Preserve, provably, the existing project-extensibility contract: a project's own
  `.claude/ccy/Dockerfile` keeps working, and gains the ability to build on a CI base.
- Produce a task breakdown ordered by dependency, where each task has a verification
  that runs on the **host** (not in a nested container).
- Run the design through an audit/fix loop with each round tracked to a file under
  `reports/`, until a round finds nothing material.

## Non-Goals

- **No code changes in this plan.** Not one line of `claude-yolo`, the libs, the
  Dockerfiles, or the plays. Explicit owner instruction.
- **No work on the `actions-hub` side.** Its deletion is the *consequence*; it is
  tracked in that repo's own plan (lts-infra Plan 00015 / 00022).
- **Not a second launcher.** See Decision 1 — a forked `ccy-ci-runner` script is the
  thing this plan exists to avoid.
- **No changes to Plan 00065's files.** Another agent is active in this repo on the
  Cloud Base blockers. This plan touches `CLAUDE/Plan/00068-*/` only.

## Context & Background

### What `ccy` is, precisely

Two things share the `claude-yolo` name and conflating them has already caused one
wrong design in the consuming repo:

| Thing                          | What it is                                                                                                                                                                                    |
| ------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `files/var/local/claude-yolo/` | The **launcher and its image**. `claude-yolo` (2847 lines) + 7 libs + `Dockerfile` + entrypoint. Deployed by `play-claude-yolo.yml`. This is "ccy".                                           |
| a project's `.claude/ccy/`     | That **project's** ccy state (`sessions/`, `history.jsonl`, `settings.json`, `ccy.env`) **and** its own `Dockerfile` — the dev container for working on that repo, `FROM claude-yolo:latest`. |

A CI capability belongs in the first. Putting it in the second means every consuming
repo repeats it, and a repo's dev container carries CI-only weight it never uses.

### Evidence — proven

Every row was read out of the source at `eb14ba2`. Line numbers are in
`files/var/local/claude-yolo/`.

| ID     | Finding                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| ------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **E1** | **`--headless` already exists — but it is a Claude-Code-invocation mode, not a launcher mode.** It sets exactly two things: `claude -p "$PROMPT"` instead of an interactive invocation (`claude-yolo:2626-2628`), and `-i` instead of `-it` (`2694-2699`). It requires `--prompt` (`728-740`).                                                                                                                                                                                                                                                                                                                   |
| **E2** | **35 interactive `read -rp` prompts across the launcher and libs**, and `HEADLESS_MODE` guards exactly **one** of them (`lib/ssh-handling.bash:357`). The rest fire regardless of `--headless`.                                                                                                                                                                                                                                                                                                                                                                                                                  |
| **E3** | **At least ten of those prompts are `while true` menu loops that spin forever on EOF.** With no TTY, `read` returns non-zero with an empty value, the `case` falls to `*) echo "Invalid choice"`, and the loop repeats — unbounded. Confirmed by replicating the exact loop shape and feeding it `/dev/null`: it span until an injected counter tripped. Sites include `claude-yolo:1104`, `claude-yolo:2011`, `lib/docker-health.bash:162,369,486`, `lib/network-management.bash:271`, `lib/dockerfile-custom.bash:37,117,157,718`. **This is the hard blocker: unattended `ccy` can hang instead of failing.** |
| **E4** | **`ccy` has no MCP support of any kind.** An exhaustive search for `mcp`/`MCP` across `claude-yolo`, all 7 libs, all 4 Dockerfiles and `entrypoint.sh` returns **zero matches**. So "inject MCP into a ccy session" is a net-new capability, not a repair.                                                                                                                                                                                                                                                                                                                                                       |
| **E5** | **`--network` means the OPPOSITE of restriction.** It attaches the container to an additional podman network so it can reach compose services (`claude-yolo:148`, `1801-1812`). The default is `--network podman` (`2516`). There is a *positive* connectivity probe that fetches `http://google.com` and warns if it fails (`2529`). There is no proxy, allowlist, or egress restriction anywhere. **A flag named `--network` that widens reach is a naming trap for anyone implementing restriction.**                                                                                                         |
| **E6** | **`--device /dev/dri:/dev/dri` is passed unconditionally** (`claude-yolo:2773`) — the single occurrence, with no guard. By contrast the GUI socket mounts *are* properly guarded (`2704-2727`). Whether podman treats a missing device node as fatal is **NOT yet verified** — see Task 1.1.                                                                                                                                                                                                                                                                                                                     |
| **E7** | **The entrypoint hard-requires `GH_TOKEN`** and exits 1 without it (`entrypoint.sh:14-17`), then runs `gh auth login --with-token` (`33`) and `gh auth status` (`53`). Both need reachable GitHub. Under a restricted-egress design, GitHub must be allowed or **the container never starts**.                                                                                                                                                                                                                                                                                                                   |
| **E8** | **A daily in-container `npm i -g @anthropic-ai/claude-code@latest`** runs once per 24h per image (`auto_update_claude_code`, `claude-yolo:1254`; `update_claude_inplace`, `1343`). In CI this is non-deterministic and needs npm-registry egress. `CCY_AUTO_UPDATE=0` degrades it to notify-only.                                                                                                                                                                                                                                                                                                                |
| **E9** | **Existing extension seams that a CI mode must reuse, not duplicate.** `CCY_EXTRA_MOUNTS` — env-supplied `-v` tokens (`1781-1790`); `.claude/ccy/ccy.env` — tracked per-project config sourced *in-container* (`entrypoint.sh:265-274`); `CCY_CLAUDE_WRAPPER` / `--supervise` — wraps the `claude` invocation (`2759-2768`, `entrypoint.sh:280-284`); `CCY_CONTAINER_ENGINE`, `CCY_AUTO_UPDATE`.                                                                                                                                                                                                                 |

### Evidence — the consumer already proved each piece

`actions-hub` did not speculate; it built and ran these. That makes them requirements
with a known shape, which is why this plan can be specific.

| Capability           | What the consumer built                                                                                           | Proven                                                                                    |
| -------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| CI tooling in a base | `ccy-baseline/Dockerfile` — bakes `github-mcp-server` v1.7.0 by pinned sha256 (`:118-125`)                        | Builds; the fetch needs release-download egress **at build time**                         |
| MCP wiring           | `entrypoint.sh:173-193` — asserts the binary, checks it advertises `--tools`, writes an MCP config                | Runs; `policy/tool-matrix.sh` + `scripts/mcp/` derive a checked tool allowlist            |
| Egress restriction   | `policy/egress.sh` — `pasta:-T,3128` forwarding container loopback to a host squid                                | Measured on the live runner VM: allowed host tunnels, denied host gets 403 **from squid** |
| Platform-owned layer | `policy/sandbox-overlay.Dockerfile` — built `--network=none`, creates mount points, asserts `claude`/`jq` present | Needed because a repo Dockerfile cannot be trusted to provide platform invariants         |

The last row is the interesting one: the consumer independently discovered that **one
project Dockerfile is not enough** — it needed a platform-controlled layer on top of a
repo-controlled one. `ccy` today has no such concept.

## Technical Decisions

### Decision 1: One launcher, layered images — NOT a `ccy-ci-runner` script

**Context.** The request floated "a `ccy-ci-runner`, a special version of ccy". Taken
literally that is a second launcher.

**Options.**

- **(A) Forked launcher + forked image.** A `ccy-ci-runner` script beside `claude-yolo`.
  *Against:* it is re-implementation #5. The two would drift on every one of the 35
  prompt sites, the token path, the image-version gate, and the podman argv. This repo's
  own `CLAUDE.md` naming rule and the three deletions behind it say precisely this.
- **(B) One launcher, orthogonal flags, layered images.** `claude-yolo` grows
  `--non-interactive`, `--mcp`, `--egress`. CI *tooling* lives in a new image layer
  (`Dockerfile.ci`, `FROM claude-yolo:full` → tag `claude-yolo:ci`) so the desktop image
  does not carry MCP servers it never runs. A project's `.claude/ccy/Dockerfile` may be
  `FROM claude-yolo:ci` and keeps working unchanged.

**Decision: (B).** The variant that is justified is an **image**, not a launcher. Image
layering is already the repo's model (`base` → `full` in the existing `Dockerfile`), so
`ci` is a third stage in an established pattern rather than a new mechanism. Every
behavioural difference becomes a flag on the one code path that is already exercised
daily by desktop use — so CI cannot silently rot.

**Consequence for the three options as posed:** they are not alternatives. A CI image
variant, MCP injection, and egress restriction are three of the four things a CI mode
needs; the fourth (non-interactivity) was not named and is the prerequisite for all of
them.

### Decision 2: `--non-interactive` is separate from `--headless`, and lands first

**Context.** `--headless` (E1) sounds like the flag for this and is not. It describes how
*Claude Code* is invoked (`-p`). The gap is that the *launcher* still prompts (E2, E3).

**Options.** Widen `--headless` to also suppress prompts / add a distinct
`--non-interactive` / infer from `[ ! -t 0 ]`.

**Decision: a distinct `--non-interactive`, and never infer.** Widening `--headless`
silently changes behaviour for existing users who pass it at a TTY. Inferring from
`-t 0` makes behaviour depend on invocation context, which is how a CI hang becomes
unreproducible by hand. An explicit flag is greppable and testable. It lands **first**
because until `ccy` reliably fails instead of hanging, no other CI capability can be
verified unattended.

**Semantics:** under `--non-interactive`, every prompt site becomes one of —
*(i)* satisfied from a flag/env already, *(ii)* takes a documented safe default and logs
that it did, or *(iii)* **fails fast** with a message naming the flag that would have
answered it. Never a silent default, never a wait.

### Decision 3: Egress restriction is independent of CI, and useful on the desktop

**Context.** The consumer's proven egress work exists to defend **prompt injection**:
its AI action files GitHub issues from *system log lines*, which carry
outsider-controlled strings (crafted SSH usernames, User-Agents) into a tool-using model
holding a live write token.

**Decision:** `--egress` is not a CI-only feature and must not be gated behind the CI
variant. The same threat applies to a desktop `ccy` session pointed at untrusted input.
It ships as an independent flag, usable from either. This also means it can be developed
and proven **on the workstation**, where iteration is cheap, before a runner exists.

## Tasks

### Phase 1: Ground the unverified claims (host-run, no nesting)

Everything here runs on the **HOST**. This session is inside a podman container; a nested
`podman` test is not evidence about the host — an attempt to check E6 that way failed for
an unrelated userns/subuid reason and would have been mistaken for a result.

- [ ] 🔄 **Task 1.1**: Resolve E6 — does an absent `/dev/dri` make `ccy` fail?
  - [x] ✅ `triage.bash` probing: whether `/dev/dri` exists; whether
    `podman run --device /dev/dri` succeeds; whether `--device` with a *missing* node
    is fatal or ignored; podman version. **Rebuilt on the plan-script library**
    (lts-infra Plan 00023 Task 3.4) after the first version shipped a defect — see below.
  - [x] ✅ E6 **CONFIRMED A BLOCKER** by the owner's host run:
    `EXIT 125 — Error: stat /dev/plan00068-definitely-absent: no such file or directory`.
    A missing `--device` path is **fatal** to podman, so `claude-yolo`'s unconditional
    `--device /dev/dri:/dev/dri` is a guaranteed day-one failure on any host without
    `/dev/dri` — i.e. every headless server. `--device` must become conditional, exactly as
    the GUI socket mounts at `2704-2727` already are.
  - [ ] ⬜ Owner re-run of the rebuilt `triage.bash` on the HOST, to collect the remaining
    facts (image provenance, deployed-vs-checkout drift, prompt census) with the probes that
    previously mis-reported. **Needs a human** — the script now refuses to run in the
    container, correctly.
  - [ ] ⬜ Record the verdict in `reports/`. The E6 verdict itself is settled above.

#### The first `triage.bash` was defective, and the fix is upstream of this plan

The original hand-rolled `triage.bash` resolved its repo root with
`git rev-parse --show-toplevel` — following this repo's own `PlanWorkflow.md`, which
recommended it. `git rev-parse` answers about the **cwd**, not the script. Run by path from
`lts-infra`'s root it resolved to *that* repo, wrote its report there, and the
deployed-vs-checkout drift probe compared against
`<lts-infra>/files/var/local/claude-yolo/claude-yolo` — a path that does not exist there:

```
sha256sum: /home/<user>/Projects/LTS/lts-infra/files/var/local/claude-yolo/claude-yolo:
           No such file or directory
```

It then printed `Could not checksum both files` and carried on. The one probe meant to catch
launcher drift degraded into a shrug.

Fixing this one script would have fixed nothing, because the guidance was wrong. So the fix
landed upstream, as **lts-infra Plan 00023**: a tested `CLAUDE/Plan/_planlib.inc.bash` in this
repo, `CLAUDE/PlanScriptStandards.md`, and corrections to **both** places that recommended the
defective idiom (`CLAUDE/PlanWorkflow.md` and `CLAUDE/Plan/CLAUDE.md`).

What changed in the rebuilt script, beyond the root resolution:

- **`plan_require_host` enforces the host-only rule.** This plan's Phase-1 preamble already
  said a nested podman result is not evidence about the host; it was a comment asking nicely.
  It is now a guard: run in the container, the script exits 1 naming `/run/.containerenv`, and
  creates **no** run directory — so there is no half-written report to be mistaken for
  evidence. Proven from `/tmp` inside the container.
- **"Could not determine" is now a non-zero exit**, not a printed shrug. A gather leg failing
  means the *fact-finding* was incomplete — which is the honest report — rather than a check
  that quietly passed.
- **The prompt census counts both `read -rp` and `read -r -p`.** The earlier census searched
  only the first spelling, missed nine sites (the whole of `lib/token-management.bash`), and
  reported a total as if it were complete. That correction is C3 in the round-1 block above.
- **Probe logic moved into `probe-engine.bash` and `probe-launcher.bash`.** A leg command is
  passed by name, so a local shell function used that way lints as unreachable (SC2317), and
  suppressions are banned. Each probe is independently runnable and independently lint-clean.

All three scripts are `bash -n` and `shellcheck -x` clean. Note that
`./scripts/qa-all.bash` could **not** be used as evidence here: `qa-bash.bash` excludes
`*/untracked/*` against the absolute path, and this checkout lives under
`untracked/repos/fedora-desktop`, so it scans zero files and reports `✓ bash: 0 files OK`.
Tracked as a finding in lts-infra Plan 00023.

- [x] ✅ **Task 1.2**: Enumerate all 35 prompt sites into a table in `reports/`, each
  classified: *on the default launch path* vs *only reachable on an error/recovery
  path*, and *EOF-safe* vs *EOF-spins*. E3 proves the loop shape spins; this task
  establishes **which** sites a CI job would actually hit.

  **DISCHARGED by [reports/prompt-classification-round3.md](reports/prompt-classification-round3.md)**
  — 46 sites, not 35, each classified EOF-spins vs aborts, reproducibly, by tooling whose
  invariants can fail.

  **One axis is answered only partially, and inherently so.** The default-path-vs-error-path split
  this task asks for is a *runtime reachability* question, and Round 3 deliberately limits its
  reachability claims to what the call graph shows unconditionally (`select_token`/`create_token`
  sit on the default path when no token is pre-provisioned). Whether a given CI job reaches the
  compose or zombie-container paths depends on runtime state no static analysis models — the same
  residual risk Task 2.3 records for the regression guard, for the same reason. Confirming a spin
  end-to-end remains a HOST triage item.

- [x] ✅ **Task 1.3**: Confirm what `play-claude-yolo.yml` deploys and whether the image
  is built by Ansible or on first `ccy` run — this decides where `claude-yolo:ci`
  gets built and whether a CI job ever builds an image (it must not).

  **DONE. Ansible builds it** (`play-claude-yolo.yml:338-343`, `-t claude-yolo:latest`), verified
  at `:347`. The first `ccy` run does **not** — it only rebuilds if `validate_container_version`
  fails (`claude-yolo:1436`), which on a freshly-provisioned box it does not.

  **The build context is ASSEMBLED at `/opt/claude-yolo` from TWO source trees**, which is the
  non-obvious part and the thing that decides where `Dockerfile.ci` goes:

  | From                           | What                                                                                      |
  | ------------------------------ | ----------------------------------------------------------------------------------------- |
  | `files/var/local/claude-yolo/` | `Dockerfile` (`:86`), `entrypoint.sh` (`:98`), ctrl+z patch (`:110`), `plugins/` (`:122`) |
  | `files/opt/claude-yolo/`       | `ccy-startup-info.txt` (`:147`), `docs/` (`:158`), `skills/` (`:187`)                     |

  This resolves something that looks broken on inspection: the `Dockerfile`'s
  `COPY docs/…` and `COPY skills/…` reference paths that do **not** exist beside it in
  `files/var/local/claude-yolo/`. They work only because the play assembles the context first.

  Separately deployed (not build context): the launcher to `/var/local/` (`:280`), the libs
  (`:230`), bashrc includes (`:292`, `:301`), and the token/projects directories (`:317`, `:325`).

  **Consequence for `claude-yolo:ci`**: adding `Dockerfile.ci` to `files/var/local/claude-yolo/` is
  *not sufficient* — the play must also copy it into the assembled context and add a build task.
  And per Phase 3 §0.2 that same change must fix `claude-yolo:base`, which the play never builds
  at all despite three documents offering it.

### Phase 2: Design `--non-interactive`

> **Phase 2 delivered — [reports/phase2-non-interactive.md](reports/phase2-non-interactive.md).**
> Phase 2 was written before Round 3 measured anything, so the document restates Task 2.1 before
> answering it.

- [x] ✅ **Task 2.1**: For every site from Task 1.2, specify which of the three
  outcomes (satisfy / default+log / fail-fast) applies, and for fail-fast the exact
  message and the flag that answers it.

  **DONE — and the task's own unit of work is wrong, per Round 3.** "For every site" cannot be
  answered: `create_token`'s seven prompts ABORT reached bare and SPIN reached via `select_token`.
  Same lines, two verdicts. The deliverable is **entry-point decisions**, and there are far fewer
  than 46.

  Outcomes assigned by cause: **(i)** credential resolution is *removed* from the unattended path
  by token-by-value, not guarded (7 sites, and the earliest blocker); **(ii)** default+announce at
  the three container/compose entry points where "do not start it" is safe and statable;
  **(iii)** fail fast naming the **flag**, not the prompt — a CI operator cannot act on "the
  launcher asked something".

  **Decision 2 revised here: "never infer" → "never *silently* infer"** (C9). An inference is
  permitted when announced on stderr with what and why, and a safe statable default.
  `ssh-handling.bash:362` is the reference implementation, not a latent bug.

- [x] ✅ **Task 2.2**: Decide the interaction with `--headless` and `--prompt`
  (orthogonal? does `--non-interactive` imply anything?) and state it explicitly.

  **DONE.** Orthogonal axes — `--headless` is a *Claude Code invocation* mode
  (`claude-yolo:2626-2628`, `:2694-2699`, requires `--prompt` at `:728-740`); `--non-interactive`
  is a *launcher* mode. `--non-interactive` does **not** imply `--headless` (that would force
  `--prompt` on a caller who wants a live TTY afterwards). `--headless` **does** imply
  `--non-interactive`, **announced** — it already declares no human is driving, and a launcher
  that then blocks contradicts the declaration.

  **Cost stated rather than buried**: this changes behaviour for existing `--headless` users at a
  TTY, whose existence C4 established. The announcement makes it visible on the run where it
  first bites; that is the mitigation, not a claim the change is free.

- [x] ✅ **Task 2.3**: Specify the regression guard. A prompt added later must not
  silently reintroduce a hang — propose a QA gate wired into `qa-all.bash` that fails
  when a `read -rp` exists on a path reachable under `--non-interactive` without a
  guard. Note honestly whether this is statically decidable, and if only partially,
  what the residual risk is.

  **DONE — partially decidable, and the tooling already exists.** `analysis/classify-prompts.bash`

  - `bashctx.py` + `fnmap.py` (397 lines) already compute it, exit 1 on any failed invariant, and
    are mutation-tested. They already carry the Task 7.2 lesson as an assertion (`:83-84`): the
    census pattern is checked against a file *known* to use `read -r -p`, so a pattern matching
    nothing can never again report a clean sweep.

  **Decidable**: is it a prompt site; which function encloses it (validated by re-parse); is that
  function reachable from a suspending context (transitive); is it in an unbounded loop.
  **Not decidable**: whether a *given job* reaches it at runtime. Further gaps named: indirect
  dispatch is invisible to a static call graph, and a prompt in an unwalked sourced file would be
  missed — which is exactly how `token-management.bash` escaped the Round-1 census.

  Honest framing: the gate makes new spins and new unguarded default-path prompts impossible to
  add **silently**. It does not prove the absence of a hang.

  **Promotion is part of finishing this plan, not a follow-up.** This repo's own
  `CLAUDE/PlanWorkflow.md` names "a permanent QA gate wired into `qa-all.bash`" as the persistent
  case; left in `CLAUDE/Plan/00068-*/` the gate dies when the plan is archived. Seed it as a
  **ratchet** (stale baseline entries must fail too, or it only grows).

### Phase 3: Design the image layering + CI variant

> **Phase 3 delivered — [reports/phase3-image-layering.md](reports/phase3-image-layering.md).**
> It opens with two corrections to this plan's own premises, both found while trying to specify
> against them:
>
> - **`claude-yolo:full` does not exist.** It is a Dockerfile *stage* (`Dockerfile:231`), never a
>   tag; the `full` stage is published as **`claude-yolo:latest`** (`common.bash:564-565`,
>   `claude-yolo:107`). Zero matches for `claude-yolo:full` across `files/`, `playbooks/`, `docs/`.
>   The name was invented in Decision 1, repeated into Task 3.1, and passed Round 1 unchallenged.
> - **Ansible never builds `claude-yolo:base`** (`play-claude-yolo.yml:338-343`, no `--target`
>   anywhere in the play), yet three project-facing documents offer `FROM claude-yolo:base` as a
>   supported choice. It is produced only by a *launcher-triggered* build, which a provisioned box
>   has no reason to run. **A documented base whose existence depends on rebuild history** — and
>   the precedent for exactly how not to introduce `claude-yolo:ci`.

- [x] ✅ **Task 3.1**: Specify `Dockerfile.ci` as a stage/file `FROM claude-yolo:full`:
  what it adds (MCP server binaries, pinned by checksum), and what it must NOT add.

  **DONE** — and the task's own `FROM` is corrected to `claude-yolo:latest` per the note above.
  A separate **file**, not a fourth stage: the main Dockerfile's hash feeds `DOCKERFILE_HASH` and
  `validate_container_version` (`common.bash:466-469`), so a stage would make CI-tooling churn
  rebuild every desktop user's image. Adds exactly two things — the MCP binary pinned by sha256,
  and the Decision 6 CI entrypoint. Must NOT add: credentials, project toolchain, egress or
  permission policy (both runtime), or an `ENTRYPOINT` override — it *ships* the CI entrypoint,
  it does not make it the default.

- [x] ✅ **Task 3.2**: Specify how a project selects it, and prove on paper that an
  existing `.claude/ccy/Dockerfile` (`FROM claude-yolo:latest`) is unaffected.

  **DONE.** Selection is `FROM claude-yolo:ci` in the project's own Dockerfile — no new flag, the
  seam the owner endorsed used for a different base. Non-interference proven from the resolution
  path: the rebuild decision reads four inputs (`claude-yolo:1487-1529`) and a new tag changes
  none; `:1478` reads the base version from a hard-coded `"claude-yolo:latest"`. **Two residuals
  recorded rather than glossed** — a project directory named `ci` would collide with the shared
  tag (guard proposed), and that same hard-coded `:1478` means a `ci`-based project image is
  staleness-checked against the *wrong* base. The second is a real gap this design introduces,
  and it is handed to 3.3 rather than left implicit.

- [x] ✅ **Task 3.3**: Address the platform-vs-repo layer problem the consumer hit: is a
  `ccy`-owned overlay applied on top of a project's Dockerfile warranted, or is that
  complexity only justified for untrusted-checkout CI? **Argue both sides** — the
  answer is not obvious and getting it wrong in either direction is expensive.

  **DONE — no overlay**, settled by the owner steer (R1). Both sides resolve the same way *given
  Decision 4*, and that dependency is stated as the honest caveat: reverse Decision 4 and the
  overlay returns immediately, because an untrusted Dockerfile cannot be allowed to define the
  layer asserting the platform's own invariants.

  - [x] ✅ Cover the version-gate interaction: `REQUIRED_CONTAINER_VERSION`
    (`claude-yolo:39`) currently gates one base; state how it behaves with a variant.

    **DONE, and it is the one real design problem in Phase 3.** Three options; (A) parse the
    project Dockerfile's `FROM` — rejected, fragile and ambiguous under multi-stage; (B) gate
    `ci` on `REQUIRED_CONTAINER_VERSION` — rejected, re-couples CI churn to the desktop rebuild
    cycle that 3.1 split apart; **(C) move the staleness identity into an image LABEL** —
    adopted. C is R10's item 2 and fixes the gate as a side effect. It also kills a defect that
    exists today independent of CI: staleness lives in
    `$HOME/.cache/claude-yolo-${PROJECT_NAME}-dockerfile-hash` (`claude-yolo:1454`), which is
    **host-user-local**, so a provisioning user cannot answer the question for the user that runs
    jobs — precisely the question CI asks.

- [x] ✅ **Task 3.4**: Specify how the CI image is built by **Ansible**, never per-job.
  E8 (daily npm auto-update) and the consumer's build-time release fetch both need
  egress that a locked-down job must not have.

  **DONE.** Five steps mirroring the working `claude-yolo:latest` build, each inside an
  **arm → build → drop** egress window with the drop in an `always:` block so the wide posture
  cannot outlive a failed build. Step 5 fixes the `claude-yolo:base` gap above — adding a third
  history-dependent tag while leaving the second broken would be indefensible.

  **E8 resolves for free and that is worth saying out loud**: the auto-update lives in the
  *launcher* (`claude-yolo:1254`, `:1343`), and R9's by-time split means the launcher does not run
  at job time — so E8 never fires in CI. `CCY_AUTO_UPDATE=0` is explicitly **not** the answer: a
  job that needed it would already be running the launcher, which is the actual defect. That
  variable is the plausible-looking fix that would have hidden the real problem.

### Phase 4: Design MCP injection

> **Phases 4 and 5 delivered — [reports/phase45-mcp-and-egress.md](reports/phase45-mcp-and-egress.md).**
> Designed together because both are **runtime** properties, so neither is reachable from the
> mechanism the owner's steer endorses (restatement §3) and both must be launcher/entrypoint
> surface or nothing.

- [x] ✅ **Task 4.1**: Specify the interface (`--mcp <name>`? a `ccy.env` declaration?
  both?), and where the config is written given the entrypoint already symlinks
  `/root/.claude` → `/workspace/.claude/ccy` (`entrypoint.sh:183-195`).

  **DONE — both, because the owner asked for both**: `--mcp <name>` (ad-hoc/desktop) and an
  `MCP` declaration in `ccy.env` (declarative/tracked) *select* servers; a baked binary
  (`Dockerfile.ci` or the project image) *supplies* them.

  **The config must NOT go in the symlinked location.** `entrypoint.sh:195` means anything under
  `/root/.claude` lands in the checkout — which both mutates the job's tree and makes the config
  an input the checkout controls, i.e. E10 row 4 in a second costume. Spec: a container-local
  path, passed via `--mcp-config`, regenerated per launch. Not state.

  **Two hard assertions specified**, the second copied deliberately from the consumer
  (`entrypoint.sh:133-137`, rationale at `:114-115`): assert the running `claude` actually
  advertises `--mcp-config`/`--strict-mcp-config`, and assert a declared server's binary exists.
  **ccy needs this more than the consumer does** — it auto-updates Claude Code daily in place
  (E8), so its CLI is a moving target *by its own design*, and a silently-unrecognised flag would
  yield a session with no MCP servers and no error.

  Also decided: ccy passes `--strict-mcp-config` whenever it passes `--mcp-config`, so a
  checkout's `.mcp.json` cannot silently merge into a session ccy claims to have configured
  (`entrypoint.sh:213-214` records the semantics).

- [x] ✅ **Task 4.2**: Decide whether tool-level restriction (the consumer's
  `tool-matrix.sh` + checked vocabulary) belongs in `ccy` or stays consumer policy.
  **Default to "stays out"** unless there is a general case — `ccy`'s job is to wire a
  server, not to own one consumer's authorisation matrix.

  **DONE — stays out**, and Decision 4 makes it structural rather than a preference. The
  consumer's matrix serves `--permission-mode default` + `--allowedTools`; without that
  fail-closed posture a tool allowlist is decoration — it narrows the MCP server's tools while
  `--dangerously-skip-permissions` leaves everything else ungated. **A control that fires
  without discriminating**, which is worse than absent because its presence invites reliance.

- [x] ✅ **Task 4.3**: State how this serves the ad-hoc desktop case the owner asked
  about ("inject MCP into a standard ccy session"), not just CI.

  **DONE.** `--mcp` is a launcher flag with no CI dependency and `ccy.env` predates this plan —
  Decision 3's principle (a generally-useful capability must not be gated behind the CI variant)
  applied to Phase 4. The only CI-specific part is where the *binary* comes from.

### Phase 5: Design egress restriction

- [x] ✅ **Task 5.1**: Specify `--egress`. Resolve the `--network` naming collision from
  E5 head-on: two flags whose names suggest the same axis and act oppositely is a
  trap. Propose either a rename (with a deprecation path) or names that cannot be
  confused.

  **DONE, and the task under-scoped its own problem twice.** It is three-way, not two (R11:
  `--no-network` does not narrow anything). And more seriously — **it is not a naming problem at
  all, it is a capability conflict.** The consumer measured that `--network pasta:…` and
  `--network <name>` are mutually exclusive (`pasta-loopback-forward-probe.sh:42`); both occupy
  podman's single `--network` argument. So a session **cannot** have both compose-service
  attachment and proxy-forwarded egress, and any design that renames the flags and stops there
  ships a launcher that silently drops one of the two.

  Spec: `--egress` + `--network` together is a **hard error** naming the conflict, never a
  precedence rule. Renames for honesty with a deprecation path: `--network` → `--attach-network`,
  `--no-network` → `--no-network-detect`. Real isolation, if ever wanted, is a *third* flag
  mapping to `--network none` — it does not exist today and must not be conjured by renaming
  something that never did it.

- [x] ✅ **Task 5.2**: Specify the mechanism, reusing the consumer's *measured* result
  (`pasta:-T,<port>` forwarding container loopback to a host proxy) rather than
  re-deriving it. Record why `--map-host-loopback` was rejected: it was measured to
  expose the host's entire loopback.

  **DONE** — `pasta:-T,3128` per `policy/egress.sh:46-47`, plus `--http-proxy=true` injection.
  `--map-host-loopback` rejected on the recorded V8/V9 measurements (`egress.sh:18-22`;
  `pasta-loopback-forward-probe.sh:22-33`): podman's defaults already expose nothing, and
  `--map-host-loopback` exposes the host's **entire** loopback to obtain one proxy port. The
  probes read the live pasta argv rather than trusting docs, which is what makes the numbers
  reusable here.

- [x] ✅ **Task 5.3**: Reconcile with E7 — the entrypoint cannot start without reaching
  GitHub — and with E8's npm fetch. State the minimum allowlist for a container that
  merely boots.

  **DONE.** Under the **desktop** entrypoint: **`api.github.com`** — three fatal touchpoints
  (`entrypoint.sh:14-17`, `:33-36`, `:53-56`) plus one soft (`:111`, falls back at `:130-133`).
  E8 is not in the boot set at all (launcher-side; never runs at job time — Phase 3).

  **Under the Decision 6 CI entrypoint the minimum boot allowlist is EMPTY** — it prepares
  nothing and authenticates nothing. A previously-unstated payoff of Decision 6: it takes GitHub
  off the *boot* path, so egress policy is decided by what the workload needs rather than by what
  session-prep demands.

- [x] ✅ **Task 5.4**: Specify the proof. An egress control asserted but not measured is
  worth nothing; a `triage.bash` must show a denied host actually refused **by the
  proxy** and an allowed host reaching through.

  **DONE — three probes, not two.** Allowed-through (a real status code; `401`/`404` counts),
  denied **by the proxy** (a `403` *from squid*, explicitly not a timeout — a timeout proves only
  that something failed, the weaker claim that gets mistaken for the stronger), and the
  bypass attempt DROPPED by the uid fence. Without the third, the first two prove only that the
  proxy works *when used*. Reuses the shape of `lts-infra`'s `RUNNER-VM-DESIGN.md` §9 T1/T2
  rather than inventing a parallel battery. Must not run nested (Task 1.1's rule), and each probe
  asserts a *specific* outcome rather than merely non-zero.

### Phase 6: Audit / fix loop — tracked to files

Each round is a file in `reports/`. A round that finds nothing material ends the loop.

- [x] ✅ **Task 6.1**: Round 1 — hostile review of Phases 1-5 (fable). Brief: attack the
  design, not the prose. Hunt specifically for *a true statement about a check
  presented as a stronger statement about the world* — the recurring failure mode in
  this estate, and the reason Task 1.1 exists as a task rather than an assertion.
  → `reports/fable-review-1.md`. **2 BLOCKER, 4 MAJOR, 2 MINOR** — both blockers landed
  on the thesis, not the details.

- [x] ✅ **Task 6.2**: Round 1 — independent deep scan (sonnet) for what the author and
  the hostile reviewer both missed. Read the actual source; do not trust this plan's
  own citations. → `reports/sonnet-scan-1.md`. **5 CRITICAL, 2 HIGH, 2 MEDIUM**, and it
  caught E3 being wrong — a claim the hostile reviewer had accepted.

- [x] ✅ **Task 6.3**: Apply round-1 findings. Corrections **append**; never rewrite a
  section a reviewer has already reviewed, or their finding stops referring to a real
  document. → the ROUND 1 CORRECTIONS block at the head of this file, C1-C11. Each
  correction was re-verified against source before being accepted; the body is untouched.

- [ ] 🔄 **Task 6.4**: Repeat rounds until a round finds nothing material. Record every
  round, including the quiet one that ends the loop.

  **Round 2 run and applied** — [reports/fable-review-2.md](reports/fable-review-2.md),
  **2 BLOCKER + 2 MAJOR**, carried into the ROUND 2 CORRECTIONS block as D1–D4. **The loop is
  NOT quiet; Round 3 is required.**

  Both BLOCKERs are structural and both are mine. **D1** — I identified the "who invokes the CI
  entrypoint" fork early, then wrote five documents without settling it; the answer (both, split
  by time) was already implied by R9 and I failed to carry it into Phases 2/4/5, which orphans
  `--egress`-as-a-launcher-flag for the CI path and narrows Phase 2's justification. **D2** — my
  containment citation answered *escape* and *destination*, not the **confused-deputy** threat
  that Round 1's C1 was actually about; citation withdrawn, and Decision 4's true price recorded.

  **D4 is the one worth remembering**: Task 4.2 rejects the consumer's tool-matrix as "a control
  that fires without discriminating", and Decision 5's trust flag does exactly that. I applied
  the principle to someone else's design and not to my own, in the same document.

- [ ] 🔄 **Task 6.5**: Final gate — restate the design in one page, and list what a
  **later** implementation plan must prove on real hardware before any task is ✅.

  - [x] ✅ **The hardware-proof list** —
    [reports/hardware-proof-checklist.md](reports/hardware-proof-checklist.md). Five groups:
    (A) claims made from code paths that runtime state could contradict — **A1, whether
    `claude-yolo:base` exists on a provisioned box, is load-bearing and would retract Phase 3
    §0.2 if it comes back present**; (B) behaviour classified but never executed, where **B4
    (does `--no-network` actually leave the container networked?) would shrink Task 5.1 back to a
    naming problem if it comes back the other way**; (C) the consumer's measurements reused but
    never re-measured under `ccy`, of which **C3 is the one to re-measure first** since it is the
    only borrowed claim driving a hard failure; (D) the three-probe egress battery; (E)
    prerequisites, both of which are blocked.

  - [ ] ⬜ **The one-page restatement** — deliberately held until Round 2 reports, since that is
    the half an audit finding could invalidate. The checklist above is stable either way: a
    hostile finding would *add* to it rather than change it.

### Phase 7: Round-2 restatement — carry C1-C11 into a corrected design

Round 1 invalidated the thesis, so Round 2 is a restatement rather than a polish. These
tasks **replace** the corresponding Phase 2-5 tasks above where they conflict.

- [x] ✅ **Task 7.1**: Restate the thesis per C1/C2 — the shared surface is the **image**;
  the launcher and entrypoint diverge because the trust models diverge. Evaluate on the
  merits the third option Decision 1 never considered: *a second small CI entrypoint
  beside `entrypoint.sh`, sharing the base image*.

  **DONE — [reports/round2-restatement.md](reports/round2-restatement.md)**, summarised as
  R1–R11 in the ROUND 2 block at the head of this file.

  The restatement is sharper than "the shared surface is the image", because the image turns
  out to *contain* the entrypoint (`Dockerfile:215`; no `ENTRYPOINT` in any of the three
  project-facing templates). So the three-layer split — image / entrypoint / launcher — is the
  load-bearing structure, and it is what makes half the owner's ask unreachable from the
  mechanism the owner endorses. That is R2/R3 and it is the whole finding.

  The third option is **adopted** (Decision 6), on a merit Decision 1 could not have seen:
  it is the correction for a defect three separate codebases have now made independently,
  each hand-rolling `--entrypoint /bin/bash` and two of them getting it wrong.

  - [x] ✅ Add **E10** (the `--dangerously-skip-permissions` axis) to the evidence table
    with its own Decision — does `ccy` grow a permission surface at all, or is the CI path
    explicitly "container + network boundary only"? Either answer is defensible; silence
    is not.

    **E10 recorded (R4); Decision 4 = NO, it does not.** C1 named one citation; there are
    **four**, and the fourth — `entrypoint.sh:269-274` sourcing the workspace's own `ccy.env`
    as shell, then `exec`ing `CCY_CLAUDE_WRAPPER` from it (`:280-282`) — means the checked-out
    tree controls the command that runs. That reframes the posture from "a loose default" to
    "a coherent trust model", which is what makes declining a permission surface the *right*
    answer rather than the lazy one.

  - [x] ✅ State plainly whether these flags serve *trusted* automation rather than
    replacing `run-sandbox.sh` — and if so, stop using the consumer's deletion as the
    motivating example.

    **Stated (R5): trusted automation only.** The consumer's deletion is no longer the
    motivating example; the motivating example is lts-infra's duplicate staleness-gate, which
    R10 shows is blocked on three concrete, nameable gaps. Decision 5 makes the scope an
    asserted precondition rather than a documented intention.

- [x] ✅ **Task 7.2**: Re-run the prompt census per C3 with a pattern that also matches
  `read -r -p`, across all seven libs (expect ~46). Give `select_token`/`create_token`
  their own named sub-problem — they sit on the default path and are the earliest blocker.

  **DONE — [reports/prompt-census-round2.md](reports/prompt-census-round2.md). C3 confirmed:
  37 → 46, and the predicted ~46 is exact.**

  **The miscount is the least interesting part.** All 9 missed prompts are in
  `lib/token-management.bash`, and that file was **100% invisible** to the Round-1 pattern — not
  undercounted, unseen. It uses `read -r -p` exclusively, so a pattern requiring `-rp` found
  nothing and reported **zero**. A file with nine blocking prompts on the default path looked
  exactly like a file with none. That is this repo's recurring failure shape — a control that
  silently becomes a no-op — living inside the census itself.

  **The named sub-problem is worse than "more prompts" — and my first statement of it was
  wrong.** I originally wrote that `create_token:322`'s `while true` suspends `errexit` and spins.
  Executing bash disproved it: a loop *body* does not suspend errexit, only a *condition* does.
  What decides spin-vs-abort is the **call context of the enclosing function**:

  ```
  f() { while true; do read -r -p "x: " v; ...; done; }
  ( f )      < /dev/null   -> rc=1, zero iterations   ABORTS
  f || { … } < /dev/null   -> "SPUN 5x"               SPINS
  ```

  So the real spinner is **`select_token:610`**, because `claude-yolo:1004` and `:1117` call it as
  `select_token … || { … }` — the `||` suspends errexit through the entire function body, the
  EOF-failed `read` does not abort, an empty selection prints `Invalid selection: (empty)` and
  `continue`s, forever. `create_token` called bare **aborts** instead — and aborts *undiagnosably*,
  which is C4's other class. Reached *via* `select_token` it inherits the suspension and spins.

  **The same source line is therefore a spin or an abort depending on the path that reached it**,
  which is the substance of Task 7.3 and the reason "classify each site" cannot be done by looking
  at the site.

  **This changes a Task 7.4 priority.** `create_token` is an irreducibly human OAuth flow, and it
  is on the default path when no token is pre-provisioned. So accepting `CLAUDE_CODE_OAUTH_TOKEN`
  **by value** (C5) is not one capability among six — it is the *only* way the default path is
  survivable unattended, and should be treated as a prerequisite.

  Also superseded: every task text quoting "35 prompts" now reads 46. And the census pattern is
  itself a known-fragile control — whatever re-runs it must be asserted against a file known to
  contain `read -r -p`, so a pattern matching nothing can never again report a clean sweep.

  Claimed by inspection only: that these prompts exist. **Superseded by Task 7.3** — the
  spin/abort mechanism was subsequently *executed* rather than inferred, and the `:322` loop
  turns out to spin only when reached via `select_token`; called bare it aborts. `ccy` itself
  still has not been run, so confirming the spin end-to-end remains a HOST triage item.

- [ ] 🔄 **Task 7.3**: Re-scope `--non-interactive` per C4/C9. Classify each site *spins* vs
  *aborts undiagnosably*; fix the spinning TUIs. Soften "never infer" to "never **silently**
  infer", reconciling with `ssh-handling.bash:357`, which already does precisely that.

  > **This task's own wording was wrong and is corrected here.** It said the discriminator is
  > "errexit suspended by an `if`/`while` **ancestor**". A loop *body* does not suspend
  > `errexit` — only a *condition* does. The discriminator is the **call context of the
  > enclosing function**, propagated transitively: `outer || …` suspends `errexit` through
  > `outer`'s whole body *and* through anything `outer` calls bare. Measured, not reasoned.

  - [x] ✅ **Classification complete** —
    [reports/prompt-classification-round3.md](reports/prompt-classification-round3.md),
    reproducible via [analysis/classify-prompts.bash](analysis/classify-prompts.bash) (exits 1
    if any invariant breaks; mutation-tested in both directions).

    | Verdict                     | Sites |
    | --------------------------- | ----: |
    | ABORTS only                 |    32 |
    | SPINS only                  |     6 |
    | SPINS **or** ABORTS by path |     6 |
    | Falls through, or aborts    |     1 |
    | GUARDED — never reached     |     1 |

    Three findings change the remaining work:

    1. **There are five spin paths across four functions, not "two spinning TUIs"** —
       `select_token`, `create_token` (beneath it), `show_zombie_container_tui`,
       `check_project_containers_startup`, `_do_compose_start`.
    2. **The same source line gets different verdicts by path.** `create_token`'s seven
       prompts ABORT when called bare from `claude-yolo`, and SPIN when reached via
       `select_token` (which every call site guards with `||`). Classification is a property
       of the call graph, not of the site — so "fix each site" is not a well-formed unit of
       work; the fix belongs at the entry points.
    3. **`ssh-handling.bash:357` is the only guarded prompt of all 46.** It detects
       non-interactivity, *announces* the inference, and proceeds. The correct pattern is
       already in the codebase and used **once out of 46 opportunities** — so "never
       *silently* infer" is not a new mechanism to design, it is an existing one to apply.

  - [ ] ⬜ Fix the five spin paths — a spin needs *both* a suspended call context *and* an
    unbounded loop with no EOF exit; removing either breaks it. Prefer the
    `ssh-handling.bash:357` shape (detect non-interactive, announce, proceed or fail loudly)
    over adding EOF checks to 46 individual `read`s.

  - [ ] ⬜ Make the 32 abort sites diagnosable — today `set -e` kills the script with no
    message naming the prompt.

  - [x] ✅ Soften "never infer" to "never **silently** infer" in the design text, citing
    `ssh-handling.bash:357` as the reference implementation.

    **DONE — Decision 2 revised in [reports/phase2-non-interactive.md](reports/phase2-non-interactive.md)**
    (Task 2.1). The permitted shape: announced on stderr, states what was inferred and why, takes
    a safe and statable default. Silent inference stays banned. The guard is at
    `ssh-handling.bash:362` (Round 3's re-read; C9 and the earlier task text both cite `:357`,
    which is the enclosing `if`).

    **The two implementation sub-items above are now SPECIFIED but remain open**, correctly — this
    plan changes no code. Phase 2 gives them their design: guard the five **entry points** rather
    than 46 `read`s (a spin needs both a suspended context and an unbounded loop, so breaking
    either suffices), and make the abort sites diagnosable via outcome (iii), whose message must
    name the **flag** rather than the prompt.

  - [ ] ⬜ Requires a CCY version bump when the launcher/libs are edited
    (`CLAUDE/ContainerRules.md`), and QA on the HOST.

- [x] ✅ **Task 7.4**: Add the capabilities Round 1 surfaced as missing.

  **SPECIFIED — [reports/task74-capabilities.md](reports/task74-capabilities.md).** "Add" reads
  as "specify" here: this plan changes no code by explicit owner instruction (Non-Goals), so each
  item below is a statement of required behaviour handed to the implementation plan.

  - [x] ✅ **Token from environment** (C5) — accept `CLAUDE_CODE_OAUTH_TOKEN` by value,
    bypassing the token-file subsystem, plus the out-of-band provisioning story, since
    `create_token` is an irreducibly human OAuth flow.

    **C5 holds — and the evidence contains a trap.** `CLAUDE_CODE_OAUTH_TOKEN` has **twelve**
    occurrences, one of them on the `podman run` argv (`claude-yolo:2777`), so a grep says the
    capability exists. It does not: the variable is an **output** channel ccy populates
    (`:2756`, from `$CLAUDE_OAUTH_TOKEN`, set only at `:1033`/`:1135` as `$(cat "$SELECTED_TOKEN")`
    — a **file**), never an **input** a caller can fill. *The recurring failure mode running
    backwards*: a true statement about a grep would be a false statement about the world. Anyone
    "fixing" C5 by pointing at `:2777` has read the arrow the wrong way.

    Confirmed as the **prerequisite**, not one item of six: Round 3 showed `select_token` *spins*
    on EOF, on the default path, so an unattended launch hangs at credential resolution before
    anything else this plan designs is reached.

  - [x] ✅ **Concurrency safety** (C7) — run-ID-salted container names, and scope the
    `rm -f` safety net so it cannot reap a live sibling.

    **Confirmed verbatim** (`lib/common.bash:583-595` — no lock; `claude-yolo:2747` — unconditional
    `rm -f`). Spec is **two** independent changes, because they are two faults: scope the `rm -f`
    to non-running containers (which preserves the corrupt-storage case it was written for), *and*
    admit a caller-supplied run identifier (not inferred from `GITHUB_RUN_ID` — that is the
    derive-from-environment shape). Fixing either alone leaves a live hole.

  - [x] ✅ **`--no-network` mandatory for CI** (C8), or make the `alpine`/`google.com`
    preflight conditional. State which.

    **Both — and the sub-item's framing was incomplete.** The preflight is fatal
    (`claude-yolo:2529`, `exit 1` at `:2597`), so C8's "mandatory" holds. But `--no-network` does
    **not** isolate (`:2514-2517` — no `--network` argument is passed at all), so a design that
    treats it as isolation inherits a hole. It answers *skip the preflight* and nothing else.
    Spec: rename per Task 5.1 **and** make the preflight conditional on its own terms — a probe
    validating a *chosen* network must not run when egress is proxy-mediated, where it measures
    the wrong thing and fails on the right answer.

  - [x] ✅ **Compose teardown on failure** (C10) — `trap`-based, since `set -e` makes the
    current trailing block unreachable on a non-zero run.

    **Confirmed unreachable — but "trap-based" is the wrong fix and would cause a regression.**
    A trap already exists: `trap cleanup EXIT` (`claude-yolo:1716`), the only one in the tree. It
    fires reliably; `cleanup()` (`:1695-1715`) simply does not touch compose — it restores stty,
    removes `$CONFIG_TEMP`, prints the debug log. **A second `trap … EXIT` REPLACES the first**,
    so adding one would silently discard the temp-dir cleanup and the stty restore. Spec: move
    the teardown *into* `cleanup()`, and make it non-interactive-safe — the current block prompts,
    and a prompt on the exit path is a hang at the worst possible moment.

  - [x] ✅ **Image distribution** (C6) — self-hosted-only decision, or registry support as
    declared new scope.

    **Decided: self-hosted only, registry declared out of scope** (argument in Phase 3 Task 3.4).
    Costless to state — zero engine `push`/`pull` matches across the launcher and all seven libs,
    so there is no half-built path left to rot.

  - [x] ✅ **Workspace mutation** — decide whether a CI variant may write `.claude/ccy/`
    into the job checkout at all (`claude-yolo:2613`), given a PR checkout is untrusted.

    **Decided: no**, and by evidence rather than preference. `.claude/ccy/` is not merely written
    (`entrypoint.sh:185`, `:195`, `:204-226`, `:230-237`; `claude-yolo:2613`) — it is also
    **read and executed** (`entrypoint.sh:269-274` sources `ccy.env`; `:280-282` `exec`s
    `CCY_CLAUDE_WRAPPER` from it). Read-write *and* execution-bearing. This and Decision 4's
    trusted-only scope are the same decision seen from two directions.

- [x] ✅ **Task 7.5**: Re-order per C11 — Phases 3/4 designed in parallel with 2; only
  *proving unattended* is gated on Phase 2. Then re-run the audit loop (Task 6.4).

  **DONE, and demonstrated rather than asserted** — Phases 3, 4 and 5 were designed in this
  session *without* Phase 2 existing, which is C11's claim shown rather than argued. Revised
  order: **token-by-value → Phase 2 → unattended proof**, with Phases 3/4/5 parallel throughout.
  Token-by-value moves *ahead* of Phase 2 rather than being one of its sites: it is not a
  non-interactivity fix, and Phase 2 can demonstrate nothing unattended until it exists.
  Audit loop (Task 6.4) re-run dispatched against all four new documents.

## Dependencies

- **Coordination:** another agent is active in this repo on Plan 00065 (Cloud Base
  blockers). This plan is confined to `CLAUDE/Plan/00068-*/` and on branch
  `plan-00066-ccy-ci-runner` (branch name retains the pre-renumber number, see the header note),
  so it cannot collide with that work.
- **Counter note:** `hooksdaemon.latestPlanNumber` was stale at 63 while 00065 existed
  (00064/00065 arrived via `git pull` from another clone). Reconciled to 65 per
  `mkplan.bash`'s own drift-guard message before scaffolding; this plan was scaffolded as 00066
  and is now **00068** (renumbered — see the header note; the counter did not protect against a
  concurrent branch taking the same number).
- **Consumes:** the measured results from lts-infra Plan 00015 (egress probes V4-V9) and
  Plan 00022 (the re-implementation audit).
- **Blocks:** the `actions-hub` deletion of `run-claude-sandboxed` + `ccy-baseline`.
- **Not blocked by** Plan 00065 — this plan writes no plays and provisions nothing.

## Success Criteria

- [x] ✅ Every claim in this plan is either cited to a file:line or explicitly marked
  unverified with a named probe that resolves it.

  Each Round-2 report closes with an explicit "what this does not settle" section. Three claims
  are marked unverified with their resolving command: `claude-yolo:base`'s absence on a real host
  (`podman image ls claude-yolo`), the pasta measurements (the consumer's, on its runner, not
  re-measured under ccy), and end-to-end confirmation of the spins.

- [x] ✅ Task 1.1's `/dev/dri` question is answered by a host run, not by inference.

  Answered by the owner's host run: `EXIT 125 — Error: stat /dev/plan00068-definitely-absent: no such file or directory`. A missing `--device` path is fatal, so the unconditional
  `--device /dev/dri:/dev/dri` at `claude-yolo:2773` is a guaranteed day-one failure on any
  headless server.

- [x] ✅ The four capabilities each have a specified interface, and it is stated which are
  CI-only and which are generally useful.

  Non-interactivity (Phase 2), image layering (Phase 3), MCP (Task 4.1), egress (Task 5.1/5.2).
  CI-only: none of the four — egress is explicitly desktop-usable (Decision 3), MCP explicitly so
  (Task 4.3), and only *where the MCP binary comes from* is CI-specific.

- [x] ✅ A reader can say what happens to an existing `.claude/ccy/Dockerfile` — proven by
  reading the resolution path, not assumed.

  Task 3.2, from the four rebuild inputs at `claude-yolo:1487-1529` and the hard-coded
  `"claude-yolo:latest"` at `:1478` — with two residuals recorded rather than glossed.

- [ ] ⬜ The audit loop has run to a quiet round, with every round on disk in `reports/`.

  Round 1 (`fable-review-1.md`, `sonnet-scan-1.md`) and Round 3's classification are on disk.
  Round 2 dispatched against all five new documents. **Not yet quiet** — a round that finds
  nothing material has not happened, and Round 2 has not reported.

- [ ] 🚫 **No source file outside this plan folder has been modified.**

  **NOT MET, and the failure is this plan's own bookkeeping rather than a slip in this session.**
  Commit `73396b3`, under this plan's number, added **1,667 lines across six files outside the
  plan folder**: `CLAUDE/Plan/_planlib.inc.bash` (710), `scripts/test-planlib.bash` (623),
  `CLAUDE/PlanScriptStandards.md` (271), plus `CLAUDE/PlanWorkflow.md`, `CLAUDE/Plan/CLAUDE.md`
  and `CLAUDE/Plan/.gitignore`. That work is legitimate and documented in this file — it is the
  plan-script library, governed by lts-infra Plan 00023 — but it landed on this branch under
  *this* plan's number, which is exactly what this criterion forbids. Plan 00067's
  `da6de33` is also on the branch, though that is a different plan's work and not attributable
  here.

  **The Non-Goal it exists to protect IS met, and provably**: *"Not one line of `claude-yolo`,
  the libs, the Dockerfiles, or the plays."* `git diff --name-only F44...HEAD -- files/ playbooks/`
  is **empty**.

  So the criterion's *intent* holds and its *wording* does not. Recorded rather than ticked,
  because a criterion quietly reinterpreted to match what happened is not a criterion. The
  correct remedy is a wording fix that says what was meant — no changes to `files/`, `playbooks/`,
  or `scripts/` — plus an honest note that the plan-system work was mis-attributed to this plan
  number instead of being committed under 00023's.

## Risks & Mitigations

| Risk                                                                                        | Impact | Probability | Mitigation                                                                                          |
| ------------------------------------------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| Designing from this plan's citations instead of the source, propagating any error in them   | H      | M           | Task 6.2 re-reads the source independently and is told not to trust these citations                 |
| `--non-interactive` scope creep across 35 sites, stalling everything behind it              | M      | H           | Task 1.2 splits default-path from error-path sites; only the former block a first cut               |
| A nested-container test is mistaken for host evidence                                       | H      | M           | Owner instruction, now plan policy: anything needing an engine goes in `triage.bash` for a host run |
| Growing `ccy` for a CI consumer degrades the daily desktop experience                       | H      | M           | Decision 1 keeps CI weight in an image layer; Decision 3 keeps flags opt-in and default-off         |
| `claude-yolo` is 2847 lines with a version-hash gate; a large change is hard to land safely | M      | H           | Phases are independently shippable; each bumps `CCY_VERSION` per `CLAUDE/ContainerRules.md`         |
| The consumer keeps its fork anyway, leaving two implementations                             | H      | L           | Deletion is an explicit success criterion of the *consumer's* plan, gated on this one landing       |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00068-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Scaffolded on branch `plan-00066-ccy-ci-runner` as plan 00066; renumbered to **00068** on
  2026-07-30 to clear a collision with `F44`'s `00066-ftp-camera-…`. Failsafe recovery cron
  `ffc583d1`.
