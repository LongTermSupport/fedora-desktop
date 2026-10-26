# Plan 00185: review of the colour emoji fallback, round 2

Reviewer: adversarial, read-only. Subject: `git diff F44...HEAD` on the plan branch (three
commits, ending in the round-1 fixes). All experiments ran under `untracked/scratch/r2-review/`
with the container's fontconfig 2.14.1. The host has 2.17, and acceptance.bash is what covers
that difference there.

**Verdict: MERGE**, once S1 is handled. S1 is a merge conflict in `meta-deploy.list`, which the
merge has to resolve anyway. Every round-1 finding is fixed, and I checked each one by
experiment.

## Round-1 findings, re-checked

- **B1 (the suite could not see the rules): fixed.** I copied the suite and the conf into a
  scratch repo root and ran it against these mutants of the conf:

  | Mutant                                    | Result |
  | ----------------------------------------- | ------ |
  | unchanged                                 | 95/0   |
  | scan edit removed                         | 69/26  |
  | monospace rule removed                    | 83/12  |
  | sans-serif rule removed                   | 83/12  |
  | serif rule removed                        | 89/6   |
  | all three pattern rules removed           | 65/30  |
  | `binding="strong"`                        | 74/21  |
  | `mode="prepend"` (still weak)             | 95/0   |
  | `mode="append_last"`                      | 95/0   |
  | scan `lang` set with `assign`, not append | 95/0   |

  Every rule is load-bearing, and each of the four removals turns the suite red. The fixture
  file names now give ties to the Symbola stand-in. The fixture config follows Fedora's
  order, there are separate Sans/Serif/Code fonts, and the "no generic family" path is
  exercised. Prepend and append_last staying green is N1. Assign staying green does not
  matter.

- **B2 (host run not queued): fixed** (`meta-deploy.list:51-58`). The description is
  accurate: it says triage runs before and after, what the play does, and the 17 checks.

- **S1 (wrong terminal font): fixed.** I stubbed `_planlib`, `gsettings` and
  `FONTCONFIG_FILE` and ran acceptance.bash against the real fonts (Source Code Pro,
  Symbola, Noto Emoji, Noto Color Emoji, DejaVu) under the stock conf.d:

  - With the file, checks 2-17 pass. Check 1 fails only because nothing is under `/etc`
    here.
  - Without the file it is REJECTED on 9 checks: 3-7, 12, 14 and 15.
  - With `use-system-font=true` it reads the interface font.
  - A missing size, or a family fontconfig does not resolve, fails check 13 and then 14-17,
    with a reason, and COVERAGE stays 17/17. Fail-fast holds.

- **S2 (text symbols): fixed.** The trade-off is documented in the conf comment and in
  `docs/playbooks.md`. The suite checks it for the terminal path, and so does acceptance
  check 17. With the real fonts, U+2714, U+2713 and U+2600 stay on DejaVu Sans or Source
  Code Pro for Source Code Pro, monospace and sans-serif. The one regression I saw is
  `serif:charset=2714`, which moves from DejaVu Sans to Noto Color Emoji. That is exactly
  the trade-off the docs describe, and it is not a terminal path.

- **S3 (locale): fixed** (`export FC_LANG=en`, line 37).

- **N1-N3: fixed.** The fixture order is right. `family_for` turns an fc-match failure into a
  rejected value (`$?` is fc-match's status there). The Flatpak caveat is documented.

## Also verified

- The fix works on real fonts. For Source Code Pro, monospace, sans-serif and serif, the
  single best match gives Noto Color Emoji for all five of #60's code points. The digits,
  `#`, `*` and the space stay on the text font. In Source Code Pro's ordered list, Noto
  Color Emoji now comes before Noto Emoji and Symbola. Without the file it came after
  them.
- `ansible-playbook --syntax-check playbooks/imports/play-fonts.yml` passes, and shellcheck
  is clean on the suite, deploy.bash and acceptance.bash. Every script is 100755.
- No `play-ms-fonts` reference is left outside history and research documents. The
  retired-plays map, `playbook-main.yml`, the docs, `qa-gates.tsv`, `QA.md` and
  `addedgates_test.go` are all consistent.
- The bash limits do not apply: `scripts/test-*.bash` and the plan scripts are exempt.
- The diff and this report contain no install-specific names.

## Findings

### S1 (should-fix): the merge conflicts in `meta-deploy.list`

`CLAUDE/Plan/meta-deploy.list:35-58`. F44 has moved on since the branch base b1e298e1c. It
removed the rclone-tail, mpv and 00167 entries and queued `play-speech-to-text.yml`.
`git merge-tree F44 HEAD` reports a content conflict on exactly this file. Every other file
merges cleanly.

Fix: when merging, keep F44's speech-to-text entry with its description, and add the 00185
block after it. Drop the three entries that F44 has already removed. The two runs do not
depend on each other.

### S2 (should-fix): the new gate needs fontconfig, and nothing declares it for the container or CI

`scripts/qa-gates.tsv:66` and `scripts/test-colour-emoji-fallback.bash:43-48`.

- In the CCY container, `fc-cache` is there only because another package pulls it in.
  `files/var/local/claude-yolo/Dockerfile` never names fontconfig.
- In CI (ubuntu-latest), the workflow does not install it either. That probably works, but
  nobody has verified it, because the branch is not pushed and CI has not run.

The CLAUDE.md missing-dependency rule makes this the branch's task. Fix:

- Add `fontconfig` to the CCY Dockerfile's package list, with the mandatory CCY version
  bump.
- Either add `sudo apt-get install -y fontconfig` to `.github/workflows/qa.yml`, or record
  that the runner image ships it, once a CI run shows the gate green.

### N1 (nit): the suite cannot tell "after the generic name" from "before it"

`scripts/test-colour-emoji-fallback.bash:98-117` and the conf comment at lines 27-31. The
weak `prepend` and `append_last` mutants both stay green. The comment says that appending
after the generic name keeps the colour font behind every text font the distribution
prefers. That includes the `<prefer>` entries from files numbered after 69, such as
69-unifont. With `prepend`, a later prefer would land after the colour font. The suite has
no rule after the include, so it cannot see the difference.

Fix: in `sealed_config`, add one `<alias><family>monospace</family><prefer>` naming a
late text font with the SYMBOLS, after the `<include>`. Then check that it comes before Noto
Color Emoji in the ordered list. The prepend mutant then goes red.

### N2 (nit): check 13 rejects a generic family name in the font setting

`CLAUDE/Plan/00185-terminal-emoji-glyphs/acceptance.bash:212-215`. A setting such as
`Monospace 11`, or any alias, resolves to a different family name. The check then reports
"fontconfig gives 'DejaVu Sans Mono'" and fails 13-17. The host is not set up this way today.

Fix: if the family, lowercased, is `monospace`, `sans`, `sans-serif` or `serif`, use the
resolved family as `terminal_font` instead of rejecting.

### N3 (nit): nothing says what a failure of check 17 means

`acceptance.bash:237-244` and `PLAN.md` Task 2.2. On the host, a failure of check 17 is the
documented trade-off showing up for the terminal font. It is not a deploy fault. The plan
does not say what follows from it.

Fix: in the check-17 failure hint, and in Task 2.2, say that a red 17 hands the owner a
decision: accept the trade-off, or add a text symbol font to the sans-serif list.

VERDICT: MERGE (resolve S1 in the merge; S2 to be fixed in this branch or tracked; N1-N3 optional)
