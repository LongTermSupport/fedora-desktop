# Plan 00066 — Hostile Review, Round 1 (fable)

Scope per the brief: attack the design, verify every citation against the actual
source (not the plan's word for it), hunt for "a true statement about a check
presented as a stronger statement about the world," and evaluate Decisions 1–3
and the ordering. All line numbers below were re-read directly from
`files/var/local/claude-yolo/{claude-yolo,entrypoint.sh,lib/*.bash}` and from
`actions-hub/.github/actions/run-claude-sandboxed/*` + `ccy-baseline/Dockerfile`
in this session — none are taken from PLAN.md's own citations without
independent verification.

---

## 1. BLOCKER — the plan never asks whether ccy's core invocation is even

compatible with the consumer's threat model: `claude-yolo` always runs with
`--dangerously-skip-permissions`; the consumer's whole design is the opposite posture

**Evidence.** `claude-yolo:2792` — the actual container invocation, unconditional,
gated by no flag anywhere in the script:

```
claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"
```

Confirmed by grep across the launcher tree: every occurrence of
`--dangerously-skip-permissions` in `claude-yolo` is either documentation/echo
text or this one hardcoded invocation — there is no `--permission-mode`,
`--allowedTools`, or `--disallowedTools` anywhere in `claude-yolo`,
`lib/*.bash`, or `entrypoint.sh`.

Contrast with the consumer's `entrypoint.sh:206-238`, whose own comment states
the design explicitly:

> "Note what is NOT here: no `--dangerously-skip-permissions`, no
> `bypassPermissions`, no repo-supplied flags of any kind... `--permission-mode default`: in headless mode a tool that is neither allowed nor denied is
> REFUSED."

And `tool-matrix.sh:21-34`, `policy/sandbox-overlay.Dockerfile`, and
`action.yml:11-37` build an entire allowlist-first, fail-closed permission
architecture around that one flag being *absent*.

**Why it matters.** This is not a missing flag among the four the plan
enumerates (non-interactive / image layering / MCP / egress) — it is the single
most consequential axis of the whole system, and the plan's evidence tables
(E1–E9) never mention it. `--dangerously-skip-permissions` exists precisely so a
trusted human doesn't have to approve every tool call; the consumer's actions run
attacker-controlled GitHub text (issue bodies, PR diffs, comments) through the
model specifically *because* prompt injection is assumed, so the model must be
made to fail closed on anything not explicitly granted. These are not two
configurations of one launcher tuned by flags — they are opposite security
postures. A "CI mode" that still hardcodes `--dangerously-skip-permissions`
(which is what happens today, unconditionally) is not merely "missing
non-interactivity" — it is unusable for exactly the threat model that motivated
`actions-hub`'s sandbox in the first place, no matter how well Phases 2–5 are
executed.

**What the plan must change.** Add this as its own finding (a proposed E10) and
its own Decision: does `ccy` grow a `--permission-mode`/`--allowedTools` surface
at all, and if so is that even a "CI capability" or a desktop-wide behavioural
change with its own hostile review? If the answer is "out of scope, the CI
variant still runs `--dangerously-skip-permissions` and relies on the container

- network boundary alone for containment" — that is a legitimate design choice,
  but it must be **stated**, because it directly contradicts the premise that
  extending `ccy` lets `actions-hub` delete its allowlist machinery
  (`tool-matrix.sh`, the MCP `--tools` narrowing, `permissions.allow` in the
  mirrored settings). Right now the plan is silent on the one flag that decides
  whether any of this is safe to point at a PR.

---

## 2. BLOCKER — Decision 1's premise is contradicted by the consumer's own,

already-shipped architecture: the launcher and entrypoint are not "missing a CI
flag", they are structurally discarded, on purpose, with the reasons written down

**Evidence.** `run-sandbox.sh:375-402` (comment attached to `--entrypoint /bin/bash`):

> "`claude-yolo` sets `ENTRYPOINT ["/usr/bin/tini","--","/usr/local/bin/entrypoint.sh"]`
> (Dockerfile:215)... A command placed after the image name does NOT override an
> ENTRYPOINT — podman appends it as ARGUMENTS to it. So the container actually ran
> `tini -- /usr/local/bin/entrypoint.sh /bin/bash /policy/entrypoint.sh` i.e. the
> image's INTERACTIVE session-prep script, which hard-fails on a missing
> `GH_TOKEN` (entrypoint.sh:14) and then runs `gh auth login`. The platform's
> entrypoint was never reached."

This is the consumer independently re-deriving, and citing by line number, the
exact same `entrypoint.sh:14-17`/`33` that this plan's own E7 cites — but the
consumer's conclusion is not "extend it", it is "override it entirely with
`--entrypoint /bin/bash /policy/entrypoint.sh`", running the consumer's own
**240-line** `entrypoint.sh` that does something categorically different: reads
the prompt from stdin (never argv), asserts the rootfs is mounted read-only
before doing anything (`entrypoint.sh:91-103`), writes an MCP config to a tmpfs,
and execs `claude --print --permission-mode default --strict-mcp-config --settings /policy/settings.json` — none of which resembles `claude-yolo`'s
entrypoint (symlink `/root/.claude → /workspace/.claude/ccy`, `gh auth login`
with a token, source `ccy.env`, LSP plugin install, `.claude.json` trust flag).

Further: `ccy-baseline/Dockerfile` and `resolve-image.sh` show the consumer took
**only the base image** from `ccy` (`FROM claude-yolo:latest`), and its own
header states why: "Every ccy image in the estate extends `claude-yolo:latest`...
Using the same base means the baseline and the per-repo images are the same
shape." Nothing in the consumer's ~1,737 lines reuses `claude-yolo`'s launcher
script or `entrypoint.sh` — the "re-implementation" the plan characterises as
wasteful duplication is, on inspection, a *replacement of the parts of ccy that
cannot be reused*, built next to a base image it **does** reuse.

**Why it matters.** Decision 1 frames the choice as "one launcher, orthogonal
flags, layered images" vs. "a forked `ccy-ci-runner` script", and picks the
former because a fork would "drift on every one of the 35 [sic] prompt sites,
the token path, the image-version gate, and the podman argv." But the consumer
never touches any of those things via `claude-yolo` at all — not even once. The
actual podman argv the consumer needs (`--read-only`, `--tmpfs` HOME/work/tmp,
`--pid/--ipc/--uts private`, checkout mounted read-only at `/srcrepo` not
`/workspace`, `.claude/` hidden under an empty tmpfs, `--entrypoint` override) is
not a flag's throw of `claude-yolo`'s existing argv (`claude-yolo:2770-2792`) —
it is a different container profile serving a different mount model
(`ccy`'s model: one writable project dir, bind-mounted whole, sessions persisted
into it via a symlink). No amount of `--non-interactive`/`--mcp`/`--egress` flags
converges these, because the incompatibility is in the *mount and entrypoint
contract*, not in interactivity, tooling, or network policy.

**Steelmanning the option the plan talks the owner out of.** The owner asked for
"a `ccy-ci-runner`, a special version of ccy." Read charitably, and in light of
what actually got built, that request was *already correct* about where the line
falls: the reusable, shared thing is the **image** (`claude-yolo:latest` as a
toolchain base, which the consumer already uses); the launcher and entrypoint are
legitimately two different artifacts because they serve two different trust
models (human at a TTY who explicitly wants permission checks disabled, vs. an
unattended process that must fail closed against attacker text). A `ccy-ci-runner`
*entrypoint* (not a forked 2,847-line launcher — just a second, small, CI-shaped
entrypoint/invocation, sharing the image) is arguably **exactly** what the
consumer built, just not living in this repo. The plan's Decision 1 rejects "a
second launcher" wholesale and never considers "a second, small entrypoint
sharing the same base image" as a distinct third option — which is what the
evidence says actually works today.

**What the plan must change.** Rewrite Decision 1 to scope "extend ccy" honestly:
the shared, reusable surface is the **image build** (Phase 3's `Dockerfile.ci`
idea is sound and should stay), not the launcher script or `entrypoint.sh`. State
explicitly whether Phases 2 (`--non-interactive`), 4 (MCP), and 5 (egress) are
being designed as flags on `claude-yolo` on the theory that a *future* variant of
the launcher will be invoked in CI in place of `run-sandbox.sh`'s direct `podman run` — and if so, justify that against the fact that the consumer's hostile
threat model (untrusted PR content) required discarding `--dangerously-skip-permissions`,
the writable `/workspace` mount, and the ccy entrypoint outright. If the honest
answer is "these flags are for other CI-like uses (a trusted, non-`actions-hub`
automation), not for replacing `run-sandbox.sh`", the plan should say that
plainly instead of using `actions-hub`'s deletion as its motivating, load-bearing
example throughout Context & Background and Dependencies.

---

## 3. MAJOR — E2's prompt-site count is wrong, and the citation method that

produced it has a blind spot on exactly the file most relevant to CI: credential
resolution

**Evidence.** Two independent greps across
`files/var/local/claude-yolo/{claude-yolo,lib/*.bash}`:

- `grep -rn -- 'read -rp\|read -p '` → **37** matches (not 35), spread across
  `claude-yolo`, `lib/docker-health.bash`, `lib/ssh-handling.bash`,
  `lib/network-management.bash`, `lib/dockerfile-custom.bash`.
- `grep -rn 'read -r '` (catching the space-separated `-r -p` form the first
  pattern cannot match) surfaces **9 more** prompts, all in
  `lib/token-management.bash`, none counted by either of the above:
  `171, 212, 324, 331, 346, 364, 407, 611, 826`.

So the actual count is **at least 46**, not 35, and the plan's own evidence
table (E2/E3) contains **zero** citations into `lib/token-management.bash`
(878 lines) even though `lib/token-management.bash:611` is:

```
611:        read -r -p "Select token [${prompt_hint}]: " selection
```

— the body of `select_token()`, which `claude-yolo:1004` calls **unconditionally
on the default launch path** whenever more than one token file exists (or the
existing one needs disambiguation):

```
1003:        # Multiple tokens exist or single token - show selection
1004:        select_token "$TOKEN_DIR" "container" || { ... }
```

This is not a recovery-only or `--debug`-only path — it is the ordinary,
first-run-of-the-day token resolution flow. Confirmed: `HEADLESS_MODE` is
referenced in exactly four places in the whole tree
(`claude-yolo:431,504,729,2626,2695` and `lib/ssh-handling.bash:357`), and
`select_token`/`create_token`/`validate_token` are not among them. A "headless"
CI launch with more than one token on the runner (or none) hangs here, on the
credential-resolution step — a harder, earlier, more certain blocker than any of
the sites the plan currently cites.

**Why it matters (this is exactly the recurring failure mode the brief asked me
to hunt for).** E2 states "35 interactive `read -rp` prompts across the launcher
and libs" as if it were an exhaustive census ("across... the libs" implies all
seven library files). It is not — it is exhaustive only for the specific string
pattern the search used, and that pattern silently excludes `read -r -p`
(two separate short flags rather than one bundled flag), which happens to be the
house style used consistently throughout `token-management.bash`
(`grep -c` in that file: 0 hits for `read -rp`, 9 hits for `read -r -p`). A true
statement about *what one grep pattern found* is being presented as a stronger
statement about *how many places ccy can hang* — the same shape as the
`--device`/exit-125 mistake the plan already caught and built Task 1.1 to avoid,
reproduced here in the evidence-gathering for E2/E3 itself.

**What the plan must change.** Task 1.2 ("Enumerate all 35 prompt sites") is
built on a wrong number and an incomplete file list. Before Task 1.2 executes,
re-run the census with a pattern that also matches `read -r -p` (or better,
match on `\bread\b.*-p\b` tokenised, or just grep `read.*-p ` case by case and
manually verify), across **all seven** `lib/*.bash` files plus `claude-yolo`
itself, and add `token-management.bash`'s 9 sites — especially `select_token`
at line 611 — to the evidence table. This also changes Decision 2's framing:
the very first "hard blocker" a CI job hits is not one of the recovery-path
menus E3 describes, it is ordinary token disambiguation on the credential path,
which arguably deserves its own named sub-problem rather than being folded
anonymously into "35 sites, split default-path vs error-path" in Task 1.2.

---

## 4. MAJOR — the recurring failure-mode ("a check standing in for a stronger

claim about the world") also appears inside Decision 2 itself: the plan's own
cited precedent for solving this problem already does the thing Decision 2 says
must never be done

**Evidence.** E2 cites `lib/ssh-handling.bash:357` as the one place
`HEADLESS_MODE` guards a prompt. Read in full, the guard is:

```
357: if [ "${HEADLESS_MODE:-false}" = "true" ] || [ ! -t 0 ]; then
358:     echo "  Non-interactive launch — enabling 443 automatically (the only way to proceed)."
```

That is, the **one existing precedent** in this codebase for "how do we make an
interactive prompt safe when there's no human to answer it" already uses `[ ! -t 0 ]` as a fallback trigger, OR'd with the explicit flag — precisely the
mechanism Decision 2 rules out ("never infer... [from] `-t 0`... how a CI hang
becomes unreproducible by hand"). The plan cites this line as evidence *for* the
existence of a guard, but never engages with *how* that guard actually works,
and so never notices that its own Decision 2 overrides existing, working,
already-merged precedent without saying so.

**Why it matters.** Decision 2's stated reasoning against `-t 0` inference
("makes behaviour depend on invocation context") is a real concern, but it is
asserted, not reconciled against the one place ccy already had to solve exactly
this problem and chose the opposite answer. Either the existing `ssh-handling.bash:357`
guard is itself a latent bug that should be fixed to use the flag only (in which
case say so — it's an existing behavioural change, not just new work), or there
is a reason `-t 0` was acceptable there (a narrow SSH-auth-fallback prompt with a
safe, printed default) that generalises to a rule less absolute than "never
infer" — e.g. "never *silently* infer; a printed line saying which branch was
taken, as line 358 already does, is fine." The plan should not leave a direct
contradiction with existing, shipped code unaddressed.

**What the plan must change.** Task 2.2 must explicitly reconcile Decision 2
with `ssh-handling.bash:357` — either flag that line for a follow-up fix (and
say why the new rule is stricter and correct) or soften Decision 2's "never
infer" into the precedent it is actually contradicting.

---

## 5. MAJOR — a required capability is entirely unnamed: the credential-acquisition

model, not just the prompts around it

**Evidence.** `claude-yolo`'s whole OAuth story (`claude-yolo:960-1235`,
`lib/token-management.bash` in full — 878 lines) is built around **named token
files** under `$TOKEN_DIR`, selected by a human (`select_token`), created via an
interactive wizard (`create_token`), and round-tripped against the live Claude
API on every launch (`validate_token`, `claude-yolo:1057`). There is no code
path anywhere in the tree for "take `CLAUDE_CODE_OAUTH_TOKEN` directly from the
process environment and skip file resolution/selection entirely."

Contrast: `run-sandbox.sh:64-74` —

```
if [[ -z "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]]; then
    die "CLAUDE_CODE_OAUTH_TOKEN is not set in the step environment..."
fi
...
printf '::add-mask::%s\n' "$CLAUDE_CODE_OAUTH_TOKEN"
```

No file, no selection UI, no `validate_token` API round-trip, no "create a
token now?" prompt — the token is a GitHub Actions secret injected straight into
the step environment.

**Why it matters.** The plan frames the CI gap as four capabilities
(non-interactive / image layering / MCP / egress) plus "reuse existing
extension seams" (E9) for the rest. But E9's seam list (`CCY_EXTRA_MOUNTS`,
`ccy.env`, `CCY_CLAUDE_WRAPPER`, `CCY_CONTAINER_ENGINE`, `CCY_AUTO_UPDATE`) does
not include anything that lets a caller skip `select_token`/`create_token`
entirely and hand `claude-yolo` a token by value. Even under a perfect
`--non-interactive` implementation (Phase 2), the *default*, description-in-plan
behaviour when zero or multiple tokens exist is still to run `select_token`,
which per Decision 2's own semantics would have to "fail fast, naming the flag
that would have answered it" — but no such flag currently exists (`--token NAME` selects among *existing files*; it does not accept a bare token value or
env var). This is a fifth, unnamed, and arguably more invasive requirement:
`claude-yolo` needs a "take the token from `$CLAUDE_CODE_OAUTH_TOKEN` and skip
the token-file subsystem altogether" mode, which touches ~200 lines of
`token-management.bash` that the plan's Goals/Non-Goals never mention.

**What the plan must change.** Add this as a fifth capability (or as an explicit
sub-task of Phase 2/Decision 2's "satisfied from a flag/env already" bucket) and
say so in Task 2.1's per-site outcome table: `select_token`/`create_token`'s
outcome under `--non-interactive` cannot be "fail-fast, use `--token NAME`"
unless `--token` (or a new flag) is taught to accept a token **value**, not just
a **stored file name** — which is new functionality, not a non-interactivity
fix.

---

## 6. MAJOR — concurrency and workspace-mutation are unexamined, and the

consumer's architecture shows they are real, already-solved-differently problems

**Evidence.**

- `claude-yolo:2613`: `save_launch_config ".claude/ccy" "$TOKEN_NAME_FOR_CONFIG" "$SSH_KEYS_STR" "$AUTO_CONNECT_NETWORK"` — writes launch state into the project's
  own `.claude/ccy/` on every single launch.
- `claude-yolo:2619`/`lib/common.bash:583`: `get_next_container_name "$PROJECT_NAME" "yolo"` derives the container name from the **project name only** — no run ID, no PID salting shown at the call site.
- `claude-yolo:2747`: a "safety net" that force-removes any pre-existing container of that name before launch (`container_cmd rm -f "$CONTAINER_NAME"`).
- Contrast, `run-sandbox.sh:222`: `container_name="claude-${ACTION_CLASS}-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"` — explicitly run-ID-qualified so concurrent jobs cannot collide, and the container writes to `--tmpfs`, never to the checkout.

**Why it matters.** A GitHub Actions runner is not guaranteed to run one job at a
time forever, and even a "one job at a time, ephemeral VM" runner still leaves
the question of what happens when two ccy-based CI steps in the *same* job (or a
retried job attempt) reuse the same project directory. `save_launch_config`
writing into `.claude/ccy/` inside what may be a **shared, checked-out, or even
untrusted** working tree is exactly the kind of workspace-mutation problem the
consumer's design treats as a first-class threat (`run-sandbox.sh:316-326`'s
read-only `/srcrepo` mount exists specifically so "the container cannot modify
the checkout the rest of the job then acts on"). `claude-yolo` mutates the
checkout by design (that is the whole point of the `.claude/ccy` symlink model
for a human's dev container) — the plan's Task 3.2 asks only "does an existing
`.claude/ccy/Dockerfile` still work", never "should a CI variant even be allowed
to write into the checkout at all, or does it need `claude-yolo`'s entire
session-state model redirected off the checkout and onto a scratch/tmpfs path."

**What the plan must change.** Add explicit tasks (Phase 3 or a new phase) for:
(a) container-name collision safety for concurrent/retried CI invocations, and
(b) whether a CI variant may write `.claude/ccy/` into the job's checkout at
all, given that on a PR job that checkout may be the untrusted tree the rest of
the plan already worries about hiding (`.claude/` under a PR head is exactly what
`actions-hub` hides with a tmpfs — E9/Phase 4 should at least flag the tension).

---

## 7. MINOR — E8 is accurate but incomplete: a second, unmentioned egress

touchpoint exists at container start

**Evidence.** `entrypoint.sh:111`:

```
github_meta=$(curl -sL --max-time 5 https://api.github.com/meta 2>/dev/null) || github_meta=""
```

This fetches GitHub's published SSH host keys for `known_hosts` pinning. It is
**not** fatal — a failure falls back to `StrictHostKeyChecking=accept-new`
(`entrypoint.sh:131-132`) — so it does not contradict E7's "GitHub must be
allowed or the container never starts" framing, but it is a second, distinct
egress touchpoint at boot that Task 5.3 ("state the minimum allowlist for a
container that merely boots") should enumerate alongside `gh auth login`/`gh auth status`, since a CI job using HTTPS-token git auth (no SSH keys at all)
would still hit this line if `SSH_KEY_PATHS` happens to be set, and a CI variant
that never sets SSH keys skips it — worth stating explicitly rather than leaving
implicit.

**What the plan must change.** Add `entrypoint.sh:111` to E7/E9's citation set
and to Task 5.3's minimum-allowlist enumeration, noting it is soft-failing
(non-fatal) unlike the `GH_TOKEN`/`gh auth login` requirement.

---

## 8. MINOR — ordering: Phase 2 is asserted, not shown, to gate Phases 4 and 5

as *designs*

**Evidence.** Decision 3 states egress "can be developed and proven **on the
workstation**, where iteration is cheap, before a runner exists" — i.e.
interactively, with a human present to answer any prompt that might fire. Phase
4 (MCP injection design) is a static question of interface shape and where a
config file is written relative to `entrypoint.sh:183-195`'s
`/root/.claude → /workspace/.claude/ccy` symlink; nothing about answering that
question requires `--non-interactive` to exist first.

**Why it matters.** The plan's own Risks table calls out "`--non-interactive`
scope creep... stalling everything behind it" as a risk, then Decision 2
declares Phase 2 "lands first because... no other CI capability can be verified
unattended" — conflating *designed* with *proven end-to-end unattended*. Only
the latter needs Phase 2 finished; the former does not, and Decision 3 already
half-admits this for egress specifically without drawing the general conclusion.

**What the plan must change.** Split the Tasks section's implicit ordering:
Phases 3 and 4 can be **designed** in parallel with Phase 2 (and Phase 5 can be
designed *and proven interactively* in parallel, per Decision 3's own words);
only the final "prove unattended in CI" gate (Task 6.5's "what a later
implementation plan must prove on real hardware") needs Phase 2 complete first.
As written, Tasks 3.x/4.x/5.x sit textually after Phase 2 with no stated
dependency edge, which reads as sequencing that the plan's own Decision 3
contradicts.

---

## WHAT THE PLAN GETS RIGHT

Kept short, and only where scrutiny survived:

- **E1, E5, E6, E7, E9's specific citations, as far as they go, check out
  exactly against source** (`--headless` semantics at 2626-2628/2694-2699/728-740;
  `--network`'s reversed meaning and the `google.com` probe at 2515-2529;
  the unconditional `--device /dev/dri` at 2773 vs. the guarded GUI mounts at
  2704-2727; `GH_TOKEN`/`gh auth login`/`gh auth status` at entrypoint.sh:14-17/33/53;
  `CCY_EXTRA_MOUNTS`/`ccy.env`/`CCY_CLAUDE_WRAPPER` at 1781-1790/265-274/280-284).
  They are simply not the whole picture (see §1, §3, §5 above).
- **Task 1.1's host-only triage discipline is sound methodology** and correctly
  refuses to let a nested-container result stand in for a host result — the
  exact discipline this review is asking the rest of the plan to apply to its
  own evidence-gathering.
- **The image-layering mechanism itself** (`Dockerfile.ci FROM claude-yolo:full`)
  is the one part of Decision 1 that survives: it matches what the consumer
  already does (`FROM claude-yolo:latest` in `ccy-baseline/Dockerfile`), so a
  shared toolchain-image layer is a real, already-validated win — it is the
  launcher/entrypoint-convergence framing built around it that does not hold.
