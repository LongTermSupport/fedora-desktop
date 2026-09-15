# Plan 00122 Tasks 4.2–4.5 — the shared freeze library

Everything is in the working tree, uncommitted. `./scripts/qa-all.bash` is green:
906 files, freezelib 204, lxcfreeze 74, podfreeze 187.

## The library's path, and why

`files/home/.local/lib/freeze/freeze-common.bash` → `~/.local/lib/freeze/freeze-common.bash`, mode 0644.

User tools get a user-scope library. `files/home/.local/lib/` already exists here
(`clean-paste` keeps its helper in a per-tool subdirectory under it), whereas
`files/var/local/claude-yolo/lib/` is root-owned system scope for a root-deployed tool —
the wrong model for two tools `copy`d into `~/.local/bin` as the login user.

It pays for itself twice: because `files/` MIRRORS the target filesystem, one relative
path — `<dir of the tool>/../lib/freeze/freeze-common.bash` — finds the library from a
repo checkout and from a deployed `~/.local/bin` alike. No env var, no build step.

**The one wrinkle, and it is not hypothetical.** `scripts/test-podfreeze.bash` cuts the
definitions above podfreeze's argument loop into a temp file and sources THAT, so by
then `BASH_SOURCE[0]` is under `/tmp` and the rule above resolves into the temp
directory. That suite may not be edited — it is the safety net the extraction is
measured against — so the resolver tries the same two layouts beside the ENTRY POINT
(`$0`) as well, which for that suite lands back in the checkout. Commented in both tools
and in the journal, because it looks like an odd second candidate until you know why.

## The hooks, and what each abstracts

No `if` on an engine name anywhere in the shared half.

| Hook                       | What it abstracts                                                                      |
| -------------------------- | -------------------------------------------------------------------------------------- |
| `freeze_hook_preflight`    | engine present, privilege available — `assert_podman` vs `assert_lxc` + `assert_sudo`  |
| `freeze_hook_refresh`      | the inventory query and any derived map (podfreeze also rebuilds its network map)      |
| `freeze_hook_menu_rows`    | which groups exist — CCY / network / identity, or bridge                               |
| `freeze_hook_select`       | a menu key → `SELECTED`; non-zero is RECOVERABLE and re-prompts                        |
| `freeze_hook_act`          | one container, one call — `podman pause` vs `sudo lxc-freeze -n`                       |
| `freeze_hook_table_header` | the columns after NAME and STATE                                                       |
| `freeze_hook_table_row`    | those columns for one inventory entry, BY INDEX (every caller has already resolved it) |

Five required settings — `FREEZE_TOOL`, `FREEZE_STATE_RUNNING`, `FREEZE_STATE_FROZEN`,
`FREEZE_HOST_ONLY_NOTE`, `FREEZE_TARGET_HINT` — plus the optional `FREEZE_LIST_NOTE`.
The library refuses to load without the first five, and refuses two state words that are
the same string: collapse those and every container is both a freeze target and a thaw
target at once.

## Task 4.4 — the 187 cases

**All 187 pass with `scripts/test-podfreeze.bash` byte-identical** (`git diff --quiet`
confirms). **No case moved out of it.**

One user-visible string DID change, flagged rather than buried: podfreeze had TWO
different "no terminal to ask on" messages for the identical condition — one in
`pick_target` listing `NAME..., --ccy, --network NET, or --all`, one in
`drill_into_group` saying the same alternatives in different words. The library has one,
built from `FREEZE_TARGET_HINT`. Neither is covered by any case (both functions die
without a TTY, which the suite records as out of scope). No other behaviour, wording,
ordering or exit status differs.

## What lxcfreeze gained

`fzf` when present, the two-level drill-down, `TAB` (or `2,4,5`) member selection,
`ENTER`/`1` for all, `b` to go back, and a budget of 3 WRONG ANSWERS rather than 3
prompts. Its top-level per-container rows became the drill-down, where a container shows
its state and bridge and several can be chosen at once.

Its own help text has described that drill-down since Phase 2 — carried over from
podfreeze while the menu underneath was a flat list. The adoption made the documentation
true rather than requiring it to change.

**Its suite went 69 → 74, which is not "5 new".** Roughly 25 cases MOVED to
`scripts/test-freezelib.bash` because the code they drove moved (`lxcf_index_of`,
`lxcf_count_in_state`, `lxcf_infer_action`, `lxcf_target_effect`, `lxcf_partition`).
Every assertion survived intact, and each now runs TWICE — under podman's
`running`/`paused` and under LXC's `RUNNING`/`FROZEN`. That is strictly more than either
tool's suite could say alone: a library hardcoding one engine's state word passes one
pass and fails the other, and a single-vocabulary suite cannot kill that mutant at all.

Roughly 30 new cases cover what is genuinely LXC's and was never covered: `bridge_names`
(neither non-bridge label may be offered as a group), `select_bridge` (members in
inventory order, an unknown bridge RETURNS, a machine with no bridges gets a different
message), and all four hooks — including that the freeze branch is the one calling
`lxc-freeze`, read out of the function's own text, since an inverted mapping would thaw
everything a user asked to freeze and nothing runnable in this container would see it.

## The library's own suite

`scripts/test-freezelib.bash`, 204 cases, wired into `qa-all.bash` with a row in
`CLAUDE/QA.md` and both gate counts bumped (thirty-four → thirty-five, twenty-seven →
twenty-eight). 20 mutants, 0 survivors, each killed by a NAMED case
(`untracked/scratch/mutate_freezelib.py`, untracked — the suite is the deliverable).

**`do_action` is covered for the first time.** Task 4.1 put it out of scope explicitly,
and it is exactly the code where a defect hits both tools at once. Its act/skip/vanished
split, its dry run, its per-target failure accounting and its undo hint are driven
against a recording hook that logs every call, so a case asserts what the engine WAS
asked, not only what the tool printed. `interactive_loop`'s control flow is covered too —
a group that went away must re-prompt, not end the session.

One mutant found a weakness in the SUITE rather than the library: the empty-group case
called `drill_into_group` directly, so a mutant that died there took the run down and the
kill read as "(died before reporting)". It goes through a command substitution now.

## Task 4.5 — the plays, reconciled by NOT merging them

`tasks/deploy-freeze-lib.yml`, included by both plays — the pattern `tasks/ensure-jq.yml`
already establishes in three plays here.

- **A third play owning the library is disqualified outright**: `ansible-playbook play-podfreeze.yml` on a fresh host must produce a WORKING podfreeze, and the tool sources
  the library at startup. A dependency satisfied only by a play the user was not told to
  run is a trap, not a deployment.
- **Merging was not needed** once the question was asked precisely: what do the two plays
  actually share? One artefact. Not `fzf` (podfreeze's alone), not the tool copy, not the
  name, not the docs anchor, not podfreeze's pre-rename cleanup task. So every worry about
  renaming `play-podfreeze.yml` — the catalogue heading, the `#play-podfreezeyml` anchor
  `docs/ccy.md` links, Plan 00079's `deploy.bash` — evaporates: nothing needs renaming.

`qa-deployed-drift.bash` gained the library. Its loop walks `.local/bin` only, so a
deployed tool whose own bytes match the repo could still be sourcing a stale menu — the
exact invisible drift that gate exists for, one directory over. Its pairs array grew a
third field naming the play to run, since the list now spans two plans and a hardcoded
remedy would send half its readers to the wrong play.

## What I could NOT verify from the container

**Neither tool was run against a real engine.** This container has no reachable podman
and no `lxc`, and both tools refuse to start inside a container by design. Every claim
above is about decisions driven directly, never about a container that was actually
frozen.

What I did exercise on the shipped files: `podfreeze --help` and `lxcfreeze --help` (both
resolve and source the library, parse args, exit 0), and `podfreeze list` / `lxcfreeze list` (contract check passes with all seven hooks defined, then the container guard
refuses with the engine's own explanation — byte-identical to podfreeze's original).

**The host must deploy before it tests.** The library is a NEW file and both tools source
it at startup, so a refreshed `~/.local/bin/podfreeze` without
`~/.local/lib/freeze/freeze-common.bash` beside it does not start. Run
`play-podfreeze.yml` and `play-lxcfreeze.yml`; either deploys the library, both are safe
to run twice.

## Also worth knowing

Writing `tasks/deploy-freeze-lib.yml` was blocked by the hooks daemon's
`ansible-playbook --syntax-check`, which reads any `.yml` as a playbook. Checked before
working around it: the same command rejects `tasks/ensure-jq.yml`, in the repo since
February and included by three plays. `qa-ansible-syntax.bash` derives its population by
CONTENT (`- hosts:` / `- import_playbook:`), so neither task file is in it, and both
plays syntax-check clean.

`freeze_lib_path` is duplicated in both tools. That is inherent bootstrap duplication —
you cannot source the library to find the library.

Task 3.5 (`qa-reviewer` over the diff) is still open and is the plan's last non-host step.

## Files

- `/workspace/files/home/.local/lib/freeze/freeze-common.bash` (new)
- `/workspace/files/home/.local/bin/podfreeze`
- `/workspace/files/home/.local/bin/lxcfreeze`
- `/workspace/scripts/test-freezelib.bash` (new)
- `/workspace/scripts/test-lxcfreeze.bash`
- `/workspace/scripts/test-podfreeze.bash` (UNCHANGED, byte-identical)
- `/workspace/tasks/deploy-freeze-lib.yml` (new)
- `/workspace/playbooks/imports/optional/common/play-podfreeze.yml`
- `/workspace/playbooks/imports/optional/common/play-lxcfreeze.yml`
- `/workspace/scripts/qa-all.bash`, `/workspace/scripts/qa-deployed-drift.bash`
- `/workspace/CLAUDE/QA.md`, `/workspace/CLAUDE/ContainerEngines.md`, `/workspace/docs/playbooks.md`
- `/workspace/CLAUDE/Plan/00122-lxc-freeze-thaw-shared-with-podfreeze/PLAN.md` + `JOURNAL/00122-Journal-26-09-15.md`
- `/workspace/untracked/scratch/mutate_freezelib.py` (untracked)
