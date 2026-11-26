# Fact check: the "on F44" marks (diff of REGISTER.md)

12 claims: 10 verified, 1 refuted (fixed), 1 verified with a wording caveat.

| # | Claim | Verdict | Evidence |
|---|-------|---------|----------|
| 1 | origin/F44 is e67e28d72 or contains it | VERIFIED | `git rev-parse origin/F44` = e67e28d72dc8...; `merge-base --is-ancestor e67e28d72 origin/F44` true |
| 2 | v2-smoke 03ac8dcf7 tree equals e67e28d72's | VERIFIED | both `^{tree}` = e8caa7f692d5fdb3d8068e7581bd088e47f9c9ec |
| 3 | e67e28d72 is an ancestor of v2-smoke | VERIFIED | `merge-base e67e28d72 HEAD` = e67e28d72 (not an ancestor of 03ac8dcf7 itself) |
| 4 | Each v2-smoke batch hash cited (f59504a18, 6b300c2a0, 76d9bf5f0, 544ebda3d, d9a4fcc80, f1e50d031, 0db93cad0, b239a00a6, 2a0b1854c, 8d93dd3d7, 058938264, 9114b2b70) is an ancestor of 03ac8dcf7 | VERIFIED | `merge-base --is-ancestor` true for all 12. Not ancestors of e67e28d72: F44 holds the content under its own linear history (e67e28d72 parent is b4f456389), so "on F44" rests on tree equality, not hash ancestry |
| 5 | R60 line: `2e057a895` is the F44 commit | REFUTED | `2e057a895` is "batch 20: R68 a coordinator's reply finds a questions-room post...; ccy 3.124.65", an F44 ancestor of e67e28d72 but not the R60 batch. Fixed to `e67e28d72` |
| 6 | R57, R59, R62, R63, R64, R65, R67, R68 "on F44 (e67e28d72)" | VERIFIED | tree equality (claim 2) plus e67e28d72 changelog entries naming R57, R59, R60, R63, R64, R68. R62, R65 and R67 are not ccy changes, so they have no changelog line and rest on tree equality alone |
| 7 | R36 parts through ccy 3.124.72 are in e67e28d72 | VERIFIED | `git show e67e28d72:docs/ccy-changelog.md` top entry is 3.124.72, and `CCY_VERSION="3.124.72"` in the launcher |
| 8 | 3.124.72 holds fsx exclusive temporary | VERIFIED | changelog: "files written whole (fsx) create their temporary exclusively" |
| 9 | ...remove forgets seat records, never following a link above them | VERIFIED | changelog: "ccy-teams remove forgets the team's seat conversation records" and "never follows a link above a team's seat records" |
| 10 | ...`--fresh` for an unused seat | VERIFIED | changelog: "Start a ccy session adds --fresh when the seat ... was never used" |
| 11 | ...rule `temp-not-exclusive` | VERIFIED | in the 3.124.72 entry; `.semgrep/temp-exclusive.{yml,py,fixture}` exist in e67e28d72 |
| 12 | ccy 3.124.73 to 3.124.77 are not on F44 | VERIFIED | `grep -c '3\.124\.7[3-7]'` on e67e28d72's changelog = 0 |

## REFUTED

- Claim 5, the R60 State line cited `2e057a895` as the F44 commit for batch 12. It is the R68 batch's F44 hash. The line now cites `e67e28d72`, like the other rows.
- Not refuted, but note: v2-smoke batch hashes in these rows do not exist on F44. A reader running `git branch --contains` on one finds only v2-smoke.
