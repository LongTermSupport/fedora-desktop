## 04:25 · action · —

Green for item 12, SG6 (sc-D15).

- declare: `Team.CarriedFrom`, key `carried_from`, optional for a team entry and for team.json,
  read with `proto.ValidDescription` (description's grammar, 1-120 characters); written last by
  `members()` only when non-empty, so Canonical and Input.Encode of an unmarked team are
  unchanged (the pinned bytes pass on both sides of the change).
- host: the first-apply refusal at step refusals, right after server-name is read and before
  the approval is loaded: Drift (70), "team T is declared as carried: run team import first,
  then team apply"; no call, no file.
- SPEC-carry: SG6's grammar and tests, sc-D15, section 13's declare line; PLAN.md's carry line.
- The play hands each teamchat_teams entry whole to `team apply` (play-teamchat.yml's stdin is
  `{'team': item, ...} | to_json`), so no play change is needed (gate A).
- Not an image input (declare is not in image-source-paths): no CCY bump.

`go test ./internal/teamchat/...` passes; golangci-lint over declare, host, admin: 0 issues.
The apply over a tree carry.Import placed, with the marker, is SA1's (item 13).
