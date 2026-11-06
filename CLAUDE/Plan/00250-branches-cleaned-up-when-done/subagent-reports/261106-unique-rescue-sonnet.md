# Plan 00250 Task 2.3: the 15 UNIQUE scratch files rescued

Source: the table "Every UNIQUE file, and where it belongs" in `SCRATCH-REVIEW-261106.md`.
All 15 files are committed under their target names. Each `.md` has the provenance line under
its title; the `.py` has it as its first comment. No file in any worktree was changed.

## Where each file went

| From (worktree, untracked/…)                                    | To (CLAUDE/Plan/…)                                                                             |
| --------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| agent-a5ee80f7… agent-reports/261023-ccy-pids-limit.md          | 00181-ccy-session-limits/subagent-reports/261023-ccy-pids-limit.md                             |
| agent-a5ee80f7… agent-reports/261023-ccy-pids-limit-review-1.md | 00181-ccy-session-limits/subagent-reports/261023-ccy-pids-limit-review-1.md                    |
| agent-a5ee80f7… agent-reports/261023-ccy-pids-limit-review-2.md | 00181-ccy-session-limits/subagent-reports/261023-ccy-pids-limit-review-2.md                    |
| agent-a5ee80f7… scratch/261023-a5ee80f767118a918-handoff.md     | 00181-ccy-session-limits/subagent-reports/261023-ccy-pids-limit-handoff.md                     |
| agent-ad49d488… agent-reports/261023-ccy-go-cache-opus-5-5.md   | 00213-agent-bus-performance-go-carve-out/subagent-reports/261023-ccy-go-cache.md               |
| agent-ad49d488… agent-reports/261023-ccy-go-cache-review-r1..r4 | 00213-agent-bus-performance-go-carve-out/subagent-reports/261023-ccy-go-cache-review-r1..r4.md |
| agent-ad49d488… agent-reports/261023-go-cache-handoff.md        | 00213-agent-bus-performance-go-carve-out/subagent-reports/261023-ccy-go-cache-handoff.md       |
| agent-ae87230b… scratch/plan-173-followup.md                    | 00173-shutdown-with-update-always-goes-down/subagent-reports/261013-evening-run-cc-warning.md  |
| bus-v2-s1-declare scratch/261021-s1-declare-reader-notes.md     | 00209-agent-bus-v2-in-go/subagent-reports/261021-s1-declare-builder.md                         |
| bus-v2-s1-declare scratch/261021-s1-pycompat-notes.md           | 00209-agent-bus-v2-in-go/subagent-reports/261021-s1-pycompat-builder.md                        |
| bus-v2-s1-declare scratch/261021-s1-seat-reader-notes.md        | 00209-agent-bus-v2-in-go/subagent-reports/261021-s1-seat-builder.md                            |
| bus-v2-s1-declare scratch/261021-s1-declare-gen-fixtures.py     | 00209-agent-bus-v2-in-go/s1-declare-gen-fixtures.py                                            |

## Plan folders

- 00181 exists (`CLAUDE/Plan/00181-ccy-session-limits`, Not Started). It had no
  `subagent-reports/`, so one was made.
- 00213 exists (`CLAUDE/Plan/00213-agent-bus-performance-go-carve-out`, In Progress). The
  table names no plan that owns the go-cache branch, so 00213 is the nearest fit: it holds the
  problem report the build report cites.
- 00173 exists (Blocked). Its `subagent-reports/` was also made new.

## Scrubbing

- The pids handoff gave the worktree as an absolute path under the container mount, which is
  now `<repo>/.claude/worktrees/…`. Its snapshot design also named the session-record
  directory by absolute path, which is now `<repo>/.claude/ccy/`.
- The pids DBF report gave a home-directory cache path for the DBF spec, which is now
  `<dbf-spec-cache>/SPEC.md`.
- The other references were left as they were, because none of them is specific to an install:
  - `/workspace` as the container's mount point in the design text;
  - `~/.cache/ccy/<project>`, ccy's design path;
  - `/home/<user>`, already a placeholder;
  - agent branch names, which are origin refs.
- No container names, usernames, hostnames or IDs were found. The hook guards passed every
  Write.

## The generator (.py)

- Its bytes were copied with `cp`, not retyped through Write. The file holds literal invisible
  whitespace (NBSP, U+3000, U+2003, U+202F, U+001F) in the `comment-whitespace.yml` case,
  and a retyped copy could have lost those bytes without anyone noticing. The provenance
  comment was then added with Edit.
- `ruff check` was already clean. `ruff format --check` failed, so `ruff format` was applied.
  The change is layout and quote style only.
- Proof that it does the same thing: run from an empty scratch directory, before and after the
  format, it wrote 138 files. `diff -r` against F44's
  `src-go/internal/bus/declare/testdata/parity/` found them IDENTICAL.
- One edit to content: `261021-s1-declare-builder.md` names the generator by its old scratch
  path, so a note was added giving its new name in this plan.

## QA and commit

- `./scripts/qa-changed.bash`: passed on all 16 staged files (ruff, docs, plan-tests and the
  others). The log is in `untracked/scratch/qa-changed-unique-rescue.log`.
- The journal entry is `00250-…/JOURNAL/2026-11-06/170635Z-action-150c4b.md`.
- Only these files were staged, each by explicit path. The commit was made with
  `git commit -F` and was not pushed.

## Dogfood fault (not logged: this seat cannot file)

`hooks-daemon find-plan` warns on every call that a venv under
`.claude/hooks-daemon/untracked/` records Python 3.14 but now runs 3.13, and that it is
skipping it. The command still answers. The venv's directory name also embeds the host
checkout path. The coordinator should log this.
