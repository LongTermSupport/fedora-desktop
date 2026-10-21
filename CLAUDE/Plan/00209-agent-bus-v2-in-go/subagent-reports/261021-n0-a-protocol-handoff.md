# Plan 00209 N0 area A (protocol and rooms): handoff

The designer stopped at its context budget, in the middle of fixing review round 1.

## State

- Branch `feat/bus-v2-n0-a-protocol`, worktree `/workspace/untracked/worktrees/bus-v2-n0-a-protocol`.
  The tip at handoff is the commit that adds this file. The design draft is complete in all 20
  sections.
- Design: `CLAUDE/Plan/00209-agent-bus-v2-in-go/DESIGN-v2-A-protocol.md`.
- Review round 1 (Opus, FIX-FIRST: 0 blocker, 6 major, 14 minor):
  `subagent-reports/261021-n0-a-protocol-review.md`.
- Research digests (untracked): `untracked/scratch/a-digest-{spec-proto,matrix,00211,inject-hooks,v1-code}.md`
  in the worktree. Read these, not the sources.

## Round-1 findings: fixed and committed

1, 2 (partly: 6.6 and 3.2 rewritten; see below), 4, 6 (partly: 4.2 and 6.4; 12.2 still pending), 7,
8, 10 (2.2, 4.1, 7.4; others may remain), 11, 12 (partly), 14 (partly: 5.1 done, 9.3 pending), 15, 18.

## Round-1 findings still to fix

- **2, remaining.** In section 6.6, liveness is now the seat lock (R27), presence is display, the
  worker's sync marks online, and acceptance is derived by C from the recorded `<v2-bin>` version.
  Still to update to match: 11.2 (drop the `status_msg` row); 14 server-behaviour row (presence and
  `set_presence=offline`); 15 cost row; 16 HC-A1 text (presence works, filtered sync, and
  `set_presence=offline` means no change); 17 OD-A1 (options: (a) presence as display, (b) no
  liveness on the wire; not v1's state event); 19 drops row; 20 B (worker's sync sets presence,
  others pass `set_presence=offline`, keeper sets offline on release) and C (records each seat's
  `<v2-bin>` version and uses it for the raise, enables presence in Tuwunel).
- **3.** In 13.2, each line gets its own grammar: a fixed template with typed slots. Name slots,
  handles, team, IDs, verb, ref, branch and time are full-match ASCII with no space. JSON-string
  slots (`body:`, and the `question:` headline) go through the 13.4 escaper; the headline uses
  ASCII-only mode, with every non-ASCII character as `\u`. Restate 13.6 points 1 and 3: sender text
  appears only in JSON strings from the one escaper, on `body:` and `question:`. Mark the headline as
  the coordinator's words in 13.5.
- **5.** Name the secret check explicitly in 10.2 and 10.3. 7.3 already names it for every free
  text. Add `secret` corpus cases for `ask-human` and `thread` to 14.
- **6, remaining.** In 12.2, cite I4 (not I2) for redaction, drop "a location" from the closed
  list, and say files, locations and `/me` are open and ignored.
- **9.** The `trust:` values are `trusted` (listed or org) or `external`. Fix 9.3's "class as
  `trust`".
- **10, remaining.** Grep for stale cross-references and for "every coordinator" or "coordinators"
  wording, outside the R28 quote.
- **12, remaining.** 10.4 and 10.2 still say "every coordinator" in places.
- **13.** The duplicate-window key in 11.2 also takes a SHA-256 of the text for text-bearing kinds
  (text, report, question, thread).
- **14, remaining.** 9.3 still says the credential goes to "the roster's `forge.api` origin". It
  should say the origin is fixed per kind.
- **16.** In 10.3, the thread fallback `m.in_reply_to` targets the latest thread event the
  coordinator knows of, not the root.
- **17.** Not fixed yet. In 8.3, a TIMEOUT should fire only when local now is past the deadline
  plus a 30 s grace (one long poll). In 8.4, an on-time answer that arrives after its TIMEOUT should
  produce an `answered:` line marked "after its TIMEOUT".
- **19.** In 12.3, the health check reports by user ID and shows a display name only when it passes
  the human or handle grammar (R39).
- **20.** Section 13 weighs no alternative envelope format. Add a paragraph that weighs one compact
  JSON object per entry, on one line with ASCII-only escaping. It is rejected because the model reads
  key-value lines better, the transcript proof and the skill rest on the current lines, and once
  finding 3 is fixed the per-line grammar is a table. Keep the key-value form.

## After the fixes

Run `./scripts/qa-changed.bash` and commit. Then spawn a fresh Opus adversarial reviewer for round
2, at most 3 rounds in total, writing to the same review path; give it the round-1 report so it can
check the fixes. Fix its findings and commit. Finally send the lead a message under 1200
characters: branch, tip, file, verdict, the defaulted decisions (OD-A1 to OD-A9), and the
assumptions on B, C and D (section 20).
