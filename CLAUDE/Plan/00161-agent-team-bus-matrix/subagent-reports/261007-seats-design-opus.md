# Plan 00161: seats design (one member per ccy session)

Design only; nothing is built. The design itself is in `DESIGN.md` §5.5 (seats), §5.6 (the
IaC), §12 "U20" (the acceptance in this checkout), the U29-U32 rows of §12, the M2 milestone
row, D32-D39 and "Owner questions". This report records how the owner's three decisions of
2026-10-07 were turned into that design, what was checked in the code to do it, where the
coordinator's working assumptions held and where they did not, and what each existing unit
must change.

## The owner's decisions and where they landed

| Decision                                                                                                                                                 | Design                                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1. One seat per session, numeric or role-based ("{repo}-dev, {repo}-audit, {repo}-pm")                                                                   | §5.5, D32: a seat is a member identity a checkout offers; one session holds it; the handle carries it as `<repo>.<seat>+<host>.<type>`                  |
| 2. A play driven by host_vars writes `PINGBUS_TEAMS` and the seat config into `ccy.env.local` and installs the bundles under `.claude/ccy/pingbus/`      | §5.6, D33: `agent_bus_ccy_checkouts` in host_vars; `play-agent-bus.yml` renders a spec and runs `agent-bus-install seats`, which writes both            |
| 3. U20 in the real local fedora-desktop checkout, several sessions at once, through the IaC path, the acceptance team, team and seats removed afterwards | §12 "U20", D34: three named-only seats of the acceptance team in `PLAN_REPO_ROOT`, provisioned and removed by the play, three headless sessions at once |

## What was read, and the facts the design rests on

- **Handle grammar** (`docs/agent-bus-protocol.md` §3, `protocol.py`): `<repo>.<n>+<host>.<type>`,
  `<n>` = `[1-9][0-9]{0,5}`, `<repo>` = `[a-z0-9][a-z0-9_-]{0,47}`. `<repo>` contains no
  `.`, so a seat can sit in `<n>`'s place, between `.` and `+`, unambiguously. A role name
  starting with a letter is disjoint from a number, so one alternation keeps every existing
  handle valid.
- **Registry** (`helpers/agent_bus/registry.py`): already uses the word "seat" for the
  counter key `<repo>+<host>.<type>`; `<n>` is never reused because the counter only goes
  up, but a removed handle is not recorded anywhere else. Named seats need an explicit
  `retired` list, so registry v2.
- **`add-member`** (`admin.py`) refuses a handle that already has an account; a Tuwunel
  account once deactivated is not assumed to be re-creatable, which is one more reason a
  removed seat comes back under a new name.
- **Locks** (`inbox.py`): `flock`, the holder's kind written into the file, `probe_lock` by
  a non-blocking `flock`, never a PID. Per team, held only by a running waker or briefly by
  `recv`.
- **The entrypoint** (`files/var/local/claude-yolo/entrypoint.sh`, AGENT-BUS block):
  `PINGBUS_TEAMS` only from `ccy.env.local`, `PINGBUS_HOME` defaults to
  `/workspace/.claude/ccy/pingbus`, `config check` refusal, plugin args added after
  `claude`; the script ends in `exec "${_ccy_wrapper[@]}" "$@"` or `exec "$@"`, so a
  descriptor opened before that `exec` is inherited by the session.
- **ccy runs several containers per checkout** (`get_next_container_name`:
  `<project>_yolo`, `<project>_yolo_2`, …), all labelled with the same `ccy-project`. U20's
  current teardown removes containers by that label, which in the owner's checkout would
  also remove the owner's own sessions: hence the new `ccy-seat` label.
- **Quick Launch** (`claude-yolo`): taken only when no token, SSH or network flag is given;
  it then `read`s a Y/n answer from stdin unless the launch is a session restore. A headless
  session fed through a fifo would have that `read` consume its first stream-json line.
  Headless launches skip SSH key unlocking entirely (`_probe_unlock_keys`).
- **This checkout's `.claude/ccy/ccy.env`** exports `CCY_CLAUDE_WRAPPER` with the supervisor
  armed, and the checkout runs the hooks daemon; neither was present in U20's throwaway
  checkouts.
- **U20 as built** (`_acceptance-u20.inc.bash`, journal 26-10-07 14:06): member c had no
  socket through `CLAUDE_CODE_HARBOR_KITE=0` in a `ccy.env` the harness wrote; transcripts
  were collected with a glob over `.claude/ccy/projects/*/*.jsonl`; M2.1 required exactly
  one turn before the review.

## The coordinator's working assumptions

| Assumption                                                                                                   | Verdict                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| A named seat via a ccy flag (`--seat dev`) passed in as an env var, else the first free seat                 | Kept: `--seat` → `PINGBUS_SEAT`. Added `PINGBUS_NAMED_SEATS`: seats an unnamed launch never takes. Without it, any of the owner's sessions in this checkout (a restarted one included) could take an acceptance seat during U20, and the owner could not keep a `pm` seat for explicit use only.                                                                                                                                                                                                                                               |
| Claiming by the existing sync-lock machinery (`inbox.py`, protocol §12/§14)                                  | Partly wrong. The sync lock is per team and held only while a waker runs, so it cannot mark a seat as taken from a session's start to its end, nor cover a seat in several teams. The design adds a seat lock (`seats/<seat>/seat.lock`) on the same primitive (`flock`, kind in the file, no PID; U30 moves `acquire_lock`/`probe_lock` onto a path) and exit 75 for busy.                                                                                                                                                                    |
| Released at session end                                                                                      | Held, by construction: `pingbus seat exec` takes the lock and `execvp`s the session with the descriptor inheritable, so the lock lives exactly as long as some process of the session holds it; in ccy that is the container (`--rm`, PID 1 is the session). Survives `/clear`.                                                                                                                                                                                                                                                                |
| No free seat: start without the bus and say so, or refuse                                                    | Refuse (D36). Starting without the bus in an opted-in checkout, with a message, is the "skip and warn" pattern CLAUDE.md bans, and produces a session nobody can ping. The refusal names each seat's state and the bypass `ccy --no-bus`, which is the pattern `--no-supervise` already sets.                                                                                                                                                                                                                                                  |
| Headless launches reuse `.last-launch.conf` (`LAST_TOKEN`, `LAST_SSH_KEYS`) without prompting, never secrets | Kept, as a ccy change (U31, D38): `--headless` with no launch-choice flag takes the Quick Launch choices without the prompt, as a restore does, and is refused when there are none. The harness then passes no token or key at all. Passing `--token`/`--ssh-key` explicitly instead would skip Quick Launch and run SSH discovery, which can show a menu reading the session's stdin. A key needing a passphrase cannot be unlocked headless, so U20 checks before launch that every key is usable with no prompt and fails naming `ssh-add`. |
| host_vars shape `agent_bus_ccy_checkouts: [{path, teams: [{team, seats: [dev, audit]}]}]`                    | Kept, with `host` per checkout (the handles' `<host>`) and per-seat mappings for `role` and `named_only`. A simpler per-checkout `teams` + `seats` shape was considered and rejected: the acceptance must provision one team's seats in a checkout without touching another team's, which needs seats per team.                                                                                                                                                                                                                                |

## Decisions the design had to make beyond the assumptions

- **Seat names** `[1-9][0-9]{0,5}` or `[a-z][a-z0-9]{0,11}`; no `-`, `_`, `.`. The owner's
  `{repo}-dev` is rendered `<repo>.dev+<host>.podman`; a `-` separator would be ambiguous
  because repository names contain `-`.
- **Protocol version stays 1** (D37, owner question 1). The §1 rule makes a grammar change a
  new version, but v1 has run only in the acceptance team, which is purged per run; every v1
  handle stays valid; an old pingbus facing a role seat in the team record fails loudly
  (room untrusted, exit 10).
- **Never reissue a handle** (D37, owner question 2): registry v2 `retired`.
- **Per-seat `PINGBUS_HOME`** (`.claude/ccy/pingbus/seats/<seat>/`): the bundle format,
  pingbus's state, the watcher, the sync locks and the Stop guard are reused unchanged; only
  `seat exec` and status/hook additions are new code. The pre-seats layout is refused
  rather than supported twice.
- **The seat record lives in `ccy.env.local`'s managed block**, which ccy binds read-only, so
  a session cannot add seats or change which teams a seat is in. Three variables:
  `PINGBUS_TEAMS` (kept as the opt-in marker), `PINGBUS_SEATS='<seat>=<team>[,…] …'`,
  `PINGBUS_NAMED_SEATS`.
- **The IaC never writes `HOOKS_DAEMON_HOSTNAME`**: the acceptance's handles use host
  `acceptance` in the owner's own checkout, and writing the role variable would have changed
  the owner's hooks-daemon role.
- **The logic sits in the installer** (`agent-bus-install seats`, decisions in Python
  `agent-bus render seats`), the play only calls it (D2), so other projects can provision
  their ccy checkouts the same way.
- **The installer scopes a run to the teams its spec names** and rebuilds the block from the
  previous block plus the spec, so U20 cannot disturb the owner's own seats in the checkout.
- **M2.3 stops a live watcher** instead of starting a socket-less session (D39): the old
  switch needed a tracked `ccy.env` written into the real checkout and an undocumented
  Claude Code variable.

## The U20 rework

The full design is in DESIGN.md §12 "U20"; the parts most likely to bite the builder:

- **Containers by `ccy-seat` label only.** The project label matches the owner's sessions.
- **Transcripts by session ID**, moved out of `.claude/ccy/projects/` into the run
  directory; the owner's own transcripts are never read.
- **The hooks daemon runs in these sessions.** Its Stop handlers want `STOPPING BECAUSE:` and
  may add turns, so the orders end every turn with that phrase, and no check rests on a turn
  count alone (M2.1 judges the notice preceding the `recv` turn, and one line of harness
  input).
- **The checkout must come out unchanged** (M2.7): `ccy.env.local`, `.claude/ccy/pingbus/`,
  `git status --porcelain=v1` and `HEAD` compared with a record taken before M2.0. The
  sessions run with bypass permissions in the owner's working tree.
- **To confirm first:** `--no-supervise` against this checkout's `ccy.env`, which exports an
  armed `CCY_CLAUDE_WRAPPER`; and that haiku follows the orders under this repository's
  `CLAUDE.md` and hooks daemon.
- **The unnamed-launch refusal is not run on the host**, because an unnamed launch could take
  one of the owner's own seats there; U30 and U31 prove it in the container with real
  `flock` between processes.

## Effect on other units

| Unit                   | State              | Effect                                                                                                                                                                                                                 |
| ---------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| U12 hooks, status      | built              | Mechanism unchanged (each seat is its own `PINGBUS_HOME`). U30 adds the seat line at SessionStart, `SEAT` lines to `status`, and rewords `seat_held`.                                                                  |
| U14/U15 registry/admin | built              | U29: registry v2 (`retired`, explicit seats, the counter key renamed `prefix`), `add-member --seat`.                                                                                                                   |
| U18 plugin, skill      | built              | U30: the skill's seats section; `test_plugin_contract.py` covers the new commands it names. Hooks file unchanged.                                                                                                      |
| U19 ccy entrypoint     | built              | U31 reshapes the AGENT-BUS block: the three variables, `seat exec` in front of the final `exec`, `--no-bus`, the pre-seats layout refused; dist text version 3; ccy and container version bumps.                       |
| U20 M2 acceptance      | built, superseded  | Reworked for this checkout, three seats, the play; needs U29-U32. Its throwaway-checkout code (`u20_make_checkouts`, `u20_add_members`, the `HARBOR_KITE` member, removal by project label, the transcript glob) goes. |
| U21 docs, READMEs      | built              | U32 updates `docs/agent-bus.md` and `README.podman`: ccy members are provisioned by the play; seats; `--seat`, `--no-bus`; another project re-copies the kit before a role seat joins.                                 |
| U22 play               | built, host-run    | U32 adds the `agent_bus_ccy_checkouts` assertions and the `seats` step after the team installs; its existing deploy legs are unchanged.                                                                                |
| U23 other encaps.      | built, run pending | No change: non-ccy members keep counter-numbered handles. Its members run the pingbus of the same build, which accepts the wider grammar. Its host run does not wait for U29-U32.                                      |
| U27 privacy checks     | not built          | P8 covers the seat bundle paths; P5's secret scan covers the acceptance seats' transcripts.                                                                                                                            |
| U16, U17, U24-U26      | -                  | No change.                                                                                                                                                                                                             |

Build order: {U23, U29} → {U30} → {U31, U32} → {U20} → {U24} → {U27} → {U28}.

## Open questions for the owner

Each with the answer the design assumes; also listed in DESIGN.md "Owner questions".

1. **Widen the handle grammar inside protocol v1** rather than release v2? Recommended: yes
   (no team but the purged acceptance team has run v1; every v1 handle stays valid).
2. **Never reuse a removed seat's name in a team** (`dev` returns as `dev2`)? Recommended:
   yes, as `<n>` already is; old pings and forge text then never name a new holder.
3. **Seats in a team hosted on another host**: left to that host's IaC, refused by this
   play? Recommended: yes for v1; nothing asks for it yet.
4. **Success criterion 1** said "two ccy sessions in different projects"; U20 now runs in one
   checkout. Recommended: reword to several sessions in one checkout, each on its own seat
   (done in PLAN.md, pending your confirmation); members of different repositories are
   already proven by M1 and U23.
5. **U20's sessions use your SSH choice** (`LAST_SSH_KEYS`) although they commit nothing, and
   a key needing a passphrase fails the run before launch (load it with `ssh-add` first).
   Recommended: yes, so the sessions are launched exactly as you launch them.
