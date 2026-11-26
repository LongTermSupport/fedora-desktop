# Plan 00267 fact check (sonnet)

Scope: the diff changes only DESIGN.md's header (it now says the design was read in the code on 26 Nov 2026 on v2-smoke, and that nothing is built). Branch is v2-smoke (git branch --show-current), so that claim is VERIFIED. I also spot-checked the [C] citations that carry the main claims.

| #   | Claim                                                                                             | Verdict                                  | Evidence                                                                                                                                                                         |
| --- | ------------------------------------------------------------------------------------------------- | ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Header: read on v2-smoke; nothing built                                                           | VERIFIED                                 | `git branch --show-current` = v2-smoke; no teamchat file implements the design.                                                                                                  |
| 2   | Judge runs rules in order; reports room gives Fault before decode [C verdict.go:209-242, 235-237] | VERIFIED                                 | verdict.go:235-237 `case j.Purpose == PurposeReports: v.Outcome = Fault`. It comes before `j.main(v)`, which is the only caller of DecodeContent.                                |
| 3   | Drops counted only as counters/log line [C session.go:959-968]                                    | VERIFIED                                 | session.go:959-968 `drop` calls `Counts.Drop(reason)` and appends a log line. `take` at 903-941 matches the described outcomes.                                                  |
| 4   | Unknown typed key dropped `schema` [C content.go:300-302, 351-366, verdict.go:450-452]            | VERIFIED                                 | content.go:300-302 returns ReasonSchema when typedKey is ""; 362-364 returns "" for a key not in `keys`; verdict.go:450-452 `drop(v, reason)`.                                   |
| 5   | v!=1 or features refused [C content.go:368-378]                                                   | VERIFIED                                 | content.go:369-378.                                                                                                                                                              |
| 6   | Delivery proven by transcript, nonce [C engine/proof.go:19-30, 466-499]                           | VERIFIED on content, citation path wrong | The file is `member/engine/proof.go`; there is no `engine/proof.go` under `teamchat/`. Lines 19-30 (clocks) and 466-499 (`prove` matching `Handed == a.Nonce`, `receipt`) match. |

## REFUTED

1. DESIGN.md cites `engine/proof.go` (and probably `engine/...` in the same form throughout, e.g. lines 131-162, 205, 231). The header says paths are under `src-go/internal/teamchat/`, but the file is `src-go/internal/teamchat/member/engine/proof.go`. The same document gives `member/teamsync/session.go` in full, so the proof citations are inconsistent with it. The fix is to change them to `member/engine/proof.go`. The line numbers are right, so the design's claims stand.

Not checked: the other table rows (roughly 80) and `member/proof/scan.go`, `store.go`, `teams.go`, `strictjson.go` citations.
