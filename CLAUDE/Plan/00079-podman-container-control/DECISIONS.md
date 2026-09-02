# Plan 00079 — Technical Decisions

Supporting document for [PLAN.md](PLAN.md). Each decision records the context,
the choice, and why it is not merely a preference. Fact references (F-numbers)
resolve in [RESEARCH-facts.md](RESEARCH-facts.md); the full working behind each move is in
`JOURNAL/`.

## D1: Mechanism — pause/unpause only

**Context**: user asked for freeze/unfreeze *and* suspend/unsuspend.
**Decision**: both pairs are the same operation here: `podman pause`/`unpause`.
CRIU checkpoint (the only genuinely different "suspend to disk") is root-only
(F2) and incompatible with CCY's `--rm` (F6, H5) — excluded as a non-goal. The
tool's help text says this plainly: *frozen containers do not survive a
reboot*. **Date**: 2026-08-19

## D2: Build vs adopt — build a small tool; existing TUIs don't cover the asks

**Context**: surveyed podman-tui, lazydocker, ctop, Cockpit (cockpit-podman),
dockge (research log in JOURNAL). podman-tui is the only Fedora-packaged,
Podman-native TUI (F9), but nothing surveyed offers network-scoped bulk
freeze, a CCY-group verb, or self-freeze protection; lazydocker/ctop/dockge are
Docker-oriented and/or unpackaged for Fedora.
**Decision**: build `podfreeze` for the verbs; do **not** bundle podman-tui
into this plan (anyone wanting a browsing dashboard can propose it separately —
YAGNI). **Date**: 2026-08-19

## D3: Implementation — bash + fzf, not Python/curses, not Go/Rust

**Context**: repo conventions — user-facing interactive tools under
`files/home/.local/bin/` are bash (`open`, `ftp-camera`, `nord`, ccy itself);
`helpers/` stdlib-Python is for Ansible-invoked logic, not interactive TUIs
(`helpers/CLAUDE.md`); a Go/Rust TUI would add a toolchain this repo does not
have.
**Decision**: single bash executable using `podman` CLI + fzf `--multi` picker,
numbered-menu fallback when fzf is absent (F7 precedent). Binds to
`CLAUDE/InteractiveScripts.md` and `CLAUDE/StderrHygiene.md` in full.
**Date**: 2026-08-19

## D4: CCY identification — run-time session label, name pattern for older CCY

**Decision (settled)**: `--ccy` selects on the run-time `ccy=true` label (set by
CCY ≥ 3.40.0, so it cannot be inherited) **or** the F4 session name pattern. The
inherited `claude-yolo-version` image label is **not** consulted — see D6.

This moved three times (H2 demoted Phase 1 to an enhancement, F16 re-promoted
it, landing it then made the image label pure liability); the working is in the
journal, so re-read that before re-litigating it. **Date**: 2026-08-20

## D5: Naming — `podfreeze`

Behaviour-descriptive, no mood: `podfreeze` with verbs
`freeze` / `thaw` / `list` (no-arg = interactive picker). "freeze/thaw" is the
user's own vocabulary; help text states it equals pause/unpause.
**Date**: 2026-08-19

Originally `podman-freeze`; shortened at the user's request. The pre-rename
binary is removed by this plan's `deploy.bash`, not by a `state: absent` task in
the play: the play describes the machine's steady state, and a one-off cleanup
for a name that only ever existed mid-plan does not belong in it permanently.
It is still not done by hand — the plan script owns it, and fails if the removal
does not succeed.

## D6: the image label is dropped, and that is a narrowing with no gap

**Context**: F16 — the image label marks anything built FROM the CCY image, so
it selected non-sessions. Not hypothetical: the gate's own throwaway was one.
**Decision**: once Phase 1 landed, drop the image label outright rather than
keep it as a fallback.

**Why that narrowing is safe.** Narrowing is normally the dangerous direction —
an over-match is *visible* (named in the printed set, undone by repeating) while
an under-match is *silent*, a session absent while you believe you froze
everything. That asymmetry is why the union was kept until an exact positive
signal existed, and it stops applying here because the image label covers
**nothing** the other two miss: ≥ 3.40.0 sessions carry `ccy=true`, older ones
are named `<project>_yolo[_N]` by construction (`get_next_container_name`). Its
only remaining contribution was F16's false positive. **Date**: 2026-08-20

## D7: the interactive UX — groups, a derived verb, no confirm, and a loop

**Context**: user feedback on the first build — three prompts to freeze the CCY
group, with the group targets reachable only by knowing the flag names.

**Decisions**, each with the reason it is not merely a preference:

1. **The menu offers groups, not containers** — per-container picking is the
   last entry. Acting on a whole group is the common case, and it had been the
   hardest thing to reach.
2. **The verb is derived, not asked**: anything running is frozen, a set with
   nothing running is thawed, so the same choice twice toggles. There is only
   one sensible move for a given set, and offering the other is offering a
   mistake. An explicit `freeze`/`thaw` still wins, so scripts can say what
   they mean instead of depending on current state.
3. **No confirmation prompt.** This reverses the "confirm before destructive
   action" reflex deliberately: freezing is *not* irreversible, and the chosen
   menu row already named the verb and the count. `-y`/`--yes` is removed
   rather than left as a no-op; `--dry-run` remains.
4. **The menu loops**: act, re-read the inventory, re-offer. The re-read is
   load-bearing — counts must describe the machine now, not at startup.
5. **No "frozen things stay frozen" warning** — it restated the verb.

**Consequence for F15**: the resolved-set preview is a record printed as it
acts, not a gate. Protection against freezing a network holding a live session
moved earlier, into the menu row that names the count before the choice, and
rests on reversibility rather than on a prompt. **Date**: 2026-08-19

## D8: group by session identity, not just by name and network

**Context**: user ask — *"label ccy containers by key label and anthropic token
as well? so could easily freeze all containers with github ID XXX"*. Once a
session is labelled at run time at all (D6), the same mechanism answers a
question the tool could not previously ask: **who** is this session running as.

**Decision**: CCY stamps `ccy-github`, `ccy-token` and `ccy-ssh-keys`;
`podfreeze` gains `--github` / `--token` / `--ssh-key` and one menu row per
distinct value in the live inventory.

- **Values drive the menu**, derived from what is running (as the network rows
  are), and an axis appears only at ≥ 2 distinct values — with one account,
  "github: \<id>" and "all CCY containers" are the same button.
- **`ccy-ssh-keys` is multi-valued**, so it matches by WORD while the
  single-valued axes compare whole. Being lenient there would silently widen a
  freeze, so the two are separate paths, each unit-tested.
- **`none`, not empty**, so "no GitHub identity" cannot read as "unlabelled".
- **An unknown value is an error** listing the known ones — never an empty set
  and exit 0, the trap `select_network` already guards.
- **Nothing in the container reads these.** Pre-3.40.0 sessions carry none and
  stay reachable by name or `--ccy`; `--github` says so rather than pretending
  they do not exist. **Date**: 2026-08-20

## D9: choosing a group opens it, rather than acting immediately

**Context**: user ask — *"can we have a way to drill into a grouping … first
option is 'all' or its then possible to pick specific ones … easiest and
clearest"*.

**Decision**: a group row leads to a second screen listing the group's members,
with **"act on all of it" as the first row**, each member showing the verb it
will get. The standalone "pick individual containers…" entry is removed:
drilling into "all containers" *is* that, so keeping it would be a second route
to one screen.

**Why this does not undo D7.** The earlier feedback was that per-container
tab-selection was *forced* and that group targets were unreachable without
knowing flag names. Here the whole group stays one keypress — ENTER on row 1 —
so the fast path is intact and the picking is optional. What it buys is
**sight of the members before acting**, which a group row structurally cannot
give: "network: podman — FREEZE 5" names nothing, and F15/F17 are exactly the
case where one of those five is a live Claude session on an app network.

The "all" row is keyed by a sentinel (`*all*`) rather than the word: podman
container names must start with an alphanumeric, so no container can collide
with it. **Date**: 2026-08-20
