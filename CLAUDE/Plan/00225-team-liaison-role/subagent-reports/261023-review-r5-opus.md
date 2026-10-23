# Plan 00225 review r5 (adversarial, Opus)

Scope: `git diff F44...HEAD` at f6858f510 (PLAN.md, DESIGN.md, journal, README row). I checked
it against `files/var/local/claude-yolo/entrypoint.sh` and `claude-yolo`, and against the r4
report.

## Round 4 findings

- **r4 M1 (configuration binds against the entrypoint): the stated conflict is resolved, but the
  new mechanism does not hold (B1 below).** The entrypoint now keeps its writes and stays the
  one owner of the `jq` merge. `/root/.claude.json` is dropped, with the reason given. Task 0.1
  now runs a real session against the binds. All three r4 sub-points are addressed on paper.
- **r4 M2 (coordinator sent every human message): resolved.** LZ9 now owes the coordinator only
  items on a `Q<n>` whose `handed-off:` names its own hand-off. A human's other messages belong
  to the liaison while the liaison is active. Task 2.1 opens with the red test r4 asked for. The
  inactive flush (messages since the last live status) is reasonable, because nobody else read
  them.
- **r4 m1 (status from the liaison): resolved** (LZ10, Task 2.1).
- **r4 m2 (`-c` and `--output`): resolved in LZ15.** Task 4.1 names `git -c` but not
  `--output`, `--ext-diff`, `--textconv` or `--exec-path` (m1).
- **r4 m3 (history marking): resolved** (`relayed`, `owed`, `liaison's`).

## Blocking

**B1. The in-container `ro` binds are either impossible or removable by the liaison.**

There is no `USER` directive in the image, and the entrypoint `exec`s the seat and wrapper
chain directly (entrypoint.sh:756, 764). So Claude Code, and every Bash command the liaison
runs, is the same container root that LZ17's kit binary runs as, with the same capabilities.
`claude-yolo` passes no `--cap-add`, so a default rootless podman container has no
`CAP_SYS_ADMIN` and cannot `mount --bind` at all. There are two cases:

- If Task 0.1 makes the binds work by granting `CAP_SYS_ADMIN`, the liaison's own Bash can
  `umount` them and write the configuration again. That capability is also a large new power
  for the session.
- If it does not, the primary path is dead and only LD12 remains.

As written, Task 0.1's "can root make the binds" passes in exactly the case where the binds
protect nothing.

Fix: state that the primary path counts only if the capability is gone before `exec`. Either
drop it from the bounding set (for example `setpriv --bounding-set -sys_admin` in the kit
binary before it hands on), or make the mounts locked from the session's user namespace. Add a
Task 0.1 and 5.1 probe that the session's own `umount` and `mount -o remount,rw` fail.
Otherwise make LD12 the design, not a fallback.

**B2. The receiver never checks edits or redactions of the question post, so a relay can
authorise something the human never saw.**

LZ6 compares the `Q<n>` post's own content with the hand-off byte for byte. But the liaison
holds its own Matrix token. The threat model already assumes raw posts: Task 8.2 has a "forged
raw relay". So the liaison can `m.replace` its verbatim post. Element shows the edited text,
the human answers that text, and the coordinator fetches the original event and gets
`checked on the homeserver`.

Example: the coordinator asks "May I force-push X?". The liaison edits the post to "May I wait
on X?". The human says "yes", and the coordinator force-pushes with verified authority. That is
the one thing the owner said a relay must never be.

LZ6 also lists only liaison posts inside the thread. A liaison message elsewhere in the room
that reframes `Q7` is not shown.

Fix:

- Any `m.replace` or redaction of the `Q<n>` post, by anyone, makes the relay
  `NOT VERIFIED: edited` (a new closed-set reason, with a test in Task 2.3).
- `verify-human` applies the same check.
- The relay block lists every liaison post in the questions room between the `Q<n>` post and
  the answer, not only those in the thread.

**B3. "Read-only access to the code" is not enforced on the remote.**

The plan does not mention SSH keys or GitHub tokens anywhere. ccy offers `SSH_KEYS` and a token
at launch. A liaison with push credentials can write code without the checkout:

- `curl` to the GitHub API, which no guard pattern sees;
- `gh api -X POST`, since `api` is not on LZ15's `gh` list but the guard is a pattern match;
- a clone in `/tmp`, pushed with `ssh`.

The read-only mount only stops writes to the local checkout.

Fix: LZ17, LZ19 and LZ20 launch a liaison with no SSH keys and no write-scoped GitHub token (a
read-only token, or none). The board and launcher refuse a liaison launch that selects them.
Task 8.2 adds a check that a push from the liaison fails.

## Should-fix

- **s1. LD12 fallback: the host launcher reads `.claude/ccy/` at the checkout path.**
  `claude-yolo` reads `state/restart-request.json` (3738), `Dockerfile`, `allowed-hostnames`,
  `config.json`, `sessions/` and `.last-launch.conf`. With a per-seat directory, the
  supervisor's restart request lands where the launcher never looks. The restart then fails
  closed ("left no restart request"). Name which of these files the launcher reads from the
  per-seat directory.
- **s2. In LD12, the liaison's own `settings.json` persists to its next launch.** The `jq` merge
  keeps unknown keys, so `disableAllHooks` would survive and switch off its guard. The `ro`
  checkout still holds, but the guard is the only control on `gh` (see B3). Re-render or reset
  that file at a liaison launch.
- **m1.** Task 4.1's cases should include `--output`, `--ext-diff`, `--textconv`,
  `--exec-path` and `gh api`.

## Authority, otherwise

These parts hold:

- relays are `PEER`;
- the receiver fetches the events itself and binds them to its own hand-off;
- the whole thread is shown, and `answers differ` is flagged;
- terminal answers carry no authority;
- the liaison's note sits in a separate key, and authority is capped at the question as posted.

B2 is the one gap I found.

VERDICT: CHANGES
