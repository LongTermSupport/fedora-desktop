# Plan 00185: review of the colour emoji fallback, round 3

Reviewer: adversarial, read-only. Subject: `git diff F44...HEAD` (five commits), with focus on
`93a45b06a..HEAD` (498e7d120) and the merge 93a45b06a's resolution of
`CLAUDE/Plan/meta-deploy.list`. Experiments ran under `untracked/scratch/r3-review/` with the
container's fontconfig 2.14.1.

**Verdict: MERGE.** Every round-2 item is resolved and checked by experiment. The new changes
add no defects. Three nits are left, all optional.

## Round-2 findings, re-checked

- **S1 (meta-deploy conflict): resolved.** `git show --cc 93a45b06a` shows F44's three removed
  entries gone, F44's `play-speech-to-text.yml` entry and its description kept verbatim, and
  the 00185 block after it. `git diff F44...HEAD -- CLAUDE/Plan/meta-deploy.list` is exactly
  the 8 added lines. F44 has not moved since (tip c15620d06), and
  `git merge-tree --write-tree F44 HEAD` is clean.

- **S2 (fontconfig not declared): resolved.** `fontconfig` is in the package list of
  `.claude/ccy/Dockerfile:32`, with a reason comment, and `fc-match --version` is in the
  toolchain assertion (`:233`). This is the project image, which ContainerRules.md:48-52 names
  as the home for a project-only test dependency. Only the base image
  (`files/var/local/claude-yolo/`) needs the CCY bump, so not bumping is correct.
  `.github/workflows/qa.yml:73-75` installs it in CI inside the `set -euo pipefail` step.

- **N1 (prepend undetectable): resolved for monospace.** I ran my own mutant matrix
  (`tools/mutate.py`), with the suite copied into a scratch repo root beside each mutated conf:

  | Mutant                                | passed/failed           |
  | ------------------------------------- | ----------------------- |
  | unchanged                             | 96/0                    |
  | scan edit removed                     | 70/26                   |
  | scan lang `de` instead of `en`        | 70/26                   |
  | monospace rule removed                | 84/12                   |
  | sans-serif rule removed               | 84/12                   |
  | serif rule removed                    | 90/6                    |
  | all pattern rules removed             | 66/30                   |
  | strong, all / mono / sans / serif     | 74/22, 89/7, 87/9, 90/6 |
  | `prepend` on all three rules          | 95/1                    |
  | `prepend` on the sans-serif rule only | **96/0**                |
  | `prepend` on the serif rule only      | **96/0**                |
  | `prepend_first`                       | 77/19                   |
  | `append_last`                         | 96/0 (harmless)         |
  | scan `assign` instead of `append`     | 96/0 (harmless)         |

  These match the worker's reported counts (96, prepend 1, strong 22). The gap that remains is
  N1 below.

- **N2 (generic name as the terminal font): resolved.** I stubbed `_planlib`, `gsettings` and
  `FONTCONFIG_FILE`, and ran `acceptance.bash` against the real fonts (Source Code Pro, Symbola,
  Noto Emoji, Noto Color Emoji, DejaVu) under the stock conf.d:

  - `Source Code Pro 11`, `Monospace 11`, `use-system-font=true` with `Sans 10`, and
    `Serif 12` all resolve. Checks 13-17 pass, except check 17 for Serif (see N2 below).
  - Check 1 fails only because nothing is deployed under `/etc` in the container.
  - Without the file, the run is REJECTED on 3-7, 12, 14 and 15. COVERAGE is 17/17 in every
    case.

- **N3 (meaning of a red check 17): resolved.** The hint prints only when there are misses
  (seen in the Serif case). PLAN.md Task 2.2 says the same.

## Also verified

- On real fonts, with the file, all of #60's emoji go to Noto Color Emoji under monospace,
  sans-serif, serif, Source Code Pro and DejaVu Sans Mono. Without the file they go to Symbola
  or Noto Emoji. The digits, `#`, `*` and the space stay on the text font. U+2714 and U+2600
  stay on DejaVu for every terminal path. The one move is `serif:charset=2714`, to the colour
  font, which is the documented cost.
- `ansible-playbook --syntax-check playbooks/imports/play-fonts.yml` passes.
  `shellcheck -x` is clean on the suite and the plan scripts. All scripts are 100755.
- The gate registration (`qa-gates.tsv:66`, QA.md, `addedgates_test.go`) is unchanged and
  consistent.
- No install-specific names appear in the diff or in this report.

## Findings

### N1 (nit): the later-prefer guard covers monospace only, but the terminal route is sans-serif

`scripts/test-colour-emoji-fallback.bash:118` and `:164-167`. A `prepend` on the sans-serif
rule alone, or on the serif rule alone, stays 96/0. Source Code Pro reaches the fallback
through sans-serif (49-sansserif). A real distribution file after 69 also prefers fonts for
sans-serif and serif: 69-unifont prefers FreeSans, Code2000 and others there. So the case the
conf comment at lines 29-31 promises is unguarded on exactly the #60 route.

Fix: emit the late `<prefer>` for all three generics after the `<include>`, for example
`Fixture Late` for each. Then add the `first_of with <generic> "Fixture Late" "Noto Color Emoji"`
check for `sans\-serif`, `serif` and `Fixture Code`. Confirm that prepend-sans and
prepend-serif go red.

### N2 (nit): the wording of the check-17 hint, and a missed generic spelling

`CLAUDE/Plan/00185-terminal-emoji-glyphs/acceptance.bash:211-215` and `:252-255`.

- The hint always says "the sans-serif list". For a serif or monospace-named terminal font,
  the list that applies is that font's own generic.
- The case also misses `Sans Serif`, a common Pango spelling. `Sans Serif 11` fails check 13
  with "fontconfig gives 'DejaVu Sans'", and then fails 14-17.

Fix: add `"sans serif"` to the case pattern. Word the hint as "the generic family list the
terminal font falls back to (sans-serif for a font with no generic family)".

### N3 (nit): no journal entry for the round-2 work

`CLAUDE/Plan/00185-terminal-emoji-glyphs/JOURNAL/00185-Journal-26-10-23.md`. The last entry
(10:54) covers round 1. The round-2 changes are recorded only in the worker report
`261023-font-fix.md`: the Late fixture, the prepend mutant now red, the fontconfig
declarations, and the merge resolution. Fix: append one `finding` entry through
`mkplan.bash --journal`.

VERDICT: MERGE
