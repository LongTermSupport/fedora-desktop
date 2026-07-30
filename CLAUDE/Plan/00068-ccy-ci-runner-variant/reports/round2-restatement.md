# Plan 00068 — Round 2: the restated thesis (Task 7.1)

Round 1 invalidated this plan's central claim, so Round 2 is a restatement rather than a polish.
It is written against two inputs: the C1–C11 corrections block in `PLAN.md`, and an owner steer
received after Round 3 which settles a question Task 3.3 had been told to argue both ways.

Every claim below carries a `file:line` citation. Citations into
`files/var/local/claude-yolo/` are given relative to that directory (`claude-yolo:2792`,
`entrypoint.sh:245`, `lib/common.bash:583`). All of them were re-read directly from source for
this document rather than carried over from `PLAN.md`'s own evidence table — Round 1 found two
citations that did not survive that treatment, so it is now the standing method.

---

## 0. The owner steer, and exactly how much it settles

> "allow each project to have its OWN ccy runner in the NORMAL WAY - dockerfile customisation,
> custom tooling etc etc - its perfect
> BUT - CI need safety and MCP etc - it needs either adhoc or full blown customisation"

**The first half settles Task 3.3.** That task asked whether a `ccy`-owned overlay applied *on
top of* a project's Dockerfile is warranted, and instructed me to argue both sides. The answer is
that the existing per-project seam is the mechanism and is not to be replaced. A project's CI
runner image is just a project image. The "mandatory platform overlay for the general case"
branch is dead; it survives only for the untrusted-checkout case, which §5 shows is a different
plan entirely.

**The second half is where the work is, and it does not resolve the way the first half does.**
The rest of this document is mostly about why.

---

## 1. `ccy` is three layers, and the steer is a statement about exactly one of them

Round 1's C2 established that the shared surface is "the image". That is right but too coarse to
design against, because one of the three layers *lives inside* the image while behaving like part
of the launcher.

| Layer          | What it is                                                     | Where it lives                               | Reachable from a project Dockerfile?  |
| -------------- | -------------------------------------------------------------- | -------------------------------------------- | ------------------------------------- |
| **Image**      | toolchain: apt/npm/pipx packages, LSP servers, `gh`, `yq`      | `Dockerfile`, `.claude/ccy/Dockerfile`       | **Yes — this is the seam, by design** |
| **Entrypoint** | in-container session prep: gh auth, symlinks, trust flags      | `entrypoint.sh`, `COPY`d at `Dockerfile:184` | **No — inherited, see §1.1**          |
| **Launcher**   | credential resolution, prompts, container argv, mounts, egress | `claude-yolo` + 7 libs, on the **host**      | **No — never enters the image**       |

The owner's "normal way" is an **image** mechanism. It therefore delivers **tooling**, which is
what it was built for and what it is genuinely excellent at. It cannot deliver anything that
lives in the other two rows, and safety lives entirely in the other two rows.

### 1.1 The sharp bit: the entrypoint is inside the image, so you cannot take one without the other

`Dockerfile:215` declares `ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/entrypoint.sh"]`,
once, in the `base` stage — inherited by `full` (`Dockerfile:231`). None of the three
project-facing templates declares `ENTRYPOINT` or `CMD`:
`Dockerfile.project-template`, `Dockerfile.example-golang`, `Dockerfile.example-ansible` — zero
occurrences in all three. Per OCI inheritance an image that declares no `ENTRYPOINT` runs its
base's, so a project image built the normal way runs the **desktop** entrypoint.

This is not inferred. It has been measured independently, twice, by two other codebases that hit
it in production:

- The consumer's `run-sandbox.sh:375-402` (quoted in `fable-review-1.md` §2): *"A command placed
  after the image name does NOT override an ENTRYPOINT — podman appends it as ARGUMENTS to it.
  So the container actually ran `tini -- /usr/local/bin/entrypoint.sh /bin/bash /policy/entrypoint.sh` … The platform's entrypoint was never reached."*
- `lts-infra`'s `tasks/runner-ccy-project-image.yml:287-300`, whose comment reads: *"`--entrypoint /bin/bash` IS LOAD-BEARING — do not remove it. … `.claude/ccy/Dockerfile` is a bare `FROM`, so
  it INHERITS that entrypoint. … Without this flag the command below becomes ARGUMENTS TO the
  entrypoint and the task dies on `ERROR: GH_TOKEN environment variable not set`"* — and that
  repo records the same defect being fixed in its CI workflow but *not* in its provisioning copy,
  so the play aborted mid-converge.

Three independent derivations of one fact is as settled as this plan gets. **A project that
takes the base image "in the normal way" also takes `GH_TOKEN`-or-die, `gh auth login`, the
`/root/.claude` → checkout symlink, and the trust flags — whether or not it wants them.**

---

## 2. E10 — the permission posture, and why it is a coherent design rather than an oversight

Round 1's C1 named `--dangerously-skip-permissions` as the missing axis and was right. Reading
the entrypoint in full makes the finding stronger and changes what should be done about it.

`ccy` asserts *"this workspace is trusted"* in **four** independent places, three of which C1
did not cite:

| #   | Citation                | What it does                                                                    |
| --- | ----------------------- | ------------------------------------------------------------------------------- |
| 1   | `claude-yolo:2792`      | `claude --dangerously-skip-permissions "${CLAUDE_CMD_ARGS[@]}"` — unconditional |
| 2   | `entrypoint.sh:240-252` | writes `"bypassPermissionsModeAccepted": true` into `/root/.claude.json`        |
| 3   | `entrypoint.sh:257-263` | sets `projects["/workspace"].hasTrustDialogAccepted = true`, unconditionally    |
| 4   | `entrypoint.sh:269-274` | **sources `/workspace/.claude/ccy/ccy.env` as shell**, inside the container     |

Row 4 is new to this plan and it is the one that decides §5. `ccy.env` is a file **in the
workspace**; it is sourced immediately before the exec, and `entrypoint.sh:280-282` then honours
`CCY_CLAUDE_WRAPPER` from it by `exec`-ing that command. So **the checked-out tree controls the
command that runs.** The code's own comment (`entrypoint.sh:266-268`) is careful and correct about
the property it claims — *"Sourced HERE, inside the container … never on the host — so a project
cannot execute code on the host via it"* — and that is exactly the right guarantee for the
desktop case. It is also precisely the guarantee that is insufficient when the workspace is a
pull request from a stranger.

Rows 2 and 3 have a nuance worth stating because it cuts the other way from what you would
expect: `/root/.claude.json` is **not** under `/root/.claude`, so it is *not* caught by the
symlink at `entrypoint.sh:195` and is **not** persisted into the checkout. It is re-created fresh
in every container (`entrypoint.sh:240`). The bypass pre-acceptance is therefore not sticky
state — it is re-asserted on every single launch, unconditionally.

**E10, stated: `ccy`'s permission posture is not a loose default that a flag could tighten. It is
a coherent, four-point, deliberately-built trust model whose premise is that the operator owns
the workspace.**

### Decision 4 — `ccy` does NOT grow a permission surface

Task 7.1 required that this be decided rather than left silent. It is decided: **no.**

- Adding `--permission-mode`/`--allowedTools` would put two opposite security postures in one
  artifact. C2 already found that the consumer's launcher and entrypoint diverge *because the
  trust models diverge*; re-converging them in one file re-creates the divergence inside a
  single 2,847-line script, where it is harder to see rather than easier.
- It would not be sufficient anyway. Rows 3 and 4 above are entrypoint behaviour, so a launcher
  flag leaves the trust-dialog suppression and the workspace-sourced `exec` in place. A partial
  fix that *looks* like a permission surface is worse than none, because it invites someone to
  rely on it — this repo's own recurring finding, and `lts-infra`'s
  `.claude/rules/bash-standards.md` §9 in one sentence.
- The estate already has the containment this buys, one trust boundary higher up. `lts-infra`'s
  `docs/RUNNER-VM-DESIGN.md` §6.4 proves the runner VM cannot reach the hypervisor, any other
  guest, or the PVE API, via five already-armed controls; §5.4 adds the L2 CONNECT-allowlist
  proxy and the uid-fenced nftables policy. That is a boundary that survives full guest-root
  compromise. A permission matrix inside the container does not.

**The consequence must be stated plainly, because it is the honest half:** this means `ccy` in CI
is for **trusted automation only** — the org's own code, on its own commits. It is *not* a
replacement for `run-sandbox.sh`, and this plan must stop using the consumer's deletion as its
motivating example. C1 said either answer was defensible but silence was not; this is the answer
and this is its price.

### Decision 5 — the trusted-only scope is asserted, not documented

A scope that lives only in a design document is a scope that the next caller violates without
knowing. Per `lts-infra`'s `.claude/rules/no-armed-flags.md` — a precondition is asserted, never
summarised — the CI path must **refuse to start** unless the caller has declared the checkout
trusted, and the declaration must be an operator's chosen intent (`_enabled`-shaped), never
derived from the environment (`_armed`-shaped, banned).

A launcher cannot tell a trusted checkout from an untrusted one. The *caller* can — a workflow
knows whether it is on `push` to its own branch or `pull_request_target` from a fork. So the
declaration belongs at the call site, and its absence is a hard stop with a message naming what
is missing. Concretely this is one required flag with no default, and no inference from
`GITHUB_EVENT_NAME` — inference would be exactly the "derive our own fate from the environment"
shape the rule bans.

### Decision 6 — a second, small CI entrypoint beside `entrypoint.sh`: **ADOPT**

Task 7.1 required this option be evaluated on the merits, because Decision 1 rejected "a second
launcher" wholesale and never considered it. It is the right shape, for a reason that only became
visible once §1.1 was established.

**For.**

- It solves the problem at the layer the problem is actually in. §1.1 shows a project image
  cannot escape the desktop entrypoint by any amount of Dockerfile customisation. A second
  entrypoint *in the image*, selected by `--entrypoint`, is the only fix that leaves the owner's
  endorsed mechanism intact.
- It is the fix for a defect that has now been made three times. Every caller today hand-rolls
  `--entrypoint /bin/bash`; the consumer got it wrong once (`run-sandbox.sh:375-402`) and
  `lts-infra` got it wrong once and then fixed only one of its two copies
  (`runner-ccy-project-image.yml:296-300`). Leaving the override to callers is how it happens a
  fourth time. A named, shipped entrypoint that does the right thing is the correction.
- It is cheap for the desktop. One small script in the image, referenced by nothing unless asked
  for. Contrast growing the 2,847-line launcher, where every addition is exercised by every
  desktop launch.
- It is proven. The consumer's 240-line entrypoint has been doing this in production.

**Against, and the honest answers.**

- *"Two entrypoints is two code paths, and this project's rule is fewest code paths."* The rule
  bans two paths through **one decision** — a gate that can silently take the wrong branch. These
  are two different **jobs**: prepare an interactive human session, versus exec one command and
  exit. Folding them into one entrypoint behind a mode flag is what would create the banned shape,
  and would put the §2 trust assertions behind a conditional, which is strictly worse than having
  them unconditionally present in a script whose name says who it is for.
- *"It puts CI concerns in the desktop image."* Only the ~small script. The heavy CI weight — MCP
  server binaries — is what a `Dockerfile.ci` layer is for, and stays out of the desktop image.
- *"The consumer already has one; why does it belong here?"* This is the real question, and the
  answer turns on scope. The consumer's entrypoint is a **fail-closed sandbox** for untrusted
  input; Decision 4 declined to build that here, so this is **not** a proposal to absorb it. What
  belongs here is its unglamorous half: a CI entrypoint whose whole job is *prepare nothing,
  assert nothing about trust, exec what you were told*. That is the piece every caller needs and
  every caller is currently reimplementing badly.

**Scope, so this is not read as more than it is.** The CI entrypoint is the trusted-automation
counterpart to the desktop entrypoint, not a security boundary. It omits the §2 trust assertions
because it never makes them, not because it defends against them being wrong.

---

## 3. "Ad-hoc or full-blown" resolves per capability, not globally

The steer offers two routes. Mapping the four capabilities onto them is the substance of Round 2,
and two of the four rows do not fit either route.

| Capability               | Ad-hoc (stock image + runtime config)                  | Full-blown (project Dockerfile)           | Verdict                                   |
| ------------------------ | ------------------------------------------------------ | ----------------------------------------- | ----------------------------------------- |
| **Tooling**              | partial — `CCY_EXTRA_MOUNTS` (`claude-yolo:1781-1790`) | **yes — already works, nothing to build** | The steer is right. This row is done.     |
| **MCP**                  | wiring is net-new                                      | binary can be baked; wiring still net-new | Both routes viable; **both need §4 work** |
| **Safety — permissions** | **no** (§2 rows 1–4)                                   | **no** (§1.1 — entrypoint is inherited)   | Neither route. Decision 4 declines it.    |
| **Safety — egress**      | plausible — a runtime property                         | **no** — cannot be baked into an image    | Ad-hoc only.                              |

The middle two rows are the finding. **Half of what the owner asked for is not reachable from the
mechanism the owner (correctly) endorses**, and saying so is better than stretching the steer to
cover it.

### 3.1 MCP remains net-new — E4 re-confirmed

Round 1's E4 claimed zero MCP support. Re-verified for this document by case-insensitive search
for `mcp` across `claude-yolo`, all 7 `lib/*.bash`, `entrypoint.sh`, and all four Dockerfiles:
**zero matches.** Wiring an MCP server is therefore new capability on either route. Baking the
*binary* into an image is the easy half and the route the steer endorses; writing the config the
running `claude` reads is the half that has no home yet, and `entrypoint.sh:195`'s
`/root/.claude` → `/workspace/.claude/ccy` symlink means anything written there lands **in the
checkout**, which §5 flags.

---

## 4. What the launcher is actually good for in CI — and it is not job time

The reusable parts of the launcher are the ones that **choose and build an image**, and CI must
not do that at job time. `tasks/runner-ccy-project-image.yml:19-23` in `lts-infra` states the
constraint from the other side, having hit it live:

> "The image needs Debian archives, go.dev, the Go module proxy and pypi — deliberately absent
> from the runtime allowlist, so a job that tried to build would be refused by squid, correctly."

So the split is by **time**, not by feature:

- **Provision time** (Ansible, on the runner VM, wide egress armed for that window only): build
  the project's ccy image from its own `.claude/ccy/Dockerfile`. This is exactly the owner's
  "normal way", executed by the machine that will run the jobs.
- **Job time**: run the already-built image. No build, no image resolution, no version gate, no
  registry.

This also closes **C6** for this estate. C6 objected that "pre-built by Ansible" is meaningless on
a genuinely ephemeral runner. The runner design here is **JIT-ephemeral registration on a
persistent VM** (`RUNNER-VM-DESIGN.md` §7.4: a JIT runner performs at most one job and is then
removed by GitHub) — the *registration* is per-job, the VM and its podman image store are not. So
provision-time build is coherent. **Registry support is therefore declared out of scope**, which
is the explicit decision C6 demanded; confirmed reachable because there is no registry code to
remove: searching `claude-yolo` and all 7 libs for `push`/`pull` against the container engine
returns **zero matches**.

### 4.1 The `--rebuild` logic already exists, and is being reimplemented elsewhere

`claude-yolo:1457-1529` is the project-image staleness gate: it tags the project image
`claude-yolo:${PROJECT_NAME}` (`:1453`), rebuilds when the image is absent (`:1491`), when the
project Dockerfile's md5 changed (`:1471`, `:1495`), when the base image version moved (`:1478`,
`:1499`), or on `--rebuild` (`:1516`).

`lts-infra`'s `tasks/runner-ccy-project-image.yml` reimplements that decision in Ansible —
sha256 instead of md5, an image **label** instead of a `~/.cache` file — and its own header calls
the situation out. `lts-infra` Plan 00026 Task 3.3 slates that file for deletion as a
duplicate — a premise D6 re-opened. **This plan must provide what the Ansible version has and
`ccy` does not — originally three items; D6 retracted item 1, leaving two:**

1. ~~**A non-interactive build mode** that resolves and builds the project image and then exits.~~
   **RETRACTED BY D6** — nothing invokes the launcher at provision time either; Ansible calls
   `podman build` directly. The list below is therefore **two** things, not three.
2. **A build identity readable from the image**, not from `$HOME/.cache`. `ccy` records staleness
   in `$HOME/.cache/claude-yolo-${PROJECT_NAME}-dockerfile-hash` (`claude-yolo:1454`) — state
   **outside the image**. CI answers the question from a checkout plus the image, and never has
   host-local cache state available at all *[corrected per D5]*. An OCI `LABEL` carrying the
   Dockerfile digest is the fix, and is what the Ansible version already does.
3. **A way to run a command in the image without the desktop entrypoint** — currently every
   caller hand-rolls `--entrypoint /bin/bash`, and §1.1 shows two codebases getting that wrong.

Item 2 is the load-bearing one and is a genuine defect independent of CI: **staleness state that
lives in one user's `$HOME` cannot answer "was this image built from this Dockerfile?" for anyone
else**, which is precisely the question CI asks.

---

## 5. Workspace mutation — the one place the steer's model actively fights CI

`entrypoint.sh` writes into the mounted workspace, unconditionally and by design:

- `:185` — `mkdir -p /workspace/.claude/ccy`
- `:195` — `ln -sf /workspace/.claude/ccy /root/.claude`, so **all** Claude state redirects there
- `:204-226` — writes `settings.json` (i.e. into the checkout)
- `:230-237` — installs the PHPantom plugin into the checkout
- `claude-yolo:2613` — `save_launch_config ".claude/ccy" …` writes launch state on every launch

For a developer's own repo this is the feature — sessions persist across ephemeral containers.
For CI it means the job **mutates the tree the rest of the job then acts on**, and a subsequent
`git status` is dirty for reasons the workflow did not cause. Task 7.4's open item ("decide
whether a CI variant may write `.claude/ccy/` into the job checkout at all") is answered by
§2 row 4 rather than by preference: since `ccy.env` is *read* from that same directory and
`exec`'d, the directory is an **input** as well as an output, and on an untrusted checkout it is
an attacker-controlled input. Redirecting session state off the checkout is therefore not
tidiness; it is the same decision as Decision 5, seen from the filesystem.

---

## 6. Corrections to Round 1 that this pass produced

- **C8 is confirmed and its "mandatory `--no-network`" conclusion is right, but the flag does not
  do what its name says.** The preflight pulls `alpine` and fetches plain `http://google.com`
  (`claude-yolo:2529`) and `exit 1`s on failure (`:2597`) — fatal, as C8 said. `--no-network`
  (`:501-502`) does skip it (`:2525`). **But `--no-network` does not isolate the container**: at
  `:2514-2517` it merely leaves `NETWORK_FLAG` empty, so *no* `--network` argument is passed and
  podman's default applies. There is no `--network none` anywhere in the codebase.
- **This makes Task 5.1's naming problem three-way, not two-way.** `--network` *widens* reach
  (`:1801-1812`), `--no-network` *does not narrow* it (`:2514-2517`), and a reader would
  reasonably expect the opposite of both. Task 5.1 was scoped to reconcile `--egress` against
  `--network`; it must now reconcile against `--no-network` too, which is the more dangerous of
  the two because its name is an explicit safety promise it does not keep.
- **C7 confirmed verbatim.** `lib/common.bash:583-595` computes the name from
  `${project_name}_${suffix}` by scanning `ps -a`, with no lock; `claude-yolo:2747` then runs
  `container_cmd rm -f "$CONTAINER_NAME"` unconditionally. Two concurrent same-project jobs can
  have the second force-remove the first's **running** container.
- **fable §7 confirmed.** `entrypoint.sh:111`'s `curl https://api.github.com/meta` is soft-failing
  (`:130-133` falls back to `StrictHostKeyChecking=accept-new`), unlike the hard `GH_TOKEN` and
  `gh auth login`/`gh auth status` requirements at `:14-17`, `:33-36`, `:53-56`.

---

## 7. What this document does NOT settle

Stated explicitly, because the failure mode this plan keeps catching is a partial result read as
a complete one.

- **Phases 3, 4 and 5 are not designed here.** This is Task 7.1 — the thesis and the two
  decisions Round 1 demanded. The interface specifications (`Dockerfile.ci` contents, the MCP
  interface, `--egress` mechanism and proof) remain open.
- **Nothing in `ccy` has been executed.** Every claim above is read from source or cited from
  another codebase's recorded live measurement. The spins classified in Round 3 remain unconfirmed
  end-to-end, and Task 1.1's remaining host-run items are still outstanding.
- **The `--entrypoint` claim is proven for the templates this repo ships**, plus OCI inheritance
  semantics, plus two independent live confirmations. It does not prove a project *could* not
  override `ENTRYPOINT` — only that nothing in the repo's guidance suggests doing so, and that the
  template states its scope as *"for TOOLS, not for application code"*
  (`Dockerfile.project-template:133`).
- **Decision 4 is a design decision, not a proof of safety.** It says a permission surface should
  not be built in `ccy`; it does not say the container-plus-network boundary is sufficient for any
  particular workload. That judgement is per-workload and belongs to whoever points a job at it.

---

## ⚠ CORRECTIONS APPLIED AFTER THIS DOCUMENT WAS WRITTEN

This report is preserved as written (line numbers are cited by later review rounds). The
correction blocks at the head of [../PLAN.md](../PLAN.md) are AUTHORITATIVE where they differ.
Appended per **D9**, which found that none of the six reports carried any correction note.

- **D6 retracts §4.1 item 1** (a non-interactive build-and-exit mode). Nothing invokes the
  `claude-yolo` launcher at provision time either — Ansible calls `podman build` directly — so
  **the launcher is never on the CI path at all**. §4.1 now reads as two items, not three.
- **D5 corrected inline at `:267`**: the "CI runs as a different user than provisioning" premise
  is false (`runner_user: "runner"` is the same identity for both). The conclusion — build
  identity belongs in an image `LABEL` — is unaffected; the correct argument is that state outside
  the image cannot travel with it or be read from a checkout.
- **Decisions 4, 5 and 6 stand.** D2 withdrew one citation supporting Decision 4 (the lts-infra
  containment proofs answer escape and destination, not the confused-deputy threat); the decision
  itself was not retracted.
