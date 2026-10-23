# Plan 00225 review r3 (adversarial, Opus)

Scope: `git diff dcb76af56..HEAD` (PLAN.md, DESIGN.md, README row). I checked it against
`helpers/pingbus/` (syncer.py, cli.py, protocol.py), the pingbus SKILL.md,
`files/var/local/claude-yolo/claude-yolo` and `entrypoint.sh`, and the r1 and r2 reports.

## Round 2 findings

- **M1 (a relay hides what the human said): resolved.** LZ6 now shows the whole question:
  every human answer and edit on Q<n>, `answers differ`, and any liaison post in the thread.
  LZ5a forbids liaison posts in a handed-off thread (`handed-off-thread`). After a relay, later
  human messages on that question are delivered directly. Task 2.3 tests all of this.
- **M2 (main-room history): resolved.** LZ12 now says the liaison does not read other members'
  main-room traffic, and builds updates from reports, the questions room, its hand-offs and
  texts written to it. LZ14 drops the `done` ping as a source.
- **M3 (nothing produces `LiaisonActive`): resolved.** LZ10 adds `liaison.json`, written by the
  coordinator's syncer with `read_at` freshness. Tasks 2.1 and 4.2 cover it.
- **M4 (the read-only mount breaks ccy state): partly resolved.** The binds are now `rw` binds of
  the real host paths, but the chosen bind brings a new defect (M1 below).
- **M5 (no version bump for Phase 4): resolved** by Task 4.3.
- **Minors: resolved.**
  - m1: `say --to-liaison` is open to any member.
  - m2: `thread --ask` is handed off like `ask-human`.
  - m3: `wrong-room` is merged into `not-found`.
  - m4: the note has its own key, `liaison_note`.
  - m5: the questions-room join stands in for `human_text`.
  - m6: the check uses `until`.
  - m7: Task 1.0 covers the goldens.
  - m8: Task 2.4 adds the LZ2 notice.

Facts I verified as true:

- The watcher publishes `agent_bus.status` with `WATCH_STATUS_TTL_MS = 600_000`
  (cli.py:1753, 1817), so LZ10's `until` test can be met by a live watcher.
- The coordinator collects members' statuses in `Syncer.statuses` (syncer.py:747).
- The reports room is joined but never synced (syncer.py:454), so LZ12's "syncs the reports
  room's timeline" is a real change. Task 2.1 lists it.
- In ccy the member kit loads with `--plugin-dir` from the image (entrypoint.sh:592), so the
  guard is not in the checkout, as LZ17 says.

## Major

**M1. LZ17's `rw` bind of the whole `.claude/ccy/` leaves the session's configuration writable,
which is the opposite of what LZ17 promises.** `entrypoint.sh:338` runs
`ln -sf /workspace/.claude/ccy /root/.claude`. That makes `.claude/ccy/` the session's Claude
Code user config directory, not just its state:

- `settings.json` holds user settings, which can disable hooks or change `enabledPlugins` and
  permissions.
- `claude-supervise.py` is the wrapper the session runs through (claude-yolo:3634).
- `mounts` holds the tracked per-project binds that are added to the next launch
  (claude-yolo:2535). A liaison could add an `rw` bind of a checkout subpath there, and it
  would apply to every later launch of any role.
- `ccy.env`, `Dockerfile` and `allowed-hostnames` are also there.

LZ17 binds `.claude/ccy/` `rw` by name, and Task 5.1 repeats it ("`.claude/ccy/` among them").
Its test checks only `.claude/settings*`, the hooks and the daemon config. So "the liaison
cannot switch off its own guard" does not hold: user settings and the wrapper both shape what
the session runs.

There is also a conflict to resolve. The entrypoint itself writes
`/root/.claude/settings.json` (entrypoint.sh:347-368) and `/root/.claude.json` at every launch.
So simply marking that file `ro` breaks the launch.

Fix:

- Bind only the measured state subpaths of `.claude/ccy/` (projects and transcripts, history,
  todos, credentials) `rw`.
- Overlay `ro` binds on `settings.json`, `claude-supervise.py`, `mounts`, `ccy.env*`,
  `Dockerfile` and `allowed-hostnames`, or give the liaison an entrypoint-generated settings
  file outside the checkout.
- Say how the entrypoint's own settings writes happen before the file is read-only.
- Make Task 0.1 classify each `.claude/ccy/` entry, and extend Task 5.1's "never writable"
  test to cover them.

## Minor

- **m1. LZ15's git-write list is incomplete.** It misses `pull`, `add`, `rm`, `mv`, `apply`,
  `restore`, `clean`, `worktree add`, `update-ref`, `notes`, and `gh` writes beyond `pr`. That
  matters most on `cc`, which has only the guard. An allowlist of read verbs (`status`, `log`,
  `show`, `diff`, `blame`, `grep`, `branch` with no name, `fetch`) would fail closed instead.

- **m2. The liaison's view of the code may be frozen at launch.** With `.git` read-only, a ccy
  or LXC liaison cannot `git fetch`, so it cannot see branches pushed after it launched. Live
  worktrees inside the checkout are visible. Task 0.1 tests only `git status` and `git log`.
  Test `fetch` too, and say in LZ17 what the liaison can and cannot see (relevant to "read-only
  access to all of the relevant code").

- **m3. The `--update` flag is described two ways.**

  - LZ4a says the liaison's `ask-human` accepts `--update`.
  - LZ13 and Task 2.2 make `--update` the coordinator's hand-off kind, and say the liaison
    delivers updates as a `pingbus report`.

  Choose one.

- **m4. Stale wording.**

  - LD11 still says "a `Liaison's note:` may follow"; LZ5a now uses the `liaison_note` key.
  - The Risks row for the `ro` mount still says "writable per-session dirs", which r2 M4
    replaced with real-path binds.

- **m5. LZ6 does not check who posted the `Q<n>` question.** It should require the post's
  sender to be the team's current liaison, or the coordinator. That closes an odd path: a
  human-posted `Q<n>` that carries a `handed-off:` key.

- **m6. LZ7 calls `verify-human` "the existing rule", but the command does not exist yet.**
  Today's skill (SKILL.md:44-46) confirms through `pingbus history`. Say that `verify-human` is
  new (Task 2.2), and whether `history --room questions` also shows a relayed answer as `in`
  `HUMAN`.

VERDICT: FIX-FIRST
