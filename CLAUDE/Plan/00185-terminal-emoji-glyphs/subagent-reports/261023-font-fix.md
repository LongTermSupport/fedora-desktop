# Plan 00185 font fix: worker report

Branch `agent-a4b30c7e52ae391c7-45766f1c`. Not pushed, not merged. Review rounds are listed
at the end.

## Built

- `files/etc/fonts/conf.d/69-colour-emoji-fallback.conf`: Noto Color Emoji appended weakly
  after monospace, sans-serif and serif, and "en" recorded for it when fontconfig scans the
  font. The comment names the cost (text symbols) and that Flatpak apps are not reached.
- `playbooks/imports/play-fonts.yml`, renamed from `play-ms-fonts.yml`: installs the font,
  deploys the file, runs `fc-cache -f` and prints a reopen-terminals notice.
- `scripts/test-colour-emoji-fallback.bash`: the qa-gates suite, fontconfig against BDF
  fixture fonts (99 checks).
- The plan's `deploy.bash` and `acceptance.bash` (17 checks).
- `CLAUDE/Plan/meta-deploy.list`: entry `00185-terminal-emoji-glyphs` with its description
  (triage, play-fonts.yml, acceptance, triage again; reopen terminals afterwards).
  `scripts/test-meta-deploy.bash`: passed 26.

## The finding

FcFontMatch ranks a font that supports the language of the locale above any weak family
entry, so English-capable Symbola wins the single best match until "en" is recorded for
Noto Color Emoji at scan time. FcFontSort (the ordered list Pango walks) needs the weak
family entries. Both are needed.

### Reproduced with the real fonts, not only the fixtures

In the container (fontconfig 2.14.1, Debian conf.d), with Source Code Pro, Symbola, Noto
Emoji, Noto Color Emoji and DejaVu (downloaded into `untracked/scratch/real-fonts/`):

| Request, U+1F4C8                | without the file | with the file    |
| ------------------------------- | ---------------- | ---------------- |
| `Source Code Pro`, best match   | Symbola          | Noto Color Emoji |
| `Source Code Pro`, ordered list | Noto Emoji       | Noto Color Emoji |
| monospace, sans-serif, serif    | Symbola          | Noto Color Emoji |

Digits, `#`, `*` and the space stay on the text font. With the real fonts, the scan edit on
its own fixes only the single best match. The weak rules on their own fix only the ordered
list, and the sans-serif rule is the one that reaches Source Code Pro.

`acceptance.bash` was run in a harness with the planlib calls stubbed and a fake
`gsettings` (Ptyxis set to `Source Code Pro 11`). Against the real-font configs it gave
ACCEPTED, 17 of 17, with the file, and REJECTED, 8 of 17 failed, without it. A missing
Ptyxis schema, the system-font path and a font that is set but not installed each gave the
expected verdict.

## Round 1 fixes

- **B1.** The suite now has a text font for each generic family, `Fixture Code` (a font
  with no generic family, reaching sans-serif through the 49-sansserif rule), the
  distribution rules in Fedora's order, and the Symbola stand-in named `a-symbols.bdf` so
  that ties go to it. Mutation proof: each mutant of the file was run through a copy of the
  suite, then the real suite was run on the restored file.

  | Mutant                   | Result           |
  | ------------------------ | ---------------- |
  | scan edit removed        | red, 26 failed   |
  | monospace rule removed   | red, 12 failed   |
  | sans-serif rule removed  | red, 12 failed   |
  | serif rule removed       | red, 6 failed    |
  | all three pattern rules  | red, 30 failed   |
  | `binding="strong"`       | red, 25 failed   |
  | `mode="prepend"`         | red, 4 failed    |
  | prepend, sans-serif only | red, 2 failed    |
  | unchanged (restored)     | green, 99 passed |

  The counts are from the final suite, with `Fixture Late` preferred late for every family
  (added in rounds 2 and 3).
  `append_last` stays green: it still places the colour font after every text font.

  Scratch: `untracked/scratch/r2-mutants/mutate.py`.

- **B2.** The meta-deploy entry was added (above).

- **S1.** Acceptance checks 13-17 read the Ptyxis font: `use-system-font`, then
  `font-name` or the desktop `monospace-font-name`, with the size stripped. A missing
  setting, or a font that is not installed, fails check 13 and checks 14-17.

- **S2.** The trade-off is documented in the conf comment, `docs/playbooks.md` and
  PLAN.md. Acceptance check 17 confirms that U+2714, U+2713 and U+2600 under the terminal
  font come from a text font. The suite checks the same for `Fixture Code`.

- **S3.** `export FC_LANG=en` in the suite. It stays green under `LANG=de_DE.UTF-8`.

- **N2.** `family_for` turns a failed fc-match into a value that the checks reject.
  **N3.** Flatpak is noted. **N1** is folded into B1.

## Review rounds

- Round 1: CHANGES (`261023-font-fix-review-r1.md`).
- Round 2: MERGE (`261023-font-fix-review-r2.md`). Its items were applied as follows:
  - S1: F44 merged in. The meta-deploy entry is kept after F44's play-speech-to-text entry.
  - S2: fontconfig is declared in `.claude/ccy/Dockerfile`, which is the project image and
    so needs no CCY bump, and in `.github/workflows/qa.yml`.
  - N1: a later monospace `<prefer>` (`Fixture Late`) makes the prepend mutant red.
  - N2: a generic name as the terminal font now resolves to the font it stands for.
  - N3: a red check 17 prints the decision it hands the owner, and PLAN.md says the same.
- Round 3: MERGE (`261023-font-fix-review-r3.md`). Its nits were applied. The late prefer
  now covers every family, so a prepend on the sans-serif rule alone turns red.
  `Sans Serif` is accepted as a generic setting. The rounds are in the journal.
