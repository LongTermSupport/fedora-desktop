# SPEC: adding a running session from the teams list, and restarting a team

Plan 00203, Tasks 3.9 to 3.12 (A: 3.9; C1-C4: 3.10; C5, C6 and B: 3.11; C7: 3.12). They were
numbered 3.8 to 3.11 until F44's lxc-checkout task took 3.8. The owner's two requests, condensed:

- "We support adding an LXC session to a team, but we don't seem to support adding an
  already-running ccy session to a team." The add existed (`ccy-teams session add`, and Add to
  a team in the Running sessions view) but neither the team's menu nor a seat's menu led to it.
- "After rebooting, or whenever, I'd like to restart an entire team in one go. Leave the LXC
  side out. The ccy and cc members running directly on the desktop: a single command to
  restart an entire team, using --continue so they all resume."

The second is built to resume each member's own conversation by id, not with `--continue`:
the facts in section 3 show `--continue` picks the wrong conversation or refuses.

## 1. Facts this spec rests on

- F1. `ccy-teams session add <session> <team>[/<seat>]` (`internal/teams/hot`) and the Running
  sessions view's Add to a team (`picker/sessions.go`, team, seat, role) are the one add.
- F2. A ccy session's supervisor can restart it in place: the ccy-lifecycle plugin returns
  `ExitForRestart` at an idle moment, the supervisor types `/exit`, writes
  `.claude/ccy/state/restart-request.json` with the one session id it knows
  (`CLAUDE_CODE_SESSION_ID` in its own PID namespace) and exits 75; the launcher updates the
  image and relaunches with `--resume <id>` (`lib/restart-request.bash`). The host asks for it
  through `relaunch.json` in the session's read-only membership directory (H22's first-join
  relaunch). Every ccy session on the bus has the plugin when its supervisor takes plugins.
- F3. cc has no supervisor: nothing can restart a running cc session but ending it.
- F4. Several ccy sessions in one checkout share one transcript directory
  (`<checkout>/.claude/ccy/projects/-workspace`), so "the most recent conversation in this
  folder" is the newest of all of them.
- F5. (The continue-trap investigation, Claude Code 2.1.303.) `--continue` skips every
  conversation that a non-interactive record in `~/.claude/sessions/*.json` holds, and counts a
  record from another PID namespace as live for ever. Every ccy container is a new namespace,
  so one stale `/background` record makes `--continue` refuse for that conversation's lineage
  for good. `--resume <id>` is not subject to that check.
- F6. Claude Code's SessionStart hook is given the current `session_id` on startup, resume,
  clear and compact: every time the id changes. `~/.claude/sessions/<pid>.json` also names it,
  but under a PID of the session's own namespace, which the host cannot tie to a tmux session
  once the container is gone, and stale files of that kind are what F5's trap is made of.
- F7. A session's seat keeper writes a host-visible report to
  `<checkout>/.claude/ccy/pingbus/sessions/<session>/` (SPEC-hot-control.md 3.3); a ccy
  container sees the same directory under `/workspace`, and a cc session is on the host.
- F8. Restore after a reboot (`ccy-sessions restore`, `registry.RestoreArgs`) appended
  `--continue`. The session record holds no conversation id.

## 2. Adding a running session from the teams list (A)

- A1. A team row's menu has the line **Add a running session to `<team>`**, with no key, after
  Add an LXC container's agent to `<team>`.
- A2. The add lines stay together and ahead of every line that ends or removes something:
  Shut down every session and Take `<team>` down stay after them.
- A3. Choosing it lists every running session `ccy-teams session list` lists: its name, its
  checkout, and each team it is in by the seat's full handle and the role it was told, then
  its state. The screens are Teams › `<team>` › Add a running session, steps 1/3 Session,
  2/3 Seat and 3/3 Role, each with what was chosen so far as its context line.
- A4. A session already in the team is refused with "`<s>` is already in `<team>` as
  `<handle>`: one seat per team per session."; one that cannot be changed while it runs with
  its state. A refused answer is asked again, three times at most (rule of M5).
- A5. After the session, the seat and the role are asked exactly as Add to a team asks them,
  by the same code, and `ccy-teams session add` runs with the words a person would type.
- A6. With no running session, or none that can join, nothing is asked: the line says why
  (each session's reason) and the list comes back. The line is never hidden.
- A7. A seat row whose session is running has **Add this session to another team**, after the
  LXC line and before Remove seat. A seat with no running session does not.
- A8. It is the Running sessions view's Add to a team for that session (team, seat, role),
  its own team refused by A4's words.
- A9. A seat whose session cannot be changed while it runs says so, and asks nothing.
- A10. LXC members get no such line: a hot add changes a session on ccy's tmux, and an LXC
  agent is not one (`lxc-add` is its route).

## 3. Each session's conversation, recorded (C)

- C1. The pingbus SessionStart hook (`helpers/pingbus/hooks.py`), in every session that carries
  the bus kit (every ccy session through ccy's tmux unless `--no-bus`, and `cc --teams`/`--bus`),
  writes `conversation.json` into the session's report directory (F7):
  `{"v": 1, "session": <name>, "session_id": <id>, "source": <startup|resume|clear|compact|other>, "written_ms": <ms>}`,
  atomically, mode 0600. The session name and kind come from the desired membership the host
  wrote; the directory is `/workspace/.claude/ccy/pingbus/sessions/<name>` for ccy and
  `<checkout>/.claude/ccy/pingbus/sessions/<name>` for cc.
- C2. It is written on every SessionStart, so it follows `/clear`, a resume and a compaction.
  A write that fails is one stderr line, never a failed hook.
- C3. Why the hook and not `~/.claude/sessions/<pid>.json`: the hook is told the id at each
  change and names the session through the host's own membership file; the PID file needs the
  session's PID namespace, which is gone after a reboot, and its stale entries are F5's trap.
  Why beside the report and not in the session record: a ccy container cannot reach the
  host's registry, and the record's format is shared with the bash launcher (which may not
  grow, CLAUDE.md's bash limits); the report directory is the one place both kinds of session
  and the host share.
- C4. The host reads it with `conversation.For(record)` (`src-go/internal/conversation`): the
  file must be exactly C1's shape, name the record's session, carry a Claude session id (a
  UUID), and that conversation's transcript must exist (`transcript.Dir`). Anything else is an
  error naming the session and the file, and the way to resume by hand (`ccy --resume`, whose
  picker lists the conversations).
- C5. `registry.ResumeArgs(compose, prefix, args, id)` is RestoreArgs with every recorded
  conversation selector removed (`-c`, `--continue`, `-r`/`--resume` and their value,
  `--session-id` and its value, either side of `--`) and one `--resume <id>` appended, as the
  supervisor's relaunch does. It never adds `--continue`.
- C6. `ccy-sessions restore` starts each record with ResumeArgs and its recorded conversation.
  A record with none is not started: it is a failure that names the session and why, and the
  record is kept. No fallback to `--continue` (F4, F5). (Decision 10.)
- C7. Claude Code's agent view (`/background`, `--bg`, `claude agents`, its daemon) is off in
  the ccy image: `CLAUDE_CODE_DISABLE_AGENT_VIEW=1` (Decision 11). Background sub-agents and
  shell tasks are left on: `disableBackgroundTasks` is not set.

## 4. Restarting a team's desktop sessions (B)

- B1. `ccy-teams restart <team> [--yes]`, and the team row's menu line **Restart the team's
  desktop sessions**, with no key, after Update and before Shut down.
- B2. The members are the team's seat rows on the board (`board.Snapshot`), as `shutdown`
  reads them. Each session record that names a seat of the team is one job, once per session
  (a session on several teams is restarted once, in all of them).
- B3. Left out, and named with why: an LXC member ("restart it inside its container"), any
  other member not run on this desktop, a parked seat (nothing), and a seat no session record
  names ("nothing recorded to restart; Start a ccy session in `<team>`").
- B4. A running ccy session is restarted in place (Decision 8): `relaunch.json` with
  `"why": "restart"` is written into its membership directory; at its next idle moment its
  supervisor resumes it by its own session id on a fresh container (F2). The job is done when
  the session's container has changed and every seat it names is held again. The lifecycle
  plugin says "the owner restarted its team" for this request.
- B5. A running cc session is left running and said ("-", not a failure; Decision 9): cc has
  no supervisor (F3). The line says to `/exit` it and run restart again to start it.
- B6. A stopped session (its record kept, no tmux session) is started as `ccy-sessions restore`
  starts a record: `CCY_SESSION_RESTORE=1 <launcher> ResumeArgs(...)` on ccy's tmux, with its
  recorded conversation (C4). No recorded conversation: ✗, named, and nothing started. Its
  folder gone: ✗. The job is done when every seat it names is held.
- B7. Every request and start is made first, then all are waited for together: a restart in
  place within 300 s, a start within 180 s. A restart in place not done in time withdraws its
  request (so it does not restart later, unasked) and is ✗; a start not holding its seats is
  ✗ ("it may be waiting at a question: attach it, ccy-sessions").
- B8. Before anything is done the plan is shown, one line per session and per member left
  out, and a question is asked, default no; with no terminal, `--yes` is required (exit 64).
- B9. The result is one line per session (✓ done, ✗ failed with why, - left as it is) and per
  member left out, then the count. Each session is failed alone; the others go on. Exit 1 if
  any session failed, else 0. An LXC member left out is not a failure: the owner asked for it.
- B10. Nothing here applies a play, so there is no play lock to wait for.
- B11. Local only: tmux, the registry, the membership directory and `agent-bus seat list`, as
  the user. Nothing goes on the bus.

## 5. Decisions for the owner

Recorded in PLAN.md as Decisions 8 to 11; each runs as stated until the owner decides.

- D8. A running ccy member is restarted in place, at its next idle moment, through its own
  supervisor (B4), rather than skipped. It picks up the current image, keeps its tmux session
  and resumes its exact conversation; a busy session waits for its idle moment (up to 300 s).
- D9. A running cc member is left running (B5). The alternative: type `/exit` into it as
  `shutdown` does, wait, and start it again with its recorded conversation.
- D10. Restore and restart resume by recorded id only (C6, B6). A session that carries no bus
  kit (`ccy --no-bus`, a plain `cc`) records no conversation, so after a reboot it is not
  restored but named, with `ccy --resume` to bring it back. The alternative: a hook for every
  session, not only the bus ones.
- D11. The agent view is switched off in the image's environment (C7), not in the project's
  `settings.json`, which the entrypoint merges and a project may own.
