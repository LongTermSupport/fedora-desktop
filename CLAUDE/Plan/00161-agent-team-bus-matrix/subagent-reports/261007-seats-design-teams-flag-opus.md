# Plan 00161: seats design, `--teams seat@team`

Design only; nothing is built. The owner's two decisions of 2026-10-07 replace the
`--team`/`--seat` design and the owner prerequisite on `HOOKS_DAEMON_HOSTNAME`.

DESIGN.md changes: the intro, the placeholders, §1, §2 (Member, Multi-team), §5.1, §5.2
(the podman row, the role variable, one session per seat), §5.3, §5.5 (rewritten), §5.6
(rewritten), §6 (SessionStart context), §9, P8, §11, §12 (U20, U29-U32 rows, M2, section
"U20"), D18, D19, D32, D34-D36, D41, D44, D45 (withdrawn), the new D48-D50, and "Owner
questions". PLAN.md changes: the overview, Task 1.8 (now points forward), the new Task 1.10,
M2, and success criterion 1.

## Decision 1: one flag, a seat per team

| Point                   | Design                                                                                                                                                                                                                                                                                                                                                                                                                |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Flag                    | `--teams <seat>@<team>[,<seat>@<team>…]`, also `--teams=<list>` (ccy already accepts `--update-token=NAME`). `--team` and `--seat` no longer exist, and there is no `--no-bus`.                                                                                                                                                                                                                                       |
| Whitespace              | Spaces and tabs around an item are dropped, so a quoted `'dev1@dev-team, qa2@other-team'` works. The list is always one argument. The owner's unquoted spelling reaches ccy as `dev1@dev-team,` followed by a stray word that would go to `claude` as a prompt. Its trailing comma is therefore refused (64), and the message shows the list without the space, or quoted. Refusing is safer than joining argv words. |
| Usage errors, exit 64   | Each is raised before anything runs. The cases: no value; `--teams` given twice; an empty list or item (leading, doubled or trailing comma); an item without exactly one `@`; a bad seat or team name; and a team named twice, whether with two seats or the same seat.                                                                                                                                               |
| Where the checks run    | The launcher's Bash catches only a missing value and a second `--teams`. Every grammar rule lives in pingbus's `seat.py` and runs on the host through a new `agent-bus seat check <list>`. That command changes nothing, prints the canonical list, and is called right after argument parsing. The launcher's Bash therefore holds no copy of the grammars.                                                          |
| Other exits             | 78: `agent-bus` is missing, a team is not active on this host, the checkout is unusable, or a seat cannot be created. 75: a seat is held, either on the host before the container starts or by the claim inside it.                                                                                                                                                                                                   |
| Identity                | A seat belongs to one team, so `dev1@a` and `dev1@b` are two members. The layout is `.claude/ccy/pingbus/seats/<team>/<seat>/`, holding the bundle and `seat.lock`.                                                                                                                                                                                                                                                   |
| Seat creation           | `agent-bus seat take <canonical list>` runs just before the container starts. It makes one `sudo agent-bus add-member --seat` for each missing seat (`sudo -n` when headless). It creates nothing if any named seat is held, and runs no sudo when every seat already exists.                                                                                                                                         |
| What the container gets | One variable, `PINGBUS_SEATS=<canonical list>`, plus the label `ccy-seats=<canonical list>`; restart and restore get `--teams <canonical list>`. The entrypoint refuses a bus variable that `ccy.env` or `ccy.env.local` sets or changes, and refuses a `PINGBUS_TEAMS` or `PINGBUS_HOME` that comes from the launcher (that would mean an old launcher).                                                             |
| Claim (`seat exec`)     | It takes every seat lock without waiting, and any held seat is exit 75. It then builds the session home `/tmp/pingbus-home` inside the container: one `<team>` symlink per seat, the Stop guard memory and `watch.log`. It sets `PINGBUS_HOME` and `PINGBUS_TEAMS` and runs `config check`.                                                                                                                           |
| Durability              | D34, D40 and D43 now apply per (team, seat): park on `seat remove <seat>@<team>`, return by name, and history read from the room. All durable state (inbox, outbox, sync position, room view, sync lock) stays in the seat's directory.                                                                                                                                                                               |
| Status and hooks        | `SEAT` lines read `SEAT team seat held/free self/- handle`. At SessionStart a session sees its own seats plus the other seats of its own teams; seats of teams it is not in are left out.                                                                                                                                                                                                                             |
| Lowest-free numbering   | Removed (YAGNI): the owner always names `<seat>@<team>`, so nothing calls it any more. Numbers stay valid seat names, because the registry's counter issues them to non-ccy members and every pre-U29 handle must still parse.                                                                                                                                                                                        |

Why the session home is made of links (D50): pingbus reads a team's bundle at
`PINGBUS_HOME/<team>/`, and a session's seats now sit in different teams' directories.
Links leave the bundle format, the state layout and the multi-team model unchanged. The two
per-session files describe the session, so it is right that they die with it. For that
reason U20 copies `watch.log` out with `podman exec` before it closes each session's input.
`config check` gains one rule: a symlinked team entry is accepted only when it points at a
real directory owned by the user with mode 0700.

## Decision 2: `<host>` defaults to `local`

- The rule: `HOOKS_DAEMON_HOSTNAME`, where the checkout's `ccy.env.local` assigns it, is
  parsed on the host and never sourced. In every other case, including when the file does
  not exist, the value is the literal `local`. The launcher passes it to `add-member` as
  `--host`.
- `local` matches the existing `<host>` grammar, so the protocol grammar does not change
  (D37). U29 only names `local` in §3's handle-building rules.
- It names no machine, so D19 stands.
- In v1 every member of a team runs on one machine (D46), so `local` cannot collide. A
  later-phase team spanning machines would hit `add-member`'s refusal of a current handle,
  which is loud.
- The prerequisite "`ccy.env.local` must assign `HOOKS_DAEMON_HOSTNAME`" is removed from
  U20 and everywhere else. The rule that refused a seat whose existing handles carry a
  different `<host>` is dropped too: it only made sense while one seat spanned teams.
- A known edge, documented in §5.5 "Return": a seat that was removed and then returned
  after the role changed gets a new handle, while the old one stays parked. The launcher
  prints the handle before it creates the account.

### Does the handle still need four parts?

Yes. The handle is a localpart on its own team's homeserver, so it needs no team part.
`<repo>` separates repositories in a team, `<seat>` separates a checkout's sessions,
`<type>` separates encapsulations, and `<host>` separates machines in the later phase. The
only grammar change is U29's existing widening of `<seat>`.

## Units

| Unit | Effect                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U29  | Adds a single line: §3 names `local`.                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| U30  | Gains the list parser and its canonical form, the claim of several locks, the session home of links, and the symlink rule in `config.py`. Loses lowest-free picking. `PINGBUS_SEAT` becomes `PINGBUS_SEATS`.                                                                                                                                                                                                                                                                          |
| U31  | `--teams` replaces `--team`/`--seat`. Adds `seat check` (after argument parsing) and `seat take` (before launch), and the `ccy-seats` label.                                                                                                                                                                                                                                                                                                                                          |
| U32  | Adds `seat check`. Every command takes a list. Adds the `<host>` rule with `local`. Drops seat picking and the cross-team `<host>` refusal.                                                                                                                                                                                                                                                                                                                                           |
| U33  | No change, apart from wording: history is per seat, and `--team` selects one of the session's seats.                                                                                                                                                                                                                                                                                                                                                                                  |
| U20  | Uses seats `acca`, `accb` and `accc` at `@acceptance`. M2.5 now tests a held seat (75), two seats of one team (64) and a trailing comma (64), with nothing changed. The numbered-seat check (old M2.9) is removed. A plain `ccy` is M2.9, and the checkout-unchanged check is M2.10. Cleanup copies `watch.log` out and runs `seat remove` with a list. The `HOOKS_DAEMON_HOSTNAME` prerequisite is gone. A leftover `seats/acceptance/` from an interrupted run is deleted at start. |

No unit is dropped. The build order is unchanged: {U23, U29} → {U30, U33} → {U31, U32} →
{U20} → {U27} → {U28}.

## Owner questions

None. Two choices I made myself, each recorded in the design:

- The whitespace rule: tolerate spaces inside a quoted list, and refuse the unquoted split.
- The session home made of links (D50).
