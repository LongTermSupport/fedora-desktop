# M2 pre-merge review (Fable): the launcher and the entrypoint

Branch `feat/hot-control-m2-launcher` at `235c8cc2`, diffed against `F44` (`4cdd2d3b`, which
holds M0, CCY 3.98.12, container 2.67). Merge-base `2e164edf`, before M0. Read-only; no QA
run. Developer notes: `subagent-reports/261016-m2-launcher-opus.md` on that branch. Spec:
[SPEC-hot-control.md](../SPEC-hot-control.md) revision 2.

## Verdict: FIX-FIRST

Blocking, one line each:

1. **Decision 1 is broken and PLAN.md still records it as the condition of the whole kit-in-every-session choice**: "no `crossSessionInbound` accept unless the session is on a team" (PLAN.md Decisions 1) is now false for every plain session, and Task 2.3 is ticked complete under it. The owner's decision must be re-taken explicitly (and the plan, H33b and docs/ccy.md's "differ only by the mount" sentence corrected), or the safer fallback in section 1.4 built.
2. **Container version**: the branch bumps 2.66 → 2.67, F44 is already at 2.67 with a different Dockerfile; a host that deployed M0 relaunches into "DEVELOPER ERROR: Dockerfile modified without version bump", and the entrypoint/zipapp/plugin changes ride on the hash check, not the version. Bump to 2.68 (and the acceptance (d) text) when resolving the `claude-yolo` conflict.
3. **Plan 00201 and Plan 00161 acceptance scripts are now false**: confirmed (section 5). They are not queued, so nothing will surface it; either port them to read `PINGBUS_MEMBERSHIP` plus the host membership file, or mark them superseded in their plans in the same merge.
4. **`acceptance.bash` add/add conflict with `origin/fix/teams-server-robustness`**: both branches create the plan's one `acceptance.bash` for different milestones; this needs a real reconciliation (one dispatcher sourcing `_acceptance-m2.inc.bash` and `_acceptance-steps.inc.bash`), not a pick-one resolution. Same for the JOURNAL day-file (union, time-ordered) and PLAN.md.

Non-blocking findings follow each section.

## 1. Security of inbound accepted in every kit session

### 1.1 What a plain, non-team session now accepts

Before M2 a plain ccy session ran `claude` with no `--settings`, no plugin and (per the U20
builder's finding on Claude Code 2.1.292) no inbox socket: with the cross-session gate off,
`CLAUDE_CODE_MESSAGING_SOCKET` is unset. After M2 every session started in ccy's tmux without
`--no-bus` is launched with:

- `--settings /tmp/pingbus-home/settings.json` carrying `crossSessionInbound: accept`, so Claude
  Code binds its inbox unix socket and exports `CLAUDE_CODE_MESSAGING_SOCKET` and
  `CLAUDE_CODE_MESSAGING_TOKEN`; a message written to that socket with the token is entered into
  the conversation as a **user turn** (or a `queued_command` attachment mid-turn) and starts a
  turn in a bypass-permissions session (that is exactly what `accept` buys: without it the
  delivery is held);
- the pingbus plugin (`--plugin-dir`), every hook guarded on `$PINGBUS_HOME/active`;
- the SessionStart inert branch, which writes a second copy of the token to
  `/tmp/pingbus-home/socket.json` (0600, container-private);
- a status line whose command is the shell guard, replacing the session's own `statusLine`
  with a wrapper that `exec`s it.

No pingbus process runs; nothing the kit starts uses the socket until a seat is applied.

### 1.2 Who can send

Anyone who can connect to the socket path and present the token.

- **Inside the container**: every process. The container runs the agent as root with
  `--dangerously-skip-permissions`; the token is in `/proc/<claude pid>/environ` and in
  `socket.json`. "Every process" means the agent, its tool calls, anything it `npm install`s or
  `pip install`s, any script a cloned repository ships, the hooks daemon's handlers, any
  project hook. That is the population a sandbox exists to contain.
- **From the host**: the owner, through `podman exec`. Not new and not a concern.
- **From another container on the same checkout**: depends on where Claude Code puts the
  socket. The repository does not record the path. `/root/.claude` is a symlink into
  `/workspace/.claude/ccy` (Dockerfile note), which is bind-mounted and shared by every ccy
  container of that checkout; if the socket lives under `~/.claude`, a sibling container can
  connect to it (same uid mapping). The token is not on the shared mount by anything this
  repository writes, but whether Claude Code's own peer registry stores it there is unknown.
  **Probe before relying on "container-private":**
  `podman exec <c> sh -c 'echo $CLAUDE_CODE_MESSAGING_SOCKET'` and check whether the path is
  under `/tmp` or under `/root/.claude`.

### 1.3 Is this an injection route a plain session never had?

Yes. Before M2 a plain session had no socket at all. Now any code the agent runs in the
container can forge a message that arrives with **user authority** and starts a turn. The
spec's wording ("the agent could inject into its own session, which it can already do") is
true but undersells it in two ways:

- it is not only the agent; it is everything running under the agent's uid in the container,
  including supply-chain code the agent did not write and did not read;
- the pre-existing routes (edit CLAUDE.md, edit `.claude/settings`, write the transcript) are
  file edits that take effect later and look like file edits in the tool log; the socket is
  a live channel whose output is indistinguishable from the owner typing.

Severity: **medium**, bounded to the container, which the agent already fully controls; the
marginal power is authority laundering (tool output → user turn) and immediacy. It is not
acceptable by default because Decision 1 made its absence the condition of loading the kit in
every session. It may be acceptable if the owner re-decides it with that stated.

### 1.4 Safer fallbacks

1. **Join-requires-relaunch (recommended).** Launch a zero-seat session with the kit but
   **without** `crossSessionInbound` (no socket). `ccy-teams session add` on a session whose
   membership was empty at launch goes through the existing restart path
   (`lib/restart-request.bash` and the registry replay, which already carries `--teams`); every
   later change is hot. Decision 1 holds exactly; the cost is one restart on the first join of
   a plain session, which is what the owner has today. The keeper (M1) still needs no settings
   rewrite. The `active` flag, the guard and the mount are unchanged.
2. **Static accept plus a hook that drops cross-session input while the membership is empty.**
   Not available as described: a socket message is written as a user entry or a
   `queued_command`; whether `UserPromptSubmit` fires for it and whether its payload names the
   source is not established in this repository, and a hook is a filter after the socket has
   accepted the write, not a wall. It would need its own spike before anyone relies on it.
3. **A second spike on live reload.** The M2 spike tested only the `--settings` flag file.
   Claude Code watches some settings sources for changes while running; whether
   `crossSessionInbound` in a watched source (the user file, here
   `/workspace/.claude/ccy/settings.json`) takes effect live was not tested. If it does, the
   keeper can switch it on the first add with no relaunch. Worth one probe; not a dependency.
4. **The internal gate variable** the U20 builder found (`CLAUDE_CODE_HARBOR_KITE=0`) is an
   internal name pinned to one Claude Code version. Not a mechanism to build on.
5. **If accept stays:** do not write `socket.json` in the inert branch (a second on-disk copy
   of the token serves nothing until a seat exists; SessionStart can record it when `active`
   appears, or the keeper can), and name the exposure in docs/ccy.md as "anything running in
   the container", not "the agent".

## 2. Mount semantics

Correct. `-v "$dir:/run/ccy/membership:ro${CCY_MOUNT_RELABEL:+,Z}"` is a **directory** bind
mount of `$XDG_RUNTIME_DIR/ccy/membership/<session>/`. `fsx.WriteAtomic` writes
`membership.json.tmp.<pid>` in the same directory and renames over `membership.json`, so the
container sees each generation atomically through the directory inode; the new file inherits
the directory's SELinux label (`Z` relabels the directory private to the container at start),
so later generations stay readable. `:ro` gives EROFS on `touch`; ccy adds no `--privileged`
or `--cap-add`, so the acceptance's remount leg is meaningful. Directory and parents are
0700 with `Lstat` symlink refusal; rootless podman maps the owner to container root, so it
reads. Nits: the `.tmp.<pid>` file is briefly visible inside the mount (readers only open
`membership.json`, fine); unnamed `--teams` launches leave one directory per container name
under the runtime dir until logout (harmless, tmpfs).

## 3. Entrypoint refusal; old images, old launchers, restored sessions

- The entrypoint runs only at container start, so **running sessions are untouched**.
- **New launcher, old image**: the image is rebuilt before any launch by
  `validate_container_version` (version or hash mismatch both return 1 and rebuild). On a host
  already at 2.67 from M0 the trigger is the **hash**, with the "DEVELOPER ERROR" banner
  (blocking item 2). Acceptance (d) attributes the protection to `REQUIRED_CONTAINER_VERSION`,
  which on such a host is not what fired.
- **Old launcher, new image**: refused with "ccy on the host is older than this image" for the
  unset marker and for a leftover `PINGBUS_SEATS`/`TEAMS`/`HOME`. The play deploys launcher
  and build context together, so this is only reachable with a stale copy of ccy; the refusal
  is the right behaviour.
- **Restored sessions** replay through the new launcher inside ccy's tmux, so they get the
  label, the membership and the marker; `--no-bus` is kept in both flag lists (see 4).
- The "command is not claude" refusal is not a regression: the launcher always runs
  `claude --dangerously-skip-permissions …` (claude-yolo line 3922).
- **New regression surface**: `inner_status_line` refuses (78) a session whose
  `.claude/settings.local.json`, `.claude/settings.json` or user `settings.json` is not JSON.
  A plain session that started yesterday in such a checkout will not start today, with a
  pingbus error. Fail-fast is the project rule, but this is a new way for a non-team session
  to be refused; the message names the file, which is adequate. Note it in the changelog.
- D62's snapshot covers `PINGBUS_MEMBERSHIP` (every `PINGBUS_*`), so a `ccy.env` cannot forge
  the marker. Good.

## 4. `--no-bus` replay on restore

Kept: `CCY_REGISTRY_KEEP_FLAGS` and Go `keepFlags` both carry `--no-bus`, pinned equal by
`FlagClassAgreesWithBash`. One case to check before merge: a `--no-bus` record in a checkout
whose saved Quick Launch choices include a team. Unattended launches take the saved choices,
`ccy_no_bus_check` runs again for the Quick Launch team, and the restore exits 64 for that
session. If a `--no-bus` launch saves "no team" into the checkout's choices this cannot
happen; if a later `--teams` launch in the same checkout updates them, it can. One test in
`scripts/test-ccy-session-registry.bash` or `test-ccy-teams.bash` would settle it.

## 5. Plan 00201 and 00161 acceptance scripts

Confirmed stale, both ways:

- `00201/_acceptance-m7.inc.bash` lines 179 and 472 find containers with
  `--filter label=ccy-seats`; `m7_check.py expect-plain` asserts no `PINGBUS_` variable on the
  plain container. The seated legs find nothing; P1 fails on `PINGBUS_MEMBERSHIP=off` (its
  sessions are headless outside ccy's tmux, so `off` is what they get).
- `00161/_acceptance-u20.inc.bash` lines 135 and 363 find seats by `ccy-seats`; line 988's
  plain leg still passes (a headless plain launch outside tmux gets `off`, no kit, no label).
  Its seated sessions, headless with `--teams` outside tmux, get a membership named after the
  container and **no label at all**, so nothing on the host can find them by label any more.

Neither is in `meta-deploy.list`, so nothing will run them and nothing will report the rot.
Port them to `podman inspect` `Config.Env` (`PINGBUS_MEMBERSHIP`) plus the host-side
`membership.json`, or mark both plans' acceptance as superseded by 00203's, in this merge.

## 6. Can `acceptance.bash` start its own sessions?

Yes, and it should. `ccy_tmux_current_session` names a session from `$TMUX` when the socket
basename is `ccy` (`CCY_TMUX_SOCKET`), and `ccy_tmux_insulate` is a no-op already inside that
server. So:

```
tmux -L ccy new-session -d -s m2-plain -c <checkout> -- \
    ccy --headless --no-restore --no-supervise --prompt '…' -- --input-format stream-json …
```

with U20's fifo-held stdin (`_acceptance-u20.inc.bash` `u20_launch`) keeps a headless session
idle, labelled `ccy-session=m2-plain`, with a membership and the marker. `--no-restore` marks
the record so a reboot does not bring the throwaway back. Three such launches (plain, `--teams <seat>@<team>` for a team on the host, `--no-bus`) plus cleanup (`tmux -L ccy kill-session`,
container removal, seat release as U20 does) make the leg runnable by the host agent without
seating the owner's projects. Two further defects in the current `m2-check.bash find` argue
for it: it takes the **first** running session of each kind, so on the owner's host the
"plain" session may be the coordinator's own busy session, which then fails the idle-CPU leg
spuriously and has a hook executed inside it; and a host with no `--no-bus` session running
exits 2, so the owner is asked to start three sessions by hand for a read-only check.

## 7. Conflicts (`git merge-tree`)

- **vs local F44**: three content conflicts, all trivial: `CLAUDE/Plan/meta-deploy.list` (two
  queue entries, keep both), `docs/ccy-changelog.md` (3.98.12 and 3.99.0 entries, keep both,
  newest first), `files/var/local/claude-yolo/claude-yolo` (the `CCY_VERSION` line: 3.99.0
  wins; `REQUIRED_CONTAINER_VERSION` auto-merged because both say 2.67, which is blocking
  item 2). `Dockerfile`, `agent-bus-protocol.md`, `cli.py` auto-merge.
- **vs origin/fix/teams-server-robustness** (`9e449dea`, three commits, based before M0, at
  2.66 and never touching the launcher): five conflicts, all plan files: `ACCEPTANCE.md`
  (add/add), `JOURNAL/00203-Journal-26-10-16.md` (add/add), `PLAN.md`, `acceptance.bash`
  (add/add, substantive), `meta-deploy.list`. Code auto-merges (`docs/agent-bus.md`,
  `bash-limits.allowlist`, `board/collect*.go`).

## 8. Version consistency

- Local F44: CCY 3.98.12, container 2.67, plugin 1.2.3.
- This branch: CCY 3.99.0, container 2.67 (a bump from 2.66 that lands on M0's number), plugin
  1.3.0.
- `fix/teams-server-robustness`: CCY 3.98.11, container 2.66, plugin 1.2.3; it does not bump
  anything, so "both claim 2.67" is not quite the fact: the clash is this branch against
  F44/M0. After merge the Dockerfile and entrypoint differ from the 2.67 that M0 hosts built,
  under the same label. 2.68.

## 9. Smaller findings

- H33b and docs/ccy.md say a plain session and a `--no-bus` one "differ only by the mount".
  They also differ by the plugin, the settings override of `statusLine`, and inbound accepted.
  Correct the sentence whichever way item 1 is decided.
- The kit's `settings.json` in the image is now unused by ccy (only `agent-bus-claude` reads
  it); say so in the Dockerfile comment or it will be read as the file ccy passes.
- `hooks.json` SessionStart inert branch: `|| exit 1` on a failed `jq` surfaces as a hook
  error every session start if `jq` is ever missing from the image; fine as fail-fast, but the
  plugin contract test should pin that `jq` is in the image's package list.
- `membership.go` `sessionName` admits `@` and `+`; the directory name is the session or
  container name, and the launcher's registry names are narrower. Harmless; worth one test
  that a name with a slash or `..` is refused (it is, by the regex).
- The developer's notes claim M1 should build on `parse_membership` and the empty-membership
  claim. Agreed; record that in M1's task text so M1 does not re-implement them.
