# Fact-check: REGISTER.md R79 additions (commits 0f4676546, eaaa11f89, 2476e0ad6)

Scope: the one added hunk (REGISTER.md around line 1528).

| #   | Claim                                                                                                   | Verdict                   | Evidence                                                                                                                                                |
| --- | ------------------------------------------------------------------------------------------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | The stopgap is moved aside and put back if the play fails                                               | VERIFIED                  | r79-private-names.bash: `plan_on_cleanup restoreHandWritten`, then `mv pinned aside` before the play; restore runs on cleanup and after the play        |
| 2   | "Hand-written" is decided by pin's own header test (`qa-private-names pinned-state`)                    | VERIFIED                  | src-go/qa/privatenames/pin.go `pinnedState`: `strings.HasPrefix(old, PinnedHeader)` gives generated, else hand-written; the script calls `pinned-state` |
| 3   | A test holds every value off argv and the environment                                                   | VERIFIED                  | vaultlist_test.go `TestValuesTravelOnStdinOnlyUnderTheCheckoutsConfig` checks each call's line and Env for both values                                  |
| 4   | SecurityRules names `vault-list`                                                                        | VERIFIED                  | CLAUDE/SecurityRules.md:76                                                                                                                              |
| 5   | Real `ansible-vault decrypt --output -` stdin and `vault.bash encrypt` from a pipe run only on the host | UNVERIFIABLE-HERE         | The tests use a fake runner; a real run needs the vault password on the host                                                                            |
| 6   | The stopgap's names are imported whole, team names included                                             | VERIFIED                  | vaultlist.go `readImport` (line 326) adds every non-blank, non-comment line; no team-name filter                                                        |
| 7   | A team name kept that way stays in `private_name_terms` until the owner deletes the item                | VERIFIED (follows from 6) | `add` only appends; no remove path in vaultlist.go                                                                                                      |
| 8   | Context: the play runs pin with `chdir: root_dir`                                                       | VERIFIED                  | playbooks/imports/play-git-hooks-security.yml:146                                                                                                       |

No REFUTED claims.
