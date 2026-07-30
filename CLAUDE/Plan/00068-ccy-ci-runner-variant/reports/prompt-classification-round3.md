# Plan 00068 — Prompt classification, Round 3 (Task 7.3)

Task 7.3 asked to classify each of the 46 prompt sites as *spins* vs *aborts undiagnosably*.
Its own wording said the discriminator was "errexit suspended by an `if`/`while` **ancestor**".

**That premise is wrong, and so is the task text.** A loop *body* does not suspend `errexit`;
only a *condition* does. What decides the verdict is the **call context of the enclosing
function**, propagated transitively through the call graph. Two sites can be the same line and
get different verdicts.

Everything below is measured, not reasoned. The measurement tooling is in
[`../analysis/`](../analysis/) and every step has a check that can fail.

## The mechanism, executed

```
set -e
inner() { while true; do read -r -p "x: " v; ...; done; }
outer() { inner; }              # bare call, inside outer

( outer )   < /dev/null   ->  rc=1, ZERO iterations, no message   ABORTS
outer || …  < /dev/null   ->  "SPUN 5x", then continued           SPINS
```

The second line is the load-bearing one: `||` suspends `errexit` through `outer`'s **entire
body**, and that suspension is **inherited by `inner`**, which was called bare. So a bare call
is not evidence of a live `errexit` — you have to walk up to the outermost frame.

This is exactly the shape of `claude-yolo:1004` → `select_token` → `create_token`.

## Verdicts

| Verdict                     | Sites | What happens on EOF                                                           |
| --------------------------- | ----: | ----------------------------------------------------------------------------- |
| **ABORTS** (only)           |    32 | `set -e` kills the script at the `read`. No message naming the cause.         |
| **SPINS** (only)            |     6 | `errexit` suspended + unbounded `while true` with no EOF exit. Burns forever. |
| **SPINS or ABORTS by path** |     6 | Same source line, verdict decided by which call path reached it.              |
| **Falls through or aborts** |     1 | Suspended but no loop — proceeds on an empty answer.                          |
| **GUARDED** (never reached) |     1 | Behind a TTY/headless check. The one site that is already correct.            |
| **Total**                   |    46 |                                                                               |

### SPINS — the five distinct paths

A spin needs *both* a suspended call context *and* an unbounded loop with no EOF exit. Five
paths satisfy both:

| Spinning function                   | Prompt sites                    | Suspended by                                                                  |
| ----------------------------------- | ------------------------------- | ----------------------------------------------------------------------------- |
| `select_token`                      | `:611`                          | `claude-yolo:1004`, `:1117` — `select_token … \|\| { … }`                     |
| `create_token` (via `select_token`) | `:171 :324 :331 :346 :364 :407` | inherits, called bare at `token-management.bash:639`, `:656`                  |
| `show_zombie_container_tui`         | `:162 :182`                     | `claude-yolo:1219` — `if ! check_zombie_containers_startup …`                 |
| `check_project_containers_startup`  | `:486 :509`                     | `claude-yolo:1229` — `if ! check_project_containers_startup …`                |
| `_do_compose_start`                 | `:546`                          | `if check_and_start_compose_services …` (×5), `if offer_compose_start …` (×2) |

**Task 7.3 says "fix the two spinning TUIs first". There are five paths across four
functions**, not two. The undercount comes from Round 1 never having examined
`token-management.bash` at all (Task 7.2) and from classifying by site rather than by path.

### The same line, two verdicts

`create_token`'s seven prompts are reached two ways:

| Reached from                                               | `errexit` | Verdict on EOF |
| ---------------------------------------------------------- | --------- | -------------- |
| `claude-yolo:897 :907 :970 :1000 :1015 :1111 :1123` (bare) | live      | **ABORTS**     |
| `token-management.bash:639 :656`, inside `select_token`    | suspended | **SPINS**      |

Which prompt you hit also depends on the `preset_name` argument: `select_token:639` passes a
name, so the `:171` naming loop is skipped and the first blocker is `:324`; `select_token:656`
passes none, so `:171` blocks first. `:212` (`Overwrite? (y/N)`) sits in an `if` with no loop —
suspended it falls through on an empty answer and returns "Cancelled", which is a *silent
inference*, not a hang.

### The one site that is already right

`ssh-handling.bash:362` is the only prompt of 46 behind a guard:

```bash
if [ "${HEADLESS_MODE:-false}" = "true" ] || [ ! -t 0 ]; then
    echo "  Non-interactive launch — enabling 443 automatically (the only way to proceed)."
    enable_443=true
else
    read -rp "Enable GitHub SSH over 443 for this session? [Y/n] " reply_443
```

It detects non-interactivity, **announces** the inference, states *why* it is the only option,
and proceeds. In CI the `read` is never reached.

This is the reconciliation Task 7.3 asked for: **"never infer" should read "never *silently*
infer"**. The codebase already contains the correct pattern — it is used **once out of 46
opportunities**. The fix for the other 45 is not to invent a mechanism, it is to apply this one.

## Why "aborts" is not the mild verdict

An abort at least terminates, so it cannot burn a JIT runner slot. But `set -e` kills the script
**with no message about which prompt failed or why** — stdout ends mid-banner. All 32
abort-only sites are plain `read` commands with no `||`, no `-t` timeout, and no `/dev/tty`
redirect (verified across all 46), so nothing distinguishes "EOF on a prompt" from any other
failure. That is the "aborts undiagnosably" class C4 named.

## Method, and the checks that could have failed

Four measurements, each with a discriminating check:

1. **Prompt sites (46).** `awk` matching `read` with `-p` in any flag order. Reproduces Task
   7.2's 46 exactly.
2. **Function boundaries.** Brace tracking, then **every extracted body re-parsed with
   `bash -n`**: 63/63 parse. Mutation-tested — truncating each body by one line makes 63/63
   *reject*, so the check has teeth. (Extending by one line does not, because the following
   line is usually blank; the end is pinned from the other side by asserting each end line is
   a closing brace.)
3. **Block nesting.** Tracked to a per-line stack; **asserted balanced at EOF** for all 9
   files.
4. **Call graph.** Every call site of every prompt-bearing function, classified by its own
   line's syntax (`bare` / `||` / `if` / `!`), then propagated transitively.

**One bug was found by check 3 and it mattered.** The first block tracker leaked an unclosed
`if@2626` in `claude-yolo`. The cause: `claude-yolo` builds its startup banners as multi-line
double-quoted strings, and the prose inside contains the words `if`, `for` and `done` —

```
    STARTUP_INFO+="
    …
The container will automatically rebuild if the Dockerfile changes.
```

A per-line stripper reads that `if` as a keyword and corrupts every block boundary after it,
including prompt site `:2818`. Quote state now carries across lines. This is the same shape as
the `grep`-has-no-`\t` defect: the tool was syntactically fine and silently computed the wrong
answer, and only an invariant that could fail (stack balance at EOF) exposed it.

## Reconciliation with C3 and C4

The Round-1 corrections block contains two statements that cannot both be complete, and this
classification resolves which.

- **C4 is right about the mechanism.** It says spins happen where an ancestor is "invoked as an
  `if`/`while` *condition*, because bash suspends `errexit` for that whole subtree". That is
  correct, and it is *sharper* than Task 7.3's paraphrase of it ("errexit suspended by an
  `if`/`while` **ancestor**"), which drops the word *condition* and thereby inverts the meaning.
  This report refines C4 rather than contradicting it: the suspending context is any of
  `if`/`while`/`until` **condition**, `&&`/`||` non-final position, or `!`, and it propagates
  transitively through bare calls beneath it.
- **C4's enumeration is incomplete.** It names only the two `check_*_containers_startup` TUIs
  (`docker-health.bash:161`, `:485`). There are **five** paths — it misses `select_token` /
  `create_token` and both routes into `_do_compose_start`.
- **C4 and C3 were in tension.** C3 says `select_token` on the default launch path is "the
  earliest and most certain unattended blocker"; C4 says only the two container TUIs spin. Both
  were written from the same evidence base. The resolution: `select_token` **does** spin — every
  call site guards it with `||` (`claude-yolo:1004`, `:1117`) — so C3's blocker is also a hang,
  and C4 simply had not walked that call path. C4's count of 2 should read **5**.

C4's negative claims all hold: `claude-yolo:1104`, `:2011`, `network-management.bash:271`, and
`dockerfile-custom.bash:37/117/157` do **not** spin. The first two are top-level code under the
script's own `set -e`; the last four sit in functions called bare.

## What this does not claim

Nothing in `ccy` was executed — the mechanism experiment above is a synthetic reproduction of
the call shapes, not a run of `claude-yolo` itself. **Reachability** is therefore stated only
where the call graph shows it unconditionally: `select_token`/`create_token` sit on the default
path when no token is pre-provisioned. Whether the compose and zombie-container paths are
reached in a given CI job depends on runtime state this analysis does not model. Confirming the
spins empirically remains a HOST triage item.
