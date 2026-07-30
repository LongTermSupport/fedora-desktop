# Plan 00068 — Phase 2: `--non-interactive` (Tasks 2.1–2.3)

Phase 2 was written before Round 3 measured anything. Round 3's central finding — that a prompt's
verdict is a property of the **call graph**, not of the site — invalidates the shape of Task 2.1,
so this document restates the task before answering it.

Task 1.2's census is treated as discharged by
[prompt-classification-round3.md](prompt-classification-round3.md): 46 sites, each classified,
reproducibly, by tooling whose invariants can fail.

---

## Task 2.1 — the per-site outcome table is the wrong unit of work

Task 2.1 says: *"For every site from Task 1.2, specify which of the three outcomes (satisfy /
default+log / fail-fast) applies."*

**That cannot be answered per site.** Round 3 measured `create_token`'s seven prompts ABORTing when
reached bare from `claude-yolo` (`:897 :907 :970 :1000 :1015 :1111 :1123`) and SPINning when reached
via `select_token` (`token-management.bash:639`, `:656`), because `select_token`'s own call sites
guard it with `||` (`claude-yolo:1004`, `:1117`) and the suspension propagates transitively. Same
lines, two verdicts. Round 3 states the consequence directly: *"'fix each site' is not a
well-formed unit of work; the fix belongs at the entry points."*

So the deliverable is **entry-point decisions**, and there are far fewer than 46.

### The three outcomes, assigned by cause rather than by line

| Outcome                      | Where it applies                                        | Count reached  |
| ---------------------------- | ------------------------------------------------------- | -------------- |
| **(i) satisfied from input** | credential resolution — `select_token` + `create_token` | 7 sites        |
| **(ii) default + announce**  | a genuine safe default exists (the `:362` pattern)      | 3 entry points |
| **(iii) fail fast, named**   | everything else                                         | the remainder  |

**(i) — credential resolution is removed, not guarded.** Task 7.4 item 1 (token by value) deletes
this branch from the unattended path entirely: no glob, no selection, no `create_token`. This is
why it is a prerequisite rather than one of 46 fixes — it is also the *earliest* blocker, and
`create_token` is an irreducibly human browser OAuth flow that no flag can make unattended.

**(ii) — the pattern already exists and is used once in 46 opportunities.**
`ssh-handling.bash:362`:

```bash
if [ "${HEADLESS_MODE:-false}" = "true" ] || [ ! -t 0 ]; then
    echo "  Non-interactive launch — enabling 443 automatically (the only way to proceed)."
    enable_443=true
else
    read -rp "Enable GitHub SSH over 443 for this session? [Y/n] " reply_443
```

It detects non-interactivity, **announces the inference**, states why it is the only option, and
proceeds. Round 3: *"the fix for the other 45 is not to invent a mechanism, it is to apply this
one."* Applies to the three container/compose entry points where "do not start it" is a safe,
statable default: `show_zombie_container_tui`, `check_project_containers_startup`,
`_do_compose_start`.

**(iii) — fail fast, naming the flag that would have answered it.** Everything else. The message
must name the *flag*, not the prompt: a CI operator cannot act on "the launcher asked something".

### The mechanism: guard entry points, not 46 `read`s

A spin needs **both** a suspended call context **and** an unbounded loop with no EOF exit
(Round 3). Removing either breaks it. Adding EOF checks to 46 individual `read`s is therefore both
more work and less reliable than guarding the five entry points — and a per-`read` fix would have
to be repeated for every prompt added later, which is what Task 2.3 exists to prevent.

### Decision 2 revised: "never infer" → "never **silently** infer"

C9 was right and this is where it lands. Decision 2's absolute *"never infer... from `-t 0`"* was
dogma, and it contradicted shipped, working code (`ssh-handling.bash:362`) without saying so.

**Revised rule:** an inference is permitted when it is **announced on stderr**, states **what** was
inferred and **why**, and takes a default that is **safe** and **statable**. A silent inference
remains banned. `ssh-handling.bash:362` is the reference implementation — it is not a latent bug to
be fixed, it is the pattern to be copied.

*(This also discharges Task 7.3's "soften the design text" sub-item.)*

---

## Task 2.2 — interaction with `--headless` and `--prompt`

`--headless` is a **Claude Code invocation** mode: `claude -p "$PROMPT"` (`claude-yolo:2626-2628`),
`-i` instead of `-it` (`:2694-2699`), and it requires `--prompt` (`:728-740`). `--non-interactive`
is a **launcher** mode. They are orthogonal axes and neither is a superset of the other.

**Does `--non-interactive` imply `--headless`?** **No.** A scripted launch that answers no launcher
prompts but then hands a live TTY to a human is coherent and useful. Implying `--headless` would
force `--prompt` (`:728-740`) on a caller who does not want it.

**Does `--headless` imply `--non-interactive`?** **Yes — announced.** `--headless` already declares
that no human is driving Claude Code; a launcher that then blocks on a prompt contradicts the
declaration the caller just made. Under the revised Decision 2 this is a legitimate inference
because it is announced, not silent.

**The cost, stated rather than buried.** This is a behaviour change for existing `--headless` users
at a TTY: a launcher prompt they would previously have answered now takes a default or fails fast.
Round 1's C4 established that such users exist (its whole correction rested on `set -e` behaviour
at an interactive terminal). The announcement makes the change visible on the run where it first
bites, which is the mitigation; it is not a reason to call the change free.

**Rejected alternative:** leaving them fully orthogonal and merely warning. That preserves today's
hang-or-abort behaviour under `--headless`, which is the exact defect Phase 2 exists to remove, in
the one mode whose name most strongly implies it was already fixed.

---

## Task 2.3 — the regression guard, and whether it is statically decidable

Task 2.3 asked for a QA gate that fails when a `read -rp` exists on a path reachable under
`--non-interactive` without a guard, and told me to *"note honestly whether this is statically
decidable, and if only partially, what the residual risk is."*

### It is partially decidable, and the tooling already exists

`analysis/classify-prompts.bash` (172 lines) plus `analysis/bashctx.py` (74) and
`analysis/fnmap.py` (151) already compute exactly this: prompt sites in any flag order, function
boundaries validated by re-parsing each extracted body with `bash -n`, block nesting asserted
balanced at EOF, and the call graph classified and propagated transitively. It exits **1** when any
invariant fails, with the header stating *"the classification is NOT trustworthy, do not quote
it"* — and it was mutation-tested in both directions.

It also already carries the Task 7.2 lesson as an assertion (`:83-84`): the census pattern is
checked against a file **known** to use `read -r -p`, so a pattern matching nothing can never again
report a clean sweep. That is the single most important property a gate of this kind can have, and
it is there because the census failed exactly that way once.

### What is decidable, and what is not

| Question                                                       | Decidable?                       |
| -------------------------------------------------------------- | -------------------------------- |
| Is this a prompt site?                                         | **yes**                          |
| Which function encloses it?                                    | **yes** — validated by re-parse  |
| Is its enclosing function reachable from a suspending context? | **yes** — call graph, transitive |
| Is it inside an unbounded loop with no EOF exit?               | **yes**                          |
| Will a *given CI job* actually reach it at runtime?            | **no**                           |

**Residual risk, stated plainly.** The gate cannot decide *runtime* reachability — Round 3 says so
about its own results, limiting reachability claims to what the call graph shows unconditionally.
So a prompt on a path that only executes under particular runtime state (a compose project
present, a zombie container present) is classified correctly as *capable* of spinning, but the gate
cannot say whether any job will get there. Two further gaps: indirect dispatch (a function name in
a variable) is invisible to a static call graph, and a prompt introduced in a *sourced* file the
walker does not know about would not be seen — which is precisely how `token-management.bash`
escaped the Round-1 census.

The honest framing is that the gate makes **new spins** and **new unguarded default-path prompts**
impossible to add silently. It does not prove the absence of a hang.

### Specification

1. **Promote the tooling out of the plan folder.** This repo's own rule is explicit — plan-local is
   for the transient, and *"a permanent QA gate wired into `qa-all.bash`"* is named as the
   persistent case (`CLAUDE/PlanWorkflow.md`). Promotion into `scripts/` is therefore **part of
   finishing this plan, not a follow-up**; leaving it in `CLAUDE/Plan/00068-*/` means the gate dies
   when the plan is archived.
2. **Seed it as a ratchet.** Baseline the current 46 sites and their verdicts; fail on a **new**
   spin path or a **new** unguarded default-path prompt. A stale baseline entry — a site that no
   longer exists — must also fail, or the list only ever grows and stops being a ratchet.
3. **Keep every fail-capable invariant.** The header already says it: *"Do not remove a check to
   make this script 'work'."* The block-balance invariant is the one that caught the multi-line
   banner bug; a future maintainer under time pressure is exactly who would delete it.
4. **Wire it into `qa-all.bash`**, so it runs on every commit rather than when someone remembers.

---

## What Phase 2 does not settle

- **The per-entry-point message text is not written.** Outcome (iii) requires each fail-fast
  message to name the flag that answers it; the flags for outcomes still being designed
  (`--mcp`, `--egress`, token-by-value) do not have final spellings.
- **Task 2.2's behaviour change is unmeasured.** How many callers pass `--headless` at a TTY today
  is unknown; the population was not surveyed.
- **Nothing is implemented, and the promotion in 2.3 is a specification.** The tooling exists and
  works, but it currently reads `claude-yolo` sources from *this checkout* by a plan-relative walk;
  promoting it means re-rooting that resolution, which is a real change and not a file move.
