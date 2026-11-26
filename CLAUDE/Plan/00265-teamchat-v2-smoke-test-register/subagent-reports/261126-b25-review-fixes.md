# Batch 25 review fixes

Review of `git diff eed98ea2a a410d64ec`; five findings, all fixed in one commit on v2-smoke.

1. `CLAUDE/Plan/meta-deploy.list`, the play-claude-yolo entry: the #430 paragraph no longer says
   the image is unchanged. Added, in the owner's words: #431 (ccy-sessions retire-conversation,
   forget retires the record), Task 4.2 / R90 (`--teams <team>` takes this checkout's seat,
   asks only between two seats with a conversation, in words; a bare `--teams` takes the only
   team; no seat numbers), and Plan 00267 Tasks 7.6 and 7.7 (the image goes to 2.84.95, deliveries
   limited by size only, in sections). Each says which play and when it takes effect; the
   play-teamchat entry gains a line for the host kit's copy of 7.6/7.7.
2. `files/var/local/claude-yolo/claude-yolo:17`: the CCY_VERSION comment (about 12,000
   characters) is one line pointing at `docs/ccy-changelog.md`. The changelog has entries for
   3.124.86 to 3.124.91. The CCY version gate demanded a bump for the comment cut, so it is
   3.124.92 (container unchanged at 2.84.95) with its own changelog entry.
3. `docs/agent-bus-protocol.md` (watch row) and `DESIGN-v2-B-member.md` B23: a delivery is
   limited by 12 KiB only, no entry cap. `recv`'s 8-item cap and Task 7.3's 8-per-fold limit
   are untouched.
4. Plans 00262 and 00270 cross-linked. 00262 is not wholly covered: its Phase 1 (interim CI
   wins: Go cache in CI, parallel jobs, parallel helper tests) is not in 00270, and 00270 decides
   history (Q2) and v1 (Q4) differently. So 00262 is not superseded; it says 00270 owns the split
   and only Phase 1 is worked there. Its invalid status `Proposed` became `Not Started` (the plan
   QA edit gate refused any edit otherwise), and its index row was shortened.
5. `CLAUDE/Plan/README.md`: the 00270 row says Q2, Q4 and Q7 decided, the rest open.

Checks: plan-qa `--check-staged` has no block findings (the advisories were there before:
five long index rows elsewhere and two plan-root include files). `go test ./internal/ccylaunch/...`
was run because the launcher changed.
