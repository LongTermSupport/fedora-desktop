# Plan 00068 — Task 7.4: the capabilities Round 1 surfaced, and Task 7.5's reordering

Six items, each raised by a Round-1 finding. All six re-verified against source for this document.
Two of them turned out to be more precisely stated than the correction that raised them, and one
carries a trap that will make the next reader conclude it is already solved.

---

## 1. Token from environment (C5) — and the trap in the evidence

**C5 holds.** There is no path that *accepts* a token by value.

The trap: `CLAUDE_CODE_OAUTH_TOKEN` appears **twelve** times in the launcher and libs. A reader
who greps for it will conclude the capability exists and that C5 was wrong. It is not, and the
distinction is the whole point:

| Direction                    | Exists? | Evidence                                                                |
| ---------------------------- | ------- | ----------------------------------------------------------------------- |
| ccy → container (**output**) | **yes** | `claude-yolo:2756` exports it; `:2777` passes it by name into `run`     |
| caller → ccy (**input**)     | **no**  | its value comes from `$CLAUDE_OAUTH_TOKEN`, set only at `:1033`/`:1135` |

Both of those assignments are `CLAUDE_OAUTH_TOKEN=$(cat "$SELECTED_TOKEN")` — read from a **file**
resolved by the token subsystem. `:926` is the empty initialiser. So the variable is an output
channel ccy populates, never an input channel a caller can fill.

**This is the recurring failure mode running backwards**, and worth recording explicitly: a true
statement about what a grep found ("`CLAUDE_CODE_OAUTH_TOKEN` is supported — twelve hits, one of
them on the `run` argv") would be a false statement about the world. Anyone who "fixes" C5 by
pointing at `:2777` has read the arrow the wrong way.

**Why this is the prerequisite, not one item of six.** Round 3 established that `select_token`
(`token-management.bash:611`) **spins** on EOF rather than aborting, because every call site guards
it with `||` (`claude-yolo:1004`, `:1117`), and that `create_token` beneath it is an irreducibly
human browser OAuth flow. Both sit on the default path when no token is pre-provisioned. So an
unattended launch does not fail — it hangs, on credential resolution, before anything else this
plan designs is reached.

**Specification.** A launcher input that takes the token **by value** from the environment and
**bypasses the token-file subsystem entirely** — not a new way into `select_token`, a way past it.
When supplied: no file glob, no selection UI, no `create_token`, and no `validate_token` API
round-trip on every launch (`claude-yolo:1057`), which is a startup-latency and egress cost CI
should not pay per job. Absence of both the env value and a resolvable token file is a **hard,
named failure**, never a prompt.

`--token NAME` is not this and must not be stretched into it: it selects among *existing files*.

---

## 2. Concurrency safety (C7) — confirmed verbatim

`lib/common.bash:583-595`: the name is `${project_name}_${suffix}`, and collision avoidance is a
`container_cmd ps -a` scan plus increment. **No lock.** Classic TOCTOU: two launches can read the
same `ps -a` and choose the same name.

`claude-yolo:2747`, unconditional and before launch:

```
if container_cmd rm -f "$CONTAINER_NAME" 2>&1 | grep -q "$CONTAINER_NAME"; then
```

`rm -f` force-removes a **running** container. Its comment explains it as an "external entity"
edge case — recovering a name held by corrupt storage after an unclean shutdown — which is a real
problem and a reasonable thing to want. But the remedy does not distinguish *a stale name* from
*a live sibling*, so a second same-project job reaps the first job's running container.

`PROJECT_NAME` derives from the directory, with no run identifier. The consumer salts explicitly
(`run-sandbox.sh:222`: `claude-${ACTION_CLASS}-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}`)
precisely because a retried attempt reuses the same checkout path.

**Specification.** Two independent changes, because they fix two different faults:

1. **Scope the `rm -f`** so it can only remove a container that is *not running*. The
   corrupt-storage case it was written for involves a name with no live container; that is exactly
   the case a state check preserves. Losing a live sibling is not a trade this safety net was
   meant to make.
2. **Admit a run identifier into the name.** Not inferred from `GITHUB_RUN_ID` — that is the
   derive-from-environment shape — but supplied by the caller, defaulting to today's behaviour so
   desktop use is byte-identical.

Fixing only (2) leaves the reaper armed for anyone who does not pass the identifier; fixing only
(1) leaves two concurrent jobs fighting over one name. Both.

---

## 3. `--no-network` for CI (C8) — the conclusion holds, the reasoning was incomplete

**Confirmed fatal.** The preflight runs a *separate* container — `container_cmd run --rm --network "$SELECTED_NETWORK" alpine wget -q -O- --timeout=10 http://google.com` (`claude-yolo:2529`) —
and its failure path ends in `exit 1` (`:2597`). Under a correctly-scoped allowlist neither the
`alpine` pull nor plain-HTTP `google.com` is reachable, so ccy hard-exits before the real container
starts, for a reason an operator would find bizarre. C8's "mandatory for CI" stands.

**But `--no-network` does not do what its name promises**, and a design that treats it as isolation
inherits a hole. At `:2514-2517` it only prevents `NETWORK_FLAG` being set; **no `--network`
argument is passed at all**, so podman's default network applies. There is no `--network none`
anywhere in the codebase.

So `--no-network` is precisely: *skip network auto-detection and skip the connectivity preflight*.
It is the right flag to pass for reason (b) and **no answer at all** to reason (a).

**Specification.** Rename to `--no-network-detect` with deprecation (Task 5.1), and make the
preflight conditional on its own terms rather than riding on a flag about detection — a probe that
exists to validate a *chosen* network should not run when egress is proxy-mediated, because it is
then measuring the wrong thing and failing on the right answer.

---

## 4. Compose teardown on failure (C10) — confirmed, and there *is* a trap

C10 said the teardown block is unreachable on non-zero exit. Confirmed, with one correction that
matters for the fix.

`set -e` at `claude-yolo:41`. The container runs unguarded at `:2770-2792`. The compose teardown is
top-level code at `:2794-2846`. A non-zero exit from the session — **including the ordinary CI case
of a task that legitimately fails** — terminates the script before `:2794`.

The correction: an `EXIT` trap **does** exist — `trap cleanup EXIT` (`:1716`), the only trap in the
tree. So the fix is not "add a trap". `cleanup()` (`:1695-1715`) does three things: restore the
terminal suspend character, `rm -rf "$CONFIG_TEMP"`, and print the debug log. **It does not touch
compose services.** The trap fires reliably and does not cover this.

That distinction is worth stating because "wrap it in a trap" would produce a *second* EXIT trap,
and in bash a second `trap … EXIT` **replaces** the first — silently discarding the temp-directory
cleanup and the stty restore. The fix is to move the teardown *into* `cleanup`, not beside it.

**Specification.** Move compose teardown into `cleanup()`, guarded by the existing
`CCY_COMPOSE_WAS_STARTED` flag (`:2795`), and make it non-interactive-safe: the current block
prompts, and a prompt in an exit path is a hang at the worst possible moment.

---

## 5. Image distribution (C6) — closed in Phase 3

Decided: **self-hosted only; registry support is declared out of scope, not silently absent.**

Grounds (full argument in `phase3-image-layering.md` Task 3.4): the runner is JIT-ephemeral
*registration* on a *persistent* VM (`RUNNER-VM-DESIGN.md` §7.4), so the image store survives jobs
and provision-time build is coherent — which is the objection C6 raised. And it is costless to say
so: searching `claude-yolo` and all seven libs for engine `push`/`pull` returns **zero** matches,
so there is no half-built registry path being left to rot.

If a genuinely ephemeral runner is ever wanted, this becomes new, declared scope.

---

## 6. Workspace mutation — decided by evidence, not preference

**A CI variant must not write session state into the job checkout.** The reason is not tidiness.

`entrypoint.sh:185` creates `/workspace/.claude/ccy`; `:195` symlinks `/root/.claude` to it, so
**all** Claude state lands in the checkout — `settings.json` (`:204-226`), the plugin
(`:230-237`) — and `claude-yolo:2613` writes launch config there on every launch.

The decisive part is that the same directory is an **input**: `entrypoint.sh:269-274` sources
`/workspace/.claude/ccy/ccy.env` as shell and `:280-282` `exec`s `CCY_CLAUDE_WRAPPER` from it. So
`.claude/ccy/` is read-write *and* execution-bearing. On a trusted checkout that is the feature. On
an untrusted one it is the whole attack surface, and it is why Decision 4's trusted-only scope and
this item are the same decision seen from two directions.

**Specification.** Under the CI path, session state is redirected to a container-local scratch
path; the checkout is not written. This is also where Phase 4's MCP config must go, for the same
reason (`phase45-mcp-and-egress.md`, Task 4.1).

---

## Task 7.5 — the reordering (C11)

C11 was right that the plan over-serialised. The corrected dependency graph, as executed:

- **Phases 3, 4 and 5 were designed without Phase 2**, which is the direct demonstration C11
  asked for. Nothing in image layering, MCP interface shape, or egress mechanism needed
  `--non-interactive` to exist.
- **Only *proving* things unattended is gated on Phase 2** — and specifically on item 1 above,
  since `select_token` spins before any other gate is reached.
- **Phase 5 can additionally be proven interactively on a workstation**, per Decision 3's own
  words, so its proof is not gated on Phase 2 either.

The revised order is therefore: **item 1 (token by value) → Phase 2 → unattended proof**, with
Phases 3/4/5 parallel to all of it. Item 1 moves ahead of Phase 2 rather than being one of its
sites: it is not a non-interactivity fix, it is the capability that makes the default path
survivable, and Phase 2 cannot demonstrate anything unattended until it exists.

---

## What Task 7.4 does not settle

- **Nothing here is implemented.** Design-only by explicit owner instruction; every "specification"
  above is a statement of required behaviour, not a change.
- **Item 3's preflight redesign touches desktop behaviour.** Making the probe conditional on
  something other than `--no-network` changes when desktop users see it. Not surveyed.
- **Item 2's run-identifier default is asserted, not measured.** "Defaulting to today's behaviour
  so desktop use is byte-identical" is the requirement; whether the name-collision loop at
  `common.bash:596-616` behaves identically under an empty identifier has not been traced line by
  line.

---

## ⚠ CORRECTIONS APPLIED AFTER THIS DOCUMENT WAS WRITTEN

This report is preserved as written (line numbers are cited by later review rounds). The
correction blocks at the head of [../PLAN.md](../PLAN.md) are AUTHORITATIVE where they differ.
Appended per **D9**, which found that none of the six reports carried any correction note.

- **C7, C8 and C10 are DESKTOP-ONLY** per D6/D7. C8's "`--no-network` mandatory for CI" framing is
  retracted twice over: `--no-network` does not isolate (R11), and the launcher is not on the CI
  path at all (D6). They remain worth doing on desktop merits.
