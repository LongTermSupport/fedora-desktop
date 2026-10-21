# Plan 00209 N0 fix pass: theme "member" (process model and lifecycle)

Branch `feat/bus-v2-n0-f2-member`, cut from `feat/bus-v2-n0` at cf5c57d9. Edits are in
`DESIGN-v2-B-member.md` (every section except 2.2.x, 2.7.x and 2.13) and in `DESIGN-v2.md`'s rows
named at the end.

## The design change in one paragraph

`teamchat run` is now the keeper: it starts Claude Code as its child and stays its parent, so the
process that bounds a ccy container (tini's one child) or an LXC unit is the one that posts the
session's end. The member process formerly called the worker is now the **link** (`teamchat link`,
`link.sock`, `link.lock`), and it always runs, teams or none. That removes the keeper's reduced
server, the `SCM_RIGHTS` socket hand-over in both directions, the leave-only worker, the
two-writer `conversation.json` and `status.json`, the hook-written `no-worker.json`, `repair` and
adopt mode. The link ignores `SIGTERM`, `SIGINT` and `SIGHUP`, runs in its own session with no
terminal, and the keeper writes nothing to the terminal after the start. The notice at the end is
decided by a table (B 2.11) from how Claude Code ended and the SessionEnd reason. A crash loop is
bounded (`failed` at the 6th exit with no 10-minute run, or at once on 78), and on `failed` the
keeper frees the seat locks. The host posts `disconnected` for any seat whose lock it saw held and
finds free with nothing said in the room, from a host-held bundle (ccy launcher when podman
returns; health tick for every kind). A seat with no store resumes from the earlier of its read
receipt and a host-written `released_ms`.

## Dispositions

| Review, finding                                                    | Disposition                                                      | Change                                                                                                                                                                                                                                                                            |
| ------------------------------------------------------------------ | ---------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| quality 2 (run the worker for the whole session)                   | fixed: B13, B45, 2.1, 2.3, 2.4, 2.8, 2.10, 2.14                  | the link always runs and binds `link.sock` itself under `link.lock`; no keeper server, no hand-over, no leave-only worker; every file one writer; `keeper.json` (keeper-only) carries the link's state for clients that get no answer                                             |
| quality 3 (cut `repair` and adopt mode)                            | fixed: 2.4.5, 2.11, 2.15, BD17                                   | deleted `repair`, adopt mode, `keeper-lost` resume and the socket-facts-on-Stop rule; a keeper death ends the session (BD16) and is recovered by the kind's restart                                                                                                               |
| quality 4 ("worker" is both process and role; drop "bus")          | fixed in B's owned sections; cross-fixer (D names, delivery 2.2) | process renamed **link**; status line and hook text say `teamchat`, never "bus" (2.9, 2.8, 2.12)                                                                                                                                                                                  |
| quality 16 (the one hook-written file)                             | fixed: 2.8                                                       | removed: the hook says its line at SessionStart and on every prompt while the link does not answer, so it needs no record                                                                                                                                                         |
| operations 1 (goodbye killed before it is sent, ccy and LXC)       | fixed: B46, B47, 2.1, 2.4.1-2.4.3, 2.11                          | `run` does not exec Claude Code; the link ignores stop signals; stdio never the terminal; per-kind trace table and launcher contract (20 s container stop timeout, 30 s `TimeoutStopSec`); per-kind VM-lab test in 2.15; unit and launcher settings are cross-fixer requests to C |
| operations 2 (a session that never comes back is never announced)  | fixed: B29, 2.11, BD9; cross-fixer (C)                           | host `seat disconnected` rule by seat lock and room, called by the ccy launcher when podman returns and by the health tick for every kind; the link posts `back` when a run follows a `disconnected` (cross-fixer to A)                                                           |
| operations 15 (crash loop never ends; recovery not named per kind) | fixed: B48, 2.4.2, 2.9, 2.8, BD18                                | 1, 2, 4, 8, 16 s, `failed` at the 6th exit with no 10-minute run, or at once on 78; seat locks freed so the host announces it; recovery templates per `--kind`                                                                                                                    |
| operations 16 (keeper's no-wakeup bar ignores Go's runtime)        | fixed: 2.4.2, 2.14, risks                                        | keeper sets `SetGCPercent(-1)` and a 16 MiB memory limit, so forced GC never runs; the bar counts every thread's context switches; the link's GC is counted in its bar                                                                                                            |
| operations 24 (resident-cost figures disagree)                     | fixed for the member rows; rest not this theme                   | B 2.14 and DESIGN-v2 section 6 member rows agree (keeper and link, teams or none); the Tuwunel and timer figures are C's and D's                                                                                                                                                  |
| operations "outside" (pidfd wording, EL9 kernel)                   | fixed: 2.4.1                                                     | the 5.3 bar matters only for a VM member's kernel; containers and guests run on the host's                                                                                                                                                                                        |
| operations "outside" (BD9 wording in DESIGN-v2 section 7)          | fixed: DESIGN-v2 BD9 row                                         | rewritten for the host's tick                                                                                                                                                                                                                                                     |
| security 1 (released token can skip the next holder's messages)    | fixed: B49, B44, 2.6 step 01 (b), INT1; cross-fixer (C)          | start from the earlier of the read receipt and the bundle's host-written `released_ms`; items at or before it marked `restored`; test row in 2.15; DC16 option (c) and K13 restatement are C's                                                                                    |
| coherence 10 (`teamchat-claude` specified by no chapter)           | fixed by removal: 2.4.1, B 6 C3, D4; cross-fixer (D)             | no separate launcher: every launcher ends in `teamchat run --kind`, the LXC unit runs it in its pane; D drops the row and `~/.config/teamchat/env`                                                                                                                                |
| coherence 16 (C says the keeper posts the goodbye)                 | cross-fixer (C)                                                  | B 2.11 states the link posts every notice; C 3.7.5 and C 7 item 4 follow it                                                                                                                                                                                                       |
| integration to-do, challenges (1) (2) (3)                          | fixed                                                            | (1) the host's tick (2.11); (2) exit 69 at once with the recovery, bounded restarts (2.4.2, 2.4.4); (3) the SessionEnd hook is one local round trip under 300 ms and nothing posted depends on it (2.8, measured in 2.14)                                                         |

Not in this theme and left to their owners: coherence 1 and 2 (LXC apply phases and the kit and unit
mechanism, host fixer), quality 1 and 12 (agent-facing texts, delivery fixer, who may ask for 2.12
edits), security 6 (launcher mount rule, C).

## Rows of DESIGN-v2.md changed (for the merge)

- Seam contract: row **27** (now "announcing a session that ended without its notice", owners C)
  and row **32** (the `released_ms` bound).
- Decisions: **BD1** (the link), **BD2**, **BD9**, **BD11**, **INT1**, and new **BD16**, **BD17**,
  **BD18** after BD15.
- Section 6, the two "per member session" rows.
- Not changed, for the build session: section 2's shape diagram and text still say "worker"; the
  global rename of the process to "link" there, in B 2.2 and B 2.7, and in A, C and D wherever they
  mean the process.

## Cross-fixer requests

1. **D (names):** name the process the **link** (`teamchat link`, internal) and its files
   `<v2-run>/link.sock`, `link.lock`, `keeper.lock`, `keeper.json`, `link.log`, `keeper.log`; no
   user-facing "bus"; drop the `teamchat-claude` row and `~/.config/teamchat/env` (B 6 D4).
2. **Delivery (B 2.2.1, 2.2.2, 2.2.4):** delete the `repair` and `keeper` verb rows; `worker` row
   becomes `link` (run by the keeper only); `run` gains `--kind ccy|lxc|host|other`;
   `seat disconnected --bundle DIR --since MS --seed ID` (run by the ccy launcher and the health
   tick); exit 69 codes `no-session`, `no-link` (restarting, with the restart time), `link-failed`
   (failed or keeper gone, with the recovery), replacing `no-worker` and `worker-failed`; the
   `runtime` record's `"worker"` object becomes `"link"`; 2.7's "worker" becomes "link".
3. **C (host), launcher and unit:** the ccy `entrypoint.sh` execs `teamchat run --kind ccy` as
   tini's one child, with the ccy supervisor as CMD; `podman run --stop-timeout 20`, and the
   launcher's cleanup stop uses at least 20 s; the LXC unit's text (C owns it, and neither chapter
   has it): `ExecStart` a tmux session whose pane runs `teamchat run --kind lxc ...` directly,
   `TimeoutStopSec=30`, the default `KillMode`, `After=network-online.target`, `Restart=` on exit,
   `RuntimeDirectory=` (B 6 C3).
4. **C (host), disconnected:** run `seat disconnected` from the ccy launcher as soon as podman
   returns, whatever the session record says; run it from the health tick for every seat of every
   kind whose lock was seen held and is free (or whose container or guest is not running); keep a
   host-held copy of each LXC and host seat's bundle, owner-only, never mounted (B 6 C4).
5. **C (host), resume bound:** add `released_ms` to `seat.json` (C 3.7.2), written at each take
   from the pool's last release; restate K13 to cover silent skipping and continued reading; weigh
   DC16 option (c), a fresh device per take deleted at release (B 6 C10).
6. **C (host):** `ccy-teams restart <session>` covers an LXC member by restarting its unit (B 6
   C11); homeserver units stop after member containers at shutdown (B 6 C12); C 3.7.5 and C 7 item 4
   say the link, not the keeper, posts the goodbye (coherence 16).
7. **A (protocol):** a subject-posted `back` notice (A 7.4), posted once when a run starts on a
   seat whose latest own lifecycle notice is `disconnected` (B 2.11).

## The delivery fixer's requests to B, taken (from its 261021-n0-fix-delivery.md)

| Request                                                                      | Disposition                                                                                                      |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| 1. drop the awaiting `teams` field; keep HUMAN and TIMEOUT room in `pending` | fixed, B 2.3.2: the example, the sentence and the cap; 256 `pending` entries kept for HUMAN and TIMEOUT (A 11.4) |
| 2. the 2.15 rows                                                             | fixed: the "several teams" row is replaced by "one team per delivery"; a flood-budget-at-admission row added     |
| 3. the 2.4.4 error example                                                   | fixed: `"v":1`, exit 23 `not-found`                                                                              |
| 4. B 5's `"v": 2`                                                            | fixed: `"v": 1`                                                                                                  |
| 5. the gap line                                                              | fixed, 2.6 step 03 and BD15: A 13.2's `gap:` trailing line                                                       |
| 6. `human_text` from the roster                                              | fixed, 2.6 step 10 and B 6 C1 (`human_text` is out of `seat.json`)                                               |
| 7. the 2.2.4 exit-69 paragraph and row                                       | fixed with the link names: `no-session`, `no-link`, `link-failed`; the CLI reads `keeper.json`                   |
| 8. the hook texts in B's form                                                | fixed, 2.8: `teamchat <verb>` in backticks; the recovery commands named as the owner's                           |
| 9. E26's never-restarted codes                                               | fixed, 2.4.2 (Bounded): 78 is `failed (config)`; 10 and 64 never end the link; 8 has no v2 exit of that meaning  |

Not done, since both are in 2.7 (the delivery fixer's): 2.7.6's E26 row still says "worker", and
2.7.3 still has the "list of every team in the delivery" sentence. Both fall to the merge rename.

## For the migration fixer (D 3.2, 3.3), applied by the build session at merge

The member process is the **link**: `teamchat link` (internal, started only by the keeper), with
`<v2-run>/link.sock`, `link.lock` and `link.log`. The keeper is `teamchat run` itself, with
`keeper.lock`, `keeper.json` and `keeper.log`. D 3.2's `teamchat` binary row reads "Static Go member
binary: CLI, keeper (`run`), link, hooks, status line (B)". D 3.3's `teamchat-claude` row goes: every
launcher ends in `teamchat run --kind <kind>`, and the LXC unit runs it in its tmux pane. No
user-facing text says "bus".
