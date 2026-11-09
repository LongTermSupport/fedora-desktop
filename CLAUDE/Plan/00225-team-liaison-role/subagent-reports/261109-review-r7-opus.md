# Plan 00225 review r7 (adversarial, Opus)

Scope: commit 74149100f (the round-6 fixes) and DESIGN.md / PLAN.md as they stand at that head,
checked against `files/var/local/claude-yolo/{entrypoint.sh,claude-yolo,lib/common.bash,lib/restart-request.bash,lib/token-handoff.bash}`,
the hooks daemon's `untracked/` layout and its `daemon/paths.py`, and
`src-go/internal/teamchat/{proto,member}`.

## Round 6 findings

- **r6 B1 (daemon `untracked/` classed as `rw` state): the rule is fixed, the layout that
  replaces it does not work.** DESIGN:344-346 now says anything executed is configuration.
  DESIGN:356-359 classifies `bin/`, the venvs, `upgrade-*` and `supervise/` as configuration.
  The project's `untracked/` is configuration (DESIGN:360-362). That part is resolved. The
  replacement layout (DESIGN:375-378: per-seat binds "over the daemon's socket, pid, lock, log
  and ledger paths" inside an `ro` `untracked/`) is new blocker B1 below.
- **r6 B2 (LD12 against the launcher's and the entrypoint's reads): mostly resolved.** The
  per-seat directory now holds only user-level files and session state (DESIGN:367-374). The
  tracked launch files are bound `ro` from the checkout over it. `ccy.env.local` keeps its bind.
  `mounts`, `ccy.env.local`, `Dockerfile` and `allowed-hostnames` are read from the checkout
  (DESIGN:387-391). This matches lib/common.bash:426, 704-716, 786 and 852, and entrypoint.sh:450
  and 632. Two gaps remain, s2 and m1 below.
- **r6 s1 (liveness mis-cited): resolved.** DESIGN:269-272 and Task 0.1 now name A 7.4's
  `left`/`disconnected`/`back` (`proto/content.go:70-72`) and presence. The sync filter does
  carry members' presence (`proto/filter.go`, `presence.senders` = humans plus members).
- **r6 s2 (launcher routes): resolved.** The refusal is in Go `launch-mounts` over the final
  selection, with an in-container check and a read-only token for `member/forge`
  (DESIGN:423-435, Task 5.2). `forge/token.go:26` does read `gh auth token`, as stated.
- **r6 s3 (framing window): resolved.** The window starts at the hand-off. It includes main-room
  texts to humans, which the coordinator does receive: the main-room timeline filter is not
  sender-scoped (`proto/filter.go` `syncFilter`). The text says the listing is transparency,
  not a guard, and LZ7 gives the coordinator its rule. The `liaison_note` split is defined, and
  `formatted_body` gives `reworded` (DESIGN:105-115, 138-153, 179-182; Task 2.3).
- **r6 s4 (`unshare`): resolved** (DESIGN:403-411, Task 0.1, Task 5.1).
- **r6 s5 (mirroring both ways): resolved** (DESIGN:202-206).
- **r6 s6 (shared state): resolved for ccy** by the per-seat `.claude/ccy/`. It is not resolved
  for LXC (B1).
- **r6 m1-m7: resolved.** `human_text`, the `teamchat:` deny prefix, `launch-mounts` marked
  new, `--re` gone from LZ11, `git grep -O` added, the Plan 00246 deploy dependency added, and a
  journal entry added.

## Blocking

**B1. The per-seat daemon state layout (DESIGN:375-378, Task 5.1) cannot be built as written,
and LZ20 and a PLAN risk row still describe the old "state `rw` at its real path" model.**

DESIGN:375-378 binds a per-seat directory "over the daemon's socket, pid, lock, log and ledger
paths", one by one, inside an `untracked/` that is otherwise `ro`. Against the tree, this fails
in four ways.

1. **The daemon refuses to start on an `untracked/` it cannot write.** Its bootstrap check
   tests `os.access(untracked, W_OK)` and reports `untracked-writable` as missing
   (`.claude/hooks-daemon/src/claude_code_hooks_daemon/daemon/paths.py:804-811`). With an `ro`
   `untracked/`, the liaison has no hooks at all, and so no guard (LZ15/LZ16 run as hooks).
2. **The paths cannot be known at launch.** The socket, pid, `socket-path` and start-lock files
   and the `events-<id>/` socket directory are named after `socket.gethostname()`
   (`paths.py:63-82, 2580, 2620`). ccy passes no `--hostname` (no match in claude-yolo or lib/),
   so inside a ccy container the hostname is the container ID, which exists only after
   `podman run` has created the container. The launcher cannot compute these mount
   destinations.
3. **Files created at run time cannot be bind targets in an `ro` parent.** The runtime cannot
   create a mount point for a file that does not yet exist inside an `ro` bind. A socket is
   unlinked and recreated by the daemon, and unlinking a mount point fails (`EBUSY`).
4. **Ledgers are written by rename.** The daemon writes its JSON state atomically: a temporary
   file in `untracked/`, then a rename over the target. The temporary file cannot be created
   in an `ro` directory, and a rename over a bind-mounted file fails. Many such files sit flat
   in `untracked/`: the goal ledger, stop events, cron records, the usage snapshot,
   `.venv-bootstrap.*` and `.python-cmd-cache`. A per-file list would also go stale with every
   daemon upgrade.

The same per-path model is still in two other places:

- LZ20 (DESIGN:450-453): `BindPaths=` "binding each measured state path to itself". This is
  the shared-state model r6 s6 removed for ccy, applied to LXC. A systemd `BindPaths=` on a
  source that does not exist fails the unit unless the source is prefixed with `-`.
- PLAN:341, the risk row: "state bound `rw` at its real path", which contradicts LZ17's
  per-seat design.

**Fix:**

- Mount a per-seat directory over the whole of `.claude/hooks-daemon/untracked/`, seeded at
  each launch (or empty, letting the daemon bootstrap into it).
- Bind `bin/` (and every other executable entry Task 0.1 finds) `ro` over it from a trusted
  source. The per-seat venv then simply lives in the per-seat `untracked/`.
- Use the same directory-level rule for the project's `untracked/scratch/`.
- In LZ20, use the same shape (a per-member state directory, `BindReadOnlyPaths=` for the
  executables), and replace the PLAN risk row.
- Task 0.1 should probe that the daemon starts and that hooks fire in a liaison container
  built this way. Today it probes a real session only on the optional LD12 path.

## Should-fix

- **s1. Executed code inside the per-seat `.claude/ccy/` survives the reset.** The reset
  covers only `settings.json`, `plugins/` and `skills/` (DESIGN:381-383). On this tree the
  directory also holds:

  - `shell-snapshots/`, which Claude Code's Bash tool sources;
  - `__pycache__/claude-supervise.cpython-*.pyc`, bytecode of the wrapper the entrypoint runs;
  - `dev-mods/`, `jobs/` and `daemon/`.

  These are configuration by LZ17's own rule ("anything executed ... is configuration even when
  written at run time"). A liaison can plant them for its own next launch. The effect is
  limited to its own seat, so it is not a blocker. Fix: invert the reset so that everything
  per-seat is wiped at launch except a named state allowlist (transcripts under `projects/`,
  todos, `sessions/`, `state/`). Task 0.1's classification of `.claude/ccy/` should cover the
  per-seat copy as well.

- **s2. The host reads that move to the per-seat directory are bash changes, and the plan says
  the launcher does not grow.** DESIGN:387-391 and Task 5.1 point the launcher's reads of
  `state/restart-request.json`, `config.json`, `sessions/` and `.last-launch.conf` at the
  per-seat directory. Those reads are in bash:

  - `lib/restart-request.bash:34`, the `CCY_RESTART_REQUEST_REL` constant relative to the
    project;
  - `claude-yolo:481-507` (`sessions/` and `config.json`; note this is a migration prompt that
    can `rm -rf`);
  - `claude-yolo:540` (`.last-launch.conf`).

  `claude-yolo`, `restart-request.bash` and `common.bash` are on the bash-limits ratchet
  allowlist (`scripts/bash-limits.allowlist:49, 52, 56`), so they may not grow. LZ17's "the
  launcher only appends what Go printed, so it does not grow" (DESIGN:420-421) covers only the
  mount arguments. State how the reads are redirected: one Go-printed directory replacing the
  constant at net zero, or the restart path ported to Go.

- **s3. The sync filter is not named as a change.** `proto.SyncFilter` adds the questions room
  only for the coordinator (`proto/filter.go`, `syncFilter`), and no member syncs the reports
  room. LZ9 needs the liaison on questions and LZ12 needs it on reports. Name `SyncFilter` in
  DESIGN section 8 and Task 1.1 (or 2.1), with a golden that leaves the filter unchanged for a
  team with no liaison (LZ0).

## Minor

- m1. DESIGN:369 lists "the token file the launcher hands every seat" as per-seat state in
  `.claude/ccy/`. No such file exists there:

  - The Claude token reaches the container as `CLAUDE_CODE_OAUTH_TOKEN` in the environment
    (claude-yolo:2297-2305).
  - The GitHub token is a hand-off file under `XDG_RUNTIME_DIR` or `/tmp`, taken on the host
    (`lib/token-handoff.bash:21-63`).

  Drop the phrase, or name the real file if Task 0.1 finds one.

- m2. "`ccy.env.local` ... ordered after the per-seat mount" and "the fixture pins the mount
  order" (DESIGN:371, Task 5.1). Podman orders mounts by destination depth, not by argument
  order. The fixture should pin destinations and options. A nested bind lands after its parent
  anyway.

- m3. PLAN:3, the status line, still describes the round-5 fixes as the current state. Mention
  round 6 and this round.

## v2 and tree facts checked (true)

- A 7.4 lifecycle notices `left`, `disconnected`, `back` (`proto/content.go:70-72`), and
  `member/ends` exists.
- `human_text` (`proto/roster.go:131`, `declare/declare.go:48`).
- The forge credential from `gh auth token` (`member/forge/token.go:26`).
- The entrypoint links `/root/.claude` to `/workspace/.claude/ccy` (entrypoint.sh:343). It
  sources `ccy.env` and `ccy.env.local` (entrypoint.sh:450) and runs `claude-supervise.py` by
  default (entrypoint.sh:632). Its writes are under `/root/.claude` (entrypoint.sh:352-399).
- `mounts` is read on the host (lib/common.bash:426). `ccy.env.local` is bound `ro`
  (lib/common.bash:716). The workspace is bound with `-v "$PWD:/workspace"` (claude-yolo:2465).
- The daemon's container venv is named after the `/workspace` path, and the host's venv sits
  beside it in the same `untracked/`.

## Public repo

Nothing install-specific in DESIGN.md, PLAN.md or the new journal entry. `Owner: joseph` is the
plan-tree convention. This report names the daemon's venvs, sockets and container hostname by
kind only.

VERDICT: CHANGES — 1. B1: the per-seat daemon-state layout binds individual socket, pid, lock and ledger files inside an `ro` `untracked/`. The daemon refuses to start on an unwritable `untracked/` (paths.py:804-811). The file names carry the container hostname, which is unknown at launch. Run-time-created and renamed files cannot be bind targets. LZ20 and PLAN's risk row still bind state `rw` at its real path. Mount a per-seat `untracked/` with the executables `ro` over it, in ccy and LXC alike.
