# Plan 00225 review r4 (adversarial, Opus)

Scope: `git diff dcb76af56..HEAD` (PLAN.md, DESIGN.md, README row). I checked it against
`files/var/local/claude-yolo/entrypoint.sh` and `claude-yolo`, `helpers/pingbus/`
(syncer.py, cli.py, protocol.py, config.py, inbox.py) and the r3 report.

## Round 3 findings

- **M1 (`.claude/ccy/` bound `rw` whole): the stated problem is resolved, but the fix is not
  sound.** LZ17 now splits state from configuration entry by entry. Task 0.1 classifies every
  `.claude/ccy/` entry, and Task 5.1 tests the named configuration files. The mechanism that
  replaces the `rw` bind conflicts with the entrypoint (M1 below).
- **m1 (git write list): resolved.** LZ15 is now an allowlist, and Task 4.1 tests unlisted
  verbs.
- **m2 (no fetch): resolved.** LZ17 says the liaison reads the host checkout live and needs no
  fetch.
- **m3 (`--update`): resolved.** It is the coordinator's hand-off kind in LZ4a, LZ13 and
  Task 2.2.
- **m4 (stale wording): resolved** in both LD11 and the Risks row.
- **m5 (who posted `Q<n>`): resolved.** LZ6 requires the sender to be the team's liaison.
- **m6 (`verify-human`): mostly resolved.** LZ7 says it is a new command. It still does not say
  how `history --room questions` shows a relayed answer (see m3).

## Major

**M1. LZ17's liaison launch cannot get past the entrypoint, and one file it makes `ro` is not
in the checkout.**

The entrypoint runs under `set -e` (entrypoint.sh:6) and writes into `/root/.claude` (which is
`/workspace/.claude/ccy`) without a condition at every launch:

- the `jq` merge, then `echo "$UPDATED" > settings.json` (347-368);
- the PHPantom plugin install into `plugins/` (373 onward);
- `mkdir -p` and `cp -r` of the image's skills into `skills/` (392-393);
- `echo "$trust_updated" > /root/.claude.json` (424).

LZ17 says three things that cannot all hold:

- the launcher binds a host-rendered `settings.json` `ro` over its path;
- `.claude/ccy/` configuration (plugins and skills decide what runs) stays `ro`;
- the entrypoint "gains no line beyond that one call", a check that each file is present and
  not writable.

The entrypoint's own write to the `ro` `settings.json` (or to `skills/`) fails, `set -e` stops
the launch, and the check is never reached. Every liaison launch would fail.

Separately, `/root/.claude.json` is not under `/workspace` at all. It is created inside the
container (claude-yolo:2343) and is not persisted by any bind, so it does not shape the next
launch. Claude Code also rewrites `~/.claude.json` throughout a session, by replacing the file.
A file bind blocks that replace, and a read-only one refuses it. Binding it `ro` protects
nothing and is likely to break the session. Task 0.1 only measures writes inside the checkout,
so it would not catch this.

Fix:

- Decide how the entrypoint behaves in the liaison case. Either it skips its writes into
  `/root/.claude` when they are pre-rendered, which costs lines on a ratcheted file, so move that
  logic into the kit Go binary; or the rendering covers `skills/` and `plugins/` too and the
  entrypoint's writes are made conditional.
- Drop `/root/.claude.json` from the `ro` set, or justify it. Add a Task 0.1 probe that runs a
  real session against the rendered `ro` files.
- "Renders exactly what the entrypoint would write" copies the entrypoint's `jq` merge into Go
  as a second implementation that can drift. Name one owner for that logic.

**M2. LZ9's fallback sends the coordinator every human message meant for the liaison.**

LZ9 stores *every* human message and edit in the questions room as a quiet item on the
coordinator. It delivers each one still unrelayed after `liaison_relay_s` (900 s), and all of
them at once when the liaison goes inactive. But the questions room is also where the human
talks to the liaison: "what's happening", and replies to updates (LZ13). Most of those messages
are attached to no hand-off, so the liaison never relays them. Each one reaches the coordinator
15 minutes later as `HUMAN`, with the line "the liaison has not passed this on".

The owner's whole aim is that the coordinator is not interrupted by human chat that is not for
it, and LZ9 interrupts it with every such message, late. This conflicts with LZ12-LZ13, and with
the Success Criterion that the coordinator is not woken while the liaison is active.

Fix:

- Make the keeper and inactive-flush delivery apply only to messages attached to a `Q<n>` posted
  from a coordinator's hand-off (or to a question the coordinator posted itself).
- Or let the liaison close an item without a relay (`pingbus relay --none Q<n>` or similar),
  shown in `pingbus questions`.
- Add a Task 2.1 test: a human's unattached message to the liaison is never delivered to the
  coordinator while the liaison is active.

## Minor

- **m1.** A status counts only from a role holder (syncer.py:332, protocol §8). LZ10's active
  test needs the liaison's status to count, so Task 1.1 or 2.1 should name that change.

- **m2.** The LZ15 allowlist admits verbs that write or run code through options:

  - `git diff --output=<file>` and `git log --output` write a file;
  - `git -c core.pager=...`, `-c core.fsmonitor=...` and `-c alias.*` run commands.

  Task 4.1 lists `git -c` as a case. Say that `-c` and `--output` are denied, so the tests
  pin it.

- **m3.** r3 m6 left open what `history --room questions` shows for a relayed answer: `HUMAN`,
  or its relay status. Say which.

VERDICT: FIX-FIRST
