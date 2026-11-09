# Untracked sweep, second pass (Plan 00245 Task 2.2)

Every file was copied through a generic scrub (home and checkout paths, IPs, e-mail
addresses, container ids), then through the private-name guard, with each hit replaced by a
placeholder; a last grep looked for what the guard cannot know (client names, account names,
hostnames). Counts only; no names appear here. Nothing under `untracked/` was deleted or moved.

## Filed (163 files)

| Source                                     | Files | Home                                                                                                            |
| ------------------------------------------ | ----: | --------------------------------------------------------------------------------------------------------------- |
| `agent-reports/*.md`                       |    53 | the plan the report names, in `subagent-reports/`                                                               |
| `agent-reports/auto/`                      |    46 | same; a pair sharing one agent id keeps the larger reply                                                        |
| `scratch/*.md`                             |    38 | same                                                                                                            |
| `scratch/` subfolders                      |     7 | same (00143 report, 00161 unit reports, 00066 camera spec)                                                      |
| owner sheets and branch triage             |     3 | this folder: `OWNER-DECISIONS-2026-10-30.md`, `OWNER-UNBLOCK-2026-10-28.md`, `RESEARCH-branch-triage-261103.md` |
| top-level notes, private plan, host triage |    15 | `subagent-reports/swept/`                                                                                       |

Routing was by the plan number in the file name, else the plan number the text mentions
most. A report that names no existing plan is in `swept/`. A wrong guess still keeps the
content; move it when the right plan is known.

## Already tracked (not re-added)

57 files: 11 agent reports, 1 auto reply, 32 scratch files, 13 scratch-subfolder files. The
test: the five longest lines of the file are found in a tracked file on F44.

## Skipped, by kind

- compaction summaries of a coordinator: 16
- merge-conflict working copies: 32
- bus message drafts (`to-coord-*`, `to-seat*`): 49
- issue and comment body drafts (filed upstream): 28
- under 2.5 KB: 686
- duplicate short replies pointing at a saved full reply: 12
- third-party material that is not our work (downloaded model cards, vendor docs and
  READMEs, clones of public repositories, the vendored Claude permission-modes doc): about 45
- scratch trees that are test fixtures or build copies (`m1tree`, `mdv-branch`, `r2-red`,
  `sigpipe-red`, `review-watchleak`, `fable-mut`, `gofloor`, `m12v*`, `mut-followups2-r2`):
  not read; they are working trees, not documents

## Left for the owner (cannot be anonymised without losing the value)

1. The scoping report for Plan 00183 (`scratch/lts-00183-scoping.md`, 30 KB): built on the
   names of a sibling repository's hosts, VMs, accounts and team. 20 guard hits plus
   many it cannot know.
2. `plan-00235/old-range-decisions.tsv`: image ids and container data of the host; useless
   without them. It is an input to a deploy and says to stay under `untracked/`.
3. `reports/2026-09-02-skill-opportunities.md`: derived from conversation transcripts.
4. The client website analysis folder and `speech-recorder/`: client material, owner intent
   unknown.
5. `triage/` captures (`perf-261021`, `disk-261016`, `stt-*`) and `stt-triage-261027`,
   `stt-acceptance`: raw host captures (process lists, journals, audio).
6. a sibling repository's plan file under `scratch/` (2.7 KB).
7. The 36 other `wip-rescue/` files: the triage found them committed on branches other than
   F44; not repeated.
8. A full reply that vanished: the saved full reply of one 2026-10-28 store-repair worker is no
   longer in `agent-reports/auto/`; only its short pointer survived and was skipped as a
   duplicate of nothing. Its branch name is in the pointer.

## Code

No code pushed as branches by this pass. The four bus M1 rescues were pushed by the first
sweep and were not repeated; the `.patch` files were not touched.

## Method notes

- The guard has no file mode; each batch was staged and run with `staged`.
- The guard's list contains terms that are also part of this repository's own name; a
  replacement pass that ignores that would damage the text, so the repository name is
  protected during replacement.
- `qa-changed.bash`: see the commit body for what ran.
