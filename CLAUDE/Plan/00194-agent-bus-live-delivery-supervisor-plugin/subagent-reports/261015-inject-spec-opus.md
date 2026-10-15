# Task 2.0c spec report (Opus, 2026-10-15)

**Spec:** [SPEC-inject.md](../SPEC-inject.md), on branch `feat/bus-inject-message` (cut from
`origin/F44`). No code yet; it waits for review.

**Path chosen.** The inbox socket carries the envelope: it is the only shipping path that
wakes an idle session and shows the words to the human. The UserPromptSubmit hook cannot
wake a session and its context is hidden, so it is not the carrier; it is used as a second
channel that attests each delivery number from a session-private ledger (E13), against the
socket's "Another Claude session" frame on a human's message (threat T5, residual R1).

**Points for the reviewer.**

- E7 points an oversize body at `history`, not `recv` as the brief said: the item is consumed
  once delivered, so `recv` would not show it again.
- E9 sends the socket write while holding the spool lock shared with `recv` (at most the 5 s
  send timeout), so the exclusion is exact without spool files.
- R2: a clean socket send is not proof that Claude Code displayed it.
- E13/E14/E15 (attestation, the SessionStart line, quiet hooks) are the parts that could be
  cut if the review judges the frame acceptable.

**Overlap.** `fix/pingbus-short-name-reply` was not on origin. The envelope's `reply:` line
reuses `reply_line`, so it carries the short-name `--to` bug until that fix lands (SPEC
section 8).

**Journal.** The owner's 17:10 entry is uncommitted in the coordinator's checkout; this
branch's day-file has the new 2.0c decision entry after 09:28, so the two need joining at
merge.
