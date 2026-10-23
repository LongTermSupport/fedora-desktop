# Plan 00185: review of the colour emoji fallback, round 1

Reviewer: adversarial, read-only. Subject: the staged diff on the plan branch (base HEAD
b1e298e1c; no commit existed yet when this was written). Experiments were run under
`untracked/scratch/r1/` with the container's fontconfig 2.14.1 (the host has 2.17).

**Verdict: CHANGES.** The fontconfig reasoning holds, and the deployed file is probably right.
But the QA suite that is meant to prove it passes with two of the file's three parts deleted,
and the host run is not in `meta-deploy.list`.

## What was verified (holds)

- **Ranking claim.** In FcFontMatch, charset ranks above strong family, which ranks above
  lang, which ranks above weak family. So for `monospace:charset=1f4c8` an English-capable
  Symbola beats a weak-listed, language-less Noto Color Emoji. Recording `en` at scan time
  makes the two tie on lang, and the weak family entry then decides. I reproduced this
  (`exp.bash`): without the file the symbol stand-in wins, with it the colour font wins.
  FcFontSort does follow the weak family once the language is satisfied. Even with
  `lang=de`, the colour font sits ahead of the symbol font in the ordered list.
- **Locale.** `lang=en`, `en-gb` and `en-us` all give the colour font. `lang=de` gives the
  symbol font back for the single best match, which the comment says it will. The ordered
  list Pango walks keeps the colour font first in every locale I tried.
- **Caches.** I built a user cache before the file and the system cache after it with the
  file (the host does have user `cache-9` files for system directories, triage-analysis
  "user fontconfig caches"). With both cache dirs configured, in either order, fontconfig
  read the newer system cache (`,en`). So `fc-cache -f` as root, run after the copy, is
  enough here. Flatpak apps have their own fontconfig and are not covered. Ptyxis is an
  RPM, so this does not matter for #60; worth one line in the docs.
- **Number 69.** It sorts after 45-latin, 49-sansserif, 60-latin and 65-nonlatin, and
  before 69-unifont (that file's `<prefer>` entries go in front of the generic name, so they
  still come before the colour font). Nothing in 70-90 edits the generic families. Fine.
- **Digits.** Strong binding is caught: the mutant fails 10 checks, all digit/#/\*/space.
  `Missing Font:charset=30` (the 49-sansserif path) stays on the text font when the fixture
  config is in Fedora's order.
- **Other checks.** `ansible-playbook --syntax-check playbooks/imports/play-fonts.yml`
  passes. Every changed script is 100755 in the index. The suite runs green (46/0). No
  install-specific names appear in the diff. Every reference to `play-ms-fonts.yml` outside
  history documents is renamed, and the retired-plays map is updated.

## Findings

### B1 (blocking): the suite cannot see the weak family entries, or the sans-serif and serif rules

File: `scripts/test-colour-emoji-fallback.bash:72-99`.

I built mutants of the conf file and ran the suite against each, by copying it into a
scratch repo root:

| Mutant                                                                 | Result          |
| ---------------------------------------------------------------------- | --------------- |
| scan edit removed                                                      | 25/21 red, good |
| `binding="strong"`                                                     | 36/10 red, good |
| **all three weak `<match target="pattern">` removed (scan edit only)** | **46/0 green**  |
| **`sans-serif` rule disabled**                                         | **46/0 green**  |

There are two causes:

1. **The tie-break favours the colour font by accident.** With both fonts carrying `en`
   and no family entry, the tie between "Fixture Symbols" and "Noto Color Emoji" is settled
   by scan order. `colour-emoji.bdf` sorts before `symbola.bdf`, so the colour font wins.
   I renamed the stand-in `a-symbola.bdf` and the scan-only mutant went 25/21 red. The
   suite's green is therefore a property of the fixture file names, not of the file under
   test.
2. **Every generic family resolves to `monospace`.** All three generics `<prefer>` Fixture
   Mono, and Fixture Mono `<default>`s to `monospace`. So `sans\-serif` and `serif`
   requests carry the token `monospace`, and the monospace rule appends the colour font for
   them. The config's order (the generic aliases before the 45-latin-style default) also
   differs from Fedora's.

Fix:

- Name the fixture files so that, without the family entries, the symbol stand-in wins the
  tie (for example `a-symbols.bdf`, `z-colour-emoji.bdf`). Add a check that the scan-only
  mutant is red, as an inline variant of the conf with the `pattern` matches stripped, in
  the same way the "without" config reproduces the defect.
- Add a distinct "Fixture Sans" text font for `sans-serif`/`serif`. Do not default it to
  `monospace`. Order the sealed config the way Fedora does: name-to-generic defaults, then
  the 49-sansserif rule, then the generic `<prefer>` aliases.
- Add a request for a font with no generic mapping (Ptyxis's Source Code Pro takes this
  path on the host: it is in neither 45-latin nor 60-latin, so 49-sansserif adds
  `sans-serif`).
- Then show each of the three pattern rules red when removed on its own.

### B2 (blocking): the host run is not queued

Files: `CLAUDE/Plan/meta-deploy.list` (not in the diff), and the Status line in `PLAN.md`
("waits on its host run through meta-deploy").

CLAUDE.md, PlanWorkflow.md:144-152 and PlanScriptStandards R9 all say the same commit adds
the run. Fix: add an entry for `00185-terminal-emoji-glyphs`, with a `#` description
(play-fonts.yml: installs Noto Color Emoji if missing, deploys the fallback file, rebuilds
the caches; then the 12 checks; reopen terminals afterwards). The plan folder has a
`triage.bash`, so a plain plan entry also runs triage first. Say in the description whether
that is wanted. If not, use the `<plan>/deploy.bash` script form.

### S1 (should-fix): acceptance checks the wrong terminal font

File: `CLAUDE/Plan/00185-terminal-emoji-glyphs/acceptance.bash:150-166`.

Checks 7, 10, 11 and 12 use `fc-match monospace`'s first family (Noto Sans Mono or DejaVu
Sans Mono on Fedora). But the analysis records that Ptyxis uses
`use-system-font=false, font-name='Source Code Pro 11'`. That font reaches the fallback by
a different route: 49-sansserif adds `sans-serif`, then the sans-serif rule appends the
colour font. As written, acceptance never exercises the font that showed #60.

Fix: read the terminal font from the settings, not a literal:

- Ptyxis: `gsettings get org.gnome.Ptyxis use-system-font` / `font-name`, or `dconf read`;
  if it uses the system font, `org.gnome.desktop.interface monospace-font-name`.
- Strip the trailing size, escape the family, and run checks 7, 10, 11 and 12 on it as
  well. A missing setting is a FAIL, not a skip.

### S2 (should-fix): text-presentation symbols move to the colour font

Shown in `exp.bash`. A character that the requested terminal font and every
monospace-listed font lack, but that an unlisted text font (DejaVu Sans, Noto Sans
Symbols 2) has, is now drawn from Noto Color Emoji in both the single best match and the
ordered list. Without the file it was drawn from the text font. For example, U+2714 and
U+2600 went from Fixture Sans to Noto Color Emoji under `monospace`.

In a terminal this means a square colour glyph in a width-1 cell for characters TUIs use
as plain symbols (check marks, arrows, ☀ ☺ ✂). On Fedora, DejaVu Sans Mono is in the
monospace list and covers most dingbats, so the effect may be small, but it is not
measured. Also, `fc-list :lang=en` now lists Noto Color Emoji, so language-filtered font
pickers will show it.

Fix: record this as a known trade-off in the conf comment and in `docs/playbooks.md`.
Add one acceptance check that a common text symbol (U+2714 or U+2713, U+2192) still comes
from a text font under the terminal font, so a regression is visible on the host.

### S3 (should-fix): the suite's result depends on the caller's locale

File: `scripts/test-colour-emoji-fallback.bash` (no `FC_LANG`/`LC_ALL` set).

Under a `de` locale the "with the file" single-best-match checks fail: I measured `lang=de`
giving Fixture Symbols. A gate that turns red because of where it ran is a gate people
learn to ignore. Fix: `export FC_LANG=en` (fontconfig honours it) near the top of the
suite, with a one-line comment saying why.

### N1 (nit): fixture config order

File: `scripts/test-colour-emoji-fallback.bash:85-88`. The order does not mirror Fedora
(45 name-to-generic before 60 generic `<prefer>`), so the `<default>` alias appends
`monospace` after the monospace alias has already run. This is folded into B1's fix.

### N2 (nit): acceptance aborts instead of rejecting when fc-match fails

File: `acceptance.bash:18` with `family_for` inside `$(...)`. Under `set -e`, a failing
`fc-match` ends the run before the COVERAGE/VERDICT lines are printed. The non-zero exit is
still correct (fail-fast holds), but the verdict wording is lost. Optional fix: capture the
rc in `family_for` and print `fc-match failed` as the got-value.

### N3 (nit): docs

File: `docs/playbooks.md:381-387`. Add that Flatpak apps read their own fontconfig and are
not changed by this file. The rename and the Microsoft fonts task need nothing more.

## Not findings (checked)

- **Rename versus a new play.** Renaming the play that already owns `fontconfig` is the
  right home (F3 in PLAN.md). The retired-plays map covers the ledger, and no other
  references remain.
- **Handlers.** The forced `fc-cache -f` handler is commented with its reason. The notice is
  a handler `debug`, not a warn-and-continue.
- **PlanScriptStandards.** The R1 bootstrap, R2 `plan_require_host`, R3
  `plan_prime_sudo` before `plan_start_log`, R6, R7 and R9 (acceptance as the last deploy
  leg, skipped under `--check` and said so) all hold. Setting `PLAN_FAILED_LEGS` directly
  in acceptance matches seven sibling acceptance scripts.
- **Name escaping.** `escaped()` handles `\`, `-`, `:` and `,`, which are the characters
  FcNameParse treats as special in the family part. The `sans\-serif` literal is right.
- **Gate registration.** The gate is registered consistently in `qa-gates.tsv`, `QA.md`
  and `addedgates_test.go`, and its output format matches the count reader's
  `passed:`/`failed:` lines.
