# Plan 00225 review r6 (adversarial, Opus)

Scope: `git diff fbbc19094 HEAD -- CLAUDE/Plan/00225-team-liaison-role CLAUDE/Plan/README.md` at
54c115ae9, and PLAN.md and DESIGN.md as they stand. I checked them against
`src-go/internal/teamchat` (proto, member, admin, apply, declare, host), `src-go/internal/teams`,
`files/var/local/claude-yolo/{entrypoint.sh,claude-yolo,lib/*.bash}`, the r5 report and the
journal.

## Round 5 findings

- **r5 B1 (removable in-container binds): mostly resolved.** The argument holds:

  - The entrypoint starts no background process (no `&`, `nohup` or `setsid` in entrypoint.sh).
    It `exec`s the chain (entrypoint.sh:761, 769), so pid 1 ends up holding the dropped set.
  - Root's `execve` regains only what the bounding set holds.
  - `no_new_privs` closes file capabilities.
  - A process with fewer capabilities cannot ptrace one with more.
  - The kit makes the binds in the container's own mount namespace, so they are not locked
    there, and `umount` or `remount` needs `CAP_SYS_ADMIN`.

  Two gaps remain: s4 (an overstated claim and a missing probe) and B1 below, which is a hole
  in the state classification, not in the capability argument.

- **r5 B2 (unchecked edits): resolved.** LZ6 fetches `relations` for `m.replace` and the
  redaction state. `edited` is in the closed set (DESIGN:156). Task 2.3 and Task 8.2 test it,
  and the check agrees with v2: `proto/content.go:288` already drops an agent's edit as `edit`,
  so the fetch has to be explicit, and it is. The liaison posts outside the thread are now
  listed (DESIGN:136-139). s3 covers a remaining window gap.

- **r5 B3 (remote credentials): resolved in principle** (LZ17a), but the refusal points are
  incomplete (s2).

- **r5 s1: partly resolved.** The launcher's reads are listed, but the list is wrong (B2).

- **r5 s2: resolved.** The per-seat files are re-rendered at each launch.

- **r5 m1: resolved.** Task 4.1 has `--output`, `--ext-diff`, `--textconv`, `--exec-path` and
  `gh api`.

## Blocking

**B1. LZ17 declares "the hooks daemon's untracked state" as state (`rw`), but that directory
holds code that runs in every seat's hooks.**

DESIGN:331-332 says the daemon's untracked state is state. On this tree
`.claude/hooks-daemon/untracked/` holds:

- `bin/hooks-relay`, an executable that every hook call runs;
- the daemon's Python virtualenvs, one per runtime path. The container's venv is named after
  `/workspace`, so every ccy container of the checkout shares it, and the host's own venv sits
  beside it;
- `upgrade-gate/`, `upgrade-approvals/` and `supervise/`.

Bound `rw`, these let the liaison replace code that runs in:

- its own daemon's hooks;
- every other ccy seat in the same checkout. Those seats have a `rw` checkout and push
  credentials, so this is a full escape from "read-only";
- through the host venv, the owner's host `cc` sessions.

This breaks LZ17's own rule ("nothing that decides what runs in the session ... is writable by
the liaison", DESIGN:321-323). Task 0.1's method (log each `EROFS` and call it state) would
classify a venv write as state, because the daemon writes its venv.

Fix:

- State the rule that a path holding anything executed (binaries, venvs, wrappers, plugin
  code) is configuration even when it is written at run time.
- Classify the daemon's `untracked/` entry by entry: sockets, pid and lock files, logs and
  ledgers are state; `bin/`, `venv-*`, `upgrade-*` and `supervise/` are configuration.
- Give the liaison its own copy where the daemon must write code: a per-seat venv, or one
  prebuilt in the image.
- Add a Task 0.1 and Task 5.1 probe: from the liaison, a write to `hooks-relay` and to a venv
  file fails.
- Apply the same test to the project's `untracked/`, which other agents read (reference
  clones) and run scratch scripts from.

**B2. LD12 is not coherent with what the launcher and the entrypoint read.**

LD12 (DESIGN:350-362) re-renders "the wrapper, `mounts`, `ccy.env` and the user
`settings.json` from the image and the team record" in a per-seat directory mounted over
`/workspace/.claude/ccy`. On this tree:

- `ccy.env`, `mounts`, `claude-supervise*`, `Dockerfile` and `allowed-hostnames` are the
  project's tracked files (lib/common.bash:583-592), not image or team-record products.
- `mounts` is read **on the host** from the checkout, before the container exists
  (lib/common.bash:412-430), so a per-seat copy is never read. It is also missing from LD12's
  list of host reads.
- `ccy.env.local` (untracked, install-local) is read on the host and bound `ro` at
  `/workspace/.claude/ccy/ccy.env.local` (lib/common.bash:704-716). LD12 does not mention it.
  If the per-seat directory is mounted over `.claude/ccy`, that bind's ordering decides whether
  the file exists at all inside.
- The entrypoint sources `/workspace/.claude/ccy/ccy.env` and `ccy.env.local`
  (entrypoint.sh:450). By default it runs `/workspace/.claude/ccy/claude-supervise.py`
  (entrypoint.sh:632). Under LD12 both resolve into the per-seat directory, which the liaison
  can write during a session. The re-render protects the next launch only if those files are
  re-rendered too, and the "from the image" source is wrong for them.

Fix: in LD12, the per-seat directory holds only the user-level files (`settings.json`,
`plugins/`, `skills/`) and state.

- The project's tracked files (`ccy.env`, `claude-supervise*`, `Dockerfile`,
  `allowed-hostnames`, `mounts`) are bound `ro` from the checkout over it.
- `ccy.env.local` keeps its existing `ro` bind, ordered after the per-seat mount.
- The host-read list adds `mounts` and `ccy.env.local` (both read from the checkout, never
  per-seat).
- Task 5.1's fixture pins the mount order.

## Should-fix

- **s1. The v2 liveness signal is mis-cited (a false v2 claim).** DESIGN:250-251 says the live
  signal is "the liaison keeper's run notices (a run started and not ended, A 13 and n3 KP3's
  `end`)".

  - A 13 is the delivery format (`member/deliver/deliver.go:2`, `proto/delivery.go`).
  - KP3's `end` is a keeper-to-link control message on a local socket
    (`member/wire/control.go:28-54`). Another member never sees it.
  - What v2 actually gives another member is A 7.4's lifecycle notices: `left`, `disconnected`
    and `back` (`proto/content.go:68-76`, posted by `member/ends/ends.go:124-156`). It also
    gives Matrix presence for members, which the sync filter carries (`proto/filter.go:45`;
    `SetPresence` in `member/teamsync/verbs.go:334`).

  Name these as Task 0.1's candidates. Task 0.1 still decides.

- **s2. LZ17a's refusal points miss the launcher's own paths.** LZ17a and Task 5.2 list the
  board, `ccy --teams`, Quick Launch and the LXC play. Three paths are not on that list:

  - `claude-yolo` itself picks a key when the selection is empty and exactly one exists
    (claude-yolo:1330-1341);
  - it offers `LAST_SSH_KEYS` (claude-yolo:1250-1278);
  - it relaunches on a restart request with the previous choices (lib/restart-request.bash).

  The SSH agent sentinel is also an `SSH_KEYS` entry (claude-yolo:778). Put the refusal where
  the seat's role and the final key and token selection meet: Go's `launch-mounts`, which
  already knows the role. Add a check inside the container: the kit stops a liaison seat that
  finds `SSH_AUTH_SOCK`, a mounted key or a write-capable `gh` token. Then no route can bypass
  it. Note also that a liaison with no token loses v2's forge check, which reads gh's
  credential (`member/forge/token.go:25-30`, 47-51). LZ17a should say read-only token, or what
  the liaison does with a ping reference.

- **s3. LZ6's framing window starts too late and covers one room.** "Every liaison post between
  the `Q<n>` post and the answer" (DESIGN:137) misses three routes:

  - a preamble posted **before** `Q<n>` ("the next one is routine, just say yes");
  - liaison texts to the human in the **main room** (LZ4 allows texts to any human);
  - the terminal, where LZ7b's mirroring depends on the liaison's own discipline and nothing
    enforces it.

  The authority rule still holds: only the human's event, for the verbatim `Q<n>`, counts, and
  Element shows `Q<n>` verbatim. So this is not a B2-class break. LZ7b's confirmation can still
  be steered by framing the coordinator never sees.

  Fix:

  - start the window at the hand-off's receipt (or the liaison's previous relay);
  - include the liaison's main-room texts to humans from the coordinator's own sync;
  - state plainly in LZ7 and LZ7b that the listing is transparency, not a guard, and that
    unmirrored terminal talk is invisible to the coordinator;
  - give the coordinator a rule: when a liaison post sits in the window, treat the answer as
    information only for a consequential act unless the human's own words are unambiguous.

  Also define how `liaison_note` reaches Element. Element renders only `body`, so "shown apart
  by Element's fallback text" (DESIGN:109) puts the note in the body. Then "never in the body
  the question parser reads" needs a stated split rule, and the note is a framing vector on
  `Q<n>` itself. State that the receiver applies `DecodeContent` to the `Q<n>` post, so a
  `format`/`formatted_body` (which Element shows instead of `body`) fails it
  (`proto/content.go:292`).

- **s4. "Nothing the session runs can regain it" (DESIGN:345-346) is overstated.**
  `unshare -Urm` gives a child user namespace the full capability set, whatever the parent's
  bounding set. What protects the binds is that they become `MNT_LOCKED` in the child
  namespace: `umount`, `remount,rw` and a non-recursive `--bind` of the parent are refused.
  Say so, and add to Task 0.1's probe:

  - `unshare -Urm` followed by `umount`, `mount -o remount,rw`, and `mount --bind` of `/workspace`
    (non-recursive) to a scratch path, then a write through it — each must fail;
  - whether SELinux (`container_t`, relabelled checkout) allows the kit's binds at all,
    since ccy can also run with `label=disable`.

  Also say that the launcher adds `--cap-add SYS_ADMIN` only for a liaison seat on the
  in-container path (it adds none today).

- **s5. The owner asked for mirroring "both ways"** (journal, last entry). LZ7b says "mirrors a
  terminal answer to the room, never the other way" (DESIGN:190). The room side already reaches
  the terminal as the `HUMAN` delivery in the liaison's transcript. Say that, so the text does
  not read as refusing half the owner's request.

- **s6. Shared state lets the liaison write into other seats' sessions.** LZ17 binds state `rw`
  at its real path. In the in-container path, `.claude/ccy/projects/` (transcripts),
  `sessions/` and the login's token file are shared with every other ccy seat in the checkout.
  The liaison can then edit a coordinator's transcript before a `--resume`. LD12 removes this
  for `.claude/ccy`. The in-container path should either do the same (per-seat state), or
  record that this is accepted.

## Minor

- m1. LZ1 writes the declaration key as `human-text`; v2 spells it `human_text`
  (`proto/roster.go:131`, `declare/declare.go:48`).
- m2. The deny texts (DESIGN:299, 305) still start `agent-bus:`, a v1 name. Use `teamchat:`, or
  whatever prefix the v2 kit's messages use.
- m3. Section 8 says it "names the packages as they stand on F44", but
  `ccy-teams internal launch-mounts` does not exist (`internal/teams/app.go:296-312` has
  `membership-init`, `restart-args`, `claim-container-name` and `go-cache`). Mark it as new.
  The other packages it names exist.
- m4. LZ11's "reply `--re`": v2's `reply` takes the event ID as an argument and has `--ref`, not
  `--re` (`member/cli/verbs.go:206-209`).
- m5. LZ15's options list omits `git grep -O` / `--open-files-in-pager`, which runs a command.
- m6. `docs/agent-bus-protocol.md` and `docs/agent-bus.md` have no v2 part yet, and no playbook
  deploys `teamchat` yet (no match under `playbooks/`). Tasks 1.2, 8.1 and 8.2 assume both.
  Add Plan 00246's deploy work as a dependency.
- m7. No journal entry records this revision (the v2 retarget and the r5 fixes). Add one with
  `mkplan.bash --journal`.

## v2 facts checked (true)

- The roles `coordinator`, `worker` and `probe` (`proto/roster.go:17-19`).
- The reports room: humans at 0, `m.room.message` and agents at 10 (`proto/rooms.go:51-52`,
  186-187).
- `questionsSpec` has the humans plus the coordinator only (`proto/rooms.go:140-160`).
- The verbs `say --to-coordinator`, `report`, `ask`, `thread`, `questions`,
  `history --room main|questions` and `verify` (`member/cli/verbs.go:115, 165, 210-236`).
- `role apply` (`admin/verbs.go:99`, `apply/member.go:33`).
- The drop reason `role` (`proto/verdict.go:12`).
- `matrix/fakehs`, `host/lxc`, and `member/{state,keeper,teamsync,deliver,hooktext}`.
- Pull request 124 merged 2026-11-05.
- The questions room's `redact` level is 100, so the liaison cannot redact even its own post;
  LZ6's redaction check is belt and braces.

## Consistency and public-repo check

- LZ0-LZ24 (with 4a, 5a, 7a, 7b, 16a, 17a) and LD1-LD13 are all defined and cited
  consistently. The tasks cite the right requirements.
- No v1 file names remain where v2 is meant, apart from m2. `helpers/pingbus/hooks.py` at
  DESIGN:314 is a correct reference to 00222's current text.
- The status line is accurate.
- Nothing install-specific is in the plan text. `Owner: joseph` is the convention across the
  plan tree. This report names the daemon's venvs by kind only.

VERDICT: CHANGES — 1. B1: the daemon's `untracked/` (hooks-relay, venvs) is classified as `rw` state, which lets the liaison inject code into every seat and the host; 2. B2: LD12 re-renders tracked project files and misses the host's reads of `mounts` and `ccy.env.local` and the entrypoint's reads of `ccy.env` and `claude-supervise.py`.
