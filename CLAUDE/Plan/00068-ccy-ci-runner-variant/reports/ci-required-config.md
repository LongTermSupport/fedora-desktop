# Plan 00068 — Task 2.4: the fail-fast contract, asserted inside `ccy`

**This document previously specified a standalone CI preflight with 15 preconditions across three
layers. Task 3.7 retracted the standalone mechanism — CI invokes `ccy`.** Re-scoped here: every
assertion lives inside `ccy`, and most of them already do.

Every precondition is **asserted**, never summarised. No `<thing>_armed` boolean is derived from
whether a credential happens to exist; no `| default('')` is applied to a credential, because that
converts UNDEFINED into EMPTY and blinds the check.
See [.claude/rules/no-armed-flags.md](../../../.claude/rules/no-armed-flags.md).

Nothing here has been executed. The plan implements nothing.

---

## 1. The 15 preconditions, re-checked against what `ccy` already does

The plan's working rule: *before specifying a mechanism, name the existing thing it replaces and
state why that thing cannot do the job.* Applied to all 15, **four survive**.

### Layer VM

| #   | Precondition                             | What `ccy` already does                                                                                          | Verdict                |
| --- | ---------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------- |
| V1  | Container engine present and usable      | `common.bash:36-40` — absent ⇒ names the engine, names the fix, `exit 1`                                         | **already asserted**   |
| V2  | Base image present                       | `claude-yolo:1424-1433` — absent ⇒ builds it; no Dockerfile ⇒ `exit 1` naming `play-claude-yolo.yml` (`:1427-8`) | **already asserted**   |
| V3  | Base image carries `claude-yolo-version` | subsumed by V4 — an absent label reads as `"0"` (`common.bash:464`) and fails the match                          | **subsumed**           |
| V4  | Base version matches what ccy expects    | `validate_container_version` (`common.bash:456`), called at `claude-yolo:1436`; mismatch ⇒ **auto-rebuild**      | **already handled**    |
| V5  | CI entrypoint present on the VM          | there is no CI entrypoint — Task 3.6 retracted it                                                                | **moot**               |
| V6  | Egress infrastructure, when requested    | nothing yet                                                                                                      | **survives** — Phase 5 |

**V4 is the fifth instance of this plan's signature defect.** The retracted design specified a
version-match assertion, printing both values and aborting. `ccy` has done exactly that comparison
since before this plan existed — and does something *better* than asserting: it rebuilds. The
`⚠️ MUST MATCH REQUIRED_CONTAINER_VERSION` comment at `Dockerfile:36` is not an un-machine-checked
invariant; `common.bash:456-530` is the machine that checks it.

### Layer JOB

| #   | Precondition                          | What `ccy` already does                                                          | Verdict                        |
| --- | ------------------------------------- | -------------------------------------------------------------------------------- | ------------------------------ |
| J1  | Checkout is a real repo               | `claude-yolo:56` — `check_git_repo \|\| exit 1`; ccy runs in the checkout as cwd | **already asserted**           |
| J2  | Project image exists                  | `claude-yolo:1457-1528` builds and caches it from `.claude/ccy/Dockerfile`       | **already handled**            |
| J3  | Run identity for container naming     | nothing — `:2619` uses `get_next_container_name`, which has no lock              | **survives** — C7              |
| J4  | Mode declared (`--claude` / `--exec`) | ccy runs `claude` with args; there is no second mode to declare                  | **moot**                       |
| J5  | Claude credential available           | ccy's own token store — but **resolving it prompts**                             | **survives, re-scoped**        |
| J6  | Caller-declared required env vars     | `entrypoint.sh:14-17` is already GH_TOKEN-or-die                                 | **dropped** — YAGNI, no caller |
| J7  | Something to run                      | `claude-yolo:728-740` — `--headless` without `--prompt` ⇒ named error, `exit 1`  | **already asserted**           |

**J2 is the sixth instance.** It specified an assertion that the project image exists and was built
by an earlier phase. The project Dockerfile seam builds it *on the spot*, keyed on the Dockerfile
hash and the base version (`:1471`, `:1478`).

**J6 is dropped rather than moved.** A generic "declare the env vars your workload needs" mechanism
has no caller and no second use — the one credential a triage job needs beyond the Claude token is
`GH_TOKEN`, which the entrypoint already refuses to start without.

### Layer PROJECT

| #   | Precondition                     | Why it is gone                                                                                    |
| --- | -------------------------------- | ------------------------------------------------------------------------------------------------- |
| P1  | Image descends from a ccy base   | **moot** — `ccy` builds the project image `FROM` the base itself; descent is by construction      |
| P2  | `/usr/bin/tini` present in image | **moot** — existed only because the retracted runner set `--entrypoint`. `ccy` never overrides it |

**Survivors: V6, J3, J5** — and the thing the contract is actually about, the 46 prompt sites.

### The three-layer taxonomy survives; the tag does not

The layers are real — a broken engine is infra's problem, a missing flag is the workflow author's,
a broken `.claude/ccy/Dockerfile` is the project's. But a printed `[VM]`/`[JOB]`/`[PROJECT]` tag is
**dropped**: nothing consumes it, and ccy's existing messages already carry the same information as
a *concrete remediation* — `claude-yolo:1428` says "Please run the ansible playbook:
playbooks/imports/play-claude-yolo.yml", which tells the reader whose problem it is far better than
a three-letter tag.

**What the taxonomy becomes is an authoring rule**: every fail-fast message names a remediation, and
that remediation must be actionable by exactly one of infra / workflow author / project. If it
cannot be, the message is not finished.

---

## 2. The message format is not new

`claude-yolo:728-740` is the reference implementation and already ships:

```bash
print_error "--headless flag requires --prompt with content"
echo "" >&2
echo "Usage:" >&2
echo "  ccy --headless --prompt \"your prompt here\"" >&2
echo "" >&2
echo "The --headless flag runs Claude Code in non-interactive mode," >&2
echo "which requires a prompt to execute." >&2
exit 1
```

Shape: `print_error` headline → blank → **what to do** → **why** → exit. Everything on stderr.

Where a value is *wrong* rather than *absent*, the expected/found shape also already ships, at
`common.bash:503-516`:

```
Version:       $required_version (unchanged)
Image hash:    $image_hash
Current hash:  $current_hash
```

**Task 2.4 introduces no new format.** It makes these two the mandatory shapes: absent-value uses
the first, wrong-value adds the second.

### Secrets

- **Never print a credential value, prefix, suffix, or length.** Report `set` / `unset` /
  `set but empty` only. A length is a real if weak leak.
- **Pass credentials by env NAME, never by value.** Already ccy's deliberate pattern
  (`claude-yolo:2751-2757`, `:2777-2778`, BSH-09) — keeps the value out of argv and `/proc`.
- Token **expiry** is not a secret: it is in the filename (`token-management.bash:199`) and is
  exactly what a reader needs.

---

## 3. Exit codes

Per [.claude/rules/bash-standards.md](../../../.claude/rules/bash-standards.md) §9 — distinct
states get distinct codes, and `EX_USAGE` (64) is reserved so it can never be confused with a real
result.

| Code | Meaning                                          | Where                 |
| ---- | ------------------------------------------------ | --------------------- |
| 0    | success                                          | unchanged             |
| 1    | every failure `ccy` exits 1 for today (35 sites) | **unchanged**         |
| 64   | `EX_USAGE` — the invocation itself is wrong      | **new branches only** |
| 78   | `EX_CONFIG` — a precondition is unmet            | **new branches only** |

**No existing `exit` is renumbered.** Desktop behaviour stays byte-identical, because on the desktop
these conditions produce a *prompt*, not an exit — so no condition yields 1 on one path and 78 on
another.

**The cost, stated rather than buried:** a CI job can still receive a bare `exit 1` from a
pre-existing path with no structured cause. Renumbering those is a desktop behaviour change and is
the owner's call, not this task's.

The exit code is also the machine-readable channel that keeps GitHub-specific knowledge **out** of
`ccy`: a workflow step can map 78 → `::error::` annotation itself. `ccy` never learns what GitHub
Actions is.

---

## 4. The prompt contract

### 4.1 Mechanics — measured, not reasoned

Three facts had to hold. All three were executed:

1. **`exit` inside a function invoked as `f || { … }` terminates the whole shell** — the `||`
   handler does not run. Measured: script exited 78, handler silent. This is the exact shape of
   `select_token` at `claude-yolo:1004` and `:1117`.
2. **In command substitution `v=$(f)` the same `exit` is swallowed** — the outer script continues
   with `v` empty and exits 0.
3. **No prompt-bearing function is ever invoked in command substitution.** Checked across all 12
   owners; the search returned nothing. A checked absence, not an assumption.

(1) is what makes a *single* guarded primitive sufficient: a guard that `exit`s cannot spin,
whatever the caller's errexit state — so the five spin paths and the 32 undiagnosable aborts are one
problem, not two. (2)+(3) is why that holds *in this tree*, and (3) is a fact about today's code, so
Task 2.3's regression gate must assert it stays true.

### 4.2 The primitive

One function — `ccy_prompt` — in `common-pure.bash`, beside `print_error`.

**Existing thing it replaces:** 46 bare `read` calls, one of which
(`ssh-handling.bash:357-362`) already does the right thing. **Why that cannot do the job:** the
correct guard is applied **once in 46 opportunities**, and hand-repeating it 46 times must be
repeated again for every prompt added later — which is precisely what Task 2.3's gate exists to
prevent.

- **Interactive** — byte-identical to `read -rp` today.
- **Non-interactive** — one of exactly two declared outcomes:

| Outcome     | Declaration                          | Behaviour                                                                       |
| ----------- | ------------------------------------ | ------------------------------------------------------------------------------- |
| **default** | `ccy_prompt --default Y --why "…"`   | announce on stderr *what* was inferred and *why* it is safe; return the default |
| **fail**    | `ccy_prompt --answered-by '--token'` | print the contract message naming the flag that answers it; `exit 78`           |

There is no third outcome. A prompt with neither a declared default nor a named flag is a **QA
failure**, not a runtime default — that is the ratchet in Task 2.3.

The `--why` text is mandatory on a default. This is the revised Decision 2 in force: *never
**silently** infer*. `ssh-handling.bash:358` is the model — it states the inference **and** that it
is the only way to proceed.

### 4.3 Site-by-site — 46 sites, 12 owners

Sites and owners computed by `analysis/fnmap.py`; totals reconcile with the 46 of
[prompt-census-round2.md](prompt-census-round2.md).

**The flag that answers a prompt almost always already exists** (`claude-yolo:135-169`): `--token`,
`--ssh-key`, `--no-ssh`, `--network`, `--no-network`, `--disable-custom-docker`, `--prompt`. The
contract is mostly *wiring existing flags to existing prompts*, not adding flags.

#### (a) Already correct — 1 site

| Site                    | Owner                           | Outcome                                         |
| ----------------------- | ------------------------------- | ----------------------------------------------- |
| `ssh-handling.bash:362` | `build_ssh_mounts_and_validate` | default `true`, announced at `:358`. No change. |

#### (b) The default is already written in the source, and is right for CI — 4 sites

Each is `while true` → `read` → `${var:-DEFAULT}` on the **next line**. On EOF the `read` leaves the
variable empty and the existing `:-DEFAULT` supplies the answer — these abort today only because
`set -e` kills the script *at the `read`*, before that line runs. The contract does not invent a
default; it declares the one already in the source so it can be announced.

| Site               | Default | Set at  | Meaning                          |
| ------------------ | ------- | ------- | -------------------------------- |
| `claude-yolo:1854` | `Y`     | `:1855` | continue without network         |
| `claude-yolo:2227` | `Y`     | `:2228` | continue without network         |
| `claude-yolo:2428` | `N`     | `:2429` | do **not** start compose         |
| `claude-yolo:2818` | `Y`     | `:2819` | stop compose after the run (C10) |

#### (c) The source default exists but is WRONG for CI — 4 sites

**The interactive default is evidence of intent, not automatically the CI answer.** These four were
chosen for desktop convenience and each violates the owner's explicit steer that CI must be *more*
restricted. They must be declared explicitly to the restrictive value, and the divergence is
recorded here so it is not read later as an oversight.

| Site               | Source default | Set at  | Why it is wrong for CI                                                                                                                                     |
| ------------------ | -------------- | ------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `claude-yolo:822`  | `Y` reuse      | `:823`  | reuses the previous launch's token/ssh/network — a job would inherit `$HOME` state from whatever ran before, so the run stops being determined by its args |
| `claude-yolo:2091` | `Y` start      | `:2092` | starts compose services unasked; also contradicts `:2428`, the other compose prompt, which defaults `N`                                                    |
| `claude-yolo:2266` | `Y` connect    | `:2267` | auto-joins a discovered container network — the opposite of a restricted egress posture                                                                    |
| `claude-yolo:2301` | `Y` connect    | `:2302` | same, on the multi-match path                                                                                                                              |

#### (d) Structurally safe when unanswered — 2 sites

| Site                         | Owner                      | Why unanswered is safe                                       |
| ---------------------------- | -------------------------- | ------------------------------------------------------------ |
| `claude-yolo:78`             | migration                  | `case … *)` at `:92` — anything unrecognised keeps the files |
| `dockerfile-custom.bash:763` | `create_dockerfile_guided` | "Press Enter to continue" — no value is consumed             |

#### (e) Must fail fast — 35 sites

| Concern                 | Sites                                                                                                                                         | Named in the message                                     |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------- |
| Credential resolution   | `claude-yolo:968 :992 :1013 :1104 :1121`; `token-management.bash:171 :212 :324 :331 :346 :364 :407` (`create_token`), `:611` (`select_token`) | `--token NAME`; `ccy --create-token`                     |
| Token export            | `token-management.bash:826`                                                                                                                   | `--export-token NAME` (the non-interactive form)         |
| Network selection       | `claude-yolo:2334`; `network-management.bash:158 :271 :274`                                                                                   | `--network NETWORK` / `--no-network`                     |
| SSH key selection       | `ssh-handling.bash:230 :277`                                                                                                                  | `--ssh-key PATH` / `--no-ssh`                            |
| Engine/network recovery | `claude-yolo:2011 :2030`                                                                                                                      | no flag exists — a human must resolve it; say so         |
| Dockerfile authoring    | `dockerfile-custom.bash:37 :117 :157 :718`                                                                                                    | these modes *are* interactive; `--disable-custom-docker` |
| Debug layer selection   | `claude-yolo:662 :700 :1752`                                                                                                                  | `--debug` is documented interactive-only (`:137`)        |
| Container manager       | `docker-health.bash:369` (`show_container_top`)                                                                                               | `--top` exists only to be watched by a human             |

**Credential resolution is the load-bearing one** — it is the earliest blocker on the default launch
path, and `create_token` is an irreducibly human browser OAuth flow that no flag can make unattended.
The assertion:

> Under `--non-interactive`, exactly one of these must hold: `--token NAME` names a **valid**
> token in the store; **or** the store holds exactly one valid token. Otherwise: `exit 78`, naming
> `--token`, listing the token names and expiry dates found (never contents), and stating that
> `ccy --create-token` is a browser OAuth flow that cannot be run unattended.

The "exactly one valid token" case is a **default, announced** — it is the only choice available,
which is the same justification `ssh-handling.bash:358` uses.

**This supersedes Phase 2's outcome (i).** [phase2-non-interactive.md](phase2-non-interactive.md)
said credential resolution would be *removed* from the unattended path by Task 7.4's
"token by value". That followed from CI not being a fedora-desktop VM. It is one, ccy's store is
already there (`play-claude-yolo.yml:311-327`), so the branch is **guarded, not removed**, and
token-by-value is no longer load-bearing for CI.

#### (f) Safe default, announced — the 5 spin paths, minus credentials

| Owner                                                     | Sites       | Default      | Why it is safe                                                                |
| --------------------------------------------------------- | ----------- | ------------ | ----------------------------------------------------------------------------- |
| `show_zombie_container_tui` (`docker-health.bash`)        | `:162 :182` | do nothing   | under J3 the container name is run-scoped, so a foreign zombie cannot collide |
| `check_project_containers_startup` (`docker-health.bash`) | `:486 :509` | do not start | a CI job declares its own services; starting found ones is unasked-for state  |
| `_do_compose_start` (`network-management.bash`)           | `:546`      | do not start | same; a job that needs compose starts it before invoking `ccy`                |

No compose opt-in flag is specified. `podman-compose` is on the VM and the workflow can run it — a
new flag would be a mechanism with no caller.

---

## 5. The non-prompt assertions

### E6 — `--device /dev/dri` is unconditional

`claude-yolo:2773` passes `--device /dev/dri:/dev/dri` on every launch. Confirmed fatal on a
headless runner by the owner's host run: `EXIT 125 — stat /dev/…: no such file or directory`.

**Existing thing it replaces: nothing — this is a conditional, not an assertion.** The pattern to
copy already ships directly above it: `GUI_MOUNTS` at `:2703-2727` is conditional on the socket
existing and emits a `debug` line when it skips (`:2720`). E6's fix is the same shape, with the skip
announced rather than debug-only, per revised Decision 2.

### J3 / C7 — container naming

`claude-yolo:2619` names containers with `get_next_container_name`, which computes a free name from
`ps -a` plus an increment with **no lock**; `:2747` then unconditionally `rm -f`s that name, which
force-removes a **running** container. Two concurrent jobs on one repo have the second kill the
first.

Assertion under `--non-interactive`: a run-scoped unique name must be resolvable — from
`GITHUB_RUN_ID` + `GITHUB_RUN_ATTEMPT`, or from an explicit name argument. `get_next_container_name`
must never be reached on this path.

### V6 — egress

Phase 5 owns it. The assertion is conditional on a **declared mode**, never on whether some config
file happens to be present.

---

## 6. What the distributed contract loses, stated plainly

The retracted preflight ran every check, collected every failure, and printed them together — so a
three-mistake workflow cost one CI round-trip instead of three.

**A contract distributed through `ccy` cannot do that in general**, because each assertion sits at
the point where its value is first needed, and reaching the second requires the first to have
succeeded. Building a validation pass that checks everything up front means duplicating ccy's
resolution logic — which is the reinvention this plan has now made four times.

**What recovers most of the benefit without a second mechanism:** the three survivors (V6, J3, J5)
depend on nothing that runs before them and can be asserted together in one early block. Everything
else is inherently in-place. The residual cost — a second round-trip when an early failure hides a
later one — is real, and is the price of not building a parallel runner.

---

## 7. Banned in this contract

Each converts a misconfiguration into a silent behaviour change:

- `<thing>_armed` derived from a credential's presence, and any gate consuming it.
- `<thing>_required: false` opt-outs.
- `| default('')` on a credential — turns UNDEFINED into EMPTY, and the guard stops seeing it.
- `2>/dev/null` or `|| true` to quiet a probe. Capture into a variable and report the reason.
- Warning and continuing. There is no advisory tier.
- A prompt with neither a declared default nor a named answering flag.

---

## 8. Unproven

| Claim                                                                              | Status                                                      |
| ---------------------------------------------------------------------------------- | ----------------------------------------------------------- |
| `exit` under suspended errexit terminates the shell                                | **measured** (synthetic reproduction of the call shape)     |
| No prompt-bearing function is invoked in `$( )`                                    | **measured** — search across all 12 owners returned nothing |
| 46 sites / 12 owners / the per-site mapping                                        | **measured** — `analysis/`, all invariants held             |
| E6 is fatal on a headless host                                                     | **confirmed** by the owner's host run                       |
| The `${var:-DEFAULT}` lines in (b)/(c) are reached on EOF once the read is guarded | not executed — inferred from the source, needs the host run |
| Which of these sites a given CI job actually reaches                               | **not statically decidable** — Task 2.3 states this limit   |
| Exit 78 surfaces usefully in a GitHub Actions log                                  | not measured                                                |
