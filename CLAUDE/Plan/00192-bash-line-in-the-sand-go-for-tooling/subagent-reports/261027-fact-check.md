# Fact check of PLAN.md diff (Plan 00192), 27-10-27

Checked against branch F44 at /workspace. Sampled the checkable repository claims in the diff; host results, bus event ids and owner rulings were skipped as not checkable here.

## REFUTED (most consequential first)

1. "`setup-rclone` ... ported on `go-port/setup-rclone-3` ... under review, not merged" (PLAN.md Task 3.3; PORTS.md "ported on go-port/setup-rclone-2, under review, not merged"; SPEC-setup-rclone.md describes the wrapper and allowlist row as if only on the branch). Evidence: `git branch --merged F44` lists go-port/setup-rclone, -2 and -3. `src-go/cmd/setup-rclone` (2 tracked files), `src-go/internal/setuprclone` and `scripts/setup-rclone.bash` (uses SETUP_RCLONE_ROOT) are in the F44 tree. Fix: status should read merged into F44, with the review state restated.
2. "every one of the 28 `qa/cmd` mains uses cmdmain ... all 28 `qa/cmd` mains call RefuseClosedStdout (28 directories, 28 non-test callers)" (PORTS.md). Evidence: `ls -d src-go/qa/cmd/*/` gives 29 directories and 29 non-test files call `cmdmain.RefuseClosedStdout` at current F44. The text pins the count to 1d8bc1037, which is an ancestor of F44, so the count is stale at the tree being checked. Fix: 29, or state the count is as of that commit.

## Table

| #   | Claim                                                                                                        | Verdict           | Evidence                                                   |
| --- | ------------------------------------------------------------------------------------------------------------ | ----------------- | ---------------------------------------------------------- |
| 1   | 16 gate packages use rootargs                                                                                | VERIFIED          | git grep count 16                                          |
| 2   | 12 use emit                                                                                                  | VERIFIED          | count 12                                                   |
| 3   | 28 qa/cmd mains                                                                                              | REFUTED           | 29                                                         |
| 4   | setup-rclone not merged                                                                                      | REFUTED           | see above                                                  |
| 5   | wrapper `scripts/setup-rclone.bash`; its allowlist row gone; `go-binary-suites.tsv` has `setup-rclone qa-go` | VERIFIED          | tsv:36; no allowlist match                                 |
| 6   | gh-account-setup at `src-go/cmd/gh-account-setup`; merged at 9e404ae6b                                       | VERIFIED          | dir exists; commit exists                                  |
| 7   | rclone-tail merged at 1d8bc1037; b1e298e1c exists; Task 3.7 merge daf53f21c                                  | VERIFIED          | commits exist; 1d8bc1037 is an ancestor of F44             |
| 8   | `helpers/self_update/affected_plays.py` loads `not-play-inputs.json`                                         | VERIFIED          | affected_plays.py:82; json file exists                     |
| 9   | rules `play-input-dropped`, `play-program-read-dropped` in QA.md                                             | VERIFIED          | QA.md:1454, 1477                                           |
| 10  | `changed_plays.reasons` formatter                                                                            | VERIFIED          | helpers/play_ledger/changed_plays.py:259                   |
| 11  | test-self-update-cycle fixture copies `*.json` too                                                           | VERIFIED          | scripts/test-self-update-cycle.bash:248                    |
| 12  | B53: `qa-hook-handler-rules.bash`, `.semgrep/hook-handlers.yml`                                              | VERIFIED          | both exist                                                 |
| 13  | `recorderLaunch.js` `discardRecorder` (W15a)                                                                 | VERIFIED          | extensions/speech-to-text@fedora-desktop/recorderLaunch.js |
| 14  | Plans 00191, 00209 (agent-bus v2 in Go), 00214 exist                                                         | VERIFIED          | folders exist                                              |
| 15  | mpv installed by `play-cli-tools.yml`                                                                        | VERIFIED          | play-cli-tools.yml:185                                     |
| 16  | Task 3.2 `go-port/agent-bus-install` built, not merged                                                       | VERIFIED          | branch exists, not in the --merged list                    |
| 17  | "10 of them also emit"; 24 commands, 12 plays; 15 findings red; QA.md anchors                                | UNVERIFIABLE-HERE | would need running the tools                               |
| 18  | Host results, bus event ids, owner decisions                                                                 | UNVERIFIABLE-HERE | outside the repository                                     |

Totals: 18 claims: 14 verified, 2 refuted, 2 unverifiable (groups 17 and 18 count as one each).
