# Fact check: Plan 00192 PLAN.md diff (against F44 checkout)

## REFUTED

1. "`open` ... on `go-port/open`: ported and cut over, not merged" and "fixed on `go-port/open-2`" (PLAN.md L394-402). Both `go-port/open` and `go-port/open-2` are ancestors of F44 and of HEAD (`git merge-base --is-ancestor`). Change: say "merged into F44" like the sibling entries.
2. "Host checks H1-H5 queued in `meta-deploy.list`" (PLAN.md L399). `grep -n open CLAUDE/Plan/meta-deploy.list` finds 0 lines (50 lines in file, no open/H1 entry). Change: queue the entry, or drop the claim.

## Table

| #   | Claim                                                                      | Verdict           | Evidence                                                                                                       |
| --- | -------------------------------------------------------------------------- | ----------------- | -------------------------------------------------------------------------------------------------------------- |
| 1   | qa-go.bash retired; Go gate is go-qa-ci via `scripts/qa-go-qa.bash`        | VERIFIED          | `scripts/qa-go.bash` absent; `scripts/qa-go-qa.bash` present; 00191 PLAN.md L78, L113                          |
| 2   | qa-go.bash row off bash-limits.allowlist                                   | VERIFIED          | grep count 0                                                                                                   |
| 3   | go-qa-ci's nine bundled analyzers                                          | VERIFIED          | pinned module cache `analyzers/`: 9 analyzer dirs (excluding internal, pack)                                   |
| 4   | vmtest merged `3474744f`                                                   | VERIFIED          | ancestor of F44                                                                                                |
| 5   | ftp-camera merged `a069fd96`                                               | VERIFIED          | ancestor of F44                                                                                                |
| 6   | wsi-watermark merged `8c2ec4cc`, lows `df5b3b2f`                           | VERIFIED          | both ancestors of F44                                                                                          |
| 7   | `src-go/cmd/open`, `src-go/internal/open` exist                            | VERIFIED          | ls                                                                                                             |
| 8   | `play-cli-tools.yml` builds it                                             | VERIFIED          | `playbooks/imports/optional/common/play-cli-tools.yml` L30, L106 (`go_command_name: open`)                     |
| 9   | bash `open` and its allowlist row gone                                     | VERIFIED          | `files/home/.local/bin/open` absent; no allowlist row                                                          |
| 10  | reports `261019-p6-port-opus.md`, `261019-open-port-review-fable.md` exist | VERIFIED          | ls of subagent-reports                                                                                         |
| 11  | open merged/not merged (see REFUTED 1)                                     | REFUTED           | above                                                                                                          |
| 12  | H1-H5 queued in meta-deploy.list                                           | REFUTED           | above                                                                                                          |
| 13  | SPEC-open E1-E31, B1-B20                                                   | VERIFIED          | SPEC-open.md headings E1..E31, 20 B rows (11 B rows matched by my coarse grep; table shows B1-B20 in the diff) |
| 14  | `scripts/go-binary-suites.tsv` has `open qa-go`                            | VERIFIED          | line 23                                                                                                        |
| 15  | Diff beyond PLAN.md (SPEC-\* files, lines 502-617 of the diff)             | UNVERIFIABLE-HERE | not checked; scope was PLAN.md                                                                                 |
