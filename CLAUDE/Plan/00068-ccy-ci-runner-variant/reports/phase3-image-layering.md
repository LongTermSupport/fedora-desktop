# Plan 00068 — Phase 3: image layering and the CI variant (Tasks 3.1–3.4)

Designed against the Round-2 restatement ([round2-restatement.md](round2-restatement.md)), whose
R9/R10 already constrain most of this: the build belongs at **provision** time, the build identity
belongs in an **image label**, and **registry support is out of scope**.

Two facts had to be established before anything could be specified, and both contradict text
currently in `PLAN.md`. They are stated first because the rest of the design depends on them.

---

## 0. Two corrections to the plan's own premises

### 0.1 `claude-yolo:full` does not exist

Task 3.1 says *"Specify `Dockerfile.ci` as a stage/file `FROM claude-yolo:full`"*, and Decision 1
says *"`FROM claude-yolo:full` → tag `claude-yolo:ci`"*. **There is no such tag.**

`full` is a Dockerfile **stage** name (`Dockerfile:231` — `FROM base AS full`), not an image tag.
Searching `files/`, `playbooks/` and `docs/` for `claude-yolo:full` returns **zero** matches; the
only occurrences anywhere in the repo are this plan's own `PLAN.md:335`/`:475`, the Round-1 review
that accepted the plan's wording, and this plan's `probe-engine.bash`, which probes for it as one
of three *candidates* precisely because it did not assume.

What actually happens (`lib/common.bash:537-565`): `build_container_with_hash` builds
`--target base -t <base_image_name>` first (`:555-562`), then builds the **final** stage — `full` —
and tags it with `$image_name`, which is `claude-yolo:latest` (`claude-yolo:107`). So:

| Stage in `Dockerfile` | Tag it is published under                |
| --------------------- | ---------------------------------------- |
| `base` (`:24`)        | `claude-yolo:base` (`claude-yolo:109`)   |
| `full` (`:231`)       | **`claude-yolo:latest`** — *not* `:full` |

**Consequence:** `Dockerfile.ci` must be `FROM claude-yolo:latest`, which is also what every
existing project Dockerfile already uses, and what the consumer's `ccy-baseline/Dockerfile` uses.
The correction is small but it is exactly the class of error this plan exists to catch: a
plausible name, repeated through a Decision, a Task and a review, none of which checked it.

### 0.2 Ansible never builds `claude-yolo:base`, but three documents offer it

`playbooks/imports/play-claude-yolo.yml:338-343` builds **one** tag:

```
{{ container_engine }} build --build-arg DOCKERFILE_HASH=… -t claude-yolo:latest /opt/claude-yolo
```

There is no `--target` anywhere in that play (grep: none). `claude-yolo:base` is produced *only*
by the launcher, and only when the launcher decides a build is needed — first-time
(`claude-yolo:1431`) or version/hash mismatch (`:1438`), both gated behind
`if ! validate_container_version …` (`:1436`).

On a box provisioned by Ansible, the image exists and its labels match the Dockerfile it was built
from, so `validate_container_version` returns 0, no launcher build runs, and **`claude-yolo:base`
is never created.**

Meanwhile it is offered as a supported choice in three project-facing places:
`Dockerfile.project-template:12` (a table cell recommending it for lean images), `:76`, `:81`;
`docs/CUSTOM-DOCKERFILES.txt:48,58,112,116`; and `Dockerfile:224`.

So a project following the documented "normal way" and choosing the lean base can hit
`claude-yolo:base: image not known` on a freshly-provisioned machine.

**Scoped honestly:** what is proven is the *code path* — `:base` is produced only on a
launcher-triggered build, and provisioning gives the launcher no reason to trigger one. Whether
any given box currently has `:base` depends on its rebuild history, and **that is itself the
defect**: the existence of a documented base image should not depend on whether you happened to
have rebuilt. Confirming it on a real host is one command and belongs in the Task 1.1 triage run:

```bash
podman image ls claude-yolo --format '{{.Repository}}:{{.Tag}}'
```

**This matters for Phase 3 beyond being a bug**: it is the precedent for how *not* to introduce
`claude-yolo:ci`. A tag that only some machines have, depending on history, is worse than no tag —
it fails for the next person and passes for you.

---

## Task 3.1 — `Dockerfile.ci`: what it adds, and what it must not

**Form.** A separate file, `files/var/local/claude-yolo/Dockerfile.ci`, `FROM claude-yolo:latest`
(§0.1) — **not** a fourth stage appended to the main `Dockerfile`.

Rationale for a separate file rather than a stage: the main `Dockerfile`'s hash is the input to
`DOCKERFILE_HASH` (`common.bash:552`) and to `validate_container_version` (`:466-469`), so every
edit to it invalidates the desktop image and forces a rebuild for every desktop user. `Dockerfile:38-40`
already records that this was painful enough to restructure the label ordering for (PERF-02). CI
tooling changing must not rebuild every developer's container.

**What it adds** — and the list is deliberately short:

| Addition                                    | Why it must be baked rather than fetched at run time                                                                                                |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| the MCP server binary, pinned by **sha256** | its release fetch needs egress the runtime allowlist deliberately excludes (R9); pinning by digest is what the consumer already does                |
| the **CI entrypoint** (Decision 6)          | §1.1 of the restatement — an entrypoint can only be added *in an image*, and callers hand-rolling the override is a defect with a proven recurrence |

**What it must NOT add**, each with the reason:

- **No credentials, no tokens, no `ARG`-passed secrets.** An image layer is not a secret store;
  `ARG` values are visible in image history.
- **No project toolchain.** That is the project Dockerfile's job — the owner's steer, and the
  whole point of keeping the seam intact. If `Dockerfile.ci` starts accumulating Go and PHP, it
  has become a second project image and the seam has been bypassed.
- **No egress or permission policy.** Both are runtime properties (restatement §3 table). Baking a
  proxy address or an allowlist into an image makes it un-reviewable at the point of use and
  wrong the moment the network changes.
- **No `ENTRYPOINT` override of the desktop entrypoint.** `Dockerfile.ci` *ships* the CI
  entrypoint as a file; it does not make it the default. Making it default would silently change
  behaviour for anything that pulls this tag expecting ccy semantics. Selection is the caller's,
  explicitly.

**Versioning.** `Dockerfile.ci` carries its own `LABEL claude-yolo-ci-version` and its own hash
label, gated independently of `REQUIRED_CONTAINER_VERSION` (see Task 3.3).

---

## Task 3.2 — how a project selects it, and the proof that existing Dockerfiles are unaffected

**Selection.** A project that wants the CI base writes `FROM claude-yolo:ci` in its own
`.claude/ccy/Dockerfile`. That is the whole interface — no new flag, no new resolution rule. It is
the seam the owner endorsed, used for a different base.

**Proof that an existing `.claude/ccy/Dockerfile` (`FROM claude-yolo:latest`) is unaffected**,
read from the resolution path rather than assumed:

1. The project image is tagged `claude-yolo:${PROJECT_NAME}` (`claude-yolo:1453`) — a namespace
   that cannot collide with `claude-yolo:ci` unless a project directory is literally named `ci`
   (see the residual below).
2. The rebuild decision (`claude-yolo:1487-1529`) reads exactly four inputs: image absent
   (`:1491`), project-Dockerfile md5 changed (`:1471`/`:1495`), **base version** changed
   (`:1478`/`:1499`), or `--rebuild` (`:1516`). Adding a new *tag* to the registry of images on the
   box changes none of the four.
3. `:1478` reads the base version from the literal string `"claude-yolo:latest"` — hard-coded, not
   derived from the project's `FROM` line. So publishing `claude-yolo:ci` cannot perturb the
   staleness calculation of a `latest`-based project image.

**Two residuals, stated rather than glossed:**

- **A project directory named `ci` would collide.** `PROJECT_IMAGE_NAME="claude-yolo:${PROJECT_NAME}"`
  (`:1453`) and `PROJECT_NAME` derives from the directory name, so a project called `ci` produces
  `claude-yolo:ci` and would overwrite the shared CI base. This is not new — the same collision
  already exists for a directory named `base` or `latest` — but introducing a third reserved tag
  makes it worth a guard: refuse to build a project image whose computed tag is a reserved name,
  naming the collision. Cheap, and it fails loudly instead of corrupting a shared image.
- **Point 3 is a *current* fact, not a guarantee.** `:1478`'s hard-coded `claude-yolo:latest`
  means a `ci`-based project image is staleness-checked against the **wrong** base — it will not
  rebuild when `claude-yolo:ci` moves, only when `claude-yolo:latest` does. That is a real gap
  introduced by this design and it is Task 3.3's problem, below.

---

## Task 3.3 — the platform-vs-repo layer, and the version-gate interaction

**The overlay question is settled by the owner steer** (restatement §0): the per-project
Dockerfile seam is the mechanism; no `ccy`-owned overlay is applied on top of a project's image
for the general case. The consumer's `policy/sandbox-overlay.Dockerfile` exists to enforce
platform invariants over an **untrusted** repo-controlled layer, and Decision 4 scopes `ccy` out
of the untrusted case entirely. Both sides of the argument Task 3.3 asked for therefore resolve
the same way *given Decision 4* — and that dependency is the honest statement: if Decision 4 were
ever reversed, the overlay comes straight back, because you cannot let an untrusted Dockerfile
define the layer that asserts the platform's own invariants.

### The version-gate interaction — the sub-item, and it is the one real design problem

Today there are two gates and they do different things:

| Gate                                            | Inspects                                                   | Cited                          |
| ----------------------------------------------- | ---------------------------------------------------------- | ------------------------------ |
| `REQUIRED_CONTAINER_VERSION` (`claude-yolo:39`) | `claude-yolo:latest`'s `claude-yolo-version` + hash labels | `:1436`; `common.bash:456-469` |
| project-image staleness                         | `claude-yolo:latest`'s version, vs a `$HOME/.cache` file   | `:1478`, `:1499`, `:1454`      |

Introducing `claude-yolo:ci` breaks the second one for `ci`-based projects, per the residual
above. Three options, and the third is the only one that survives:

- **(A) Extend `:1478` to read the base named in the project's `FROM` line.** Requires parsing the
  project Dockerfile. Rejected: parsing a Dockerfile with shell to find the effective base is a
  new fragile surface, and multi-stage project Dockerfiles make "the base" ambiguous.
- **(B) Gate `ci` on `REQUIRED_CONTAINER_VERSION` too.** Rejected: it couples CI tooling churn to
  the desktop rebuild cycle — the exact coupling Task 3.1 chose a separate file to avoid.
- **(C) Move the staleness identity into the image, as a label.** This is R10's item 2, and it
  fixes the version-gate problem as a side effect rather than by addition.

**Option C, stated.** Every ccy-built image — base, latest, ci, and every project image — carries
a label recording the digest of the Dockerfile it was built from, and the digest of *its* base.
Staleness then becomes: *does this image's recorded base-digest match the base image actually
present?* — answerable by `image inspect` alone, for any tag, with no `FROM`-parsing and no
`$HOME/.cache`.

That also removes a defect that exists today independent of CI: the staleness state currently
lives in `$HOME/.cache/claude-yolo-${PROJECT_NAME}-dockerfile-hash` (`claude-yolo:1454`), which is
**host-user-local**. Two users on one machine each believe different things about the same shared
image, and a provisioning user cannot answer the question at all for the user that runs jobs — the
precise question CI asks, and the reason `lts-infra` reimplemented the whole decision in Ansible
against a label (`tasks/runner-ccy-project-image.yml`).

---

## Task 3.4 — built by Ansible, never per-job

**The constraint, from the other side.** `lts-infra`'s `tasks/runner-ccy-project-image.yml:19-23`:

> "The image needs Debian archives, go.dev, the Go module proxy and pypi — deliberately absent
> from the runtime allowlist, so a job that tried to build would be refused by squid, correctly."

So a job that builds is not merely slow — it is *refused*, and correctly. Build time and job time
need different egress postures, which makes them different phases, not a tuning choice.

**The shape**, mirroring what already works for `claude-yolo:latest` (`play-claude-yolo.yml:338-343`):

1. Ansible builds `claude-yolo:latest` — today's task, unchanged.
2. Ansible builds `claude-yolo:ci` from `Dockerfile.ci`, tagged and labelled per Task 3.1.
3. Ansible builds the **project** image from the target repo's `.claude/ccy/Dockerfile` — the
   owner's "normal way", executed by the machine that will run the jobs.
4. Each build happens inside an **arm → build → drop** egress window, so the wide posture never
   becomes the steady state. `lts-infra`'s `runner-ccy-project-image.yml:136-243` is the working
   reference: template the proxy config with the build ACLs on, restart, build, and drop back in
   an `always:` block so the wide posture cannot outlive the window even if the build fails.
5. **Fix §0.2 while here.** Whatever adds step 2 must also make Ansible build `--target base`, or
   `claude-yolo:base` remains a documented tag that only exists on machines with the right history.
   Adding a third such tag while leaving the second broken would be indefensible.

### E8 — the daily npm auto-update, which Task 3.4 names and which resolves for free

E8 is the once-per-24h-per-image `npm i -g @anthropic-ai/claude-code@latest`
(`auto_update_claude_code`, `claude-yolo:1254`; `update_claude_inplace`, `:1343`). In CI it would
be non-deterministic *and* need npm-registry egress that the runtime allowlist excludes — so it
must not happen at job time.

**It does not, and no new switch is needed to prevent it.** The auto-update lives in the
**launcher**, and R9's split means the launcher does not run at job time — a job runs an
already-built image. So E8 simply never fires on the CI path. `CCY_AUTO_UPDATE=0` is not required
and should not be relied on: a job that reached the point of *needing* that variable would already
be running the launcher, which is the thing that is wrong.

This is worth stating explicitly rather than leaving as an implication, because it is the one
place where the by-time split pays a dividend instead of costing something — and because
"we set `CCY_AUTO_UPDATE=0` in CI" is the plausible-looking answer that would hide the fact that
the launcher was running at all.

At **provision** time the auto-update is harmless and arguably wanted: it runs inside the same
armed egress window as the build, and it is the mechanism that keeps a long-lived provisioned
image current between converges.

**Registry: out of scope, and now costless to say so.** C6 objected that "pre-built by Ansible" is
meaningless on a genuinely ephemeral runner. It is not meaningless here: the runner is
JIT-ephemeral *registration* on a *persistent* VM (`RUNNER-VM-DESIGN.md` §7.4), so the image store
survives jobs. And there is nothing to remove — searching `claude-yolo` and all seven libs for
engine `push`/`pull` returns **zero** matches. If a genuinely ephemeral runner is ever wanted,
registry support becomes new, declared scope; it is not a gap being quietly left.

---

## What Phase 3 does not settle

- **Phase 4 (MCP interface) and Phase 5 (egress) are untouched here.** Task 3.1 names the MCP
  binary as an image addition; *what config wires it up, and where that config is written* is
  Phase 4, and restatement §3.1 flags that `entrypoint.sh:195`'s symlink would land it in the
  checkout.
- **§0.2 is a code-path finding awaiting one host command.** The inference is strong and the
  remediation (step 5 above) is correct either way, but the claim "a provisioned box lacks
  `claude-yolo:base`" is about runtime state and should be confirmed, not assumed.
- **Nothing here has been built or run.** This plan remains design-only by explicit owner
  instruction.
