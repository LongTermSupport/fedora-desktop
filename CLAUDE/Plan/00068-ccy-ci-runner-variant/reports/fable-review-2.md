# Plan 00068 — Hostile Review, Round 2 (fable)

Scope per the brief: attack the design (not the prose), verify every citation against actual
source, hunt for a third instance of "a true statement about a check presented as a stronger
statement about the world," attack Decisions 4/5/6 hardest, verify the two claimed Round-2
corrections independently, and look for what the plan's own tasks silently assume is answered.

Every `file:line` below was re-read directly from source in this session — `files/var/local/ claude-yolo/{claude-yolo,entrypoint.sh,lib/*.bash,Dockerfile*}`, `playbooks/imports/ play-claude-yolo.yml`, `/workspace/docs/RUNNER-VM-DESIGN.md` (lts-infra), and
`/workspace/untracked/repos/actions-hub/.github/actions/run-claude-sandboxed/{entrypoint.sh, run-sandbox.sh,policy/egress.sh}` + `scripts/tests/pasta-loopback-forward-probe.sh` — none taken
on the documents' word.

**Verdict up front**: the two claimed corrections (`claude-yolo:full` does not exist;
Ansible never builds `claude-yolo:base`) are both independently confirmed correct — good,
careful work. But Round 2 has a structural hole at its centre that Round 1 flagged and Round 2
did not close: **the documents never state who invokes the Decision-6 CI entrypoint**, and the
two plausible answers are each incompatible with large parts of what Phases 2, 4 and 5 already
designed. This is a genuine BLOCKER, not a nitpick — it determines whether roughly half of this
round's deliverable work is actually on the path to the plan's stated goal.

---

## 1. BLOCKER — the plan never states who invokes the Decision-6 CI entrypoint, and the two answers are incompatible with what Phases 2, 4 and 5 already designed

**Evidence that selection is a raw, caller-side `podman --entrypoint` invocation, not a
`claude-yolo` capability.** Three independent statements in this round's own documents:

- `round2-restatement.md:160`: "A second entrypoint *in the image*, **selected by
  `--entrypoint`**, is the only fix that leaves the owner's endorsed mechanism intact."
- `round2-restatement.md:163`: "Every caller today hand-rolls `--entrypoint /bin/bash`" —
  citing `run-sandbox.sh:375-402` and `lts-infra`'s `runner-ccy-project-image.yml:287-300`, both
  of which invoke `podman run --entrypoint …` directly, with **no `claude-yolo` involvement at
  all**.
- `phase3-image-layering.md`, Task 3.1, "What it must NOT add": *"No `ENTRYPOINT` override of the
  desktop entrypoint. `Dockerfile.ci` **ships** the CI entrypoint as a file; it does not make it
  the default… **Selection is the caller's, explicitly.**"*

Read together, this is unambiguous: the CI entrypoint is meant to be selected by a caller
writing their own `podman run --entrypoint /opt/claude-yolo/<ci-entrypoint> …` command — exactly
the pattern the consumer and `lts-infra` already use, and exactly the pattern C2/R2 correctly
diagnosed as "structurally discarding" `claude-yolo`. Nowhere does `claude-yolo` (the 2,847-line
launcher) grow a flag that swaps its own `--entrypoint` argument at `claude-yolo:2770-2792` — no
such flag is proposed anywhere in Phase 3, 4, or 5, and Task 3.1 explicitly rules out the
alternative (making the CI entrypoint the image default).

**Why this matters — it is not academic, it invalidates the framing of two whole phases.**

If a CI caller invokes the container directly (bypassing `claude-yolo`), then:

- **Phase 5's entire deliverable is orphaned.** Task 5.1 specifies `--egress`/`--attach-network`/
  `--no-network-detect` as **`claude-yolo` CLI flags** (`phase45-mcp-and-egress.md:135-145`), and
  Task 5.3 talks about "the minimum allowlist ... under the Decision 6 CI entrypoint" as if some
  layer of ccy enforces it. But a caller who launches the CI entrypoint with their own `podman run` command never invokes `claude-yolo` at all, so a `claude-yolo --egress` flag is never on
  their command line. Egress for the CI path would have to be the **caller's own** podman argv
  (exactly as the consumer's `run-sandbox.sh` already sets `--network "$SANDBOX_NETWORK"`
  directly) — which is a completely different deliverable from "design and prove a
  `claude-yolo --egress` flag."
- **Phase 4's declarative MCP route is contradicted by Decision 6's own scope statement** — see
  Finding 3 below, which is a direct consequence of this same ambiguity.
- **Phase 2's `--non-interactive` work (Tasks 7.2/7.3's exhaustive 46-site classification) loses
  its stated justification for the CI use case**, though it keeps a *narrower* one: R10 item 1
  needs a non-interactive **build-and-exit** mode for provisioning-time project-image builds
  (`claude-yolo` invoked by Ansible, not by a CI job). That is real and worth keeping, but it is a
  materially smaller scope than "unattended `ccy` can hang instead of failing" (E3) implies, and
  none of Task 7.4's remaining items (C7 concurrency, C8 network preflight, C10 compose leak) are
  reachable from a build-and-exit invocation either — they are runtime-session defects in
  `claude-yolo`'s *session-launch* path, which a CI job that never invokes `claude-yolo` never
  executes.
- **Decision 4's own reasoning gets weaker, not stronger, once Decision 6 exists.** Decision 4's
  second bullet argues a launcher `--permission-mode` flag "would not be sufficient anyway" because
  rows 3–4 of E10 are entrypoint behaviour baked into the *desktop* entrypoint. But the CI
  entrypoint (Decision 6) **already omits rows 2–4** by design (its own Scope note: "It omits the
  §2 trust assertions because it never makes them"). If a caller invokes the CI entrypoint
  directly, `--dangerously-skip-permissions` is not baked in anywhere at all for that path — the
  caller constructs their own `claude …` command line and the CI entrypoint's whole job is to
  `exec` it unmodified. So for the CI-entrypoint path specifically, the permission posture is
  **already entirely the caller's choice, with zero `ccy` involvement**, which makes the elaborate
  Decision 4 debate (cite RUNNER-VM-DESIGN, weigh "does ccy grow a surface") arguably moot for
  that path — the real, simpler statement would be "the CI entrypoint imposes no permission
  posture of its own; each caller supplies their own, exactly as they already must supply their
  own `podman run` invocation." The document does not make this connection and instead argues
  Decision 4 as though `claude-yolo`'s own invocation line is what's at stake.

**What the plan must change.** Add an explicit task/decision that answers, in one place: *does
`claude-yolo` ever invoke the Decision-6 CI entrypoint, or is it exclusively a caller-invoked,
raw-podman artifact like the consumer's own entrypoint?* Then re-audit Phase 5 and the
declarative half of Phase 4 against that answer — as written, they are designed for a
`claude-yolo`-mediated CI path that Task 3.1's own words ("selection is the caller's, explicitly")
say does not exist.

---

## 2. BLOCKER — Decision 4's central safety citation defends against the wrong threat, and is scoped to infrastructure `ccy`'s own design does not require

The brief asked directly: does Decision 4's "the estate already has containment one boundary up"
argument smuggle in an assumption about *where* `ccy` will run? It does, and worse — even where
that assumption holds, the cited controls do not address the threat that motivated the debate.

**What Decision 4 actually cites.** `round2-restatement.md:123-127`: *"The estate already has
stronger containment one trust boundary up (`lts-infra` `RUNNER-VM-DESIGN.md` §5.4/§6.4)."*

**What §5.4 and §6.4 actually say, read in full** (`/workspace/docs/RUNNER-VM-DESIGN.md`):

- **§6.4** ("PROOF: the runner VM has no pve1-host access", lines 484-515) is entirely about
  **hypervisor-escape** protection: host firewall default-deny, SSH IPSet restriction, PVE API
  closure, no inter-guest routing, no host credential on the guest. All five controls answer "can
  the runner VM reach the Proxmox host or another guest" — none of them answer anything about
  what a container running inside that VM can do with the credentials it was *already handed*.
- **§5.4** (lines 308-331) is a three-layer **destination-allowlist** (L1 host FORWARD, L2
  VM-local nftables + CONNECT-proxy, L3 podman network) — it restricts *which external hosts* the
  workload can reach. It says nothing about what the workload can do to the destinations it is
  **already allowed** to reach.

**Why the mismatch matters.** Round 1's original BLOCKER #1 (`fable-review-1.md` §1, correctly
preserved and cited by E10/R4) is not a network-egress or hypervisor-escape concern — it is that
`--dangerously-skip-permissions` plus a live, write-capable `GH_TOKEN` inside a session processing
possibly-injected content is a **confused-deputy** risk: the model can push code, delete
branches, or exfiltrate the token itself through a destination the allowlist already permits
(`api.github.com`/`github.com` are on every plausible allowlist, since the workload needs them to
function at all). Neither §5.4 nor §6.4 defends against that — §5.4 restricts *destinations*,
not *actions at an allowed destination*; §6.4 restricts *hypervisor reachability*, not
*container-to-workload-owner reachability*. This is precisely the reason the consumer built
`--permission-mode default` + `--allowedTools` + server-side `--tools` narrowing
(`fable-review-1.md §1`, `actions-hub/.github/actions/run-claude-sandboxed/entrypoint.sh:210-236`,
verified above) **in addition to** its own egress allowlist — the consumer's design treats these
as two independent layers because they defend against two independent things. Decision 4 cites
only the layer that does not address the threat in question, and presents it as if it did.

**The "smuggled assumption about where `ccy` runs."** Even setting the mismatch aside: §5.4/§6.4
describe **one specific VM**, purpose-built by `lts-infra`'s own plan, with an armed L2
CONNECT-proxy and a uid-fenced nftables policy. Nothing in `ccy`'s design (Decision 5's "required
declaration", or anything else) requires, checks, or asserts that the host running the Decision-6
CI entrypoint has any of this. `ccy` is a `fedora-desktop` artifact usable on any host that runs
the image — a laptop, a bare cloud VM, a Jenkins runner with no L2 proxy at all. If Decision 4's
safety argument for declining a permission surface leans on "the estate already has containment
one boundary up," that containment is a property of one consumer's *optional*, separately-built
infrastructure, not a property `ccy` enforces or even checks for. A different caller who adopts
the Decision-6 entrypoint on a plain VM gets **none** of the §5.4/§6.4 protections and **none**
of a permission surface either — a strictly worse position than either side of the debate as
individually described.

**What the plan must change.** Either (a) drop the RUNNER-VM-DESIGN citation from Decision 4's
justification and rest the decision solely on the "trusted-only scope" argument (R5) — which is
self-sufficient and does not need borrowed infrastructure to be true — and explicitly name the
residual: *if the trust declaration (Decision 5) is ever wrong, there is no defense-in-depth
inside `ccy` against a live `GH_TOKEN` being misused, regardless of which host runs it*; or (b)
scope Decision 4 explicitly to "when running with `lts-infra`'s specific L1/L2/L3 controls armed"
and say plainly that the decision does not hold, and must be revisited, for any other caller.
Leaving the citation as blanket justification is the recurring failure mode itself: a true
statement about one VM's hypervisor-isolation proof, presented as a stronger statement about
`ccy`'s safety in general.

---

## 3. MAJOR — Task 4.1's declarative MCP route directly contradicts Decision 6's own scope statement

**Evidence.** Decision 6's Scope paragraph (`round2-restatement.md:189-191`): *"The CI entrypoint
… omits the §2 trust assertions because it never makes them, not because it defends against them
being wrong."* §2's four listed trust assertions (`round2-restatement.md:84-89`) are, in order:
(1) `--dangerously-skip-permissions` — launcher, not entrypoint; (2)
`bypassPermissionsModeAccepted` write; (3) `hasTrustDialogAccepted` write; **(4) "sources
`/workspace/.claude/ccy/ccy.env` as shell … then execs `CCY_CLAUDE_WRAPPER` from it"**. Row 4 is
explicitly and repeatedly the row this plan treats as load-bearing — it is the entire reason §5
("Workspace mutation") calls `ccy.env` "an **input**, not just an output," and it is the reason
Decision 5 exists at all.

**The contradiction.** Task 4.1's "Declarative" MCP route (`phase45-mcp-and-egress.md:26-35`)
proposes an `MCP` declaration inside `.claude/ccy/ccy.env`, and states: *"`ccy.env` is the right
home for the declarative route because it already exists as the tracked per-project config seam
(`entrypoint.sh:269-274`) and is sourced in-container immediately before the exec."* That citation
— `entrypoint.sh:269-274` — is the **desktop** entrypoint's `ccy.env`-sourcing code, i.e. exactly
E10 row 4, the row Decision 6 says the CI entrypoint omits. If the CI entrypoint genuinely omits
row 4 (as Decision 6 states), it cannot also be the vehicle for Task 4.1's declarative MCP route,
because that route depends entirely on `ccy.env` being sourced in-container — which is precisely
the behaviour Decision 6 says does not exist there.

Two ways to resolve this, and the documents pick neither:

- If the CI entrypoint **does** source `ccy.env` after all (on the theory that Decision 5's
  trust declaration makes it safe, since the whole CI path is scoped to trusted automation), then
  Decision 6's Scope statement is wrong as written — it should say the CI entrypoint omits rows
  1–3 but keeps row 4, and should explain why keeping row 4 is fine under the trusted-only scope
  while rows 1–3 are not (this is not obvious: row 4 is the one E10 calls "the checked-out tree
  controls the command that runs", which is exactly as strong a trust assertion as rows 2–3, not
  weaker).
- If the CI entrypoint genuinely does **not** source `ccy.env` (matching Decision 6 as written),
  then Task 4.1's declarative route has no home for the CI path, and the plan's own Goals
  ("decide the shape [of MCP]… what becomes a flag, what becomes an image variant") is not
  actually answered for the use case that motivated Phase 4 in the first place — CI MCP wiring
  (the desktop `--mcp <name>` route is explicitly scoped desktop-only by Task 4.3, so it cannot
  cover this gap).

**What the plan must change.** State explicitly whether the CI entrypoint sources `ccy.env`, and
if it does, reconcile that with Decision 6's Scope paragraph (which currently reads as though it
does not). If it does not, Task 4.1 needs an actual CI-path MCP mechanism — most likely env-vars
passed by whichever caller launches the container (mirroring the consumer's own
`entrypoint.sh:35-43` "required environment" contract), not a `ccy.env` declaration.

---

## 4. MAJOR — Decision 5's "required declaration" is an unenforceable self-attestation, and the document does not say so

The brief asked directly: is Decision 5's "assert the trusted-only scope at the call site"
actually enforceable, or is it advice dressed as a control? It is the latter, and the plan's own
language elsewhere names this exact failure shape without applying it to itself.

**What Decision 5 specifies** (`round2-restatement.md:135-148`): one required flag, no default,
no inference from `GITHUB_EVENT_NAME`; its absence is a hard stop.

**What it does not do.** The flag has **no verification behind it**. Nothing checks that a
caller who passes `--trusted` (or whatever it is eventually named) is telling the truth. A
workflow author who copies boilerplate from a trusted job into a `pull_request_target`-triggered
job — the exact scenario that motivated the consumer's entire sandbox — gets **identical**
unrestricted behaviour to a genuinely trusted caller, because the flag's only effect is "must be
present to proceed," not "is checked against anything." This is precisely the shape Task 4.2
correctly rejects for the consumer's tool-matrix: *"A control that fires without discriminating…
is worse than absent, because its presence invites reliance"* (`phase45-mcp-and-egress.md:96`).
Decision 5's flag fires (it gates startup) without discriminating (it verifies nothing about the
claim it requires). The document does not apply its own stated principle to itself.

**The "no inference from `GITHUB_EVENT_NAME`" reasoning over-reaches.** The no-armed-flags rule
this plan correctly invokes (`.claude/rules/no-armed-flags.md`) bans **deriving** a go/no-go
decision from environment state instead of an operator's declared intent — it does not ban using
environment state as a **secondary sanity check** against an already-required, already-explicit
declaration. A caller who passes `--trusted` on an event that is structurally
`pull_request_target` from a fork is not a case where inference would replace the required
declaration; it is a case where the declaration and the observable environment disagree, and
warning or refusing on that disagreement is *additional* defense-in-depth, not the banned shape.
Decision 5 forecloses this option by treating "no inference" as absolute, without distinguishing
"derive the decision" (banned) from "cross-check the decision" (not banned, and cheap).

**What the plan must change.** State plainly, in the document itself, that the trust declaration
is an unverified attestation — its only enforcement is "the caller must have typed the word." If
that residual is accepted (which may well be the right call, given `ccy`'s existing trust model is
built the same way for the desktop case), say so explicitly rather than presenting the flag as
though requiring it closes the gap C1 opened. Separately, reconsider whether a `GITHUB_EVENT_NAME`
cross-check belongs as an optional, non-authoritative warning — it is not the banned inference
shape once the explicit flag is still the sole authority.

---

## What holds up

Kept short, and only for things independently re-verified in this session:

- **Both claimed Round-2 corrections are correct.** `claude-yolo:full` is a Dockerfile *stage*
  name (`Dockerfile:231`), never a tag; the built/published tag is `claude-yolo:latest`
  (`claude-yolo:107`, `common.bash:564-565`) — confirmed zero matches for `claude-yolo:full`
  anywhere outside this plan folder. `play-claude-yolo.yml:338-343` builds exactly one tag
  (`claude-yolo:latest`) with no `--target`, bypassing `build_container_with_hash`'s
  `base_image_name` parameter entirely (`common.bash:537-570`) — confirmed `claude-yolo:base` is
  produced only by a launcher-triggered build (`claude-yolo:1431`/`:1438`, gated by
  `validate_container_version` at `:1436`), exactly as claimed.
- **R11's `--no-network` finding is correct and precisely cited.** `claude-yolo:2514-2517`:
  when `NO_NETWORK_MODE` is true, the `elif` that would set `NETWORK_FLAG="--network podman"`
  is skipped, so `NETWORK_FLAG` stays empty and no `--network` argument reaches `podman run`
  at `:2772` — podman's own default network stack applies. Confirmed no `--network none` exists
  anywhere in the tree. This is a real, previously-unflagged safety-naming defect, correctly
  found.
- **E10's four citations all check out exactly** against `entrypoint.sh` as shipped:
  `claude-yolo:2792` (unconditional `--dangerously-skip-permissions`), `entrypoint.sh:240-252`
  (`bypassPermissionsModeAccepted`), `:257-263` (`hasTrustDialogAccepted`), `:269-274`/`:280-282`
  (`ccy.env` sourced then `CCY_CLAUDE_WRAPPER` exec'd). The nuance that `/root/.claude.json` is
  *not* caught by the `/root/.claude` symlink and is therefore re-created fresh every launch
  (not persisted) is also correct on inspection.
- **The `--entrypoint`/OCI-inheritance claim (§1.1) is solid.** `Dockerfile:215` sets
  `ENTRYPOINT` once in `base`; none of `Dockerfile.project-template`, `Dockerfile.example-golang`,
  `Dockerfile.example-ansible` declares `ENTRYPOINT` or `CMD` — confirmed zero occurrences in all
  three — so a project image built "the normal way" does inherit the desktop entrypoint, exactly
  as claimed, and the `Dockerfile.project-template:133` "for TOOLS, not for application code"
  citation is exact.
- **Phase 5's egress mechanism citations into the consumer's repo are all accurate**: `pasta: -T,3128` (`policy/egress.sh:46-47`), the V8/V9 measurement rationale, and the mutual-exclusivity
  finding (`pasta-loopback-forward-probe.sh:42`) all check out verbatim against source. The
  `require_flag`/`--strict-mcp-config`/`--permission-mode default` citations into the consumer's
  `entrypoint.sh` (`:114-115`, `:122-137`, `:210`, `:213-214`, `:231-236`) are likewise exact.
- **Task 3.3's version-gate analysis (Option C, image LABEL) is a sound fix** for the residual it
  names, and correctly identifies that the current staleness cache
  (`$HOME/.cache/claude-yolo-*-dockerfile-hash`, `claude-yolo:1454`) is host-user-local — a real,
  independently-verifiable defect.
