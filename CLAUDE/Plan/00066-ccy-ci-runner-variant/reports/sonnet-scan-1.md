# Independent Deep Scan — Plan 00066 (ccy CI-runner variant)

**Scope**: read `claude-yolo` (2847 lines, in full), all 7 `lib/*.bash`, `entrypoint.sh`,
`Dockerfile`, `Dockerfile.project-template`, `playbooks/imports/play-claude-yolo.yml`, then
`PLAN.md`. All line numbers below were re-derived from source in this session, not copied
from the plan. Several claims were verified empirically by reproducing the exact bash
control-flow shape in a throwaway script (shown inline) rather than asserted from reading
alone.

Severity key: **CRITICAL** = would silently break or hang CI; **HIGH** = real defect the
plan doesn't account for; **MEDIUM** = real but narrower/operational gap.

---

## 1. CRITICAL — There is no non-interactive path to obtain a token at all

`create_token()` (`lib/token-management.bash:127-464`) is the only way ccy ever populates a
usable token file. Every path through it is interactive with no bypass flag:

- `read -r -p "Enter a name for this token..."` (line 171) — skippable only via the
  `preset_name` arg, which is only reachable from `--update-token=NAME`, not from a first
  creation.
- A bare `read -r -p "Overwrite? (y/N): "` (line 212) when a same-named token exists.
- A bare `read -r` with **no prompt text and no default** at line 239 ("Press Enter to
  continue...") — this always blocks on stdin with zero way to skip it.
- The actual auth step is `claude setup-token` run inside a throwaway container with `-it`
  (line 254) — Anthropic's own OAuth device-code flow: "CLI → Browser → CLI" per the
  script's own instructions (lines 232-236). This is inherently a human-in-a-browser flow;
  it cannot be scripted.
- If the container's stdout can't be regex-matched for the token, there is a **second**
  interactive retry loop asking the human to paste it manually (lines 322-417).

**Consequence for the plan**: Phase 2 (`--non-interactive` design) never asks "how does a
CI job obtain `CLAUDE_CODE_OAUTH_TOKEN` in the first place?" The only viable answer is: a
human runs `claude setup-token` interactively, once, on a workstation, and the resulting
token is placed directly into `~/.claude-tokens/ccy/tokens/<name>.<expiry>.token` (or
injected as `CLAUDE_CODE_OAUTH_TOKEN`, which ccy does not currently accept as an input —
see finding 8) as a CI secret, entirely bypassing `create_token()`. Renewal is comment-
documented as *also* unknowable up front: "`claude setup-token` doesn't tell us when the
token actually expires, so we use 90 days as a conservative estimate" (line 189-191) — i.e.
even the file's own encoded expiry is a guess, and real rotation requires a human to re-run
the OAuth flow periodically. This is an operational dependency the design phase should name
explicitly as a Task, not leave implicit under "satisfied from a flag/env already."

---

## 2. CRITICAL — Ephemeral-runner image distribution has no mechanism in this codebase

Task 1.3 asks "does the image get built by Ansible or by ccy's first run?" — answer
confirmed: **Ansible** (`playbooks/imports/play-claude-yolo.yml:333-345`, task "Build Claude
YOLO Container Image with Hash", `{{ container_engine }} build ... -t claude-yolo:latest /opt/claude-yolo`). Task 3.4 concludes from this that the CI image "must be built by
Ansible, never per-job."

That is correct for a **persistent, Ansible-provisioned host** (self-hosted runner). It does
not address a genuinely **ephemeral** GitHub-hosted runner (fresh VM per job): there is
**no registry push/pull anywhere** in `claude-yolo`, the libs, or any playbook I could find
— `container_cmd build`/`commit` only ever write to local podman/docker storage. "Built by
Ansible ahead of time" has no meaning on a runner that doesn't exist until the job starts.
The plan needs an explicit decision here (self-hosted-only for now vs. add registry
push/pull as a new capability) — right now Task 3.4 reads as if "Ansible builds it" already
answers the ephemeral case, and it doesn't.

---

## 3. CRITICAL — Container-name TOCTOU lets one concurrent CI job kill another's live container

`get_next_container_name()` (`lib/common.bash:583-616`) computes the next free name by
`container_cmd ps -a` + string match + increment — a classic read-then-decide race with no
lock. `PROJECT_NAME` (`get_project_name()`, `lib/common.bash:426-438`) is derived **purely**
from `basename $PWD` + parent-dir name — no PID, run ID, branch, or job ID anywhere.

Immediately before launch, ccy does this unconditionally (`claude-yolo:2747-2749`):

```bash
if container_cmd rm -f "$CONTAINER_NAME" 2>&1 | grep -q "$CONTAINER_NAME"; then
    echo "Removed leftover container: $CONTAINER_NAME"
```

`rm -f` on podman/docker **force-stops and removes a still-running container**, it is not
restricted to dead/stale ones (that's what `clean_stale_containers_startup` — a different,
correctly-scoped function — is for). If two concurrent CI jobs for the same repo (same
directory basename+parent → same `PROJECT_NAME`) run on a shared/self-hosted runner and
happen to compute the same "next available" name in the race window, the second job's
"safety net" cleanup will silently kill the first job's live, still-running container.
Nothing in Phase 1/Task 1.2 (which is scoped to *prompt* sites) or anywhere else in the
plan considers this. This is a distinct defect from the prompt-hang issues E2/E3 describe —
it's a correctness/data-loss bug in container naming, not a UX one.

---

## 4. CRITICAL (but narrower than the plan claims) — the container-management TUIs are the *only* prompts that truly hang forever, and they are exactly what a concurrent same-project CI job hits

I verified this by reproducing the exact call shape. Two functions are invoked as the
**tested condition of an `if`** (with `!`):

```bash
# claude-yolo:1219
if ! check_zombie_containers_startup "yolo"; then exit 0; fi
# claude-yolo:1229
if ! check_project_containers_startup "$PROJECT_NAME" "yolo"; then exit 0; fi
```

Bash suspends `errexit` for the **entire subtree** of a command that is itself the tested
condition of `if`/`while`/`until` (or negated with `!`) — including nested function calls
and loops inside it. I reproduced this exactly:

```bash
$ cat repro.sh
set -e
inner_loop() { while true; do read -rp "choice: " x; case "$x" in y) return 0;; *) : ;; esac; done; }
outer_func() { inner_loop; return 0; }
if ! outer_func; then exit 0; fi
$ bash repro.sh < /dev/null   # never returns — genuine busy-loop spin, confirmed and killed manually
```

So `show_zombie_container_tui`'s `while true; do read -rp "Choice [a/s/i/q]: "...` (docker-
health.bash:161-215) and `check_project_containers_startup`'s `while true; do read -rp "Choice [c/s/m/q]: "...` (docker-health.bash:485-537) **do** genuinely spin forever on EOF,
exactly as the plan says.

But **`check_project_containers_startup` only fires when a running container already exists
for this `PROJECT_NAME`** (docker-health.bash:437-449) — i.e. precisely the situation created
by a second concurrent CI job for the same repo (see finding 3). So findings 3 and 4 compound:
a second concurrent job either (a) hits this prompt and busy-loops forever consuming CPU
until something external kills it, or (b) if the race window closes the other way, force-
kills the first job's container. Both outcomes are bad; the plan currently designs against
neither.

---

## 5. CRITICAL — E3's own cited line numbers include sites that do **not** spin; several abort immediately instead

E3 states "at least ten of those prompts are `while true` menu loops that spin forever on
EOF" and cites, among others, `claude-yolo:1104`, `claude-yolo:2011`,
`lib/network-management.bash:271`, `lib/dockerfile-custom.bash:37,117,157,718`.

I checked each by tracing the call chain to the top level and reproducing the shape. The
distinguishing factor (confirmed above) is whether an *ancestor* is invoked as an if/while
*condition* — if not, `errexit` is live and a failing `read` aborts the whole script on the
first EOF (exit 1, no spin, no output past that point). I reproduced this too:

```bash
$ cat repro2.sh
set -e
if [ "$TOKEN_VALIDATION_FAILED" = true ]; then
    while true; do read -rp "Select option [1-3]: " x; case "$x" in 1) break;; *) : ;; esac; done
fi
$ TOKEN_VALIDATION_FAILED=true bash repro2.sh < /dev/null
$ echo $?
1        # aborts immediately — does NOT spin
```

- `claude-yolo:1104` — the token-validation recovery menu — is a bare `while true` inside a
  plain `if [ "$TOKEN_VALIDATION_FAILED" = true ]; then` body (`claude-yolo:1066-1170`), not
  behind a tested function call. **Aborts, does not spin.**
- `claude-yolo:2011` — the cross-engine-mismatch cleanup menu — same shape, plain `if [[ "$CONTAINER_ENGINE" = "podman" ]]; then ... while true; do read...`. **Aborts, does not
  spin.**
- `lib/network-management.bash:271` — `connect_to_network()`'s network-selection prompt.
  This function is invoked as a bare statement, `connect_to_network "$CONNECT_NETWORK" "_yolo" "ccy"; exit $?` (`claude-yolo:784`) — not an if-condition. **Aborts, does not spin.**
- `lib/dockerfile-custom.bash:37,117,157` — `custom_dockerfile()`'s menus. Also invoked as a
  bare statement, `custom_dockerfile "$0" ".claude/ccy" "ccy"; exit 0` (`claude-yolo:790`).
  **Abort, do not spin.** (Line 718, inside `create_dockerfile_guided()`, is called the same
  bare way at `claude-yolo:796` — same reasoning applies; not independently re-verified line
  by line but the invocation context is identical.)

**Why this matters for the plan, not just as pedantry**: Decision 2/Phase 2 treats "hangs"
as the uniform failure mode across all 35 sites and frames `--non-interactive` as the fix
for hanging. In fact the *majority* of cited sites already fail fast today under `set -e` —
just with an unhelpful, non-descriptive `exit 1` (the script dies mid-banner with no
"here's what you should have passed" message) rather than a designed fail-fast error. The
true "hangs forever, consumes CPU indefinitely, will not even show up as a normal CI
timeout-with-log" failure mode is real but narrower — concentrated in the two
`check_*_containers_startup` container-management preflights (finding 4). The design
consequence: Task 2.1's per-site classification work is still needed (a confusing `exit 1`
mid-flow is still a bug to fix under `--non-interactive`), but the *priority order* implied
by "this is the hard blocker" overstates uniform urgency — the two genuinely-hanging sites
deserve to be fixed first and separately, since they're also the ones a concurrent CI job
is statistically most likely to hit (finding 4).

Also note: the enumerated `read -rp`/`read -r -p` count is closer to **45** than 35 by my
own grep across `claude-yolo` + all 7 libs (e.g. `lib/network-management.bash:158`, the
"Select container" prompt in `connect_to_network`, isn't in E2/E3's list at all). Not a
material error, but Task 1.2's "enumerate all 35" should re-derive the count rather than
inherit it.

---

## 6. HIGH — The unconditional default-network preflight is an undocumented, unrelated hard-fail dependency for any egress-restricted CI design

When `CONTAINER_ENGINE=podman` (the default) and `--no-network` is **not** passed, ccy
unconditionally does (`claude-yolo:2511-2517`):

```bash
elif [[ "$CONTAINER_ENGINE" = "podman" ]] && [[ "$NO_NETWORK_MODE" != true ]]; then
    SELECTED_NETWORK="podman"
    NETWORK_FLAG="--network podman"
fi
```

...then runs a connectivity preflight (`claude-yolo:2524-2599`):

```bash
if container_cmd run --rm --network "$SELECTED_NETWORK" alpine wget -q -O- --timeout=10 http://google.com >/dev/null 2>&1; then
    ...
else
    ... # full diagnostic banner
    exit 1
fi
```

This is a **hard, fatal dependency** on (a) being able to pull the `alpine` image from a
registry, and (b) plain, unauthenticated HTTP reachability to `google.com` specifically —
neither of which has anything to do with Claude Code, GitHub, or npm (the services E7/E8/
Task 5.3 actually discuss). It runs in a **separate throwaway container**, entirely outside
whatever `--egress` proxy mechanism Phase 5 designs, so a correctly-scoped egress allowlist
(GitHub + Anthropic + npm registry, per Task 5.3's own framing) will **not** include this
and ccy will hard-exit before the real container ever starts — for a reason a CI operator
debugging egress denials would find bizarre (why does `google.com:80` need to be allowed to
run a Claude Code session?).

`--no-network` is the only escape (it skips the `elif` above entirely), but nothing in the
plan currently states that `--no-network` must be a **mandatory** CI flag for this reason —
E5's discussion of `--network` is entirely about the naming collision with a hypothetical
egress-restriction flag, not about this unconditional preflight being a separate hard
dependency. Task 5.3 should enumerate this probe as its own line item.

---

## 7. HIGH — A failing claude/container run skips compose cleanup; the intended `exit $?` verification target has a real, separate bug

The task brief asked specifically to check whether anything after `container_cmd run`
clobbers `$?`. Two independent things are true here, and they point in different
directions:

- **Exit-code propagation to ccy's own caller is correct.** `container_cmd run ...`
  (`claude-yolo:2770-2792`) is a bare top-level statement (not wrapped in any conditional),
  so under `set -e` a non-zero exit from it aborts the script immediately with that same
  code. The `trap cleanup EXIT` (`claude-yolo:1716`) does **not** clobber this — I verified
  empirically that bash preserves the pre-trap `$?` for the process's final exit status
  regardless of what the trap handler itself returns, unless the trap explicitly calls
  `exit N`; `cleanup()` (`claude-yolo:1694-1716`) never does. So `ccy`'s own exit code
  faithfully mirrors `claude`'s, which is what CI correctness needs.
- **But the block that follows `container_cmd run`
  (`claude-yolo:2794-2846`, "Offer to stop compose services") never runs on a non-zero
  exit**, because `set -e` fires and the script is already gone by the time that `if` would
  be reached. Concretely: any docker-compose services ccy auto-started this session
  (`CCY_COMPOSE_WAS_STARTED=true`, set at e.g. `claude-yolo:2103`) are silently left running
  — with no attempt at teardown — on **any** failing exit from the real claude invocation,
  including the entirely ordinary CI case of a coding task that legitimately fails and
  returns non-zero. This is a genuine resource-leak bug, not CI-specific, but it matters
  more in CI because there's no human at a terminal to notice and clean it up between jobs
  on a reused runner.

Net: the plan's implicit assumption that exit-code propagation "just works" is correct, but
the adjacent cleanup-skip is a real defect the plan doesn't mention and should track
alongside the `--non-interactive` work (it's the same `set -e`-vs-cleanup interaction class
as findings 4-5).

---

## 8. MEDIUM — `--no-ssh` doesn't remove the GitHub-CLI dependency; it just moves it to `gh auth token`

Even with `--no-ssh`, `build_ssh_mounts_and_validate` (`lib/ssh-handling.bash:312-508`) still
runs unconditionally and, with `SSH_KEYS` empty, falls to the `else` branch at line 495-507:

```bash
GH_TOKEN=$(gh auth token 2>/dev/null)
if [ -z "$GH_TOKEN" ]; then
    print_error "Not authenticated with GitHub CLI"
    ...
    return 1
fi
```

This fails fast (good), but it means a CI runner needs `gh` **already authenticated**
out-of-band (e.g. `GH_TOKEN` env var that `gh` itself reads, or a prior `gh auth login`) —
a second, separate credential-provisioning requirement beyond the Claude OAuth token
(finding 1), not enumerated anywhere in the plan's evidence table (E7 only covers the
in-container `gh auth login --with-token`/`gh auth status` calls, not this host-side
pre-launch call).

Also note ccy has **no way to accept a token via `CLAUDE_CODE_OAUTH_TOKEN` directly** —
`SELECTED_TOKEN` is always resolved from a file glob under `$TOKEN_DIR`
(`claude-yolo:930-953`, `978`). A CI-provisioning story that plans to "just set the env var
as a secret" will not work without also writing the file in the exact
`NAME.YYYY-MM-DD.token` shape ccy expects.

---

## 9. MEDIUM — `.last-launch.conf` reconfirmation prompt on any persisted/cached checkout

`save_launch_config` runs unconditionally after every successful launch
(`claude-yolo:2612-2613`), writing into the project-local `.claude/ccy/.last-launch.conf`
(part of the bind-mounted `/workspace`, i.e. part of the checkout, not `$HOME`). On any CI
setup that persists or caches the workspace between runs (self-hosted runner, or an
`actions/cache` step over the checkout — plausible if a team wants to speed up repeated
runs), a subsequent invocation that doesn't pass **all three** of `--no-ssh`, `--token NAME`, and `--no-network` together (the exact AND-gate at `claude-yolo:803-805`) re-enters
`load_launch_config` and, on success, the "Quick Launch Available... Use same
configuration? [Y/n]" prompt (`claude-yolo:800-859`). This one aborts under `set -e` rather
than spinning (same reasoning as finding 5 — it's a bare top-level `while true`), so it
fails fast rather than hanging, but it's still an easy, undocumented footgun: a CI recipe
that only passes `--token NAME` (forgetting `--no-ssh`/`--no-network`, e.g. because a
previous run auto-populated them into the saved config) will intermittently break depending
on whether the config file happens to exist yet.

---

## Verdict on the plan's E1-E9 table

| ID  | Verdict                                                                                                                                                                     | Notes                                                                                                                                                                                                                                                                                                                                                                                                        |
| --- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| E1  | **Confirmed accurate.**                                                                                                                                                     | Lines match exactly: `2626-2628` (`-p`), `2694-2699` (`-i`/`-it`), `729-740` (`--prompt` required).                                                                                                                                                                                                                                                                                                          |
| E2  | **Directionally correct, count is an undercount.**                                                                                                                          | HEADLESS_MODE guards exactly one site (`ssh-handling.bash:357`), confirmed. Actual `read -rp`/`read -r -p` count is closer to 45 than 35 (see finding 5).                                                                                                                                                                                                                                                    |
| E3  | **Partly wrong — see finding 5.**                                                                                                                                           | The "spins forever" mechanism is real but only for prompts reached via an ancestor invoked as an if/while *condition*. Of E3's own cited sites, `claude-yolo:1104`, `claude-yolo:2011`, `network-management.bash:271`, and `dockerfile-custom.bash:37/117/157` do **not** spin — they abort immediately via `set -e` (verified by reproduction). Only the two container-management TUI sites genuinely spin. |
| E4  | **Confirmed accurate.**                                                                                                                                                     | `grep -rin mcp` across the whole `claude-yolo/` tree returns zero matches.                                                                                                                                                                                                                                                                                                                                   |
| E5  | **Confirmed accurate, and there's more to it.**                                                                                                                             | Default network + probe lines match exactly (`2514-2516`, `2529`). Finding 6 adds that this preflight is a distinct, unrelated hard-fail dependency (alpine pull + plain HTTP to google.com) that the plan's egress-allowlist discussion (Task 5.3) doesn't yet cover.                                                                                                                                       |
| E6  | **Correctly marked unverified in the plan** — appropriately deferred to Task 1.1 (host-only test), not re-verified here.                                                    |                                                                                                                                                                                                                                                                                                                                                                                                              |
| E7  | **Confirmed accurate**, and finding 8 adds a second, separate GitHub-CLI dependency (the host-side `gh auth token` fallback) not covered by E7's in-container-only framing. |                                                                                                                                                                                                                                                                                                                                                                                                              |
| E8  | **Confirmed accurate.**                                                                                                                                                     | `auto_update_claude_code`/`update_claude_inplace` trace matches exactly (`1254`, `1343`); the temp-container-then-commit mechanism is real (`1353-1374`) and re-applies the ctrl+z patch.                                                                                                                                                                                                                    |
| E9  | **Confirmed accurate.**                                                                                                                                                     | All four seams (`CCY_EXTRA_MOUNTS`, `.claude/ccy/ccy.env`, `CCY_CLAUDE_WRAPPER`/`--supervise`, `CCY_CONTAINER_ENGINE`/`CCY_AUTO_UPDATE`) exist exactly as described.                                                                                                                                                                                                                                         |

## Summary of what the author and a hostile reviewer would both likely miss

1. Token provisioning is not just "one more `--non-interactive` site to fix" — it is
   structurally impossible to automate (real OAuth/browser flow) and needs an explicit
   out-of-band provisioning design, not a flag.
2. The image-distribution story silently assumes a persistent host; it has no answer for a
   truly ephemeral runner without adding registry push/pull, which doesn't exist today.
3. Concurrency isn't just "prompts might hang" — there's a genuine TOCTOU that lets one
   concurrent job's cleanup force-kill another job's live container, and the one scenario
   that reliably triggers the *real* infinite-hang bug (finding 4) is exactly two concurrent
   jobs on the same project.
4. The evidence table's own "hangs forever" claim is broader than the source supports;
   precisely locating which sites truly hang (vs. fail fast today) changes what Phase 2
   should fix first.
5. A completely unrelated preflight (alpine + `http://google.com`) is a hard, silent
   dependency that will break any properly-scoped egress allowlist for a reason that has
   nothing to do with the workload.
