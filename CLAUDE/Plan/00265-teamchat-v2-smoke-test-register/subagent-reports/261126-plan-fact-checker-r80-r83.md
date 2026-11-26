# Fact check: R80-R83 and placeholder sweep (diffs 00265, 00161, 00209)

Result: 11 claims checked, 10 verified, 0 refuted, 1 unverifiable-here.

| #   | Claim                                                                                                                  | Verdict           | Evidence                                                                                                                                                                                                      |
| --- | ---------------------------------------------------------------------------------------------------------------------- | ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | R80: kit tree 90f502ac `teamsync/verbs.go:245` joins only on 403                                                       | VERIFIED          | `git show 90f502ac:internal/teamchat/member/teamsync/verbs.go` line 245: `if !errors.Is(err, matrix.ErrForbidden) {`                                                                                          |
| 2   | R80: F44 `verbs.go:254` is the 404 join                                                                                | VERIFIED          | line 254 of F44 and of the working tree: `!errors.Is(err, matrix.ErrForbidden) && !errors.Is(err, matrix.ErrNotFound)`. The local F44 ref is stale; origin/F44 holds e67e28d72                                |
| 3   | R82: `cc-teams-settings.json:26` denies `Bash(*org.lightning_matrix.Lightning*)`                                       | VERIFIED          | line 26                                                                                                                                                                                                       |
| 4   | R82: lines 12-15 are the Read/Edit denies on the `~/.var/app/...` tree                                                 | VERIFIED          | lines 12-15                                                                                                                                                                                                   |
| 5   | R82: Claude Code's deny beats any allow                                                                                | UNVERIFIABLE-HERE | external product behaviour; settled by Claude Code permission docs or a live test                                                                                                                             |
| 6   | R82: `src-go/cmd/chat-client-info`, `internal/chatclientinfo` (with `hole_test.go`) and `internal/matrixclients` exist | VERIFIED          | `ls`                                                                                                                                                                                                          |
| 7   | R82: installed by `play-claude-yolo.yml`                                                                               | VERIFIED          | task "Build and Install the chat-client-info Command", `play-claude-yolo.yml:406`                                                                                                                             |
| 8   | R82: documented in `docs/agent-bus.md` and `CLAUDE/QA.md` (matrix-client-guard-uncovered)                              | VERIFIED          | `docs/agent-bus.md` mentions chat-client-info; `CLAUDE/QA.md:3461`                                                                                                                                            |
| 9   | R83: `place.go:50` reads the issued seat from stdin                                                                    | VERIFIED          | `placeissued.Place(ctx, d, e.Stdin)`                                                                                                                                                                          |
| 10  | R83: `stdinread.go:245` is the notice and its advice text                                                              | VERIFIED          | line 245 holds the "waiting for input on stdin..." text with the ctrl-D advice                                                                                                                                |
| 11  | Diffs 00161 and 00209 are one-line team-name swaps                                                                     | VERIFIED          | each diff has exactly one `-` and one `+` line. The 00209 swap is `lts-ops` to `<team>`. The 00161 diff's changed line (D63) is cut off in my output at 300 characters, so I did not compare it word by word. |

## Not checked

The `qa-private-names.bash` leak run was not completed. I called it with no command, so it printed its usage and did no check.

## REFUTED

None.
