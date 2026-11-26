# R79: the private-name guard learns teamchat's names and the owner's pins

Not a single term's value appears in this report.

## What a container can learn (checked, 26 Nov)

- `pingbus status`: not on PATH in this container, so the v1 bus taught nothing.
- `teamchat status` (exit 0): JSON Lines with 1 `team`, 7 `member`, 3 `room` and 1 `runtime`
  record. These cover this session's teams only (verb doc: "this session's teams, roles, members").
- `/run/teamchat/membership` holds this session's membership only. `/var/lib/teamchat*` and
  `/etc/teamchat` (the host's team state) are not mounted.
- **The other teams on the same bus host cannot be learned from inside a container.** The
  owner pins them in `<git dir>/private-terms-pinned`.
- `private-terms-export-skip` is not this list. It only keeps learned terms out of the
  published denylist. The pinned list adds terms.

## Fix (commit 81a4f18cd)

- `src-go/qa/privatenames/main.go`: `busStatus` asks `pingbus status` and `teamchat status`
  through the one seam (`a.run`). teamchat's team and member records become Derive's TEAM and
  MEMBER lines. Exit 69 (not a member session) counts as an empty answer. Any other failure,
  or unreadable output, is exit 2.
- `pinned`: `<git dir>/private-terms-pinned` is read one name per line, skipping `#` lines and
  blank lines. A pinned name always blocks, is never printed, is never written into
  `private-terms`, and goes into the denylist (subject to export-skip).
- In a container, a bus that reported a session but taught no team or member name is exit 2,
  unless a name is pinned.
- usageText, package doc, `CLAUDE/SecurityRules.md` now name the real sources.

## Red, then green

`src-go/qa/privatenames/teamchat_test.go`. Before the fix, with only the names declared, 6 of
the tests failed:

- TestATeamchatSessionsTeamAndMembersBlockACommit
- TestTeamchatOutsideASessionIsEmptyAndAnyOtherFailureIsFatal
- TestAContainerSessionThatTeachesNoBusNameIsRefusedUnlessPinned
- TestAPinnedNameAlwaysBlocksAndIsNeverPrintedOrCopied
- TestAnUnreadablePinnedListIsFatal
- TestAPinnedListAloneIsJudgedAgainstOutsideAContainer

After the fix, all pass, including the new denylist test. Checks run:

- `go test -race ./privatenames/ ./cmd/qa-private-names/`: ok, 92.4% coverage.
- `golangci-lint run ./privatenames/`: 0 issues.

## Real tree

- `qa-private-names refresh`: 4 terms before (all `host name`), 19 after (1 `agent-bus team`,
  14 `agent-bus member`, 4 `host name`).
- `tree HEAD` after the fix finds **23 findings**: term #19 (this session's own team) 19 hits
  in 5 places, and term #10 (one member handle) 4 hits in 2 places. They are in Plan 00161
  DESIGN.md, Plan 00209 DESIGN-v2-A-protocol.md, and Plan 00265's REGISTER.md, ROUTE-TO-LIVE.md,
  one JOURNAL entry and one subagent report. Neither term is a dictionary word, so these are
  real leaks the old guard never saw. They are left for the owner (replace with placeholders,
  or allowlist). Until then the local `qa-private-names` gate is red. CI has no list, so it
  still prints SKIPPED.
